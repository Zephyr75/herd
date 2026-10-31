package app

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"herd/internal/settings"
	"herd/internal/store"
)

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
	fake, _ := filepath.Abs("testdata/fake-claude.sh")
	a := New(home, nil)
	if err := Start(a, context.Background(), &fakePlatform{}); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { Stop(a) })
	if err := a.settings.UpdateConfig(func(c *settings.Config) { c.ClaudeCommand = fake }); err != nil {
		t.Fatal(err)
	}
	if err := a.SaveProject(settings.Project{ID: "core", Name: "pony-core", Path: repo, BaseBranch: "main", PortRange: "7100-7199"}); err != nil {
		t.Fatal(err)
	}
	return a, repo
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
	waitFor(t, "review done", func() bool { return stepState(a, rev) == store.Done })
	s, _ := a.db.Step(impl)
	if s.SHA == "" || s.Guidance != "keep it small" {
		t.Fatalf("impl step: %+v", s)
	}
	if msg := run(t, filepath.Join(repo, ".worktrees", tv.Slug), "git", "log", "-1", "--format=%s"); msg != "["+tv.Slug+"] implement-1" {
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

	// The review's command got the implement sha.
	sess, ok := a.run.Get(rev)
	if !ok {
		t.Fatal("review session not live")
	}
	_, back := sess.Attach()
	if !strings.Contains(string(back), "/herd:review --local "+s.SHA) {
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
	ags = append([]settings.Agent{{ID: "ask", Name: "Ask", Command: "/herd:ask {task}", Writes: "ASK.md", Done: "status: done", Permissions: "manual"}}, ags...)
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
	// Opening the failed step resumes its conversation.
	if _, err := a.TerminalURL(id); err != nil {
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

func TestPermissionMode(t *testing.T) {
	if permissionMode("readonly") != "dontAsk" || permissionMode("acceptEdits") != "acceptEdits" || permissionMode("") != "manual" {
		t.Fatal("permission modes")
	}
}
