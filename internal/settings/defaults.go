package settings

// DefaultConfig is written to config.toml on first launch.
func DefaultConfig() Config {
	return Config{
		Appearance: Appearance{
			Theme:        "dark",
			UIFont:       "IBM Plex Sans",
			UIFontSize:   13,
			TermFont:     "IBM Plex Mono",
			TermFontSize: 12.5,
		},
		ZedCommand:      "zed",
		ClaudeCommand:   "claude",
		MaxLiveSessions: 6,
		CopilotTimeout:  30,
		Context:         map[string]ContextMeta{},
	}
}

// DefaultGeneralContext is context/general.md on first launch.
const DefaultGeneralContext = `# General context
- Match the style already in the repo; no new dependencies without asking.
- Never commit or push: herd does it.
- Artifacts go to $HERD_RUN_DIR and start with YAML front matter.
- Prefer small diffs; explain trade-offs in the artifact, not in code comments.
`

// DefaultAgents is the default pipeline: Plan, Implement, Review, Push.
func DefaultAgents() []Agent {
	return []Agent{
		{
			ID: "plan", Name: "Plan", Command: "/herd:plan {task} -- {guidance}", Model: "opus", Trigger: "manual",
			Writes: "PLAN.md", Done: "status: done", Permissions: "readonly",
			Allowed: []string{"Read", "Grep", "Glob", "Bash(gh issue view:*)", "Edit(/{run}/**)", "Write(/{run}/**)"},
			Denied:  []string{"Edit(/{worktree}/**)"},
			Instructions: `Read the task source: the GitHub issue, the description, or both.
Explore the repo and decide an approach.
Write PLAN.md with Goal, Approach (a mermaid flowchart), Files and Out of scope.
Do not edit code. End with front matter status: done.`,
		},
		{
			ID: "impl", Name: "Implement", Command: "/herd:implement {task} -- {guidance}", Model: "opus", Trigger: "manual",
			Writes: "IMPLEMENT-{n}.md", Done: "status: done", Permissions: "acceptEdits", Commit: true,
			Allowed: []string{"Read", "Edit", "Write", "Bash(go test:*)", "Bash(pnpm test:*)", "Bash(npm test:*)"},
			Denied:  []string{"Bash(git commit:*)", "Bash(git push:*)"},
			Instructions: `Read PLAN.md, and FEEDBACK.md when it exists.
Implement the plan in this worktree and run the relevant tests.
Do not commit: herd commits when you finish.
Write IMPLEMENT-{n}.md with Changes, Tests and Notes for the reviewer. End with status: done.`,
		},
		{
			ID: "review", Name: "Review", Command: "/herd:review {task} {sha}", Model: "sonnet", Trigger: "after:impl",
			Writes: "REVIEW-{n}.md", Done: "verdict: approve | changes", Permissions: "readonly",
			Allowed: []string{"Read", "Grep", "Glob", "Bash(git show:*)", "Bash(git diff:*)", "Edit(/{run}/**)", "Write(/{run}/**)"},
			Denied:  []string{"Edit(/{worktree}/**)"},
			Instructions: `Review commit {sha} against PLAN.md.
List findings, most important first, and append them to FEEDBACK.md.
Write REVIEW-{n}.md with front matter verdict: approve or changes.
Never edit code.`,
		},
		{
			ID: "push", Name: "Push", Command: "/herd:push {task}", Model: "haiku", Trigger: "manual",
			Writes: "PR.md", Preview: "FEEDBACK.md", Done: "status: done", Permissions: "readonly", Push: true,
			Allowed: []string{"Read", "Grep", "Bash(git log:*)", "Edit(/{run}/**)", "Write(/{run}/**)"},
			Denied:  []string{"Bash(git push:*)"},
			Instructions: `Write PR.md: a title line, then a body summarising PLAN.md and the IMPLEMENT files.
Add Closes #{issue} when the task has an issue.
Do not push: herd pushes, opens the PR and collects the Copilot review.`,
		},
	}
}

// DefaultKeymap lists every action and its bindings. "Mod" is Cmd on macOS
// and Ctrl elsewhere.
func DefaultKeymap() Keymap {
	return Keymap{
		"palette":       {"Mod+P"},
		"scope":         {"Mod+O"},
		"newTask":       {"Mod+N"},
		"runPrimary":    {"Mod+Enter"},
		"toggleArchive": {"Mod+Shift+A"},
		"nextTask":      {"Alt+ArrowDown"},
		"prevTask":      {"Alt+ArrowUp"},
		"tab1":          {"Mod+1"},
		"tab2":          {"Mod+2"},
		"tab3":          {"Mod+3"},
		"tab4":          {"Mod+4"},
		"tab5":          {"Mod+5"},
		"tab6":          {"Mod+6"},
		"tab7":          {"Mod+7"},
		"tab8":          {"Mod+8"},
		"tab9":          {"Mod+9"},
		"closeTab":      {"Mod+W"},
		"openWorktree":  {"Mod+Shift+E"},
		"openDiff":      {"Mod+Shift+D"},
		"settings":      {"Mod+,"},
		"close":         {"Escape"},
	}
}
