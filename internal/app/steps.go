package app

import (
	"errors"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"

	"herd/internal/artifacts"
	"herd/internal/github"
	"herd/internal/gitops"
	"herd/internal/hooks"
	"herd/internal/runner"
	"herd/internal/settings"
	"herd/internal/store"
)

type launch struct {
	task    store.Task
	proj    settings.Project
	agent   settings.Agent
	step    store.Step
	prevSHA string
	mode    launchMode
}

// launchMode is how spec starts claude for a step.
type launchMode int

const (
	modeNew    launchMode = iota // a new session with the agent's prompt
	modeResume                   // --resume the step's session
	modeRerun                    // --resume it and send the agent's prompt again
)

func (l launch) placeholders(guidance string) *strings.Replacer {
	task := l.task.Issue
	if task == "" {
		task = "--local"
	}
	return strings.NewReplacer(
		"{task}", task, "{guidance}", guidance, "{n}", fmt.Sprint(l.step.N), "{sha}", l.prevSHA, "{issue}", l.task.Issue, "{base}", l.proj.BaseBranch,
	)
}

func (a *App) prompt(l launch) string {
	p := l.placeholders(l.step.Guidance).Replace(l.agent.Command)
	p = strings.TrimSpace(p)
	p = strings.TrimSpace(strings.TrimSuffix(p, "--"))
	return p
}

// stepBlock is appended to the system prompt: per-step facts the agent's
// command file refers to.
func (a *App) stepBlock(l launch, runDir, wt string) string {
	var b strings.Builder
	b.WriteString("# herd step\n")
	fmt.Fprintf(&b, "- task: %s (%s), project %s\n", l.task.ID, l.task.Title, l.proj.Name)
	fmt.Fprintf(&b, "- step: %s ({n} = %d)\n", l.agent.DisplayName(l.step.N), l.step.N)
	if l.task.Issue != "" {
		fmt.Fprintf(&b, "- {issue} = %s (%s)\n", l.task.Issue, a.issueRepo(l.task))
		ref := "#" + l.task.Issue
		if l.task.IssueRepo != "" && l.task.IssueRepo != l.proj.Repo {
			ref = l.task.IssueRepo + "#" + l.task.Issue
			fmt.Fprintf(&b, "- the issue lives in another repository: refer to it as %s\n", ref)
		}
		if l.task.Sister {
			fmt.Fprintf(&b, "- sister task: its main task closes the issue; this one's PR is only Part of %s\n", ref)
		}
	} else {
		b.WriteString("- LOCAL task: no GitHub issue\n")
	}
	if l.prevSHA != "" {
		fmt.Fprintf(&b, "- {sha} = %s (the latest commit herd made)\n", l.prevSHA)
	}
	if l.mode == modeRerun {
		b.WriteString("- rerun: you already ran this step in this conversation. The user changed the worktree since; do the step again on the current state and rewrite your artifact\n")
	}
	fmt.Fprintf(&b, "- worktree (cwd): %s, branch %s; {base} = %s (the base branch)\n", wt, branchName(l.task), l.proj.BaseBranch)
	for _, x := range a.sisters(l.task.ID) {
		xp := a.projectOf(x)
		fmt.Fprintf(&b, "- sister task %s handles the %s part of this work, in %s; its run directory %s holds every artifact of that task: keep what both share consistent with it, and change only this repository\n", x.ID, xp.Name, worktreePath(xp, x), a.runDir(xp, x))
	}
	fmt.Fprintf(&b, "- run directory: %s (TASK.md, CONTEXT.md and every artifact)\n", runDir)
	fmt.Fprintf(&b, "- your artifact: %s\n", filepath.Join(runDir, l.agent.ArtifactName(l.step.N)))
	b.WriteString("- finished when its front matter has status: done (succeeded) or status: failed (did not; say why). The next automatic agent only starts after a success\n")
	if l.task.PRNumber > 0 {
		fmt.Fprintf(&b, "- pull request: #%d %s (gh pr checks %d shows its CI checks)\n", l.task.PRNumber, l.task.PRURL, l.task.PRNumber)
	}
	for _, dir := range a.knowledgeDirs(l.proj) {
		fmt.Fprintf(&b, "- knowledge base: %s (read-only reference, search it when you need background)\n", dir)
	}
	if rules := strings.TrimSpace(a.settings.Config().Rules); rules != "" && !l.agent.IgnoreRules {
		fmt.Fprintf(&b, "\n# Rules\nAlways follow these rules:\n%s\n", rules)
	}
	return b.String()
}

// knowledgeDirs lists the existing knowledge base folders for a project's
// tasks: the general one, then the project's.
func (a *App) knowledgeDirs(p settings.Project) []string {
	var out []string
	for _, id := range []string{"general", p.ID} {
		c, ok := a.settings.Context(id)
		if !ok || c.Knowledge == "" {
			continue
		}
		dir := expandHome(c.Knowledge)
		if fi, err := os.Stat(dir); err == nil && fi.IsDir() && !slices.Contains(out, dir) {
			out = append(out, dir)
		}
	}
	return out
}

