package evaluator

import (
	"strings"
	"testing"

	"github.com/antaeusio/antaeus/internal/schematest"
)

func TestResultExecutionTextBoundaries(t *testing.T) {
	for _, field := range []struct {
		name  string
		limit int
		value func(*Result) *string
	}{
		{"adapter version", 128, func(r *Result) *string { return &r.Metadata.AdapterVersion }},
		{"fixture version", 128, func(r *Result) *string { return &r.Metadata.FixtureVersion }},
		{"provider", 128, func(r *Result) *string { return &r.Metadata.Provider }},
		{"model", 256, func(r *Result) *string { return &r.Metadata.Model }},
		{"model revision", 256, func(r *Result) *string { return &r.Metadata.ModelRevision }},
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
				r := validResult()
				if err := ValidateResult(validRequest(), r); err != nil {
					t.Fatalf("invalid control: %v", err)
				}
				value := field.value(&r)
				*value = tt.value
				err := ValidateResult(validRequest(), r)
				if (err == nil) != tt.valid {
					t.Fatalf("valid=%t want %t: %v", err == nil, tt.valid, err)
				}
				if err != nil && !strings.HasPrefix(err.Error(), field.name+" must contain ") {
					t.Fatalf("wrong rejection boundary: %v", err)
				}
				if *value != tt.value {
					t.Fatal("validation changed input text")
				}
			})
		}
	}
}

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
