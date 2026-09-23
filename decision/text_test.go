package decision

import (
	"errors"
	"path/filepath"
	"strings"
	"testing"

	"github.com/antaeusio/antaeus/internal/schematest"
)

func TestDecisionExecutionTextBoundaries(t *testing.T) {
	for _, field := range []struct {
		name  string
		limit int
		value func(*Decision) *string
	}{
		{"adapterVersion", 128, func(d *Decision) *string { return &d.Evaluator.AdapterVersion }},
		{"fixtureVersion", 128, func(d *Decision) *string { return d.Evaluator.FixtureVersion }},
		{"provider", 128, func(d *Decision) *string { return &d.Evaluator.Provider }},
		{"model", 256, func(d *Decision) *string { return &d.Evaluator.Model }},
	} {
		for _, tt := range []struct {
			name, value string
			valid       bool
		}{
			{"astral at limit", strings.Repeat("😀", field.limit), true},
			{"astral over limit", strings.Repeat("😀", field.limit+1), false},
			{"ASCII at limit", strings.Repeat("x", field.limit), true},
			{"ASCII over limit", strings.Repeat("x", field.limit+1), false},
			{"mixed at limit", " \t" + strings.Repeat("😀", field.limit-4) + "\u0085 ", true},
			{"replacement code point", "\ufffd", true},
			{"invalid leading byte", "\xff", false},
			{"invalid after text", "model\xff", false},
			{"truncated astral", "\xf0\x9f\x98", false},
			{"encoded surrogate", "\xed\xa0\x80", false},
		} {
			t.Run(field.name+"/"+tt.name, func(t *testing.T) {
				var d Decision
				decodeFixture(t, filepath.Join("examples", "v0alpha1", "decision", "fixture-review.json"), &d)
				if err := d.Validate(); err != nil {
					t.Fatalf("invalid control: %v", err)
				}
				value := field.value(&d)
				*value = tt.value
				err := d.Validate()
				if (err == nil) != tt.valid {
					t.Fatalf("valid=%t want %t: %v", err == nil, tt.valid, err)
				}
				var validation *ValidationError
				if err != nil && (!errors.As(err, &validation) || validation.Path != "$.evaluator."+field.name || validation.Code != "string.invalid") {
					t.Fatalf("wrong rejection boundary: %v", err)
				}
				if *value != tt.value {
					t.Fatal("validation changed input text")
				}
			})
		}
	}
}

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
