package settings

import "testing"

func TestCheckTriggers(t *testing.T) {
	ok := []Agent{{ID: "plan", Name: "Plan", Trigger: "manual"}, {ID: "impl", Name: "Implement", Trigger: "after:plan"}, {ID: "review", Name: "Review", Trigger: "after:plan"}}
	if err := checkTriggers(ok); err != nil {
		t.Fatal(err)
	}
	for name, ags := range map[string][]Agent{
		"self":    {{ID: "a", Name: "A", Trigger: "after:a"}},
		"missing": {{ID: "a", Name: "A", Trigger: "after:b"}},
		"loop":    {{ID: "a", Name: "A", Trigger: "after:b"}, {ID: "b", Name: "B", Trigger: "after:c"}, {ID: "c", Name: "C", Trigger: "after:a"}},
		"unknown": {{ID: "a", Name: "A", Trigger: "sometimes"}},
	} {
		if checkTriggers(ags) == nil {
			t.Errorf("%s: accepted", name)
		}
	}
}
