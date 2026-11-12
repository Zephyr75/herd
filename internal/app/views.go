package app

import (
	"fmt"
	"path/filepath"
	"regexp"
	"strings"

	"herd/internal/github"
	"herd/internal/settings"
	"herd/internal/store"
)

// Status is a task's derived status, shown on its card.
type Status struct {
	Kind  string `json:"kind"`  // running | waiting | approval | idle | done | failed
	Label string `json:"label"` // "Running · Implement 2"
	Short string `json:"short"` // "running"
}

// StepView is a step as the frontend shows it.
type StepView struct {
	store.Step
	Name     string `json:"name"`
	Live     bool   `json:"live"`
	Auto     bool   `json:"auto"`
	Artifact string `json:"artifact"`
	Commits  bool   `json:"commits"`
	Diff     bool   `json:"diff"`     // Diff has something to open: its segment's commit, or the uncommitted work
	Revert   bool   `json:"revert"`   // after the latest commit: Revert puts the worktree back to its start and removes it and later steps
	Paused   bool   `json:"paused"`   // stopped by the user until Resume
	Rerun    bool   `json:"rerun"`    // Rerun applies (see markRerun)
	Skip     bool   `json:"skip"`     // the check and cross buttons apply: the latest step, or one not finished
	External bool   `json:"external"` // run by herd, not Claude: the Copilot review step
	GitError string `json:"gitError"` // why herd's commit, push or PR after it failed
	Busy     bool   `json:"-"`        // herd is running what follows its success: git actions, the automatic next step
}

// Option is one next-step button.
type Option struct {
	Agent string `json:"agent"`
	N     int    `json:"n"`
	Label string `json:"label"`
}

// Ghost is the automatic step about to start after the latest one, shown in
// the timeline.
type Ghost struct {
	Name  string `json:"name"`
	After string `json:"after"`
}

// Next is the next-step box for a task.
type Next struct {
	Options  []Option `json:"options"`
	Text     string   `json:"text"`
	Approval string   `json:"approval"` // step id waiting for permission
	MarkDone string   `json:"markDone"` // step id that can be marked done
	Warning  string   `json:"warning"`  // e.g. the plan lists options: pick one in the guidance
	// Elsewhere: before a step that commits, the repositories outside the
	// worktree where agents left uncommitted edits (only the worktree is committed).
	Elsewhere []string `json:"elsewhere"`
	// Sisters: repositories the plan also needs, with no sister task yet.
	Sisters []SisterOffer `json:"sisters"`
	Ghost   *Ghost        `json:"ghost"`
}

