package app

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"herd/internal/github"
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
func (f *fakePlatform) PickFolder(string) (string, error)  { return "", nil }
func (f *fakePlatform) PickFiles(string) ([]string, error) { return nil, nil }
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
	tv, err := a.CreateTask("core", "", "Add a fleet heatmap endpoint for ops", "", true, nil)
	if err != nil {
		t.Fatal(err)
	}
	if tv.Slug != "local-add-a-fleet" || tv.Branch != "ar-local-add-a-fleet" {
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
	// The review passed, so Test 1 starts by itself.
	waitFor(t, "test done", func() bool { return stepState(a, store.StepID(tv.ID, "test-1")) == store.Done })
	waitFor(t, "next after test", func() bool {
		v, _ = a.GetTask(tv.ID)
		return len(v.Next.Options) == 2 && v.Next.Options[0].Label == "Push" && v.Next.Options[1].Label == "Implement 2"
	})
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
	ags = append([]settings.Agent{{ID: "ask", Name: "Ask", Command: "/herd:ask {task}", Writes: "ASK.md"}}, ags...)
	if err := a.SaveAgents(ags); err != nil {
		t.Fatal(err)
	}
	tv, err := a.CreateTask("core", "", "approve things please", "", true, nil)
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
	t.Setenv("FAKE_STATUS", "working") // the plan never says it finished
	tv, err := a.CreateTask("core", "", "fail me", "", true, nil)
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
	tv, _ := a.CreateTask("core", "", "pause me", "", true, nil)
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
	ags := append([]settings.Agent{{ID: "ask", Name: "Ask", Command: "/herd:ask {task}", Writes: "ASK.md"}}, a.settings.Agents()...)
	if err := a.SaveAgents(ags); err != nil {
		t.Fatal(err)
	}
}

