package settings

import "strings"

// Prompt is a text herd adds to what agents get, beside their own
// instructions. Settings > Context > Prompts edits it; config.toml's
// [prompts] keeps only the ones that differ from Default.
type Prompt struct {
	ID      string `json:"id"`
	Name    string `json:"name"`
	Where   string `json:"where"` // where herd uses it
	Vars    string `json:"vars"`  // what herd replaces in it
	Default string `json:"default"`
	Text    string `json:"text"` // the text in use: the user's, else Default
}

// Prompt ids.
const (
	PromptProtocol  = "protocol"
	PromptDiagrams  = "diagrams"
	PromptSisters   = "sisters"
	PromptRebase    = "rebase"
	PromptFixChecks = "fixChecks"
	PromptFeedback  = "fixFeedback"
	PromptNotes     = "diffNotes"
)

// DefaultPrompts lists every prompt, in the order Settings shows them.
func DefaultPrompts() []Prompt {
	return []Prompt{
		{ID: PromptProtocol, Name: "Protocol", Where: "Every agent's command, before its instructions", Vars: "{artifact}: the agent's artifact, e.g. PLAN-{n}.md", Default: `Text after ` + "`--`" + ` in the arguments is guidance from the user for this step; follow it.
The *herd step* block in your system prompt gives the task, the step number ` + "`{n}`, `{sha}`, `{issue}`" + ` and the exact artifact path.

## Protocol

1. Read ` + "`TASK.md`" + ` and then ` + "`CONTEXT.md`" + ` in the run directory (` + "`$HERD_RUN_DIR`" + `) before anything else.
2. Work in the current directory: it is the task's own git worktree.
3. Never commit or push: herd does it.
   Skills you can use whenever they help: context7 (current docs of libraries and frameworks) and archify (diagrams, see below).
4. Write your artifact (` + "`{artifact}`" + `) in the run directory. It starts with YAML front matter; when the step is finished it contains ` + "`status: done`" + ` (it succeeded) or ` + "`status: failed`" + ` (it did not, and the artifact says why).
   Only add that line when the step is truly finished. If you need the user, ask in the terminal and stop.`},
		{ID: PromptDiagrams, Name: "Diagrams", Where: "Every agent's command, after its instructions", Vars: "{stem}: the artifact's name without .md, e.g. PLAN-{n}", Default: `## Diagrams

Every diagram is an archify page: run the archify skill (Skill tool), never draw it by hand. herd shows the page inside your artifact, and the reader can open it full size.
- Save it in the run directory as ` + "`{stem}.html`" + ` (` + "`{stem}-<topic>.html`" + ` for more than one); keep archify's working files in ` + "`$HERD_RUN_DIR/.archify/`" + ` (and pass ` + "`--out-dir $HERD_RUN_DIR/.archify/<name>`" + ` to finalize so its reports go there too), never in the worktree.
- Embed it in your artifact on its own line: ` + "`![<what it shows>]({stem}.html)`" + `.
- Draw what the reader needs to understand, not every detail; mark what changes.
- Write a ` + "```mermaid" + ` fence only when your instructions ask for mermaid, or when the archify skill is not in your skill list.`},
		{ID: PromptSisters, Name: "Sister tasks", Where: "The command of each agent with Can create sister tasks, at the end. Keep the ## Sister tasks and ### <repository> headings: Next step reads them", Vars: "{artifact}: the agent's artifact", Default: `## Sister tasks

When the work also needs changes in other repositories, do only this repository's part, then end {artifact} with a "---" line and a "## Sister tasks" section:
one "### <repository folder name>" per repository, then what that part must do, written as the description of its task. herd offers to create each one in a click.
Leave out repositories that already have a sister task (listed in the herd step block).`},
		{ID: PromptRebase, Name: "Rebase", Where: "The guidance of Rebase with Implement: in the PR tab when the PR conflicts with its base, and in the task header when the branch is behind it", Vars: "{base}: the base branch, {behind}: how many of its commits the branch lacks", Default: `Rebase this branch on origin/{base}, which has {behind} commits it lacks. git fetch origin {base}, then git rebase origin/{base}; resolve each conflict keeping what both sides meant, and continue with git rebase --continue (the only commits allowed in this step). Run the tests of the files you resolved. Do not push: Commit and push does it. List the conflicts and how you resolved them, or say there were none.`},
		{ID: PromptFixChecks, Name: "Fix CI checks", Where: "The guidance of Fix with Implement, in the PR tab's CI checks", Vars: "{pr}: the PR number, {checks}: the failing checks, {links}: their links", Default: `Fix the failing CI checks of PR #{pr}: {checks}. Read their logs (gh run view <run id> --log-failed; the run id is in each check's link: {links}), fix the cause in the code, or the test when the test is what is wrong, and run those tests locally when you can.`},
		{ID: PromptFeedback, Name: "Fix review comments", Where: "The guidance of Fix with Implement, in the PR tab's Review comments", Vars: "{pr}: the PR number, {comments}: the chosen comments, with their file, line and replies", Default: `Address these review comments on PR #{pr}. For each one, fix the code when it is right; when it is not, or when it asks a question, say so in your artifact with the answer to post. List in your artifact which comment each change answers.

{comments}`},
		{ID: PromptNotes, Name: "Diff comments", Where: "The guidance of the step that Send to starts, from your comments on the diff (Changes)", Vars: "{comments}: your comments, each with its file, lines and code", Default: `Address my comments on the diff, one by one. Each gives the file, the lines and their code, then what I want. List in your artifact what you changed for each.

{comments}`},
	}
}

// Fill replaces each {name} of vars in text.
func Fill(text string, vars map[string]string) string {
	for k, v := range vars {
		text = strings.ReplaceAll(text, "{"+k+"}", v)
	}
	return text
}
