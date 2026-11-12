package app

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"herd/internal/github"
)

// The outdated check: GitHub marks a review thread outdated once a push
// changes the lines it points at, which says nothing about whether the
// comment was handled. A quick Claude run (Copilot's check model) reads the
// current code and says; the user resolves the thread from that.

// OutdatedCheck is what the check found for a thread.
type OutdatedCheck struct {
	Resolved bool      `json:"resolved"` // the current code handles the comment
	Why      string    `json:"why"`
	At       time.Time `json:"at"`
}

func outdatedKey(thread string) string { return "outdated:" + thread }

// outdatedChecks lists the checks made on the open outdated threads.
func (a *App) outdatedChecks(fs []github.Feedback) map[string]OutdatedCheck {
	out := map[string]OutdatedCheck{}
	for _, f := range fs {
		if !f.Outdated || f.ResolvedBy != "" {
			continue
		}
		var c OutdatedCheck
		if raw, _ := a.db.Get(outdatedKey(f.ID)); raw != "" && json.Unmarshal([]byte(raw), &c) == nil {
			out[f.ID] = c
		}
	}
	return out
}

// CheckOutdated asks Claude whether the current code handles the comment of
// an outdated review thread of the task's PR. It changes nothing: the user
// resolves the thread.
func (a *App) CheckOutdated(taskID, thread string) (OutdatedCheck, error) {
	t, err := a.db.Task(taskID)
	if err != nil {
		return OutdatedCheck{}, err
	}
	var f *github.Feedback
	for _, x := range a.feedbackOf(t.ID) {
		if x.ID == thread {
			f = &x
		}
	}
	if f == nil {
		return OutdatedCheck{}, errors.New("this comment is no longer on the PR: refresh the PR tab")
	}
	ctx, cancel := context.WithTimeout(a.ctx, 5*time.Minute)
	defer cancel()
	ag := agentFor(a.settings.Agents(), copilotAgent)
	sys := a.withRules(fmt.Sprintf("# herd\nYou check one review comment of PR #%d, in the worktree of the task %s. Read only: never change a file, commit or push.", t.PRNumber, t.ID))
	out, err := a.headless(ctx, t, ag.CheckModel, sys, outdatedPrompt(*f)).Output()
	if err != nil {
		return OutdatedCheck{}, fmt.Errorf("the check did not finish: %w", err)
	}
	c, err := parseOutdated(string(out))
	if err != nil {
		return OutdatedCheck{}, err
	}
	b, _ := json.Marshal(c)
	_ = a.db.Set(outdatedKey(thread), string(b))
	a.emitTask(t.ID)
	return c, nil
}

func outdatedPrompt(f github.Feedback) string {
	var b strings.Builder
	fmt.Fprintf(&b, "A reviewer left this comment on %s, line %d. GitHub marks it outdated: a later push changed the code it pointed at.\n\n", f.Path, f.Line)
	fmt.Fprintf(&b, "Comment by %s:\n%s\n", f.Author, f.Body)
	for _, r := range f.Replies {
		fmt.Fprintf(&b, "\nReply by %s:\n%s\n", r.Author, r.Body)
	}
	if f.Hunk != "" {
		fmt.Fprintf(&b, "\nThe code it was left on:\n```diff\n%s\n```\n", f.Hunk)
	}
	fmt.Fprintf(&b, "\nRead the current %s in this worktree (git log -p -- %s shows how it changed). Decide whether the current code handles what the comment asks. Be quick: look only at what the comment is about.\n", f.Path, f.Path)
	b.WriteString("Answer with exactly two lines and nothing else: RESOLVED or NOT RESOLVED, then one short sentence saying why.")
	return b.String()
}

// parseOutdated reads the check's answer: RESOLVED or NOT RESOLVED, then why.
func parseOutdated(out string) (OutdatedCheck, error) {
	var lines []string
	for _, l := range strings.Split(out, "\n") {
		if l = strings.TrimSpace(strings.Trim(strings.TrimSpace(l), "*`")); l != "" {
			lines = append(lines, l)
		}
	}
	for i, l := range lines {
		up := strings.ToUpper(l)
		var c OutdatedCheck
		switch {
		case strings.HasPrefix(up, "NOT RESOLVED"):
		case strings.HasPrefix(up, "RESOLVED"):
			c.Resolved = true
		default:
			continue
		}
		c.Why, c.At = strings.Join(lines[i+1:], " "), time.Now()
		return c, nil
	}
	return OutdatedCheck{}, fmt.Errorf("the check gave no answer: %q", lastLines(strings.TrimSpace(out), 2))
}
