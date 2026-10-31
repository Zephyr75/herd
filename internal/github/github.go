// Package github shells out to gh for issues, pull requests and reviews.
package github

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"regexp"
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
	var out, errb bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &errb
	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("gh %s: %w: %s", args[0], err, strings.TrimSpace(errb.String()))
	}
	return strings.TrimSpace(out.String()), nil
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

// CanAccess reports whether gh can read repo.
func (g GH) CanAccess(ctx context.Context, repo string) bool {
	_, err := g.run(ctx, "", "repo", "view", repo, "--json", "name")
	return err == nil
}

var prNum = regexp.MustCompile(`/pull/(\d+)`)

// CreatePR opens a pull request and returns its number and URL.
func (g GH) CreatePR(ctx context.Context, worktree, repo, base, head, title, bodyFile string) (int, string, error) {
	out, err := g.run(ctx, worktree, "pr", "create", "--repo", repo, "--base", base, "--head", head, "--title", title, "--body-file", bodyFile)
	if err != nil {
		// An existing PR for the branch is fine.
		if v, e2 := g.run(ctx, worktree, "pr", "view", head, "--repo", repo, "--json", "number,url"); e2 == nil {
			var pr struct {
				Number int
				URL    string
			}
			if json.Unmarshal([]byte(v), &pr) == nil && pr.Number > 0 {
				return pr.Number, pr.URL, nil
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

// PRState returns open, merged or closed, and when it changed.
func (g GH) PRState(ctx context.Context, repo string, n int) (string, time.Time, error) {
	out, err := g.run(ctx, "", "pr", "view", strconv.Itoa(n), "--repo", repo, "--json", "state,mergedAt,closedAt")
	if err != nil {
		return "", time.Time{}, err
	}
	var v struct {
		State    string
		MergedAt *time.Time
		ClosedAt *time.Time
	}
	if err := json.Unmarshal([]byte(out), &v); err != nil {
		return "", time.Time{}, err
	}
	at := time.Now()
	if v.MergedAt != nil {
		at = *v.MergedAt
	} else if v.ClosedAt != nil {
		at = *v.ClosedAt
	}
	return strings.ToLower(v.State), at, nil
}

// Comment is one review comment.
type Comment struct {
	Path string `json:"path"`
	Line int    `json:"line"`
	Body string `json:"body"`
}

// CopilotReview returns the Copilot review body and comments on a PR, and
// whether the review has arrived.
func (g GH) CopilotReview(ctx context.Context, repo string, n int) (string, []Comment, bool, error) {
	out, err := g.run(ctx, "", "api", fmt.Sprintf("repos/%s/pulls/%d/reviews", repo, n))
	if err != nil {
		return "", nil, false, err
	}
	var reviews []struct {
		ID   int64
		Body string
		User struct{ Login string }
	}
	if err := json.Unmarshal([]byte(out), &reviews); err != nil {
		return "", nil, false, err
	}
	var id int64
	body := ""
	for _, r := range reviews {
		if isCopilot(r.User.Login) {
			id, body = r.ID, r.Body
		}
	}
	if id == 0 {
		return "", nil, false, nil
	}
	out, err = g.run(ctx, "", "api", fmt.Sprintf("repos/%s/pulls/%d/reviews/%d/comments", repo, n, id))
	if err != nil {
		return body, nil, true, err
	}
	var cs []Comment
	_ = json.Unmarshal([]byte(out), &cs)
	return body, cs, true, nil
}

func isCopilot(login string) bool {
	return strings.Contains(strings.ToLower(login), "copilot")
}
