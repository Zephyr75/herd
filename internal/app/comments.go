package app

import (
	"encoding/json"
	"regexp"

	"herd/internal/github"
	"herd/internal/store"
)

// Review comments a step studied: its artifact's front matter lists them by
// GitHub comment id (fixed: 4513677327, rejected: 4523856444). herd keeps
// each rejection with the comment, for the PR tab: the user resolves those
// threads, or sends them again.

// Rejection is the step that found a review comment wrong or unnecessary.
type Rejection struct {
	Step string `json:"step"`
	Name string `json:"name"`
}

func rejectedKey(comment string) string { return "rejected:" + comment }

var number = regexp.MustCompile(`\d+`)

// recordComments keeps what step s decided about the review comments it
// handled: a fix clears an earlier rejection.
func (a *App) recordComments(s store.Step) {
	art, ag := a.artifactFor(s)
	for _, id := range number.FindAllString(art.Get("fixed"), -1) {
		_ = a.db.Set(rejectedKey(id), "")
	}
	for _, id := range number.FindAllString(art.Get("rejected"), -1) {
		b, _ := json.Marshal(Rejection{Step: s.ID, Name: ag.DisplayName(s.N)})
		_ = a.db.Set(rejectedKey(id), string(b))
	}
}

var discussion = regexp.MustCompile(`#discussion_r(\d+)`)

// commentID is the GitHub id of a review thread's first comment, from its URL.
func commentID(f github.Feedback) string {
	if m := discussion.FindStringSubmatch(f.URL); m != nil {
		return m[1]
	}
	return ""
}

// rejections lists the open threads a step rejected, by thread id.
func (a *App) rejections(fs []github.Feedback) map[string]Rejection {
	out := map[string]Rejection{}
	for _, f := range fs {
		id := commentID(f)
		if id == "" || f.ResolvedBy != "" {
			continue
		}
		var r Rejection
		if raw, _ := a.db.Get(rejectedKey(id)); raw != "" && json.Unmarshal([]byte(raw), &r) == nil {
			out[f.ID] = r
		}
	}
	return out
}
