package app

// Edits outside the task's worktree: an agent may change files in another
// repository, or in the project's main checkout. herd records the files its
// edit tools touched (PostToolUse), shows their uncommitted diffs with the
// step's changes, and warns before Push, which only pushes the worktree.

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"herd/internal/hooks"
	"herd/internal/store"
	"herd/internal/zed"
)

// ExternalRepo is a repository outside the worktree that a task changed.
type ExternalRepo struct {
	Root string `json:"root"`
	Name string `json:"name"`
}

// editTools are the Claude Code tools that write a file at a known path.
var editTools = map[string]bool{"Edit": true, "MultiEdit": true, "Write": true, "NotebookEdit": true}

// recordExternal keeps the file an edit tool wrote when it is outside the
// task's worktree and run dir, inside a git repository.
func (a *App) recordExternal(ev hooks.Event) {
	var p struct {
		ToolName  string `json:"tool_name"`
		ToolInput struct {
			FilePath     string `json:"file_path"`
			NotebookPath string `json:"notebook_path"`
		} `json:"tool_input"`
	}
	if json.Unmarshal(ev.Raw, &p) != nil || !editTools[p.ToolName] {
		return
	}
	path := p.ToolInput.FilePath
	if path == "" {
		path = p.ToolInput.NotebookPath
	}
	if !filepath.IsAbs(path) {
		return
	}
	path = filepath.Clean(path)
	s, err := a.db.Step(ev.StepID)
	if err != nil {
		return
	}
	t, err := a.db.Task(s.TaskID)
	if err != nil {
		return
	}
	pr := a.projectOf(t)
	for _, dir := range []string{worktreePath(pr, t), a.runDir(pr, t), a.Home} {
		if within(path, dir) {
			return
		}
	}
	root := a.repoRoot(filepath.Dir(path))
	if root == "" {
		return
	}
	if err := a.db.AddExternal(store.ExternalFile{StepID: s.ID, Path: path, Root: root}); err == nil {
		a.emitTask(t.ID)
	}
}

// within reports whether path is dir or inside it.
func within(path, dir string) bool {
	rel, err := filepath.Rel(dir, path)
	return err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}

// repoRoot returns the top of the git repository holding dir (its nearest
// existing parent), or "".
func (a *App) repoRoot(dir string) string {
	for {
		if st, err := os.Stat(dir); err == nil && st.IsDir() {
			break
		}
		up := filepath.Dir(dir)
		if up == dir {
			return ""
		}
		dir = up
	}
	out, err := a.git.Run(a.ctx, dir, "rev-parse", "--show-toplevel")
	if err != nil {
		return ""
	}
	return strings.TrimSpace(out)
}

// externalChanges returns the recorded files that still have uncommitted
// changes (Path absolute, Repo set), and their repositories.
func (a *App) externalChanges(t store.Task, files []store.ExternalFile) ([]FileChange, []ExternalRepo) {
	byRoot := map[string][]string{}
	var roots []string
	for _, f := range files {
		if _, ok := byRoot[f.Root]; !ok {
			roots = append(roots, f.Root)
		}
		if rel, err := filepath.Rel(f.Root, f.Path); err == nil {
			byRoot[f.Root] = append(byRoot[f.Root], rel)
		}
	}
	changes, repos := []FileChange{}, []ExternalRepo{}
	for _, root := range roots {
		fcs, err := a.uncommitted(root, byRoot[root]...)
		if err != nil || len(fcs) == 0 {
			continue
		}
		for _, fc := range fcs {
			fc.Path, fc.Repo = filepath.Join(root, fc.Path), root
			changes = append(changes, fc)
		}
		repos = append(repos, ExternalRepo{Root: root, Name: a.repoName(t, root)})
	}
	return changes, repos
}

// taskExternal lists the repositories outside the worktree where the task
// left uncommitted changes.
func (a *App) taskExternal(t store.Task) []ExternalRepo {
	files, err := a.db.Externals(t.ID)
	if err != nil || len(files) == 0 {
		return []ExternalRepo{}
	}
	_, repos := a.externalChanges(t, files)
	return repos
}

// repoName names a repository: a herd project's name, the task's own
// project for its main checkout, else the folder name.
func (a *App) repoName(t store.Task, root string) string {
	if root == a.projectOf(t).Path {
		return a.projectOf(t).Name + " (main checkout)"
	}
	for _, p := range a.settings.Config().Projects {
		if p.Path == root {
			return p.Name
		}
	}
	return filepath.Base(root)
}

// OpenRepoInZed opens a repository outside the worktree that the task changed.
func (a *App) OpenRepoInZed(taskID, root string) error {
	files, err := a.db.Externals(taskID)
	if err != nil {
		return err
	}
	for _, f := range files {
		if f.Root == root {
			return zed.Open(a.settings.Config().ZedCommand, root)
		}
	}
	return fmt.Errorf("the task did not change %s", root)
}
