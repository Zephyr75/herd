// Package termws bridges PTY sessions to xterm.js over a local WebSocket.
//
// Protocol: GET /term/<step id>?token=<token>. Server to client: binary
// frames with raw PTY output, and text frames with JSON control messages
// ({"type":"exit","code":N}). A session that already ended replays its
// output, then sends exit. Client to server: binary frames with input, and text
// frames with {"type":"resize","cols":N,"rows":N}.
package termws

import (
	"context"
	"crypto/subtle"
	"encoding/json"
	"net/http"
	"strings"

	"github.com/coder/websocket"

	"herd/internal/runner"
)

// Handler serves terminal sockets.
type Handler struct {
	Token  string
	Runner *runner.Runner
	// OnAttach is called when a viewer connects or leaves (for reaping and
	// live-session accounting). Optional.
	OnAttach func(stepID string, attached bool)
}

type control struct {
	Type string `json:"type"`
	Cols int    `json:"cols"`
	Rows int    `json:"rows"`
}

func (h *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if subtle.ConstantTimeCompare([]byte(r.URL.Query().Get("token")), []byte(h.Token)) != 1 {
		http.Error(w, "forbidden", http.StatusForbidden)
		return
	}
	stepID := strings.TrimPrefix(r.URL.Path, "/term/")
	sess, ok := h.Runner.Get(stepID)
	if !ok {
		// A session that ended during this run: replay its output, then exit.
		sess, ok = h.Runner.Ended(stepID)
	}
	if !ok {
		http.Error(w, "no session", http.StatusNotFound)
		return
	}
	// The webview's origin is wails://, so the origin check is replaced by the
	// per-launch token above.
	c, err := websocket.Accept(w, r, &websocket.AcceptOptions{InsecureSkipVerify: true})
	if err != nil {
		return
	}
	c.SetReadLimit(1 << 20)
	defer c.CloseNow()

	viewer, backlog := sess.Attach()
	defer viewer.Detach()
	if h.OnAttach != nil {
		h.OnAttach(stepID, true)
		defer h.OnAttach(stepID, false)
	}
	ctx, cancel := context.WithCancel(r.Context())
	defer cancel()

	if len(backlog) > 0 {
		if err := c.Write(ctx, websocket.MessageBinary, backlog); err != nil {
			return
		}
	}

	go func() {
		defer cancel()
		for {
			typ, data, err := c.Read(ctx)
			if err != nil {
				return
			}
			if typ == websocket.MessageBinary {
				_, _ = sess.Write(data)
				continue
			}
			var m control
			if json.Unmarshal(data, &m) == nil && m.Type == "resize" {
				_ = sess.Resize(m.Cols, m.Rows)
			}
		}
	}()

	for {
		select {
		case <-ctx.Done():
			return
		case b, ok := <-viewer.C:
			if !ok {
				msg, _ := json.Marshal(map[string]any{"type": "exit", "code": sess.ExitCode()})
				_ = c.Write(ctx, websocket.MessageText, msg)
				c.Close(websocket.StatusNormalClosure, "session ended")
				return
			}
			if err := c.Write(ctx, websocket.MessageBinary, b); err != nil {
				return
			}
		}
	}
}