// spec builds the claude command line for a step (see launchMode).
func (a *App) spec(l launch) runner.Spec {
	runDir, wt := a.runDir(l.proj, l.task), worktreePath(l.proj, l.task)
	cfg := a.settings.Config()
	bin := strings.Fields(cfg.ClaudeCommand)
	if len(bin) == 0 {
		bin = []string{"claude"}
	}
	args := append([]string{}, bin[1:]...)
	args = append(args, "--settings", a.hooksPath(), "--plugin-dir", a.pluginDir(), "--add-dir", runDir)
	for _, dir := range a.knowledgeDirs(l.proj) {
		args = append(args, "--add-dir", dir)
	}
	for _, x := range a.sisters(l.task.ID) {
		args = append(args, "--add-dir", a.runDir(a.projectOf(x), x)) // its plan, to stay consistent
	}
	if l.agent.Model != "" {
		args = append(args, "--model", l.agent.Model)
	}
	// The agent's own rules run without asking (Settings > Agents).
	if len(l.agent.Allow) > 0 {
		args = append(args, "--allowedTools", strings.Join(l.agent.Allow, ","))
	}
	// Every session runs in auto mode; what agents may do is set by the rules.
	args = append(args, "--permission-mode", "auto")
	args = append(args, "--append-system-prompt", a.stepBlock(l, runDir, wt))
	switch l.mode {
	case modeResume:
		args = append(args, "--resume", l.step.SessionID)
	case modeRerun:
		args = append(args, "--resume", l.step.SessionID, "--", a.prompt(l))
	default:
		args = append(args, "--session-id", l.step.SessionID, "--", a.prompt(l))
	}
	env := []string{
		"HERD_URL=" + a.baseURL(), "HERD_TOKEN=" + a.token, "HERD_TASK=" + l.task.ID, "HERD_STEP=" + l.step.Key,
		"HERD_RUN_DIR=" + runDir, "HERD_N=" + fmt.Sprint(l.step.N), "HERD_SHA=" + l.prevSHA, "HERD_ISSUE=" + l.task.Issue,
		"HERD_ARTIFACT=" + filepath.Join(runDir, l.agent.ArtifactName(l.step.N)),
		"HERD_ENDPOINT=" + a.endpointPath(),
	}
	return runner.Spec{StepID: l.step.ID, Dir: wt, Env: env, Unset: inheritedSessionVars, Bin: bin[0], Args: args}
}

// inheritedSessionVars are set by Claude Code for processes it spawns. When
// herd itself was started from a Claude Code session (e.g. "! make dev"),
// passing them on makes every step a "child session": Claude Code then turns
// off transcript saving and --resume finds nothing.
var inheritedSessionVars = []string{
	"CLAUDECODE", "CLAUDE_CODE_CHILD_SESSION", "CLAUDE_CODE_SESSION_ID", "CLAUDE_CODE_SESSION_ATTENDED",
	"CLAUDE_CODE_ENTRYPOINT", "CLAUDE_CODE_EXECPATH", "CLAUDE_CODE_MESSAGING_SOCKET", "CLAUDE_CODE_MESSAGING_TOKEN",
	"CLAUDE_PID", "CLAUDE_EFFORT",
}

// claudeDir is Claude Code's config directory.
func claudeDir() string {
	if d := os.Getenv("CLAUDE_CONFIG_DIR"); d != "" {
		return d
	}
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".claude")
}

// sessionSaved reports whether Claude Code has a transcript for a session.
// It answers true when it cannot tell (no projects folder).
func sessionSaved(id string) bool {
	projects := filepath.Join(claudeDir(), "projects")
	if _, err := os.Stat(projects); err != nil {
		return true
	}
	m, _ := filepath.Glob(filepath.Join(projects, "*", id+".jsonl"))
	return len(m) > 0
}

// latestSHA returns the sha of the last commit herd made before seq.
func latestSHA(steps []store.Step, beforeSeq int) string {
	sha := ""
	for _, s := range steps {
		if s.Seq >= beforeSeq {
			break
		}
		if s.SHA != "" {
			sha = s.SHA
		}
	}
	return sha
}

// StartStep runs an agent on a task. n = 0 picks the next number for that
// agent; a retry passes the failed step's n.
func (a *App) StartStep(taskID, agentID, guidance string, n int) (StepView, error) {
	l, err := a.newStep(taskID, agentID, guidance, n, store.Running)
	if err != nil {
		return StepView{}, err
	}
	a.reap()
	if _, err := a.run.Start(a.spec(l)); err != nil {
		s := l.step
		s.State, s.EndedAt = store.Failed, time.Now()
		_ = a.db.SaveStep(s)
		a.emitTask(taskID)
		return StepView{}, fmt.Errorf("start %s: %w", l.agent.Name, err)
	}
	a.emitTask(taskID)
	return a.stepView(l.task, l.step.ID), nil
}

