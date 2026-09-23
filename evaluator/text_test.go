package evaluator

import (
	"testing"

	"github.com/antaeusio/antaeus/internal/schematest"
)

func TestResultExecutionTextConformance(t *testing.T) {
	for _, tt := range schematest.TextCases(t, "../contracts/conformance/v0alpha1/text/whitespace.json") {
		for _, field := range []string{"adapterVersion", "fixtureVersion", "provider", "model", "revision"} {
			t.Run(tt.Name+"/"+field, func(t *testing.T) {
				r := validResult()
				switch field {
				case "adapterVersion":
					r.Metadata.AdapterVersion = tt.Value
				case "fixtureVersion":
					r.Metadata.FixtureVersion = tt.Value
				case "provider":
					r.Metadata.Provider = tt.Value
				case "model":
					r.Metadata.Model = tt.Value
				case "revision":
					r.Metadata.ModelRevision = tt.Value
				}
				want := tt.ECMANonBlank || (tt.Value == "" && field != "adapterVersion" && field != "fixtureVersion")
				if err := ValidateResult(validRequest(), r); (err == nil) != want {
					t.Fatalf("valid=%t want %t: %v", err == nil, want, err)
				}
			})
		}
	}
}
