package contracttest

import (
	"path/filepath"
	"testing"
)

func TestAlpha2ContractExamples(t *testing.T) {
	compiler := newCompiler(t)
	for _, group := range []struct {
		directory, schema string
		files             []string
	}{
		{"decision", "decision.schema.json", []string{"allow.json", "allow-with-usage.json", "review.json", "deny.json", "failure.json", "fixture-review.json", "uncertain-review.json"}},
		{"request", "decision-request.schema.json", []string{"inline.json", "reference.json"}},
		{"evaluator-profile", "evaluator-profile.schema.json", []string{"quickstart-fixture.json", "semantic-routing.json"}},
		{"regression-result-set", "regression-result-set.schema.json", []string{"quickstart-passed.json"}},
	} {
		schema, err := compiler.Compile("https://antaeus.io/contracts/v0alpha2/" + group.schema)
		if err != nil {
			t.Fatal(err)
		}
		for _, name := range group.files {
			t.Run(group.directory+"/"+name, func(t *testing.T) {
				document := loadJSON(t, contractsPath(filepath.Join("examples", "v0alpha2", group.directory, name)))
				if err := schema.Validate(document); err != nil {
					t.Fatal(err)
				}
			})
		}
	}
}

func TestAlpha2RejectsLegacyProfile(t *testing.T) {
	schema, err := newCompiler(t).Compile("https://antaeus.io/contracts/v0alpha2/evaluator-profile.schema.json")
	if err != nil {
		t.Fatal(err)
	}
	document := loadJSON(t, contractsPath("examples/v0alpha1/evaluator-profile/semantic-routing.json"))
	if schema.Validate(document) == nil {
		t.Fatal("accepted legacy profile")
	}
}

func TestAlpha2RejectsInvalidDocuments(t *testing.T) {
	for _, tt := range []struct {
		name, schema, example string
		mutate                func(map[string]any)
	}{
		{"review failure object", "decision", "decision/uncertain-review.json", func(d map[string]any) {
			d["failure"] = map[string]any{"code": "evaluation.unresolved_rule", "stage": "evaluation", "retryable": false}
		}},
		{"failure without object", "decision", "decision/failure.json", func(d map[string]any) { delete(d, "failure") }},
		{"matched without outcome", "decision", "decision/allow.json", func(d map[string]any) { delete(d["ruleResults"].([]any)[1].(map[string]any), "outcome") }},
		{"legacy decision", "decision", "decision/allow.json", func(d map[string]any) { d["apiVersion"] = "decision.antaeus.io/v0alpha1" }},
		{"legacy request", "decision-request", "request/inline.json", func(d map[string]any) { d["apiVersion"] = "decision.antaeus.io/v0alpha1" }},
		{"legacy embedded decision", "regression-result-set", "regression-result-set/quickstart-passed.json", func(d map[string]any) {
			d["results"].([]any)[0].(map[string]any)["decision"].(map[string]any)["apiVersion"] = "decision.antaeus.io/v0alpha1"
		}},
	} {
		t.Run(tt.name, func(t *testing.T) {
			schema, err := newCompiler(t).Compile("https://antaeus.io/contracts/v0alpha2/" + tt.schema + ".schema.json")
			if err != nil {
				t.Fatal(err)
			}
			d := loadJSON(t, contractsPath("examples/v0alpha2/"+tt.example)).(map[string]any)
			if err := schema.Validate(d); err != nil {
				t.Fatalf("invalid test baseline: %v", err)
			}
			tt.mutate(d)
			if schema.Validate(d) == nil {
				t.Fatal("accepted invalid document")
			}
		})
	}
}
