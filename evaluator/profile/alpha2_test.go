package profile

import "testing"

func TestRejectsLegacyVersionBeforeExecution(t *testing.T) {
	source := readContract(t, "examples", "v0alpha1", "evaluator-profile", "semantic-routing.json")
	if _, err := Parse(source, FormatJSON); err == nil {
		t.Fatal("accepted v0alpha1 profile")
	}
}
