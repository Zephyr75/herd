package app

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"

	"herd/internal/artifacts"
	"herd/internal/settings"
	"herd/internal/store"
)

// A Copilot step's files, in the run directory:
//   - COPILOT-n.md: its state (front matter: status, comments, ci) and an
//     explanation of the PR's changes, written by a Claude run herd starts
//     with the step (its model and instructions: Settings > Agents > Copilot);
//   - COPILOT-n-review.md: Copilot's review and the CI checks, for the steps
//     that fix them (the PR tab shows both too);
//   - .COPILOT-n-explain.md: the explanation, until herd puts it in.

func (a *App) reviewPath(t store.Task, s store.Step) string {
	return strings.TrimSuffix(a.copilotPath(t, s), ".md") + "-review.md"
}

func (a *App) explainPath(t store.Task, s store.Step) string {
	p := a.copilotPath(t, s)
	return filepath.Join(filepath.Dir(p), "."+strings.TrimSuffix(filepath.Base(p), ".md")+"-explain.md")
}

// writeCopilot rewrites COPILOT-n.md: its front matter with set applied,
// the explanation (or where it stands), and where the review is.
func (a *App) writeCopilot(t store.Task, s store.Step, set map[string]string) {
	a.copilotMu.Lock()
	defer a.copilotMu.Unlock()
	path := a.copilotPath(t, s)
	art, _ := artifacts.Read(path)
	var b strings.Builder
	b.WriteString("---\n")
	for _, k := range []string{"status", "pr", "comments", "ci"} {
		v, ok := set[k]
		if !ok {
			v = art.Get(k)
		}
		if v != "" {
			fmt.Fprintf(&b, "%s: %s\n", k, v)
		}
	}
	b.WriteString("---\n\n")
	if ex, err := artifacts.Read(a.explainPath(t, s)); err == nil && strings.TrimSpace(ex.Body) != "" {
		b.WriteString(strings.TrimSpace(ex.Body))
	} else {
		b.WriteString("_Claude is writing an explanation of the changes…_")
	}
	review := "Copilot's review and the CI checks are in the PR tab"
	if _, err := os.Stat(a.reviewPath(t, s)); err == nil {
		review += ", and in " + filepath.Base(a.reviewPath(t, s)) + " for the steps that fix them"
	}
	fmt.Fprintf(&b, "\n\n---\n\n%s.\n", review)
	_ = os.WriteFile(path, []byte(b.String()), 0o644)
}

// explainChanges runs Claude, without a terminal, to explain the PR's changes
// in the Copilot step's COPILOT-n.md.
func (a *App) explainChanges(t store.Task, s store.Step) {
	a.writeCopilot(t, s, nil)
	p := a.projectOf(t)
	runDir, wt, file := a.runDir(p, t), worktreePath(p, t), a.explainPath(t, s)
	stem := strings.TrimSuffix(filepath.Base(a.copilotPath(t, s)), ".md")
	ag := agentFor(a.settings.Agents(), copilotAgent)
	prompt := settings.Fill(ag.Instructions, map[string]string{"pr": fmt.Sprint(t.PRNumber), "base": p.BaseBranch, "file": file}) +
		"\n\n" + settings.Fill(a.settings.Prompt(settings.PromptDiagrams), map[string]string{"stem": stem, "artifact": filepath.Base(file)})
	bin := strings.Fields(a.settings.Config().ClaudeCommand)
	if len(bin) == 0 {
		bin = []string{"claude"}
	}
	args := append([]string{}, bin[1:]...)
	if ag.Model != "" {
		args = append(args, "--model", ag.Model)
	}
	args = append(args, "-p", "--permission-mode", "auto", "--settings", a.hooksPath(), "--add-dir", runDir)
	for _, dir := range a.knowledgeDirs(p) {
		args = append(args, "--add-dir", dir)
	}
	sys := fmt.Sprintf("# herd\nYou explain the changes of the task %s (project %s) in its worktree %s. The run directory %s holds TASK.md and every artifact.", t.ID, p.Name, wt, runDir)
	if rules := strings.TrimSpace(a.settings.Config().Rules); rules != "" {
		sys += "\n\n# Rules\nAlways follow these rules:\n" + rules
	}
	args = append(args, "--append-system-prompt", sys, "--", prompt)
	ctx, cancel := context.WithCancel(a.ctx)
	defer cancel()
	cmd := exec.CommandContext(ctx, bin[0], args...)
	cmd.Dir = wt
	// No HERD_TASK: herd's hooks do nothing for this run.
	cmd.Env = append(slices.DeleteFunc(os.Environ(), func(kv string) bool {
		name, _, _ := strings.Cut(kv, "=")
		return slices.Contains(inheritedSessionVars, name) || strings.HasPrefix(name, "HERD_")
	}), "HERD_RUN_DIR="+runDir, "HERD_ARTIFACT="+file)
	out, err := cmd.CombinedOutput()
	if a.ctx.Err() != nil {
		return // herd is quitting
	}
	if ex, _ := artifacts.Read(file); strings.TrimSpace(ex.Body) == "" { // what it wrote counts, whatever its exit
		why := strings.TrimSpace(string(out))
		if err != nil {
			why = strings.TrimSpace(err.Error() + ": " + lastLines(why, 3))
		}
		_ = os.WriteFile(file, []byte(fmt.Sprintf("_herd could not write an explanation of the changes (%s)._\n", strings.ReplaceAll(why, "\n", " "))), 0o644)
	}
	if cur, err := a.db.Step(s.ID); err == nil {
		a.writeCopilot(t, cur, nil)
	}
	a.emit(EvArtifact, map[string]string{"taskId": t.ID, "file": filepath.Base(a.copilotPath(t, s))})
}
