package app

// Sister tasks: when a plan needs changes in other repositories, it ends
// PLAN.md with a "## Sister tasks" section, one per repository. Next step then offers to
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

	"herd/internal/settings"
	"herd/internal/store"
)

// SisterOffer is a repository a step asks for, without a sister task yet.
type SisterOffer struct {
	Repo    string `json:"repo"`    // as the artifact names it, e.g. okai-tcp-server
	Scope   string `json:"scope"`   // what that part covers
	Project string `json:"project"` // the herd project for it; "" when not added yet
	From    string `json:"-"`       // the artifact that asks for it
}

// SisterRef is a sister task, for the header link.
type SisterRef struct {
	ID      string `json:"id"`
	Project string `json:"project"` // its project's name
}

var sisterHeading = regexp.MustCompile(`(?mi)^(#{1,6})\s*(?:sister tasks|other repositories)\s*$`)
var mdHeading = regexp.MustCompile(`^(#{1,6})\s+(.*?)\s*$`)
var otherRepoLine = regexp.MustCompile(`^\s*[-*]\s*\**` + "`?" + `([A-Za-z0-9._/-]+)` + "`?" + `\**\s*(?::|—|–|-)\s*(.+)$`)

// planRepos reads the sister tasks a PLAN.md body asks for: after a "---"
// line, a "## Sister tasks" section with one "### <repository>" each, then
// the description of that task. The older "## Other repositories" section,
// one "- <repository>: <what that part covers>" line each, still works.
func planRepos(body string) []SisterOffer {
	m := sisterHeading.FindStringSubmatchIndex(body)
	if m == nil {
		return nil
	}
	level := m[3] - m[2]
	var out []SisterOffer
	var cur *SisterOffer
	flush := func() {
		if cur != nil {
			cur.Scope = strings.TrimSpace(cur.Scope)
			out = append(out, *cur)
			cur = nil
		}
	}
	for _, l := range strings.Split(body[m[1]:], "\n") {
		if h := mdHeading.FindStringSubmatch(l); h != nil {
			flush()
			if len(h[1]) <= level {
				break // the next section
			}
			if repo := strings.Trim(h[2], "`* "); repo != "" {
				cur = &SisterOffer{Repo: filepath.Base(repo)}
			}
			continue
		}
		if cur != nil {
			cur.Scope += l + "\n"
		} else if r := otherRepoLine.FindStringSubmatch(l); r != nil {
			out = append(out, SisterOffer{Repo: filepath.Base(r[1]), Scope: strings.TrimSpace(r[2])})
		}
	}
	flush()
	return out
}

func sistersKey(taskID string) string { return "sisters:" + taskID }

// sisters lists a task's sister tasks that still exist.
func (a *App) sisters(taskID string) []store.Task {
	// Every task of its sister group, linked directly or through another
	// sister: two sisters of the same task know each other too.
	seen := map[string]bool{taskID: true}
	todo := a.sisterLinks(taskID)
	var out []store.Task
	for len(todo) > 0 {
		id := todo[0]
		todo = todo[1:]
		if seen[id] {
			continue
		}
		seen[id] = true
		if t, err := a.db.Task(id); err == nil {
			out = append(out, t)
			todo = append(todo, a.sisterLinks(id)...)
		}
	}
	return out
}

// sisterLinks are the ids a task is directly linked to.
func (a *App) sisterLinks(taskID string) []string {
	v, _ := a.db.Get(sistersKey(taskID))
	var ids []string
	_ = json.Unmarshal([]byte(v), &ids)
	return ids
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

// sisterOffers lists the repositories that the succeeded steps of agents
// allowed to (Settings > Agents: Plan, Implement…) ask for, without a sister
// task yet. A later step's description of a repository wins.
func (a *App) sisterOffers(t store.Task, steps []StepView, agents []settings.Agent) []SisterOffer {
	out := []SisterOffer{}
	if t.Archived {
		return out
	}
	var asked []SisterOffer
	for _, s := range steps {
		if !agentFor(agents, s.Agent).Sisters || !s.Succeeded() {
			continue
		}
		art, _ := a.artifactFor(s.Step)
		if !art.Exists {
			continue
		}
		for _, o := range planRepos(art.Body) {
			o.From = art.Path
			if i := slices.IndexFunc(asked, func(x SisterOffer) bool { return x.Repo == o.Repo }); i >= 0 {
				asked[i] = o
			} else {
				asked = append(asked, o)
			}
		}
	}
	sis := a.sisters(t.ID)
	all, _ := a.db.Tasks()
	for _, o := range asked {
		o.Project, _ = a.projectFor(o.Repo)
		if o.Project == t.Project || slices.ContainsFunc(sis, func(x store.Task) bool { return o.Project != "" && x.Project == o.Project }) {
			continue // the group already has it
		}
		if t.Issue != "" && slices.ContainsFunc(all, func(x store.Task) bool { return o.Project != "" && x.Project == o.Project && x.Issue == t.Issue }) {
			continue // a task for the same issue exists there already
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
		return TaskView{}, fmt.Errorf("no step asks for a sister task in %s", repo)
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
		"The %s part is described in %s (its run folder %s holds the plan and every step). Read them, and keep what both repositories share (APIs, messages, field names) consistent with it.",
		p.Name, ref, t.Title, repo, o.Scope, p.Name, o.From, a.runDir(p, t))
	sv, err := a.createTask(o.Project, t.Issue, a.issueRepo(t), desc, branchName(t), false, nil)
	if err != nil {
		return TaskView{}, err
	}
	if err := a.updateTask(sv.ID, func(x *store.Task) error { x.Sister = true; return nil }); err != nil {
		return sv, err
	}
	// Linked with every task of the group, not only the one that asked.
	for _, x := range append([]store.Task{t}, a.sisters(t.ID)...) {
		if x.ID == sv.ID {
			continue
		}
		if err := a.linkSister(x.ID, sv.ID); err != nil {
			return sv, err
		}
		if err := a.linkSister(sv.ID, x.ID); err != nil {
			return sv, err
		}
	}
	// Linked first, so its Plan already sees the sister.
	if first, ok := settings.First(a.settings.Agents()); ok {
		if _, err := a.StartStep(sv.ID, first.ID, "", 0); err != nil {
			a.toast("Could not start "+first.Name, err.Error(), true)
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