func (a *App) stepView(t store.Task, stepID string) StepView {
	v, _ := a.view(t)
	for _, sv := range v.Steps {
		if sv.ID == stepID {
			return sv
		}
	}
	s, _ := a.db.Step(stepID)
	return StepView{Step: s}
}

// newStep saves a new step in state and writes the run files it reads. A
// manual step still open is closed (done) by the step that follows it.
func (a *App) newStep(taskID, agentID, guidance string, n int, state string) (launch, error) {
	t, err := a.db.Task(taskID)
	if err != nil {
		return launch{}, err
	}
	if t.Archived {
		return launch{}, errors.New("restore the task before running a step")
	}
	if t.WorktreeRemoved {
		return launch{}, errors.New("the task's worktree was removed")
	}
	ag, ok := a.settings.Agent(agentID)
	if !ok {
		return launch{}, fmt.Errorf("unknown agent %q", agentID)
	}
	if ag.Builtin {
		return launch{}, fmt.Errorf("%s is run by herd: it starts by itself after the agent it follows", ag.Name)
	}
	p := a.projectOf(t)
	if _, err := os.Stat(worktreePath(p, t)); err != nil {
		return launch{}, fmt.Errorf("worktree missing: %s", worktreePath(p, t))
	}

	a.mu.Lock()
	steps, err := a.db.Steps(taskID)
	if err != nil {
		a.mu.Unlock()
		return launch{}, err
	}
	for _, s := range steps {
		if s.State == store.Running || s.State == store.Approval {
			if _, live := a.run.Get(s.ID); live {
				a.mu.Unlock()
				return launch{}, fmt.Errorf("%s is still running", agentFor(a.settings.Agents(), s.Agent).DisplayName(s.N))
			}
		}
	}
	if n <= 0 {
		for _, s := range steps {
			if s.Agent == agentID && s.N >= n {
				n = s.N + 1
			}
		}
		if n <= 0 {
			n = 1
		}
	}
	seq, err := a.db.NextSeq(taskID)
	if err != nil {
		a.mu.Unlock()
		return launch{}, err
	}
	key := fmt.Sprintf("%s-%d", agentID, n)
	for _, s := range steps {
		if s.Key == key {
			key = fmt.Sprintf("%s-%d-%d", agentID, n, seq)
		}
	}
	s := store.Step{
		ID: store.StepID(taskID, key), TaskID: taskID, Key: key, Agent: agentID, N: n, Seq: seq, State: state,
		Guidance: strings.TrimSpace(guidance), SessionID: uuid.NewString(), Model: ag.Model, StartedAt: time.Now(),
	}
	for _, x := range steps {
		if x.Agent == copilotAgent && x.State == store.Running { // stop waiting: the user moved on
			x.State, x.EndedAt, x.Completed, x.Verdict = store.Done, time.Now(), true, skipped
			_ = a.db.SaveStep(x)
		}
	}
	err = a.db.SaveStep(s)
	a.mu.Unlock()
	if err != nil {
		return launch{}, err
	}

	l := launch{task: t, proj: p, agent: ag, step: s, prevSHA: latestSHA(append(steps, s), s.Seq)}
	a.snapshot(t, s.ID) // what Revert goes back to
	if first, ok := settings.First(a.settings.Agents()); ok && first.ID == agentID && t.Issue != "" {
		if err := a.writeIssueFile(p, t); err != nil { // the issue may have changed since
			a.toast("Could not copy issue #"+t.Issue, err.Error(), true)
		}
	}
	if err := a.writeTaskFile(p, t); err != nil {
		return launch{}, err
	}
	if err := a.writeContextFile(p, t); err != nil {
		return launch{}, err
	}
	return l, nil
}

// resume reopens a step's Claude Code conversation in a new PTY.
func (a *App) resume(stepID string) error {
	s, err := a.db.Step(stepID)
	if err != nil {
		return err
	}
	if s.SessionID == "" {
		return errors.New("this step has no Claude Code session to resume")
	}
	t, err := a.db.Task(s.TaskID)
	if err != nil {
		return err
	}
	if t.WorktreeRemoved {
		return errors.New("the worktree was removed, so the session cannot be resumed")
	}
	if t.Archived {
		return errors.New("this task is archived: restore it to reopen its Claude Code sessions")
	}
	mode := modeResume
	if !sessionSaved(s.SessionID) {
		ag := agentFor(a.settings.Agents(), s.Agent)
		return fmt.Errorf("Claude Code never saved this %s session: its process ended before the first prompt was recorded, so there is nothing to resume. Use Retry %s to run the step again", ag.DisplayName(s.N), ag.DisplayName(s.N))
	}
	p := a.projectOf(t)
	ag := agentFor(a.settings.Agents(), s.Agent)
	steps, _ := a.db.Steps(t.ID)
	l := launch{task: t, proj: p, agent: ag, step: s, prevSHA: latestSHA(steps, s.Seq), mode: mode}
	_ = a.writeContextFile(p, t)
	a.reap()
	if _, err := a.run.Start(a.spec(l)); err != nil && !errors.Is(err, runner.ErrRunning) {
		return err
	}
	a.emitTask(t.ID)
	return nil
}

