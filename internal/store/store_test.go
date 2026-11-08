package store

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestFiles(t *testing.T) {
	home := t.TempDir()
	pathFor := func(t Task) string { return filepath.Join(home, "runs", t.Project, t.Slug, TaskFile) }
	d, err := Open(home, pathFor)
	if err != nil {
		t.Fatal(err)
	}
	task := Task{ID: "core:12", Project: "core", Slug: "12", Title: "pause ids", Branch: "ar-pause-ids", CreatedAt: time.Now()}
	if err := d.SaveTask(task); err != nil {
		t.Fatal(err)
	}
	for i, k := range []string{"plan-1", "impl-1", "review-1"} {
		if err := d.SaveStep(Step{ID: StepID(task.ID, k), TaskID: task.ID, Key: k, Seq: i + 1, State: Running}); err != nil {
			t.Fatal(err)
		}
	}
	d.SaveStep(Step{ID: StepID(task.ID, "plan-1"), TaskID: task.ID, Key: "other", Seq: 9, State: Done, SHA: "abc"})
	d.Set("snap:core:12/impl-1", "tree")
	d.Set("sisters:core:12", `["okai:12"]`)
	d.Set("tabs", "[]")
	d.AddExternal(ExternalFile{StepID: StepID(task.ID, "review-1"), Path: "/x/a.go", Root: "/x"})
	if err := d.SaveStep(Step{ID: "nope:1/a-1", TaskID: "nope:1"}); err == nil {
		t.Fatal("a step of a missing task was saved")
	}

	// Everything comes back from the files.
	d, err = Open(home, pathFor)
	if err != nil {
		t.Fatal(err)
	}
	got, err := d.Task(task.ID)
	if err != nil || got.Branch != "ar-pause-ids" || got.Title != "pause ids" {
		t.Fatalf("task %+v %v", got, err)
	}
	steps, _ := d.Steps(task.ID)
	if len(steps) != 3 || steps[0].Key != "plan-1" || steps[0].Seq != 1 || steps[0].State != Done || steps[0].SHA != "abc" {
		t.Fatalf("steps %+v", steps)
	}
	if n, _ := d.NextSeq(task.ID); n != 4 {
		t.Fatalf("next seq %d", n)
	}
	for k, want := range map[string]string{"snap:core:12/impl-1": "tree", "sisters:core:12": `["okai:12"]`, "tabs": "[]", "paused:core:12/plan-1": ""} {
		if v, _ := d.Get(k); v != want {
			t.Errorf("%s = %q, want %q", k, v, want)
		}
	}
	if b, _ := os.ReadFile(filepath.Join(home, "state.json")); string(b) != "{\n  \"tabs\": \"[]\"\n}\n" {
		t.Errorf("state.json %q", b)
	}
	if running, _ := d.StepsInStates(Running); len(running) != 2 {
		t.Fatalf("running %+v", running)
	}

	// Revert: steps from impl-1 on go, with their outside edits.
	d.DeleteStepsFrom(task.ID, 2)
	if steps, _ := d.Steps(task.ID); len(steps) != 1 {
		t.Fatalf("after delete %+v", steps)
	}
	if ex, _ := d.Externals(task.ID); len(ex) != 0 {
		t.Fatalf("externals %+v", ex)
	}
	if err := d.DeleteTask(task.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(pathFor(task)); !os.IsNotExist(err) || len(must(d.Tasks())) != 0 {
		t.Fatal("task left behind")
	}
}

func must[T any](v T, _ error) T { return v }
