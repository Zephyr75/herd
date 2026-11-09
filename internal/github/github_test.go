package github

import "testing"

func TestRunOf(t *testing.T) {
	for link, want := range map[string][2]string{
		"https://github.com/o/r/actions/runs/123/job/456": {"123", "456"},
		"https://github.com/o/r/actions/runs/123":         {"123", ""},
		"https://example.com/check/9":                     {"", ""},
	} {
		if run, job := RunOf(link); run != want[0] || job != want[1] {
			t.Errorf("RunOf(%q) = %q, %q", link, run, job)
		}
	}
}

func TestParseFeedback(t *testing.T) {
	out := `{"data":{"repository":{"pullRequest":{"headRefOid":"abc",
"reviewThreads":{"nodes":[
 {"id":"T1","isResolved":false,"isOutdated":true,"path":"a.py","line":null,"originalLine":12,
  "comments":{"nodes":[{"author":{"login":"ana"},"body":" Rename it ","url":"u1"},{"author":{"login":"bob"},"body":"agreed","url":"u2"}]}},
 {"id":"T2","isResolved":true,"resolvedBy":{"login":"Copilot"},"isOutdated":false,"path":"b.py","line":3,"originalLine":3,
  "comments":{"nodes":[{"author":{"login":"ana"},"body":"done","url":"u3"}]}}]},
"reviews":{"nodes":[
 {"id":"R1","author":{"login":"ana"},"body":"Please add a test","url":"u4","commit":{"oid":"abc"}},
 {"id":"R2","author":{"login":"ana"},"body":"old","url":"u5","commit":{"oid":"old"}},
 {"id":"R3","author":{"login":"copilot-pull-request-reviewer"},"body":"overview","url":"u6","commit":{"oid":"abc"}}]}}}}}`
	fs, err := parseFeedback(out)
	if err != nil {
		t.Fatal(err)
	}
	if len(fs) != 3 {
		t.Fatalf("got %d, want both threads and the review of the head commit: %+v", len(fs), fs)
	}
	if f := fs[0]; f.ID != "T1" || f.Line != 12 || !f.Outdated || f.Body != "Rename it" || len(f.Replies) != 1 || f.Replies[0].Author != "bob" {
		t.Errorf("thread = %+v", f)
	}
	if f := fs[1]; f.ID != "T2" || f.ResolvedBy != "Copilot" || fs[0].ResolvedBy != "" {
		t.Errorf("resolved thread = %+v", f)
	}
	if f := fs[2]; f.ID != "R1" || f.Path != "" || f.Body != "Please add a test" {
		t.Errorf("review = %+v", f)
	}
}