func TestSessionSurvivesRestart(t *testing.T) {
	a, _ := newTestApp(t)
	addAskAgent(t, a)
	tv, _ := a.CreateTask("core", "", "survive a restart", "", true, nil)
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
	tv, _ := a.CreateTask("core", "", "change some files", "", true, nil)
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
	tv, _ := a.CreateTask("core", "", "guard the session", "", true, nil)
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
	tv, _ := a.CreateTask("core", "", "notify me", "", true, nil)
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
	tv, _ := a.CreateTask("core", "", "two review rounds", "", true, nil)
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
	// The diff herd shows for a file is the commit's.
	d, err := a.FileDiff(store.StepID(tv.ID, "review-1"), "file-1.txt")
	if err != nil || !strings.Contains(d.Diff, "+change 1") || strings.Contains(d.Diff, "file-2") {
		t.Fatalf("diff %+v %v", d, err)
	}
	if _, err := a.FileDiff(store.StepID(tv.ID, "review-1"), "../secret"); err == nil {
		t.Fatal("diff of a file outside the step's changes")
	}
	// An Implement opens the commit its Review made, not the latest one.
	lines, err := a.OpenInZed(tv.ID, store.StepID(tv.ID, "impl-1"))
	full := run(t, wt, "git", "rev-parse", shas[store.StepID(tv.ID, "review-1")])
	if err != nil || !strings.Contains(strings.Join(lines, "\n"), "zed://git/commit/"+full+"?repo=") {
		t.Fatalf("impl-1 opened %v %v", lines, err)
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
	tv, err := a.CreateTask("core", "", "rules apply", "", false, nil)
	if err != nil {
		t.Fatal(err)
	}
	task, _ := a.db.Task(tv.ID)
	p, _ := a.settings.Project("core")
	for _, ag := range a.settings.Agents() {
		args := strings.Join(a.spec(launch{task: task, proj: p, agent: ag, step: store.Step{N: 1, SessionID: "s"}}).Args, "\x00")
		if strings.Contains(args, "Never touch migrations/") == ag.IgnoreRules || !strings.Contains(args, "--add-dir\x00"+kb) || strings.Contains(args, "allowedTools") {
			t.Fatalf("%s (ignore rules %v) args: %q", ag.ID, ag.IgnoreRules, args)
		}
	}
	if err := a.writeContextFile(p, task); err != nil {
		t.Fatal(err)
	}
	if b, _ := os.ReadFile(filepath.Join(tv.RunDir, "CONTEXT.md")); !strings.Contains(string(b), kb) {
		t.Fatalf("CONTEXT.md: %s", b)
	}
}

func TestManualStep(t *testing.T) {
	a, _ := newTestApp(t)
	os.MkdirAll(filepath.Join(os.Getenv("CLAUDE_CONFIG_DIR"), "projects"), 0o755) // transcripts are tracked
	tv, _ := a.CreateTask("core", "", "implement it myself", "", true, nil)
	waitFor(t, "plan", func() bool { return stepState(a, store.StepID(tv.ID, "plan-1")) == store.Done })
	sv, err := a.StartManualStep(tv.ID, "impl", "", 0)
	if err != nil {
		t.Fatal(err)
	}
	if _, live := a.run.Get(sv.ID); live || !sv.Manual || !sv.Paused || !sv.Fresh || sv.State != store.Idle {
		t.Fatalf("manual step: live=%v %+v", live, sv)
	}
	v, _ := a.GetTask(tv.ID)
	if v.Next.Ghost == nil || !v.Next.Ghost.Manual || v.Next.Ghost.Start == nil || v.Next.Ghost.Start.Agent != "review" {
		t.Fatalf("ghost: %+v", v.Next.Ghost)
	}
	if len(v.Next.Options) != 1 || v.Next.Options[0].Label != "Review 1" {
		t.Fatalf("options: %+v", v.Next.Options)
	}
	// Resume in herd starts its session there, without a prompt.
	if _, err := a.ResumeStep(sv.ID); err != nil {
		t.Fatal(err)
	}
	sess, ok := a.run.Get(sv.ID)
	if !ok {
		t.Fatal("not resumed")
	}
	waitFor(t, "blank session", func() bool { _, b := sess.Attach(); return strings.Contains(string(b), "fake claude: "+sv.SessionID) })
	waitFor(t, "impl done", func() bool { return stepState(a, sv.ID) == store.Done })
	time.Sleep(300 * time.Millisecond)
	if steps, _ := a.db.Steps(tv.ID); len(steps) != 2 {
		t.Fatalf("Review started by itself after a manual step: %d steps", len(steps))
	}
	sess.Write([]byte("bye\r"))
	waitFor(t, "impl exited", func() bool { _, live := a.run.Get(sv.ID); return !live })
	if _, err := a.StartStep(tv.ID, "review", "", 0); err != nil {
		t.Fatal(err)
	}
}

func TestManualStepClosedByNextStep(t *testing.T) {
	a, _ := newTestApp(t)
	tv, _ := a.CreateTask("core", "", "manual then review", "", true, nil)
	waitFor(t, "plan", func() bool { return stepState(a, store.StepID(tv.ID, "plan-1")) == store.Done })
	sv, _ := a.StartManualStep(tv.ID, "impl", "", 0)
	if _, err := a.StartStep(tv.ID, "review", "", 0); err != nil {
		t.Fatal(err)
	}
	if s, _ := a.db.Step(sv.ID); s.State != store.Done || a.paused(sv.ID) {
		t.Fatalf("manual step left open: %+v", s)
	}
}

func TestRerunAmendsTheReviewCommit(t *testing.T) {
	a, repo := newTestApp(t)
	withoutAgent(t, a, "test") // the review stays the latest step
	tv, _ := a.CreateTask("core", "", "rerun the review", "", true, nil)
	waitFor(t, "plan", func() bool { return stepState(a, store.StepID(tv.ID, "plan-1")) == store.Done })
	if _, err := a.StartStep(tv.ID, "impl", "", 0); err != nil {
		t.Fatal(err)
	}
	rev := store.StepID(tv.ID, "review-1")
	waitFor(t, "review commit", func() bool { s, _ := a.db.Step(rev); return s.SHA != "" })
	waitFor(t, "review done", func() bool { return stepState(a, rev) == store.Done })
	first, _ := a.db.Step(rev)
	proj := filepath.Join(os.Getenv("CLAUDE_CONFIG_DIR"), "projects", "-tmp-x")
	os.MkdirAll(proj, 0o755)
	os.WriteFile(filepath.Join(proj, first.SessionID+".jsonl"), []byte("{}\n"), 0o644)
	wt := filepath.Join(repo, ".worktrees", tv.Slug)
	os.WriteFile(filepath.Join(wt, "edited-by-hand.txt"), []byte("x\n"), 0o644)

	if err := a.RerunStep(rev); err != nil {
		t.Fatal(err)
	}
	waitFor(t, "rerun amended", func() bool { s, _ := a.db.Step(rev); return s.State == store.Done && s.SHA != first.SHA })
	s, _ := a.db.Step(rev)
	if n := run(t, wt, "git", "rev-list", "--count", "main..HEAD"); n != "1" {
		t.Fatalf("want the review commit amended, got %s commits", n)
	}
	if files := run(t, wt, "git", "show", "--format=", "--name-only", s.SHA); files != "edited-by-hand.txt\nfile-1.txt" {
		t.Fatalf("amended commit has %q", files)
	}
	if err := a.RerunStep(store.StepID(tv.ID, "impl-1")); err == nil {
		t.Fatal("rerun of a step that is not the latest")
	}
}

func TestTaskBranch(t *testing.T) {
	a, repo := newTestApp(t)
	if _, err := a.CreateTask("core", "", "no prefix", "fix-it", false, nil); err == nil {
		t.Fatal("accepted a branch without the prefix")
	}
	if _, err := a.CreateTask("core", "", "bad name", "ar-bad..name", false, nil); err == nil {
		t.Fatal("accepted an invalid branch name")
	}
	tv, err := a.CreateTask("core", "", "take a vehicle out of QA", "ar-take-a-vehicle-out-of-qa", false, nil)
	if err != nil {
		t.Fatal(err)
	}
	if tv.Branch != "ar-take-a-vehicle-out-of-qa" || run(t, filepath.Join(repo, ".worktrees", tv.Slug), "git", "branch", "--show-current") != tv.Branch {
		t.Fatalf("branch %q", tv.Branch)
	}
	if _, err := a.CreateTask("core", "", "same branch", "ar-take-a-vehicle-out-of-qa", false, nil); err == nil {
		t.Fatal("two tasks on one branch")
	}
}

func TestPRTitle(t *testing.T) {
	a, _ := newTestApp(t)
	tv, _ := a.CreateTask("core", "", "pr title", "ar-take-a-vehicle-sent-back-to-prod-out-of-QA", false, nil)
	task, _ := a.db.Task(tv.ID)
	p, err := a.normalizePRTitle(task, PRTitle{Kind: "fix", Scope: "vehicles"})
	if err != nil {
		t.Fatal(err)
	}
	if got := p.Format("9726"); got != "fix(vehicles): take a vehicle sent back to prod out of QA #9726" {
		t.Fatalf("title %q", got)
	}
	if got := p.Format(""); got != "fix(vehicles): take a vehicle sent back to prod out of QA" {
		t.Fatalf("LOCAL title %q", got)
	}
	for _, bad := range []PRTitle{{Kind: "chore", Scope: "x"}, {Kind: "feat"}, {Kind: "feat", Scope: "two words"}} {
		if _, err := a.normalizePRTitle(task, bad); err == nil {
			t.Fatalf("accepted %+v", bad)
		}
	}
	if got := defaultPRDesc("agent/local-fix-it", "ar-"); got != "local fix it" {
		t.Fatalf("old branch desc %q", got)
	}
}

func TestSessionsSkipUserClaudeMd(t *testing.T) {
	a, _ := newTestApp(t)
	b, err := os.ReadFile(a.hooksPath())
	if err != nil {
		t.Fatal(err)
	}
	var s struct {
		Excludes []string `json:"claudeMdExcludes"`
		Hooks    map[string]any
	}
	if err := json.Unmarshal(b, &s); err != nil {
		t.Fatal(err)
	}
	want := filepath.Join(os.Getenv("CLAUDE_CONFIG_DIR"), "CLAUDE.md")
	if !slices.Contains(s.Excludes, want) || len(s.Hooks) == 0 {
		t.Fatalf("settings: %s", b)
	}
}

func TestOverrideSystemClaudeMd(t *testing.T) {
	a, _ := newTestApp(t)
	path := filepath.Join(os.Getenv("CLAUDE_CONFIG_DIR"), "CLAUDE.md")
	os.MkdirAll(filepath.Dir(path), 0o755)
	os.WriteFile(path, []byte("# mine\n"), 0o644)
	a.SetRules("Never push to main")
	kb := t.TempDir()
	c, _ := a.settings.Context("general")
	c.Knowledge = kb
	a.SaveContext(c)
	if m := a.SystemClaudeMd(); !m.Exists || m.Synced {
		t.Fatalf("before: %+v", m)
	}
	backup, err := a.OverrideSystemClaudeMd()
	if err != nil {
		t.Fatal(err)
	}
	if b, _ := os.ReadFile(backup); string(b) != "# mine\n" {
		t.Fatalf("backup %q holds %q", backup, b)
	}
	b, _ := os.ReadFile(path)
	if !strings.Contains(string(b), "General context") || !strings.Contains(string(b), "# Rules") || !strings.Contains(string(b), "Never push to main") || !strings.Contains(string(b), "# Knowledge base") || !strings.Contains(string(b), "- "+kb) {
		t.Fatalf("CLAUDE.md: %s", b)
	}
	if m := a.SystemClaudeMd(); !m.Synced {
		t.Fatal("not in sync after override")
	}
	if again, _ := a.OverrideSystemClaudeMd(); again != "" {
		t.Fatal("backed up a file that was already in sync")
	}
}

func TestUntrackedDiff(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "new.txt"), []byte("a\nb\n"), 0o644)
	d, err := untrackedDiff(dir, "new.txt")
	if err != nil || d != "--- /dev/null\n+++ b/new.txt\n@@ -0,0 +1,2 @@\n+a\n+b\n" {
		t.Fatalf("%q %v", d, err)
	}
}

