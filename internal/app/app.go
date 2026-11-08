// Package app is herd's service: it owns all state and every Claude Code
// process, and its exported methods are bound to the frontend by Wails.
package app

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"herd/internal/artifacts"
	"herd/internal/github"
	"herd/internal/gitops"
	"herd/internal/hooks"
	"herd/internal/runner"
	"herd/internal/settings"
	"herd/internal/store"
	"herd/internal/termws"
	"herd/internal/zed"
)

// Platform is what the app needs from the desktop shell.
type Platform interface {
	Emit(event string, data any)
	PickFolder(title string) (string, error)
	// PickFiles opens a native picker for several files; nil when cancelled.
	PickFiles(title string) ([]string, error)
	// Notify shows a desktop notification; a click on it should focus
	// taskID in herd.
	Notify(title, body, taskID string)
}

// Events emitted to the frontend.
const (
	EvTask     = "task:changed"
	EvRemoved  = "task:removed"
	EvStep     = "step:changed"
	EvArtifact = "artifact:changed"
	EvSettings = "settings:changed"
	EvToast    = "toast"
)

// App is the bound service.
type App struct {
	Home string // ~/.herd
	// HoldCmd runs a session holder (herd's own path and "hold"), so
	// sessions outlive herd. Empty: sessions end when herd quits.
	HoldCmd []string

	settings *settings.Store
	db       *store.DB
	run      *runner.Runner
	git      gitops.Git
	gh       github.GH
	plat     Platform

	token string
	port  int
	srv   *http.Server

	focused atomic.Bool // the herd window has focus (no notifications then)

	ctx    context.Context
	cancel context.CancelFunc
	mu     sync.Mutex // serialises step and task state transitions

	pollMu sync.Mutex // one pollPRs at a time

	checksMu sync.Mutex
	checks   map[string][]github.Check    // CI checks of each open PR, by task id
	labels   map[string][]string          // labels of each open PR, by task id
	drafts   map[string]bool              // whether each open PR is a draft, by task id
	conflict map[string]bool              // whether each open PR conflicts with its base, by task id
	feedback map[string][]github.Feedback // what reviewers still ask for on each open PR, by task id
	behind   map[string]int               // commits of origin/<base> each task's branch lacks, by task id

	busy sync.Map // ids of the steps whose post-success actions (git, automatic next steps) are running
}

// New creates the service; call Start before use.
func New(home string) *App {
	return &App{Home: home, checks: map[string][]github.Check{}, labels: map[string][]string{}, drafts: map[string]bool{}, conflict: map[string]bool{}, feedback: map[string][]github.Feedback{}, behind: map[string]int{}}
}

// DefaultHome returns ~/.herd, or $HERD_HOME when set.
func DefaultHome() string {
	if h := os.Getenv("HERD_HOME"); h != "" {
		return h
	}
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".herd")
}

