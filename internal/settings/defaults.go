package settings

// DefaultConfig is written to config.toml on first launch.
func DefaultConfig() Config {
	return Config{
		Appearance: Appearance{
			Theme:        "dark",
			UIFont:       "Geist",
			UIFontSize:   13,
			TermFont:     "Geist Mono",
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

// DefaultAgents is the default pipeline: Plan, Implement or Discuss, Review,
// Test, Commit and push, then Copilot (built in). Every step ends with
// status: done (succeeded) or status: failed.
func DefaultAgents() []Agent {
	return []Agent{
		{
			ID: "plan", Name: "Plan", Command: "/herd:plan {task} -- {guidance}", Model: "sonnet", Trigger: "manual",
			Writes: "PLAN-{n}.md", Sisters: true, Choose: true, Next: []string{"impl", "discuss"}, OnFail: []string{"plan"},
			Instructions: `Read TASK.md, and ISSUE.md when the task has a GitHub issue (herd copied it there, comments included).
Explore the repo and decide an approach. Do not edit code.
Write PLAN-{n}.md with these sections, in this order:
TLDR: two or three sentences, right after the front matter, saying what is wrong and how it will be fixed. Then ELI5: a short paragraph explaining the same thing as you would to a five-year-old, with an everyday metaphor (no jargon, no file or function names). Write both with the humanizer skill so they read naturally; do not use humanizer anywhere else.
1. Goal: what is wrong or missing today and what will be true once it is done, in plain words that someone new to this code understands.
2. Current flow: PLAN-{n}-current.html, an archify diagram of how the code works today, drawn from the code with source evidence, the part that goes wrong marked. Embed it: ![Current flow](PLAN-{n}-current.html).
3. New flow: PLAN-{n}-new.html, the same flow with the fix applied and what changes marked, laid out like the current one so the two compare at a glance. Embed it: ![New flow](PLAN-{n}-new.html).
4. Options, only when there is a real choice (designs, libraries, where the code goes): each with its advantages and disadvantages and its estimated size (about +N / -M lines), then the one you recommend.
5. Files: a table with one row per file to change and a sentence or two on what changes there and why. Under it, one line estimating the size of the change, e.g. "Estimated change: about +120 / -30 lines" (for the recommended option when there are several).
6. Tests: the behaviours to verify once it is built, as a short list (main path, edge cases, what must not change).
Keep PLAN-{n}.md short, intuitive and easy to read.
End with status: done, or status: failed if the task cannot be done as described (say why).`,
		},
		{
			ID: "impl", Name: "Implement", Command: "/herd:implement {task} -- {guidance}", Model: "opus", Trigger: "manual",
			Writes: "IMPLEMENT-{n}.md", ShowDiff: true, Sisters: true, Revert: true, OnFail: []string{"impl", "discuss", "plan"},
			Instructions: `Read the newest PLAN-n.md (PLAN.md in older tasks), and the newest REVIEW-n.md, TEST-n.md, COPILOT-n.md and DISCUSS-n.md in the run directory when they exist (older ones were handled by earlier steps).
Implement the plan's New flow and Files in this worktree. When the plan lists Options, take the one the guidance picks, else the recommended one.
When the task already has a pull request, also fix its failing CI checks (gh pr checks) and the comments in the newest FEEDBACK-n.md. Check each comment against the code first and change the code only when its concern is real and worth fixing; when it is wrong or unnecessary, reject it: leave the code as it is. List the ids of the comments you handled in the front matter of IMPLEMENT-{n}.md (the number after "comment" in FEEDBACK-n.md or in your guidance), comma-separated: fixed: <ids> and rejected: <ids>. herd marks the rejected ones in the PR tab, where the user resolves them.
Do not write or change tests: Test adds them once the review passes.
Run the existing tests that cover what you changed, and the build or type-check.
Do not commit: Commit and push commits once Review and Test pass.
Write IMPLEMENT-{n}.md with these sections:
TLDR: two or three sentences, right after the front matter, saying what changed and why. Then ELI5: a short paragraph explaining the same thing as you would to a five-year-old, with an everyday metaphor (no jargon, no file or function names). Write both with the humanizer skill so they read naturally; do not use humanizer anywhere else.
1. What changed: an intuitive explanation of the change and how each part of the code had to be updated for it, following the flow. When the plan lists Options, start with the option taken and why: the user's guidance, else the plan's recommendation.
2. Files: a table of the files actually changed, one row each, with what changed in it.
3. Review comments, only when you handled some: one row per comment (its id and title), fixed or rejected, and why (for a rejection: why it is wrong or unnecessary).
4. Notes for the reviewer, only when there are any: deviations from the plan, what to look at closely, checks that failed or could not run.
Keep IMPLEMENT-{n}.md short, intuitive and easy to read.
End with status: done, or status: failed if you could not implement it (say what blocks).`,
		},
		{
			ID: "discuss", Name: "Discuss", Command: "/herd:discuss {task} -- {guidance}", Model: "opus", Trigger: "manual",
			Writes: "DISCUSS-{n}.md", ShowDiff: true, Sisters: true, Revert: true, Next: []string{"review", "impl"}, OnFail: []string{"discuss", "impl"},
			Instructions: `Read TASK.md, the newest PLAN-n.md (PLAN.md in older tasks), and the newest REVIEW-n.md, TEST-n.md, COPILOT-n.md and FEEDBACK-n.md in the run directory when they exist, and look at the branch's changes (git diff origin/{base}...HEAD, git status).
This step is a conversation led by the user: they may want to understand something, explore an idea, or change code. Do not start working on your own.
After reading, say in two or three lines where the task stands, then wait for the user's questions. When there is guidance, start from it.
Change code only when the user asks, one change at a time, and explain each briefly. Do not write or change tests unless asked, and do not commit: Commit and push commits.
If you changed code, run the existing tests that cover it, and the build or type-check, before finishing.
Only when the user says the discussion is over, write DISCUSS-{n}.md: a short summary of what you discussed and concluded, or whatever the user asked you to write. If you changed code, add What changed and Files, so Review can check them; when the plan lists Options and you implemented one, say which and why.
Keep DISCUSS-{n}.md short, intuitive and easy to read.
End with status: done, or status: failed if the user stops the work (say why).`,
		},
		{
			ID: "review", Name: "Review", Command: "/herd:review {task}", Model: "opus", Trigger: "after:impl",
			Writes: "REVIEW-{n}.md", Rerun: true, Next: []string{"test", "impl", "discuss"}, OnFail: []string{"impl", "discuss"},
			Instructions: `Review the uncommitted changes in this worktree (git status, git diff HEAD) against the newest PLAN-n.md (its New flow and Files; PLAN.md in older tasks) and the newest IMPLEMENT-n.md or DISCUSS-n.md.
Write REVIEW-{n}.md with the findings, most important first. End with status: done if the changes are ready, or status: failed if they need changes.
Never edit code, and do not commit: Commit and push commits once the tests pass.`,
		},
		{
			ID: "test", Name: "Test", Command: "/herd:test {task} -- {guidance}", Model: "sonnet", Trigger: "manual",
			Writes: "TEST-{n}.md", ShowDiff: true, Next: []string{"push", "impl", "discuss"}, OnFail: []string{"impl", "discuss"},
			Instructions: `Read the newest PLAN-n.md (its Tests section lists the behaviours to verify; PLAN.md in older tasks), the IMPLEMENT and DISCUSS files and the REVIEW files.
Write tests for the reviewed, uncommitted changes (git status, git diff HEAD), following the repo's test conventions:
the cases in the plan's Tests, the edge cases, and anything the reviews flagged.
Before writing, look at the tests this branch already added or changed (git diff origin/{base} -- on the test files): an earlier Test wrote them,
and the code may have changed since. Update or remove the ones that no longer match the code. Never change tests that already exist on {base}.
Run them. Change only tests, never the code under test: if a test shows a bug, keep it failing and explain it.
Write TEST-{n}.md with Tests added, How to run and Results.
Do not commit: Commit and push commits once the tests pass.
End with status: done when the tests pass, or status: failed when one shows a bug.`,
		},
		{
			ID: "push", Name: "Commit and push", Command: "/herd:push {task}", Model: "haiku", Trigger: "manual",
			Writes: "PUSH.md", Commit: true, Push: true, OpenPR: true, IgnoreRules: true, Next: []string{"impl", "discuss"},
			Instructions: PushInstructions,
		},
		{
			ID: CopilotID, Name: "Copilot", Trigger: "after:push", Writes: "COPILOT-{n}.md", Builtin: true, Model: "opus", Instructions: CopilotInstructions,
			Next: []string{"impl", "discuss"}, OnFail: []string{"impl", "discuss"},
		},
	}
}

// PushInstructions: the default PR agent asks for the PR's type, scope and
// title in a form, then writes the commit message and the PR in PUSH.md.
const PushInstructions = `First bring the branch up to date: git fetch origin {base}; when origin/{base} has commits this branch lacks,
run git rebase --autostash origin/{base}. If the rebase stops on a conflict, run git rebase --abort,
list the conflicting files and end with status: failed.
Then ask the user in one form, with the AskUserQuestion tool: the type of change (feat, fix, perf, test, refactor, chore),
the scope (the part of the code it touches) and the title. Suggest a scope and a title from the changes.
Write PUSH.md: the title line "<type>(<scope>): <title> #{issue}", then a short body saying what changed and why, from the newest
PLAN-n.md's Goal and the What changed of the IMPLEMENT and DISCUSS files, ending with "Closes #{issue}". When the herd step block
says the issue lives in another repository, write "<that repository>#{issue}" instead of "#{issue}"; when it says this is a sister task, write "Part of" instead of "Closes".
In the body, under a "## Flow" heading, draw the flow before and after the change as two mermaid flowcharts
(mermaid code blocks: GitHub renders them, not archify pages), based on the current and new flow diagrams of the newest PLAN-n.md
and updated with what was actually implemented. Keep each to the steps that matter (about 10 nodes) and mark what changed
in the node text, e.g. "(new)" or "(changed)": no colors, fills, classDef or style lines, which become unreadable in GitHub's dark or light theme. Leave the section out when the change has no flow to show (docs, config, a one-line fix).
When the newest PLAN-n.md lists Options, add a "## Choices" section before Flow: for each choice, the option taken, the ones set aside
in a few words, and why (the guidance in the herd step block, the IMPLEMENT and DISCUSS files, else the plan's recommendation).
For a LOCAL task (no issue), leave the issue references out.
Do not commit or push: herd commits every change with PUSH.md's title line and body as the message, pushes, and opens
(or updates) the draft PR with them; then Copilot reviews it.
End with status: done.`

// DefaultKeymap binds only what is needed every day: searching tasks, a new
// task, hiding the side panels, the todo lists, and Escape for popups. config.toml's [keymap]
// can bind any other action the Shortcuts page lists (scope, tab1…tab9, …). "Mod" is
// Cmd on macOS and Ctrl elsewhere; in a terminal, Ctrl+<letter> stays the
// terminal's.
func DefaultKeymap() Keymap {
	return Keymap{
		"palette":     {"Mod+F"},
		"newTask":     {"Mod+N"},
		"toggleLeft":  {"Alt+E"},
		"toggleRight": {"Alt+A"},
		"todos":       {"Alt+T"},
		"close":       {"Escape"},
	}
}
