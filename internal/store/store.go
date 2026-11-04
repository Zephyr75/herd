// Package store keeps tasks, steps and UI state in SQLite (~/.herd/herd.db).
package store

import (
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	_ "modernc.org/sqlite"
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
	IssueRepo       string    `json:"issueRepo"` // the repository holding Issue, when not the project's (a sister task)
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

// DB wraps the SQLite database.
type DB struct{ db *sql.DB }

const schema = `
CREATE TABLE IF NOT EXISTS tasks (
  id TEXT PRIMARY KEY, project TEXT NOT NULL, slug TEXT NOT NULL, issue TEXT NOT NULL DEFAULT '',
  title TEXT NOT NULL DEFAULT '', description TEXT NOT NULL DEFAULT '', port_base INTEGER NOT NULL DEFAULT 0,
  created_at INTEGER NOT NULL, archived INTEGER NOT NULL DEFAULT 0, archived_at INTEGER NOT NULL DEFAULT 0,
  pr_number INTEGER NOT NULL DEFAULT 0, pr_url TEXT NOT NULL DEFAULT '', pr_state TEXT NOT NULL DEFAULT '',
  worktree_removed INTEGER NOT NULL DEFAULT 0
);
CREATE TABLE IF NOT EXISTS steps (
  id TEXT PRIMARY KEY, task_id TEXT NOT NULL REFERENCES tasks(id) ON DELETE CASCADE, key TEXT NOT NULL,
  agent TEXT NOT NULL, n INTEGER NOT NULL, seq INTEGER NOT NULL, state TEXT NOT NULL,
  guidance TEXT NOT NULL DEFAULT '', session_id TEXT NOT NULL DEFAULT '', sha TEXT NOT NULL DEFAULT '',
  verdict TEXT NOT NULL DEFAULT '', model TEXT NOT NULL DEFAULT '', completed INTEGER NOT NULL DEFAULT 0,
  started_at INTEGER NOT NULL DEFAULT 0, ended_at INTEGER NOT NULL DEFAULT 0
);
CREATE INDEX IF NOT EXISTS steps_task ON steps(task_id, seq);
CREATE TABLE IF NOT EXISTS kv (key TEXT PRIMARY KEY, value TEXT NOT NULL);
CREATE TABLE IF NOT EXISTS external_files (
  step_id TEXT NOT NULL REFERENCES steps(id) ON DELETE CASCADE, path TEXT NOT NULL, root TEXT NOT NULL,
  PRIMARY KEY (step_id, path)
);
`

// Open opens (and migrates) the database at path.
func Open(path string) (*DB, error) {
	db, err := sql.Open("sqlite", "file:"+path+"?_pragma=foreign_keys(1)&_pragma=journal_mode(WAL)&_pragma=busy_timeout(5000)")
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)
	if _, err := db.Exec(schema); err != nil {
		db.Close()
		return nil, err
	}
	// Columns added after the first release.
	for _, col := range []string{"branch", "issue_repo"} {
		if _, err := db.Exec(`ALTER TABLE tasks ADD COLUMN ` + col + ` TEXT NOT NULL DEFAULT ''`); err != nil && !strings.Contains(err.Error(), "duplicate column") {
			db.Close()
			return nil, err
		}
	}
	return &DB{db: db}, nil
}

// Close closes the database.
func (d *DB) Close() error { return d.db.Close() }

// ErrNotFound is returned when a row does not exist.
var ErrNotFound = errors.New("not found")

func ts(t time.Time) int64 {
	if t.IsZero() {
		return 0
	}
	return t.UnixMilli()
}

func fromTS(v int64) time.Time {
	if v == 0 {
		return time.Time{}
	}
	return time.UnixMilli(v)
}

func b2i(b bool) int {
	if b {
		return 1
	}
	return 0
}

