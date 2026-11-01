package app

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"herd/internal/holder"
	"herd/internal/settings"
	"herd/internal/store"
)

// The test binary doubles as the session holder, like herd itself.
func TestMain(m *testing.M) {
	if len(os.Args) > 1 && os.Args[1] == "hold" {
		os.Exit(holder.Main(os.Args[2:]))
	}
	os.Exit(m.Run())
}

type fakePlatform struct {
	mu     sync.Mutex
	events []string
}

func (f *fakePlatform) Emit(ev string, _ any) {
	f.mu.Lock()
	f.events = append(f.events, ev)
	f.mu.Unlock()
}
func (f *fakePlatform) PickFolder(string) (string, error) { return "", nil }
func (f *fakePlatform) Notify(title, body, _ string) {
	f.mu.Lock()
	f.events = append(f.events, "notify:"+title+": "+body)
	f.mu.Unlock()
}

func run(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command(args[0], args[1:]...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("%v: %v\n%s", args, err, out)
	}
	return strings.TrimSpace(string(out))
}

func newTestApp(t *testing.T) (*App, string) {
	t.Helper()
	if _, err := exec.LookPath("curl"); err != nil {
		t.Skip("curl not installed")
	}
	root := t.TempDir()
	repo := filepath.Join(root, "pony-core")
	os.MkdirAll(repo, 0o755)
	run(t, repo, "git", "init", "-q", "-b", "main")
	run(t, repo, "git", "config", "user.email", "t@example.com")
	run(t, repo, "git", "config", "user.name", "t")
	os.WriteFile(filepath.Join(repo, "README.md"), []byte("hi\n"), 0o644)
	run(t, repo, "git", "add", ".")
	run(t, repo, "git", "commit", "-q", "-m", "init")

	home := filepath.Join(root, "herd")
	// Claude Code's transcripts, for the resume checks.
	t.Setenv("CLAUDE_CONFIG_DIR", filepath.Join(root, "claude"))
	os.MkdirAll(filepath.Join(root, "claude", "projects"), 0o755)
	// herd started from inside a Claude Code session must not pass that on.
	t.Setenv("CLAUDE_CODE_CHILD_SESSION", "1")
	fake, _ := filepath.Abs("testdata/fake-claude.sh")
	a := startApp(t, home)
	t.Cleanup(func() {
		a.run.StopAll() // tests leave no holder behind
		Stop(a)
	})
	if err := a.settings.UpdateConfig(func(c *settings.Config) { c.ClaudeCommand = fake }); err != nil {
		t.Fatal(err)
	}
	if err := a.SaveProject(settings.Project{ID: "core", Name: "pony-core", Path: repo, BaseBranch: "main"}); err != nil {
		t.Fatal(err)
	}
	return a, repo
}

func startApp(t *testing.T, home string) *App {
	t.Helper()
	a := New(home, nil)
	a.HoldCmd = []string{os.Args[0], "hold"}
	if err := Start(a, context.Background(), &fakePlatform{}); err != nil {
		t.Fatal(err)
	}
	return a
}

func waitFor(t *testing.T, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for %s", what)
}

func stepState(a *App, id string) string {
	s, err := a.db.Step(id)
	if err != nil {
		return ""
	}
	return s.State
}

