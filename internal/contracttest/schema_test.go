package contracttest

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
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

func TestEvaluatorProfileSchemaRejectsInvalidCombinations(t *testing.T) {
	schema, err := newCompiler(t).Compile(schemaBase + "evaluator-profile.schema.json")
	if err != nil {
		t.Fatalf("compile evaluator profile schema: %v", err)
	}
	validData, err := os.ReadFile(contractsPath(filepath.Join("examples", "v0alpha1", "evaluator-profile", "quickstart-fixture.json")))
	if err != nil {
		t.Fatalf("read evaluator profile: %v", err)
	}
	semantic := func(evaluator map[string]any) {
		evaluator["mode"] = "semantic"
		evaluator["adapter"] = map[string]any{"id": "io.example.semantic", "version": "1"}
		evaluator["parameters"] = map[string]any{}
	}
	tests := []struct {
		name   string
		mutate func(map[string]any, map[string]any, map[string]any)
	}{
		{name: "fixture mode with other adapter", mutate: func(_ map[string]any, evaluator, _ map[string]any) {
			evaluator["adapter"] = map[string]any{"id": "io.example.semantic", "version": "1"}
		}},
		{name: "fixture adapter with semantic mode", mutate: func(_ map[string]any, evaluator, _ map[string]any) { evaluator["mode"] = "semantic" }},
		{name: "fixture provider", mutate: func(_ map[string]any, evaluator, _ map[string]any) { evaluator["provider"] = "example" }},
		{name: "fixture model", mutate: func(_ map[string]any, evaluator, _ map[string]any) { evaluator["model"] = "model-1" }},
		{name: "fixture template", mutate: func(_ map[string]any, evaluator, _ map[string]any) {
			evaluator["instructionTemplate"] = map[string]any{"digest": "sha256:0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"}
		}},
		{name: "fixture parameter", mutate: func(_ map[string]any, evaluator, _ map[string]any) {
			evaluator["parameters"].(map[string]any)["endpoint"] = "https://example.invalid"
		}},
		{name: "fallback missing classes", mutate: func(_ map[string]any, _ map[string]any, routing map[string]any) {
			routing["fallbacks"] = []any{"other"}
		}},
		{name: "classes without fallback", mutate: func(_ map[string]any, _ map[string]any, routing map[string]any) {
			routing["fallbackOn"] = []any{"timeout"}
		}},
		{name: "escalate missing evaluator", mutate: func(_ map[string]any, _ map[string]any, routing map[string]any) {
			routing["confidence"] = map[string]any{"enabled": true, "minimumAccepted": 0.5, "onLowConfidence": "escalate"}
		}},
		{name: "unused escalation", mutate: func(_ map[string]any, _ map[string]any, routing map[string]any) { routing["escalation"] = "other" }},
		{name: "disabled confidence threshold", mutate: func(_ map[string]any, _ map[string]any, routing map[string]any) {
			routing["confidence"].(map[string]any)["minimumAccepted"] = 0.5
		}},
		{name: "template missing digest", mutate: func(_ map[string]any, evaluator, _ map[string]any) {
			semantic(evaluator)
			evaluator["instructionTemplate"] = map[string]any{"id": "io.example.template"}
		}},
		{name: "timeout below bound", mutate: func(_ map[string]any, evaluator, _ map[string]any) { evaluator["timeoutMs"] = float64(0) }},
		{name: "retry count above bound", mutate: func(_ map[string]any, evaluator, _ map[string]any) {
			retry := evaluator["retry"].(map[string]any)
			retry["maxAttempts"] = float64(6)
			retry["retryOn"] = []any{"timeout"}
			retry["initialBackoffMs"] = float64(100)
			retry["maxBackoffMs"] = float64(1000)
			retry["multiplier"] = float64(2)
		}},
		{name: "retry missing class", mutate: func(_ map[string]any, evaluator, _ map[string]any) {
			retry := evaluator["retry"].(map[string]any)
			retry["maxAttempts"] = float64(2)
			retry["initialBackoffMs"] = float64(100)
			retry["maxBackoffMs"] = float64(1000)
			retry["multiplier"] = float64(2)
		}},
		{name: "retry class without retry", mutate: func(_ map[string]any, evaluator, _ map[string]any) {
			evaluator["retry"].(map[string]any)["retryOn"] = []any{"timeout"}
		}},
		{name: "backoff without retry", mutate: func(_ map[string]any, evaluator, _ map[string]any) {
			retry := evaluator["retry"].(map[string]any)
			retry["initialBackoffMs"] = float64(100)
			retry["maxBackoffMs"] = float64(1000)
			retry["multiplier"] = float64(2)
		}},
		{name: "retry missing schedule", mutate: func(_ map[string]any, evaluator, _ map[string]any) {
			retry := evaluator["retry"].(map[string]any)
			retry["maxAttempts"] = float64(2)
			retry["retryOn"] = []any{"timeout"}
		}},
		{name: "unsafe provider", mutate: func(_ map[string]any, evaluator, _ map[string]any) {
			semantic(evaluator)
			evaluator["provider"] = "https://internal.invalid"
		}},
		{name: "mixed evaluator modes", mutate: func(spec map[string]any, evaluator, _ map[string]any) {
			clone := make(map[string]any, len(evaluator))
			for key, value := range evaluator {
				clone[key] = value
			}
			semantic(clone)
			clone["id"] = "semantic"
			spec["evaluators"] = append(spec["evaluators"].([]any), clone)
		}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			var document map[string]any
			if err := json.Unmarshal(validData, &document); err != nil {
				t.Fatalf("decode valid profile: %v", err)
			}
			spec := document["spec"].(map[string]any)
			evaluator := spec["evaluators"].([]any)[0].(map[string]any)
			routing := spec["routing"].(map[string]any)
			test.mutate(spec, evaluator, routing)
			if err := schema.Validate(document); err == nil {
				t.Fatal("schema Validate() error = nil, want rejection")
			}
		})
	}
}