// TaskView is a task with its steps and derived state.
type TaskView struct {
	store.Task
	ProjectName string               `json:"projectName"`
	Repo        string               `json:"repo"`
	BaseBranch  string               `json:"baseBranch"`
	Worktree    string               `json:"worktree"`
	WorktreeRel string               `json:"worktreeRel"`
	Branch      string               `json:"branch"`
	RunDir      string               `json:"runDir"`
	Source      string               `json:"source"`
	Status      Status               `json:"status"`
	Steps       []StepView           `json:"steps"`
	Next        Next                 `json:"next"`
	ProjectGone bool                 `json:"projectGone"` // its project was removed from Settings > Projects
	Checks      []github.Check       `json:"checks"`      // CI checks of the open PR, refreshed every minute
	Labels      []string             `json:"labels"`      // labels of the open PR
	Sisters     []SisterRef          `json:"sisters"`     // tasks for the other repositories of the same work
	Draft       bool                 `json:"draft"`       // the open PR is a draft (herd opens them so)
	Conflicting bool                 `json:"conflicting"` // the open PR conflicts with its base: GitHub runs no checks
	Feedback    []github.Feedback    `json:"feedback"`    // review threads (resolved ones too) and reviews of the open PR
	Rejected    map[string]Rejection `json:"rejected"`    // open threads a step found wrong or unnecessary, by thread id
	Behind      int                  `json:"behind"`      // commits of origin/<base> the branch lacks
	Notes       []Note               `json:"notes"`       // the user's comments on the diff, until sent to a step
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
			Artifact: ag.ArtifactName(s.N), Commits: ag.Commit, Paused: a.paused(s.ID),
			External: s.Agent == copilotAgent,
		})
		sv := &v.Steps[len(v.Steps)-1]
		sv.GitError, _ = a.db.Get(gitErrKey(s.ID))
		_, sv.Busy = a.busy.Load(s.ID)
	}
	for i := range v.Steps {
		sv := &v.Steps[i]
		sv.Skip = !t.Archived && !sv.Busy && (i == len(v.Steps)-1 || sv.State != store.Done)
	}
	if v.Steps == nil {
		v.Steps = []StepView{}
	}
	// Steps share their segment's commit, or the uncommitted work after the last commit.
	commits := a.branchCommits(t)
	for i := range v.Steps {
		v.Steps[i].Revert = a.revertable(t, v.Steps, i, commits)
	}
	dirty := -1 // unknown until a step needs it
	for i := range v.Steps {
		sv := &v.Steps[i]
		sv.Diff = segmentCommit(commits, steps, i) != nil
		if !sv.Diff && !t.WorktreeRemoved {
			if dirty < 0 {
				// No optional locks: a background status must not hold the index
				// lock while a step commits (git add -A would fail).
				out, err := a.git.Run(a.ctx, v.Worktree, "--no-optional-locks", "status", "--porcelain")
				dirty = 0
				if err == nil && strings.TrimSpace(out) != "" {
					dirty = 1
				}
			}
			sv.Diff = dirty == 1
		}
	}
	_, known := a.settings.Project(t.Project)
	v.ProjectGone = !known
	a.markRerun(t, v.Steps, agents)
	v.Labels = a.labelsOf(t.ID)
	if v.Labels == nil {
		v.Labels = []string{}
	}
	v.Draft = a.draftOf(t.ID)
	v.Conflicting = a.conflictOf(t.ID)
	v.Feedback = a.feedbackOf(t.ID)
	v.Rejected = a.rejections(v.Feedback)
	v.Behind = a.behindOf(t.ID)
	v.Notes = a.notesOf(t.ID)
	v.Checks = a.checksOf(t.ID)
	if v.Checks == nil {
		v.Checks = []github.Check{}
	}
	v.Status = statusOf(t, v.Steps, a.prReady(t.ID))
	v.Next = nextOf(t, v.Steps, agents)
	v.Next.Warning = a.optionsWarning(t, v.Steps, v.Next)
	v.Next.Sisters = []SisterOffer{}
	if len(agents) > 0 {
		v.Next.Sisters = a.sisterOffers(t, v.Steps, agents)
	}
	v.Sisters = []SisterRef{}
	for _, x := range a.sisters(t.ID) {
		v.Sisters = append(v.Sisters, SisterRef{ID: x.ID, Project: a.projectOf(x).Name})
	}
	v.Next.Elsewhere = []string{}
	for _, o := range v.Next.Options {
		if agentFor(agents, o.Agent).Commit {
			v.Next.Elsewhere = a.outsideRepos(t)
			break
		}
	}
	return v, nil
}

var optionsHeading = regexp.MustCompile(`(?mi)^#{1,6}\s*options\b`)

// optionsWarning asks the user to choose when the latest step's agent asks
// for a choice (Settings > Agents) and its artifact lists Options.
func (a *App) optionsWarning(t store.Task, steps []StepView, nx Next) string {
	if len(steps) == 0 || len(nx.Options) == 0 || t.Archived {
		return ""
	}
	last := steps[len(steps)-1]
	if !last.Succeeded() || !agentFor(a.settings.Agents(), last.Agent).Choose {
		return ""
	}
	art, _ := a.artifactFor(last.Step)
	if !art.Exists || !optionsHeading.MatchString(art.Body) {
		return ""
	}
	return fmt.Sprintf("%s lists several options. Read them, then name the one to take in the guidance before starting %s.", last.Artifact, nx.Options[0].Label)
}

// markRerun sets Rerun on the task's latest step when its agent can be rerun
// (Settings > Agents) and every step finished: it runs again in its own
// conversation on the current worktree, e.g. a Review after hand edits.
func (a *App) markRerun(t store.Task, steps []StepView, agents []settings.Agent) {
	if t.Archived || t.WorktreeRemoved || len(steps) == 0 {
		return
	}
	for _, s := range steps {
		if s.State != store.Done && s.State != store.Failed {
			return
		}
	}
	if last := &steps[len(steps)-1]; agentFor(agents, last.Agent).Rerun && !last.External {
		last.Rerun = true
	}
}