func TestPlanImplementReview(t *testing.T) {
	a, repo := newTestApp(t)
	tv, err := a.CreateTask("core", "", "Add a fleet heatmap endpoint for ops", true)
	if err != nil {
		t.Fatal(err)
	}
	if tv.Slug != "local-add-a-fleet" || tv.Branch != "agent/local-add-a-fleet" {
		t.Fatalf("slug %q branch %q", tv.Slug, tv.Branch)
	}
	if _, err := os.Stat(filepath.Join(repo, ".worktrees", tv.Slug)); err != nil {
		t.Fatal("worktree missing")
	}
	if b, _ := os.ReadFile(filepath.Join(repo, ".git", "info", "exclude")); !strings.Contains(string(b), ".worktrees/") {
		t.Fatal(".worktrees/ not excluded")
	}
	task, _ := os.ReadFile(filepath.Join(tv.RunDir, "TASK.md"))
	if !strings.Contains(string(task), "source: local") {
		t.Fatalf("TASK.md: %s", task)
	}

	plan := store.StepID(tv.ID, "plan-1")
	waitFor(t, "plan done", func() bool { return stepState(a, plan) == store.Done })
	v, _ := a.GetTask(tv.ID)
	if v.Status.Label != "Waiting for input · after Plan" || len(v.Next.Options) != 1 || v.Next.Options[0].Label != "Implement 1" {
		t.Fatalf("after plan: %+v %+v", v.Status, v.Next)
	}

	// Click 2: Implement 1. Review 1 must start by itself.
	if _, err := a.StartStep(tv.ID, "impl", "keep it small", 0); err != nil {
		t.Fatal(err)
	}
	impl := store.StepID(tv.ID, "impl-1")
	rev := store.StepID(tv.ID, "review-1")
	waitFor(t, "review committed", func() bool { s, _ := a.db.Step(rev); return s.SHA != "" })
	// Implement leaves its work uncommitted; its Review commits it.
	s, _ := a.db.Step(impl)
	if s.SHA != "" || s.Guidance != "keep it small" {
		t.Fatalf("impl step: %+v", s)
	}
	if msg := run(t, filepath.Join(repo, ".worktrees", tv.Slug), "git", "log", "-1", "--format=%s"); msg != "["+tv.Slug+"] review-1" {
		t.Fatalf("commit message %q", msg)
	}
	if n := run(t, filepath.Join(repo, ".worktrees", tv.Slug), "git", "rev-list", "--count", "main..HEAD"); n != "1" {
		t.Fatalf("want 1 commit, got %s", n)
	}
	r, _ := a.db.Step(rev)
	if r.Verdict != "changes" {
		t.Fatalf("verdict %q", r.Verdict)
	}
	v, _ = a.GetTask(tv.ID)
	if len(v.Next.Options) != 2 || v.Next.Options[0].Label != "Implement 2" || v.Next.Options[1].Label != "Push" {
		t.Fatalf("after review: %+v", v.Next.Options)
	}
	ctxFile, _ := os.ReadFile(filepath.Join(tv.RunDir, "CONTEXT.md"))
	if !strings.Contains(string(ctxFile), "General context") {
		t.Fatalf("CONTEXT.md: %s", ctxFile)
	}

	sess, ok := a.run.Get(rev)
	if !ok {
		t.Fatal("review session not live")
	}
	_, back := sess.Attach()
	if !strings.Contains(string(back), "/herd:review --local") {
		t.Fatalf("review prompt: %q", back)
	}

	// Killing herd mid-step: a fresh start marks dead live steps failed.
	if _, err := a.StartStep(tv.ID, "impl", "", 0); err != nil {
		t.Fatal(err)
	}
}

func TestApprovalAndAllowOnce(t *testing.T) {
	a, _ := newTestApp(t)
	ags := a.settings.Agents()
	ags = append([]settings.Agent{{ID: "ask", Name: "Ask", Command: "/herd:ask {task}", Writes: "ASK.md", Done: "status: done"}}, ags...)
	if err := a.SaveAgents(ags); err != nil {
		t.Fatal(err)
	}
	tv, err := a.CreateTask("core", "", "approve things please", true)
	if err != nil {
		t.Fatal(err)
	}
	id := store.StepID(tv.ID, "ask-1")
	waitFor(t, "approval", func() bool { return stepState(a, id) == store.Approval })
	v, _ := a.GetTask(tv.ID)
	if v.Status.Kind != "approval" || v.Next.Approval != id {
		t.Fatalf("%+v %+v", v.Status, v.Next)
	}
	if err := a.AllowOnce(id); err != nil {
		t.Fatal(err)
	}
	waitFor(t, "done", func() bool { return stepState(a, id) == store.Done })
}

