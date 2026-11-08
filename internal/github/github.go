// Package github shells out to gh for issues, pull requests and reviews.
package github

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"os/exec"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"
)

// GH runs the gh CLI.
type GH struct {
	Bin string
	Log func(line string)
}

func (g GH) run(ctx context.Context, dir string, args ...string) (string, error) {
	return g.runIn(ctx, dir, "", args...)
}

// runIn is run with stdin (e.g. a PR body for --body-file -).
func (g GH) runIn(ctx context.Context, dir, stdin string, args ...string) (string, error) {
	bin := g.Bin
	if bin == "" {
		bin = "gh"
	}
	if g.Log != nil {
		g.Log("$ gh " + strings.Join(args, " "))
	}
	ctx, cancel := context.WithTimeout(ctx, 2*time.Minute)
	defer cancel()
	cmd := exec.CommandContext(ctx, bin, args...)
	cmd.Dir = dir
	if stdin != "" {
		cmd.Stdin = strings.NewReader(stdin)
	}
	var out, errb bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &errb
	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("gh %s: %w: %s", args[0], err, strings.TrimSpace(errb.String()))
	}
	return strings.TrimSpace(out.String()), nil
}

// output is run, but keeps stdout when gh exits with an error: gh pr checks
// exits non-zero while checks fail (1) or are pending (8).
func (g GH) output(ctx context.Context, args ...string) (string, error) {
	bin := g.Bin
	if bin == "" {
		bin = "gh"
	}
	ctx, cancel := context.WithTimeout(ctx, 2*time.Minute)
	defer cancel()
	var out, errb bytes.Buffer
	cmd := exec.CommandContext(ctx, bin, args...)
	cmd.Stdout, cmd.Stderr = &out, &errb
	err := cmd.Run()
	if err != nil {
		err = fmt.Errorf("gh %s: %w: %s", args[0], err, strings.TrimSpace(errb.String()))
	}
	return strings.TrimSpace(out.String()), err
}

// Available reports whether gh is installed and authenticated.
func (g GH) Available(ctx context.Context) (bool, string) {
	if _, err := exec.LookPath(orDefault(g.Bin, "gh")); err != nil {
		return false, "gh is not on PATH"
	}
	if _, err := g.run(ctx, "", "auth", "status"); err != nil {
		return false, "gh is not authenticated: run gh auth login"
	}
	return true, ""
}

func orDefault(s, d string) string {
	if s == "" {
		return d
	}
	return s
}

// IssueTitle returns an issue's title.
func (g GH) IssueTitle(ctx context.Context, repo, issue string) (string, error) {
	return g.run(ctx, "", "issue", "view", issue, "--repo", repo, "--json", "title", "-q", ".title")
}

// IssueDoc is an issue with its comments, as herd copies it to ISSUE.md.
type IssueDoc struct {
	Number    int       `json:"number"`
	Title     string    `json:"title"`
	Body      string    `json:"body"`
	State     string    `json:"state"`
	URL       string    `json:"url"`
	CreatedAt time.Time `json:"createdAt"`
	Author    struct {
		Login string `json:"login"`
	} `json:"author"`
	Labels []struct {
		Name string `json:"name"`
	} `json:"labels"`
	Comments []struct {
		Author struct {
			Login string `json:"login"`
		} `json:"author"`
		Body      string    `json:"body"`
		CreatedAt time.Time `json:"createdAt"`
	} `json:"comments"`
}

// Issue returns an issue with its comments.
func (g GH) Issue(ctx context.Context, repo, issue string) (IssueDoc, error) {
	out, err := g.run(ctx, "", "issue", "view", issue, "--repo", repo, "--json", "number,title,body,state,url,createdAt,author,labels,comments")
	var d IssueDoc
	if err != nil {
		return d, err
	}
	return d, json.Unmarshal([]byte(out), &d)
}

