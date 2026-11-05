package settings

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestCheckTriggers(t *testing.T) {
	ok := []Agent{{ID: "plan", Name: "Plan", Trigger: "manual"}, {ID: "impl", Name: "Implement", Trigger: "after:plan"}, {ID: "review", Name: "Review", Trigger: "after:plan"}, {ID: "test", Name: "Test", Trigger: "after:review"}}
	if err := checkTriggers(ok); err != nil {
		t.Fatal(err)
	}
	for name, ags := range map[string][]Agent{
		"self":    {{ID: "a", Name: "A", Trigger: "after:a"}},
		"missing": {{ID: "a", Name: "A", Trigger: "after:b"}},
		"loop":    {{ID: "a", Name: "A", Trigger: "after:b"}, {ID: "b", Name: "B", Trigger: "after:c"}, {ID: "c", Name: "C", Trigger: "after:a"}},
		"unknown": {{ID: "a", Name: "A", Trigger: "sometimes"}},
		"verdict": {{ID: "a", Name: "A"}, {ID: "b", Name: "B", Trigger: "after:a=approve"}},
	} {
		if checkTriggers(ags) == nil {
			t.Errorf("%s: accepted", name)
		}
	}
}

func TestAgentListsAreNeverNull(t *testing.T) {
	s, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if err := s.SaveAgents(DefaultAgents()); err != nil {
		t.Fatal(err)
	}
	for _, a := range s.Agents() {
		b, _ := json.Marshal(a)
		if strings.Contains(string(b), `"next":null`) || strings.Contains(string(b), `"onFail":null`) {
			t.Fatalf("%s: %s", a.ID, b)
		}
	}
}
