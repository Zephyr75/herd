package artifacts

import "testing"

func TestParse(t *testing.T) {
	f, body := Parse("---\r\nverdict: changes   # review\r\nreviewed: \"b7c41d9\"\r\n---\r\n\r\n# Findings\n")
	if len(f) != 2 || f[0].V != "changes" || f[1].V != "b7c41d9" || body != "# Findings\n" {
		t.Fatalf("%+v %q", f, body)
	}
	for status, want := range map[string][2]bool{"done": {true, false}, "Failed": {true, true}, "working": {false, false}} {
		a := Artifact{Exists: true, Front: []Field{{K: "status", V: status}}}
		if fin, failed := a.Outcome(); [2]bool{fin, failed} != want {
			t.Errorf("status %s: finished %v, failed %v", status, fin, failed)
		}
	}
	if fin, _ := (Artifact{Exists: true, Front: f}).Outcome(); fin {
		t.Error("an artifact without status is finished")
	}
	if f, body := Parse("# no front matter"); f != nil || body != "# no front matter" {
		t.Fatal("plain")
	}
}
