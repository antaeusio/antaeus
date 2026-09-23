package decision

import (
	"path/filepath"
	"testing"

	"github.com/antaeusio/antaeus/internal/schematest"
)

func TestDecisionExecutionTextConformance(t *testing.T) {
	for _, tt := range schematest.TextCases(t, "../contracts/conformance/v0alpha1/text/whitespace.json") {
		for _, field := range []string{"adapterVersion", "fixtureVersion", "provider", "model"} {
			t.Run(tt.Name+"/"+field, func(t *testing.T) {
				var d Decision
				decodeFixture(t, filepath.Join("examples", "v0alpha1", "decision", "fixture-review.json"), &d)
				switch field {
				case "adapterVersion":
					d.Evaluator.AdapterVersion = tt.Value
				case "fixtureVersion":
					d.Evaluator.FixtureVersion = &tt.Value
				case "provider":
					d.Evaluator.Provider = tt.Value
				case "model":
					d.Evaluator.Model = tt.Value
				}
				want := tt.ECMANonBlank || (tt.Value == "" && (field == "provider" || field == "model"))
				if err := d.Validate(); (err == nil) != want {
					t.Fatalf("valid=%t want %t: %v", err == nil, want, err)
				}
			})
		}
	}
}
