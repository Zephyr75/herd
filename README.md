<p align="center">
  <img src="build/appicon.svg" width="112" alt="herd logo: a horse head" />
</p>

<h1 align="center">herd</h1>

<p align="center"><b>Round up your Claude Code agents.</b><br />
One task, one git worktree, one pipeline, while you sip your coffee. ☕</p>

---

## 🐎 The ride

```
Plan → Implement → Review → Test → Commit and push → Copilot
         ↑___________ fails send you back here _________|
```

- **Implement** kicks off **Review** by itself. Happy? Go to **Test**. Not happy? Edit by hand and **Rerun** the review.
- **Test** fails? Back to **Implement**. Nothing is committed before **Commit and push**.
- **Commit and push** commits, pushes and opens a **draft** PR (`#issue` in the title, `Closes #issue` in the body), asks Copilot for a review and tracks CI. **Mark ready** when you are.

Every step is a real `claude` session in its own terminal: watch it, answer it, or take it over in Zed.

## 🚀 Saddle up

You need Go, Node 20+, `git`, `gh` (logged in), `claude`, `zed`, `curl`, and on Linux `webkit2gtk-4.1`.

```sh
go install github.com/wailsapp/wails/v2/cmd/wails@v2.17.0
make dev     # the app, with hot reload
make build   # build/bin/herd
make ui      # just the UI in a browser, on mock data
make test    # Go tests, with a fake claude
```

> 🐴 **Working on herd with herd?** `make dev` restarts on any new `.go` file, including in a task's
> worktree, which kills the running step. Use `make build && ./build/bin/herd`, or move the
> project's worktrees out of the repo (Settings → Projects → Worktrees folder, e.g. `../herd.worktrees`).

## 🧭 Good to know

- **Agents** live in `~/.herd/agents.toml` (Settings → Agents). Rules in Settings → Context go into every session.
- **Run files** (TASK.md, PLAN.md, REVIEW-n.md…) live in `~/.herd/runs/<project>/<task>/`, one click from the Steps panel.
- **Diagrams:** agents draw them with the [archify](https://github.com/tt-a1i/archify) skill (install it in `~/.claude/skills`): Plan draws the current flow and the new one, to compare at a glance. They appear inside the artifact; **Open full size** lists every diagram of the task. Without archify, agents fall back to mermaid.
- **Several repos?** Plan ends with a `## Sister tasks` section (after a `---` line), one `### <repo>` each; Next step then offers, in one click, a **sister task** in each: same issue, its own worktree, plan and PR, linked from the task header.
- **Edits outside the worktree** by mistake are spotted: Next step warns before Commit and push.
- **Quitting herd** keeps working steps alive: the next herd reattaches them.
- **Shortcuts:** Alt+E and Alt+A hide the side panels, Esc closes pop-ups.

## 🗺️ Where things are

```
main.go, app.go      Wails entry
internal/app         tasks, steps, hooks, commit, push
internal/runner      one PTY per step  ·  holder keeps it alive across restarts
internal/github      PRs, CI, Copilot reviews
internal/…           store (SQLite), settings, gitops, hooks, artifacts, zed
frontend/            Vue 3 + xterm.js + mermaid; archify pages in a sandboxed iframe
plugin/              the Claude Code plugin herd ships
```

Design notes: `Agentflow implementation plan.md`.
