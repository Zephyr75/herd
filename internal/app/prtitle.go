package app

import (
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"slices"
	"strings"

	"herd/internal/store"
)

// PRTitle is the pull request title chosen in the Push popup:
// "<kind>(<scope>): <desc> - #<issue>".
type PRTitle struct {
	Kind  string `json:"kind"`  // feat | fix | perf | test
	Scope string `json:"scope"` // the main part of the code it changes, e.g. vehicles
	Desc  string `json:"desc"`  // defaults to the branch name without its prefix
}

var prKinds = []string{"feat", "fix", "perf", "test"}

var scopeChars = regexp.MustCompile(`^[A-Za-z0-9._/-]+$`)

// Format renders the title; LOCAL tasks have no issue suffix.
func (p PRTitle) Format(issue string) string {
	s := fmt.Sprintf("%s(%s): %s", p.Kind, p.Scope, p.Desc)
	if issue != "" {
		s += " - #" + issue
	}
	return s
}

// defaultPRDesc is the branch without the prefix, dashes as spaces.
func defaultPRDesc(branch, prefix string) string {
	b := strings.TrimPrefix(branch, prefix)
	if i := strings.LastIndex(b, "/"); b == branch && i >= 0 {
		b = b[i+1:] // agent/<slug> and other foreign prefixes
	}
	return strings.Join(strings.Fields(strings.NewReplacer("-", " ", "_", " ").Replace(b)), " ")
}

func (a *App) normalizePRTitle(t store.Task, p PRTitle) (PRTitle, error) {
	p.Kind, p.Scope = strings.TrimSpace(p.Kind), strings.TrimSpace(p.Scope)
	p.Desc = strings.Join(strings.Fields(p.Desc), " ")
	if !slices.Contains(prKinds, p.Kind) {
		return p, fmt.Errorf("the PR type must be one of %s", strings.Join(prKinds, ", "))
	}
	if p.Scope == "" {
		return p, errors.New("name the part of the code the PR changes, e.g. vehicles")
	}
	if !scopeChars.MatchString(p.Scope) {
		return p, errors.New("the scope is one word: letters, digits, . _ / -")
	}
	if p.Desc == "" {
		p.Desc = defaultPRDesc(branchName(t), a.settings.Config().BranchPrefix)
	}
	return p, nil
}

// prTitle returns the title saved for a task's PR, if any.
func (a *App) prTitle(taskID string) (PRTitle, bool) {
	v, _ := a.db.Get("prtitle:" + taskID)
	var p PRTitle
	if v == "" || json.Unmarshal([]byte(v), &p) != nil {
		return PRTitle{}, false
	}
	return p, true
}

// StartPush saves the PR title from the Push popup, then runs the push agent.
func (a *App) StartPush(taskID, agentID, guidance string, n int, title PRTitle) (StepView, error) {
	t, err := a.db.Task(taskID)
	if err != nil {
		return StepView{}, err
	}
	title, err = a.normalizePRTitle(t, title)
	if err != nil {
		return StepView{}, err
	}
	b, _ := json.Marshal(title)
	if err := a.db.Set("prtitle:"+taskID, string(b)); err != nil {
		return StepView{}, err
	}
	return a.StartStep(taskID, agentID, guidance, n)
}
