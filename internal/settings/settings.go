// Package settings loads and saves herd's files under ~/.herd: config.toml,
// agents.toml, context/*.md and keymap.json.
package settings

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"

	"github.com/BurntSushi/toml"
)

// Project is a git repository registered in Settings > Projects.
type Project struct {
	ID           string `toml:"id" json:"id"`
	Name         string `toml:"name" json:"name"`
	Path         string `toml:"path" json:"path"`
	Repo         string `toml:"repo" json:"repo"`
	BaseBranch   string `toml:"base_branch" json:"baseBranch"`
	WorktreesDir string `toml:"worktrees_dir" json:"worktreesDir"`
}

// Appearance holds the theme and fonts.
type Appearance struct {
	Theme        string  `toml:"theme" json:"theme"` // dark | light | system
	UIFont       string  `toml:"ui_font" json:"uiFont"`
	UIFontSize   float64 `toml:"ui_font_size" json:"uiFontSize"`
	TermFont     string  `toml:"terminal_font" json:"termFont"`
	TermFontSize float64 `toml:"terminal_font_size" json:"termFontSize"`
}

// ContextMeta is the per-context settings stored in config.toml; the markdown
// itself lives in context/<id>.md.
type ContextMeta struct {
	ClaudeMd  bool   `toml:"claude_md" json:"claudeMd"`
	Knowledge string `toml:"knowledge,omitempty" json:"knowledge"` // a knowledge base folder sessions can read
}

// Config is config.toml.
type Config struct {
	Appearance      Appearance             `toml:"appearance" json:"appearance"`
	ZedCommand      string                 `toml:"zed_command" json:"zedCommand"`
	ClaudeCommand   string                 `toml:"claude_command" json:"claudeCommand"`
	MaxLiveSessions int                    `toml:"max_live_sessions" json:"maxLiveSessions"`
	CopilotTimeout  int                    `toml:"copilot_timeout_minutes" json:"copilotTimeoutMinutes"`
	Notifications   bool                   `toml:"notifications" json:"notifications"` // desktop notifications while herd is in the background
	MaxTabs         int                    `toml:"max_tabs" json:"maxTabs"`             // open terminal tabs; the oldest closes first
	Rules           string                 `toml:"rules" json:"rules"`                  // always followed by every agent
	Projects        []Project              `toml:"project" json:"projects"`
	Context         map[string]ContextMeta `toml:"context" json:"contextMeta"`
}

// Agent is one step type, from agents.toml. The order of agents is the pipeline.
type Agent struct {
	ID           string   `toml:"id" json:"id"`
	Name         string   `toml:"name" json:"name"`
	Command      string   `toml:"command" json:"command"`
	Model        string   `toml:"model" json:"model"`
	Trigger      string   `toml:"trigger" json:"trigger"` // manual | after:<agent id>
	Writes       string   `toml:"writes" json:"writes"`
	Preview      string   `toml:"preview,omitempty" json:"preview"`
	Done         string   `toml:"done" json:"done"`
	Commit       bool     `toml:"commit,omitempty" json:"commit"`
	Push         bool     `toml:"push,omitempty" json:"push"`
	Instructions string   `toml:"instructions" json:"instructions"`
}

type agentsFile struct {
	Agent []Agent `toml:"agent"`
}

// Context is a markdown block given to every session.
type Context struct {
	ID        string `json:"id"` // "general" or a project id
	Text      string `json:"text"`
	ClaudeMd  bool   `json:"claudeMd"`
	Knowledge string `json:"knowledge"`
}

// Keymap maps an action to its key bindings, e.g. "palette": ["Mod+P"].
type Keymap map[string][]string

// Snapshot is everything the frontend needs from Settings.
type Snapshot struct {
	Config  Config             `json:"config"`
	Agents  []Agent            `json:"agents"`
	Context map[string]Context `json:"context"`
	Keymap  Keymap             `json:"keymap"`
	Dir     string             `json:"dir"`
}

