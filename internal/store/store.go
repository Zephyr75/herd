// Package store keeps tasks, steps and their state in files: one JSON file
// per task, in the task's run directory (.herd-task.json: the task, its
// steps, its values and the files its steps edited outside the worktree),
// and the values tied to no task in ~/.herd/state.json. herd is the only
// writer: a lock and atomic writes are enough.
package store

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

// Task is one unit of work in one project.
type Task struct {
	ID              string    `json:"id"` // <project>:<slug>
	Project         string    `json:"project"`
	Slug            string    `json:"slug"` // issue number or local-<words>
	Issue           string    `json:"issue"`
	Title           string    `json:"title"`
	Description     string    `json:"description"`
	Branch          string    `json:"-"`         // "" for tasks made before branches were named: agent/<slug>
	IssueRepo       string    `json:"issueRepo"` // the repository holding Issue, when not the project's (a common issues repository, or a sister task's)
	Sister          bool      `json:"sister"`    // created as a sister task: another task closes the issue
	CreatedAt       time.Time `json:"createdAt"`
	Archived        bool      `json:"archived"`
	ArchivedAt      time.Time `json:"archivedAt"`
	PRNumber        int       `json:"pr"`
	PRURL           string    `json:"prUrl"`
	PRState         string    `json:"prState"` // open | merged | closed
	WorktreeRemoved bool      `json:"worktreeRemoved"`
}

// Step states. Done: the agent finished, and succeeded unless Verdict is
// Failure. Failed: it ended before finishing.
const (
	Running  = "running"
	Idle     = "idle"
	Approval = "approval"
	Done     = "done"
	Failed   = "failed"
)

// Failure is the Verdict of a finished step whose agent reported status:
// failed, e.g. a review asking for changes or a test showing a bug.
const Failure = "failed"

// Unsuccessful reports whether the step finished with a failure ("changes"
// is how reviews failed before steps had outcomes).
func (s Step) Unsuccessful() bool {
	return s.State == Done && (s.Verdict == Failure || s.Verdict == "changes")
}

// Succeeded reports whether the step finished without failing.
func (s Step) Succeeded() bool { return s.State == Done && !s.Unsuccessful() }

// Step is one run of an agent on a task.
type Step struct {
	ID        string    `json:"id"` // <task id>/<key>
	TaskID    string    `json:"taskId"`
	Key       string    `json:"key"` // e.g. impl-2
	Agent     string    `json:"agent"`
	N         int       `json:"n"`
	Seq       int       `json:"seq"`
	State     string    `json:"state"`
	Guidance  string    `json:"guidance"`
	SessionID string    `json:"session"`
	SHA       string    `json:"sha"`
	Verdict   string    `json:"verdict"`
	Model     string    `json:"model"`
	Completed bool      `json:"completed"` // post-done actions ran
	StartedAt time.Time `json:"startedAt"`
	EndedAt   time.Time `json:"endedAt"`
}

// TaskFile is the name of a task's file in its run directory.
const TaskFile = ".herd-task.json"

// DB holds every task in memory and writes each change to its file.
type DB struct {
	mu      sync.Mutex
	home    string
	pathFor func(Task) string // where a new task's file goes
	tasks   map[string]*record
	global  map[string]string
}

// record is a task's file.
type record struct {
	path     string
	Task     taskJSON          `json:"task"`
	Steps    []Step            `json:"steps"`
	Values   map[string]string `json:"values,omitempty"`
	External []ExternalFile    `json:"external,omitempty"`
}

// taskJSON is Task as stored: every field, Branch included.
type taskJSON struct {
	ID              string    `json:"id"`
	Project         string    `json:"project"`
	Slug            string    `json:"slug"`
	Issue           string    `json:"issue,omitempty"`
	IssueRepo       string    `json:"issueRepo,omitempty"`
	Sister          bool      `json:"sister,omitempty"`
	Title           string    `json:"title"`
	Description     string    `json:"description,omitempty"`
	Branch          string    `json:"branch,omitempty"`
	CreatedAt       time.Time `json:"createdAt"`
	Archived        bool      `json:"archived,omitempty"`
	ArchivedAt      time.Time `json:"archivedAt,omitzero"`
	PRNumber        int       `json:"pr,omitempty"`
	PRURL           string    `json:"prUrl,omitempty"`
	PRState         string    `json:"prState,omitempty"`
	WorktreeRemoved bool      `json:"worktreeRemoved,omitempty"`
}

func (j taskJSON) task() Task {
	return Task{ID: j.ID, Project: j.Project, Slug: j.Slug, Issue: j.Issue, IssueRepo: j.IssueRepo, Sister: j.Sister, Title: j.Title,
		Description: j.Description, Branch: j.Branch, CreatedAt: j.CreatedAt, Archived: j.Archived, ArchivedAt: j.ArchivedAt,
		PRNumber: j.PRNumber, PRURL: j.PRURL, PRState: j.PRState, WorktreeRemoved: j.WorktreeRemoved}
}