// Start opens the stores, starts the local server and background loops.
// It is a function rather than a method so Wails does not bind it.
func Start(a *App, ctx context.Context, plat Platform) error {
	a.ctx, a.cancel = context.WithCancel(ctx)
	a.plat = plat
	var err error
	if a.settings, err = settings.Open(a.Home); err != nil {
		return err
	}
	// A task's file lives in its run directory, with its artifacts.
	taskFile := func(t store.Task) string { return filepath.Join(a.runDir(a.projectOf(t), t), store.TaskFile) }
	if a.db, err = store.Open(a.Home, taskFile); err != nil {
		return err
	}
	a.run = runner.New(a.onExit)
	a.run.Hold, a.run.SockDir = a.HoldCmd, filepath.Join(a.Home, "sessions")
	a.run.Sweep()
	a.focused.Store(true)

	tok := make([]byte, 24)
	if _, err := rand.Read(tok); err != nil {
		return err
	}
	a.token = hex.EncodeToString(tok)
	if err := hooks.WriteSettings(a.hooksPath(), userMemory()); err != nil {
		return err
	}
	if err := a.writePlugin(); err != nil {
		log.Printf("plugin: %v", err)
	}

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return err
	}
	a.port = ln.Addr().(*net.TCPAddr).Port
	// Hooks of sessions started by an earlier herd read the current address
	// and token from here.
	endpoint := fmt.Sprintf("HERD_URL=%s\nHERD_TOKEN=%s\n", a.baseURL(), a.token)
	if err := os.WriteFile(a.endpointPath(), []byte(endpoint), 0o600); err != nil {
		return err
	}
	mux := http.NewServeMux()
	mux.Handle("/hook", &hooks.Receiver{Token: a.token, OnEvent: a.onHook})
	mux.Handle("/term/", &termws.Handler{Token: a.token, Runner: a.run})
	a.srv = &http.Server{Handler: mux, ReadHeaderTimeout: 10 * time.Second}
	go func() {
		if err := a.srv.Serve(ln); err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Printf("server: %v", err)
		}
	}()

	a.reconcile()
	_ = a.settings.Watch(a.ctx, func() {
		if err := a.writePlugin(); err != nil {
			log.Printf("plugin: %v", err)
		}
		a.emit(EvSettings, a.settings.Snapshot())
	})
	_ = artifacts.Watch(a.ctx, filepath.Join(a.Home, "runs"), a.onArtifact)
	go a.loop()
	return nil
}

// Stop ends every session and closes the stores.
func Stop(a *App) {
	if a.cancel != nil {
		a.cancel()
	}
	if a.run != nil {
		// Steps still working keep running in their holders; the next herd
		// reattaches them. Finished and paused ones end.
		a.run.Shutdown(func(stepID string) bool {
			s, err := a.db.Step(stepID)
			active := s.State == store.Running || s.State == store.Idle || s.State == store.Approval
			return err == nil && active && !a.paused(stepID)
		})
	}
	if a.srv != nil {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		_ = a.srv.Shutdown(ctx)
		cancel()
	}
	if a.db != nil {
		_ = a.db.Close()
	}
}

// userMemory is the user's own CLAUDE.md and rules: herd sessions skip them,
// so agents follow only herd's Rules, context and the repo's CLAUDE.md.
func userMemory() []string {
	d := claudeDir()
	return []string{filepath.Join(d, "CLAUDE.md"), filepath.Join(d, "rules", "**")}
}

// SystemMemory is the state of the user's own ~/.claude/CLAUDE.md compared
// with herd's General context and Rules (Settings > Context).
type SystemMemory struct {
	Path   string `json:"path"`
	Exists bool   `json:"exists"`
	Synced bool   `json:"synced"` // it holds exactly what Override would write
}

// systemClaudeMd is what Override writes: the General context, the Rules,
// then the knowledge base folders (General, and each project's for its repo).
func (a *App) systemClaudeMd() string {
	var b strings.Builder
	b.WriteString("<!-- Written by herd from Settings > Context: General context, Rules and knowledge base folders. -->\n\n")
	if c, ok := a.settings.Context("general"); ok && strings.TrimSpace(c.Text) != "" {
		b.WriteString(strings.TrimSpace(c.Text))
		b.WriteString("\n")
	}
	if r := strings.TrimSpace(a.settings.Config().Rules); r != "" {
		b.WriteString("\n# Rules\n\nAlways follow these rules:\n\n")
		b.WriteString(r)
		b.WriteString("\n")
	}
	var kb []string
	if c, ok := a.settings.Context("general"); ok && isDir(c.Knowledge) {
		kb = append(kb, "- "+expandHome(c.Knowledge))
	}
	for _, p := range a.settings.Config().Projects {
		if c, ok := a.settings.Context(p.ID); ok && isDir(c.Knowledge) {
			kb = append(kb, fmt.Sprintf("- %s (when working in %s)", expandHome(c.Knowledge), p.Path))
		}
	}
	if len(kb) > 0 {
		b.WriteString("\n# Knowledge base\n\nReference folders you can read (never edit them). Search them for background on the domain, conventions and past decisions:\n\n")
		b.WriteString(strings.Join(kb, "\n"))
		b.WriteString("\n")
	}
	return b.String()
}