// Store reads and writes the settings files. It is safe for concurrent use.
type Store struct {
	Dir string
	mu  sync.RWMutex
	cfg Config
	ags []Agent
	ctx map[string]Context
	km  Keymap
}

// Open loads (and creates with defaults where missing) the settings in dir.
func Open(dir string) (*Store, error) {
	s := &Store{Dir: dir}
	for _, d := range []string{dir, filepath.Join(dir, "context"), filepath.Join(dir, "runs"), filepath.Join(dir, "views")} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			return nil, err
		}
	}
	if err := s.Reload(); err != nil {
		return nil, err
	}
	return s, nil
}

func (s *Store) path(name string) string { return filepath.Join(s.Dir, name) }

// Reload re-reads every file from disk.
func (s *Store) Reload() error {
	cfg := DefaultConfig()
	if b, err := os.ReadFile(s.path("config.toml")); err == nil {
		if _, err := toml.Decode(string(b), &cfg); err != nil {
			return fmt.Errorf("config.toml: %w", err)
		}
	} else if errors.Is(err, os.ErrNotExist) {
		if err := writeTOML(s.path("config.toml"), cfg); err != nil {
			return err
		}
	} else {
		return err
	}
	fillConfig(&cfg)

	var af agentsFile
	if b, err := os.ReadFile(s.path("agents.toml")); err == nil {
		if _, err := toml.Decode(string(b), &af); err != nil {
			return fmt.Errorf("agents.toml: %w", err)
		}
	} else if errors.Is(err, os.ErrNotExist) {
		af.Agent = DefaultAgents()
		if err := writeTOML(s.path("agents.toml"), af); err != nil {
			return err
		}
	} else {
		return err
	}
	if len(af.Agent) == 0 {
		af.Agent = DefaultAgents()
	}

	km := DefaultKeymap()
	if b, err := os.ReadFile(s.path("keymap.json")); err == nil {
		var user Keymap
		if err := json.Unmarshal(b, &user); err != nil {
			return fmt.Errorf("keymap.json: %w", err)
		}
		for k, v := range user {
			km[k] = v
		}
	} else if errors.Is(err, os.ErrNotExist) {
		b, _ := json.MarshalIndent(km, "", "  ")
		if err := os.WriteFile(s.path("keymap.json"), b, 0o644); err != nil {
			return err
		}
	}

	ctx := map[string]Context{}
	ids := []string{"general"}
	for _, p := range cfg.Projects {
		ids = append(ids, p.ID)
	}
	for _, id := range ids {
		meta, ok := cfg.Context[id]
		if !ok {
			meta = ContextMeta{ClaudeMd: id != "general"}
		}
		text := ""
		if b, err := os.ReadFile(filepath.Join(s.Dir, "context", id+".md")); err == nil {
			text = string(b)
		} else if id == "general" {
			text = DefaultGeneralContext
			_ = os.WriteFile(filepath.Join(s.Dir, "context", "general.md"), []byte(text), 0o644)
		}
		ctx[id] = Context{ID: id, Text: text, ClaudeMd: meta.ClaudeMd, Knowledge: meta.Knowledge}
	}

	s.mu.Lock()
	s.cfg, s.ags, s.ctx, s.km = cfg, af.Agent, ctx, km
	s.mu.Unlock()
	return nil
}

