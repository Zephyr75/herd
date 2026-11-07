package app

// Revert: the latest step of an agent that can be reverted (Implement,
// Discuss), while nothing committed after it, can be undone. herd keeps a snapshot of the worktree as each step starts; Revert
// puts the worktree back to Implement's and removes it and every later step.

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"

	"herd/internal/store"
)

func snapKey(stepID string) string { return "snap:" + stepID }

func (a *App) snapIndex(t store.Task) string {
	return filepath.Join(a.runDir(a.projectOf(t), t), ".snapshot-index")
}

// snapshot records the worktree as step id starts.
func (a *App) snapshot(t store.Task, stepID string) {
	if tree, err := a.git.Snapshot(a.ctx, worktreePath(a.projectOf(t), t), a.snapIndex(t)); err == nil {
		_ = a.db.Set(snapKey(stepID), tree)
	}
}

// revertable reports whether step i can be reverted: its agent can be
// (Settings > Agents), no step of such an agent or of one that commits ran
// after it, it and every step after it finished, nothing was committed since
// it started, and herd has a snapshot of its start.
func (a *App) revertable(t store.Task, steps []StepView, i int, commits []commit) bool {
	s := steps[i]
	agents := a.settings.Agents()
	if t.Archived || t.WorktreeRemoved || s.External || !agentFor(agents, s.Agent).Revert {
		return false
	}
	for _, x := range steps[i+1:] {
		if ag := agentFor(agents, x.Agent); ag.Revert || ag.Commit {
			return false
		}
	}
	for _, x := range steps[i:] {
		if x.State != store.Done && x.State != store.Failed {
			return false
		}
	}
	raw := make([]store.Step, len(steps))
	for k := range steps {
		raw[k] = steps[k].Step
	}
	if segmentCommit(commits, raw, i) != nil {
		return false
	}
	snap, _ := a.db.Get(snapKey(s.ID))
	return snap != ""
}

// RevertStep puts the worktree back to how it was when the step started and
// removes the step and every later one, with their artifacts.
func (a *App) RevertStep(stepID string) error {
	s, err := a.db.Step(stepID)
	if err != nil {
		return err
	}
	t, err := a.db.Task(s.TaskID)
	if err != nil {
		return err
	}
	v, err := a.view(t)
	if err != nil {
		return err
	}
	i := slices.IndexFunc(v.Steps, func(x StepView) bool { return x.ID == stepID })
	if i < 0 || !v.Steps[i].Revert {
		return errors.New("this step cannot be reverted: only the latest step of an agent with Revert on, once finished and while nothing was committed after it")
	}
	snap, _ := a.db.Get(snapKey(stepID))
	p := a.projectOf(t)
	if err := a.git.Restore(a.ctx, worktreePath(p, t), snap, a.snapIndex(t)); err != nil {
		return fmt.Errorf("could not restore the worktree: %w", err)
	}
	agents := a.settings.Agents()
	for _, x := range v.Steps[i:] {
		a.run.Stop(x.ID)
		_ = os.Remove(filepath.Join(a.runDir(p, t), agentFor(agents, x.Agent).ArtifactName(x.N)))
		_ = a.db.Set(snapKey(x.ID), "")
	}
	if err := a.db.DeleteStepsFrom(t.ID, s.Seq); err != nil {
		return err
	}
	a.emitTask(t.ID)
	return nil
}
