package app

import (
	"errors"
	"fmt"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"slices"
	"strings"
	"time"

	"herd/internal/artifacts"
	"herd/internal/github"
	"herd/internal/gitops"
	"herd/internal/settings"
	"herd/internal/store"
	"herd/internal/zed"
)

// ListTasks returns every task with its steps, active and archived.
func (a *App) ListTasks() ([]TaskView, error) {
	tasks, err := a.db.Tasks()
	if err != nil {
		return nil, err
	}
	out := make([]TaskView, 0, len(tasks))
	for _, t := range tasks {
		v, err := a.view(t)
		if err != nil {
			return nil, err
		}
		out = append(out, v)
	}
	return out, nil
}

// GetTask returns one task.
func (a *App) GetTask(id string) (TaskView, error) {
	t, err := a.db.Task(id)
	if err != nil {
		return TaskView{}, err
	}
	return a.view(t)
}

func (a *App) emitTask(id string) {
	v, err := a.GetTask(id)
	if err != nil {
		return
	}
	a.emit(EvTask, v)
}

func (a *App) emitAllTasks() {
	tasks, err := a.ListTasks()
	if err != nil {
		return
	}
	for _, v := range tasks {
		a.emit(EvTask, v)
	}
}

// IssueTitle fetches an issue's title for the New task dialog.
func (a *App) IssueTitle(projectID, issue string) (string, error) {
	p, ok := a.settings.Project(projectID)
	if !ok {
		return "", fmt.Errorf("unknown project %q", projectID)
	}
	if p.Repo == "" {
		return "", errors.New("the project has no GitHub repo")
	}
	return a.gh.IssueTitle(a.ctx, p.Repo, issue)
}

// MyIssues lists the project's open issues assigned to you (the gh user).
func (a *App) MyIssues(projectID string) ([]github.Issue, error) {
	p, ok := a.settings.Project(projectID)
	if !ok {
		return nil, fmt.Errorf("unknown project %q", projectID)
	}
	if p.Repo == "" {
		return nil, errors.New("the project has no GitHub repo")
	}
	return a.gh.MyIssues(a.ctx, p.Repo)
}

var nonWord = regexp.MustCompile(`[^a-z0-9 ]+`)

// LocalSlug is local-<first three words of the description>.
func LocalSlug(desc string) string {
	words := strings.Fields(nonWord.ReplaceAllString(strings.ToLower(desc), " "))
	if len(words) > 3 {
		words = words[:3]
	}
	if len(words) == 0 {
		return "local-task"
	}
	return "local-" + strings.Join(words, "-")
}

// checkBranch validates a new task's branch name.
func (a *App) checkBranch(p settings.Project, tasks []store.Task, prefix, branch string) error {
	if !strings.HasPrefix(branch, prefix) || len(branch) == len(prefix) {
		return fmt.Errorf("the branch must start with %q and name the task, e.g. %sfix-unlock-retry", prefix, prefix)
	}
	if _, err := a.git.Run(a.ctx, p.Path, "check-ref-format", "--branch", branch); err != nil {
		return fmt.Errorf("%q is not a valid branch name", branch)
	}
	for _, t := range tasks {
		if t.Project == p.ID && branchName(t) == branch {
			return fmt.Errorf("task %q already uses the branch %s", t.Title, branch)
		}
	}
	if _, err := a.git.Run(a.ctx, p.Path, "show-ref", "--verify", "--quiet", "refs/heads/"+branch); err == nil {
		return fmt.Errorf("the branch %s already exists in %s", branch, p.Name)
	}
	return nil
}

func titleFromDescription(desc string) string {
	line := strings.TrimSpace(strings.SplitN(strings.TrimSpace(desc), "\n", 2)[0])
	line = strings.TrimRight(line, ".")
	if r := []rune(line); len(r) > 80 {
		line = string(r[:77]) + "…"
	}
	return line
}

var digits = regexp.MustCompile(`^[0-9]+$`)