func isDir(p string) bool {
	if p == "" {
		return false
	}
	fi, err := os.Stat(expandHome(p))
	return err == nil && fi.IsDir()
}

// SystemClaudeMd reports whether ~/.claude/CLAUDE.md matches herd's context.
func (a *App) SystemClaudeMd() SystemMemory {
	m := SystemMemory{Path: filepath.Join(claudeDir(), "CLAUDE.md")}
	if b, err := os.ReadFile(m.Path); err == nil {
		m.Exists, m.Synced = true, string(b) == a.systemClaudeMd()
	}
	return m
}

// OverrideSystemClaudeMd replaces ~/.claude/CLAUDE.md with the General
// context and the Rules. A different existing file is first copied to
// CLAUDE.md.bak-<time> next to it; that path is returned.
func (a *App) OverrideSystemClaudeMd() (string, error) {
	m := a.SystemClaudeMd()
	if m.Synced {
		return "", nil
	}
	backup := ""
	if m.Exists {
		old, err := os.ReadFile(m.Path)
		if err != nil {
			return "", err
		}
		backup = m.Path + ".bak-" + time.Now().Format("20060102-150405")
		if err := os.WriteFile(backup, old, 0o644); err != nil {
			return "", fmt.Errorf("could not back up %s: %w", m.Path, err)
		}
	}
	if err := os.MkdirAll(filepath.Dir(m.Path), 0o755); err != nil {
		return backup, err
	}
	tmp := m.Path + ".herd-tmp"
	if err := os.WriteFile(tmp, []byte(a.systemClaudeMd()), 0o644); err != nil {
		return backup, err
	}
	return backup, os.Rename(tmp, m.Path)
}

func (a *App) hooksPath() string    { return filepath.Join(a.Home, "hooks.json") }
func (a *App) endpointPath() string { return filepath.Join(a.Home, "endpoint") }
func (a *App) pluginDir() string    { return filepath.Join(a.Home, "plugin") }
func (a *App) baseURL() string      { return fmt.Sprintf("http://127.0.0.1:%d", a.port) }

func (a *App) emit(ev string, data any) {
	if a.plat != nil {
		a.plat.Emit(ev, data)
	}
}

func (a *App) toast(title, body string, isErr bool) {
	a.emit(EvToast, map[string]any{"title": title, "body": body, "error": isErr})
}

// loop runs the reaper and the PR poller (state and Copilot reviews).
func (a *App) loop() {
	reap := time.NewTicker(20 * time.Second)
	prs := time.NewTicker(time.Minute)
	drift := time.NewTicker(5 * time.Minute)
	defer reap.Stop()
	defer prs.Stop()
	defer drift.Stop()
	go a.pollPRs()
	go a.pollDrift()
	for {
		select {
		case <-a.ctx.Done():
			return
		case <-reap.C:
			a.reap()
		case <-prs.C:
			go a.pollPRs()
		case <-drift.C:
			go a.pollDrift()
		}
	}
}

func (a *App) reap() {
	cfg := a.settings.Config()
	a.run.Reap(2*time.Minute, cfg.MaxLiveSessions, func(stepID string) bool {
		s, err := a.db.Step(stepID)
		return err == nil && (s.State == store.Done || s.State == store.Failed)
	})
}

// ---- bound methods: bootstrap and settings ----

// Bootstrap is everything the frontend needs at launch.
type Bootstrap struct {
	Settings settings.Snapshot `json:"settings"`
	Tasks    []TaskView        `json:"tasks"`
	Tabs     json.RawMessage   `json:"tabs"`
	OS       string            `json:"os"`
}

// Bootstrap returns settings, tasks and the open tabs.
func (a *App) Bootstrap() (Bootstrap, error) {
	tasks, err := a.ListTasks()
	if err != nil {
		return Bootstrap{}, err
	}
	tabs, _ := a.db.Get("tabs")
	if tabs == "" {
		tabs = "null"
	}
	return Bootstrap{Settings: a.settings.Snapshot(), Tasks: tasks, Tabs: json.RawMessage(tabs), OS: runtime.GOOS}, nil
}

