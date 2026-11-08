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