func TestEvaluatorProfileSchemaAcceptsConfidenceWithoutEscalation(t *testing.T) {
	schema, err := newCompiler(t).Compile(schemaBase + "evaluator-profile.schema.json")
	if err != nil {
		t.Fatalf("compile evaluator profile schema: %v", err)
	}
	data, err := os.ReadFile(contractsPath(filepath.Join("examples", "v0alpha1", "evaluator-profile", "semantic-routing.json")))
	if err != nil {
		t.Fatalf("read semantic profile: %v", err)
	}
	var document map[string]any
	if err := json.Unmarshal(data, &document); err != nil {
		t.Fatalf("decode semantic profile: %v", err)
	}
	routing := document["spec"].(map[string]any)["routing"].(map[string]any)
	delete(routing, "escalation")
	routing["confidence"].(map[string]any)["onLowConfidence"] = "indeterminate"
	if err := schema.Validate(document); err != nil {
		t.Fatalf("Validate() error = %v", err)
	}
}

func TestLocalSecretBindingsSchemaRejectsInvalidCombinations(t *testing.T) {
	schema, err := newCompiler(t).Compile(schemaBase + "local-secret-bindings.schema.json")
	if err != nil {
		t.Fatalf("compile local secret bindings schema: %v", err)
	}
	validData, err := os.ReadFile(contractsPath(filepath.Join("examples", "v0alpha1", "local-secret-bindings", "development.json")))
	if err != nil {
		t.Fatalf("read local secret bindings: %v", err)
	}
	tests := []struct {
		name   string
		mutate func(map[string]any)
	}{
		{name: "empty bindings", mutate: func(document map[string]any) { document["secretBindings"] = map[string]any{} }},
		{name: "too many adapters", mutate: func(document map[string]any) {
			bindings := make(map[string]any, 17)
			for i := 0; i < 17; i++ {
				bindings[fmt.Sprintf("io.example.adapter%d", i)] = oneSlotBinding()
			}
			document["secretBindings"] = bindings
		}},
		{name: "too many slots", mutate: func(document map[string]any) {
			slots := make(map[string]any, 17)
			for i := 0; i < 17; i++ {
				slots[fmt.Sprintf("slot-%d", i)] = environmentReference()
			}
			document["secretBindings"] = map[string]any{"io.example.semantic": slots}
		}},
		{name: "invalid adapter ID", mutate: func(document map[string]any) {
			document["secretBindings"] = map[string]any{"semantic": oneSlotBinding()}
		}},
		{name: "fixture adapter", mutate: func(document map[string]any) {
			document["secretBindings"] = map[string]any{"io.antaeus.fixture": oneSlotBinding()}
		}},
		{name: "invalid slot ID", mutate: func(document map[string]any) {
			document["secretBindings"] = map[string]any{"io.example.semantic": map[string]any{"Provider": environmentReference()}}
		}},
		{name: "missing reference name", mutate: func(document map[string]any) {
			document["secretBindings"] = map[string]any{"io.example.semantic": map[string]any{"provider-api-key": map[string]any{"source": "environment"}}}
		}},
		{name: "empty reference name", mutate: func(document map[string]any) {
			document["secretBindings"] = map[string]any{"io.example.semantic": map[string]any{"provider-api-key": map[string]any{"source": "environment", "name": ""}}}
		}},
		{name: "reference name starts with digit", mutate: func(document map[string]any) {
			document["secretBindings"] = map[string]any{"io.example.semantic": map[string]any{"provider-api-key": map[string]any{"source": "environment", "name": "1EXAMPLE_API_KEY"}}}
		}},
		{name: "reference name too long", mutate: func(document map[string]any) {
			document["secretBindings"] = map[string]any{"io.example.semantic": map[string]any{"provider-api-key": map[string]any{"source": "environment", "name": "K" + strings.Repeat("A", 128)}}}
		}},
		{name: "adapter ID too long", mutate: func(document map[string]any) {
			document["secretBindings"] = map[string]any{"io." + strings.Repeat("a", 126): oneSlotBinding()}
		}},
		{name: "bare string reference", mutate: func(document map[string]any) {
			document["secretBindings"] = map[string]any{"io.example.semantic": map[string]any{"provider-api-key": "EXAMPLE_API_KEY"}}
		}},
		{name: "wrong API version", mutate: func(document map[string]any) { document["apiVersion"] = "config.antaeus.io/v1" }},
		{name: "wrong kind", mutate: func(document map[string]any) { document["kind"] = "SecretBindings" }},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			var document map[string]any
			if err := json.Unmarshal(validData, &document); err != nil {
				t.Fatalf("decode local secret bindings: %v", err)
			}
			test.mutate(document)
			if err := schema.Validate(document); err == nil {
				t.Fatal("schema Validate() error = nil, want rejection")
			}
		})
	}
}