func TestExitWithoutArtifactFails(t *testing.T) {
	a, _ := newTestApp(t)
	ags := a.settings.Agents()
	ags[0].Command = "/herd:plan {task}"
	ags[0].Writes = "NEVER.md" // fake claude writes $HERD_ARTIFACT, so point it elsewhere via done rule
	ags[0].Done = "status: finished"
	if err := a.SaveAgents(ags); err != nil {
		t.Fatal(err)
	}
	tv, err := a.CreateTask("core", "", "fail me", true)
	if err != nil {
		t.Fatal(err)
	}
	id := store.StepID(tv.ID, "plan-1")
	waitFor(t, "idle", func() bool { return stepState(a, id) == store.Idle })
	sess, _ := a.run.Get(id)
	sess.Write([]byte("bye\r"))
	waitFor(t, "failed", func() bool { return stepState(a, id) == store.Failed })
	v, _ := a.GetTask(tv.ID)
	if len(v.Next.Options) != 1 || v.Next.Options[0].Label != "Retry Plan" || v.Next.MarkDone != id {
		t.Fatalf("%+v", v.Next)
	}
	// Opening the failed step shows the output of the run that just ended
	// instead of resuming it.
	if _, err := a.TerminalURL(id); err != nil {
		t.Fatal(err)
	}
	if _, ok := a.run.Get(id); ok {
		t.Fatal("TerminalURL resumed a session that ended during this run")
	}
	if s, ok := a.run.Ended(id); !ok || s.Exited() == false {
		t.Fatal("ended session output not kept")
	}
	// Resume refuses a conversation Claude Code never saved...
	if _, err := a.ResumeStep(id); err == nil || !strings.Contains(err.Error(), "never saved") {
		t.Fatalf("want a never-saved error, got %v", err)
	}
	// ...and resumes one it did.
	step, _ := a.db.Step(id)
	proj := filepath.Join(os.Getenv("CLAUDE_CONFIG_DIR"), "projects", "-tmp-x")
	os.MkdirAll(proj, 0o755)
	os.WriteFile(filepath.Join(proj, step.SessionID+".jsonl"), []byte("{}\n"), 0o644)
	if _, err := a.ResumeStep(id); err != nil {
		t.Fatal(err)
	}
	if _, ok := a.run.Get(id); !ok {
		t.Fatal("not resumed")
	}
	if err := a.MarkDone(id); err != nil {
		t.Fatal(err)
	}
	if stepState(a, id) != store.Done {
		t.Fatal("not done")
	}
}

func TestWorktreesOutsideRepo(t *testing.T) {
	in := settings.Project{Path: "/code/herd", WorktreesDir: ".worktrees"}
	out := settings.Project{Path: "/code/herd", WorktreesDir: "../herd.worktrees"}
	abs := settings.Project{Path: "/code/herd", WorktreesDir: "/tmp/wt"}
	task := store.Task{Slug: "local-x"}
	if excludePattern(in) != ".worktrees/" || worktreePath(in, task) != "/code/herd/.worktrees/local-x" {
		t.Fatal(excludePattern(in), worktreePath(in, task))
	}
	if excludePattern(out) != "" || worktreePath(out, task) != "/code/herd.worktrees/local-x" {
		t.Fatal(excludePattern(out), worktreePath(out, task))
	}
	if excludePattern(abs) != "" || worktreePath(abs, task) != "/tmp/wt/local-x" {
		t.Fatal(excludePattern(abs), worktreePath(abs, task))
	}
}