// SaveTask inserts or replaces a task.
func (d *DB) SaveTask(t Task) error {
	_, err := d.db.Exec(`INSERT INTO tasks (id, project, slug, issue, title, description, created_at, archived, archived_at, pr_number, pr_url, pr_state, worktree_removed, branch, issue_repo)
		VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)
		ON CONFLICT(id) DO UPDATE SET project=excluded.project, slug=excluded.slug, issue=excluded.issue, title=excluded.title,
		description=excluded.description, archived=excluded.archived, archived_at=excluded.archived_at,
		pr_number=excluded.pr_number, pr_url=excluded.pr_url, pr_state=excluded.pr_state, worktree_removed=excluded.worktree_removed, branch=excluded.branch, issue_repo=excluded.issue_repo`,
		t.ID, t.Project, t.Slug, t.Issue, t.Title, t.Description, ts(t.CreatedAt), b2i(t.Archived), ts(t.ArchivedAt),
		t.PRNumber, t.PRURL, t.PRState, b2i(t.WorktreeRemoved), t.Branch, t.IssueRepo)
	return err
}

const taskCols = `id, project, slug, issue, title, description, created_at, archived, archived_at, pr_number, pr_url, pr_state, worktree_removed, branch, issue_repo`

func scanTask(r interface{ Scan(...any) error }) (Task, error) {
	var t Task
	var created, archivedAt int64
	var archived, removed int
	err := r.Scan(&t.ID, &t.Project, &t.Slug, &t.Issue, &t.Title, &t.Description, &created, &archived, &archivedAt,
		&t.PRNumber, &t.PRURL, &t.PRState, &removed, &t.Branch, &t.IssueRepo)
	t.CreatedAt, t.ArchivedAt = fromTS(created), fromTS(archivedAt)
	t.Archived, t.WorktreeRemoved = archived == 1, removed == 1
	return t, err
}

// Task returns one task.
func (d *DB) Task(id string) (Task, error) {
	t, err := scanTask(d.db.QueryRow(`SELECT `+taskCols+` FROM tasks WHERE id = ?`, id))
	if errors.Is(err, sql.ErrNoRows) {
		return t, ErrNotFound
	}
	return t, err
}

