// Package hooks receives Claude Code hook events (POST /hook) and writes the
// hooks settings file that makes every herd session post them.
package hooks

import (
	"crypto/subtle"
	"encoding/json"
	"io"
	"net/http"
	"os"
	"strings"
)

// Event is one hook call, decoded from Claude Code's JSON payload.
type Event struct {
	Name             string // SessionStart, UserPromptSubmit, Notification, PostToolUse, Stop
	StepID           string // <task id>/<step key>
	SessionID        string `json:"session_id"`
	Source           string `json:"source"` // SessionStart: startup | resume | clear | compact
	Message          string `json:"message"`
	NotificationType string `json:"notification_type"`
	Raw              json.RawMessage
}

// IsPermission reports whether a Notification asks for permission.
func (e Event) IsPermission() bool {
	if e.NotificationType != "" {
		return e.NotificationType == "permission_prompt"
	}
	return strings.Contains(strings.ToLower(e.Message), "permission")
}

// Receiver handles POST /hook.
type Receiver struct {
	Token   string
	OnEvent func(Event)
}

func (h *Receiver) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if subtle.ConstantTimeCompare([]byte(r.Header.Get("X-Herd-Token")), []byte(h.Token)) != 1 {
		http.Error(w, "forbidden", http.StatusForbidden)
		return
	}
	body, err := io.ReadAll(io.LimitReader(r.Body, 4<<20))
	if err != nil {
		http.Error(w, "bad body", http.StatusBadRequest)
		return
	}
	ev := Event{Name: r.URL.Query().Get("event"), StepID: r.Header.Get("X-Herd-Step"), Raw: body}
	_ = json.Unmarshal(body, &ev)
	if ev.Name == "" || ev.StepID == "" || ev.StepID == "/" {
		http.Error(w, "missing event or step", http.StatusBadRequest)
		return
	}
	w.WriteHeader(http.StatusNoContent)
	if h.OnEvent != nil {
		go h.OnEvent(ev)
	}
}

// Events herd listens to.
var Events = []string{"SessionStart", "UserPromptSubmit", "Notification", "PostToolUse", "Stop"}

// Command is the shell command a hook runs: it does nothing outside herd
// sessions and never fails the hook. $HERD_ENDPOINT holds the current herd's
// address and token, so sessions that outlived a herd restart still report.
func Command(event string) string {
	return `[ -r "$HERD_ENDPOINT" ] && . "$HERD_ENDPOINT"; [ -n "$HERD_URL" ] && curl -s -m 3 -X POST "$HERD_URL/hook?event=` + event + `" -H "X-Herd-Token: $HERD_TOKEN" -H "X-Herd-Step: $HERD_TASK/$HERD_STEP" -H "Content-Type: application/json" --data-binary @- >/dev/null 2>&1; exit 0`
}

// Settings returns the Claude Code settings JSON with herd's hooks.
func Settings() ([]byte, error) {
	type hook struct {
		Type    string `json:"type"`
		Command string `json:"command"`
	}
	type matcher struct {
		Hooks []hook `json:"hooks"`
	}
	all := map[string][]matcher{}
	for _, e := range Events {
		all[e] = []matcher{{Hooks: []hook{{Type: "command", Command: Command(e)}}}}
	}
	return json.MarshalIndent(map[string]any{"hooks": all}, "", "  ")
}

// WriteSettings writes the hooks file (~/.herd/hooks.json).
func WriteSettings(path string) error {
	b, err := Settings()
	if err != nil {
		return err
	}
	return os.WriteFile(path, b, 0o644)
}
