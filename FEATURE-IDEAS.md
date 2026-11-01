# Feature ideas for herd

Suggestions that build on what herd already does. Effort: **S** small, **M** medium, **L** large.

## Top picks

| Feature | Why | Effort |
|---|---|---|
| **Desktop notifications** when a step needs approval, waits for input, or finishes | Today the only alerts are in-app toasts, so a blocked agent goes unnoticed while you're in Zed | S |
| **Changed files per step** in the right panel (`git diff --stat` of the step's commit, with +/- counts) | Shows what a step did without opening Zed. Click a file to open it in Zed | S |
| **Resume guard**: before Resume, check whether a `claude --resume <id>` process is still running (e.g. in Zed) and warn | Closes the remaining gap in the manual Stop / Resume flow | S |
| **Usage per step and per task** (tokens and cost, from the transcript `.jsonl`) in the timeline and status bar | Parallel sessions burn through limits fast. Already listed as a risk in the plan | M |
| **Sessions that survive a herd restart** (run claude under `dtach` or `tmux` and reattach) | Quitting herd kills every live step today | M |

## Git and pull requests

- **Behind-base indicator and "Update from base"**: shows "behind dev by 12" and rebases or merges the worktree, flagging conflicts. Long tasks drift. **M**
- **CI checks on the task card** (`gh pr checks`): the PR shows red or green next to its number. **S**
- **Human review comments into FEEDBACK.md**, not only Copilot's, with a one-click "Implement feedback" step. **M**
- **Better commit messages**: build the commit message from the IMPLEMENT artifact's summary (conventional commits), with an optional squash before Push. **S**
- **Merge from herd**: a Merge button once checks pass and the PR is approved, then auto-archive. **S**
- **Bulk cleanup**: remove worktrees of tasks merged more than N days ago, and show the disk used by `.worktrees/`. **S**

## Sessions and terminal

- **Plain shell tab per task**: a non-Claude terminal opened in the worktree. Useful for tests, git, or running the resume command. **S**
- **Search in a step's output** (xterm search addon). **S**
- **Fork a step**: `claude --resume <id> --fork-session` to try another approach without losing the original. **M**
- **Rerun with another model**: retry a step on Sonnet or Opus from the timeline menu. **S**
- **Dev server per task**: a project-level "run" command (e.g. `make dev`) started in a tab, using the task's port range (`HERD_PORT_BASE`). **M**

## Pipeline and agents

- **Review ↔ Implement loop limit**: auto-run Implement after "changes requested", up to N rounds, then stop and ask you. **M**
- **Shell gates**: a pipeline step that runs a command (lint, tests, build) instead of Claude. It blocks the commit or Push on failure and its output shows in the timeline. **M**
- **Per-project agent overrides**: different model, tools or rules per repo (e.g. no tests in an infra repo). **M**
- **Plan approval gate**: edit PLAN.md (or open it in Zed) and press Approve before Implement can start. **S**
- **Import and export the pipeline** (agents.toml plus context) to share a setup with teammates. **S**

## Tasks

- **Issue picker in New task**: browse open issues (assigned to me, or by label) instead of typing a number. Pick several to create a batch. **M**
- **Task queue**: queue Plan steps to start as live slots free up (respecting `max_live_sessions`), e.g. overnight. **M**
- **Timeline durations**: start time and duration per step, and total time per task. **S**

## UI

- **Hamburger as an app menu**, like Zed: Settings, Shortcuts, Open ~/.herd, Check prerequisites, Quit. **S**
- **Command palette for actions**, not only tasks: Stop, Resume, Copy resume command, Open diff, Archive. **S**

## Later (out of scope for v1 in the plan)

- Linear and Jira task sources.
- Remote access: watch and approve steps from a phone.
