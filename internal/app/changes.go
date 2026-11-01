package app

import (
	"bytes"
	"errors"
	"fmt"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"

	"herd/internal/zed"
)

// FileChange is one file a step changed.
type FileChange struct {
	Path    string `json:"path"`
	Status  string `json:"status"` // A added, M modified, D deleted, ? untracked
	Added   int    `json:"added"`
	Deleted int    `json:"deleted"`
	Binary  bool   `json:"binary"`
}

// Changes is what a step changed: its commit, the commit of the step right
// after it (Implement's work is committed by its Review), or the uncommitted
// work in the worktree while it is the task's latest step.
type Changes struct {
	Source string       `json:"source"` // commit | worktree | "" (nothing to show)
	SHA    string       `json:"sha"`
	Via    string       `json:"via"` // the step that made the commit, when not this one
	Files  []FileChange `json:"files"`
}

// StepChanges lists the files a step changed, with line counts.
func (a *App) StepChanges(stepID string) (Changes, error) {
	s, err := a.db.Step(stepID)
	if err != nil {
		return Changes{}, err
	}
	t, err := a.db.Task(s.TaskID)
	if err != nil {
		return Changes{}, err
	}
	p := a.projectOf(t)
	out := Changes{Files: []FileChange{}}
	steps, _ := a.db.Steps(t.ID)
	sha := s.SHA
	for i, x := range steps {
		if x.ID == s.ID && sha == "" && i+1 < len(steps) && steps[i+1].SHA != "" {
			sha = steps[i+1].SHA
			out.Via = agentFor(a.settings.Agents(), steps[i+1].Agent).DisplayName(steps[i+1].N)
		}
	}
	if sha != "" {
		out.Source, out.SHA = "commit", sha
		num, err := a.git.Run(a.ctx, p.Path, "-c", "core.quotePath=false", "show", "--no-renames", "--format=", "--numstat", sha)
		if err != nil {
			return out, err
		}
		names, err := a.git.Run(a.ctx, p.Path, "-c", "core.quotePath=false", "show", "--no-renames", "--format=", "--name-status", sha)
		if err != nil {
			return out, err
		}
		out.Files = parseChanges(num, names)
		return out, nil
	}
	wt := worktreePath(p, t)
	if len(steps) == 0 || steps[len(steps)-1].ID != s.ID || t.WorktreeRemoved {
		return out, nil
	}
	if _, err := os.Stat(wt); err != nil {
		return out, nil
	}
	out.Source = "worktree"
	num, err := a.git.Run(a.ctx, wt, "-c", "core.quotePath=false", "diff", "--no-renames", "--numstat", "HEAD")
	if err != nil {
		return out, err
	}
	names, err := a.git.Run(a.ctx, wt, "-c", "core.quotePath=false", "diff", "--no-renames", "--name-status", "HEAD")
	if err != nil {
		return out, err
	}
	out.Files = parseChanges(num, names)
	untracked, err := a.git.Run(a.ctx, wt, "-c", "core.quotePath=false", "ls-files", "--others", "--exclude-standard")
	if err != nil {
		return out, err
	}
	for _, rel := range strings.Split(untracked, "\n") {
		if rel == "" {
			continue
		}
		fc := FileChange{Path: rel, Status: "?"}
		if b, err := os.ReadFile(filepath.Join(wt, rel)); err == nil && len(b) < 4<<20 {
			if bytes.IndexByte(b, 0) >= 0 {
				fc.Binary = true
			} else {
				fc.Added = bytes.Count(b, []byte("\n"))
				if len(b) > 0 && b[len(b)-1] != '\n' {
					fc.Added++
				}
			}
		}
		out.Files = append(out.Files, fc)
	}
	return out, nil
}

// parseChanges joins git's --numstat and --name-status outputs.
func parseChanges(numstat, nameStatus string) []FileChange {
	status := map[string]string{}
	for _, l := range strings.Split(nameStatus, "\n") {
		if st, path, ok := strings.Cut(l, "\t"); ok {
			status[path] = st[:1]
		}
	}
	files := []FileChange{}
	for _, l := range strings.Split(numstat, "\n") {
		f := strings.SplitN(l, "\t", 3)
		if len(f) != 3 {
			continue
		}
		fc := FileChange{Path: f[2], Status: status[f[2]]}
		if f[0] == "-" {
			fc.Binary = true
		} else {
			fc.Added, _ = strconv.Atoi(f[0])
			fc.Deleted, _ = strconv.Atoi(f[1])
		}
		if fc.Status == "" {
			fc.Status = "M"
		}
		files = append(files, fc)
	}
	return files
}

