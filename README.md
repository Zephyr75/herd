# herd

A desktop app (Go + Wails) that runs Claude Code through a configurable pipeline of agents
(Plan → Implement → Review → Push), on tasks from several repositories at once, each task in
its own git worktree. A companion to Zed: same themes (Fleet Dark, Flatwhite), one click to
open a task or a step's diff in Zed.

See `Agentflow implementation plan.md` for the design; the UI follows the Herd UI preview.

## Run

Prerequisites: Go (see go.mod), Node 20+, `git`, `gh` (authenticated), `claude`, `zed` on `PATH`,
`curl` (used by the hooks), and on Linux `webkit2gtk-4.1`.

```sh
go install github.com/wailsapp/wails/v2/cmd/wails@v2.17.0
make dev      # desktop app with hot reload
make build    # build/bin/herd
make ui       # the UI alone in a browser, on mock data (no Go needed)
make test     # Go tests, including a full Plan → Implement → Review run with a fake claude
```

**Using herd on its own repo during `make dev`:** `wails dev` rebuilds and restarts the app
whenever a `.go` file appears anywhere under this folder, including in a new task worktree under
`.worktrees/`. The restart kills the step that just started. Either run the built app
(`make build && ./build/bin/herd`) for tasks on herd itself, or set the herd project's
Worktrees folder in Settings > Projects to a path outside the repo, such as `../herd.worktrees`
(absolute and `~/` paths work too).

In `make ui`, URL params open any screen of the preview, e.g.
`?sel=core:4349&left=archive`, `?overlay=palette&q=unlock`, `?overlay=new&new=local`,
`?overlay=scope&scope=branch&sel=rider:1188`, `?view=settings&tab=appearance&theme=light`.

## Layout

```
main.go, app.go        Wails entry; Shell bridges events and the folder picker
internal/app           the bound App service: tasks, steps, hooks → state, done, commit, push
internal/runner        one PTY per live step, scrollback, reaping
internal/holder        `herd hold`: keeps a step's PTY alive across herd restarts
internal/usage         tokens and API-price cost per session, from Claude Code transcripts
internal/termws        PTY ⇄ xterm.js over ws://127.0.0.1 with a per-launch token
internal/hooks         POST /hook receiver and ~/.herd/hooks.json
internal/store         SQLite (tasks, steps, open tabs)
internal/settings      config.toml, agents.toml, context/*.md, keymap.json (watched)
internal/gitops        worktrees, commits, push
internal/github        gh: issue titles, PRs, Copilot review
internal/artifacts     front matter, run-dir watcher
internal/zed           open in Zed
frontend/              Vue 3 + TypeScript, xterm.js, markdown-it, mermaid; themes/ holds the tokens
plugin/                Claude Code plugin shipped with herd (commands are generated per agent)
```

## How a step runs

```
cd <repo>/.worktrees/<task>
HERD_URL=… HERD_TOKEN=… HERD_TASK=core:4356 HERD_STEP=impl-2 HERD_RUN_DIR=~/.herd/runs/pony-core/4356 \
claude --settings ~/.herd/hooks.json --plugin-dir ~/.herd/plugin --add-dir <run dir> [--add-dir <knowledge base>] \
       --model opus --permission-mode auto \
       --append-system-prompt "<herd step: n, sha, artifact path, done rule; your rules>" \
       --session-id <uuid> -- "/herd:implement 4356 -- keep the retry budget configurable"
```

Hooks (`SessionStart`, `UserPromptSubmit`, `Notification`, `PostToolUse`, `Stop`) drive the
step state; a step is done when `Stop` fires and its artifact's front matter matches the agent's
done rule (or the user clicks Mark done). Then herd starts automatic agents (Review after
Implement), commits (each Review commits the changes it reviewed), or pushes and opens the PR
(Push) and waits for the Copilot review. Zed diff on a review opens its commit in Zed's commit
view, inside the task's worktree; a file in the Changes tab opens as it was in that commit.

To continue a session in Zed: Open worktree in Zed, press Stop in the terminal toolbar (herd's
claude ends, so only one process uses the session), copy the `claude --resume` command into a Zed
terminal, and press Resume in herd when you are done.

Every session runs in Claude Code's auto mode. The Rules in Settings > Agents apply to every agent
(say there what agents may not do); the context and knowledge base folders in Settings > Context
apply to every session.
Each claude runs inside a small `herd hold` process, so quitting herd leaves steps that are still
working running: the next herd reattaches them with their output, and their hooks reach it
through `~/.herd/endpoint`. Finished and paused steps end when herd quits.
