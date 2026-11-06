package app

// Revert: the latest Implement, while nothing committed after it, can be
// undone. herd keeps a snapshot of the worktree as each step starts; Revert
// puts the worktree back to Implement's and removes it and every later step.

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"

	"herd/internal/settings"
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

// implementAgent is the agent that writes the code: the one the first agent
// (Plan) offers first as its next step.
func implementAgent(agents []settings.Agent) string {
	if len(agents) == 0 || len(agents[0].Next) == 0 {
		return ""
	}
	return agents[0].Next[0]
}

// revertable reports whether step i can be reverted: the latest Implement
// step, with no committing step after it, everything after it finished,
// nothing committed since it started, and a snapshot of its start.
func (a *App) revertable(t store.Task, steps []StepView, i int, commits []commit) bool {
	s := steps[i]
	agents := a.settings.Agents()
	impl := implementAgent(agents)
	if t.Archived || t.WorktreeRemoved || s.External || impl == "" || s.Agent != impl {
		return false
	}
	for _, x := range steps[i+1:] {
		if ag := agentFor(agents, x.Agent); x.Agent == impl || ag.Commit || ag.Push {
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
		return errors.New("only the latest Implement can be reverted, once finished and while nothing committed after it")
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
