package app

// Sister tasks: when a plan needs changes in other repositories, it lists
// them under "## Other repositories" in PLAN.md. Next step then offers to
// create, in one click, a sister task per repository: same issue, its own
// worktree, plan and PR. Sisters read each other's run folder to keep what
// they share consistent.

import (
	"encoding/json"
	"fmt"
	"path/filepath"
	"regexp"
	"slices"
	"strings"

	"herd/internal/store"
)

// SisterOffer is a repository the plan needs, without a sister task yet.
type SisterOffer struct {
	Repo    string `json:"repo"`    // as PLAN.md names it, e.g. okai-tcp-server
	Scope   string `json:"scope"`   // what that part covers
	Project string `json:"project"` // the herd project for it; "" when not added yet
}

// SisterRef is a sister task, for the header link.
type SisterRef struct {
	ID      string `json:"id"`
	Project string `json:"project"` // its project's name
}

var otherReposHeading = regexp.MustCompile(`(?mi)^#{1,6}\s*other repositories\s*$`)
var otherRepoLine = regexp.MustCompile(`^\s*[-*]\s*\**` + "`?" + `([A-Za-z0-9._/-]+)` + "`?" + `\**\s*(?::|—|–|-)\s*(.+)$`)

// planRepos reads the "## Other repositories" section of a PLAN.md body:
// one "- <repository>: <what that part covers>" line each.
func planRepos(body string) []SisterOffer {
	loc := otherReposHeading.FindStringIndex(body)
	if loc == nil {
		return nil
	}
	var out []SisterOffer
	for _, l := range strings.Split(body[loc[1]:], "\n") {
		if strings.HasPrefix(strings.TrimSpace(l), "#") {
			break // the next section
		}
		if m := otherRepoLine.FindStringSubmatch(l); m != nil {
			out = append(out, SisterOffer{Repo: filepath.Base(m[1]), Scope: strings.TrimSpace(m[2])})
		}
	}
	return out
}

func sistersKey(taskID string) string { return "sisters:" + taskID }

// sisters lists a task's sister tasks that still exist.
func (a *App) sisters(taskID string) []store.Task {
	v, _ := a.db.Get(sistersKey(taskID))
	var ids []string
	_ = json.Unmarshal([]byte(v), &ids)
	var out []store.Task
	for _, id := range ids {
		if t, err := a.db.Task(id); err == nil {
			out = append(out, t)
		}
	}
	return out
}

func (a *App) linkSister(taskID, sisterID string) error {
	v, _ := a.db.Get(sistersKey(taskID))
	var ids []string
	_ = json.Unmarshal([]byte(v), &ids)
	if !slices.Contains(ids, sisterID) {
		ids = append(ids, sisterID)
	}
	b, _ := json.Marshal(ids)
	return a.db.Set(sistersKey(taskID), string(b))
}

// projectFor finds the herd project of a repository named in a plan.
func (a *App) projectFor(repo string) (string, bool) {
	for _, p := range a.settings.Config().Projects {
		if strings.EqualFold(p.Name, repo) || strings.EqualFold(filepath.Base(p.Path), repo) {
			return p.ID, true
		}
	}
	return "", false
}

// sisterOffers lists the repositories the task's latest plan needs that have
// no sister task yet.
func (a *App) sisterOffers(t store.Task, steps []StepView, plan string) []SisterOffer {
	out := []SisterOffer{}
	if t.Archived {
		return out
	}
	var body string
	for _, s := range steps {
		if s.Agent == plan && s.Succeeded() {
			if art, _ := a.artifactFor(s.Step); art.Exists {
				body = art.Body
			}
		}
	}
	sis := a.sisters(t.ID)
	for _, o := range planRepos(body) {
		o.Project, _ = a.projectFor(o.Repo)
		if o.Project == t.Project || slices.ContainsFunc(sis, func(x store.Task) bool { return o.Project != "" && x.Project == o.Project }) {
			continue
		}
		out = append(out, o)
	}
	return out
}

// CreateSister creates the sister task for a repository the task's plan
// needs: same issue (still the original repository's), same branch name, a
// description saying which part it handles, then runs its Plan.
func (a *App) CreateSister(taskID, repo string) (TaskView, error) {
	t, err := a.db.Task(taskID)
	if err != nil {
		return TaskView{}, err
	}
	v, err := a.view(t)
	if err != nil {
		return TaskView{}, err
	}
	i := slices.IndexFunc(v.Next.Sisters, func(o SisterOffer) bool { return o.Repo == repo })
	if i < 0 {
		return TaskView{}, fmt.Errorf("the plan does not ask for a sister task in %s", repo)
	}
	o := v.Next.Sisters[i]
	if o.Project == "" {
		return TaskView{}, fmt.Errorf("add %s in Settings > Projects first", repo)
	}
	p := a.projectOf(t)
	ref := "#" + t.Issue
	if t.Issue == "" {
		ref = t.Title
	}
	desc := fmt.Sprintf("Sister task of %s task %s (%s). Handle only the %s part: %s\n\n"+
		"The %s part is planned in %s. Read it, and keep what both repositories share (APIs, messages, field names) consistent with it.",
		p.Name, ref, t.Title, repo, o.Scope, p.Name, filepath.Join(a.runDir(p, t), "PLAN.md"))
	sv, err := a.createTask(o.Project, t.Issue, a.issueRepo(t), desc, branchName(t), false, nil)
	if err != nil {
		return TaskView{}, err
	}
	if err := a.linkSister(t.ID, sv.ID); err != nil {
		return sv, err
	}
	if err := a.linkSister(sv.ID, t.ID); err != nil {
		return sv, err
	}
	// Linked first, so its Plan already sees the sister.
	if agents := a.settings.Agents(); len(agents) > 0 {
		if _, err := a.StartStep(sv.ID, agents[0].ID, "", 0); err != nil {
			a.toast("Could not start "+agents[0].Name, err.Error(), true)
		}
	}
	a.emitTask(t.ID)
	return a.GetTask(sv.ID)
}

// issueRepo is the repository holding the task's issue.
func (a *App) issueRepo(t store.Task) string {
	if t.IssueRepo != "" {
		return t.IssueRepo
	}
	return a.projectOf(t).Repo
}