// updateStep applies fn under the state lock; fn returns false to skip saving.
func (a *App) updateStep(id string, fn func(*store.Step) bool) (store.Step, bool) {
	a.mu.Lock()
	defer a.mu.Unlock()
	s, err := a.db.Step(id)
	if err != nil {
		return s, false
	}
	if !fn(&s) {
		return s, false
	}
	if err := a.db.SaveStep(s); err != nil {
		return s, false
	}
	return s, true
}

func (a *App) artifactFor(s store.Step) (artifacts.Artifact, settings.Agent) {
	ag := agentFor(a.settings.Agents(), s.Agent)
	t, err := a.db.Task(s.TaskID)
	if err != nil {
		return artifacts.Artifact{}, ag
	}
	art, _ := artifacts.Read(filepath.Join(a.runDir(a.projectOf(t), t), ag.ArtifactName(s.N)))
	return art, ag
}

// artifactDone reports whether the step's artifact says it finished, and its
// verdict: store.Failure when it failed.
func (a *App) artifactDone(s store.Step) (bool, string) {
	art, _ := a.artifactFor(s)
	finished, failed := art.Outcome()
	if failed {
		return finished, store.Failure
	}
	return finished, ""
}

// onHook turns Claude Code hook events into step states.
func (a *App) onHook(ev hooks.Event) {
	if ev.Name == "PostToolUse" {
		a.recordExternal(ev)
	}
	var finish, shell bool
	var verdict string
	if ev.Name == "Stop" {
		if s, err := a.db.Step(ev.StepID); err == nil {
			finish, verdict = a.artifactDone(s)
		}
		shell = !finish && hooks.BackgroundShell(ev.PID)
	}
	s, changed := a.updateStep(ev.StepID, func(s *store.Step) bool {
		before := *s
		if ev.SessionID != "" {
			s.SessionID = ev.SessionID
		}
		switch ev.Name {
		case "SessionStart":
			if ev.Source != "resume" && ev.Source != "compact" && s.State != store.Done {
				s.State = store.Running
			}
		case "UserPromptSubmit":
			s.State = store.Running
		case "Notification":
			if ev.IsPermission() && s.State != store.Done {
				s.State = store.Approval
			}
		case "PreToolUse":
			if ev.ToolName == hooks.AskTool && s.State != store.Done {
				s.State = store.Idle // Claude asks the user in a form
			}
		case "PostToolUse":
			if s.State == store.Approval || s.State == store.Idle && ev.ToolName == hooks.AskTool {
				s.State = store.Running
			}
		case "Stop":
			if finish {
				s.State, s.EndedAt = store.Done, time.Now()
				s.Verdict = verdict
			} else if shell {
				s.State = store.Running // its next turn starts when the shell ends
			} else {
				s.State = store.Idle
			}
		}
		return *s != before
	})
	if !changed {
		return
	}
	a.notify(s)
	if s.State == store.Done {
		a.complete(s.ID)
	}
	a.emitTask(s.TaskID)
	a.emit(EvStep, map[string]string{"taskId": s.TaskID, "stepId": s.ID, "state": s.State})
}

// onExit handles a process ending.
func (a *App) onExit(stepID string, code int, killed bool) {
	defer func() {
		if s, err := a.db.Step(stepID); err == nil {
			a.emitTask(s.TaskID)
		}
	}()
	if killed {
		return
	}
	s, err := a.db.Step(stepID)
	if err != nil || s.State == store.Done || s.State == store.Failed {
		return
	}
	done, verdict := a.artifactDone(s)
	s, changed := a.updateStep(stepID, func(s *store.Step) bool {
		if s.State == store.Done || s.State == store.Failed {
			return false
		}
		s.EndedAt = time.Now()
		if done {
			s.State = store.Done
			s.Verdict = verdict
		} else {
			s.State = store.Failed
		}
		return true
	})
	if changed {
		a.notify(s)
	}
	if changed && s.State == store.Done {
		a.complete(s.ID)
	}
}

// MarkDone marks a step succeeded when the agent forgot its front matter.
func (a *App) MarkDone(stepID string) error {
	s, changed := a.updateStep(stepID, func(s *store.Step) bool {
		if s.State == store.Done {
			return false
		}
		s.State, s.EndedAt, s.Verdict = store.Done, time.Now(), "" // the user says it succeeded
		if s.Agent == copilotAgent {
			s.Verdict = skipped
		}
		return true
	})
	if changed {
		a.complete(s.ID)
		a.emitTask(s.TaskID)
	}
	return nil
}

