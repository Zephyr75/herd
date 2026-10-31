package artifacts

import "testing"

func TestParse(t *testing.T) {
	f, body := Parse("---\r\nverdict: changes   # review\r\nreviewed: \"b7c41d9\"\r\n---\r\n\r\n# Findings\n")
	if len(f) != 2 || f[0].V != "changes" || f[1].V != "b7c41d9" || body != "# Findings\n" {
		t.Fatalf("%+v %q", f, body)
	}
	a := Artifact{Exists: true, Front: f}
	if !a.IsDone("verdict", []string{"approve", "changes"}) || a.IsDone("status", []string{"done"}) {
		t.Fatal("IsDone")
	}
	if f, body := Parse("# no front matter"); f != nil || body != "# no front matter" {
		t.Fatal("plain")
	}
}