// ciSummary is the PR's CI state in a few words, "" before any check.
func ciSummary(cs []github.Check) string {
	if len(cs) == 0 {
		return ""
	}
	pending := false
	for _, c := range cs {
		switch c.Bucket {
		case "fail", "cancel":
			return "CI failed"
		case "pending":
			pending = true
		}
	}
	if pending {
		return "CI running"
	}
	return "CI passed"
}

func sourceText(t store.Task, p settings.Project) string {
	repo := p.Repo
	if t.IssueRepo != "" {
		repo = t.IssueRepo
	}
	switch {
	case t.Issue != "" && t.Description != "":
		return fmt.Sprintf("GitHub issue #%s + description · %s", t.Issue, repo)
	case t.Issue != "":
		return fmt.Sprintf("GitHub issue #%s · %s", t.Issue, repo)
	default:
		return "LOCAL · description only, no GitHub issue"
	}
}

// statusOf is the task's badge: its current (or last) step with Running,
// Waiting, Failure or, only for a Copilot step that passed (the task is
// done), Success; and the open PR, with Ready once it is ready for review.
// Steps that succeed after that Copilot step without changing code (a
// Discuss to understand something) keep the Success. Any other success
// waits for the next step.
func statusOf(t store.Task, steps []StepView, ready bool) Status {
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
	pr := ""
	if t.PRNumber > 0 && t.PRState == "open" {
		pr = fmt.Sprintf(" · PR #%d", t.PRNumber)
		if ready {
			pr += " Ready"
		}
	}
	st := func(kind, word string, s StepView) Status {
		return Status{kind, s.Name + " · " + word + pr, strings.ToLower(word)}
	}
	for _, s := range steps {
		switch {
		case s.Paused && s.State != store.Done && s.State != store.Failed, s.State == store.Approval:
			kind := "waiting"
			if s.State == store.Approval {
				kind = "approval"
			}
			return st(kind, "Waiting", s)
		case s.State == store.Running:
			return st("running", "Running", s)
		}
	}
	last := steps[len(steps)-1]
	switch {
	case last.State == store.Idle:
		return st("waiting", "Waiting", last)
	case last.State == store.Failed || last.Unsuccessful():
		return st("failed", "Failure", last)
	}
	if last.External || untouchedSinceCopilot(steps) {
		return st("done", "Success", last)
	}
	return st("waiting", "Waiting for next step", last)
}