// AllowOnce answers a pending permission prompt with its default, Yes.
func (a *App) AllowOnce(stepID string) error {
	sess, ok := a.run.Get(stepID)
	if !ok {
		return errors.New("the session is not live; open the step and answer in the terminal")
	}
	if _, err := sess.Write([]byte("\r")); err != nil {
		return err
	}
	s, changed := a.updateStep(stepID, func(s *store.Step) bool {
		if s.State != store.Approval {
			return false
		}
		s.State = store.Running
		return true
	})
	if changed {
		a.emitTask(s.TaskID)
	}
	return nil
}

// StopStep pauses a step: its Claude Code process ends and herd leaves the
// session alone (no auto-resume) until ResumeStep, e.g. while the user
// continues it with claude --resume in Zed.
func (a *App) StopStep(stepID string) error {
	s, err := a.db.Step(stepID)
	if err != nil {
		return err
	}
	a.setPaused(stepID, true)
	a.run.Stop(stepID)
	a.emitTask(s.TaskID)
	return nil
}

// RerunStep runs a finished step again in its own Claude Code conversation,
// e.g. Review after the user edited the worktree. Only the task's latest
// step can be rerun; a committing step amends its commit (see afterDone).
func (a *App) RerunStep(stepID string) error {
	s, err := a.db.Step(stepID)
	if err != nil {
		return err
	}
	t, err := a.db.Task(s.TaskID)
	if err != nil {
		return err
	}
	if t.Archived || t.WorktreeRemoved {
		return errors.New("restore the task and its worktree before rerunning a step")
	}
	ag := agentFor(a.settings.Agents(), s.Agent)
	if s.State != store.Done && s.State != store.Failed {
		return fmt.Errorf("%s has not finished", ag.DisplayName(s.N))
	}
	steps, _ := a.db.Steps(t.ID)
	if v, _ := a.view(t); !slices.ContainsFunc(v.Steps, func(x StepView) bool { return x.ID == s.ID && x.Rerun }) {
		return fmt.Errorf("%s cannot be rerun: only the task's latest step, of an agent with Rerun on, once every step finished; start a new %s instead", ag.DisplayName(s.N), ag.Name)
	}
	if !sessionSaved(s.SessionID) {
		return fmt.Errorf("Claude Code never saved the %s session", ag.DisplayName(s.N))
	}
	if ps := sessionProcs(s.SessionID); len(ps) > 0 {
		if _, live := a.run.Get(s.ID); !live {
			return a.elsewhereErr(s.ID)
		}
	}
	if sess, live := a.run.Get(s.ID); live {
		sess.Kill()
		for i := 0; i < 50 && !sess.Exited(); i++ {
			time.Sleep(100 * time.Millisecond)
		}
		if !sess.Exited() {
			return errors.New("the previous session did not exit")
		}
	}
	p := a.projectOf(t)
	_ = os.Remove(filepath.Join(a.runDir(p, t), ag.ArtifactName(s.N))) // done again only once rewritten
	s, _ = a.updateStep(s.ID, func(x *store.Step) bool {
		x.State, x.Completed, x.Verdict, x.EndedAt = store.Running, false, "", time.Time{}
		return true
	})
	a.setPaused(s.ID, false)
	_ = a.writeContextFile(p, t)
	l := launch{task: t, proj: p, agent: ag, step: s, prevSHA: latestSHA(steps, s.Seq), mode: modeRerun}
	a.reap()
	if _, err := a.run.Start(a.spec(l)); err != nil {
		a.updateStep(s.ID, func(x *store.Step) bool { x.State, x.EndedAt = store.Failed, time.Now(); return true })
		a.emitTask(t.ID)
		return err
	}
	a.emitTask(t.ID)
	return nil
}

func (a *App) paused(stepID string) bool {
	v, _ := a.db.Get("paused:" + stepID)
	return v != ""
}

func (a *App) setPaused(stepID string, on bool) {
	v := ""
	if on {
		v = "1"
	}
	_ = a.db.Set("paused:"+stepID, v)
}

// complete runs the orchestrator's actions the first time a step finishes
// with a success: commit, push, start the automatic agents. A failed step
// leaves the worktree as it is, and its actions still run if the user gets it
// to succeed in the same conversation.
func (a *App) complete(stepID string) {
	s, first := a.updateStep(stepID, func(s *store.Step) bool {
		if s.Completed || !s.Succeeded() {
			return false
		}
		s.Completed = true
		return true
	})
	if !first {
		return
	}
	a.busy.Store(s.ID, true) // until its actions and automatic next steps ran
	go func() {
		defer a.emitTask(s.TaskID)
		defer a.busy.Delete(s.ID)
		a.afterDone(s)
	}()
}