// CreateTask creates a task, its branch and worktree, and optionally starts
// the first agent. branch must start with the prefix in Settings; "" picks
// prefix + the task's slug.
// files are copied to the run dir's attachments/, which TASK.md lists.
func (a *App) CreateTask(projectID, issue, description, branch string, runPlan bool, files []string) (TaskView, error) {
	return a.createTask(projectID, issue, "", description, branch, runPlan, files)
}

// createTask is CreateTask; issueRepo is the repository holding the issue
// when it is not the project's (a sister task), else "".
func (a *App) createTask(projectID, issue, issueRepo, description, branch string, runPlan bool, files []string) (TaskView, error) {
	issue = strings.TrimPrefix(strings.TrimSpace(issue), "#")
	description = strings.TrimSpace(description)
	branch = strings.TrimSpace(branch)
	if issue == "" && description == "" {
		return TaskView{}, errors.New("add a GitHub issue number, a description, or both")
	}
	if issue != "" && !digits.MatchString(issue) {
		return TaskView{}, errors.New("the issue must be a number")
	}
	p, ok := a.settings.Project(projectID)
	if !ok {
		return TaskView{}, fmt.Errorf("unknown project %q", projectID)
	}
	if issueRepo == p.Repo {
		issueRepo = ""
	}
	repo := p.Repo
	if issueRepo != "" {
		repo = issueRepo
	}
	if issue != "" && repo == "" {
		return TaskView{}, fmt.Errorf("%s has no GitHub repo: leave the issue empty for a LOCAL task", p.Name)
	}
	for _, f := range files {
		if st, err := os.Stat(f); err != nil || !st.Mode().IsRegular() {
			return TaskView{}, fmt.Errorf("cannot attach %s: not a readable file", f)
		}
	}

	all, err := a.db.Tasks()
	if err != nil {
		return TaskView{}, err
	}
	taken := map[string]bool{}
	for _, t := range all {
		if t.Project == p.ID {
			taken[t.Slug] = true
		}
	}
	slug := issue
	if issue == "" {
		base := LocalSlug(description)
		slug = base
		for i := 2; taken[slug]; i++ {
			slug = fmt.Sprintf("%s-%d", base, i)
		}
	} else if taken[slug] {
		return TaskView{}, fmt.Errorf("a task for #%s already exists in %s", issue, p.Name)
	}

	title := ""
	if issue != "" {
		if t, err := a.gh.IssueTitle(a.ctx, repo, issue); err == nil && t != "" {
			title = t
		} else {
			title = "Issue #" + issue
		}
	} else {
		title = titleFromDescription(description)
	}

	prefix := a.settings.Config().BranchPrefix
	if branch == "" {
		branch = prefix + slug
	}
	if err := a.checkBranch(p, all, prefix, branch); err != nil {
		return TaskView{}, err
	}

	t := store.Task{
		ID: p.ID + ":" + slug, Project: p.ID, Slug: slug, Issue: issue, IssueRepo: issueRepo, Title: title, Description: description,
		Branch: branch, CreatedAt: time.Now(),
	}
	if pat := excludePattern(p); pat != "" {
		if _, err := a.git.EnsureExcluded(a.ctx, p.Path, pat); err != nil {
			return TaskView{}, err
		}
	}
	if err := a.git.AddWorktree(a.ctx, p.Path, worktreePath(p, t), branchName(t), p.BaseBranch); err != nil {
		return TaskView{}, err
	}
	if err := a.db.SaveTask(t); err != nil {
		return TaskView{}, err
	}
	if err := copyAttachments(a.runDir(p, t), files); err != nil {
		return TaskView{}, err
	}
	if err := a.writeTaskFile(p, t); err != nil {
		return TaskView{}, err
	}
	if runPlan {
		if agents := a.settings.Agents(); len(agents) > 0 {
			if _, err := a.StartStep(t.ID, agents[0].ID, "", 0); err != nil {
				a.toast("Could not start "+agents[0].Name, err.Error(), true)
			}
		}
	}
	a.emitTask(t.ID)
	return a.GetTask(t.ID)
}

func (a *App) updateTask(id string, fn func(*store.Task) error) error {
	a.mu.Lock()
	t, err := a.db.Task(id)
	if err == nil {
		if err = fn(&t); err == nil {
			err = a.db.SaveTask(t)
		}
	}
	a.mu.Unlock()
	if err == nil {
		a.emitTask(id)
	}
	return err
}