// SaveTabs persists the open tabs (opaque JSON from the frontend).
func (a *App) SaveTabs(tabs string) error {
	if !json.Valid([]byte(tabs)) {
		return errors.New("tabs: invalid JSON")
	}
	return a.db.Set("tabs", tabs)
}

// ListProjects returns the registered projects.
func (a *App) ListProjects() []settings.Project { return a.settings.Config().Projects }

// GetSettings returns every setting.
func (a *App) GetSettings() settings.Snapshot { return a.settings.Snapshot() }

// SaveAgents replaces the pipeline.
func (a *App) SaveAgents(agents []settings.Agent) error {
	if err := a.settings.SaveAgents(agents); err != nil {
		return err
	}
	if err := a.writePlugin(); err != nil {
		return err
	}
	a.emit(EvSettings, a.settings.Snapshot())
	a.emitAllTasks()
	return nil
}

// SaveContext writes one context block.
func (a *App) SaveContext(c settings.Context) error {
	if err := a.settings.SaveContext(c); err != nil {
		return err
	}
	a.emit(EvSettings, a.settings.Snapshot())
	return nil
}

// SaveProject adds or updates a project.
func (a *App) SaveProject(p settings.Project) error {
	if !a.git.IsRepo(a.ctx, p.Path) {
		return fmt.Errorf("%s is not a git repository", p.Path)
	}
	if err := a.settings.SaveProject(p); err != nil {
		return err
	}
	if pat := excludePattern(p); pat != "" {
		_, _ = a.git.EnsureExcluded(a.ctx, p.Path, pat)
	}
	a.emit(EvSettings, a.settings.Snapshot())
	a.emitAllTasks() // sister offers depend on the projects
	return nil
}

// RemoveProject unregisters a project; its tasks and worktrees are kept.
func (a *App) RemoveProject(id string) error {
	if err := a.settings.RemoveProject(id); err != nil {
		return err
	}
	a.emit(EvSettings, a.settings.Snapshot())
	return nil
}

// AddProject asks for a folder and registers it with values read from git.
func (a *App) AddProject() (*settings.Project, error) {
	dir, err := a.plat.PickFolder("Add project")
	if err != nil || dir == "" {
		return nil, err
	}
	return a.AddProjectPath(dir)
}

// AddProjectPath registers the repository at dir.
func (a *App) AddProjectPath(dir string) (*settings.Project, error) {
	dir = expandHome(dir)
	if !a.git.IsRepo(a.ctx, dir) {
		return nil, fmt.Errorf("%s is not a git repository", dir)
	}
	if top, err := a.git.Run(a.ctx, dir, "rev-parse", "--show-toplevel"); err == nil {
		dir = top
	}
	name := filepath.Base(dir)
	id := name
	for i := 2; ; i++ {
		if _, taken := a.settings.Project(id); !taken {
			break
		}
		id = fmt.Sprintf("%s-%d", name, i)
	}
	p := settings.Project{
		ID: id, Name: name, Path: dir, Repo: a.git.GitHubRepo(a.ctx, dir), BaseBranch: a.git.DefaultBranch(a.ctx, dir),
		WorktreesDir: ".worktrees",
	}
	if err := a.SaveProject(p); err != nil {
		return nil, err
	}
	return &p, nil
}

// SetTheme sets dark, light or system.
func (a *App) SetTheme(theme string) error {
	switch theme {
	case "dark", "light", "system":
	default:
		return fmt.Errorf("unknown theme %q", theme)
	}
	return a.updateConfig(func(c *settings.Config) { c.Appearance.Theme = theme })
}

// SaveAppearance saves fonts and the Zed command.
func (a *App) SaveAppearance(ap settings.Appearance, zedCommand string) error {
	return a.updateConfig(func(c *settings.Config) {
		theme := c.Appearance.Theme
		c.Appearance = ap
		if ap.Theme == "" {
			c.Appearance.Theme = theme
		}
		c.ZedCommand = zedCommand
	})
}

