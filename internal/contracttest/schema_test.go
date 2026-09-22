package contracttest

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	jsonschema "github.com/santhosh-tekuri/jsonschema/v6"
)

func TestEvaluatorMetadataSchemaRejectsInvalidCombinations(t *testing.T) {
	schema, err := newCompiler(t).Compile(schemaBase + "decision.schema.json")
	if err != nil {
		t.Fatalf("compile decision schema: %v", err)
	}
	validData, err := os.ReadFile(contractsPath(filepath.Join("examples", "v0alpha1", "decision", "fixture-review.json")))
	if err != nil {
		t.Fatalf("read valid fixture decision: %v", err)
	}

	tests := []struct {
		name   string
		mutate func(map[string]any)
	}{
		{name: "missing adapter version", mutate: func(e map[string]any) { delete(e, "adapterVersion") }},
		{name: "blank adapter version", mutate: func(e map[string]any) { e["adapterVersion"] = "   " }},
		{name: "missing mode", mutate: func(e map[string]any) { delete(e, "mode") }},
		{name: "missing synthetic", mutate: func(e map[string]any) { delete(e, "synthetic") }},
		{name: "invalid mode", mutate: func(e map[string]any) { e["mode"] = "other" }},
		{name: "fixture not synthetic", mutate: func(e map[string]any) { e["synthetic"] = false }},
		{name: "fixture set missing", mutate: func(e map[string]any) { delete(e, "fixtureSet") }},
		{name: "fixture set blank", mutate: func(e map[string]any) { e["fixtureSet"] = "   " }},
		{name: "fixture set invalid pattern", mutate: func(e map[string]any) { e["fixtureSet"] = "UPPER" }},
		{name: "fixture version missing", mutate: func(e map[string]any) { delete(e, "fixtureVersion") }},
		{name: "fixture version blank", mutate: func(e map[string]any) { e["fixtureVersion"] = "   " }},
		{name: "semantic marked synthetic", mutate: func(e map[string]any) {
			e["mode"] = "semantic"
			e["synthetic"] = true
			delete(e, "fixtureSet")
			delete(e, "fixtureVersion")
		}},
		{name: "semantic with fixture set", mutate: func(e map[string]any) {
			e["mode"] = "semantic"
			e["synthetic"] = false
			delete(e, "fixtureVersion")
		}},
		{name: "semantic with fixture version", mutate: func(e map[string]any) {
			e["mode"] = "semantic"
			e["synthetic"] = false
			delete(e, "fixtureSet")
		}},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			var document map[string]any
			if err := json.Unmarshal(validData, &document); err != nil {
				t.Fatalf("decode valid fixture decision: %v", err)
			}
			test.mutate(document["evaluator"].(map[string]any))
			if err := schema.Validate(document); err == nil {
				t.Fatal("schema Validate() error = nil, want rejection")
			}
		})
	}
}

const schemaBase = "https://antaeus.io/contracts/v0alpha1/"

