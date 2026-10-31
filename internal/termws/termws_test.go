package termws

import (
	"context"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"

	"herd/internal/runner"
)

func TestRoundTrip(t *testing.T) {
	r := runner.New(nil)
	if _, err := r.Start(runner.Spec{StepID: "core:1/plan-1", Bin: "cat"}); err != nil {
		t.Fatal(err)
	}
	defer r.StopAll()
	srv := httptest.NewServer(&Handler{Token: "tok", Runner: r})
	defer srv.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if _, _, err := websocket.Dial(ctx, "ws"+strings.TrimPrefix(srv.URL, "http")+"/term/core:1%2Fplan-1?token=bad", nil); err == nil {
		t.Fatal("bad token accepted")
	}
	c, _, err := websocket.Dial(ctx, "ws"+strings.TrimPrefix(srv.URL, "http")+"/term/core:1%2Fplan-1?token=tok", nil)
	if err != nil {
		t.Fatal(err)
	}
	defer c.CloseNow()
	if err := c.Write(ctx, websocket.MessageText, []byte(`{"type":"resize","cols":100,"rows":30}`)); err != nil {
		t.Fatal(err)
	}
	if err := c.Write(ctx, websocket.MessageBinary, []byte("hello herd\r")); err != nil {
		t.Fatal(err)
	}
	var got strings.Builder
	for !strings.Contains(got.String(), "hello herd") {
		_, b, err := c.Read(ctx)
		if err != nil {
			t.Fatalf("read: %v (got %q)", err, got.String())
		}
		got.Write(b)
	}
}
