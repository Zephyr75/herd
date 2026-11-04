package app

// Safety net for edits outside the task's worktree: an agent may write into
// another repository, or the project's main checkout, by mistake. herd
// records the files its edit tools touched (PostToolUse) and warns before
// Commit and push while they are still uncommitted.

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"

	"herd/internal/hooks"
	"herd/internal/store"
)

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
	path := filepath.Clean(p.ToolInput.FilePath + p.ToolInput.NotebookPath)
	s, err := a.db.Step(ev.StepID)
	if err != nil || !filepath.IsAbs(path) {
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
	if root := a.repoRoot(filepath.Dir(path)); root != "" {
		if a.db.AddExternal(store.ExternalFile{StepID: s.ID, Path: path, Root: root}) == nil {
			a.emitTask(t.ID)
		}
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
		if up := filepath.Dir(dir); up != dir {
			dir = up
		} else {
			return ""
		}
	}
	out, err := a.git.Run(a.ctx, dir, "rev-parse", "--show-toplevel")
	if err != nil {
		return ""
	}
	return strings.TrimSpace(out)
}

// outsideRepos names the repositories outside the worktree where the task's
// agents left uncommitted edits.
func (a *App) outsideRepos(t store.Task) []string {
	files, _ := a.db.Externals(t.ID)
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
	names := []string{}
	for _, root := range roots {
		if fcs, err := a.uncommitted(root, byRoot[root]...); err == nil && len(fcs) > 0 {
			name := filepath.Base(root)
			if root == a.projectOf(t).Path {
				name = a.projectOf(t).Name + " (main checkout)"
			}
			names = append(names, name)
		}
	}
	return names
}