// OpenFileInZed opens one changed file in Zed. With a commit sha it opens a
// read-only copy of the file as it was in that commit (under ~/.herd/views,
// pruned at startup), or the commit's diff when the commit deleted it.
// Without one it opens the file in the worktree.
func (a *App) OpenFileInZed(taskID, rel, sha string) error {
	t, err := a.db.Task(taskID)
	if err != nil {
		return err
	}
	p := a.projectOf(t)
	wt := worktreePath(p, t)
	path := filepath.Join(wt, rel)
	if r, err := filepath.Rel(wt, path); err != nil || strings.HasPrefix(r, "..") {
		return errors.New("the file is outside the worktree")
	}
	zedCmd := a.settings.Config().ZedCommand
	if sha == "" {
		if _, err := os.Stat(path); err != nil {
			return fmt.Errorf("%s no longer exists in the worktree", rel)
		}
		return zed.OpenFile(zedCmd, path)
	}
	full, err := a.git.Run(a.ctx, p.Path, "rev-parse", "--verify", sha+"^{commit}")
	if err != nil {
		return err
	}
	body, err := a.git.FileAt(a.ctx, p.Path, full, filepath.ToSlash(rel))
	if err != nil { // deleted in this commit: show the commit instead
		return zed.Open(zedCmd, "zed://git/commit/"+full+"?repo="+url.QueryEscape(wt))
	}
	snap := filepath.Join(a.Home, "views", p.Name, full[:12], rel)
	if err := os.MkdirAll(filepath.Dir(snap), 0o755); err != nil {
		return err
	}
	_ = os.Chmod(snap, 0o644)
	if err := os.WriteFile(snap, body, 0o444); err != nil {
		return err
	}
	return zed.OpenFile(zedCmd, snap)
}

// Proc is a process running a Claude Code session.
type Proc struct {
	PID int    `json:"pid"`
	Cmd string `json:"cmd"`
}

// SessionElsewhere lists processes outside herd that run a step's session,
// e.g. claude --resume <id> in a Zed terminal. Resuming it in herd as well
// would put two processes on one conversation.
func (a *App) SessionElsewhere(stepID string) ([]Proc, error) {
	if _, live := a.run.Get(stepID); live {
		return []Proc{}, nil // herd's own process
	}
	s, err := a.db.Step(stepID)
	if err != nil {
		return nil, err
	}
	return sessionProcs(s.SessionID), nil
}

// sessionProcs finds processes whose arguments name session id.
func sessionProcs(id string) []Proc {
	out := []Proc{}
	if id == "" {
		return out
	}
	for _, p := range processes() {
		if len(p.args) > 3 && p.args[1] == "hold" && p.args[3] == "--" {
			continue // a herd session holder: its claude child is what counts
		}
		for i, arg := range p.args {
			prev := ""
			if i > 0 {
				prev = p.args[i-1]
			}
			if (arg == id && (prev == "--resume" || prev == "-r" || prev == "--session-id")) || arg == "--resume="+id || arg == "--session-id="+id {
				cmd := strings.Join(p.args, " ")
				if len(cmd) > 160 {
					cmd = cmd[:157] + "…"
				}
				out = append(out, Proc{PID: p.pid, Cmd: cmd})
				break
			}
		}
	}
	return out
}

type proc struct {
	pid  int
	args []string
}

// processes lists running processes with their arguments (Linux /proc, or ps).
func processes() []proc {
	self := os.Getpid()
	var out []proc
	if runtime.GOOS == "linux" {
		dirs, _ := filepath.Glob("/proc/[0-9]*")
		for _, d := range dirs {
			pid, _ := strconv.Atoi(filepath.Base(d))
			b, err := os.ReadFile(filepath.Join(d, "cmdline"))
			if err != nil || len(b) == 0 || pid == self {
				continue
			}
			out = append(out, proc{pid, strings.Split(strings.TrimRight(string(b), "\x00"), "\x00")})
		}
		return out
	}
	b, err := exec.Command("ps", "-axww", "-o", "pid=,command=").Output()
	if err != nil {
		return nil
	}
	for _, l := range strings.Split(string(b), "\n") {
		f := strings.Fields(l)
		if len(f) < 2 {
			continue
		}
		pid, _ := strconv.Atoi(f[0])
		if pid != self {
			out = append(out, proc{pid, f[1:]})
		}
	}
	return out
}
