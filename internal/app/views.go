package app

import (
	"fmt"
	"path/filepath"
	"strings"

	"herd/internal/settings"
	"herd/internal/store"
	"herd/internal/usage"
)

// Status is a task's derived status, shown on its card.
type Status struct {
	Kind  string `json:"kind"`  // running | waiting | approval | idle | done
	Label string `json:"label"` // "Running · Implement 2"
	Short string `json:"short"` // "running"
}

// StepView is a step as the frontend shows it.
type StepView struct {
	store.Step
	Name     string      `json:"name"`
	Live     bool        `json:"live"`
	Auto     bool        `json:"auto"`
	Artifact string      `json:"artifact"`
	Commits  bool        `json:"commits"`
	Paused   bool        `json:"paused"` // stopped by the user until Resume
	Manual   bool        `json:"manual"` // created without running: the user drives its session
	Fresh    bool        `json:"fresh"`  // its session has no transcript yet: claude --session-id, not --resume
	Usage    usage.Usage `json:"usage"`
}

// Option is one next-step button.
type Option struct {
	Agent string `json:"agent"`
	N     int    `json:"n"`
	Label string `json:"label"`
}

// Ghost is the pending step after the latest one, shown in the timeline:
// automatic, or (after a manual step) waiting for the user to start it.
type Ghost struct {
	Name   string  `json:"name"`
	After  string  `json:"after"`
	Manual bool    `json:"manual"`
	Start  *Option `json:"start"` // set when the user starts it
}

// Next is the next-step box for a task.
type Next struct {
	Options  []Option `json:"options"`
	Text     string   `json:"text"`
	Approval string   `json:"approval"` // step id waiting for permission
	MarkDone string   `json:"markDone"` // step id that can be marked done
	Ghost    *Ghost   `json:"ghost"`
}

// TaskView is a task with its steps and derived state.
type TaskView struct {
	store.Task
	ProjectName string      `json:"projectName"`
	Repo        string      `json:"repo"`
	BaseBranch  string      `json:"baseBranch"`
	Worktree    string      `json:"worktree"`
	WorktreeRel string      `json:"worktreeRel"`
	Branch      string      `json:"branch"`
	RunDir      string      `json:"runDir"`
	Source      string      `json:"source"`
	Status      Status      `json:"status"`
	Steps       []StepView  `json:"steps"`
	Next        Next        `json:"next"`
	Usage       usage.Usage `json:"usage"`   // every step's session
	PRTitle     *PRTitle    `json:"prTitle"` // chosen at the last Push
}

func (a *App) runDir(p settings.Project, t store.Task) string {
	return filepath.Join(a.Home, "runs", p.Name, t.Slug)
}

// worktreesRoot is the folder holding a project's task worktrees: inside the
// repo by default (.worktrees), or anywhere else when the setting is an
// absolute path, ~/…, or ../….
func worktreesRoot(p settings.Project) string {
	d := expandHome(p.WorktreesDir)
	if filepath.IsAbs(d) {
		return filepath.Clean(d)
	}
	return filepath.Join(p.Path, d)
}

func worktreePath(p settings.Project, t store.Task) string {
	return filepath.Join(worktreesRoot(p), t.Slug)
}

// excludePattern is the .git/info/exclude line for the worktrees folder, or
// "" when the folder is outside the repository.
func excludePattern(p settings.Project) string {
	rel, err := filepath.Rel(p.Path, worktreesRoot(p))
	if err != nil || rel == "." || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return ""
	}
	return filepath.ToSlash(rel) + "/"
}

// branchName is the task's git branch: the one chosen in New task, or
// agent/<slug> for tasks made before branches were named.
func branchName(t store.Task) string {
	if t.Branch != "" {
		return t.Branch
	}
	return "agent/" + t.Slug
}

func (a *App) projectOf(t store.Task) settings.Project {
	if p, ok := a.settings.Project(t.Project); ok {
		return p
	}
	return settings.Project{ID: t.Project, Name: t.Project, WorktreesDir: ".worktrees", BaseBranch: "main"}
}

func agentFor(agents []settings.Agent, id string) settings.Agent {
	for _, ag := range agents {
		if ag.ID == id {
			return ag
		}
	}
	return settings.Agent{ID: id, Name: strings.ToUpper(id[:1]) + id[1:], Writes: strings.ToUpper(id) + "-{n}.md"}
}

