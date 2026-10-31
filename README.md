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

In `make ui`, URL params open any screen of the preview, e.g.
`?sel=core:4349&left=archive`, `?overlay=palette&q=unlock`, `?overlay=new&new=local`,
`?overlay=scope&scope=branch&sel=rider:1188`, `?view=settings&tab=appearance&theme=light`.

## Layout

```
main.go, app.go        Wails entry; Shell bridges events and the folder picker
internal/app           the bound App service: tasks, steps, hooks → state, done, commit, push
internal/runner        one PTY per live step, scrollback, reaping
internal/termws        PTY ⇄ xterm.js over ws://127.0.0.1 with a per-launch token
internal/hooks         POST /hook receiver and ~/.herd/hooks.json
internal/store         SQLite (tasks, steps, open tabs)
internal/settings      config.toml, agents.toml, context/*.md, keymap.json (watched)
internal/gitops        worktrees, commits, step diff views, push
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
claude --settings ~/.herd/hooks.json --plugin-dir ~/.herd/plugin --add-dir <run dir> \
       --model opus --permission-mode acceptEdits --allowedTools … --disallowedTools … \
       --append-system-prompt "<herd step: n, sha, artifact path, done rule>" \
       --session-id <uuid> -- "/herd:implement 4356 -- keep the retry budget configurable"
```

Hooks (`SessionStart`, `UserPromptSubmit`, `Notification`, `PostToolUse`, `Stop`) drive the
step state; a step is done when `Stop` fires and its artifact's front matter matches the agent's
done rule (or the user clicks Mark done). Then herd commits (Implement), starts automatic agents
(Review after Implement), or pushes and opens the PR (Push) and waits for the Copilot review.