func (a *App) stopTaskSessions(taskID string) {
	steps, _ := a.db.Steps(taskID)
	for _, s := range steps {
		a.run.Stop(s.ID)
	}
}

// ArchiveTask moves a task to the Archive by hand.
func (a *App) ArchiveTask(id string) error {
	a.stopTaskSessions(id)
	return a.updateTask(id, func(t *store.Task) error {
		t.Archived, t.ArchivedAt = true, time.Now()
		return nil
	})
}

// RestoreTask moves an archived task back to Tasks.
func (a *App) RestoreTask(id string) error {
	return a.updateTask(id, func(t *store.Task) error {
		if _, ok := a.settings.Project(t.Project); !ok {
			return fmt.Errorf("add the project %s back in Settings > Projects first", t.Project)
		}
		if t.WorktreeRemoved {
			return errors.New("the worktree was removed; this task can no longer run")
		}
		t.Archived = false
		if t.PRState != "open" {
			t.PRNumber, t.PRState, t.PRURL = 0, "", ""
		}
		return nil
	})
}

// removeWorktree removes a task's worktree and branch (after the UI asked
// for confirmation). The task stays in the Archive.
func (a *App) removeWorktree(id string) ([]string, error) {
	t, err := a.db.Task(id)
	if err != nil {
		return nil, err
	}
	if _, ok := a.settings.Project(t.Project); !ok {
		// Without the project, herd no longer knows the repository: git
		// would run in whatever directory herd started in.
		return nil, fmt.Errorf("the project %s is no longer in Settings > Projects, so herd cannot find its repository", t.Project)
	}
	a.stopTaskSessions(id)
	p := a.projectOf(t)
	var lines []string
	g := gitops.Git{Log: func(l string) { lines = append(lines, l) }}
	err = g.RemoveWorktree(a.ctx, p.Path, worktreePath(p, t), branchName(t))
	views, _ := filepath.Glob(filepath.Join(a.Home, "views", p.Name, t.Slug+"-*"))
	for _, v := range views {
		_, _ = g.Run(a.ctx, p.Path, "worktree", "remove", "--force", v)
		_ = os.RemoveAll(v)
	}
	if err != nil {
		return lines, err
	}
	return lines, a.updateTask(id, func(t *store.Task) error {
		t.WorktreeRemoved = true
		if !t.Archived {
			t.Archived, t.ArchivedAt = true, time.Now()
		}
		return nil
	})
}

// DeleteTask removes an archived task for good: its worktree and local
// branch (the pushed branch and the PR stay on GitHub), its run files
// (artifacts) and its steps in herd.
func (a *App) DeleteTask(id string) ([]string, error) {
	t, err := a.db.Task(id)
	if err != nil {
		return nil, err
	}
	if !t.Archived {
		return nil, errors.New("archive the task before deleting it")
	}
	var lines []string
	if _, ok := a.settings.Project(t.Project); !ok {
		lines = append(lines, "the project "+t.Project+" is no longer in Settings > Projects: its repository, worktree and branch were left alone")
	} else if !t.WorktreeRemoved {
		if lines, err = a.removeWorktree(id); err != nil {
			return lines, err
		}
	}
	a.stopTaskSessions(id)
	p := a.projectOf(t)
	if t.Slug != "" {
		if err := os.RemoveAll(a.runDir(p, t)); err != nil {
			return lines, err
		}
		lines = append(lines, "removed "+a.runDir(p, t))
	}
	if err := a.db.DeleteTask(id); err != nil {
		return lines, err
	}
	a.emit(EvRemoved, map[string]string{"id": id})
	return lines, nil
}

// OpenRunDir opens the folder holding the task's artifacts (TASK.md, PLAN.md,
// REVIEW-n.md, …) in the file manager.
func (a *App) OpenRunDir(taskID string) error {
	t, err := a.db.Task(taskID)
	if err != nil {
		return err
	}
	dir := a.runDir(a.projectOf(t), t)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	return openWith(dir)
}