// untouchedSinceCopilot reports whether a Copilot step passed and every step
// since succeeded without changing code.
func untouchedSinceCopilot(steps []StepView) bool {
	for i := len(steps) - 1; i >= 0; i-- {
		s := steps[i]
		if s.External {
			return s.Succeeded()
		}
		if !s.Succeeded() || s.Diff {
			return false
		}
	}
	return false
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

// successIDs are the agents Next step offers after ag succeeds: its list.
func successIDs(ag settings.Agent) []string { return ag.Next }

// failureIDs are the agents Next step offers after ag fails: its list, else
// ag again.
func failureIDs(ag settings.Agent) []string {
	if len(ag.OnFail) > 0 {
		return ag.OnFail
	}
	return []string{ag.ID}
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
			nx.Text += " The worktree is kept until you delete the task."
		}
		return nx
	}
	start, ok := settings.First(agents)
	if !ok {
		nx.Text = "No agents are defined. Add one in Settings > Agents."
		return nx
	}
	if len(steps) == 0 {
		nx.Options = append(nx.Options, option(start, 1, ""))
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
	// The agent that starts by itself after the last step, when that step
	// succeeds (or still may: it has not finished).
	var autoNext *settings.Agent
	for i := range agents {
		if agents[i].AfterAgent() == last.Agent && !last.Unsuccessful() {
			autoNext = &agents[i]
			break
		}
	}
	if autoNext != nil && last.State != store.Done {
		nx.Ghost = &Ghost{Name: autoNext.DisplayName(countAgent(steps, autoNext.ID) + 1), After: last.Name}
	}

	for _, s := range steps {
		if s.State == store.Approval {
			nx.Approval = s.ID
			nx.Text = "Claude Code wants to run a command. Answer in the terminal, or allow it once here."
			return nx
		}
	}
	// What Next step offers comes from the agents' lists: after a success,
	// after a failure (Copilot's own lists too).
	offer := func(ids []string) []Option {
		var out []Option
		for _, id := range ids {
			for _, x := range agents {
				if x.ID == id {
					out = append(out, option(x, countAgent(steps, x.ID)+1, ""))
				}
			}
		}
		return out
	}
	cop := agentFor(agents, copilotAgent)
	for _, s := range steps {
		if s.State == store.Running && s.External {
			nx.Text = fmt.Sprintf("Waiting for Copilot's review and the CI checks of PR #%d (checked every minute). Start a fix any time, or Mark done to stop waiting.", t.PRNumber)
			nx.MarkDone = s.ID
			nx.Options = append(nx.Options, offer(failureIDs(cop))...)
			return nx
		}
		if s.State == store.Running {
			nx.Text = s.Name + " is running. "
			if autoNext != nil && s.ID == last.ID {
				nx.Text += autoNext.DisplayName(countAgent(steps, autoNext.ID)+1) + " starts automatically if it succeeds."
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
	// The last step finished.
	if last.External {
		ids := successIDs(cop)
		switch {
		case last.Unsuccessful():
			nx.Text = fmt.Sprintf("PR #%d needs fixes: the PR tab lists Copilot's comments and the failing CI checks.", t.PRNumber)
			ids = failureIDs(cop)
		case last.Verdict == skipped:
			nx.Text = fmt.Sprintf("herd stopped waiting for Copilot on PR #%d.", t.PRNumber)
		default:
			nx.Text = fmt.Sprintf("Copilot found nothing to fix and CI passed on PR #%d. The task moves to Archive when the PR is merged or closed.", t.PRNumber)
		}
		nx.Options = append(nx.Options, offer(ids)...)
		return nx
	}
	if last.Unsuccessful() {
		nx.Text = last.Name + " failed: " + last.Artifact + " says why. Mark it done to go on anyway."
		nx.Options = append(nx.Options, offer(failureIDs(ag))...)
		nx.MarkDone = last.ID
		return nx
	}
	if last.Busy {
		if ag.Commit {
			nx.Text = "herd is " + gitWords(ag) + "."
		} else if autoNext != nil {
			nx.Text = autoNext.DisplayName(countAgent(steps, autoNext.ID)+1) + " is starting."
		}
		if autoNext != nil {
			nx.Ghost = &Ghost{Name: autoNext.DisplayName(countAgent(steps, autoNext.ID) + 1), After: last.Name}
		}
		return nx
	}
	if last.GitError != "" {
		nx.Text = last.GitError + ". The " + last.Name + " terminal says more; run it again once it is fixed."
		retry := option(ag, countAgent(steps, ag.ID)+1, "")
		nx.Options = append(nx.Options, retry)
		for _, o := range offer(failureIDs(ag)) {
			if o != retry {
				nx.Options = append(nx.Options, o)
			}
		}
		return nx
	}
	if autoNext != nil {
		name := autoNext.DisplayName(countAgent(steps, autoNext.ID) + 1)
		if !last.Completed {
			nx.Text = name + " is starting."
			nx.Ghost = &Ghost{Name: name, After: last.Name}
			return nx
		}
		// It should have started when the step finished.
		nx.Text = name + " did not start after " + last.Name + ". Run " + last.Name + " again, or pick the next step."
		nx.Options = append(nx.Options, option(ag, last.N+1, ""))
		nx.Options = append(nx.Options, offer(successIDs(ag))...)
		return nx
	}
	if idx < 0 { // its agent was removed
		nx.Options = append(nx.Options, option(start, countAgent(steps, start.ID)+1, ""))
		return nx
	}
	nx.Options = append(nx.Options, offer(successIDs(ag))...)
	if len(nx.Options) == 0 {
		nx.Text = "The pipeline is finished for this task."
	}
	return nx
}

// gitWords says what herd does after an agent's step: "committing",
// "committing and pushing" or "committing, pushing and opening the PR".
func gitWords(ag settings.Agent) string {
	switch {
	case ag.OpenPR:
		return "committing, pushing and opening the PR"
	case ag.Push:
		return "committing and pushing"
	}
	return "committing"
}