func TestTestAgentRunsOnlyAfterReviewSucceeds(t *testing.T) {
	for _, status := range []string{"failed", "done"} {
		t.Run(status, func(t *testing.T) {
			t.Setenv("FAKE_REVIEW", status)
			a, _ := newTestApp(t)
			tv, _ := a.CreateTask("core", "", "test after review "+status, "", true, nil)
			waitFor(t, "plan", func() bool { return stepState(a, store.StepID(tv.ID, "plan-1")) == store.Done })
			if _, err := a.StartStep(tv.ID, "impl", "", 0); err != nil {
				t.Fatal(err)
			}
			rev := store.StepID(tv.ID, "review-1")
			waitFor(t, "review done", func() bool { return stepState(a, rev) == store.Done })
			test := store.StepID(tv.ID, "test-1")
			if status == "done" {
				waitFor(t, "test started", func() bool { _, err := a.db.Step(test); return err == nil })
				return
			}
			time.Sleep(300 * time.Millisecond)
			if _, err := a.db.Step(test); err == nil {
				t.Fatal("Test started after the review failed")
			}
			if s, _ := a.db.Step(rev); s.SHA != "" {
				t.Fatal("a failed review committed")
			}
			v, _ := a.GetTask(tv.ID)
			if len(v.Next.Options) == 0 || v.Next.Options[0].Label != "Implement 2" || v.Next.Ghost != nil {
				t.Fatalf("next after a failed review: %+v", v.Next)
			}
		})
	}
}