// RequestCopilot asks Copilot to review a PR (again, after a new push).
func (g GH) RequestCopilot(ctx context.Context, repo string, n int) error {
	_, err := g.run(ctx, "", "pr", "edit", strconv.Itoa(n), "--repo", repo, "--add-reviewer", "@copilot")
	return err
}

// Check is one CI check run on a PR. Bucket is pass, fail, pending,
// skipping or cancel.
type Check struct {
	Name     string `json:"name"`
	Workflow string `json:"workflow"`
	Bucket   string `json:"bucket"`
	Link     string `json:"link"`
}

// Checks lists a PR's CI checks (none yet: an empty list).
func (g GH) Checks(ctx context.Context, repo string, n int) ([]Check, error) {
	out, err := g.output(ctx, "pr", "checks", strconv.Itoa(n), "--repo", repo, "--json", "name,workflow,bucket,link")
	cs := []Check{}
	if out != "" && json.Unmarshal([]byte(out), &cs) == nil {
		return cs, nil
	}
	if err != nil && strings.Contains(err.Error(), "no checks reported") {
		return cs, nil
	}
	return nil, err
}

// Issue is an open issue, for the New task picker.
type Issue struct {
	Number   int    `json:"number"`
	Title    string `json:"title"`
	Priority string `json:"priority"` // the "Priority" issue field (Urgent, High, …), "" when unset
}

var priorityRank = map[string]int{"urgent": 0, "high": 1, "medium": 2, "low": 3}

// MyIssues lists the open issues of repo assigned to the gh user, most
// urgent first, then newest. It uses the REST API: gh issue list does not
// return issue fields such as Priority.
func (g GH) MyIssues(ctx context.Context, repo string) ([]Issue, error) {
	login, err := g.run(ctx, "", "api", "user", "-q", ".login")
	if err != nil {
		return nil, err
	}
	out, err := g.run(ctx, "", "api", fmt.Sprintf("repos/%s/issues?assignee=%s&state=open&per_page=100", repo, url.QueryEscape(login)))
	if err != nil {
		return nil, err
	}
	var raw []struct {
		Number      int
		Title       string
		PullRequest *struct{} `json:"pull_request"`
		Fields      []struct {
			Name   string `json:"issue_field_name"`
			Option *struct {
				Name string `json:"name"`
			} `json:"single_select_option"`
		} `json:"issue_field_values"`
	}
	if err := json.Unmarshal([]byte(out), &raw); err != nil {
		return nil, err
	}
	is := []Issue{}
	for _, r := range raw {
		if r.PullRequest != nil { // the issues endpoint lists PRs too
			continue
		}
		i := Issue{Number: r.Number, Title: r.Title}
		for _, f := range r.Fields {
			if strings.EqualFold(f.Name, "priority") && f.Option != nil {
				i.Priority = f.Option.Name
			}
		}
		is = append(is, i)
	}
	rank := func(p string) int {
		if r, ok := priorityRank[strings.ToLower(p)]; ok {
			return r
		}
		if p != "" {
			return len(priorityRank)
		}
		return len(priorityRank) + 1
	}
	sort.SliceStable(is, func(a, b int) bool {
		if ra, rb := rank(is[a].Priority), rank(is[b].Priority); ra != rb {
			return ra < rb
		}
		return is[a].Number > is[b].Number
	})
	return is, nil
}

// CanAccess reports whether gh can read repo.
func (g GH) CanAccess(ctx context.Context, repo string) bool {
	_, err := g.run(ctx, "", "repo", "view", repo, "--json", "name")
	return err == nil
}

var prNum = regexp.MustCompile(`/pull/(\d+)`)