func toJSON(t Task) taskJSON {
	return taskJSON{ID: t.ID, Project: t.Project, Slug: t.Slug, Issue: t.Issue, IssueRepo: t.IssueRepo, Sister: t.Sister, Title: t.Title,
		Description: t.Description, Branch: t.Branch, CreatedAt: t.CreatedAt, Archived: t.Archived, ArchivedAt: t.ArchivedAt,
		PRNumber: t.PRNumber, PRURL: t.PRURL, PRState: t.PRState, WorktreeRemoved: t.WorktreeRemoved}
}

// Open loads the task files under home/runs and home/state.json. pathFor
// says where a new task's file goes (its run directory).
func Open(home string, pathFor func(Task) string) (*DB, error) {
	d := &DB{home: home, pathFor: pathFor, tasks: map[string]*record{}, global: map[string]string{}}
	files, _ := filepath.Glob(filepath.Join(home, "runs", "*", "*", TaskFile))
	for _, f := range files {
		b, err := os.ReadFile(f)
		if err != nil {
			return nil, err
		}
		r := &record{path: f}
		if err := json.Unmarshal(b, r); err != nil {
			return nil, fmt.Errorf("%s: %w", f, err)
		}
		d.tasks[r.Task.ID] = r
	}
	if b, err := os.ReadFile(d.statePath()); err == nil {
		if err := json.Unmarshal(b, &d.global); err != nil {
			return nil, fmt.Errorf("state.json: %w", err)
		}
	}
	return d, nil
}

// Close is a no-op: every change is already written.
func (d *DB) Close() error { return nil }

// ErrNotFound is returned when a task or a step does not exist.
var ErrNotFound = errors.New("not found")

func (d *DB) statePath() string { return filepath.Join(d.home, "state.json") }

// writeJSON writes v to path atomically: a crash leaves the old file or the new one.
func writeJSON(path string, v any) error {
	b, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), ".tmp-*")
	if err != nil {
		return err
	}
	if _, err := tmp.Write(append(b, '\n')); err != nil {
		tmp.Close()
		os.Remove(tmp.Name())
		return err
	}
	if err := tmp.Close(); err != nil {
		os.Remove(tmp.Name())
		return err
	}
	return os.Rename(tmp.Name(), path)
}

func (d *DB) save(r *record) error { return writeJSON(r.path, r) }

// taskOfStep is the task id of a step id: "pony-core:12" for
// "pony-core:12/impl-1". valueOwner uses it on a key without its name.
func taskOfStep(id string) string {
	if i := strings.Index(id, "/"); i >= 0 {
		return id[:i]
	}
	return id
}

func (d *DB) valueOwner(key string) *record {
	i := strings.Index(key, ":")
	if i < 0 {
		return nil
	}
	return d.tasks[taskOfStep(key[i+1:])]
}

// SaveTask inserts or replaces a task.
func (d *DB) SaveTask(t Task) error {
	d.mu.Lock()
	defer d.mu.Unlock()
	r := d.tasks[t.ID]
	if r == nil {
		r = &record{path: d.pathFor(t)}
		d.tasks[t.ID] = r
	}
	r.Task = toJSON(t)
	return d.save(r)
}

// Task returns one task.
func (d *DB) Task(id string) (Task, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	r := d.tasks[id]
	if r == nil {
		return Task{}, ErrNotFound
	}
	return r.Task.task(), nil
}

// Tasks returns every task, oldest first.
func (d *DB) Tasks() ([]Task, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	out := make([]Task, 0, len(d.tasks))
	for _, r := range d.tasks {
		out = append(out, r.Task.task())
	}
	sort.Slice(out, func(i, j int) bool {
		if !out[i].CreatedAt.Equal(out[j].CreatedAt) {
			return out[i].CreatedAt.Before(out[j].CreatedAt)
		}
		return out[i].ID < out[j].ID
	})
	return out, nil
}

// DeleteTask forgets a task: its file goes, with its steps and values.
func (d *DB) DeleteTask(id string) error {
	d.mu.Lock()
	defer d.mu.Unlock()
	r := d.tasks[id]
	if r == nil {
		return nil
	}
	delete(d.tasks, id)
	if err := os.Remove(r.path); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	return nil
}