func TestDeleteTask(t *testing.T) {
	a, repo := newTestApp(t)
	tv, _ := a.CreateTask("core", "", "delete me for good", "", true, nil)
	plan := store.StepID(tv.ID, "plan-1")
	waitFor(t, "plan", func() bool { return stepState(a, plan) == store.Done })
	a.setPaused(plan, true)
	if _, err := a.DeleteTask(tv.ID); err == nil {
		t.Fatal("deleted a task that is not archived")
	}
	if err := a.ArchiveTask(tv.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := a.DeleteTask(tv.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := a.db.Task(tv.ID); err == nil {
		t.Fatal("task still in the db")
	}
	if steps, _ := a.db.Steps(tv.ID); len(steps) != 0 || a.paused(plan) {
		t.Fatalf("steps or state left: %d", len(steps))
	}
	if _, err := os.Stat(tv.RunDir); !os.IsNotExist(err) {
		t.Fatal("run dir left")
	}
	if _, err := os.Stat(filepath.Join(repo, ".worktrees", tv.Slug)); !os.IsNotExist(err) {
		t.Fatal("worktree left")
	}
	if out := run(t, repo, "git", "branch", "--list", tv.Branch); out != "" {
		t.Fatalf("branch left: %q", out)
	}
}

func TestDeleteTaskOfRemovedProject(t *testing.T) {
	a, repo := newTestApp(t)
	tv, _ := a.CreateTask("core", "", "project goes away", "", false, nil)
	a.ArchiveTask(tv.ID)
	if err := a.RemoveProject("core"); err != nil {
		t.Fatal(err)
	}
	if v, _ := a.GetTask(tv.ID); !v.ProjectGone {
		t.Fatal("task view does not say the project is gone")
	}
	if err := a.RestoreTask(tv.ID); err == nil {
		t.Fatal("restored a task without its project")
	}
	if _, err := a.RemoveWorktree(tv.ID); err == nil {
		t.Fatal("ran git without knowing the repository")
	}
	if _, err := a.DeleteTask(tv.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := a.db.Task(tv.ID); err == nil {
		t.Fatal("task still in the db")
	}
	if _, err := os.Stat(filepath.Join(repo, ".worktrees", tv.Slug)); err != nil {
		t.Fatal("the worktree of an unknown repository was touched")
	}
}

func TestNextAfterOutcome(t *testing.T) {
	agents := settings.DefaultAgents()
	step := func(agent string, n int, verdict string) StepView {
		ag := agentFor(agents, agent)
		return StepView{Step: store.Step{Agent: agent, N: n, State: store.Done, Verdict: verdict}, Name: ag.DisplayName(n), Artifact: ag.ArtifactName(n)}
	}
	labels := func(nx Next) string {
		var l []string
		for _, o := range nx.Options {
			l = append(l, o.Label)
		}
		return strings.Join(l, ", ")
	}
	copilotStep := func(n int, state, verdict string) StepView {
		return StepView{Step: store.Step{Agent: copilotAgent, N: n, State: state, Verdict: verdict}, Name: "Copilot 1", Artifact: "COPILOT-1.md", External: true}
	}
	base := []StepView{step("plan", 1, ""), step("impl", 1, ""), step("review", 1, "")}
	for name, c := range map[string]struct {
		steps []StepView
		task  store.Task
		want  string
	}{
		"test failed":      {append(base, step("test", 1, store.Failure)), store.Task{}, "Implement 2, Test 2"},
		"test passed":      {append(base, step("test", 1, "")), store.Task{}, "Push, Implement 2"},
		"implement failed": {[]StepView{step("plan", 1, ""), step("impl", 1, store.Failure)}, store.Task{}, "Implement 2, Plan"},
		"pushed":           {append(base, step("test", 1, ""), step("push", 1, "")), store.Task{PRNumber: 7}, "Implement 2"},
		"copilot comments": {append(base, step("push", 1, ""), copilotStep(1, store.Done, store.Failure)), store.Task{PRNumber: 7}, "Implement 2"},
		"copilot waiting":  {append(base, step("push", 1, ""), copilotStep(1, store.Running, "")), store.Task{PRNumber: 7}, "Implement 2"},
	} {
		if got := labels(nextOf(c.task, c.steps, agents)); got != c.want {
			t.Errorf("%s: %q, want %q", name, got, c.want)
		}
	}
}

func withoutAgent(t *testing.T, a *App, id string) {
	t.Helper()
	var keep []settings.Agent
	for _, ag := range a.settings.Agents() {
		if ag.ID != id {
			keep = append(keep, ag)
		}
	}
	if err := a.SaveAgents(keep); err != nil {
		t.Fatal(err)
	}
}

func TestRerunReviewAfterHandEdit(t *testing.T) {
	a, repo := newTestApp(t)
	tv, _ := a.CreateTask("core", "", "edit after the tests", "", true, nil)
	waitFor(t, "plan", func() bool { return stepState(a, store.StepID(tv.ID, "plan-1")) == store.Done })
	if _, err := a.StartStep(tv.ID, "impl", "", 0); err != nil {
		t.Fatal(err)
	}
	rev, test := store.StepID(tv.ID, "review-1"), store.StepID(tv.ID, "test-1")
	waitFor(t, "test snapshot", func() bool { v, _ := a.db.Get(treeKey(test)); return v != "" })
	rerun := func() bool {
		v, _ := a.GetTask(tv.ID)
		for _, s := range v.Steps {
			if s.ID == rev {
				return s.Rerun
			}
		}
		return false
	}
	if rerun() {
		t.Fatal("Rerun offered on the review before any hand edit")
	}
	os.WriteFile(filepath.Join(repo, ".worktrees", tv.Slug, "by-hand.txt"), []byte("x\n"), 0o644)
	if !rerun() {
		t.Fatal("no Rerun on the review after a hand edit")
	}
	proj := filepath.Join(os.Getenv("CLAUDE_CONFIG_DIR"), "projects", "-tmp-x")
	os.MkdirAll(proj, 0o755)
	s, _ := a.db.Step(rev)
	os.WriteFile(filepath.Join(proj, s.SessionID+".jsonl"), []byte("{}\n"), 0o644)
	if err := a.RerunStep(rev); err != nil {
		t.Fatal(err)
	}
	waitFor(t, "test 2 after the rerun", func() bool { return stepState(a, store.StepID(tv.ID, "test-2")) == store.Done })
}

func TestPlanOptionsWarning(t *testing.T) {
	t.Setenv("FAKE_OPTIONS", "1")
	a, _ := newTestApp(t)
	tv, _ := a.CreateTask("core", "", "two ways to do it", "", true, nil)
	waitFor(t, "plan", func() bool { return stepState(a, store.StepID(tv.ID, "plan-1")) == store.Done })
	v, _ := a.GetTask(tv.ID)
	if !strings.Contains(v.Next.Warning, "PLAN.md lists several options") || !strings.Contains(v.Next.Warning, "Implement 1") {
		t.Fatalf("warning %q", v.Next.Warning)
	}
}

func TestCopilotStep(t *testing.T) {
	a, _ := newTestApp(t)
	tv, _ := a.CreateTask("core", "", "wait for copilot", "", false, nil)
	_ = a.updateTask(tv.ID, func(t *store.Task) error { t.PRNumber, t.PRState = 7, "open"; return nil })
	a.startCopilotStep(tv.ID)
	id := store.StepID(tv.ID, "copilot-1")
	v, _ := a.GetTask(tv.ID)
	if v.Status.Short != "copilot" || v.Next.MarkDone != id {
		t.Fatalf("waiting: %+v %+v", v.Status, v.Next)
	}
	task, _ := a.db.Task(tv.ID)
	a.finishCopilotStep(task, github.Review{ID: 1, Body: "Looks fine", Comments: []github.Comment{{Path: "a.go", Line: 3, Body: "nil check"}}})
	s, _ := a.db.Step(id)
	if !s.Unsuccessful() {
		t.Fatalf("a review with comments should fail the step: %+v", s)
	}
	b, _ := os.ReadFile(filepath.Join(tv.RunDir, "COPILOT-1.md"))
	if !strings.Contains(string(b), "status: failed") || !strings.Contains(string(b), "`a.go:3` nil check") {
		t.Fatalf("COPILOT-1.md: %s", b)
	}
}

func TestCISummary(t *testing.T) {
	for want, buckets := range map[string][]string{"": nil, "CI passed": {"pass", "skipping"}, "CI running": {"pass", "pending"}, "CI failed": {"pending", "fail"}} {
		var cs []github.Check
		for _, b := range buckets {
			cs = append(cs, github.Check{Bucket: b})
		}
		if got := ciSummary(cs); got != want {
			t.Errorf("%v: %q, want %q", buckets, got, want)
		}
	}
}

func TestCreateTaskAttachments(t *testing.T) {
	a, _ := newTestApp(t)
	src := filepath.Join(t.TempDir(), "shot.png")
	if err := os.WriteFile(src, []byte("png"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := a.CreateTask("core", "", "missing file", "", false, []string{src + ".nope"}); err == nil {
		t.Fatal("a missing file should fail the task creation")
	}
	tv, err := a.CreateTask("core", "", "fix the screen in the shot", "", false, []string{src, src})
	if err != nil {
		t.Fatal(err)
	}
	for _, n := range []string{"shot.png", "shot-2.png"} {
		p := filepath.Join(tv.RunDir, attachDir, n)
		if b, err := os.ReadFile(p); err != nil || string(b) != "png" {
			t.Fatalf("%s not copied: %v", n, err)
		}
		md, _ := os.ReadFile(filepath.Join(tv.RunDir, "TASK.md"))
		if !strings.Contains(string(md), p) {
			t.Fatalf("TASK.md does not list %s:\n%s", p, md)
		}
	}
}
