package usage

import (
	"os"
	"path/filepath"
	"testing"
)

func TestSession(t *testing.T) {
	dir := t.TempDir()
	proj := filepath.Join(dir, "projects", "-x")
	os.MkdirAll(filepath.Join(proj, "s1", "subagents"), 0o755)
	main := filepath.Join(proj, "s1.jsonl")
	// The same message logged twice (one line per content block) counts once.
	os.WriteFile(main, []byte(`{"type":"user","message":{"role":"user"}}
{"type":"assistant","message":{"id":"m1","model":"claude-opus-5-5","usage":{"input_tokens":1000000,"output_tokens":10,"cache_read_input_tokens":0,"cache_creation_input_tokens":0}}}
{"type":"assistant","message":{"id":"m1","model":"claude-opus-5-5","usage":{"input_tokens":1000000,"output_tokens":100000,"cache_read_input_tokens":0,"cache_creation_input_tokens":0}}}
`), 0o644)
	os.WriteFile(filepath.Join(proj, "s1", "subagents", "a.jsonl"), []byte(`{"type":"assistant","message":{"id":"m2","model":"claude-haiku-5-5","usage":{"input_tokens":0,"output_tokens":0,"cache_read_input_tokens":1000000,"cache_creation_input_tokens":1000000,"cache_creation":{"ephemeral_1h_input_tokens":1000000,"ephemeral_5m_input_tokens":0}}}}
`), 0o644)
	r := NewReader(dir)
	u := r.Session("s1")
	if u.Input != 1000000 || u.Output != 100000 || u.CacheRead != 1000000 || u.CacheWrite != 1000000 {
		t.Fatalf("%+v", u)
	}
	// Appended lines are picked up incrementally.
	f, _ := os.OpenFile(main, os.O_APPEND|os.O_WRONLY, 0)
	f.WriteString(`{"type":"assistant","message":{"id":"m3","model":"unknown-model","usage":{"input_tokens":5,"output_tokens":5}}}` + "\n")
	f.Close()
	if u2 := r.Session("s1"); u2.Input != 1000005 || u2.Output != 100005 {
		t.Fatalf("%+v", u2)
	}
	if r.Session("missing").Tokens() != 0 {
		t.Fatal("missing session")
	}
}
