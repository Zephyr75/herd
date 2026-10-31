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
	"io/fs"
	"log"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
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
}

// Events emitted to the frontend.
const (
	EvTask     = "task:changed"
	EvStep     = "step:changed"
	EvArtifact = "artifact:changed"
	EvSettings = "settings:changed"
	EvToast    = "toast"
)

// App is the bound service.
type App struct {
	Home      string // ~/.herd
	PluginSrc fs.FS  // the plugin/ folder shipped with the app

	settings *settings.Store
	db       *store.DB
	run      *runner.Runner
	git      gitops.Git
	gh       github.GH
	plat     Platform

	token string
	port  int
	srv   *http.Server

	ctx    context.Context
	cancel context.CancelFunc
	mu     sync.Mutex // serialises step and task state transitions

	pollMu  sync.Mutex
	copilot map[string]bool // task ids with a running Copilot poller
}

// New creates the service; call Start before use.
func New(home string, plugin fs.FS) *App {
	return &App{Home: home, PluginSrc: plugin, copilot: map[string]bool{}}
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
	if a.db, err = store.Open(filepath.Join(a.Home, "herd.db")); err != nil {
		return err
	}
	a.run = runner.New(a.onExit)

	tok := make([]byte, 24)
	if _, err := rand.Read(tok); err != nil {
		return err
	}
	a.token = hex.EncodeToString(tok)
	if err := hooks.WriteSettings(a.hooksPath()); err != nil {
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
		a.run.StopAll()
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

func (a *App) hooksPath() string { return filepath.Join(a.Home, "hooks.json") }
func (a *App) pluginDir() string { return filepath.Join(a.Home, "plugin") }
func (a *App) baseURL() string   { return fmt.Sprintf("http://127.0.0.1:%d", a.port) }

func (a *App) emit(ev string, data any) {
	if a.plat != nil {
		a.plat.Emit(ev, data)
	}
}

func (a *App) toast(title, body string, isErr bool) {
	a.emit(EvToast, map[string]any{"title": title, "body": body, "error": isErr})
}

// loop runs the reaper, the PR state poller and the Copilot pollers.
func (a *App) loop() {
	reap := time.NewTicker(20 * time.Second)
	prs := time.NewTicker(3 * time.Minute)
	defer reap.Stop()
	defer prs.Stop()
	a.resumeCopilotPollers()
	go a.pollPRs()
	for {
		select {
		case <-a.ctx.Done():
			return
		case <-reap.C:
			a.reap()
		case <-prs.C:
			go a.pollPRs()
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
	cfg := a.settings.Config()
	name := filepath.Base(dir)
	id := name
	for i := 2; ; i++ {
		if _, taken := a.settings.Project(id); !taken {
			break
		}
		id = fmt.Sprintf("%s-%d", name, i)
	}
	start := 7100
	for _, p := range cfg.Projects {
		var lo, hi int
		if _, err := fmt.Sscanf(p.PortRange, "%d-%d", &lo, &hi); err == nil && hi+1 > start {
			start = hi + 1
		}
	}
	p := settings.Project{
		ID: id, Name: name, Path: dir, Repo: a.git.GitHubRepo(a.ctx, dir), BaseBranch: a.git.DefaultBranch(a.ctx, dir),
		WorktreesDir: ".worktrees", PortRange: fmt.Sprintf("%d-%d", start, start+99),
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
			if err := a.resume(stepID); err != nil {
				return "", err
			}
		}
	}
	return a.termURL(stepID), nil
}

// ResumeStep reopens a step's Claude Code conversation (the Resume buttons)
// and returns its socket URL.
func (a *App) ResumeStep(stepID string) (string, error) {
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