func (a *App) afterDone(s store.Step) {
	t, err := a.db.Task(s.TaskID)
	if err != nil {
		return
	}
	ag := agentFor(a.settings.Agents(), s.Agent)
	sess, _ := a.run.Get(s.ID)
	say := func(line string) {
		if sess != nil {
			sess.Inject([]byte("\r\n\x1b[36m● herd: " + line + "\x1b[0m\r\n"))
		}
	}

	if !s.Succeeded() {
		return
	}
	if ag.Commit {
		a.emitTask(t.ID)
		err := a.gitActions(t, s, ag, say)
		_ = a.db.Set(gitErrKey(s.ID), errText(err))
		a.emitTask(t.ID)
		if err != nil {
			a.toast(err.Error(), "", true)
			return // what follows needs these actions (Copilot needs the PR)
		}
	}

	for _, next := range a.settings.Agents() {
		if next.AfterAgent() != s.Agent {
			continue
		}
		if next.Builtin {
			a.runCopilot(t.ID, say)
			continue
		}
		if _, err := a.StartStep(t.ID, next.ID, "", 0); err != nil {
			a.toast("Could not start "+next.Name, err.Error(), true)
		} else {
			say(next.Name + " started automatically")
		}
	}
}

func gitErrKey(stepID string) string { return "giterr:" + stepID }

func errText(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
}

// gitActions runs the agent's git actions in order, stopping at the first
// that fails: commit (the artifact's title line and body are the message),
// push the branch, open or update the draft PR (the same title and body).
func (a *App) gitActions(t store.Task, s store.Step, ag settings.Agent, say func(string)) error {
	p := a.projectOf(t)
	wt := worktreePath(p, t)
	g := gitops.Git{Log: say}
	title, body := "", ""
	art, _ := a.artifactFor(s)
	if art.Exists {
		title, body = splitTitle(art.Body)
	}
	msg := fmt.Sprintf("[%s] %s", t.Slug, strings.ToLower(strings.ReplaceAll(ag.DisplayName(s.N), " ", "-")))
	if title != "" {
		msg = strings.TrimSpace(title + "\n\n" + strings.TrimSpace(body))
	}
	// A rerun folds the new changes into the step's own commit while it is
	// still the unpushed tip, so the step keeps one diff per cycle.
	amend := s.SHA != "" && a.git.CanAmend(a.ctx, wt, s.SHA)
	var sha string
	var err error
	if amend {
		sha, err = g.AmendAll(a.ctx, wt)
	} else {
		sha, err = g.CommitAll(a.ctx, wt, msg)
	}
	switch {
	case err != nil:
		say("commit failed: " + err.Error())
		return fmt.Errorf("herd could not commit: %w", err)
	case sha == "":
		say("nothing new to commit")
	default:
		a.updateStep(s.ID, func(x *store.Step) bool { x.SHA = sha; return true })
		say(fmt.Sprintf("committed %s %q", sha, strings.SplitN(msg, "\n", 2)[0]))
	}
	a.emitTask(t.ID)
	if !ag.Push {
		return nil
	}
	if err := g.Push(a.ctx, wt, branchName(t)); err != nil {
		say("push failed: " + err.Error())
		return fmt.Errorf("herd could not push: %w", err)
	}
	say("pushed " + branchName(t))
	if !ag.OpenPR {
		return nil
	}
	if title == "" {
		title = t.Title
	}
	n, url, err := github.GH{Log: say}.CreatePR(a.ctx, wt, p.Repo, p.BaseBranch, branchName(t), title, body)
	if err != nil {
		say("gh pr create failed: " + err.Error())
		return fmt.Errorf("herd could not open the PR: %w", err)
	}
	if t.PRNumber == n {
		say(fmt.Sprintf("updated PR #%d %s", n, url))
	} else {
		say(fmt.Sprintf("opened PR #%d %s", n, url))
		_ = a.updateTask(t.ID, func(t *store.Task) error {
			t.PRNumber, t.PRURL, t.PRState = n, url, "open"
			return nil
		})
	}
	go a.pollPRs()
	return nil
}

// runCopilot starts the Copilot step: it asks Copilot to review the task's
// PR, then waits for the review and the CI checks (see settleCopilotStep).
func (a *App) runCopilot(taskID string, say func(string)) {
	t, err := a.db.Task(taskID)
	if err != nil {
		return
	}
	if t.PRNumber > 0 {
		if err := a.gh.RequestCopilot(a.ctx, a.projectOf(t).Repo, t.PRNumber); err != nil {
			say("could not ask Copilot for a review: " + err.Error())
		} else {
			say(fmt.Sprintf("asked Copilot to review PR #%d", t.PRNumber))
		}
	}
	a.startCopilotStep(t.ID)
	go a.pollPRs()
}

// copilotAgent is the built-in step herd runs itself: it waits for Copilot's
// review and the CI checks and writes them to COPILOT-<n>.md (no Claude).
const copilotAgent = settings.CopilotID

// skipped is the Verdict of a Copilot step herd stopped waiting on.
const skipped = "skipped"

