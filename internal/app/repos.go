package app

// Linked repositories: a task can change other repositories than its
// project's. Each linked one gets a worktree on the task's branch; agents may
// edit it (--add-dir) and its changes show with the step's own.

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"herd/internal/gitops"
	"herd/internal/store"
)

// LinkedRepo is another repository a task changes, in a worktree of its own.
type LinkedRepo struct {
	Name     string `json:"name"`     // the repository folder, e.g. okai-tcp-server
	Repo     string `json:"repo"`     // its main checkout
	Worktree string `json:"worktree"` // where the task's branch is checked out
}

func reposKey(taskID string) string { return "repos:" + taskID }

// linkedRepos lists the repositories linked to a task.
func (a *App) linkedRepos(taskID string) []LinkedRepo {
	v, _ := a.db.Get(reposKey(taskID))
	var rs []LinkedRepo
	if v != "" {
		_ = json.Unmarshal([]byte(v), &rs)
	}
	return rs
}

// AddTaskRepo asks for a repository and links it to the task.
func (a *App) AddTaskRepo(taskID string) ([]LinkedRepo, error) {
	dir, err := a.plat.PickFolder("Repository this task also changes")
	if err != nil || dir == "" {
		return a.linkedRepos(taskID), err
	}
	return a.AddTaskRepoPath(taskID, dir)
}

// AddTaskRepoPath links the git repository at dir to the task: a worktree
// on the task's branch, from the repository's default branch.
func (a *App) AddTaskRepoPath(taskID, dir string) ([]LinkedRepo, error) {
	t, err := a.db.Task(taskID)
	if err != nil {
		return nil, err
	}
	if t.Archived || t.WorktreeRemoved {
		return nil, errors.New("restore the task before linking a repository")
	}
	root, err := a.git.Run(a.ctx, expandHome(dir), "rev-parse", "--show-toplevel")
	if err != nil {
		return nil, fmt.Errorf("%s is not a git repository", dir)
	}
	root = strings.TrimSpace(root)
	if root == a.projectOf(t).Path {
		return nil, errors.New("that is the task's own repository")
	}
	rs := a.linkedRepos(taskID)
	for _, r := range rs {
		if r.Repo == root {
			return rs, nil
		}
	}
	// The repository's default branch, else the one checked out.
	base, err := a.git.Run(a.ctx, root, "symbolic-ref", "--short", "refs/remotes/origin/HEAD")
	if err != nil {
		if base, err = a.git.Run(a.ctx, root, "branch", "--show-current"); err != nil {
			return nil, err
		}
	}
	base = strings.TrimPrefix(strings.TrimSpace(base), "origin/")
	if _, err := a.git.EnsureExcluded(a.ctx, root, ".worktrees/"); err != nil {
		return nil, err
	}
	wt := filepath.Join(root, ".worktrees", t.Slug)
	if err := a.git.AddWorktree(a.ctx, root, wt, branchName(t), base); err != nil {
		return nil, err
	}
	rs = append(rs, LinkedRepo{Name: filepath.Base(root), Repo: root, Worktree: wt})
	b, _ := json.Marshal(rs)
	if err := a.db.Set(reposKey(taskID), string(b)); err != nil {
		return nil, err
	}
	a.emitTask(taskID)
	return rs, nil
}

// removeLinkedRepos removes the linked worktrees and their local branches
// (a pushed branch stays on GitHub).
func (a *App) removeLinkedRepos(t store.Task, say func(string)) {
	g := gitops.Git{Log: say}
	for _, r := range a.linkedRepos(t.ID) {
		if _, err := os.Stat(r.Repo); err != nil {
			continue
		}
		if err := g.RemoveWorktree(a.ctx, r.Repo, r.Worktree, branchName(t)); err != nil {
			say(err.Error())
		}
	}
}
