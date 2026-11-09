package app

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
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
	"herd/internal/hooks"
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
		a.run.StopAll()
		Stop(a)
		killHolders(t, home) // tests leave no holder behind
	})
	if err := a.settings.UpdateConfig(func(c *settings.Config) { c.ClaudeCommand = fake }); err != nil {
		t.Fatal(err)
	}
	if err := a.SaveProject(settings.Project{ID: "core", Name: "pony-core", Path: repo, BaseBranch: "main"}); err != nil {
		t.Fatal(err)
	}
	return a, repo
}

// killHolders ends the holders still listening in home/sessions, e.g. a
// step an automatic trigger started while the test stopped herd. Stop keeps
// running steps alive on purpose, for the next herd.
func killHolders(t *testing.T, home string) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for {
		socks, _ := filepath.Glob(filepath.Join(home, "sessions", "*.sock"))
		if len(socks) == 0 {
			return
		}
		if time.Now().After(deadline) {
			t.Errorf("holders still running: %v", socks)
			return
		}
		for _, sock := range socks {
			if conn, err := net.Dial("unix", sock); err == nil {
				_ = holder.WriteFrame(conn, holder.Kill, nil)
				conn.Close()
			} else {
				_ = os.Remove(sock)
			}
		}
		time.Sleep(100 * time.Millisecond)
	}
}