// SaveStep inserts a step, or updates its state (its key, agent, number and
// place stay).
func (d *DB) SaveStep(s Step) error {
	d.mu.Lock()
	defer d.mu.Unlock()
	r := d.tasks[s.TaskID]
	if r == nil {
		return fmt.Errorf("step %s: task %w", s.ID, ErrNotFound)
	}
	for i := range r.Steps {
		if r.Steps[i].ID == s.ID {
			x := &r.Steps[i]
			x.State, x.Guidance, x.SessionID, x.SHA, x.Verdict, x.Model = s.State, s.Guidance, s.SessionID, s.SHA, s.Verdict, s.Model
			x.Completed, x.StartedAt, x.EndedAt = s.Completed, s.StartedAt, s.EndedAt
			return d.save(r)
		}
	}
	r.Steps = append(r.Steps, s)
	sort.SliceStable(r.Steps, func(i, j int) bool { return r.Steps[i].Seq < r.Steps[j].Seq })
	return d.save(r)
}

// Step returns one step.
func (d *DB) Step(id string) (Step, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	if r := d.tasks[taskOfStep(id)]; r != nil {
		for _, s := range r.Steps {
			if s.ID == id {
				return s, nil
			}
		}
	}
	return Step{}, ErrNotFound
}

// Steps returns a task's steps in order.
func (d *DB) Steps(taskID string) ([]Step, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	r := d.tasks[taskID]
	if r == nil {
		return nil, nil
	}
	return append([]Step(nil), r.Steps...), nil
}

// StepsInStates returns every step in one of the given states.
func (d *DB) StepsInStates(states ...string) ([]Step, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	var out []Step
	for _, r := range d.tasks {
		for _, s := range r.Steps {
			for _, st := range states {
				if s.State == st {
					out = append(out, s)
				}
			}
		}
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].TaskID != out[j].TaskID {
			return out[i].TaskID < out[j].TaskID
		}
		return out[i].Seq < out[j].Seq
	})
	return out, nil
}

// NextSeq returns the next step sequence number for a task.
func (d *DB) NextSeq(taskID string) (int, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	n := 0
	if r := d.tasks[taskID]; r != nil {
		for _, s := range r.Steps {
			n = max(n, s.Seq)
		}
	}
	return n + 1, nil
}

// DeleteStepsFrom removes a task's steps from seq on, with the outside
// edits they recorded.
func (d *DB) DeleteStepsFrom(taskID string, seq int) error {
	d.mu.Lock()
	defer d.mu.Unlock()
	r := d.tasks[taskID]
	if r == nil {
		return nil
	}
	gone := map[string]bool{}
	steps := r.Steps[:0]
	for _, s := range r.Steps {
		if s.Seq >= seq {
			gone[s.ID] = true
		} else {
			steps = append(steps, s)
		}
	}
	r.Steps = steps
	ext := r.External[:0]
	for _, f := range r.External {
		if !gone[f.StepID] {
			ext = append(ext, f)
		}
	}
	r.External = ext
	return d.save(r)
}

// Get reads a value: "<name>:<task id>" and "<name>:<step id>" keys live in
// their task's file, the others in state.json; "" when unset.
func (d *DB) Get(key string) (string, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	if r := d.valueOwner(key); r != nil {
		return r.Values[key], nil
	}
	return d.global[key], nil
}

// Set writes a value ("" removes it).
func (d *DB) Set(key, value string) error {
	d.mu.Lock()
	defer d.mu.Unlock()
	m, save := d.global, func() error { return writeJSON(d.statePath(), d.global) }
	if r := d.valueOwner(key); r != nil {
		if r.Values == nil {
			r.Values = map[string]string{}
		}
		m, save = r.Values, func() error { return d.save(r) }
	}
	if m[key] == value {
		return nil
	}
	if value == "" {
		delete(m, key)
	} else {
		m[key] = value
	}
	return save()
}

// ExternalFile is a file a step edited outside its task's worktree, in the
// git repository at Root.
type ExternalFile struct {
	StepID string `json:"step"`
	Path   string `json:"path"`
	Root   string `json:"root"`
}

// AddExternal records a file a step edited outside its worktree.
func (d *DB) AddExternal(f ExternalFile) error {
	d.mu.Lock()
	defer d.mu.Unlock()
	r := d.tasks[taskOfStep(f.StepID)]
	if r == nil {
		return ErrNotFound
	}
	for _, x := range r.External {
		if x.StepID == f.StepID && x.Path == f.Path {
			return nil
		}
	}
	r.External = append(r.External, f)
	return d.save(r)
}

// Externals lists the files a task's steps edited outside its worktree.
func (d *DB) Externals(taskID string) ([]ExternalFile, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	r := d.tasks[taskID]
	if r == nil {
		return nil, nil
	}
	out := append([]ExternalFile(nil), r.External...)
	sort.Slice(out, func(i, j int) bool {
		if out[i].Root != out[j].Root {
			return out[i].Root < out[j].Root
		}
		return out[i].Path < out[j].Path
	})
	return out, nil
}

// StepID builds a step id from its task and key.
func StepID(taskID, key string) string { return fmt.Sprintf("%s/%s", taskID, key) }