// openWith opens a folder or link with the desktop's default application.
func openWith(target string) error {
	opener := "xdg-open"
	switch runtime.GOOS {
	case "darwin":
		opener = "open"
	case "windows":
		opener = "explorer"
	}
	cmd := exec.Command(opener, target)
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("%s %s: %w", opener, target, err)
	}
	go func() { _ = cmd.Wait() }()
	return nil
}

// OpenInZed opens the task's worktree, or for a step with a commit a view
// worktree showing only that step's changes. It returns the commands run.
func (a *App) OpenInZed(taskID, stepID string) ([]string, error) {
	t, err := a.db.Task(taskID)
	if err != nil {
		return nil, err
	}
	p := a.projectOf(t)
	cmd := a.settings.Config().ZedCommand
	path := worktreePath(p, t)
	if t.WorktreeRemoved {
		return nil, errors.New("the worktree was removed")
	}
	var lines []string
	if stepID != "" {
		if _, err := a.db.Step(stepID); err != nil {
			return nil, err
		}
		sha := ""
		if ch, err := a.StepChanges(stepID); err == nil && ch.Source == "commit" {
			sha = ch.SHA // its segment's commit
		}
		if sha != "" {
			// Zed's own commit view, in the task's worktree: no extra folder,
			// and no new Zed project when the worktree is already open.
			// Uncommitted work opens the worktree, where Zed shows it.
			full, err := a.git.Run(a.ctx, path, "rev-parse", sha)
			if err != nil {
				return nil, err
			}
			path = "zed://git/commit/" + full + "?repo=" + url.QueryEscape(path)
		}
	}
	lines = append(lines, "$ "+cmd+" "+path)
	return lines, zed.Open(cmd, path)
}

// OpenProjectInZed opens a project's repository.
func (a *App) OpenProjectInZed(projectID string) error {
	p, ok := a.settings.Project(projectID)
	if !ok {
		return fmt.Errorf("unknown project %q", projectID)
	}
	return zed.Open(a.settings.Config().ZedCommand, p.Path)
}

// ReadArtifact returns the step's parsed artifact, shown in its Preview tab.
func (a *App) ReadArtifact(stepID string) (artifacts.Artifact, error) {
	s, err := a.db.Step(stepID)
	if err != nil {
		return artifacts.Artifact{}, err
	}
	t, err := a.db.Task(s.TaskID)
	if err != nil {
		return artifacts.Artifact{}, err
	}
	ag := agentFor(a.settings.Agents(), s.Agent)
	dir := a.runDir(a.projectOf(t), t)
	return artifacts.Read(filepath.Join(dir, ag.ArtifactName(s.N)))
}

// onArtifact tells the frontend an artifact changed so the Preview reloads.
func (a *App) onArtifact(path string) {
	rel, err := filepath.Rel(filepath.Join(a.Home, "runs"), path)
	if err != nil {
		return
	}
	parts := strings.Split(rel, string(filepath.Separator))
	if len(parts) != 3 {
		return
	}
	tasks, err := a.db.Tasks()
	if err != nil {
		return
	}
	for _, t := range tasks {
		if t.Slug == parts[1] && a.projectOf(t).Name == parts[0] {
			a.emit(EvArtifact, map[string]string{"taskId": t.ID, "file": parts[2]})
		}
	}
}

// collectChecks caches the PR's CI checks, for the task view.
func (a *App) collectChecks(t store.Task) {
	cs, err := a.gh.Checks(a.ctx, a.projectOf(t).Repo, t.PRNumber)
	if err != nil {
		return
	}
	a.checksMu.Lock()
	old, had := a.checks[t.ID]
	a.checks[t.ID] = cs
	a.checksMu.Unlock()
	if !had || !slices.Equal(old, cs) {
		a.emitTask(t.ID)
	}
	a.settleCopilotStep(t) // a review waiting for CI ends when CI does
}

// readyLabel marks a PR ready for a human review.
const readyLabel = "ready"

