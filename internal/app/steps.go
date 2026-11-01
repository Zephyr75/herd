package app

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
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
}

func (l launch) placeholders(guidance string) *strings.Replacer {
	task := l.task.Issue
	if task == "" {
		task = "--local"
	}
	return strings.NewReplacer(
		"{task}", task, "{guidance}", guidance, "{n}", fmt.Sprint(l.step.N), "{sha}", l.prevSHA, "{issue}", l.task.Issue,
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
	key, values := l.agent.DoneRule()
	var b strings.Builder
	b.WriteString("# herd step\n")
	fmt.Fprintf(&b, "- task: %s (%s), project %s\n", l.task.ID, l.task.Title, l.proj.Name)
	fmt.Fprintf(&b, "- step: %s ({n} = %d)\n", l.agent.DisplayName(l.step.N), l.step.N)
	if l.task.Issue != "" {
		fmt.Fprintf(&b, "- {issue} = %s (%s)\n", l.task.Issue, l.proj.Repo)
	} else {
		b.WriteString("- LOCAL task: no GitHub issue\n")
	}
	if l.prevSHA != "" {
		fmt.Fprintf(&b, "- {sha} = %s (the latest commit herd made)\n", l.prevSHA)
	}
	fmt.Fprintf(&b, "- worktree (cwd): %s, branch %s\n", wt, branchName(l.task))
	fmt.Fprintf(&b, "- run directory: %s (TASK.md, CONTEXT.md and every artifact)\n", runDir)
	fmt.Fprintf(&b, "- your artifact: %s\n", filepath.Join(runDir, l.agent.ArtifactName(l.step.N)))
	fmt.Fprintf(&b, "- done when its front matter has %s: %s\n", key, strings.Join(values, " | "))
	for _, dir := range a.knowledgeDirs(l.proj) {
		fmt.Fprintf(&b, "- knowledge base: %s (read-only reference, search it when you need background)\n", dir)
	}
	if rules := strings.TrimSpace(a.settings.Config().Rules); rules != "" {
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

// spec builds the claude command line for a step. resume selects
// --resume <session> instead of a new session with the agent's prompt.
func (a *App) spec(l launch, resume bool) runner.Spec {
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
	if l.agent.Model != "" {
		args = append(args, "--model", l.agent.Model)
	}
	// Every session runs in auto mode; what agents may do is set by the rules.
	args = append(args, "--permission-mode", "auto")
	args = append(args, "--append-system-prompt", a.stepBlock(l, runDir, wt))
	if resume {
		args = append(args, "--resume", l.step.SessionID)
	} else {
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
	t, err := a.db.Task(taskID)
	if err != nil {
		return StepView{}, err
	}
	if t.Archived {
		return StepView{}, errors.New("restore the task before running a step")
	}
	if t.WorktreeRemoved {
		return StepView{}, errors.New("the task's worktree was removed")
	}
	ag, ok := a.settings.Agent(agentID)
	if !ok {
		return StepView{}, fmt.Errorf("unknown agent %q", agentID)
	}
	p := a.projectOf(t)
	if _, err := os.Stat(worktreePath(p, t)); err != nil {
		return StepView{}, fmt.Errorf("worktree missing: %s", worktreePath(p, t))
	}

	a.mu.Lock()
	steps, err := a.db.Steps(taskID)
	if err != nil {
		a.mu.Unlock()
		return StepView{}, err
	}
	for _, s := range steps {
		if s.State == store.Running || s.State == store.Approval {
			if _, live := a.run.Get(s.ID); live {
				a.mu.Unlock()
				return StepView{}, fmt.Errorf("%s is still running", agentFor(a.settings.Agents(), s.Agent).DisplayName(s.N))
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
		return StepView{}, err
	}
	key := fmt.Sprintf("%s-%d", agentID, n)
	for _, s := range steps {
		if s.Key == key {
			key = fmt.Sprintf("%s-%d-%d", agentID, n, seq)
		}
	}
	s := store.Step{
		ID: store.StepID(taskID, key), TaskID: taskID, Key: key, Agent: agentID, N: n, Seq: seq, State: store.Running,
		Guidance: strings.TrimSpace(guidance), SessionID: uuid.NewString(), Model: ag.Model, StartedAt: time.Now(),
	}
	err = a.db.SaveStep(s)
	a.mu.Unlock()
	if err != nil {
		return StepView{}, err
	}

	l := launch{task: t, proj: p, agent: ag, step: s, prevSHA: latestSHA(append(steps, s), s.Seq)}
	if err := a.writeTaskFile(p, t); err != nil {
		return StepView{}, err
	}
	if err := a.writeContextFile(p, t); err != nil {
		return StepView{}, err
	}
	a.reap()
	if _, err := a.run.Start(a.spec(l, false)); err != nil {
		s.State, s.EndedAt = store.Failed, time.Now()
		_ = a.db.SaveStep(s)
		a.emitTask(taskID)
		return StepView{}, fmt.Errorf("start %s: %w", ag.Name, err)
	}
	a.emitTask(taskID)
	v, _ := a.view(t)
	for _, sv := range v.Steps {
		if sv.ID == s.ID {
			return sv, nil
		}
	}
	return StepView{Step: s}, nil
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
	if !sessionSaved(s.SessionID) {
		ag := agentFor(a.settings.Agents(), s.Agent)
		return fmt.Errorf("Claude Code never saved this %s session: its process ended before the first prompt was recorded, so there is nothing to resume. Use Retry %s to run the step again", ag.DisplayName(s.N), ag.DisplayName(s.N))
	}
	p := a.projectOf(t)
	ag := agentFor(a.settings.Agents(), s.Agent)
	steps, _ := a.db.Steps(t.ID)
	l := launch{task: t, proj: p, agent: ag, step: s, prevSHA: latestSHA(steps, s.Seq)}
	_ = a.writeContextFile(p, t)
	a.reap()
	if _, err := a.run.Start(a.spec(l, true)); err != nil && !errors.Is(err, runner.ErrRunning) {
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

func (a *App) artifactDone(s store.Step) (bool, string) {
	art, ag := a.artifactFor(s)
	key, values := ag.DoneRule()
	return art.IsDone(key, values), art.Get("verdict")
}

// onHook turns Claude Code hook events into step states.
func (a *App) onHook(ev hooks.Event) {
	var finish bool
	var verdict string
	if ev.Name == "Stop" {
		if s, err := a.db.Step(ev.StepID); err == nil {
			finish, verdict = a.artifactDone(s)
		}
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
		case "PostToolUse":
			if s.State == store.Approval {
				s.State = store.Running
			}
		case "Stop":
			if finish {
				s.State, s.EndedAt = store.Done, time.Now()
				if verdict != "" {
					s.Verdict = verdict
				}
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
			if verdict != "" {
				s.Verdict = verdict
			}
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

// MarkDone marks a step done when the agent forgot its front matter.
func (a *App) MarkDone(stepID string) error {
	verdict := ""
	if s, err := a.db.Step(stepID); err == nil {
		_, verdict = a.artifactDone(s)
	}
	s, changed := a.updateStep(stepID, func(s *store.Step) bool {
		if s.State == store.Done {
			return false
		}
		s.State, s.EndedAt = store.Done, time.Now()
		if verdict != "" {
			s.Verdict = verdict
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

// complete runs the orchestrator's actions the first time a step is done:
// commit, start automatic agents, push.
func (a *App) complete(stepID string) {
	s, first := a.updateStep(stepID, func(s *store.Step) bool {
		if s.Completed || s.State != store.Done {
			return false
		}
		s.Completed = true
		return true
	})
	if !first {
		return
	}
	go a.afterDone(s)
}

func (a *App) afterDone(s store.Step) {
	t, err := a.db.Task(s.TaskID)
	if err != nil {
		return
	}
	p := a.projectOf(t)
	ag := agentFor(a.settings.Agents(), s.Agent)
	sess, _ := a.run.Get(s.ID)
	say := func(line string) {
		if sess != nil {
			sess.Inject([]byte("\r\n\x1b[36m● herd: " + line + "\x1b[0m\r\n"))
		}
	}

	if ag.Commit {
		msg := fmt.Sprintf("[%s] %s", t.Slug, strings.ToLower(strings.ReplaceAll(ag.DisplayName(s.N), " ", "-")))
		sha, err := a.git.CommitAll(a.ctx, worktreePath(p, t), msg)
		switch {
		case err != nil:
			a.toast("Commit failed", err.Error(), true)
			say("commit failed: " + err.Error())
		case sha == "":
			say("nothing to commit")
		default:
			a.updateStep(s.ID, func(x *store.Step) bool { x.SHA = sha; return true })
			say(fmt.Sprintf("committed %s %q", sha, msg))
			a.emitTask(t.ID)
		}
	}

	if ag.Push {
		a.push(t, s, say)
	}

	for _, next := range a.settings.Agents() {
		if next.AfterAgent() == s.Agent {
			if _, err := a.StartStep(t.ID, next.ID, "", 0); err != nil {
				a.toast("Could not start "+next.Name, err.Error(), true)
			} else {
				say(next.Name + " started automatically")
			}
		}
	}
}

// push pushes the branch, opens the PR from PR.md and starts waiting for the
// Copilot review.
func (a *App) push(t store.Task, s store.Step, say func(string)) {
	p := a.projectOf(t)
	wt := worktreePath(p, t)
	runDir := a.runDir(p, t)
	g := gitops.Git{Log: say}
	// Work no review committed (e.g. Implement, then Push) still goes in the PR.
	if sha, err := g.CommitAll(a.ctx, wt, fmt.Sprintf("[%s] unreviewed changes", t.Slug)); err != nil {
		say("commit failed: " + err.Error())
		a.toast("Commit failed", err.Error(), true)
		return
	} else if sha != "" {
		say(fmt.Sprintf("committed unreviewed changes as %s", sha))
	}
	if err := g.Push(a.ctx, wt, branchName(t)); err != nil {
		say("push failed: " + err.Error())
		a.toast("Push failed", err.Error(), true)
		return
	}
	ag := agentFor(a.settings.Agents(), s.Agent)
	art, err := artifacts.Read(filepath.Join(runDir, ag.ArtifactName(s.N)))
	if err != nil || !art.Exists {
		say("no " + ag.ArtifactName(s.N) + ", so no pull request was opened")
		return
	}
	title, body := splitTitle(art.Body)
	if title == "" {
		title = t.Title
	}
	if t.Issue != "" && !strings.Contains(body, "#"+t.Issue) {
		body += fmt.Sprintf("\n\nCloses #%s\n", t.Issue)
	}
	bodyFile := filepath.Join(runDir, "PR-body.md")
	if err := os.WriteFile(bodyFile, []byte(body), 0o644); err != nil {
		say(err.Error())
		return
	}
	gh := github.GH{Log: say}
	n, url, err := gh.CreatePR(a.ctx, wt, p.Repo, p.BaseBranch, branchName(t), title, bodyFile)
	if err != nil {
		say("gh pr create failed: " + err.Error())
		a.toast("Could not open the PR", err.Error(), true)
		return
	}
	say(fmt.Sprintf("opened PR #%d %s", n, url))
	_ = a.updateTask(t.ID, func(t *store.Task) error {
		t.PRNumber, t.PRURL, t.PRState = n, url, "open"
		return nil
	})
	say("waiting for the Copilot review…")
	a.startCopilotPoller(t.ID, s.ID)
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

func (a *App) startCopilotPoller(taskID, stepID string) {
	a.pollMu.Lock()
	if a.copilot[taskID] {
		a.pollMu.Unlock()
		return
	}
	a.copilot[taskID] = true
	a.pollMu.Unlock()
	go func() {
		defer func() {
			a.pollMu.Lock()
			delete(a.copilot, taskID)
			a.pollMu.Unlock()
		}()
		timeout := time.Duration(a.settings.Config().CopilotTimeout) * time.Minute
		deadline := time.Now().Add(timeout)
		tick := time.NewTicker(time.Minute)
		defer tick.Stop()
		for {
			t, err := a.db.Task(taskID)
			if err != nil || t.PRNumber == 0 {
				return
			}
			p := a.projectOf(t)
			body, comments, ok, err := a.gh.CopilotReview(a.ctx, p.Repo, t.PRNumber)
			if err == nil && ok {
				a.appendFeedback(t, p.Name, copilotMarkdown(t.PRNumber, body, comments))
				_ = a.db.Set("copilot:"+taskID, "done")
				if sess, live := a.run.Get(stepID); live {
					sess.Inject([]byte(fmt.Sprintf("\r\n\x1b[36m● herd: Copilot review, %d comments appended to FEEDBACK.md\x1b[0m\r\n", len(comments))))
				}
				a.toast("Copilot review arrived", fmt.Sprintf("PR #%d: %d comments appended to FEEDBACK.md", t.PRNumber, len(comments)), false)
				return
			}
			if time.Now().After(deadline) {
				a.appendFeedback(t, p.Name, fmt.Sprintf("## Copilot review (PR #%d)\n\nNo Copilot review arrived within %d minutes; herd stopped waiting.\n", t.PRNumber, int(timeout.Minutes())))
				_ = a.db.Set("copilot:"+taskID, "timeout")
				return
			}
			select {
			case <-a.ctx.Done():
				return
			case <-tick.C:
			}
		}
	}()
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
	}
	for _, c := range comments {
		loc := c.Path
		if c.Line > 0 {
			loc = fmt.Sprintf("%s:%d", c.Path, c.Line)
		}
		fmt.Fprintf(&b, "- `%s` %s\n", loc, strings.ReplaceAll(strings.TrimSpace(c.Body), "\n", "\n  "))
	}
	return b.String()
}

func (a *App) appendFeedback(t store.Task, projectName, md string) {
	path := filepath.Join(a.Home, "runs", projectName, t.Slug, "FEEDBACK.md")
	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return
	}
	defer f.Close()
	if fi, _ := f.Stat(); fi != nil && fi.Size() > 0 {
		md = "\n" + md
	}
	_, _ = f.WriteString(md)
}

func (a *App) resumeCopilotPollers() {
	tasks, err := a.db.Tasks()
	if err != nil {
		return
	}
	for _, t := range tasks {
		if t.Archived || t.PRNumber == 0 || t.PRState != "open" {
			continue
		}
		if v, _ := a.db.Get("copilot:" + t.ID); v != "" {
			continue
		}
		steps, _ := a.db.Steps(t.ID)
		last := ""
		if len(steps) > 0 {
			last = steps[len(steps)-1].ID
		}
		a.startCopilotPoller(t.ID, last)
	}
}

// reconcile fixes steps left live by a previous run: herd owns every
// process, so after a restart none of them is running any more.
func (a *App) reconcile() {
	steps, err := a.db.StepsInStates(store.Running, store.Idle, store.Approval)
	if err != nil {
		return
	}
	for _, s := range steps {
		if a.paused(s.ID) {
			continue // the user resumes it
		}
		if _, err := a.run.Attach(s.ID); err == nil {
			continue // still running in its holder since the last herd
		}
		done, verdict := a.artifactDone(s)
		s2, changed := a.updateStep(s.ID, func(x *store.Step) bool {
			x.EndedAt = time.Now()
			if done {
				x.State = store.Done
				if verdict != "" {
					x.Verdict = verdict
				}
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