func fillConfig(c *Config) {
	d := DefaultConfig()
	if c.Appearance.Theme == "" {
		c.Appearance.Theme = d.Appearance.Theme
	}
	if c.Appearance.UIFont == "" {
		c.Appearance.UIFont = d.Appearance.UIFont
	}
	if c.Appearance.UIFontSize == 0 {
		c.Appearance.UIFontSize = d.Appearance.UIFontSize
	}
	if c.Appearance.TermFont == "" {
		c.Appearance.TermFont = d.Appearance.TermFont
	}
	if c.Appearance.TermFontSize == 0 {
		c.Appearance.TermFontSize = d.Appearance.TermFontSize
	}
	if c.ZedCommand == "" {
		c.ZedCommand = d.ZedCommand
	}
	if c.ClaudeCommand == "" {
		c.ClaudeCommand = d.ClaudeCommand
	}
	if c.MaxLiveSessions <= 0 {
		c.MaxLiveSessions = d.MaxLiveSessions
	}
	if c.CopilotTimeout <= 0 {
		c.CopilotTimeout = d.CopilotTimeout
	}
	if c.MaxTabs <= 0 {
		c.MaxTabs = d.MaxTabs
	}
	if c.Context == nil {
		c.Context = map[string]ContextMeta{}
	}
	for i := range c.Projects {
		p := &c.Projects[i]
		if p.ID == "" {
			p.ID = p.Name
		}
		if p.Name == "" {
			p.Name = p.ID
		}
		if p.WorktreesDir == "" {
			p.WorktreesDir = ".worktrees"
		}
		if p.BaseBranch == "" {
			p.BaseBranch = "main"
		}
	}
}

// Snapshot returns a copy of all settings.
func (s *Store) Snapshot() Snapshot {
	s.mu.RLock()
	defer s.mu.RUnlock()
	ctx := make(map[string]Context, len(s.ctx))
	for k, v := range s.ctx {
		ctx[k] = v
	}
	km := make(Keymap, len(s.km))
	for k, v := range s.km {
		km[k] = v
	}
	cfg := s.cfg
	cfg.Projects = append([]Project(nil), s.cfg.Projects...)
	return Snapshot{Config: cfg, Agents: append([]Agent(nil), s.ags...), Context: ctx, Keymap: km, Dir: s.Dir}
}

// Config returns a copy of config.toml.
func (s *Store) Config() Config { return s.Snapshot().Config }

// Agents returns the pipeline in order.
func (s *Store) Agents() []Agent {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return append([]Agent(nil), s.ags...)
}

// Agent returns one agent by id.
func (s *Store) Agent(id string) (Agent, bool) {
	for _, a := range s.Agents() {
		if a.ID == id {
			return a, true
		}
	}
	return Agent{}, false
}

// Project returns one project by id.
func (s *Store) Project(id string) (Project, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	for _, p := range s.cfg.Projects {
		if p.ID == id {
			return p, true
		}
	}
	return Project{}, false
}

// Context returns one context block.
func (s *Store) Context(id string) (Context, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	c, ok := s.ctx[id]
	return c, ok
}

// SaveAgents replaces agents.toml.
func (s *Store) SaveAgents(agents []Agent) error {
	seen := map[string]bool{}
	for _, a := range agents {
		if a.ID == "" || a.Name == "" {
			return errors.New("every agent needs an id and a name")
		}
		if seen[a.ID] {
			return fmt.Errorf("duplicate agent id %q", a.ID)
		}
		seen[a.ID] = true
	}
	if err := writeTOML(s.path("agents.toml"), agentsFile{Agent: agents}); err != nil {
		return err
	}
	s.mu.Lock()
	s.ags = append([]Agent(nil), agents...)
	s.mu.Unlock()
	return nil
}

// SaveContext writes one context block.
func (s *Store) SaveContext(c Context) error {
	if c.ID == "" {
		return errors.New("context id required")
	}
	if err := os.WriteFile(filepath.Join(s.Dir, "context", c.ID+".md"), []byte(c.Text), 0o644); err != nil {
		return err
	}
	s.mu.Lock()
	s.ctx[c.ID] = c
	cfg := s.cfg
	cfg.Context = map[string]ContextMeta{}
	for k, v := range s.cfg.Context {
		cfg.Context[k] = v
	}
	cfg.Context[c.ID] = ContextMeta{ClaudeMd: c.ClaudeMd, Knowledge: c.Knowledge}
	s.cfg = cfg
	s.mu.Unlock()
	return s.writeConfig()
}

