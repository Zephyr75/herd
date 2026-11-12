<p align="center">
  <img src="build/appicon.svg" width="112" alt="herd logo: a horse head" />
</p>

<h1 align="center">herd</h1>

<p align="center"><b>Round up your Claude Code agents.</b><br />
One task, one git worktree, one pipeline, while you sip your coffee. ☕</p>

---

## 🐎 The ride

```
Plan → Implement ─→ Review → Test → Commit and push → Copilot
     ↘ Discuss ──↗   ↑____ fails send you back to Implement or Discuss ____|
```

- **Implement** codes on its own and kicks off **Review** by itself. **Discuss** is a conversation: Claude reads the task and waits for your questions, changes code only when you ask, and sums up the talk in `DISCUSS-n.md`. After a passed Copilot, a Discuss that changes no code keeps the task green.
- Review happy? Go to **Test**. Not happy? Edit by hand and **Rerun** the review.
- **Test** fails? Back to **Implement** or **Discuss**. Nothing is committed before **Commit and push**.
- **Commit and push** asks you for the PR's type, scope and title in a small form, writes `PUSH.md`, then herd commits, pushes and opens a **draft** PR with it. **Copilot** then reviews it and herd tracks CI, while Claude explains the PR in `COPILOT-n.md`: a TL;DR to retell it, then how each change works, with analogies and diagrams. Copilot's review and the CI result go to `FEEDBACK-n.md`, for the steps that fix them. The **PR** tab (next to the artifact and Changes) shows both, asks Copilot for a new review, reruns one check or all of them, and lists every unresolved review comment (anyone's) to fix in one Implement step. Hover a line in a diff and click its **+** to comment on it (drag for several lines), then **Send to Implement** (or Discuss) with all your comments. The task header says when the branch falls behind its base, with a **Rebase** button. **Mark ready** when you are.

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

- **Agents** live in `~/.herd/agents.toml` (Settings → Agents) and are all yours to change: what they read and write is in their instructions; boxes set what herd does around them (commit → push → open the PR, Revert, Rerun, sister tasks…). The first one starts every task. Only **Copilot** is built in: herd runs it, since it works with GitHub; its settings choose the model and instructions of the explanation Claude writes in `COPILOT-n.md`. Rules in Settings → Context go into every session. What herd adds around an agent's instructions (the protocol, diagram and sister-task rules) and the guidance of the PR tab's fix steps are editable in Settings → Context → Prompts; **Preview full context** on an agent shows everything a step gets: the system prompt, the repository's CLAUDE.md, the command and CONTEXT.md (your own `~/.claude/CLAUDE.md` is excluded from agent sessions).
- **One repo for all issues?** Set it as the **Issues repository** in Settings → Projects and tick *Issues come from …* on each project: New task lists your issues from there, and each project's PR closes them there (`Closes owner/repo#123`).
- **The issue** is copied, comments included, to `ISSUE.md` when the task is created and before each run of the first agent (or with **Issue** in the task header).
- **Run files** (TASK.md, ISSUE.md, PLAN-n.md, REVIEW-n.md…) live in `~/.herd/runs/<project>/<task>/`, one click from the Steps panel, with herd's own record of the task (`.herd-task.json`: its steps and state). The rest of `~/.herd` is settings (`config.toml` with your shortcuts under `[keymap]`, `agents.toml`, `context/`), what herd generates for Claude Code (`plugin/`: one command per agent; `hooks.json`, `endpoint`), the sockets of running steps (`sessions/`), what they did while herd was closed (`spool/`, emptied at start) and UI state (`state.json`).
- **Diagrams:** agents draw them with the [archify](https://github.com/tt-a1i/archify) skill (install it in `~/.claude/skills`): Plan draws the current flow and the new one, to compare at a glance. They appear inside the artifact; **Open full size** lists every diagram of the task. Without archify, agents fall back to mermaid.
- **Several repos?** Plan or Implement (any agent with *Can create sister tasks* in Settings → Agents) ends its artifact with a `## Sister tasks` section (after a `---` line), one `### <repo>` each; Next step then offers, in one click, a **sister task** in each: same issue, its own worktree, plan and PR, linked from the task header.
- **Edits outside the worktree** by mistake are spotted: Next step warns before any step that commits.
- **Skip a step:** its ✓ and ✕ buttons in the Steps panel end it at once as succeeded or failed (herd doesn't commit for it), and the task moves on from there.
- **Quitting herd** keeps working steps alive: the next herd reattaches them, and catches up on what they did while it was closed.
- **Shortcuts:** Alt+E and Alt+A hide the side panels, Esc closes pop-ups.

## 🗺️ Where things are

```
main.go, app.go      Wails entry
internal/app         tasks, steps, hooks, commit, push
internal/runner      one PTY per step  ·  holder keeps it alive across restarts
internal/github      PRs, CI checks, Copilot reviews, issues
internal/…           store (a JSON file per task), settings, gitops, hooks, artifacts, zed
frontend/            Vue 3 + xterm.js + mermaid; archify pages in a sandboxed iframe
```

Design notes: `Agentflow implementation plan.md`.