func (a *App) view(t store.Task) (TaskView, error) {
	steps, err := a.db.Steps(t.ID)
	if err != nil {
		return TaskView{}, err
	}
	agents := a.settings.Agents()
	p := a.projectOf(t)
	v := TaskView{
		Task: t, ProjectName: p.Name, Repo: p.Repo, BaseBranch: p.BaseBranch,
		Worktree: worktreePath(p, t), WorktreeRel: filepath.Join(p.WorktreesDir, t.Slug), Branch: branchName(t),
		RunDir: a.runDir(p, t), Source: sourceText(t, p),
	}
	for _, s := range steps {
		ag := agentFor(agents, s.Agent)
		_, live := a.run.Get(s.ID)
		v.Steps = append(v.Steps, StepView{
			Step: s, Name: ag.DisplayName(s.N), Live: live, Auto: ag.AfterAgent() != "",
			Artifact: ag.ArtifactName(s.N), Commits: ag.Commit, Paused: a.paused(s.ID), Manual: a.manual(s.ID),
			Usage: a.usage.Session(s.SessionID),
		})
		if sv := &v.Steps[len(v.Steps)-1]; sv.Manual && sv.State != store.Done {
			sv.Fresh = !sessionSaved(s.SessionID)
		}
		v.Usage = v.Usage.Add(v.Steps[len(v.Steps)-1].Usage)
	}
	if v.Steps == nil {
		v.Steps = []StepView{}
	}
	if pt, ok := a.prTitle(t.ID); ok {
		v.PRTitle = &pt
	}
	v.Status = statusOf(t, v.Steps)
	v.Next = nextOf(t, v.Steps, agents)
	return v, nil
}

func sourceText(t store.Task, p settings.Project) string {
	switch {
	case t.Issue != "" && t.Description != "":
		return fmt.Sprintf("GitHub issue #%s + description · %s", t.Issue, p.Repo)
	case t.Issue != "":
		return fmt.Sprintf("GitHub issue #%s · %s", t.Issue, p.Repo)
	default:
		return "LOCAL · description only, no GitHub issue"
	}
}

func statusOf(t store.Task, steps []StepView) Status {
	if t.Archived {
		day := t.ArchivedAt.Format("Jan 2")
		if t.PRNumber > 0 && t.PRState != "" && t.PRState != "open" {
			kind := "idle"
			if t.PRState == "merged" {
				kind = "done"
			}
			return Status{kind, fmt.Sprintf("PR #%d %s · %s", t.PRNumber, t.PRState, day), t.PRState}
		}
		return Status{"idle", "Archived · " + day, "archived"}
	}
	if len(steps) == 0 {
		return Status{"idle", "Not started", "not started"}
	}
	for _, s := range steps {
		if s.Paused && s.State != store.Done && s.State != store.Failed {
			if s.Manual {
				return Status{"waiting", "Manual · " + s.Name, "manual"}
			}
			return Status{"idle", "Paused · " + s.Name, "paused"}
		}
	}
	for _, s := range steps {
		if s.State == store.Approval {
			return Status{"approval", "Needs approval · " + s.Name, "approval"}
		}
	}
	for _, s := range steps {
		if s.State == store.Running {
			return Status{"running", "Running · " + s.Name, "running"}
		}
	}
	last := steps[len(steps)-1]
	switch last.State {
	case store.Idle:
		return Status{"waiting", "Waiting for input · " + last.Name, "waiting"}
	case store.Failed:
		return Status{"waiting", "Waiting for input · " + last.Name + " failed", "failed"}
	}
	if t.PRNumber > 0 && t.PRState == "open" {
		return Status{"done", fmt.Sprintf("PR #%d open", t.PRNumber), "PR open"}
	}
	return Status{"waiting", "Waiting for input · after " + last.Name, "waiting"}
}

func countAgent(steps []StepView, id string) int {
	n := 0
	for _, s := range steps {
		if s.Agent == id && s.N > n {
			n = s.N
		}
	}
	return n
}

func option(ag settings.Agent, n int, prefix string) Option {
	return Option{Agent: ag.ID, N: n, Label: prefix + ag.DisplayName(n)}
}

