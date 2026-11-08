package app

import (
	"encoding/json"
	"errors"
)

// Note is the user's comment on lines of a diff (Changes), kept with the
// task until it is sent to a step as guidance.
type Note struct {
	ID   string `json:"id"`
	Path string `json:"path"`
	Side string `json:"side"` // new or old: which line numbers From and To are
	From int    `json:"from"`
	To   int    `json:"to"`
	Code string `json:"code"` // the lines it is about
	Text string `json:"text"`
}

func (a *App) notesOf(taskID string) []Note {
	ns := []Note{}
	if v, _ := a.db.Get("notes:" + taskID); v != "" {
		_ = json.Unmarshal([]byte(v), &ns)
	}
	return ns
}

// SetNotes saves the task's diff comments (none: removes them).
func (a *App) SetNotes(taskID string, notes []Note) error {
	if _, err := a.db.Task(taskID); err != nil {
		return errors.New("no such task")
	}
	v := ""
	if len(notes) > 0 {
		b, err := json.Marshal(notes)
		if err != nil {
			return err
		}
		v = string(b)
	}
	if err := a.db.Set("notes:"+taskID, v); err != nil {
		return err
	}
	a.emitTask(taskID)
	return nil
}
