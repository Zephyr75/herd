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
		Notifications:   true,
		MaxTabs:         5,
		BranchPrefix:    "ar-",
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

// DefaultAgents is the default pipeline: Plan, Implement, Review, Test, Push.
// Every step ends with status: done (succeeded) or status: failed.
func DefaultAgents() []Agent {
	return []Agent{
		{
			ID: "plan", Name: "Plan", Command: "/herd:plan {task} -- {guidance}", Model: "sonnet", Trigger: "manual",
			Writes: "PLAN.md",
			Instructions: `Read the task source: the GitHub issue, the description, or both.
Explore the repo and decide an approach.
Write PLAN.md with Goal, Approach (a mermaid flowchart), Files and Out of scope.
When there is a real choice to make (designs, libraries, where the code goes), add Options: each one with
its advantages and disadvantages, then the one you recommend. When the way to do it is clear, skip Options.
Do not edit code. End with status: done, or status: failed if the task cannot be done as described (say why).`,
		},
		{
			ID: "impl", Name: "Implement", Command: "/herd:implement {task} -- {guidance}", Model: "opus", Trigger: "manual",
			Writes: "IMPLEMENT-{n}.md",
			Instructions: `Read PLAN.md, and FEEDBACK.md when it exists.
Implement the plan in this worktree. When PLAN.md lists Options, take the one the guidance picks, else the recommended one.
When the task already has a pull request, also fix its failing CI checks (gh pr checks) and the review comments in FEEDBACK.md.
Do not write or change tests: the Test agent adds them once the review passes.
Run the existing tests that cover what you changed, and the build or type-check.
Do not commit: herd commits your changes when the Review step ends.
Write IMPLEMENT-{n}.md with Changes, Checks run and Notes for the reviewer.
End with status: done, or status: failed if you could not implement it (say what blocks).`,
		},
		{
			ID: "review", Name: "Review", Command: "/herd:review {task}", Model: "opus", Trigger: "after:impl",
			Writes: "REVIEW-{n}.md", Commit: true,
			Instructions: `Review the uncommitted changes in this worktree (git status, git diff HEAD) against PLAN.md.
List findings, most important first, and append them to FEEDBACK.md.
Write REVIEW-{n}.md. End with status: done if the changes are ready, or status: failed if they need changes.
Never edit code: herd commits the reviewed changes when you finish.`,
		},
		{
			ID: "test", Name: "Test", Command: "/herd:test {task} -- {guidance}", Model: "sonnet", Trigger: "after:review",
			Writes: "TEST-{n}.md", Commit: true,
			Instructions: `Read PLAN.md, the IMPLEMENT files and the REVIEW files.
Write tests for the reviewed changes (git log and git show on this branch), following the repo's test conventions:
the main paths, the edge cases, and anything the reviews flagged.
Run them. Change only tests, never the code under test: if a test shows a bug, keep it failing and explain it.
Write TEST-{n}.md with Tests added, How to run and Results.
End with status: done when the tests pass, or status: failed when one shows a bug.`,
		},
		{
			ID: "push", Name: "Push", Command: "/herd:push {task}", Model: "haiku", Trigger: "manual",
			Writes: "PR.md", Push: true, IgnoreRules: true,
			Instructions: `Write PR.md: a title line, then a body summarising PLAN.md and the IMPLEMENT files.
Add Closes #{issue} when the task has an issue.
Do not push: herd pushes, opens the PR and collects the Copilot review. End with status: done.`,
		},
	}
}

// DefaultKeymap lists every action and its bindings, following Zed's Linux
// keymap where an equivalent exists (Alt+1 for tab 1, Ctrl+Alt+O for recent
// projects, Ctrl+PageDown for the next tab). "Mod" is Cmd on macOS and Ctrl
// elsewhere.
func DefaultKeymap() Keymap {
	return Keymap{
		"palette":       {"Mod+P"},
		"scope":         {"Mod+Alt+O"},
		"newTask":       {"Mod+N"},
		"runPrimary":    {"Mod+Enter"},
		"toggleArchive": {"Mod+Shift+A"},
		"nextTask":      {"Alt+ArrowDown"},
		"prevTask":      {"Alt+ArrowUp"},
		"tab1":          {"Alt+1"},
		"tab2":          {"Alt+2"},
		"tab3":          {"Alt+3"},
		"tab4":          {"Alt+4"},
		"tab5":          {"Alt+5"},
		"tab6":          {"Alt+6"},
		"tab7":          {"Alt+7"},
		"tab8":          {"Alt+8"},
		"tab9":          {"Alt+9"},
		"closeTab":      {"Mod+W"},
		"nextTab":       {"Ctrl+PageDown", "Ctrl+Tab"},
		"prevTab":       {"Ctrl+PageUp", "Ctrl+Shift+Tab"},
		"openWorktree":  {"Mod+Shift+E"},
		"openDiff":      {"Mod+Shift+D"},
		"toggleLeft":    {"Mod+Shift+B"}, // Zed's Ctrl+B is Claude Code's "run in background"
		"toggleRight":   {"Mod+Alt+B"},
		"settings":      {"Mod+,"},
		"close":         {"Escape"},
	}
}
