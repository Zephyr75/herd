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
			Writes: "PLAN.md", Next: []string{"impl"}, OnFail: []string{"plan"},
			Instructions: `Read the task source: the GitHub issue, the description, or both.
Explore the repo and decide an approach.
Write PLAN.md with Goal, Approach (a mermaid flowchart), Files and Out of scope.
When there is a real choice to make (designs, libraries, where the code goes), add Options: each one with
its advantages and disadvantages, then the one you recommend. When the way to do it is clear, skip Options.
When the work also needs changes in other repositories, plan only this repository's part and end PLAN.md
with an "## Other repositories" section: one "- <repository folder name>: <what that part must do>" line each
(herd offers a sister task for each). A sister task's plan lists only repositories not yet covered.
Do not edit code. End with status: done, or status: failed if the task cannot be done as described (say why).`,
		},
		{
			ID: "impl", Name: "Implement", Command: "/herd:implement {task} -- {guidance}", Model: "opus", Trigger: "manual",
			Writes: "IMPLEMENT-{n}.md", ShowDiff: true, OnFail: []string{"impl", "plan"},
			Instructions: `Read PLAN.md, and the newest REVIEW-n.md, TEST-n.md and COPILOT-n.md in the run directory when they exist (older ones were handled by earlier Implements).
Implement the plan in this worktree. When PLAN.md lists Options, take the one the guidance picks, else the recommended one.
When the task already has a pull request, also fix its failing CI checks (gh pr checks) and the comments in the newest COPILOT-n.md.
Do not write or change tests: Test adds them once the review passes.
Run the existing tests that cover what you changed, and the build or type-check.
Do not commit: Commit and push commits once Review and Test pass.
Write IMPLEMENT-{n}.md with Changes, Checks run and Notes for the reviewer.
End with status: done, or status: failed if you could not implement it (say what blocks).`,
		},
		{
			ID: "review", Name: "Review", Command: "/herd:review {task}", Model: "opus", Trigger: "after:impl",
			Writes: "REVIEW-{n}.md", Next: []string{"test", "impl"}, OnFail: []string{"impl"},
			Instructions: `Review the uncommitted changes in this worktree (git status, git diff HEAD) against PLAN.md.
Write REVIEW-{n}.md with the findings, most important first. End with status: done if the changes are ready, or status: failed if they need changes.
Never edit code, and do not commit: Commit and push commits once the tests pass.`,
		},
		{
			ID: "test", Name: "Test", Command: "/herd:test {task} -- {guidance}", Model: "sonnet", Trigger: "manual",
			Writes: "TEST-{n}.md", ShowDiff: true, Next: []string{"push", "impl"}, OnFail: []string{"impl"},
			Instructions: `Read PLAN.md, the IMPLEMENT files and the REVIEW files.
Write tests for the reviewed, uncommitted changes (git status, git diff HEAD), following the repo's test conventions:
the main paths, the edge cases, and anything the reviews flagged.
Run them. Change only tests, never the code under test: if a test shows a bug, keep it failing and explain it.
Write TEST-{n}.md with Tests added, How to run and Results.
Do not commit: Commit and push commits once the tests pass.
End with status: done when the tests pass, or status: failed when one shows a bug.`,
		},
		{
			ID: "push", Name: "Commit and push", Command: "/herd:push {task}", Model: "haiku", Trigger: "manual",
			Writes: "PR.md", Push: true, IgnoreRules: true, Next: []string{"impl"},
			Instructions: `First bring the branch up to date: git fetch origin {base}; when origin/{base} has commits this branch lacks,
run git rebase --autostash origin/{base}. If the rebase stops on a conflict, run git rebase --abort,
list the conflicting files and end with status: failed.
Write PR.md: a title line, then a short body saying what changed and why, from PLAN.md and the IMPLEMENT files.
Do not commit or push: herd commits every change with this title and body, pushes, opens the draft PR
(adding the issue number to its title and Closes #{issue} to its body) and collects the Copilot review.
End with status: done.`,
		},
	}
}

// DefaultKeymap binds only what is needed every day: hiding the side panels,
// and Escape for popups. keymap.json can bind any other action the Shortcuts
// page lists (palette, newTask, tab1…tab9, …). "Mod" is Cmd on macOS and Ctrl
// elsewhere.
func DefaultKeymap() Keymap {
	return Keymap{
		"toggleLeft":  {"Alt+E"},
		"toggleRight": {"Alt+A"},
		"close":       {"Escape"},
	}
}