// CreatePR opens a draft pull request and returns its number and URL.
// It stays a draft until MarkReady: repo bots then leave it alone.
func (g GH) CreatePR(ctx context.Context, worktree, repo, base, head, title, body string) (int, string, error) {
	out, err := g.runIn(ctx, worktree, body+"\n", "pr", "create", "--draft", "--repo", repo, "--base", base, "--head", head, "--title", title, "--body-file", "-")
	if err != nil {
		// The branch has a PR already: it gets the new title and body.
		if v, e2 := g.run(ctx, worktree, "pr", "view", head, "--repo", repo, "--json", "number,url"); e2 == nil {
			var pr struct {
				Number int
				URL    string
			}
			if json.Unmarshal([]byte(v), &pr) == nil && pr.Number > 0 {
				_, err := g.runIn(ctx, worktree, body+"\n", "pr", "edit", strconv.Itoa(pr.Number), "--repo", repo, "--title", title, "--body-file", "-")
				return pr.Number, pr.URL, err
			}
		}
		return 0, "", err
	}
	url := strings.TrimSpace(out[strings.LastIndex(out, "\n")+1:])
	m := prNum.FindStringSubmatch(url)
	if m == nil {
		return 0, url, fmt.Errorf("could not read the PR number from %q", url)
	}
	n, _ := strconv.Atoi(m[1])
	return n, url, nil
}

// PR is what herd polls of an open pull request.
type PR struct {
	State  string    // open, merged or closed
	At     time.Time // when it was merged or closed
	Labels []string
	Draft  bool
	// Conflicting: it cannot merge into its base; GitHub then runs no
	// pull_request checks.
	Conflicting bool
}

// PRState returns a PR's state, labels and draft flag.
func (g GH) PRState(ctx context.Context, repo string, n int) (PR, error) {
	out, err := g.run(ctx, "", "pr", "view", strconv.Itoa(n), "--repo", repo, "--json", "state,mergedAt,closedAt,labels,isDraft,mergeable")
	if err != nil {
		return PR{}, err
	}
	var v struct {
		State     string
		MergedAt  *time.Time
		ClosedAt  *time.Time
		Labels    []struct{ Name string }
		IsDraft   bool
		Mergeable string
	}
	if err := json.Unmarshal([]byte(out), &v); err != nil {
		return PR{}, err
	}
	pr := PR{State: strings.ToLower(v.State), At: time.Now(), Labels: []string{}, Draft: v.IsDraft, Conflicting: v.Mergeable == "CONFLICTING"}
	for _, l := range v.Labels {
		pr.Labels = append(pr.Labels, l.Name)
	}
	if v.MergedAt != nil {
		pr.At = *v.MergedAt
	} else if v.ClosedAt != nil {
		pr.At = *v.ClosedAt
	}
	return pr, nil
}

var runJob = regexp.MustCompile(`/actions/runs/(\d+)(?:/job/(\d+))?`)

// RunOf reads the Actions run and job ids from a check's link ("" when the
// check is not a GitHub Actions job, e.g. an external app).
func RunOf(link string) (run, job string) {
	m := runJob.FindStringSubmatch(link)
	if m == nil {
		return "", ""
	}
	return m[1], m[2]
}

// Rerun starts a check again: its job when job is set, else its whole run.
func (g GH) Rerun(ctx context.Context, repo, run, job string) error {
	args := []string{"run", "rerun", run, "--repo", repo}
	if job != "" {
		args = []string{"run", "rerun", "--job", job, "--repo", repo}
	}
	_, err := g.run(ctx, "", args...)
	return err
}

// MarkReady turns a draft PR into one ready for review.
func (g GH) MarkReady(ctx context.Context, repo string, n int) error {
	_, err := g.run(ctx, "", "pr", "ready", strconv.Itoa(n), "--repo", repo)
	return err
}

// AddLabel adds a label to a PR, e.g. ready.
func (g GH) AddLabel(ctx context.Context, repo string, n int, label string) error {
	_, err := g.run(ctx, "", "pr", "edit", strconv.Itoa(n), "--repo", repo, "--add-label", label)
	return err
}