// Tasks returns every task, oldest first.
func (d *DB) Tasks() ([]Task, error) {
	rows, err := d.db.Query(`SELECT ` + taskCols + ` FROM tasks ORDER BY created_at, id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Task
	for rows.Next() {
		t, err := scanTask(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, t)
	}
	return out, rows.Err()
}

// DeleteTask removes a task, its steps and the UI state saved for them
// (kv keys "<name>:<task id>" and "<name>:<task id>/<step>").
func (d *DB) DeleteTask(id string) error {
	tx, err := d.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.Exec(`DELETE FROM kv WHERE substr(key, instr(key, ':') + 1) = ? OR substr(key, instr(key, ':') + 1, ?) = ?`,
		id, len(id)+1, id+"/"); err != nil {
		return err
	}
	if _, err := tx.Exec(`DELETE FROM tasks WHERE id = ?`, id); err != nil {
		return err
	}
	return tx.Commit()
}

// SaveStep inserts or replaces a step.
func (d *DB) SaveStep(s Step) error {
	_, err := d.db.Exec(`INSERT INTO steps (id, task_id, key, agent, n, seq, state, guidance, session_id, sha, verdict, model, completed, started_at, ended_at)
		VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)
		ON CONFLICT(id) DO UPDATE SET state=excluded.state, guidance=excluded.guidance, session_id=excluded.session_id, sha=excluded.sha,
		verdict=excluded.verdict, model=excluded.model, completed=excluded.completed, started_at=excluded.started_at, ended_at=excluded.ended_at`,
		s.ID, s.TaskID, s.Key, s.Agent, s.N, s.Seq, s.State, s.Guidance, s.SessionID, s.SHA, s.Verdict, s.Model, b2i(s.Completed),
		ts(s.StartedAt), ts(s.EndedAt))
	return err
}

const stepCols = `id, task_id, key, agent, n, seq, state, guidance, session_id, sha, verdict, model, completed, started_at, ended_at`

func scanStep(r interface{ Scan(...any) error }) (Step, error) {
	var s Step
	var completed int
	var started, ended int64
	err := r.Scan(&s.ID, &s.TaskID, &s.Key, &s.Agent, &s.N, &s.Seq, &s.State, &s.Guidance, &s.SessionID, &s.SHA, &s.Verdict, &s.Model,
		&completed, &started, &ended)
	s.Completed = completed == 1
	s.StartedAt, s.EndedAt = fromTS(started), fromTS(ended)
	return s, err
}

// Step returns one step.
func (d *DB) Step(id string) (Step, error) {
	s, err := scanStep(d.db.QueryRow(`SELECT `+stepCols+` FROM steps WHERE id = ?`, id))
	if errors.Is(err, sql.ErrNoRows) {
		return s, ErrNotFound
	}
	return s, err
}

// Steps returns a task's steps in order.
func (d *DB) Steps(taskID string) ([]Step, error) {
	return d.querySteps(`SELECT `+stepCols+` FROM steps WHERE task_id = ? ORDER BY seq`, taskID)
}

// StepsInStates returns every step in one of the given states.
func (d *DB) StepsInStates(states ...string) ([]Step, error) {
	if len(states) == 0 {
		return nil, nil
	}
	q := `SELECT ` + stepCols + ` FROM steps WHERE state IN (?` + strings.Repeat(",?", len(states)-1) + `) ORDER BY task_id, seq`
	args := make([]any, len(states))
	for i, s := range states {
		args[i] = s
	}
	return d.querySteps(q, args...)
}

func (d *DB) querySteps(q string, args ...any) ([]Step, error) {
	rows, err := d.db.Query(q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Step
	for rows.Next() {
		s, err := scanStep(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, s)
	}
	return out, rows.Err()
}

// NextSeq returns the next step sequence number for a task.
func (d *DB) NextSeq(taskID string) (int, error) {
	var n sql.NullInt64
	if err := d.db.QueryRow(`SELECT MAX(seq) FROM steps WHERE task_id = ?`, taskID).Scan(&n); err != nil {
		return 0, err
	}
	return int(n.Int64) + 1, nil
}

// Get reads a UI state value; "" when unset.
func (d *DB) Get(key string) (string, error) {
	var v string
	err := d.db.QueryRow(`SELECT value FROM kv WHERE key = ?`, key).Scan(&v)
	if errors.Is(err, sql.ErrNoRows) {
		return "", nil
	}
	return v, err
}

// Set writes a UI state value.
func (d *DB) Set(key, value string) error {
	_, err := d.db.Exec(`INSERT INTO kv (key, value) VALUES (?, ?) ON CONFLICT(key) DO UPDATE SET value = excluded.value`, key, value)
	return err
}

// ExternalFile is a file a step edited outside its task's worktree, in the
// git repository at Root.
type ExternalFile struct {
	StepID string
	Path   string
	Root   string
}

// AddExternal records a file a step edited outside its worktree.
func (d *DB) AddExternal(f ExternalFile) error {
	_, err := d.db.Exec(`INSERT OR IGNORE INTO external_files (step_id, path, root) VALUES (?, ?, ?)`, f.StepID, f.Path, f.Root)
	return err
}

// Externals lists the files a task's steps edited outside its worktree.
func (d *DB) Externals(taskID string) ([]ExternalFile, error) {
	rows, err := d.db.Query(`SELECT e.step_id, e.path, e.root FROM external_files e JOIN steps s ON s.id = e.step_id
		WHERE s.task_id = ? ORDER BY e.root, e.path`, taskID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []ExternalFile
	for rows.Next() {
		var f ExternalFile
		if err := rows.Scan(&f.StepID, &f.Path, &f.Root); err != nil {
			return nil, err
		}
		out = append(out, f)
	}
	return out, rows.Err()
}

// StepID builds a step id from its task and key.
func StepID(taskID, key string) string { return fmt.Sprintf("%s/%s", taskID, key) }