// takeCopilotReview writes a review to the waiting Copilot step, or to a new
// one when none waits (e.g. after Mark done).
func (a *App) takeCopilotReview(t store.Task, r github.Review) {
	if a.waitingCopilot(t.ID) == nil {
		a.startCopilotStep(t.ID)
	}
	a.finishCopilotStep(t, r)
}

func (a *App) startCopilotStep(taskID string) {
	a.mu.Lock()
	defer a.mu.Unlock()
	steps, err := a.db.Steps(taskID)
	if err != nil {
		return
	}
	n := 1
	for _, s := range steps {
		if s.Agent == copilotAgent && s.N >= n {
			n = s.N + 1
		}
	}
	seq, err := a.db.NextSeq(taskID)
	if err != nil {
		return
	}
	key := fmt.Sprintf("%s-%d", copilotAgent, n)
	_ = a.db.SaveStep(store.Step{ID: store.StepID(taskID, key), TaskID: taskID, Key: key, Agent: copilotAgent, N: n, Seq: seq, State: store.Running, StartedAt: time.Now()})
	a.emitTask(taskID)
}

// finishCopilotStep writes the review into the waiting Copilot step, if any:
// it fails when Copilot left comments, so Next step offers a fix.
func (a *App) finishCopilotStep(t store.Task, r github.Review) {
	s := a.waitingCopilot(t.ID)
	if s == nil {
		return
	}
	md := fmt.Sprintf("---\npr: %d\ncomments: %d\n---\n\n%s", t.PRNumber, len(r.Comments), copilotMarkdown(t.PRNumber, r.Body, r.Comments))
	_ = os.WriteFile(a.copilotPath(t, *s), []byte(md), 0o644)
	a.settleCopilotStep(t)
}

// ciGrace is how long a Copilot step waits for CI checks that never appear
// (a repository without CI) once the review is in.
const ciGrace = 5 * time.Minute

// settleCopilotStep ends the waiting Copilot step once its review is in and
// it can tell the outcome: at once when Copilot left comments, else when the
// PR's CI finished. It succeeds only when Copilot found nothing and every
// check passed; COPILOT-n.md then lists the CI result for the next step.
func (a *App) settleCopilotStep(t store.Task) {
	s := a.waitingCopilot(t.ID)
	if s == nil {
		return
	}
	path := a.copilotPath(t, *s)
	art, _ := artifacts.Read(path)
	if !art.Exists { // the review has not arrived
		return
	}
	comments := art.Get("comments") != "0"
	checks := a.checksOf(t.ID)
	ci := ciSummary(checks)
	if !comments {
		if ci == "CI running" {
			return
		}
		if fi, err := os.Stat(path); ci == "" && err == nil && time.Since(fi.ModTime()) < ciGrace {
			return
		}
	}
	failed := comments || ci == "CI failed"
	status := "done"
	if failed {
		status = "failed"
	}
	ciWord := "none"
	if ci != "" {
		ciWord = strings.TrimPrefix(ci, "CI ")
	}
	md := fmt.Sprintf("---\nstatus: %s\npr: %d\ncomments: %s\nci: %s\n---\n\n%s\n%s", status, t.PRNumber, art.Get("comments"),
		ciWord, strings.TrimSpace(art.Body), ciMarkdown(checks))
	_ = os.WriteFile(path, []byte(md), 0o644)
	done, changed := a.updateStep(s.ID, func(x *store.Step) bool {
		if x.State != store.Running {
			return false
		}
		x.State, x.EndedAt, x.Verdict = store.Done, time.Now(), ""
		if failed {
			x.Verdict = store.Failure
		}
		return true
	})
	if changed {
		a.notify(done)
		a.complete(done.ID)
		a.emitTask(t.ID)
	}
}

// ciMarkdown is the CI section of COPILOT-n.md: the failing checks first.
func ciMarkdown(cs []github.Check) string {
	var b strings.Builder
	b.WriteString("\n## CI checks\n\n")
	switch ci := ciSummary(cs); ci {
	case "":
		b.WriteString("No CI checks reported.\n")
		return b.String()
	case "CI passed":
		fmt.Fprintf(&b, "All %d checks passed.\n", len(cs))
		return b.String()
	case "CI running":
		b.WriteString("Still running when the review arrived: `gh pr checks` shows the result.\n")
	}
	for _, c := range cs {
		if c.Bucket == "fail" || c.Bucket == "cancel" {
			word := map[string]string{"fail": "failed", "cancel": "cancelled"}[c.Bucket]
			fmt.Fprintf(&b, "- **%s / %s** %s: %s\n", c.Workflow, c.Name, word, c.Link)
		}
	}
	return b.String()
}

// waitingCopilot is the task's Copilot step waiting for its review or CI.
func (a *App) waitingCopilot(taskID string) *store.Step {
	steps, _ := a.db.Steps(taskID)
	for i := range steps {
		if steps[i].Agent == copilotAgent && steps[i].State == store.Running {
			return &steps[i]
		}
	}
	return nil
}

