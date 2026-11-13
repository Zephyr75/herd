package app

import (
	"encoding/json"
	"errors"
)

// Todo is one item of the user's todo lists: a task's (kept in its task
// file), or Others, for what belongs to no task (in state.json).
type Todo struct {
	ID     string `json:"id"`
	Text   string `json:"text"`
	Done   bool   `json:"done"`
	Urgent bool   `json:"urgent,omitempty"`
}

// EvTodos carries the Others list when it changes.
const EvTodos = "todos:changed"

// todosKey is where a list is kept: "todos:<task id>", or "todos" for Others.
func todosKey(taskID string) string {
	if taskID == "" {
		return "todos"
	}
	return "todos:" + taskID
}

func (a *App) todosOf(taskID string) []Todo {
	ts := []Todo{}
	if v, _ := a.db.Get(todosKey(taskID)); v != "" {
		_ = json.Unmarshal([]byte(v), &ts)
	}
	return ts
}

// Todos returns the Others list.
func (a *App) Todos() []Todo { return a.todosOf("") }

// SetTodos saves a task's todo list, or Others when taskID is "".
func (a *App) SetTodos(taskID string, todos []Todo) error {
	if taskID != "" {
		if _, err := a.db.Task(taskID); err != nil {
			return errors.New("no such task")
		}
	}
	v := ""
	if len(todos) > 0 {
		b, err := json.Marshal(todos)
		if err != nil {
			return err
		}
		v = string(b)
	}
	if err := a.db.Set(todosKey(taskID), v); err != nil {
		return err
	}
	if taskID == "" {
		a.emit(EvTodos, a.todosOf(""))
	} else {
		a.emitTask(taskID)
	}
	return nil
}