func TestLocalSlug(t *testing.T) {
	for in, want := range map[string]string{
		"Add haptic feedback when a ride starts": "local-add-haptic-feedback",
		"Rebalance: heatmap!":                    "local-rebalance-heatmap",
		"":                                       "local-task",
	} {
		if got := LocalSlug(in); got != want {
			t.Errorf("LocalSlug(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestPauseAndResume(t *testing.T) {
	a, _ := newTestApp(t)
	tv, _ := a.CreateTask("core", "", "pause me", true)
	id := store.StepID(tv.ID, "plan-1")
	waitFor(t, "plan done", func() bool { return stepState(a, id) == store.Done })
	step, _ := a.db.Step(id)
	proj := filepath.Join(os.Getenv("CLAUDE_CONFIG_DIR"), "projects", "-tmp-x")
	os.MkdirAll(proj, 0o755)
	os.WriteFile(filepath.Join(proj, step.SessionID+".jsonl"), []byte("{}\n"), 0o644)

	if err := a.StopStep(id); err != nil {
		t.Fatal(err)
	}
	waitFor(t, "stopped", func() bool { _, live := a.run.Get(id); return !live })
	if v, _ := a.GetTask(tv.ID); !v.Steps[0].Paused {
		t.Fatal("step not paused")
	}
	if _, err := a.TerminalURL(id); err != nil {
		t.Fatal(err) // replays the stopped output
	}
	if _, live := a.run.Get(id); live {
		t.Fatal("TerminalURL resumed a paused session")
	}
	if _, err := a.ResumeStep(id); err != nil {
		t.Fatal(err)
	}
	if _, live := a.run.Get(id); !live || a.paused(id) {
		t.Fatal("not resumed")
	}
}

func addAskAgent(t *testing.T, a *App) {
	t.Helper()
	ags := append([]settings.Agent{{ID: "ask", Name: "Ask", Command: "/herd:ask {task}", Writes: "ASK.md", Done: "status: done"}}, a.settings.Agents()...)
	if err := a.SaveAgents(ags); err != nil {
		t.Fatal(err)
	}
}

func TestSessionSurvivesRestart(t *testing.T) {
	a, _ := newTestApp(t)
	addAskAgent(t, a)
	tv, _ := a.CreateTask("core", "", "survive a restart", true)
	id := store.StepID(tv.ID, "ask-1")
	waitFor(t, "approval", func() bool { return stepState(a, id) == store.Approval })

	// Quit herd: the step keeps running in its holder.
	Stop(a)
	b := startApp(t, a.Home)
	t.Cleanup(func() { b.run.StopAll(); Stop(b) })
	sess, live := b.run.Get(id)
	if !live {
		t.Fatal("not reattached after restart")
	}
	_, scroll := sess.Attach()
	if !strings.Contains(string(scroll), "fake claude: /herd:ask") {
		t.Fatalf("scrollback not replayed: %q", scroll)
	}
	if stepState(b, id) != store.Approval {
		t.Fatalf("state %s", stepState(b, id))
	}
	// Its hooks reach the new herd: answering finishes the step.
	if err := b.AllowOnce(id); err != nil {
		t.Fatal(err)
	}
	waitFor(t, "done after restart", func() bool { return stepState(b, id) == store.Done })
}

func TestStepChanges(t *testing.T) {
	a, repo := newTestApp(t)
	tv, _ := a.CreateTask("core", "", "change some files", true)
	waitFor(t, "plan", func() bool { return stepState(a, store.StepID(tv.ID, "plan-1")) == store.Done })
	impl := store.StepID(tv.ID, "impl-1")
	if _, err := a.StartStep(tv.ID, "impl", "", 0); err != nil {
		t.Fatal(err)
	}
	rev := store.StepID(tv.ID, "review-1")
	waitFor(t, "commit", func() bool { s, _ := a.db.Step(rev); return s.SHA != "" })
	// Implement shows the commit its Review made.
	c, err := a.StepChanges(impl)
	if err != nil || c.Source != "commit" || c.Via != "Review 1" || len(c.Files) != 1 || c.Files[0] != (FileChange{Path: "file-1.txt", Status: "A", Added: 1}) {
		t.Fatalf("%+v %v", c, err)
	}
	// The latest step with no commit of its own shows the worktree's uncommitted work.
	waitFor(t, "review", func() bool { return stepState(a, rev) == store.Done })
	a.StartStep(tv.ID, "plan", "", 0)
	rev = store.StepID(tv.ID, "plan-2")
	waitFor(t, "plan 2", func() bool { return stepState(a, rev) == store.Done })
	wt := filepath.Join(repo, ".worktrees", tv.Slug)
	os.WriteFile(filepath.Join(wt, "README.md"), []byte("hi\nthere\n"), 0o644)
	os.WriteFile(filepath.Join(wt, "new.txt"), []byte("a\nb\nc"), 0o644)
	c, err = a.StepChanges(rev)
	if err != nil || c.Source != "worktree" || len(c.Files) != 2 {
		t.Fatalf("%+v %v", c, err)
	}
	if c.Files[0] != (FileChange{Path: "README.md", Status: "M", Added: 1}) || c.Files[1] != (FileChange{Path: "new.txt", Status: "?", Added: 3}) {
		t.Fatalf("%+v", c.Files)
	}
}

func TestResumeGuard(t *testing.T) {
	a, _ := newTestApp(t)
	tv, _ := a.CreateTask("core", "", "guard the session", true)
	id := store.StepID(tv.ID, "plan-1")
	waitFor(t, "plan", func() bool { return stepState(a, id) == store.Done })
	step, _ := a.db.Step(id)
	proj := filepath.Join(os.Getenv("CLAUDE_CONFIG_DIR"), "projects", "-tmp-x")
	os.MkdirAll(proj, 0o755)
	os.WriteFile(filepath.Join(proj, step.SessionID+".jsonl"), []byte("{}\n"), 0o644)
	a.StopStep(id)
	waitFor(t, "stopped", func() bool { _, live := a.run.Get(id); return !live })

	// The same session resumed in another terminal, e.g. in Zed.
	other := exec.Command("sh", "-c", "sleep 30; true", "claude", "--resume", step.SessionID)
	if err := other.Start(); err != nil {
		t.Fatal(err)
	}
	defer other.Process.Kill()
	ps, _ := a.SessionElsewhere(id)
	if len(ps) != 1 || ps[0].PID != other.Process.Pid {
		t.Fatalf("procs %+v", ps)
	}
	if _, err := a.ResumeStep(id); err == nil || !strings.Contains(err.Error(), "another process") {
		t.Fatalf("want a guard error, got %v", err)
	}
	if _, err := a.ResumeStepAnyway(id); err != nil {
		t.Fatal(err)
	}
	if _, live := a.run.Get(id); !live {
		t.Fatal("not resumed")
	}
}

func TestNotifications(t *testing.T) {
	a, _ := newTestApp(t)
	a.SetFocused(false)
	tv, _ := a.CreateTask("core", "", "notify me", true)
	waitFor(t, "plan", func() bool { return stepState(a, store.StepID(tv.ID, "plan-1")) == store.Done })
	f := a.plat.(*fakePlatform)
	waitFor(t, "notification", func() bool {
		f.mu.Lock()
		defer f.mu.Unlock()
		for _, e := range f.events {
			if strings.HasPrefix(e, "notify:Plan finished: LOCAL notify me") {
				return true
			}
		}
		return false
	})
	// Not while herd has focus.
	a.SetFocused(true)
	f.mu.Lock()
	f.events = nil
	f.mu.Unlock()
	a.StartStep(tv.ID, "impl", "", 0)
	// Review starts by itself after Implement's commit; wait for the lot.
	waitFor(t, "review", func() bool { return stepState(a, store.StepID(tv.ID, "review-1")) == store.Done })
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, e := range f.events {
		if strings.HasPrefix(e, "notify:") {
			t.Fatalf("notified while focused: %s", e)
		}
	}
}

func TestEachReviewCommitsItsOwnDiff(t *testing.T) {
	a, repo := newTestApp(t)
	a.settings.UpdateConfig(func(c *settings.Config) { c.ZedCommand = "true" })
	tv, _ := a.CreateTask("core", "", "two review rounds", true)
	waitFor(t, "plan", func() bool { return stepState(a, store.StepID(tv.ID, "plan-1")) == store.Done })
	wt := filepath.Join(repo, ".worktrees", tv.Slug)
	shas := map[string]string{}
	for n := 1; n <= 2; n++ {
		if _, err := a.StartStep(tv.ID, "impl", "", 0); err != nil {
			t.Fatal(err)
		}
		rev := store.StepID(tv.ID, fmt.Sprintf("review-%d", n))
		waitFor(t, "review commit", func() bool { s, _ := a.db.Step(rev); return s.SHA != "" })
		s, _ := a.db.Step(rev)
		shas[rev] = s.SHA
		// Each review's commit holds only its own Implement's file.
		if files := run(t, wt, "git", "show", "--format=", "--name-only", s.SHA); files != fmt.Sprintf("file-%d.txt", n) {
			t.Fatalf("review %d commit has %q", n, files)
		}
		// Zed diff opens that commit in Zed's commit view, in the worktree.
		lines, err := a.OpenInZed(tv.ID, rev)
		full := run(t, wt, "git", "rev-parse", s.SHA)
		if err != nil || !strings.Contains(strings.Join(lines, "\n"), "zed://git/commit/"+full+"?repo=") {
			t.Fatalf("%v %v", lines, err)
		}
		waitFor(t, "review done", func() bool { return stepState(a, rev) == store.Done })
		if sess, ok := a.run.Get(rev); ok {
			sess.Write([]byte("bye\r")) // let the next Implement start
			waitFor(t, "review exited", func() bool { _, live := a.run.Get(rev); return !live })
		}
	}
	if shas[store.StepID(tv.ID, "review-1")] == shas[store.StepID(tv.ID, "review-2")] {
		t.Fatal("both reviews point at the same commit")
	}
	// A changed file opens as it was in the commit, not as it is now.
	if err := os.WriteFile(filepath.Join(wt, "file-1.txt"), []byte("edited later\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := a.OpenFileInZed(tv.ID, "file-1.txt", shas[store.StepID(tv.ID, "review-1")]); err != nil {
		t.Fatal(err)
	}
	snaps, _ := filepath.Glob(filepath.Join(a.Home, "views", "*", "*", "file-1.txt"))
	if len(snaps) != 1 {
		t.Fatalf("snapshots: %v", snaps)
	}
	if b, _ := os.ReadFile(snaps[0]); string(b) != "change 1\n" {
		t.Fatalf("snapshot holds %q", b)
	}
}

func TestRulesAndKnowledge(t *testing.T) {
	a, _ := newTestApp(t)
	kb := t.TempDir()
	if err := a.SetRules("Never touch migrations/"); err != nil {
		t.Fatal(err)
	}
	c, _ := a.settings.Context("core")
	c.Knowledge = kb
	if err := a.SaveContext(c); err != nil {
		t.Fatal(err)
	}
	tv, err := a.CreateTask("core", "", "rules apply", false)
	if err != nil {
		t.Fatal(err)
	}
	task, _ := a.db.Task(tv.ID)
	p, _ := a.settings.Project("core")
	for _, ag := range a.settings.Agents() {
		args := strings.Join(a.spec(launch{task: task, proj: p, agent: ag, step: store.Step{N: 1, SessionID: "s"}}, false).Args, "\x00")
		if !strings.Contains(args, "Never touch migrations/") || !strings.Contains(args, "--add-dir\x00"+kb) || strings.Contains(args, "allowedTools") {
			t.Fatalf("%s args: %q", ag.ID, args)
		}
	}
	if err := a.writeContextFile(p, task); err != nil {
		t.Fatal(err)
	}
	if b, _ := os.ReadFile(filepath.Join(tv.RunDir, "CONTEXT.md")); !strings.Contains(string(b), kb) {
		t.Fatalf("CONTEXT.md: %s", b)
	}
}