// MarkPRReady turns the task's draft PR into one ready for review and adds
// the ready label.
func (a *App) MarkPRReady(taskID string) error {
	t, err := a.db.Task(taskID)
	if err != nil {
		return err
	}
	if t.PRNumber == 0 || t.PRState != "open" {
		return errors.New("the task has no open pull request")
	}
	repo := a.projectOf(t).Repo
	if err := a.gh.MarkReady(a.ctx, repo, t.PRNumber); err != nil {
		return err
	}
	labels := a.labelsOf(t.ID)
	a.setPR(t.ID, labels, false)
	if slices.Contains(labels, readyLabel) {
		return nil
	}
	if err := a.gh.AddLabel(a.ctx, repo, t.PRNumber, readyLabel); err != nil {
		return err
	}
	a.setPR(t.ID, append(slices.Clone(labels), readyLabel), false)
	return nil
}

// setPR caches a PR's labels and draft flag, for the task view.
func (a *App) setPR(taskID string, labels []string, draft bool) {
	a.checksMu.Lock()
	old, had := a.labels[taskID]
	wasDraft := a.drafts[taskID]
	a.labels[taskID], a.drafts[taskID] = labels, draft
	a.checksMu.Unlock()
	if !had || !slices.Equal(old, labels) || wasDraft != draft {
		a.emitTask(taskID)
	}
}

func (a *App) draftOf(taskID string) bool {
	a.checksMu.Lock()
	defer a.checksMu.Unlock()
	return a.drafts[taskID]
}

// prReady: the open PR is known to be ready for review, not a draft.
func (a *App) prReady(taskID string) bool {
	a.checksMu.Lock()
	defer a.checksMu.Unlock()
	draft, known := a.drafts[taskID]
	return known && !draft
}

func (a *App) labelsOf(taskID string) []string {
	a.checksMu.Lock()
	defer a.checksMu.Unlock()
	return a.labels[taskID]
}

func (a *App) checksOf(taskID string) []github.Check {
	a.checksMu.Lock()
	defer a.checksMu.Unlock()
	return a.checks[taskID]
}

// OpenURL opens a GitHub link (a PR, a CI run) in the browser.
func (a *App) OpenURL(link string) error {
	u, err := url.Parse(link)
	if err != nil || u.Scheme != "https" || u.Host != "github.com" {
		return fmt.Errorf("not a GitHub link: %q", link)
	}
	return openWith(link)
}

// pollPRs archives tasks whose PR was merged or closed, and collects the CI
// checks and Copilot reviews of the open ones.
func (a *App) pollPRs() {
	if !a.pollMu.TryLock() {
		return
	}
	defer a.pollMu.Unlock()
	tasks, err := a.db.Tasks()
	if err != nil {
		return
	}
	for _, t := range tasks {
		a.pollPR(t)
	}
}

// RefreshPR checks the task's PR now: its state, CI checks and Copilot's
// review, without waiting for the next minute.
func (a *App) RefreshPR(taskID string) error {
	t, err := a.db.Task(taskID)
	if err != nil {
		return err
	}
	a.pollMu.Lock() // after a poll already running
	defer a.pollMu.Unlock()
	a.pollPR(t)
	a.emitTask(t.ID)
	return nil
}

func (a *App) pollPR(t store.Task) {
	if t.Archived || t.PRNumber == 0 || t.PRState != "open" {
		return
	}
	p := a.projectOf(t)
	pr, err := a.gh.PRState(a.ctx, p.Repo, t.PRNumber)
	if err == nil {
		a.setPR(t.ID, pr.Labels, pr.Draft)
	}
	if err != nil || pr.State == "open" || pr.State == "" {
		a.collectChecks(t)
		a.collectCopilot(t)
		return
	}
	a.stopTaskSessions(t.ID)
	_ = a.updateTask(t.ID, func(t *store.Task) error {
		t.PRState, t.Archived, t.ArchivedAt = pr.State, true, pr.At
		return nil
	})
	a.toast("Archived "+refOf(t), fmt.Sprintf("PR #%d was %s", t.PRNumber, pr.State), false)
}

func refOf(t store.Task) string {
	if t.Issue != "" {
		return "#" + t.Issue
	}
	return "LOCAL " + t.Slug
}
