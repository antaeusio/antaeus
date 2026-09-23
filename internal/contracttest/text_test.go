package contracttest

import (
	"strings"
	"testing"

	"github.com/antaeusio/antaeus/internal/schematest"
)

func TestPublishedWhitespaceConformance(t *testing.T) {
	compiler := newCompiler(t)
	for _, field := range []struct {
		schema, pointer string
		ecma            bool
		limit           int
	}{
		{"execution-trace", "/$defs/text", true, 256},
		{"execution-trace", "/$defs/attempt/properties/provider", true, 128},
		{"decision", "/$defs/evaluator/properties/adapterVersion", true, 128},
		{"decision", "/$defs/evaluator/properties/fixtureVersion", true, 128},
		{"evaluator-profile", "/$defs/nonBlankVersion", false, 128},
		{"evaluator-profile", "/properties/metadata/properties/description", false, 4096},
		{"regression-suite", "/$defs/versionedIdentity/properties/version", false, 128},
	} {
		t.Run(field.schema+field.pointer, func(t *testing.T) {
			schema, err := compiler.Compile(schemaBase + field.schema + ".schema.json#" + field.pointer)
			if err != nil {
				t.Fatal(err)
			}
			for _, tt := range schematest.TextCases(t, contractsPath("conformance/v0alpha1/text/whitespace.json")) {
				want := tt.UnicodeNonBlank
				if field.ecma {
					want = tt.ECMANonBlank
				}
				if err := schema.Validate(tt.Value); (err == nil) != want {
					t.Errorf("%s: valid=%t want %t: %v", tt.Name, err == nil, want, err)
				}
			}
			if err := schema.Validate(strings.Repeat("😀", field.limit)); err != nil {
				t.Fatal(err)
			}
			if err := schema.Validate(strings.Repeat("😀", field.limit+1)); err == nil {
				t.Fatal("over-limit text accepted")
			}
		})
	}
}
