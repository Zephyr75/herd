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

// An agents.toml from before the three git boxes and the listed Copilot: the
// former push box meant commit, push and open the PR, and Copilot followed
// that agent offering what it did.
func TestNormalizeOlderAgents(t *testing.T) {
	old := []Agent{
		{ID: "plan", Name: "Plan", Trigger: "manual"},
		{ID: "push", Name: "Push", Trigger: "manual", Push: true, Next: []string{"plan"}},
		{ID: "test", Name: "Test", Trigger: "manual", OpenPR: true},
	}
	got := normalize(old)
	var ids []string
	for _, a := range got {
		ids = append(ids, a.ID)
	}
	if strings.Join(ids, " ") != "plan push copilot test" {
		t.Fatalf("order %v", ids)
	}
	if p := got[1]; !p.Commit || !p.Push || !p.OpenPR {
		t.Fatalf("former push box: %+v", p)
	}
	if c := got[2]; !c.Builtin || c.Trigger != "after:push" || strings.Join(c.Next, ",") != "plan" || c.Name != "Copilot" {
		t.Fatalf("copilot: %+v", c)
	}
	if x := got[3]; !x.Commit || !x.Push { // Open the PR needs Push, which needs Commit
		t.Fatalf("dependencies: %+v", x)
	}
	if f, _ := First(got); f.ID != "plan" {
		t.Fatalf("first %s", f.ID)
	}
}

func TestCopilotCannotBeStartedByHand(t *testing.T) {
	s, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	ags := DefaultAgents()
	ags[0].Next = append(ags[0].Next, CopilotID)
	if s.SaveAgents(ags) == nil {
		t.Fatal("Copilot offered as a next step")
	}
	ags = DefaultAgents()
	ags[1].Trigger = "after:" + CopilotID
	if s.SaveAgents(ags) == nil {
		t.Fatal("an agent starting after Copilot")
	}
}