func TestLocalSecretBindingsSchemaAcceptsPublishedLimits(t *testing.T) {
	schema, err := newCompiler(t).Compile(schemaBase + "local-secret-bindings.schema.json")
	if err != nil {
		t.Fatalf("compile local secret bindings schema: %v", err)
	}
	bindings := make(map[string]any, 16)
	for adapter := 0; adapter < 16; adapter++ {
		slots := make(map[string]any, 16)
		for slot := 0; slot < 16; slot++ {
			slots[fmt.Sprintf("slot-%d", slot)] = environmentReference()
		}
		bindings[fmt.Sprintf("io.example.adapter%d", adapter)] = slots
	}
	document := map[string]any{
		"apiVersion":     "config.antaeus.io/v0alpha1",
		"kind":           "LocalSecretBindings",
		"secretBindings": bindings,
	}
	if err := schema.Validate(document); err != nil {
		t.Fatalf("Validate() error = %v", err)
	}
}

func TestLocalSecretBindingsSchemaAcceptsIdentifierLimits(t *testing.T) {
	schema, err := newCompiler(t).Compile(schemaBase + "local-secret-bindings.schema.json")
	if err != nil {
		t.Fatalf("compile local secret bindings schema: %v", err)
	}
	document := map[string]any{
		"apiVersion": "config.antaeus.io/v0alpha1",
		"kind":       "LocalSecretBindings",
		"secretBindings": map[string]any{
			"io." + strings.Repeat("a", 125): map[string]any{
				"provider-api-key": map[string]any{
					"source": "environment",
					"name":   "K" + strings.Repeat("A", 127),
				},
			},
		},
	}
	if err := schema.Validate(document); err != nil {
		t.Fatalf("Validate() error = %v", err)
	}
}