// SetNotifications turns desktop notifications on or off.
func (a *App) SetNotifications(on bool) error {
	return a.updateConfig(func(c *settings.Config) { c.Notifications = on })
}

// SetMaxTabs sets how many terminal tabs stay open.
func (a *App) SetMaxTabs(n int) error {
	if n < 1 {
		return fmt.Errorf("keep at least one tab")
	}
	return a.updateConfig(func(c *settings.Config) { c.MaxTabs = n })
}

// SetBranchPrefix sets the prefix every new task's branch starts with.
func (a *App) SetBranchPrefix(prefix string) error {
	prefix = strings.TrimSpace(prefix)
	if prefix == "" {
		return errors.New("the branch prefix cannot be empty")
	}
	if _, err := a.git.Run(a.ctx, a.Home, "check-ref-format", "--branch", prefix+"x"); err != nil {
		return fmt.Errorf("%q cannot start a branch name", prefix)
	}
	return a.updateConfig(func(c *settings.Config) { c.BranchPrefix = prefix })
}

// SetRules saves the rules every agent follows.
func (a *App) SetRules(rules string) error {
	return a.updateConfig(func(c *settings.Config) { c.Rules = rules })
}

// SetPrompt saves one of the prompts herd adds to agents (Settings >
// Context > Prompts); an empty text restores the default.
func (a *App) SetPrompt(id, text string) error {
	if err := a.settings.SetPrompt(id, text); err != nil {
		return err
	}
	return a.writePlugin()
}

// AgentCommand is the full command an agent gets, as Settings shows it: the
// agent as being edited, between herd's prompts.
func (a *App) AgentCommand(ag settings.Agent) string {
	return a.CommandText(ag)
}

// ChooseFolder opens the native folder picker; "" when cancelled.
func (a *App) ChooseFolder(title string) (string, error) {
	return a.plat.PickFolder(title)
}

// ChooseFiles opens the native picker for several files; empty when cancelled.
func (a *App) ChooseFiles(title string) ([]string, error) {
	return a.plat.PickFiles(title)
}

// SetFocused is called by the frontend when the window gains or loses
// focus: notifications are only shown while herd is in the background.
func (a *App) SetFocused(on bool) { a.focused.Store(on) }

// notify tells the user, through the desktop, that a step needs them or ended.
func (a *App) notify(s store.Step) {
	if a.focused.Load() || !a.settings.Config().Notifications || a.plat == nil {
		return
	}
	t, err := a.db.Task(s.TaskID)
	if err != nil {
		return
	}
	name := agentFor(a.settings.Agents(), s.Agent).DisplayName(s.N)
	var title string
	switch s.State {
	case store.Approval:
		title = name + " needs approval"
	case store.Idle:
		title = name + " is waiting for you"
	case store.Done:
		title = name + " finished"
	case store.Failed:
		title = name + " failed"
	default:
		return
	}
	ref := "LOCAL"
	if t.Issue != "" {
		ref = "#" + t.Issue
	}
	a.plat.Notify(title, fmt.Sprintf("%s %s · %s", ref, t.Title, a.projectOf(t).Name), t.ID)
}

func (a *App) updateConfig(fn func(*settings.Config)) error {
	if err := a.settings.UpdateConfig(fn); err != nil {
		return err
	}
	a.emit(EvSettings, a.settings.Snapshot())
	return nil
}

// Check is one prerequisite row in Settings > Projects.
type Check struct {
	Project string `json:"project"` // "" for global checks
	OK      bool   `json:"ok"`
	Label   string `json:"label"`
	Detail  string `json:"detail"`
}

