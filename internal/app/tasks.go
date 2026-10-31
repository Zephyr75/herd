package app

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"herd/internal/artifacts"
	"herd/internal/gitops"
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
// the first agent.
func (a *App) CreateTask(projectID, issue, description string, runPlan bool) (TaskView, error) {
	issue = strings.TrimPrefix(strings.TrimSpace(issue), "#")
	description = strings.TrimSpace(description)
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
	if issue != "" && p.Repo == "" {
		return TaskView{}, fmt.Errorf("%s has no GitHub repo: leave the issue empty for a LOCAL task", p.Name)
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
		if t, err := a.gh.IssueTitle(a.ctx, p.Repo, issue); err == nil && t != "" {
			title = t
		} else {
			title = "Issue #" + issue
		}
	} else {
		title = titleFromDescription(description)
	}

	t := store.Task{
		ID: p.ID + ":" + slug, Project: p.ID, Slug: slug, Issue: issue, Title: title, Description: description,
		PortBase: portBase(p.PortRange, len(taken)), CreatedAt: time.Now(),
	}
	if _, err := a.git.EnsureExcluded(a.ctx, p.Path, strings.TrimSuffix(p.WorktreesDir, "/")+"/"); err != nil {
		return TaskView{}, err
	}
	if err := a.git.AddWorktree(a.ctx, p.Path, worktreePath(p, t), branchName(t), p.BaseBranch); err != nil {
		return TaskView{}, err
	}
	if err := a.db.SaveTask(t); err != nil {
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

// portBase gives each task a block of 10 ports in its project's range.
func portBase(r string, index int) int {
	var lo, hi int
	if _, err := fmt.Sscanf(r, "%d-%d", &lo, &hi); err != nil || hi <= lo {
		return 0
	}
	blocks := (hi - lo + 1) / 10
	if blocks <= 0 {
		return lo
	}
	return lo + 10*(index%blocks)
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

// RemoveWorktree removes a task's worktree and branch (after the UI asked
// for confirmation). The task stays in the Archive.
func (a *App) RemoveWorktree(id string) ([]string, error) {
	t, err := a.db.Task(id)
	if err != nil {
		return nil, err
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
	var lines []string
	if stepID != "" {
		s, err := a.db.Step(stepID)
		if err != nil {
			return nil, err
		}
		if s.SHA != "" {
			ag := agentFor(a.settings.Agents(), s.Agent)
			name := strings.ToLower(strings.ReplaceAll(ag.DisplayName(s.N), " ", "-"))
			view := filepath.Join(a.Home, "views", p.Name, t.Slug+"-"+name)
			g := gitops.Git{Log: func(l string) { lines = append(lines, l) }}
			if err := g.DiffView(a.ctx, p.Path, view, s.SHA); err != nil {
				return lines, err
			}
			path = view
		}
	}
	if t.WorktreeRemoved && path == worktreePath(p, t) {
		return nil, errors.New("the worktree was removed")
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

// ReadArtifact returns the parsed artifact the step's Preview shows.
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
	art, err := artifacts.Read(filepath.Join(dir, ag.PreviewName(s.N)))
	if err == nil && !art.Exists && ag.Preview != "" {
		art, err = artifacts.Read(filepath.Join(dir, ag.ArtifactName(s.N)))
	}
	return art, err
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

// pollPRs archives tasks whose PR was merged or closed.
func (a *App) pollPRs() {
	tasks, err := a.db.Tasks()
	if err != nil {
		return
	}
	for _, t := range tasks {
		if t.Archived || t.PRNumber == 0 || t.PRState != "open" {
			continue
		}
		p := a.projectOf(t)
		state, at, err := a.gh.PRState(a.ctx, p.Repo, t.PRNumber)
		if err != nil || state == "open" || state == "" {
			continue
		}
		a.stopTaskSessions(t.ID)
		_ = a.updateTask(t.ID, func(t *store.Task) error {
			t.PRState, t.Archived, t.ArchivedAt = state, true, at
			return nil
		})
		a.toast("Archived "+refOf(t), fmt.Sprintf("PR #%d was %s", t.PRNumber, state), false)
	}
}

func refOf(t store.Task) string {
	if t.Issue != "" {
		return "#" + t.Issue
	}
	return "LOCAL " + t.Slug
}