func nextOf(t store.Task, steps []StepView, agents []settings.Agent) Next {
	nx := Next{Options: []Option{}}
	if t.Archived {
		day := t.ArchivedAt.Format("Jan 2")
		if t.PRNumber > 0 && t.PRState != "open" && t.PRState != "" {
			nx.Text = fmt.Sprintf("Archived %s when PR #%d was %s.", day, t.PRNumber, t.PRState)
		} else {
			nx.Text = "Archived " + day + "."
		}
		if t.WorktreeRemoved {
			nx.Text += " The worktree and branch were removed."
		} else {
			nx.Text += " The worktree is kept until you remove it."
		}
		return nx
	}
	if len(agents) == 0 {
		nx.Text = "No agents are defined. Add one in Settings > Agents."
		return nx
	}
	if len(steps) == 0 {
		nx.Options = append(nx.Options, option(agents[0], 1, ""))
		return nx
	}
	last := steps[len(steps)-1]
	ag := agentFor(agents, last.Agent)
	idx := -1
	for i, x := range agents {
		if x.ID == last.Agent {
			idx = i
		}
	}
	var autoNext *settings.Agent
	for i := range agents {
		if agents[i].AfterAgent() == last.Agent {
			autoNext = &agents[i]
			break
		}
	}
	if autoNext != nil && last.State != store.Done {
		nx.Ghost = &Ghost{Name: autoNext.DisplayName(countAgent(steps, autoNext.ID) + 1), After: last.Name}
	}
	// After a manual step the next agent waits for the user, whatever its trigger.
	if autoNext != nil && last.Manual && last.State != store.Failed {
		o := option(*autoNext, countAgent(steps, autoNext.ID)+1, "")
		nx.Ghost = &Ghost{Name: o.Label, After: last.Name, Manual: true, Start: &o}
		if last.State != store.Running && last.State != store.Approval {
			nx.Options = append(nx.Options, o)
			nx.Text = last.Name + " is yours: use its session outside herd (copy the claude command in its toolbar) or Resume it here. Start " + o.Label + " when you are done."
			return nx
		}
	}

	for _, s := range steps {
		if s.State == store.Approval {
			nx.Approval = s.ID
			nx.Text = "Claude Code wants to run a command. Answer in the terminal, or allow it once here."
			return nx
		}
	}
	for _, s := range steps {
		if s.State == store.Running {
			nx.Text = s.Name + " is running. "
			if autoNext != nil && s.ID == last.ID && !last.Manual {
				nx.Text += autoNext.DisplayName(countAgent(steps, autoNext.ID)+1) + " starts automatically when it finishes."
			} else {
				nx.Text += "You will be asked for the next step when it finishes."
			}
			return nx
		}
	}
	switch last.State {
	case store.Idle:
		nx.Text = "Claude is waiting for you in the " + last.Name + " terminal. Mark it done if " + last.Artifact + " is complete."
		nx.MarkDone = last.ID
		return nx
	case store.Failed:
		nx.Text = last.Name + " ended before " + last.Artifact + " was done. Retry it, or open the step to resume the conversation."
		nx.Options = append(nx.Options, option(ag, last.N, "Retry "))
		nx.MarkDone = last.ID
		return nx
	}
	// last step is done
	if ag.Push {
		if t.PRNumber > 0 {
			nx.Text = fmt.Sprintf("PR #%d is open. The task moves to Archive when the PR is merged or closed.", t.PRNumber)
			return nx
		}
		nx.Text = "The push did not open a pull request. Check the Push tab, then run Push again."
		nx.Options = append(nx.Options, option(ag, last.N+1, ""))
		return nx
	}
	if autoNext != nil {
		nx.Text = autoNext.DisplayName(countAgent(steps, autoNext.ID)+1) + " is starting."
		nx.Ghost = &Ghost{Name: autoNext.DisplayName(countAgent(steps, autoNext.ID) + 1), After: last.Name}
		return nx
	}
	var next *settings.Agent
	for i := idx + 1; idx >= 0 && i < len(agents); i++ {
		if agents[i].AfterAgent() == "" {
			next = &agents[i]
			break
		}
	}
	if idx < 0 {
		next = &agents[0]
	}
	var loop *settings.Agent
	if last.Verdict != "" {
		back := ag.AfterAgent()
		if back == "" && idx > 0 {
			back = agents[idx-1].ID
		}
		for i := range agents {
			if agents[i].ID == back {
				loop = &agents[i]
			}
		}
	}
	var opts []Option
	if next != nil {
		opts = append(opts, option(*next, countAgent(steps, next.ID)+1, ""))
	}
	if loop != nil {
		lo := option(*loop, countAgent(steps, loop.ID)+1, "")
		if last.Verdict == "approve" {
			opts = append(opts, lo)
		} else {
			opts = append([]Option{lo}, opts...)
		}
	}
	nx.Options = append(nx.Options, opts...)
	if len(opts) == 0 {
		nx.Text = "The pipeline is finished for this task."
	}
	return nx
}
