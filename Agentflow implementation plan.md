# Herd implementation plan

Oct 31, 2026 · @Antoine

## Goal and scope

Herd is a standalone desktop app (Go + Wails) that runs Claude Code through a configurable pipeline of agents, on tasks from several repositories at once, each task in its own git worktree. It is a companion to Zed: same look, same themes, one click to open any task or step in Zed. The [UI preview](https://claude.ai/artifact/Jh2X5X1tHHsqxrRJ3M36e4) is the reference for every screen; its 16 artboards are listed under Frontend.

Terms used below:

- **Project**: a git repository registered in Settings (e.g. pony-core, pony-rider-app, pony-infra), with its GitHub repo and base branch.
- **Task**: one unit of work in one project, in its own worktree and branch. Its source is a GitHub issue, a description, or both. A task with no issue is tagged **LOCAL**.
- **Agent**: a step type defined in Settings: command, model, permissions, trigger, instructions, artifact. Defaults: Plan, Implement, Review, Push.
- **Step**: one run of an agent on a task, i.e. one Claude Code session. Each opened step gets its own tab.
- **Artifact**: the markdown a step writes (`PLAN.md`, `IMPLEMENT-n.md`, `REVIEW-n.md`, `PR.md`, `FEEDBACK.md`), the only handoff between steps.
- **Context**: markdown given to sessions, general plus per project, edited in Settings.

Workflow rules:

1. A task is created from the New task dialog: pick a project, then a GitHub issue number, a description, or both (at least one).
2. Every step is started by hand with a play button and optional guidance, except agents whose trigger is automatic (by default Review, after each Implement).
3. After a review, the user picks Implement n+1 or Push.
4. Push pushes the branch, opens the PR, waits for the Copilot review and appends it to `FEEDBACK.md`.
5. A task moves to the Archive when its PR is merged or closed, or by hand; it can be restored.
6. Tasks from all projects run in parallel; the left panel groups them by project.

Out of scope for v1: remote access, multiple users, Linear or Jira sources, editing code inside herd (Zed does that).

## Architecture

Herd is one Wails app: a Go process that owns all state and every Claude Code process, and a TypeScript frontend rendered in the system webview. Wails is the Go equivalent of Electron or Tauri: Go methods are bound to the frontend as async functions, and Go emits events the frontend subscribes to.

| Layer | Responsibility | Tech |
| --- | --- | --- |
| Wails shell | Native window, menus, global shortcuts, packaging per OS | Wails v2 |
| App service | Bound methods: projects, tasks, steps, settings, Zed actions | Go structs exposed to JS |
| Runner | One PTY per live step, start, resume, reap idle sessions | `creack/pty` |
| Terminal bridge | Raw PTY bytes to `xterm.js`, resize, input | WebSocket on 127.0.0.1, random port, per-launch token |
| Hook receiver | `POST /hook` from Claude Code hooks → step state | same local HTTP server |
| Store | Tasks, steps, sessions, archive | SQLite (`modernc.org/sqlite`, no cgo) |
| Settings | Projects, agents, context, theme, keymap | TOML and markdown files, watched |
| Git ops | Worktrees, commits, diff views, push | shells out to `git` and `gh` |
| Artifact watcher | Artifact writes, front matter parsing | `fsnotify` |
| Frontend | Board, settings, dialogs, pickers | Vite + TypeScript, Svelte or React, `xterm.js`, `markdown-it`, `mermaid` |
| Claude Code plugin | Default agent skills and the context skill | `plugin/` shipped with the app |

Why a WebSocket for terminals: Wails events carry JSON and suit state changes, but terminal output is a stream of raw bytes from many sessions; a local socket keeps it fast and binary. Everything else goes through bindings and events.

Files on disk:

```
~/.herd/
  config.toml                 # projects, appearance (dark | light | system), fonts, zed command, max live sessions
  agents.toml                 # agent definitions (Settings > Agents)
  context/general.md          # Settings > Context
  context/<project>.md
  keymap.json                 # Settings > Shortcuts
  hooks.json                  # Claude Code settings file with the hooks
  herd.db                     # tasks, steps, sessions, archive
  runs/<project>/<task>/      # PLAN.md, IMPLEMENT-1.md, REVIEW-1.md, PR.md, FEEDBACK.md
  views/<project>/<task>-implement-1/   # disposable diff worktrees
<each repo>/.worktrees/<task>/          # task worktrees, excluded via .git/info/exclude
```

`<task>` is the issue number (`4356`) or `local-<slug>` for a LOCAL task (`local-rebalance-heatmap`).

Source layout:

```
main.go, app.go               # Wails entry and bound App service
internal/{runner,termws,hooks,store,settings,gitops,github,artifacts,zed}/
frontend/                     # Vite app, themes/, components/
plugin/                       # Claude Code plugin with default agent skills
build/                        # Wails packaging assets
```

## Task and step state

A step's state comes from hook events plus its artifact; a task's status is derived from its last step, never stored separately.

&#91;embedded content: step flow · 4 steps, 1 automatic transition\]

The loop between Implement and Review runs as many times as you choose; only the arrow into Review fires without a click.

| Step state | Set when | Task status shown on the left |
| --- | --- | --- |
| `running` | `SessionStart` or `UserPromptSubmit` hook | Running · Implement 2 |
| `idle` | `Stop` hook, artifact not done yet (Claude asked a question or paused) | Waiting for input · Implement 2 |
| `approval` | `Notification` hook asking for permission | Needs approval · Implement 2 |
| `done` | `Stop` hook **and** the artifact exists with `status: done` (review: a `verdict`) | Waiting for input · after Review 1 |
| `failed` | process exits with no done artifact | Waiting for input · retry offered |

A task with no steps shows Not started. A pushed task shows PR #812 open until the PR is merged or closed; then it moves to the Archive (polled with `gh pr view` every few minutes) and shows PR #812 merged · Oct 28.

Why two signals for done: `Stop` fires at the end of every turn, including when Claude stops to ask a question. The artifact front matter is the agent's explicit claim that the step is finished. A **Mark done** button covers the case where the agent forgets.

On `done`, the orchestrator:

1. commits if the step is an Implement and records the sha;
2. starts every agent whose trigger is "automatically after" this agent (by default Review n after Implement n);
3. otherwise shows the next options (after Plan: Implement 1; after Review: Implement n+1 or Push, the verdict's choice first);
4. ends the process after a grace period when no viewer is attached. Clicking the step later resumes it.

Artifact front matter, the contract between agents and orchestrator:

```yaml
---
status: done            # PLAN, IMPLEMENT-n, FEEDBACK
verdict: changes        # REVIEW-n only: approve | changes
---
```

### Task sources and identity

The New task dialog takes a project and two optional fields; at least one must be filled.

| Issue | Description | Task tag | What Plan reads | Branch and worktree |
| --- | --- | --- | --- | --- |
| 1190 | empty | #1190 | `gh issue view 1190 --repo <project repo>`, title and comments | `agent/1190`, `.worktrees/1190` |
| 1190 | filled | #1190 | the issue, then the description as extra instructions | `agent/1190`, `.worktrees/1190` |
| empty | filled | LOCAL | the description only; no GitHub calls | `agent/local-<slug>`, `.worktrees/local-<slug>` |

The task title is the issue title (fetched when the dialog loses focus on the issue field, shown under it) or the description's first line. The slug is the first three words of the description, lowercased and hyphenated, with a numeric suffix on collision. Create makes the worktree and branch from the project's base branch; Create and run Plan also starts the Plan agent.

### Archive

Archived tasks keep their steps, sessions and artifacts and remain searchable. Restore moves a task back to Tasks. Remove worktree runs `git worktree remove` and deletes the branch after a confirmation; the task stays in the Archive.

## Claude Code integration

Each step is an interactive `claude` built from its agent definition in Settings, started in the task's worktree with the agent's command as the initial prompt.

**Agent definition** (`agents.toml`, edited in Settings > Agents):

```toml
[[agent]]
id = "impl"
name = "Implement"
command = "/herd:implement {task} -- {guidance}"
model = "opus"                 # --model
trigger = "manual"             # or "after:impl" for Review
writes = "IMPLEMENT-{n}.md"
done = "status: done"          # front matter that marks the step done
permissions = "acceptEdits"    # default | acceptEdits | plan (read only)
allowed = ["Read", "Edit", "Write", "Bash(go test:*)"]
denied = ["Bash(git commit:*)", "Bash(git push:*)"]
instructions = """..."""       # written to the agent's skill file
```

The order of agents is the pipeline. Placeholders: `{task}` (issue number, or `--local`), `{guidance}`, `{n}`, `{sha}`, `{issue}`. Agents can be added, e.g. a Docs agent; next-step buttons follow the order and triggers.

**Launch** (built by the runner, shown for an Implement step):

```
cd ~/code/pony/pony-core/.worktrees/4356
HERD_URL=http://127.0.0.1:<port> HERD_TOKEN=<token> \
HERD_TASK=core:4356 HERD_STEP=impl-2 HERD_RUN_DIR=~/.herd/runs/pony-core/4356 \
claude --settings ~/.herd/hooks.json --add-dir ~/.herd/runs/pony-core/4356 \
       --model opus --permission-mode acceptEdits \
       --allowedTools "Read Edit Write Bash(go test:*)" --disallowedTools "Bash(git commit:*) Bash(git push:*)" \
       "/herd:implement 4356 -- keep the retry budget configurable"
```

**Task source in the prompt.** The runner writes `TASK.md` to the run dir before the first step: the issue number and repo when there is an issue, the description when there is one, and `source: issue | issue+description | local` in front matter. Every agent's skill reads `TASK.md` first; only Plan fetches the issue body with `gh issue view`.

**Context.** Before each launch the runner writes `CONTEXT.md` to the run dir: general context, then the project context, filtered by the agents each block applies to. The plugin's `context` skill tells every agent to read it. The repo's `CLAUDE.md` is loaded by Claude Code itself; the per-project toggle in Settings only controls whether herd mentions it.

**Hooks** (`~/.herd/hooks.json`) post each event's JSON to the receiver; the token stops other local processes from faking events.

```json
{
  "hooks": {
    "SessionStart":     [{ "hooks": [{ "type": "command", "command": "[ -n \"$HERD_URL\" ] && curl -s -X POST \"$HERD_URL/hook?event=SessionStart\" -H \"X-Herd-Token: $HERD_TOKEN\" -H \"X-Herd-Step: $HERD_TASK/$HERD_STEP\" --data-binary @-" }] }],
    "UserPromptSubmit": [{ "hooks": [{ "type": "command", "command": "same, event=UserPromptSubmit" }] }],
    "Notification":     [{ "hooks": [{ "type": "command", "command": "same, event=Notification" }] }],
    "Stop":             [{ "hooks": [{ "type": "command", "command": "same, event=Stop" }] }]
  }
}
```

**Resume.** Opening a step whose process has ended runs `claude --resume <session_id>` with the same cwd, env and flags, so hooks keep reporting to that step.

**Push** is half agent, half code: the Push agent writes `PR.md`; herd then runs `git push`, `gh pr create --body-file PR.md`, polls the PR reviews until Copilot's arrives and appends its comments to `FEEDBACK.md`. That output streams into the Push tab.

## Git, worktrees and Zed

Every task lives in its own worktree on its own branch, and every finished Implement becomes one commit, so any step's changes can be shown on their own.

| Action | Commands |
| --- | --- |
| New task | `git fetch origin` then `git worktree add -b agent/4356 .worktrees/4356 origin/main` |
| Implement n done | `git -C .worktrees/4356 add -A && git commit -m "[4356] implement-n"`, sha saved on the step |
| Open live worktree | `zed <repo>/.worktrees/4356` |
| Open a step's diff | see below |
| Push | `git push -u origin agent/4356` then `gh pr create --base main --body-file PR.md` |
| Delete task (confirmed) | `git worktree remove .worktrees/4356` then `git branch -D agent/4356` |

**Step diff in Zed.** A disposable view worktree sits at the step's commit, with the index reset to its parent, so Zed's git panel shows exactly that step's changes as uncommitted edits. The live worktree is never touched.

```
V=~/.herd/views/pony-core/4356-implement-1
git -C ~/code/pony/pony-core worktree remove --force "$V" 2>/dev/null
git -C ~/code/pony/pony-core worktree add --detach "$V" b7c41d9
git -C "$V" reset b7c41d9^      # mixed reset: files stay, changes become unstaged
zed "$V"
```

Plan and Review steps have no commit; their Zed button opens the live worktree. View worktrees are pruned when the task is deleted or on startup.

Every git command runs with `-C <project path>`, so tasks from different repos never share state; only the herd process is common. A LOCAL task uses `local-<slug>` in place of the issue number everywhere (`agent/local-rebalance-heatmap`).

Prerequisites, checked in Settings > Projects with a warning row when missing: the `zed` CLI on `PATH` (the command is configurable in Appearance), `gh` authenticated with access to each project's repo, and `.worktrees/` in each repo's `.git/info/exclude` (herd adds it). Each project has a port range in Settings; a task gets `HERD_PORT_BASE` from its project's range so parallel dev servers never collide.

## Frontend

The UI copies the preview screen for screen; build each artboard as listed and use its mock data as fixtures.

| Artboard | Shows | Key behaviour |
| --- | --- | --- |
| 1 Board, Fleet Dark | Tasks by project, 4 step tabs from 3 projects, live terminal, steps, preview | the full interactive prototype |
| 2 Board, Flatwhite | Same board in the light theme | every colour comes from theme tokens |
| 3 Archive | Archive tab, archived task selected | Restore, Remove worktree |
| 4 to 6 New task | Issue only, issue + description, description only | mode box explains what Claude will read; LOCAL preview |
| 7 LOCAL task | Newly created LOCAL task, not started | empty terminal, Plan button in the next-step box |
| 8 Go to task | ⌘P finder searching "unlock" across projects | archived tasks included, listed last |
| 9 to 11 Scope picker | Opened from project, worktree or branch in the title bar | same tasks, labelled by the clicked attribute |
| 12 to 16 Settings | Agents, Context, Projects, Appearance (Light selected), Shortcuts | edits save to the files under Architecture |

**Title bar** (38 px): app mark, then `project / worktree / branch` of the selected task as three ghost buttons; each opens the scope picker on its mode. Right side: Search tasks field with ⌘P, New task with ⌘N, settings gear (⌘,).

**Left panel**: a header toggle Tasks | Archive with counts (⌘⇧A). Below, one collapsible group per project with its tasks as cards: tag (#4356 in mono, or a LOCAL chip), title, status badge. At the bottom, the next-step box for the selected task: guidance field, one play button per allowed next step (primary first, ⌘⏎), Allow once on a pending permission, Restore and Remove worktree for archived tasks.

**Tabs**: every opened step is a tab in the center tab bar, across tasks and projects. A tab shows a state dot (blue live, red approval, grey finished), `#4356 · Implement 2` and the project name, plus a close button. Opening an already open step focuses its tab; starting a step opens a new tab; closing focuses the neighbour. The focused tab drives the selected task: left card, title bar, right panel. Selecting a task card focuses its most recent tab, or opens its last step; a task with no steps shows an empty state with no tab focused. ⌘1 to ⌘9 and ⌘W work on tabs. Open tabs persist across restarts.

**Center**: a toolbar under the tabs (live or resumed chip, agent name and model, session id), then the step's terminal (`xterm.js`).

**Right panel**: the task header (tag, title, source line, description), Steps with Open worktree in Zed, the timeline (ghost row for a pending automatic step, Zed diff on finished Implement steps), then the Preview tab for the focused step's artifact: front matter chips, `markdown-it`, mermaid fences rendered with `mermaid.run()`.

**Status bar** (26 px): running, waiting and approval counts across all projects, then model, branch and project of the focused step.

**Popups**, all Zed-style: elevated surface, 1 px border, 8 px radius, shadow, keyboard first, no dimming except New task.

- **Go to task** (⌘P), centered near the top: search over title, issue number, project, description and LOCAL; every word must match; open tasks first. ⏎ opens the task's last step, ⌘⏎ opens its worktree in Zed.
- **Scope picker** (⌘O, or a click on project, worktree or branch), anchored under the title bar: a Project | Worktree | Branch toggle, project chips in Project mode, a filter, then the scoped project's open tasks labelled by title, worktree path or branch.
- **New task** (⌘N): project chips, issue field (digits only, repo shown, title fetched), description, a mode box (Issue only, Issue + description, LOCAL, or Missing), then Cancel, Create, Create and run Plan.

**Settings** replaces the three panels with a left nav and a content column; Esc or Back to board returns. Agents: pipeline strip, agent list, editor (name, artifact, command, model, trigger, permissions, allowed and denied tools, done condition, instructions). Context: General plus one entry per project, markdown editor, Also load CLAUDE.md, Applies to agent toggles. Projects: one card per repo (path, GitHub repo, base branch, worktrees folder, port range, Open in Zed, Remove) and Add project with a folder picker. Appearance: three cards, Dark, Light and System (Dark uses Fleet Dark, Light uses Flatwhite, System follows the OS and switches live when the OS does), then UI and terminal fonts and the Zed command. Shortcuts: the keymap below.

| Shortcut | Action |
| --- | --- |
| ⌘P | Go to task, all projects |
| ⌘O | Scope picker for the selected task's project |
| ⌘N | New task |
| ⌘⏎ | Run the primary next step |
| ⌘⇧A | Toggle Tasks and Archive |
| ⌥↓ / ⌥↑ | Next or previous task in the left panel |
| ⌘1 to ⌘9, ⌘W | Go to tab, close tab |
| ⌘⇧E / ⌘⇧D | Open worktree in Zed / open the focused step's diff in Zed |
| ⌘, | Settings |
| Esc | Close popup or settings |

Ctrl replaces ⌘ on Linux and Windows. Shortcuts are handled in the frontend from `keymap.json`, except ⌘P and ⌘N which also work as menu items.

**Go ↔ frontend.** Bound methods: `ListProjects`, `ListTasks`, `CreateTask(project, issue, description, runPlan)`, `StartStep(task, agent, guidance)`, `AllowOnce(step)`, `MarkDone(step)`, `ArchiveTask`, `RestoreTask`, `RemoveWorktree`, `OpenInZed(task, step?)`, `ReadArtifact(step)`, `GetSettings`, `SaveAgents`, `SaveContext`, `SaveProject`, `SetTheme`, `TerminalURL(step)` (resumes the session if needed and returns the socket URL). Events: `task:changed`, `step:changed`, `artifact:changed`, `settings:changed`.

**Themes.** Both themes are token files in `frontend/themes/`; components never use raw colours. Fleet Dark comes from [zed-fleet-themes](https://github.com/skarline/zed-fleet-themes); Flatwhite from the [Flatwhite palette](https://github.com/doomemacs/themes/blob/master/themes/doom-flatwhite-theme.el), with muted text darkened to pass 4.5:1. Type is IBM Plex Sans 13 px and IBM Plex Mono for terminals and code.

| Token | Fleet Dark | Flatwhite |
| --- | --- | --- |
| bg (panels, terminal) | `#18191B` | `#f7f3ee` |
| chrome (title, tab, status bars) | `#090909` | `#f1ece4` |
| surface (inputs, code) | `#252629` | `#fdfbf8` |
| border | `#3E4147` | `#dcd3c6` |
| text / muted | `#E0E1E4` / `#898E94` | `#605a52` / `#776b5b` |
| focus, primary button | `#2A7DEB` / `#0870E4` | `#6a4dff` / `#6a4dff` |
| running (text, bg) | `#4B8DEC` on `#223045` | `#4c5361` on `#dde4f2`, dot `#75a3ff` |
| waiting | `#FAD075` on `#362b16` | `#5b5143` on `#f7e0c3`, dot `#f08c00` |
| needs approval | `#F87C88` on `#402228` | `#5b4343` on `#f6cfcb`, dot `#f00000` |
| done, approved | `#69B090` on `#18312a` | `#525643` on `#e2e9c1`, dot `#84bd00` |
| LOCAL chip | `#AF9CFF` on 14% tint | `#614c61` on `#f1ddf1` |
| diff added / removed bg | `#283732` / `#452d31` | `#e2e9c1` / `#f6cfcb` |

## Milestones

Build in this order. Milestones 1 to 3 prove the risky part (embedded terminals plus hooks in a Wails window); each ends with something you can try. Build the frontend against the preview's mock data first, then swap in bound methods.

1. **Wails shell and one terminal.** `wails init` with Vite + TypeScript, theme tokens for both themes, title and status bars, one hard-coded `claude` in a PTY shown through `xterm.js` over the local WebSocket.
   - Done when: you can chat with Claude Code in the window, resize and Ctrl+C work, the theme switches live.
2. **Projects, tasks, worktrees.** Settings > Projects, SQLite store, New task dialog with the three source modes, worktree and branch creation, left panel grouped by project.
   - Done when: one issue task and one LOCAL task exist in two different repos, each with its worktree.
3. **Agents, hooks, statuses.** `agents.toml` and Settings > Agents, launch built from the agent, `TASK.md` and `CONTEXT.md`, hook receiver, status badges, Allow once.
   - Done when: Plan runs in two projects at once and both badges follow Running → Waiting with no polling.
4. **Tabs.** One tab per opened step, focus rules, close, ⌘1 to ⌘9, persistence.
   - Done when: four steps from three projects are open as tabs and survive a restart.
5. **Done detection, commit, automatic agents.** Artifact watcher, front matter, commit on Implement done, agents with an automatic trigger, next-step buttons.
   - Done when: Plan → Implement 1 → Review 1 runs with exactly two clicks and leaves one commit.
6. **Resume and preview.** Session ids, resume on tab focus, idle reaping, artifact preview with mermaid.
   - Done when: after a restart, focusing the Review 1 tab reopens that conversation and its REVIEW-1.md renders.
7. **Navigation.** Go to task (⌘P), scope picker from the title bar (⌘O), the full keymap from `keymap.json`.
   - Done when: every shortcut in Settings > Shortcuts works without the mouse.
8. **Zed.** Open worktree, step diff views, prerequisite checks.
   - Done when: Zed diff on Implement 1 shows only that commit's changes in Zed's git panel.
9. **Push, Copilot, Archive.** PR.md, push, PR creation, Copilot polling into FEEDBACK.md, archive on merge or close, Restore, Remove worktree.
   - Done when: merging the PR on GitHub moves the task to Archive within a few minutes.
10. **Context and polish.** Settings > Context, startup reconciliation (dead processes → failed), Mark done, live-session cap, packaging for macOS and Linux.
    - Done when: killing herd mid-step and restarting leaves every task in a sensible state.

## Risks and open questions

| Risk | Mitigation |
| --- | --- |
| Hook names, payloads, `--settings` merging or CLI flags differ from this plan | Check against the current Claude Code docs in milestone 1, before building on them |
| `xterm.js` in the system webview behaves differently per OS (WebKitGTK on Linux) | Test milestone 1 on every target OS; keep the WebGL renderer optional |
| The agent forgets the done front matter | Skill instructions + Mark done button |
| Too many live `claude` processes across projects | Cap live sessions (default 6), reap idle ones, resume on demand |
| Parallel builds fight for CPU and ports | Per-project port ranges, per-task build dirs |
| Usage limits hit faster with parallel sessions | Show usage in the status bar later; keep the cap low |
| Review edits code despite instructions | Its agent runs read-only with edit tools denied |
| Copilot review never arrives | Stop polling after a timeout and note it in FEEDBACK.md |

Open questions:

- [ ] How is the Copilot review requested: a repo rule that reviews every PR, or an explicit request after `gh pr create`?
- [ ] Squash the Implement commits at Push, or keep one commit per step in the PR?
- [ ] Does Review n+1 look only at Implement n+1's commit, or at the whole branch?
- [ ] Frontend framework: Svelte (smaller, closer to plain HTML) or React (larger ecosystem)?
- [ ] Should a LOCAL task be able to create a GitHub issue at Push time?