func (a *App) copilotPath(t store.Task, s store.Step) string {
	return filepath.Join(a.runDir(a.projectOf(t), t), agentFor(a.settings.Agents(), copilotAgent).ArtifactName(s.N))
}

// splitTitle returns the first non-empty line (without #) and the rest.
func splitTitle(md string) (string, string) {
	lines := strings.Split(strings.TrimSpace(md), "\n")
	for i, l := range lines {
		if strings.TrimSpace(l) == "" {
			continue
		}
		title := strings.TrimSpace(strings.TrimLeft(strings.TrimSpace(l), "#"))
		return title, strings.TrimSpace(strings.Join(lines[i+1:], "\n")) + "\n"
	}
	return "", ""
}

// collectCopilot writes each new Copilot review of the PR to its own Copilot
// step (COPILOT-n.md), where the next step reads it; a review that
// arrives while no Copilot step waits gets a new one. The kv key holds the last review id seen.
func (a *App) collectCopilot(t store.Task) {
	p := a.projectOf(t)
	seen, _ := a.db.Get("copilot:" + t.ID)
	after, _ := strconv.ParseInt(seen, 10, 64) // "timeout" (older herd): read them all
	rs, err := a.gh.CopilotReviews(a.ctx, p.Repo, t.PRNumber, after)
	if seen == "done" && len(rs) > 0 { // older herd appended the latest already
		_ = a.db.Set("copilot:"+t.ID, strconv.FormatInt(rs[len(rs)-1].ID, 10))
		return
	}
	for _, r := range rs {
		a.takeCopilotReview(t, r)
		_ = a.db.Set("copilot:"+t.ID, strconv.FormatInt(r.ID, 10))
		a.toast("Copilot review arrived", fmt.Sprintf("PR #%d: %d comments", t.PRNumber, len(r.Comments)), false)
	}
	if err != nil {
		log.Printf("copilot reviews of PR #%d: %v", t.PRNumber, err)
	}
}

func copilotMarkdown(pr int, body string, comments []github.Comment) string {
	var b strings.Builder
	fmt.Fprintf(&b, "## Copilot review (PR #%d)\n\n", pr)
	if strings.TrimSpace(body) != "" {
		b.WriteString(strings.TrimSpace(body))
		b.WriteString("\n\n")
	}
	if len(comments) == 0 {
		b.WriteString("- No comments\n")
		return b.String()
	}
	fmt.Fprintf(&b, "### Comments (%d)\n\n", len(comments))
	for i, c := range comments {
		loc := c.Path
		if c.Line > 0 {
			loc = fmt.Sprintf("%s:%d", c.Path, c.Line)
		}
		title := "`" + loc + "`"
		if c.Title != "" {
			title = c.Title
		}
		if c.Severity != "" {
			title += " · " + c.Severity
		}
		fmt.Fprintf(&b, "#### %d. %s\n\n", i+1, title)
		if c.Title != "" {
			fmt.Fprintf(&b, "`%s`\n\n", loc)
		}
		if hunk := lastLines(c.Hunk, 6); hunk != "" {
			fmt.Fprintf(&b, "```diff\n%s\n```\n\n", hunk)
		}
		b.WriteString(strings.TrimSpace(c.Body) + "\n\n")
		for _, r := range c.Replies {
			fmt.Fprintf(&b, "> **%s** replied: %s\n\n", r.Author, strings.ReplaceAll(strings.TrimSpace(r.Body), "\n", "\n> "))
		}
	}
	return b.String()
}

// lastLines keeps the last n lines of s: the end of a diff hunk is the
// line a review comment is on.
func lastLines(s string, n int) string {
	lines := strings.Split(strings.TrimRight(s, "\n"), "\n")
	if len(lines) > n {
		lines = lines[len(lines)-n:]
	}
	return strings.TrimRight(strings.Join(lines, "\n"), " \n")
}

// reconcile fixes steps left live by a previous run: herd owns every
// process, so after a restart none of them is running any more.
func (a *App) reconcile() {
	steps, err := a.db.StepsInStates(store.Running, store.Idle, store.Approval)
	if err != nil {
		return
	}
	for _, s := range steps {
		if a.paused(s.ID) || s.Agent == copilotAgent {
			continue // the user resumes it; herd itself waits for Copilot
		}
		if _, err := a.run.Attach(s.ID); err == nil {
			continue // still running in its holder since the last herd
		}
		done, verdict := a.artifactDone(s)
		s2, changed := a.updateStep(s.ID, func(x *store.Step) bool {
			x.EndedAt = time.Now()
			if done {
				x.State = store.Done
				x.Verdict = verdict
			} else {
				x.State = store.Failed
			}
			return true
		})
		if changed && s2.State == store.Done {
			a.complete(s2.ID)
		}
	}
	// Prune disposable diff views.
	for _, p := range a.settings.Config().Projects {
		a.git.PruneViews(a.ctx, p.Path, filepath.Join(a.Home, "views", p.Name))
	}
}