func startApp(t *testing.T, home string) *App {
	t.Helper()
	a := New(home)
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
	if v.Status.Label != "Plan 1 · Waiting for next step" || len(v.Next.Options) != 2 || v.Next.Options[0].Label != "Implement 1" || v.Next.Options[1].Label != "Discuss 1" {
		t.Fatalf("after plan: %+v %+v", v.Status, v.Next)
	}

	// Click 2: Implement 1. Review 1 must start by itself.
	if _, err := a.StartStep(tv.ID, "impl", "keep it small", 0); err != nil {
		t.Fatal(err)
	}
	impl := store.StepID(tv.ID, "impl-1")
	rev := store.StepID(tv.ID, "review-1")
	waitFor(t, "review done", func() bool { return stepState(a, rev) == store.Done })
	// Implement, Review and Test leave the work uncommitted; Commit commits it.
	s, _ := a.db.Step(impl)
	if s.SHA != "" || s.Guidance != "keep it small" {
		t.Fatalf("impl step: %+v", s)
	}
	wt := filepath.Join(repo, ".worktrees", tv.Slug)
	next := func(want string) {
		t.Helper()
		waitFor(t, "next: "+want, func() bool {
			v, _ = a.GetTask(tv.ID)
			var l []string
			for _, o := range v.Next.Options {
				l = append(l, o.Label)
			}
			return strings.Join(l, ", ") == want
		})
	}
	next("Test 1, Implement 2, Discuss 1")
	if _, err := a.StartStep(tv.ID, "test", "", 0); err != nil {
		t.Fatal(err)
	}
	waitFor(t, "test done", func() bool { return stepState(a, store.StepID(tv.ID, "test-1")) == store.Done })
	if n := run(t, wt, "git", "rev-list", "--count", "main..HEAD"); n != "0" {
		t.Fatalf("committed before Commit and push: %s commits", n)
	}
	next("Commit and push, Implement 2, Discuss 1")
	// Nothing is committed yet: every step shares the uncommitted work.
	for _, id := range []string{plan, impl, rev, store.StepID(tv.ID, "test-1")} {
		if ch, _ := a.StepChanges(id); ch.Source != "worktree" || len(ch.Files) == 0 || ch.Files[0].Path != "file-1.txt" {
			t.Fatalf("%s changes: %+v", id, ch)
		}
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
	if len(v.Next.Options) != 1 || v.Next.Options[0].Label != "Retry Plan 1" || v.Next.MarkDone != id {
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
	t.Cleanup(func() { b.run.StopAll(); Stop(b); killHolders(t, b.Home) })
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
	reviewCommits(t, a)
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
			if strings.HasPrefix(e, "notify:Plan 1 finished: LOCAL notify me") {
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
	reviewCommits(t, a)
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
	allowed := agentFor(a.settings.Agents(), "test")
	allowed.Allow = []string{"Bash(make test-path:*)", "Bash(make ruff)"}
	if args := strings.Join(a.spec(launch{task: task, proj: p, agent: allowed, step: store.Step{N: 1, SessionID: "s"}}).Args, "\x00"); !strings.Contains(args, "--allowedTools\x00Bash(make test-path:*),Bash(make ruff)\x00") {
		t.Fatalf("allowed tools: %q", args)
	}
	if err := a.writeContextFile(p, task); err != nil {
		t.Fatal(err)
	}
	if b, _ := os.ReadFile(filepath.Join(tv.RunDir, "CONTEXT.md")); !strings.Contains(string(b), kb) {
		t.Fatalf("CONTEXT.md: %s", b)
	}
}

func TestRerunAmendsTheReviewCommit(t *testing.T) {
	a, repo := newTestApp(t)
	reviewCommits(t, a)
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

func TestIssueMarkdown(t *testing.T) {
	var d github.IssueDoc
	if err := json.Unmarshal([]byte(`{"number":12,"title":"Pause ids","body":"Echo them.","state":"OPEN","url":"https://x/12",
		"createdAt":"2026-11-01T10:00:00Z","author":{"login":"ana"},"labels":[{"name":"okai"},{"name":"bug"}],
		"comments":[{"author":{"login":"bo"},"body":"Also the ack.","createdAt":"2026-11-02T09:30:00Z"}]}`), &d); err != nil {
		t.Fatal(err)
	}
	md := issueMarkdown(d, "pony-lab/pony-core")
	for _, want := range []string{"issue: 12\nrepo: pony-lab/pony-core", "state: open", "labels: okai, bug", "# Pause ids\n\nEcho them.", "## Comments", "### bo · 2026-11-02 09:30\n\nAlso the ack."} {
		if !strings.Contains(md, want) {
			t.Errorf("ISSUE.md lacks %q:\n%s", want, md)
		}
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

func TestTestWaitsForTheUser(t *testing.T) {
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
			time.Sleep(300 * time.Millisecond)
			if _, err := a.db.Step(test); err == nil {
				t.Fatal("Test and commit started by itself")
			}
			if status == "done" {
				if v, _ := a.GetTask(tv.ID); v.Next.Options[0].Label != "Test 1" {
					t.Fatalf("next after a passed review: %+v", v.Next)
				}
				return
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
	if _, err := a.removeWorktree(tv.ID); err == nil {
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

func TestSegmentCommit(t *testing.T) {
	at := func(sec int) time.Time { return time.Date(2026, 11, 4, 12, 0, sec, 0, time.UTC) }
	// Two pushes committed at 0:20 and 0:40.
	commits := []commit{{"a1", at(20)}, {"b2", at(40)}}
	steps := []store.Step{
		{ID: "impl-1", StartedAt: at(1)}, {ID: "review-1", StartedAt: at(5)}, {ID: "push-1", StartedAt: at(10)},
		{ID: "copilot-1", StartedAt: at(20).Add(300 * time.Millisecond)},                // started the second push 1 committed
		{ID: "impl-2", StartedAt: at(25)}, {ID: "push-2", StartedAt: at(40), SHA: "b2"}, // committed in the second it started
		{ID: "impl-3", StartedAt: at(45)},
	}
	for i, want := range []string{"a1", "a1", "a1", "b2", "b2", "b2", ""} {
		got := ""
		if c := segmentCommit(commits, steps, i); c != nil {
			got = c.SHA
		}
		if got != want {
			t.Errorf("%s: commit %q, want %q", steps[i].ID, got, want)
		}
	}
}

func TestStatus(t *testing.T) {
	agents := settings.DefaultAgents()
	step := func(agent string, n int, state, verdict string) StepView {
		return StepView{Step: store.Step{Agent: agent, N: n, State: state, Verdict: verdict}, Name: agentFor(agents, agent).DisplayName(n)}
	}
	open := store.Task{PRNumber: 7, PRState: "open"}
	for name, c := range map[string]struct {
		task  store.Task
		steps []StepView
		kind  string
		label string
		ready bool
	}{
		"next":     {store.Task{}, []StepView{step("plan", 1, store.Done, "")}, "waiting", "Plan 1 · Waiting for next step", false},
		"done":     {open, []StepView{step("push", 1, store.Done, ""), {Step: store.Step{Agent: copilotAgent, N: 1, State: store.Done}, Name: "Copilot 1", External: true}}, "done", "Copilot 1 · Success · PR #7", false},
		"failure":  {store.Task{}, []StepView{step("impl", 1, store.Done, ""), step("review", 1, store.Done, store.Failure)}, "failed", "Review 1 · Failure", false},
		"running":  {open, []StepView{step("impl", 2, store.Running, "")}, "running", "Implement 2 · Running · PR #7", false},
		"waiting":  {open, []StepView{step("impl", 2, store.Idle, "")}, "waiting", "Implement 2 · Waiting · PR #7", false},
		"approval": {store.Task{}, []StepView{step("impl", 1, store.Approval, "")}, "approval", "Implement 1 · Waiting", false},
		"ended":    {store.Task{}, []StepView{step("impl", 1, store.Failed, "")}, "failed", "Implement 1 · Failure", false},
		"ready":    {open, []StepView{step("push", 1, store.Done, ""), {Step: store.Step{Agent: copilotAgent, N: 1, State: store.Done}, Name: "Copilot 1", External: true}}, "done", "Copilot 1 · Success · PR #7 Ready", true},
	} {
		if s := statusOf(c.task, c.steps, c.ready); s.Kind != c.kind || s.Label != c.label {
			t.Errorf("%s: %+v, want %s %q", name, s, c.kind, c.label)
		}
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
		"review passed":    {base, store.Task{}, "Test 1, Implement 2, Discuss 1"},
		"review failed":    {[]StepView{step("plan", 1, ""), step("impl", 1, ""), step("review", 1, store.Failure)}, store.Task{}, "Implement 2, Discuss 1"},
		"test failed":      {append(base, step("test", 1, store.Failure)), store.Task{}, "Implement 2, Discuss 1"},
		"test passed":      {append(base, step("test", 1, "")), store.Task{}, "Commit and push, Implement 2, Discuss 1"},
		"implement failed": {[]StepView{step("plan", 1, ""), step("impl", 1, store.Failure)}, store.Task{}, "Implement 2, Discuss 1, Plan 2"},
		"git failed":       {append(base, step("test", 1, ""), StepView{Step: store.Step{Agent: "push", N: 1, State: store.Done, Completed: true}, Name: "Commit and push", GitError: "herd could not push: rejected"}), store.Task{PRNumber: 7}, "Commit and push"},
		"copilot not run":  {append(base, step("test", 1, ""), StepView{Step: store.Step{Agent: "push", N: 1, State: store.Done, Completed: true}, Name: "Commit and push"}), store.Task{PRNumber: 7}, "Commit and push, Implement 2, Discuss 1"},
		"pushing":          {append(base, step("test", 1, ""), StepView{Step: store.Step{Agent: "push", N: 1, State: store.Done}, Name: "Commit and push", Busy: true}), store.Task{PRNumber: 7}, ""},
		"copilot comments": {append(base, step("push", 1, ""), copilotStep(1, store.Done, store.Failure)), store.Task{PRNumber: 7}, "Implement 2, Discuss 1"},
		"copilot waiting":  {append(base, step("push", 1, ""), copilotStep(1, store.Running, "")), store.Task{PRNumber: 7}, "Implement 2, Discuss 1"},
		"discuss done":     {[]StepView{step("plan", 1, ""), step("discuss", 1, "")}, store.Task{}, "Review 1, Implement 1"},
		"discuss failed":   {[]StepView{step("plan", 1, ""), step("discuss", 1, store.Failure)}, store.Task{}, "Discuss 2, Implement 1"},
	} {
		if got := labels(nextOf(c.task, c.steps, agents)); got != c.want {
			t.Errorf("%s: %q, want %q", name, got, c.want)
		}
	}
}

// reviewCommits makes Review commit, as herd's commit mechanics (one diff
// per cycle, amend on rerun) work for any committing agent.
func reviewCommits(t *testing.T, a *App) {
	t.Helper()
	ags := a.settings.Agents()
	for i := range ags {
		if ags[i].ID == "review" {
			ags[i].Commit = true
		}
	}
	if err := a.SaveAgents(ags); err != nil {
		t.Fatal(err)
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

func TestRerunReviewUntilTest(t *testing.T) {
	a, _ := newTestApp(t)
	tv, _ := a.CreateTask("core", "", "rerun the review", "", true, nil)
	waitFor(t, "plan", func() bool { return stepState(a, store.StepID(tv.ID, "plan-1")) == store.Done })
	if _, err := a.StartStep(tv.ID, "impl", "", 0); err != nil {
		t.Fatal(err)
	}
	rev, test := store.StepID(tv.ID, "review-1"), store.StepID(tv.ID, "test-1")
	rerun := func(id string) bool {
		v, _ := a.GetTask(tv.ID)
		for _, s := range v.Steps {
			if s.ID == id {
				return s.Rerun
			}
		}
		return false
	}
	waitFor(t, "Rerun on the latest review", func() bool { return rerun(rev) })

	proj := filepath.Join(os.Getenv("CLAUDE_CONFIG_DIR"), "projects", "-tmp-x")
	os.MkdirAll(proj, 0o755)
	s, _ := a.db.Step(rev)
	os.WriteFile(filepath.Join(proj, s.SessionID+".jsonl"), []byte("{}\n"), 0o644)
	if err := a.RerunStep(rev); err != nil {
		t.Fatal(err)
	}
	waitFor(t, "the review rerun", func() bool { x, _ := a.db.Step(rev); return x.State == store.Done && x.EndedAt.After(s.EndedAt) })
	waitFor(t, "Rerun still on the review", func() bool { return rerun(rev) })
	if _, err := a.db.Step(test); err == nil {
		t.Fatal("Test and commit started by itself")
	}

	if _, err := a.StartStep(tv.ID, "test", "", 0); err != nil {
		t.Fatal(err)
	}
	waitFor(t, "test", func() bool { return stepState(a, test) == store.Done })
	if rerun(rev) || rerun(test) {
		t.Fatal("Rerun offered after Test and commit")
	}
	if err := a.RerunStep(test); err == nil {
		t.Fatal("Test and commit reran")
	}
}

func TestPlanOptionsWarning(t *testing.T) {
	t.Setenv("FAKE_OPTIONS", "1")
	a, _ := newTestApp(t)
	tv, _ := a.CreateTask("core", "", "two ways to do it", "", true, nil)
	waitFor(t, "plan", func() bool { return stepState(a, store.StepID(tv.ID, "plan-1")) == store.Done })
	v, _ := a.GetTask(tv.ID)
	if !strings.Contains(v.Next.Warning, "PLAN-1.md lists several options") || !strings.Contains(v.Next.Warning, "Implement 1") {
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
	if v.Status.Short != "running" || v.Next.MarkDone != id {
		t.Fatalf("waiting: %+v %+v", v.Status, v.Next)
	}
	task, _ := a.db.Task(tv.ID)
	a.finishCopilotStep(task, github.Review{ID: 1, Body: "Looks fine", Comments: []github.Comment{{
		Path: "a.go", Line: 3, Body: "nil check", Title: "Check the error", Severity: "Medium",
		Hunk: "@@ -1,2 +1,3 @@\n x := f()\n+y := x.v", Replies: []github.Reply{{Author: "ant", Body: "on it"}},
	}}})
	s, _ := a.db.Step(id)
	if !s.Unsuccessful() {
		t.Fatalf("a review with comments should fail the step: %+v", s)
	}
	b, _ := os.ReadFile(filepath.Join(tv.RunDir, "COPILOT-1.md"))
	for _, want := range []string{"status: failed", "#### 1. Check the error · Medium", "`a.go:3`", "```diff\n@@ -1,2 +1,3 @@\n x := f()\n+y := x.v\n```", "nil check", "> **ant** replied: on it"} {
		if !strings.Contains(string(b), want) {
			t.Fatalf("COPILOT-1.md lacks %q:\n%s", want, b)
		}
	}
	setChecks := func(cs ...github.Check) {
		a.checksMu.Lock()
		a.checks[tv.ID] = cs
		a.checksMu.Unlock()
	}
	// A later review, with no Copilot step waiting, gets a step of its own,
	// which waits for CI when Copilot found nothing.
	setChecks(github.Check{Workflow: "Tests", Name: "pytest", Bucket: "pending"})
	a.takeCopilotReview(task, github.Review{ID: 2, Body: "All good now"})
	c2 := store.StepID(tv.ID, "copilot-2")
	if stepState(a, c2) != store.Running {
		t.Fatal("copilot-2 ended before CI did")
	}
	setChecks(github.Check{Workflow: "Tests", Name: "pytest", Bucket: "pass"})
	a.settleCopilotStep(task)
	if s2, _ := a.db.Step(c2); !s2.Succeeded() {
		t.Fatalf("copilot-2 after CI passed: %+v", s2)
	}
	if b, _ := os.ReadFile(filepath.Join(tv.RunDir, "COPILOT-2.md")); !strings.Contains(string(b), "All good now") || !strings.Contains(string(b), "All 1 checks passed") {
		t.Fatalf("COPILOT-2.md: %s", b)
	}
	// Copilot is happy but a regtest failed: the step fails and says which.
	setChecks(github.Check{Workflow: "Tests", Name: "pytest", Bucket: "pass"},
		github.Check{Workflow: "Regtests (local stack)", Name: "Regtests 2/4", Bucket: "fail", Link: "https://github.com/x/y/runs/1"})
	a.takeCopilotReview(task, github.Review{ID: 3, Body: "Looks fine"})
	if s3, _ := a.db.Step(store.StepID(tv.ID, "copilot-3")); !s3.Unsuccessful() {
		t.Fatalf("copilot-3 with a failing regtest: %+v", s3)
	}
	if b, _ := os.ReadFile(filepath.Join(tv.RunDir, "COPILOT-3.md")); !strings.Contains(string(b), "**Regtests (local stack) / Regtests 2/4** failed: https://github.com/x/y/runs/1") || !strings.Contains(string(b), "ci: failed") {
		t.Fatalf("COPILOT-3.md: %s", b)
	}
	if _, err := os.Stat(filepath.Join(tv.RunDir, "FEEDBACK.md")); err == nil {
		t.Fatal("FEEDBACK.md written")
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

func TestEditsOutsideTheWorktree(t *testing.T) {
	a, repo := newTestApp(t)
	tv, _ := a.CreateTask("core", "", "edit outside the worktree", "", true, nil)
	id := store.StepID(tv.ID, "plan-1")
	waitFor(t, "plan", func() bool { return stepState(a, id) == store.Done })

	main := filepath.Join(repo, "README.md") // the project's main checkout, not the worktree
	os.WriteFile(main, []byte("hi\nchanged\n"), 0o644)
	hook := func(tool, path string) {
		raw, _ := json.Marshal(map[string]any{"tool_name": tool, "tool_input": map[string]string{"file_path": path}})
		a.onHook(hooks.Event{Name: "PostToolUse", StepID: id, Raw: raw})
	}
	hook("Read", filepath.Join(repo, "other.md"))          // not an edit
	hook("Write", filepath.Join(tv.Worktree, "README.md")) // inside the worktree
	hook("Edit", main)
	task, _ := a.db.Task(tv.ID)
	if got := a.outsideRepos(task); len(got) != 1 || got[0] != "pony-core (main checkout)" {
		t.Fatalf("outside repos: %v", got)
	}
	run(t, repo, "git", "checkout", "README.md") // nothing left uncommitted
	if got := a.outsideRepos(task); len(got) != 0 {
		t.Fatalf("clean repo still listed: %v", got)
	}
}

func TestPlanRepos(t *testing.T) {
	body := "# Goal\nx\n\n## Other repositories\n\n- okai-tcp-server: echo the pause id\n- **FirebaseFunctions** — forward it\n\n## Out of scope\n- nope: not a repo line\n"
	got := planRepos(body)
	if len(got) != 2 || got[0].Repo != "okai-tcp-server" || got[0].Scope != "echo the pause id" || got[1].Repo != "FirebaseFunctions" || got[1].Scope != "forward it" {
		t.Fatalf("plan repos: %+v", got)
	}
	body = "# Goal\nx\n\n---\n\n## Sister tasks\n\n### `okai-tcp-server`\n\nEcho the pause id in the ack.\nKeep the field name.\n\n### FirebaseFunctions\nForward it.\n"
	got = planRepos(body)
	if len(got) != 2 || got[0].Repo != "okai-tcp-server" || got[0].Scope != "Echo the pause id in the ack.\nKeep the field name." || got[1].Repo != "FirebaseFunctions" || got[1].Scope != "Forward it." {
		t.Fatalf("sister tasks: %+v", got)
	}
	if planRepos("# Goal\n- a: b\n") != nil {
		t.Fatal("repos without the section")
	}
}

func TestSisterTask(t *testing.T) {
	t.Setenv("FAKE_SISTER", "okai-tcp-server")
	a, repo := newTestApp(t)
	other := filepath.Join(filepath.Dir(repo), "okai-tcp-server")
	os.MkdirAll(other, 0o755)
	run(t, other, "git", "init", "-q", "-b", "master")
	run(t, other, "git", "-c", "user.email=t@example.com", "-c", "user.name=t", "commit", "-q", "--allow-empty", "-m", "init")

	tv, _ := a.CreateTask("core", "", "pause ids end to end", "", true, nil)
	waitFor(t, "plan", func() bool { return stepState(a, store.StepID(tv.ID, "plan-1")) == store.Done })
	v, _ := a.GetTask(tv.ID)
	if len(v.Next.Sisters) != 1 || v.Next.Sisters[0].Project != "" {
		t.Fatalf("offer before the project exists: %+v", v.Next.Sisters)
	}
	if _, err := a.CreateSister(tv.ID, "okai-tcp-server"); err == nil {
		t.Fatal("created a sister without its project")
	}
	if err := a.SaveProject(settings.Project{ID: "okai", Name: "okai-tcp-server", Path: other, BaseBranch: "master"}); err != nil {
		t.Fatal(err)
	}
	sv, err := a.CreateSister(tv.ID, "okai-tcp-server")
	if err != nil {
		t.Fatal(err)
	}
	if sv.Project != "okai" || sv.Branch != v.Branch {
		t.Fatalf("sister: %+v", sv.Task)
	}
	waitFor(t, "sister plan", func() bool { return stepState(a, store.StepID(sv.ID, "plan-1")) == store.Done })
	md, _ := os.ReadFile(filepath.Join(sv.RunDir, "TASK.md"))
	if !strings.Contains(string(md), "Handle only the okai-tcp-server part: echo the pause id") || !strings.Contains(string(md), filepath.Join(tv.RunDir, "PLAN-1.md")) {
		t.Fatalf("sister TASK.md: %s", md)
	}
	for id, other := range map[string]string{tv.ID: sv.ID, sv.ID: tv.ID} {
		x, _ := a.GetTask(id)
		if len(x.Sisters) != 1 || x.Sisters[0].ID != other || len(x.Next.Sisters) != 0 {
			t.Fatalf("%s: sisters %+v, offers %+v", id, x.Sisters, x.Next.Sisters)
		}
	}
	// Each sister's agents may read the other's run folder and know about it.
	task, _ := a.db.Task(sv.ID)
	spec := a.spec(launch{task: task, proj: a.projectOf(task), agent: agentFor(a.settings.Agents(), "impl"), step: store.Step{ID: sv.ID + "/impl-1", Key: "impl-1", N: 1}})
	if !slices.Contains(spec.Args, tv.RunDir) || !strings.Contains(strings.Join(spec.Args, " "), "sister task "+tv.ID+" handles the pony-core part") {
		t.Fatalf("sister spec: %v", spec.Args)
	}
}

func TestRevertStep(t *testing.T) {
	a, _ := newTestApp(t)
	tv, _ := a.CreateTask("core", "", "revert an implement", "", true, nil)
	plan := store.StepID(tv.ID, "plan-1")
	waitFor(t, "plan", func() bool { return stepState(a, plan) == store.Done })
	wt := tv.Worktree
	os.WriteFile(filepath.Join(wt, "keep.txt"), []byte("mine\n"), 0o644) // before Implement: survives
	if _, err := a.StartStep(tv.ID, "impl", "", 0); err != nil {
		t.Fatal(err)
	}
	impl, rev := store.StepID(tv.ID, "impl-1"), store.StepID(tv.ID, "review-1")
	waitFor(t, "review", func() bool { return stepState(a, rev) == store.Done })
	if _, err := a.StartStep(tv.ID, "test", "", 0); err != nil {
		t.Fatal(err)
	}
	test := store.StepID(tv.ID, "test-1")
	waitFor(t, "test", func() bool { return stepState(a, test) == store.Done })
	os.WriteFile(filepath.Join(wt, "by-hand.txt"), []byte("x\n"), 0o644)
	os.Remove(filepath.Join(wt, "README.md"))

	v, _ := a.GetTask(tv.ID)
	revert := map[string]bool{}
	for _, s := range v.Steps {
		revert[s.ID] = s.Revert
	}
	if revert[plan] || !revert[impl] || revert[rev] || revert[test] { // only the latest Implement
		t.Fatalf("revert offered: %v", revert)
	}
	if err := a.RevertStep(impl); err != nil {
		t.Fatal(err)
	}
	for f, want := range map[string]bool{"keep.txt": true, "README.md": true, "file-1.txt": false, "by-hand.txt": false} {
		if _, err := os.Stat(filepath.Join(wt, f)); (err == nil) != want {
			t.Errorf("%s exists=%v after the revert", f, err == nil)
		}
	}
	steps, _ := a.db.Steps(tv.ID)
	if len(steps) != 1 || steps[0].ID != plan {
		t.Fatalf("steps after the revert: %+v", steps)
	}
	if _, err := os.Stat(filepath.Join(tv.RunDir, "IMPLEMENT-1.md")); err == nil {
		t.Fatal("IMPLEMENT-1.md left behind")
	}
	if v, _ := a.GetTask(tv.ID); v.Next.Options[0].Label != "Implement 1" {
		t.Fatalf("next after the revert: %+v", v.Next.Options)
	}
}

func TestNoRevertOnceCommitted(t *testing.T) {
	a, _ := newTestApp(t)
	reviewCommits(t, a)
	tv, _ := a.CreateTask("core", "", "committed work", "", true, nil)
	waitFor(t, "plan", func() bool { return stepState(a, store.StepID(tv.ID, "plan-1")) == store.Done })
	a.StartStep(tv.ID, "impl", "", 0)
	rev := store.StepID(tv.ID, "review-1")
	waitFor(t, "review commit", func() bool { s, _ := a.db.Step(rev); return s.SHA != "" })
	waitFor(t, "view", func() bool {
		v, _ := a.GetTask(tv.ID)
		return len(v.Steps) == 3 && v.Steps[2].State == store.Done
	})
	v, _ := a.GetTask(tv.ID)
	for _, s := range v.Steps {
		if s.Revert {
			t.Fatalf("%s revertable after its commit", s.ID)
		}
	}
	if err := a.RevertStep(rev); err == nil {
		t.Fatal("reverted committed work")
	}
}

// A step that failed, then succeeds in the same conversation, still gets its
// commit, push and automatic next steps.
func TestCompleteAfterAFailure(t *testing.T) {
	a, _ := newTestApp(t)
	tv, _ := a.CreateTask("core", "", "retry in the conversation", "", false, nil)
	id := store.StepID(tv.ID, "test-1")
	_ = a.db.SaveStep(store.Step{ID: id, TaskID: tv.ID, Key: "test-1", Agent: "test", N: 1, Seq: 1, State: store.Done, Verdict: store.Failure})
	a.complete(id)
	if s, _ := a.db.Step(id); s.Completed {
		t.Fatal("a failure completed the step")
	}
	a.updateStep(id, func(s *store.Step) bool { s.Verdict = ""; return true })
	a.complete(id)
	if s, _ := a.db.Step(id); !s.Completed {
		t.Fatal("the success after a failure did not complete the step")
	}
}

func TestCommandUsesPrompts(t *testing.T) {
	a, _ := newTestApp(t)
	ag, _ := a.settings.Agent("plan")
	ag.Sisters = false
	if strings.Contains(a.AgentCommand(ag), "## Sister tasks") {
		t.Fatal("sister rules without the box")
	}
	if err := a.SetPrompt(settings.PromptSisters, "## Sister tasks\n\nEnd {artifact} with them."); err != nil {
		t.Fatal(err)
	}
	ag.Sisters = true
	if !strings.Contains(a.AgentCommand(ag), "End "+ag.Writes+" with them.") {
		t.Fatal("edited sister prompt not used")
	}
	b, _ := os.ReadFile(filepath.Join(a.pluginDir(), "commands", "plan.md"))
	if !strings.Contains(string(b), "End "+ag.Writes+" with them.") {
		t.Fatal("plan.md not rewritten")
	}
}

func TestDiffNotes(t *testing.T) {
	a, _ := newTestApp(t)
	tv, err := a.CreateTask("core", "", "notes", "", false, nil)
	if err != nil {
		t.Fatal(err)
	}
	ns := []Note{{ID: "n1", Path: "a.go", Side: "new", From: 3, To: 4, Code: "+x", Text: "rename"}}
	if err := a.SetNotes(tv.ID, ns); err != nil {
		t.Fatal(err)
	}
	if got := a.notesOf(tv.ID); len(got) != 1 || got[0].Text != "rename" {
		t.Fatalf("notes = %+v", got)
	}
	if err := a.SetNotes(tv.ID, nil); err != nil || len(a.notesOf(tv.ID)) != 0 {
		t.Fatal("notes not cleared")
	}
}

func TestBehindBase(t *testing.T) {
	a, repo := newTestApp(t)
	origin := filepath.Join(filepath.Dir(repo), "origin.git")
	run(t, "", "git", "clone", "-q", "--bare", repo, origin)
	run(t, repo, "git", "remote", "add", "origin", origin)
	run(t, repo, "git", "fetch", "-q", "origin")
	tv, err := a.CreateTask("core", "", "drift", "", false, nil)
	if err != nil {
		t.Fatal(err)
	}
	os.WriteFile(filepath.Join(repo, "new.txt"), []byte("x\n"), 0o644)
	run(t, repo, "git", "add", ".")
	run(t, repo, "git", "commit", "-q", "-m", "on main")
	run(t, repo, "git", "push", "-q", "origin", "main")
	a.pollDrift()
	if n := a.behindOf(tv.ID); n != 1 {
		t.Fatalf("behind = %d, want 1", n)
	}
}

func TestCommonIssuesRepo(t *testing.T) {
	a, repo := newTestApp(t)
	if err := a.SetIssuesRepo("org/issues"); err != nil {
		t.Fatal(err)
	}
	if err := a.SaveProject(settings.Project{ID: "core", Name: "pony-core", Path: repo, Repo: "org/core", BaseBranch: "main", CommonIssues: true}); err != nil {
		t.Fatal(err)
	}
	a.gh.Bin = "false" // no GitHub: the title stays the issue number
	tv, err := a.CreateTask("core", "42", "", "", false, nil)
	if err != nil {
		t.Fatal(err)
	}
	task, _ := a.db.Task(tv.ID)
	if task.IssueRepo != "org/issues" || task.Sister {
		t.Fatalf("task = %+v", task)
	}
	p, _ := a.settings.Project("core")
	block := a.stepBlock(launch{task: task, proj: p, agent: settings.Agent{ID: "push", Name: "Commit and push", Writes: "PR.md"}, step: store.Step{N: 1}}, "/run", "/wt")
	if !strings.Contains(block, "refer to it as org/issues#42") || strings.Contains(block, "sister task") {
		t.Fatalf("step block:\n%s", block)
	}
	task.Sister = true
	if block := a.stepBlock(launch{task: task, proj: p, agent: settings.Agent{ID: "push", Name: "Commit and push", Writes: "PR.md"}, step: store.Step{N: 1}}, "/run", "/wt"); !strings.Contains(block, "only Part of org/issues#42") {
		t.Fatalf("sister step block:\n%s", block)
	}
	if a.SetIssuesRepo("not-a-repo") == nil {
		t.Fatal("bad repository saved")
	}
}