func oneSlotBinding() map[string]any {
	return map[string]any{"provider-api-key": environmentReference()}
}

func environmentReference() map[string]any {
	return map[string]any{"source": "environment", "name": "EXAMPLE_API_KEY"}
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
			name:     "quickstart evaluator profile",
			schema:   "evaluator-profile.schema.json",
			instance: filepath.Join("examples", "v0alpha1", "evaluator-profile", "quickstart-fixture.json"),
			valid:    true,
		},
		{
			name:     "semantic routing evaluator profile",
			schema:   "evaluator-profile.schema.json",
			instance: filepath.Join("examples", "v0alpha1", "evaluator-profile", "semantic-routing.json"),
			valid:    true,
		},
		{
			name:     "local secret bindings",
			schema:   "local-secret-bindings.schema.json",
			instance: filepath.Join("examples", "v0alpha1", "local-secret-bindings", "development.json"),
			valid:    true,
		},
		{
			name:     "local secret bindings with raw value",
			schema:   "local-secret-bindings.schema.json",
			instance: filepath.Join("conformance", "v0alpha1", "local-secret-bindings", "invalid-secret-value.json"),
			valid:    false,
		},
		{
			name:     "local secret bindings unsupported source",
			schema:   "local-secret-bindings.schema.json",
			instance: filepath.Join("conformance", "v0alpha1", "local-secret-bindings", "invalid-source.json"),
			valid:    false,
		},
		{
			name:     "local secret bindings invalid environment name",
			schema:   "local-secret-bindings.schema.json",
			instance: filepath.Join("conformance", "v0alpha1", "local-secret-bindings", "invalid-environment-name.json"),
			valid:    false,
		},
		{
			name:     "local secret bindings empty adapter",
			schema:   "local-secret-bindings.schema.json",
			instance: filepath.Join("conformance", "v0alpha1", "local-secret-bindings", "invalid-empty-adapter.json"),
			valid:    false,
		},
		{
			name:     "local secret bindings dotenv path",
			schema:   "local-secret-bindings.schema.json",
			instance: filepath.Join("conformance", "v0alpha1", "local-secret-bindings", "invalid-dotenv-path.json"),
			valid:    false,
		},
		{
			name:     "local secret bindings hosted identifier",
			schema:   "local-secret-bindings.schema.json",
			instance: filepath.Join("conformance", "v0alpha1", "local-secret-bindings", "invalid-hosted-identifier.json"),
			valid:    false,
		},
		{
			name:     "local secret bindings behavior override",
			schema:   "local-secret-bindings.schema.json",
			instance: filepath.Join("conformance", "v0alpha1", "local-secret-bindings", "invalid-behavior-override.json"),
			valid:    false,
		},
		{
			name:     "local secret bindings dotenv source",
			schema:   "local-secret-bindings.schema.json",
			instance: filepath.Join("conformance", "v0alpha1", "local-secret-bindings", "invalid-dotenv-source.json"),
			valid:    false,
		},
		{
			name:     "evaluator profile unknown property",
			schema:   "evaluator-profile.schema.json",
			instance: filepath.Join("conformance", "v0alpha1", "evaluator-profile", "invalid-unknown-property.json"),
			valid:    false,
		},
		{
			name:     "evaluator profile terminal outcome",
			schema:   "evaluator-profile.schema.json",
			instance: filepath.Join("conformance", "v0alpha1", "evaluator-profile", "invalid-terminal-outcome.json"),
			valid:    false,
		},
		{
			name:     "evaluator profile terminal failure",
			schema:   "evaluator-profile.schema.json",
			instance: filepath.Join("conformance", "v0alpha1", "evaluator-profile", "invalid-terminal-failure.json"),
			valid:    false,
		},
		{
			name:     "fixture evaluator credential",
			schema:   "evaluator-profile.schema.json",
			instance: filepath.Join("conformance", "v0alpha1", "evaluator-profile", "invalid-fixture-credential.json"),
			valid:    false,
		},
		{
			name:     "evaluator profile incomplete confidence",
			schema:   "evaluator-profile.schema.json",
			instance: filepath.Join("conformance", "v0alpha1", "evaluator-profile", "invalid-confidence.json"),
			valid:    false,
		},
		{
			name:     "evaluator profile mixed modes",
			schema:   "evaluator-profile.schema.json",
			instance: filepath.Join("conformance", "v0alpha1", "evaluator-profile", "invalid-mixed-modes.json"),
			valid:    false,
		},
		{
			name:     "deterministic mode with other adapter",
			schema:   "evaluator-profile.schema.json",
			instance: filepath.Join("conformance", "v0alpha1", "evaluator-profile", "invalid-fixture-adapter.json"),
			valid:    false,
		},
		{
			name:     "quickstart regression suite",
			schema:   "regression-suite.schema.json",
			instance: filepath.Join("examples", "v0alpha1", "regression-suite", "quickstart.json"),
			valid:    true,
		},
		{
			name:     "passed regression result set",
			schema:   "regression-result-set.schema.json",
			instance: filepath.Join("examples", "v0alpha1", "regression-result-set", "quickstart-passed.json"),
			valid:    true,
		},
		{
			name:     "regression suite unknown property",
			schema:   "regression-suite.schema.json",
			instance: filepath.Join("conformance", "v0alpha1", "regression-suite", "invalid-unknown-property.json"),
			valid:    false,
		},
		{
			name:     "regression suite empty description",
			schema:   "regression-suite.schema.json",
			instance: filepath.Join("conformance", "v0alpha1", "regression-suite", "invalid-empty-description.json"),
			valid:    false,
		},
		{
			name:     "regression suite whitespace description",
			schema:   "regression-suite.schema.json",
			instance: filepath.Join("conformance", "v0alpha1", "regression-suite", "invalid-whitespace-description.json"),
			valid:    false,
		},
		{
			name:     "regression suite null description",
			schema:   "regression-suite.schema.json",
			instance: filepath.Join("conformance", "v0alpha1", "regression-suite", "invalid-null-description.json"),
			valid:    false,
		},
		{
			name:     "regression suite input at maximum depth",
			schema:   "regression-suite.schema.json",
			instance: filepath.Join("conformance", "v0alpha1", "regression-suite", "valid-input-max-depth.json"),
			valid:    true,
		},
		{
			name:     "regression suite NBSP description",
			schema:   "regression-suite.schema.json",
			instance: filepath.Join("conformance", "v0alpha1", "regression-suite", "invalid-description-nbsp.json"),
			valid:    false,
		},
		{
			name:     "regression suite NEL description",
			schema:   "regression-suite.schema.json",
			instance: filepath.Join("conformance", "v0alpha1", "regression-suite", "invalid-description-nel.json"),
			valid:    false,
		},
		{
			name:     "regression suite whitespace version",
			schema:   "regression-suite.schema.json",
			instance: filepath.Join("conformance", "v0alpha1", "regression-suite", "invalid-whitespace-version.json"),
			valid:    false,
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
		"evaluator-profile.schema.json",
		"local-secret-bindings.schema.json",
		"regression-suite.schema.json",
		"regression-result-set.schema.json",
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
