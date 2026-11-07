package app

// ISSUE.md: herd copies the task's GitHub issue, comments included, to the
// run directory, so every agent reads the same text without calling GitHub.
// It is copied when the task is created, before each run of the first agent,
// and with Refresh issue.

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"herd/internal/github"
	"herd/internal/settings"
	"herd/internal/store"
)

const issueFile = "ISSUE.md"

// issueMarkdown renders an issue and its comments.
func issueMarkdown(d github.IssueDoc, repo string) string {
	var b strings.Builder
	var labels []string
	for _, l := range d.Labels {
		labels = append(labels, l.Name)
	}
	fmt.Fprintf(&b, "---\nissue: %d\nrepo: %s\nurl: %s\nstate: %s\nauthor: %s\nlabels: %s\ncreated: %s\n---\n\n",
		d.Number, repo, d.URL, strings.ToLower(d.State), d.Author.Login, strings.Join(labels, ", "), d.CreatedAt.Format("2006-01-02"))
	fmt.Fprintf(&b, "# %s\n\n", d.Title)
	if body := strings.TrimSpace(d.Body); body != "" {
		b.WriteString(body + "\n")
	} else {
		b.WriteString("(no description)\n")
	}
	if len(d.Comments) > 0 {
		b.WriteString("\n## Comments\n")
		for _, c := range d.Comments {
			fmt.Fprintf(&b, "\n### %s · %s\n\n%s\n", c.Author.Login, c.CreatedAt.Format("2006-01-02 15:04"), strings.TrimSpace(c.Body))
		}
	}
	return b.String()
}

// writeIssueFile copies the task's issue to ISSUE.md; a task without an
// issue has none.
func (a *App) writeIssueFile(p settings.Project, t store.Task) error {
	path := filepath.Join(a.runDir(p, t), issueFile)
	if t.Issue == "" {
		_ = os.Remove(path)
		return nil
	}
	d, err := a.gh.Issue(a.ctx, a.issueRepo(t), t.Issue)
	if err != nil {
		return err
	}
	return os.WriteFile(path, []byte(issueMarkdown(d, a.issueRepo(t))), 0o644)
}

// RefreshIssue copies the task's issue again (Refresh issue in its header).
func (a *App) RefreshIssue(taskID string) error {
	t, err := a.db.Task(taskID)
	if err != nil {
		return err
	}
	if t.Issue == "" {
		return fmt.Errorf("this task has no GitHub issue")
	}
	p := a.projectOf(t)
	if err := a.writeIssueFile(p, t); err != nil {
		return fmt.Errorf("could not copy issue #%s: %w", t.Issue, err)
	}
	if err := a.writeTaskFile(p, t); err != nil {
		return err
	}
	a.emitTask(t.ID)
	return nil
}