// Comment is one Copilot review comment, with its discussion.
type Comment struct {
	ID       int64   `json:"id"`
	Path     string  `json:"path"`
	Line     int     `json:"line"`
	Body     string  `json:"body"`
	Title    string  `json:"title"`    // from the review's overview, when listed there
	Severity string  `json:"severity"` // e.g. Medium, from the overview
	Hunk     string  `json:"hunk"`     // the diff lines the comment is on
	Replies  []Reply `json:"replies"`  // later messages in its thread, oldest first
}

// Reply is a later message in a review comment's thread.
type Reply struct {
	Author string `json:"author"`
	Body   string `json:"body"`
}

// Review is one Copilot review: its summary and inline comments.
type Review struct {
	ID       int64
	Body     string
	Comments []Comment
}

// apiList runs gh api on a list endpoint, every page, and decodes its items.
func apiList[T any](ctx context.Context, g GH, path string) ([]T, error) {
	out, err := g.run(ctx, "", "api", "--paginate", "--jq", ".[]", path+"?per_page=100")
	if err != nil {
		return nil, err
	}
	var items []T
	dec := json.NewDecoder(strings.NewReader(out))
	for dec.More() {
		var x T
		if err := dec.Decode(&x); err != nil {
			return items, err
		}
		items = append(items, x)
	}
	return items, nil
}

// overviewItem matches one finding in a Copilot review's overview: its
// severity icon, then its title linked to the comment's discussion.
var overviewItem = regexp.MustCompile(`alt="(\w+) severity"[^\n]*?\[([^\]\n]+)\]\([^)\s]*#discussion_r(\d+)\)`)

// CopilotReviews returns the Copilot reviews on a PR newer than review id
// after, oldest first, each with its comments and their replies. Copilot
// reviews again each time it is asked.
func (g GH) CopilotReviews(ctx context.Context, repo string, n int, after int64) ([]Review, error) {
	type user struct{ Login string }
	reviews, err := apiList[struct {
		ID   int64
		Body string
		User user
	}](ctx, g, fmt.Sprintf("repos/%s/pulls/%d/reviews", repo, n))
	if err != nil {
		return nil, err
	}
	var rs []Review
	for _, r := range reviews {
		if isCopilot(r.User.Login) && r.ID > after {
			rs = append(rs, Review{ID: r.ID, Body: r.Body})
		}
	}
	if len(rs) == 0 {
		return nil, nil
	}
	comments, err := apiList[struct {
		ID           int64
		Path         string
		Line         int
		OriginalLine int `json:"original_line"`
		Body         string
		DiffHunk     string `json:"diff_hunk"`
		Review       int64  `json:"pull_request_review_id"`
		ReplyTo      int64  `json:"in_reply_to_id"`
		User         user
	}](ctx, g, fmt.Sprintf("repos/%s/pulls/%d/comments", repo, n))
	if err != nil {
		return nil, err
	}
	replies := map[int64][]Reply{}
	for _, c := range comments {
		if c.ReplyTo != 0 {
			replies[c.ReplyTo] = append(replies[c.ReplyTo], Reply{Author: c.User.Login, Body: c.Body})
		}
	}
	for i := range rs {
		titles := map[int64][2]string{}
		for _, m := range overviewItem.FindAllStringSubmatch(rs[i].Body, -1) {
			id, _ := strconv.ParseInt(m[3], 10, 64)
			titles[id] = [2]string{m[1], m[2]}
		}
		for _, c := range comments {
			if c.Review != rs[i].ID || c.ReplyTo != 0 {
				continue
			}
			x := Comment{ID: c.ID, Path: c.Path, Line: c.Line, Body: c.Body, Hunk: c.DiffHunk, Replies: replies[c.ID]}
			if x.Line == 0 {
				x.Line = c.OriginalLine
			}
			if t, ok := titles[c.ID]; ok {
				x.Severity, x.Title = t[0], t[1]
			}
			rs[i].Comments = append(rs[i].Comments, x)
		}
	}
	return rs, nil
}

func isCopilot(login string) bool {
	return strings.Contains(strings.ToLower(login), "copilot")
}