// CheckPrerequisites checks zed, gh and each project's worktree exclude.
func (a *App) CheckPrerequisites() []Check {
	cfg := a.settings.Config()
	var out []Check
	zc := cfg.ZedCommand
	out = append(out, Check{OK: zed.Available(zc), Label: "Zed CLI", Detail: fmt.Sprintf("%q on PATH", strings.Fields(zc + " ")[0])})
	ghOK, ghMsg := a.gh.Available(a.ctx)
	out = append(out, Check{OK: ghOK, Label: "GitHub CLI", Detail: orStr(ghMsg, "gh authenticated")})
	for _, p := range cfg.Projects {
		if !a.git.IsRepo(a.ctx, p.Path) {
			out = append(out, Check{Project: p.ID, OK: false, Label: "Repository", Detail: p.Path + " is not a git repository"})
			continue
		}
		if pat := excludePattern(p); pat == "" {
			out = append(out, Check{Project: p.ID, OK: true, Label: "Worktrees", Detail: worktreesRoot(p) + " (outside the repository)"})
		} else if !a.git.IsExcluded(a.ctx, p.Path, pat) {
			if _, err := a.git.EnsureExcluded(a.ctx, p.Path, pat); err != nil {
				out = append(out, Check{Project: p.ID, OK: false, Label: "Worktrees excluded", Detail: err.Error()})
				continue
			}
			out = append(out, Check{Project: p.ID, OK: true, Label: "Worktrees excluded", Detail: pat + " in .git/info/exclude"})
		} else {
			out = append(out, Check{Project: p.ID, OK: true, Label: "Worktrees excluded", Detail: pat + " in .git/info/exclude"})
		}
		if p.Repo != "" && ghOK {
			ok := a.gh.CanAccess(a.ctx, p.Repo)
			out = append(out, Check{Project: p.ID, OK: ok, Label: "GitHub access", Detail: p.Repo})
		}
	}
	return out
}

// TerminalURL returns the socket URL for a step's terminal. A live session
// is attached; a session that ended during this run replays its output so
// the user sees why it exited; otherwise (after a restart) the Claude Code
// conversation is resumed.
func (a *App) TerminalURL(stepID string) (string, error) {
	if _, ok := a.run.Get(stepID); !ok {
		if _, ended := a.run.Ended(stepID); !ended {
			if a.paused(stepID) {
				return "", errors.New("this session is paused: press Resume")
			}
			if err := a.elsewhereErr(stepID); err != nil {
				return "", err
			}
			if err := a.resume(stepID); err != nil {
				return "", err
			}
		}
	}
	return a.termURL(stepID), nil
}

// ResumeStep reopens a step's Claude Code conversation (the Resume buttons)
// and returns its socket URL. It refuses while another process runs the
// session (see SessionElsewhere); ResumeStepAnyway does not.
func (a *App) ResumeStep(stepID string) (string, error) {
	if _, ok := a.run.Get(stepID); !ok {
		if err := a.elsewhereErr(stepID); err != nil {
			return "", err
		}
	}
	return a.ResumeStepAnyway(stepID)
}

// elsewhereErr explains that another process runs the step's session.
func (a *App) elsewhereErr(stepID string) error {
	ps, err := a.SessionElsewhere(stepID)
	if err != nil || len(ps) == 0 {
		return nil
	}
	return fmt.Errorf("Claude Code is still running this session in another process (pid %d: %s). Exit it there first, so only one process writes the conversation", ps[0].PID, ps[0].Cmd)
}

// ResumeStepAnyway resumes even when another process runs the session.
func (a *App) ResumeStepAnyway(stepID string) (string, error) {
	a.setPaused(stepID, false)
	if _, ok := a.run.Get(stepID); !ok {
		if err := a.resume(stepID); err != nil {
			return "", err
		}
	}
	return a.termURL(stepID), nil
}

func (a *App) termURL(stepID string) string {
	return fmt.Sprintf("ws://127.0.0.1:%d/term/%s?token=%s", a.port, url.PathEscape(stepID), a.token)
}

func orStr(s, d string) string {
	if s == "" {
		return d
	}
	return s
}

func expandHome(p string) string {
	if strings.HasPrefix(p, "~/") {
		home, _ := os.UserHomeDir()
		return filepath.Join(home, p[2:])
	}
	return p
}
