---
name: context
description: Use at the start of every herd step (any /herd:* command, or when $HERD_RUN_DIR is set) to load the task source and the team context before doing anything else.
---

# herd context

herd runs you as one step of a task pipeline. Before working:

1. Read `$HERD_RUN_DIR/TASK.md`: the task source (GitHub issue, description, or both) and its worktree and branch.
2. Read `$HERD_RUN_DIR/CONTEXT.md`: general and project context written from Settings > Context. Follow it.
3. Read the artifacts of earlier steps in `$HERD_RUN_DIR` when your instructions mention them (`PLAN.md`, `IMPLEMENT-n.md`, `REVIEW-n.md`, `TEST-n.md`, `COPILOT-n.md`).

Artifacts are the only handoff between steps. Each starts with YAML front matter; add `status: done`
(the step succeeded) or `status: failed` (it did not: say why) only when your step is finished.
The next automatic agent starts only after a success.
Never commit or push: herd does it.
