package app

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"slices"
	"strconv"
	"strings"
	"time"

	"herd/internal/settings"
	"herd/internal/store"
)

// FileChange is one file a step changed.
type FileChange struct {
	Path    string `json:"path"`
	Status  string `json:"status"` // A added, M modified, D deleted, ? untracked
	Added   int    `json:"added"`
	Deleted int    `json:"deleted"`
	Binary  bool   `json:"binary"`
}

// Changes is what a step changed. Steps between two commits of the branch
// share the later one (Implement, Review and Test share their Commit and
// push); steps after the last commit share the uncommitted work. The
// Copilot step shows the whole branch against its base, as the PR does.
type Changes struct {
	Source string       `json:"source"` // commit | worktree | branch | "" (nothing to show)
	SHA    string       `json:"sha"`
	Base   string       `json:"base"` // branch: the base it is compared to, e.g. origin/dev
	Via    string       `json:"via"`  // the step that made the commit, when not this one
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
	if s.Agent == copilotAgent {
		return a.branchChanges(p, t)
	}
	steps, _ := a.db.Steps(t.ID)
	sha := ""
	i := slices.IndexFunc(steps, func(x store.Step) bool { return x.ID == s.ID })
	if c := segmentCommit(a.branchCommits(t), steps, i); i >= 0 && c != nil {
		sha = c.SHA
		for _, y := range steps { // the step that made it, when herd did
			if madeBy([]store.Step{y}, c.SHA) {
				out.Via = agentFor(a.settings.Agents(), y.Agent).DisplayName(y.N)
			}
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
	// No commit after this step yet: it shares the uncommitted work with
	// every step since the last commit.
	wt := worktreePath(p, t)
	if t.WorktreeRemoved {
		return out, nil
	}
	if _, err := os.Stat(wt); err != nil {
		return out, nil
	}
	out.Source = "worktree"
	files, err := a.uncommitted(wt)
	if err != nil {
		return out, err
	}
	out.Files = files
	return out, nil
}

// branchChanges lists what the task's branch changes against its base since
// they parted (base...HEAD): what its PR shows.
func (a *App) branchChanges(p settings.Project, t store.Task) (Changes, error) {
	out := Changes{Files: []FileChange{}}
	wt := worktreePath(p, t)
	if _, err := os.Stat(wt); t.WorktreeRemoved || err != nil {
		return out, nil
	}
	out.Source, out.Base = "branch", a.baseRef(wt, p.BaseBranch)
	num, err := a.git.Run(a.ctx, wt, "-c", "core.quotePath=false", "diff", "--no-renames", "--numstat", out.Base+"...HEAD")
	if err != nil {
		return out, err
	}
	names, err := a.git.Run(a.ctx, wt, "-c", "core.quotePath=false", "diff", "--no-renames", "--name-status", out.Base+"...HEAD")
	if err != nil {
		return out, err
	}
	out.Files = parseChanges(num, names)
	return out, nil
}

// baseRef is origin/<base> when the repository has it, else <base>.
func (a *App) baseRef(dir, base string) string {
	if _, err := a.git.Run(a.ctx, dir, "rev-parse", "--verify", "--quiet", "origin/"+base); err == nil {
		return "origin/" + base
	}
	return base
}

// uncommitted lists the uncommitted changes in the repository at dir,
// limited to paths when given.
func (a *App) uncommitted(dir string, paths ...string) ([]FileChange, error) {
	git := func(args ...string) (string, error) {
		args = append([]string{"--no-optional-locks", "--literal-pathspecs", "-c", "core.quotePath=false"}, args...)
		if len(paths) > 0 {
			args = append(append(args, "--"), paths...)
		}
		return a.git.Run(a.ctx, dir, args...)
	}
	num, err := git("diff", "--no-renames", "--numstat", "HEAD")
	if err != nil {
		return nil, err
	}
	names, err := git("diff", "--no-renames", "--name-status", "HEAD")
	if err != nil {
		return nil, err
	}
	files := parseChanges(num, names)
	untracked, err := git("ls-files", "--others", "--exclude-standard")
	if err != nil {
		return nil, err
	}
	for _, rel := range strings.Split(untracked, "\n") {
		if rel == "" {
			continue
		}
		fc := FileChange{Path: rel, Status: "?"}
		if b, err := os.ReadFile(filepath.Join(dir, rel)); err == nil && len(b) < 4<<20 {
			if bytes.IndexByte(b, 0) >= 0 {
				fc.Binary = true
			} else {
				fc.Added = bytes.Count(b, []byte("\n"))
				if len(b) > 0 && b[len(b)-1] != '\n' {
					fc.Added++
				}
			}
		}
		files = append(files, fc)
	}
	return files, nil
}

// commit is one commit of a task's branch.
type commit struct {
	SHA string
	At  time.Time
}

// branchCommits lists the task branch's own commits, oldest first: the
// boundaries between the change sets its steps share.
func (a *App) branchCommits(t store.Task) []commit {
	p := a.projectOf(t)
	base := p.BaseBranch
	if _, err := a.git.Run(a.ctx, p.Path, "rev-parse", "--verify", "-q", "origin/"+base); err == nil {
		base = "origin/" + base // the local base branch may lag behind
	}
	out, err := a.git.Run(a.ctx, p.Path, "log", "--reverse", "--format=%h %ct", base+".."+branchName(t))
	if err != nil {
		return nil
	}
	var cs []commit
	for _, l := range strings.Split(out, "\n") {
		sha, ts, ok := strings.Cut(strings.TrimSpace(l), " ")
		if n, err := strconv.ParseInt(ts, 10, 64); ok && err == nil {
			cs = append(cs, commit{sha, time.Unix(n, 0)})
		}
	}
	return cs
}

// segmentCommit is the commit holding step s's changes: the first one made
// after it started. Steps between two commits share the later one (Implement,
// Review, Test and their Commit and push); after the last commit there is
// none yet and the step shares the uncommitted work.
func segmentCommit(cs []commit, steps []store.Step, i int) *commit {
	start := steps[i].StartedAt.Truncate(time.Second) // commit times have seconds only
	for k := range cs {
		c := &cs[k]
		if c.At.After(start) || c.At.Equal(start) && madeBy(steps[i:], c.SHA) {
			return c // within the same second: only when this step or a later one made it
		}
	}
	return nil
}

// madeBy reports whether one of steps recorded commit sha (short or full).
func madeBy(steps []store.Step, sha string) bool {
	for _, s := range steps {
		if s.SHA != "" && (strings.HasPrefix(sha, s.SHA) || strings.HasPrefix(s.SHA, sha)) {
			return true
		}
	}
	return false
}

// FileDiff is one file's unified diff in a step's changes.
type FileDiff struct {
	Path      string `json:"path"`
	Diff      string `json:"diff"` // git's unified diff, headers included
	Binary    bool   `json:"binary"`
	Truncated bool   `json:"truncated"`
}

const maxDiff = 400 << 10

// FileDiff returns the diff of one file the step changed, from the same
// source as StepChanges: the step's commit (or its Review's), or the
// uncommitted worktree.
func (a *App) FileDiff(stepID, rel string) (FileDiff, error) {
	ch, err := a.StepChanges(stepID)
	if err != nil {
		return FileDiff{}, err
	}
	var fc *FileChange
	for i := range ch.Files {
		if ch.Files[i].Path == rel {
			fc = &ch.Files[i]
		}
	}
	if fc == nil {
		return FileDiff{}, fmt.Errorf("%s is not among this step's changes", rel)
	}
	out := FileDiff{Path: rel, Binary: fc.Binary}
	if fc.Binary {
		return out, nil
	}
	s, _ := a.db.Step(stepID)
	t, _ := a.db.Task(s.TaskID)
	p := a.projectOf(t)
	switch {
	case ch.Source == "branch":
		out.Diff, err = a.git.Run(a.ctx, worktreePath(p, t), "-c", "core.quotePath=false", "diff", "--no-renames", "--no-color", ch.Base+"...HEAD", "--", rel)
	case ch.Source == "commit":
		out.Diff, err = a.git.Run(a.ctx, p.Path, "-c", "core.quotePath=false", "show", "--format=", "--no-renames", "--no-color", ch.SHA, "--", rel)
	case fc.Status == "?":
		out.Diff, err = untrackedDiff(worktreePath(p, t), rel)
	default:
		out.Diff, err = a.git.Run(a.ctx, worktreePath(p, t), "-c", "core.quotePath=false", "diff", "--no-renames", "--no-color", "HEAD", "--", rel)
	}
	if len(out.Diff) > maxDiff {
		cut := strings.LastIndexByte(out.Diff[:maxDiff], '\n')
		out.Diff, out.Truncated = out.Diff[:max(cut, 0)], true
	}
	return out, err
}

// untrackedDiff shows a new, untracked file as all added lines.
func untrackedDiff(wt, rel string) (string, error) {
	b, err := os.ReadFile(filepath.Join(wt, rel))
	if err != nil {
		return "", err
	}
	body := strings.TrimSuffix(string(b), "\n")
	lines := strings.Split(body, "\n")
	if body == "" {
		lines = nil
	}
	var d strings.Builder
	fmt.Fprintf(&d, "--- /dev/null\n+++ b/%s\n@@ -0,0 +1,%d @@\n", rel, len(lines))
	for _, l := range lines {
		d.WriteString("+" + l + "\n")
	}
	return d.String(), nil
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