// SaveProject adds or replaces a project (matched by id).
func (s *Store) SaveProject(p Project) error {
	if p.ID == "" || p.Path == "" {
		return errors.New("project id and path are required")
	}
	s.mu.Lock()
	found := false
	projects := append([]Project(nil), s.cfg.Projects...)
	for i := range projects {
		if projects[i].ID == p.ID {
			projects[i] = p
			found = true
		}
	}
	if !found {
		projects = append(projects, p)
	}
	s.cfg.Projects = projects
	fillConfig(&s.cfg)
	if _, ok := s.ctx[p.ID]; !ok {
		s.ctx[p.ID] = Context{ID: p.ID, ClaudeMd: true}
	}
	s.mu.Unlock()
	return s.writeConfig()
}

// RemoveProject deletes a project from config.toml (its tasks stay in the db).
func (s *Store) RemoveProject(id string) error {
	s.mu.Lock()
	var keep []Project
	for _, p := range s.cfg.Projects {
		if p.ID != id {
			keep = append(keep, p)
		}
	}
	s.cfg.Projects = keep
	s.mu.Unlock()
	return s.writeConfig()
}

// UpdateConfig applies fn to a copy of config.toml and saves it.
func (s *Store) UpdateConfig(fn func(*Config)) error {
	s.mu.Lock()
	cfg := s.cfg
	cfg.Projects = append([]Project(nil), s.cfg.Projects...)
	fn(&cfg)
	fillConfig(&cfg)
	s.cfg = cfg
	s.mu.Unlock()
	return s.writeConfig()
}

func (s *Store) writeConfig() error {
	s.mu.RLock()
	cfg := s.cfg
	s.mu.RUnlock()
	return writeTOML(s.path("config.toml"), cfg)
}

func writeTOML(path string, v any) error {
	var buf bytes.Buffer
	enc := toml.NewEncoder(&buf)
	enc.Indent = ""
	if err := enc.Encode(v); err != nil {
		return err
	}
	return writeAtomic(path, buf.Bytes())
}

func writeAtomic(path string, b []byte) error {
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, b, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

// DisplayName is the step name for the nth run of an agent: "Implement 2",
// or just "Plan" when the agent's artifact has no {n}.
func (a Agent) DisplayName(n int) string {
	if strings.Contains(a.Writes, "{n}") {
		return fmt.Sprintf("%s %d", a.Name, n)
	}
	return a.Name
}

// Numbered reports whether runs of this agent are numbered.
func (a Agent) Numbered() bool { return strings.Contains(a.Writes, "{n}") }

// ArtifactName is the file this agent writes for run n.
func (a Agent) ArtifactName(n int) string {
	return strings.ReplaceAll(a.Writes, "{n}", fmt.Sprint(n))
}

// PreviewName is the file the Preview tab shows for run n.
func (a Agent) PreviewName(n int) string {
	if a.Preview != "" {
		return strings.ReplaceAll(a.Preview, "{n}", fmt.Sprint(n))
	}
	return a.ArtifactName(n)
}

// AfterAgent returns the agent id this one starts after, or "".
func (a Agent) AfterAgent() string {
	if strings.HasPrefix(a.Trigger, "after:") {
		return strings.TrimPrefix(a.Trigger, "after:")
	}
	return ""
}

// DoneRule parses "status: done" or "verdict: approve | changes" into a key
// and its accepted values.
func (a Agent) DoneRule() (key string, values []string) {
	d := strings.TrimSpace(a.Done)
	d = strings.TrimPrefix(d, "front matter ")
	k, v, ok := strings.Cut(d, ":")
	if !ok {
		return "status", []string{"done"}
	}
	for _, x := range strings.Split(v, "|") {
		if x = strings.TrimSpace(x); x != "" {
			values = append(values, x)
		}
	}
	sort.Strings(values)
	return strings.TrimSpace(k), values
}