func TestContractExamplesAgainstSchemas(t *testing.T) {
	compiler := newCompiler(t)

	tests := []struct {
		name     string
		schema   string
		instance string
		valid    bool
	}{
		{
			name:     "policy example",
			schema:   "policy.schema.json",
			instance: filepath.Join("examples", "v0alpha1", "policy", "vendor-onboarding.json"),
			valid:    true,
		},
		{
			name:     "decision request reference",
			schema:   "decision-request.schema.json",
			instance: filepath.Join("examples", "v0alpha1", "request", "reference.json"),
			valid:    true,
		},
		{
			name:     "allow decision",
			schema:   "decision.schema.json",
			instance: filepath.Join("examples", "v0alpha1", "decision", "allow.json"),
			valid:    true,
		},
		{
			name:     "review decision",
			schema:   "decision.schema.json",
			instance: filepath.Join("examples", "v0alpha1", "decision", "review.json"),
			valid:    true,
		},
		{
			name:     "deny decision",
			schema:   "decision.schema.json",
			instance: filepath.Join("examples", "v0alpha1", "decision", "deny.json"),
			valid:    true,
		},
		{
			name:     "failure decision",
			schema:   "decision.schema.json",
			instance: filepath.Join("examples", "v0alpha1", "decision", "failure.json"),
			valid:    true,
		},
		{
			name:     "fixture decision",
			schema:   "decision.schema.json",
			instance: filepath.Join("examples", "v0alpha1", "decision", "fixture-review.json"),
			valid:    true,
		},
		{
			name:     "problem details",
			schema:   "problem.schema.json",
			instance: filepath.Join("examples", "v0alpha1", "problem", "invalid-policy.json"),
			valid:    true,
		},
		{
			name:     "quickstart fixture set",
			schema:   "fixture-set.schema.json",
			instance: filepath.Join("examples", "v0alpha1", "fixture-set", "quickstart.json"),
			valid:    true,
		},
		{
			name:     "fixture set unknown property",
			schema:   "fixture-set.schema.json",
			instance: filepath.Join("conformance", "v0alpha1", "fixture-set", "invalid-unknown-property.json"),
			valid:    false,
		},
		{
			name:     "fixture set bad digest",
			schema:   "fixture-set.schema.json",
			instance: filepath.Join("conformance", "v0alpha1", "fixture-set", "invalid-bad-digest.json"),
			valid:    false,
		},
		{
			name:     "fixture set empty rule results",
			schema:   "fixture-set.schema.json",
			instance: filepath.Join("conformance", "v0alpha1", "fixture-set", "invalid-empty-rule-results.json"),
			valid:    false,
		},
		{
			name:     "unknown policy property",
			schema:   "policy.schema.json",
			instance: filepath.Join("conformance", "v0alpha1", "policy", "invalid-unknown-property.json"),
			valid:    false,
		},
		{
			name:     "empty policy rules",
			schema:   "policy.schema.json",
			instance: filepath.Join("conformance", "v0alpha1", "policy", "invalid-empty-rules.json"),
			valid:    false,
		},
		{
			name:     "failure missing details",
			schema:   "decision.schema.json",
			instance: filepath.Join("conformance", "v0alpha1", "decision", "invalid-failure-missing-details.json"),
			valid:    false,
		},
		{
			name:     "non-match with outcome",
			schema:   "decision.schema.json",
			instance: filepath.Join("conformance", "v0alpha1", "decision", "invalid-non-match-outcome.json"),
			valid:    false,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			schema, err := compiler.Compile(schemaBase + test.schema)
			if err != nil {
				t.Fatalf("compile schema: %v", err)
			}
			instance := loadJSON(t, contractsPath(test.instance))
			err = schema.Validate(instance)
			if test.valid && err != nil {
				t.Fatalf("Validate() error = %v", err)
			}
			if !test.valid && err == nil {
				t.Fatal("Validate() error = nil, want schema rejection")
			}
		})
	}
}

func newCompiler(t *testing.T) *jsonschema.Compiler {
	t.Helper()
	compiler := jsonschema.NewCompiler()
	compiler.AssertFormat()
	for _, name := range []string{
		"common.schema.json",
		"policy.schema.json",
		"decision-request.schema.json",
		"decision.schema.json",
		"fixture-set.schema.json",
		"problem.schema.json",
	} {
		path := contractsPath(filepath.Join("schemas", "v0alpha1", name))
		document := loadJSON(t, path)
		if err := compiler.AddResource(schemaBase+name, document); err != nil {
			t.Fatalf("add schema %s: %v", name, err)
		}
	}
	return compiler
}

func loadJSON(t *testing.T, path string) any {
	t.Helper()
	file, err := os.Open(path)
	if err != nil {
		t.Fatalf("open %s: %v", path, err)
	}
	defer func() {
		if err := file.Close(); err != nil {
			t.Errorf("close %s: %v", path, err)
		}
	}()
	document, err := jsonschema.UnmarshalJSON(file)
	if err != nil {
		t.Fatalf("decode %s: %v", path, err)
	}
	return document
}

func contractsPath(relative string) string {
	return filepath.Join("..", "..", "contracts", relative)
}
