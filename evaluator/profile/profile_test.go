package profile

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/antaeusio/antaeus/internal/schematest"

	jsonschema "github.com/santhosh-tekuri/jsonschema/v6"
	"go.yaml.in/yaml/v3"
)

func TestParseExamples(t *testing.T) {
	for _, name := range []string{"quickstart-fixture.json", "semantic-routing.json"} {
		t.Run(name, func(t *testing.T) {
			source := readContract(t, "examples", "v0alpha1", "evaluator-profile", name)
			artifact, err := Parse(source, FormatJSON)
			if err != nil {
				t.Fatalf("Parse() error = %v", err)
			}
			if artifact.APIVersion != APIVersion || artifact.Kind != Kind {
				t.Fatalf("artifact discriminator = %q %q", artifact.APIVersion, artifact.Kind)
			}
		})
	}
}

func TestJSONAndYAMLProduceSameIdentity(t *testing.T) {
	jsonArtifact, err := Parse(readContract(t, "examples", "v0alpha1", "evaluator-profile", "quickstart-fixture.json"), FormatJSON)
	if err != nil {
		t.Fatalf("Parse(JSON) error = %v", err)
	}
	yamlArtifact, err := Parse([]byte(quickstartYAML), FormatYAML)
	if err != nil {
		t.Fatalf("Parse(YAML) error = %v", err)
	}
	jsonCanonical, err := jsonArtifact.CanonicalJSON()
	if err != nil {
		t.Fatalf("CanonicalJSON(JSON) error = %v", err)
	}
	yamlCanonical, err := yamlArtifact.CanonicalJSON()
	if err != nil {
		t.Fatalf("CanonicalJSON(YAML) error = %v", err)
	}
	if !bytes.Equal(jsonCanonical, yamlCanonical) {
		t.Fatalf("canonical forms differ\nJSON: %s\nYAML: %s", jsonCanonical, yamlCanonical)
	}
	jsonDigest, _ := jsonArtifact.Digest()
	yamlDigest, _ := yamlArtifact.Digest()
	if jsonDigest != yamlDigest || !strings.HasPrefix(jsonDigest, "sha256:") {
		t.Fatalf("digests = %q and %q", jsonDigest, yamlDigest)
	}
	const wantDigest = "sha256:6ba2d4b73fae619e064b77d1799e29fd1057c6ffdfb7715151d912a5b057d929"
	if jsonDigest != wantDigest {
		t.Fatalf("digest = %q, want %q", jsonDigest, wantDigest)
	}
}

func TestParseRejectsConformanceProfiles(t *testing.T) {
	expected := map[string]string{
		"invalid-confidence.json":               "confidence.threshold_invalid",
		"invalid-fixture-adapter.json":          "fixture_adapter.invalid",
		"invalid-fixture-credential.json":       "fixture_configuration.invalid",
		"invalid-mixed-modes.json":              "evaluator_mode.mixed",
		"invalid-number-overflow.json":          "source.number",
		"invalid-semantic-number-overflow.json": "source.number",
		"invalid-semantic-unsafe-integer.json":  "source.number",
		"invalid-terminal-failure.json":         "terminal.invalid",
		"invalid-terminal-outcome.json":         "terminal.invalid",
		"invalid-unknown-property.json":         "source.schema",
		"invalid-unsafe-integer.json":           "source.number",
	}
	entries, err := os.ReadDir(contractsPath("conformance", "v0alpha1", "evaluator-profile"))
	if err != nil {
		t.Fatal(err)
	}
	seen := 0
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasPrefix(entry.Name(), "invalid-") {
			continue
		}
		want, exists := expected[entry.Name()]
		if !exists {
			t.Fatalf("missing expected error code for %s", entry.Name())
		}
		seen++
		t.Run(entry.Name(), func(t *testing.T) {
			_, err := Parse(readContract(t, "conformance", "v0alpha1", "evaluator-profile", entry.Name()), FormatJSON)
			if got := contractErrorCode(err); got != want {
				t.Fatalf("error = %v (code %q), want code %q", err, got, want)
			}
		})
	}
	if seen != len(expected) {
		t.Fatalf("tested %d invalid fixtures, want %d", seen, len(expected))
	}
}

func TestValidateRelationalInvariants(t *testing.T) {
	semantic := mustParseExample(t, "semantic-routing.json")
	tests := []struct {
		name   string
		mutate func(*Artifact)
		code   string
	}{
		{name: "duplicate credential slot", code: "credential_slot.duplicate", mutate: func(a *Artifact) {
			a.Spec.CredentialSlots = append(a.Spec.CredentialSlots, a.Spec.CredentialSlots[0])
		}},
		{name: "duplicate evaluator", code: "evaluator_id.duplicate", mutate: func(a *Artifact) {
			a.Spec.Evaluators[1].ID = a.Spec.Evaluators[0].ID
		}},
		{name: "unknown primary", code: "route.unknown", mutate: func(a *Artifact) { a.Spec.Routing.Primary = "missing" }},
		{name: "duplicate route position", code: "route.duplicate", mutate: func(a *Artifact) {
			a.Spec.Routing.Fallbacks[0] = a.Spec.Routing.Primary
		}},
		{name: "attempt exceeds total timeout", code: "timeout.invalid", mutate: func(a *Artifact) {
			a.Spec.TotalTimeoutMS = 100
		}},
		{name: "initial backoff exceeds maximum", code: "retry.backoff_invalid", mutate: func(a *Artifact) {
			initial, maximum := 200, 100
			a.Spec.Evaluators[0].Retry.InitialBackoffMS = &initial
			a.Spec.Evaluators[0].Retry.MaxBackoffMS = &maximum
		}},
		{name: "confidence capability missing", code: "confidence.capability_missing", mutate: func(a *Artifact) {
			a.Spec.Evaluators[2].RequiredCapabilities = []string{"json-input", "structured-rule-results"}
		}},
		{name: "credential slot missing", code: "credential_slot.unknown", mutate: func(a *Artifact) {
			missing := "other-key"
			a.Spec.Evaluators[0].CredentialSlot = &missing
		}},
		{name: "fallback classes without fallback", code: "fallback_classes.unused", mutate: func(a *Artifact) {
			a.Spec.Routing.Fallbacks = []string{}
			a.Spec.Routing.FallbackOn = []TransientFailure{FailureTimeout}
		}},
		{name: "fallback missing classes", code: "fallback_classes.invalid", mutate: func(a *Artifact) {
			a.Spec.Routing.FallbackOn = nil
		}},
		{name: "single attempt with backoff", code: "retry.single_attempt", mutate: func(a *Artifact) {
			backoff := 1
			a.Spec.Evaluators[1].Retry.InitialBackoffMS = &backoff
		}},
		{name: "unused escalation", code: "escalation.unused", mutate: func(a *Artifact) {
			action := LowConfidenceIndeterminate
			a.Spec.Routing.Confidence.OnLowConfidence = &action
		}},
		{name: "disabled confidence with threshold", code: "confidence.disabled", mutate: func(a *Artifact) {
			a.Spec.Routing.Confidence.Enabled = false
		}},
		{name: "model URL", code: "model.invalid", mutate: func(a *Artifact) {
			value := "https://model.example"
			a.Spec.Evaluators[0].Model = &value
		}},
		{name: "bad template digest", code: "digest.invalid", mutate: func(a *Artifact) {
			a.Spec.Evaluators[0].InstructionTemplate.Digest = "sha256:bad"
		}},
		{name: "whitespace description", code: "string.empty", mutate: func(a *Artifact) {
			value := "   "
			a.Metadata.Description = &value
		}},
		{name: "too many parameters", code: "parameters.invalid", mutate: func(a *Artifact) {
			parameters := make(map[string]any, MaxParameters+1)
			for i := 0; i <= MaxParameters; i++ {
				parameters[fmt.Sprintf("p%d", i)] = i
			}
			a.Spec.Evaluators[0].Parameters = parameters
		}},
		{name: "unknown escalation", code: "route.unknown", mutate: func(a *Artifact) {
			value := "missing"
			a.Spec.Routing.Escalation = &value
		}},
		{name: "unknown fallback", code: "route.unknown", mutate: func(a *Artifact) {
			a.Spec.Routing.Fallbacks[0] = "missing"
		}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			artifact := cloneArtifact(t, semantic)
			test.mutate(&artifact)
			var validationErr *ValidationError
			if err := artifact.Validate(); !errors.As(err, &validationErr) || validationErr.Code != test.code {
				t.Fatalf("Validate() error = %v, want code %q", err, test.code)
			}
		})
	}
}

func TestValidateParameters(t *testing.T) {
	fixture := mustParseExample(t, "quickstart-fixture.json")
	if err := fixture.ValidateParameters(nil); err != nil {
		t.Fatalf("fixture ValidateParameters(nil) error = %v", err)
	}
	semantic := mustParseExample(t, "semantic-routing.json")
	if err := semantic.ValidateParameters(nil); err == nil {
		t.Fatal("semantic ValidateParameters(nil) error = nil")
	}
	calls := 0
	validator := parameterValidatorFunc(func(parameters json.RawMessage) error {
		calls++
		if string(parameters) != "{}" {
			return fmt.Errorf("parameters = %s", parameters)
		}
		return nil
	})
	registry := ParameterValidators{
		{ID: "io.example.semantic", Version: "1.0.0"}: validator,
	}
	if err := semantic.ValidateParameters(registry); err != nil {
		t.Fatalf("ValidateParameters() error = %v", err)
	}
	if calls != len(semantic.Spec.Evaluators) {
		t.Fatalf("validator calls = %d, want %d", calls, len(semantic.Spec.Evaluators))
	}

	t.Run("exact adapter version required", func(t *testing.T) {
		artifact := cloneArtifact(t, semantic)
		artifact.Spec.Evaluators[0].Adapter.Version = "2.0.0"
		err := artifact.ValidateParameters(registry)
		if got := contractErrorCode(err); got != "adapter_schema.missing" {
			t.Fatalf("error = %v (code %q)", err, got)
		}
	})

	t.Run("nil validator fails closed", func(t *testing.T) {
		err := semantic.ValidateParameters(nilValidatorRegistry{})
		if got := contractErrorCode(err); got != "adapter_schema.missing" {
			t.Fatalf("error = %v (code %q)", err, got)
		}
	})

	t.Run("validator rejection is bounded and wrapped", func(t *testing.T) {
		cause := errors.New(strings.Repeat("rejected", 100))
		rejecting := ParameterValidators{
			{ID: "io.example.semantic", Version: "1.0.0"}: parameterValidatorFunc(func(json.RawMessage) error { return cause }),
		}
		err := semantic.ValidateParameters(rejecting)
		var validationErr *ValidationError
		if !errors.As(err, &validationErr) || validationErr.Code != "parameters.adapter_invalid" || !errors.Is(err, cause) {
			t.Fatalf("error = %v, want wrapped adapter rejection", err)
		}
		if len(validationErr.Message) > 512 {
			t.Fatalf("message length = %d", len(validationErr.Message))
		}
	})

	t.Run("validator receives canonical parameters", func(t *testing.T) {
		artifact := cloneArtifact(t, semantic)
		artifact.Spec.Evaluators[0].Parameters = map[string]any{"z": json.Number("1.0"), "a": json.Number("2e0")}
		var first json.RawMessage
		canonicalRegistry := ParameterValidators{
			{ID: "io.example.semantic", Version: "1.0.0"}: parameterValidatorFunc(func(parameters json.RawMessage) error {
				if first == nil {
					first = append(json.RawMessage(nil), parameters...)
				}
				return nil
			}),
		}
		if err := artifact.ValidateParameters(canonicalRegistry); err != nil {
			t.Fatalf("ValidateParameters() error = %v", err)
		}
		if string(first) != `{"a":2,"z":1}` {
			t.Fatalf("parameters = %s", first)
		}
	})
}

func TestSemanticJSONAndYAMLProduceSameIdentity(t *testing.T) {
	source := readContract(t, "examples", "v0alpha1", "evaluator-profile", "semantic-routing.json")
	var document map[string]any
	if err := json.Unmarshal(source, &document); err != nil {
		t.Fatal(err)
	}
	evaluators := document["spec"].(map[string]any)["evaluators"].([]any)
	evaluators[0].(map[string]any)["parameters"] = map[string]any{"temperature": 0.1, "samples": 1000.0}
	jsonSource, err := json.Marshal(document)
	if err != nil {
		t.Fatal(err)
	}
	yamlSource, err := yaml.Marshal(document)
	if err != nil {
		t.Fatal(err)
	}
	jsonArtifact, err := Parse(jsonSource, FormatJSON)
	if err != nil {
		t.Fatalf("Parse(JSON) error = %v", err)
	}
	yamlArtifact, err := Parse(yamlSource, FormatYAML)
	if err != nil {
		t.Fatalf("Parse(YAML) error = %v", err)
	}
	jsonDigest, _ := jsonArtifact.Digest()
	yamlDigest, _ := yamlArtifact.Digest()
	if jsonDigest != yamlDigest {
		t.Fatalf("digests = %q and %q", jsonDigest, yamlDigest)
	}
	canonical, err := jsonArtifact.CanonicalJSON()
	if err != nil {
		t.Fatalf("CanonicalJSON() error = %v", err)
	}
	roundTripped, err := Parse(canonical, FormatJSON)
	if err != nil {
		t.Fatalf("Parse(CanonicalJSON()) error = %v", err)
	}
	roundTripDigest, _ := roundTripped.Digest()
	if roundTripDigest != jsonDigest {
		t.Fatalf("round-trip digest = %q, want %q", roundTripDigest, jsonDigest)
	}
}

func TestProgrammaticProfileHonorsWholeDocumentLimits(t *testing.T) {
	artifact := mustParseExample(t, "semantic-routing.json")
	var nested any = "leaf"
	for i := 0; i < MaxNestingDepth-4; i++ {
		nested = []any{nested}
	}
	artifact.Spec.Evaluators[0].Parameters = map[string]any{"nested": nested}
	if got := contractErrorCode(artifact.Validate()); got != "source.depth" {
		t.Fatalf("depth error code = %q", got)
	}

	artifact = mustParseExample(t, "semantic-routing.json")
	artifact.Spec.Evaluators[0].Parameters = map[string]any{"nodes": make([]any, MaxParsedNodes-10)}
	if got := contractErrorCode(artifact.Validate()); got != "source.nodes" {
		t.Fatalf("node error code = %q", got)
	}
}

func TestHTMLHeavyProfileUsesCanonicalSize(t *testing.T) {
	artifact := mustParseExample(t, "semantic-routing.json")
	artifact.Spec.Evaluators[0].Parameters = map[string]any{"content": strings.Repeat("<&", 100_000)}
	var buffer bytes.Buffer
	encoder := json.NewEncoder(&buffer)
	encoder.SetEscapeHTML(false)
	if err := encoder.Encode(artifact); err != nil {
		t.Fatal(err)
	}
	source := bytes.TrimSuffix(buffer.Bytes(), []byte{'\n'})
	if len(source) >= MaxSourceBytes {
		t.Fatalf("test source size = %d, want below %d", len(source), MaxSourceBytes)
	}
	parsed, err := Parse(source, FormatJSON)
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}
	canonical, err := parsed.CanonicalJSON()
	if err != nil {
		t.Fatalf("CanonicalJSON() error = %v", err)
	}
	if len(canonical) >= MaxSourceBytes {
		t.Fatalf("canonical size = %d, want below %d", len(canonical), MaxSourceBytes)
	}
	if _, err := Parse(canonical, FormatJSON); err != nil {
		t.Fatalf("Parse(canonical) error = %v", err)
	}
}

func TestIntegerSpellingsHaveParity(t *testing.T) {
	source := string(readContract(t, "examples", "v0alpha1", "evaluator-profile", "quickstart-fixture.json"))
	jsonSource := strings.Replace(source, `"totalTimeoutMs": 2000`, `"totalTimeoutMs": 2e3`, 1)
	yamlSource := strings.Replace(quickstartYAML, "totalTimeoutMs: 2000", "totalTimeoutMs: 2000.0", 1)
	jsonArtifact, err := Parse([]byte(jsonSource), FormatJSON)
	if err != nil {
		t.Fatalf("Parse(JSON) error = %v", err)
	}
	yamlArtifact, err := Parse([]byte(yamlSource), FormatYAML)
	if err != nil {
		t.Fatalf("Parse(YAML) error = %v", err)
	}
	jsonDigest, _ := jsonArtifact.Digest()
	yamlDigest, _ := yamlArtifact.Digest()
	if jsonDigest != yamlDigest {
		t.Fatalf("digests = %q and %q", jsonDigest, yamlDigest)
	}
}

func profileSchema(t *testing.T) *jsonschema.Schema {
	t.Helper()
	compiler := jsonschema.NewCompiler()
	compiler.UseRegexpEngine(schematest.CompilePattern)
	compiler.AssertFormat()
	for _, name := range []string{"common.schema.json", "evaluator-profile.schema.json"} {
		file, err := os.Open(contractsPath("schemas", "v0alpha1", name))
		if err != nil {
			t.Fatal(err)
		}
		document, err := jsonschema.UnmarshalJSON(file)
		_ = file.Close()
		if err != nil {
			t.Fatal(err)
		}
		if err := compiler.AddResource("https://antaeus.io/contracts/v0alpha1/"+name, document); err != nil {
			t.Fatal(err)
		}
	}
	schema, err := compiler.Compile("https://antaeus.io/contracts/v0alpha1/evaluator-profile.schema.json")
	if err != nil {
		t.Fatal(err)
	}
	return schema
}

func TestParserAndSchemaAgreeOnPublishedFixtures(t *testing.T) {
	schema := profileSchema(t)
	for _, name := range []string{"quickstart-fixture.json", "semantic-routing.json"} {
		source := readContract(t, "examples", "v0alpha1", "evaluator-profile", name)
		if err := schema.Validate(decodeSchemaInstance(t, source)); err != nil {
			t.Fatalf("schema rejected valid %s: %v", name, err)
		}
		if _, err := Parse(source, FormatJSON); err != nil {
			t.Fatalf("parser rejected valid %s: %v", name, err)
		}
	}
	// The two original numeric fixtures also violate closed fixture parameters.
	// They remain byte-for-byte unchanged; TestNumericConformance isolates the
	// numeric rule using open semantic parameters and schema-valid controls.
	for _, name := range []string{
		"invalid-confidence.json",
		"invalid-fixture-adapter.json",
		"invalid-fixture-credential.json",
		"invalid-mixed-modes.json",
		"invalid-terminal-failure.json",
		"invalid-terminal-outcome.json",
		"invalid-unknown-property.json",
	} {
		source := readContract(t, "conformance", "v0alpha1", "evaluator-profile", name)
		if err := schema.Validate(decodeSchemaInstance(t, source)); err == nil {
			t.Fatalf("schema accepted invalid %s", name)
		}
		if _, err := Parse(source, FormatJSON); err == nil {
			t.Fatalf("parser accepted invalid %s", name)
		}
	}
}

func TestParseSourceSafety(t *testing.T) {
	valid := readContract(t, "examples", "v0alpha1", "evaluator-profile", "quickstart-fixture.json")
	var nullDocument map[string]any
	if err := json.Unmarshal(valid, &nullDocument); err != nil {
		t.Fatal(err)
	}
	nullDocument["metadata"].(map[string]any)["description"] = nil
	nullSource, _ := json.Marshal(nullDocument)
	caseVariant := bytes.Replace(valid, []byte(`"timeoutMs"`), []byte(`"TimeoutMs"`), 1)

	for _, test := range []struct {
		name   string
		source []byte
		code   string
	}{
		{name: "null", source: nullSource, code: "source.schema"},
		{name: "case variant", source: caseVariant, code: "source.schema"},
		{name: "duplicate JSON key", source: []byte(`{"apiVersion":"evaluator.antaeus.io/v0alpha1","apiVersion":"evaluator.antaeus.io/v0alpha1"}`), code: "source.duplicate_key"},
	} {
		t.Run(test.name, func(t *testing.T) {
			_, err := Parse(test.source, FormatJSON)
			assertParseCode(t, err, test.code)
		})
	}

	var deepDocument map[string]any
	if err := json.Unmarshal(valid, &deepDocument); err != nil {
		t.Fatal(err)
	}
	parameters := deepDocument["spec"].(map[string]any)["evaluators"].([]any)[0].(map[string]any)["parameters"].(map[string]any)
	var nested any = "leaf"
	for i := 0; i < MaxNestingDepth; i++ {
		nested = []any{nested}
	}
	parameters["nested"] = nested
	deepSource, _ := json.Marshal(deepDocument)
	_, err := Parse(deepSource, FormatJSON)
	assertParseCode(t, err, "source.depth")

	parameters["nested"] = make([]any, MaxParsedNodes)
	nodesSource, _ := json.Marshal(deepDocument)
	_, err = Parse(nodesSource, FormatJSON)
	assertParseCode(t, err, "source.nodes")
}

func TestLoadFileAndLimits(t *testing.T) {
	directory := t.TempDir()
	yamlPath := filepath.Join(directory, "profile.yaml")
	if err := os.WriteFile(yamlPath, []byte(quickstartYAML), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadFile(yamlPath); err != nil {
		t.Fatalf("LoadFile(YAML) error = %v", err)
	}
	unsupportedPath := filepath.Join(directory, "profile.toml")
	if err := os.WriteFile(unsupportedPath, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	_, err := LoadFile(unsupportedPath)
	assertParseCode(t, err, "source.format")
	largePath := filepath.Join(directory, "profile.json")
	if err := os.WriteFile(largePath, bytes.Repeat([]byte(" "), MaxSourceBytes+1), 0o600); err != nil {
		t.Fatal(err)
	}
	_, err = LoadFile(largePath)
	assertParseCode(t, err, "source.too_large")
}

func TestParseRejectsDuplicateYAMLKey(t *testing.T) {
	_, err := Parse([]byte("apiVersion: evaluator.antaeus.io/v0alpha1\nkind: EvaluatorProfile\nkind: EvaluatorProfile\n"), FormatYAML)
	assertParseCode(t, err, "source.duplicate_key")
}

func TestParseRejectsMissingRequiredNestedField(t *testing.T) {
	var document map[string]any
	if err := json.Unmarshal(readContract(t, "examples", "v0alpha1", "evaluator-profile", "quickstart-fixture.json"), &document); err != nil {
		t.Fatal(err)
	}
	routing := document["spec"].(map[string]any)["routing"].(map[string]any)
	delete(routing, "confidence")
	source, err := json.Marshal(document)
	if err != nil {
		t.Fatal(err)
	}
	_, err = Parse(source, FormatJSON)
	assertParseCode(t, err, "source.schema")
}

type parameterValidatorFunc func(json.RawMessage) error

func (f parameterValidatorFunc) ValidateParameters(parameters json.RawMessage) error {
	return f(parameters)
}

type nilValidatorRegistry struct{}

func (nilValidatorRegistry) ValidatorFor(ComponentIdentity) (ParameterValidator, bool) {
	return nil, true
}

func mustParseExample(t *testing.T, name string) Artifact {
	t.Helper()
	artifact, err := Parse(readContract(t, "examples", "v0alpha1", "evaluator-profile", name), FormatJSON)
	if err != nil {
		t.Fatal(err)
	}
	return artifact
}

func cloneArtifact(t *testing.T, artifact Artifact) Artifact {
	t.Helper()
	data, err := json.Marshal(artifact)
	if err != nil {
		t.Fatal(err)
	}
	var clone Artifact
	if err := json.Unmarshal(data, &clone); err != nil {
		t.Fatal(err)
	}
	return clone
}

func assertParseCode(t *testing.T, err error, code string) {
	t.Helper()
	var parseErr *ParseError
	if !errors.As(err, &parseErr) || parseErr.Code != code {
		t.Fatalf("error = %v, want ParseError code %q", err, code)
	}
}

func contractErrorCode(err error) string {
	var parseErr *ParseError
	if errors.As(err, &parseErr) {
		return parseErr.Code
	}
	var validationErr *ValidationError
	if errors.As(err, &validationErr) {
		return validationErr.Code
	}
	return ""
}

func decodeSchemaInstance(t *testing.T, source []byte) any {
	t.Helper()
	value, err := jsonschema.UnmarshalJSON(bytes.NewReader(source))
	if err != nil {
		t.Fatal(err)
	}
	return value
}

func readContract(t *testing.T, elements ...string) []byte {
	t.Helper()
	data, err := os.ReadFile(contractsPath(elements...))
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func contractsPath(elements ...string) string {
	return filepath.Join(append([]string{"..", "..", "contracts"}, elements...)...)
}

const quickstartYAML = `apiVersion: evaluator.antaeus.io/v0alpha1
kind: EvaluatorProfile
metadata:
  name: quickstart-fixture
  description: Deterministic evaluator for the credential-free quickstart.
spec:
  totalTimeoutMs: 2000
  credentialSlots: []
  evaluators:
    - id: fixture-primary
      mode: deterministic-fixture
      adapter:
        id: io.antaeus.fixture
        version: 0.1.0
      protocol:
        id: io.antaeus.rule-match
        version: v0alpha1
      requiredCapabilities:
        - json-input
        - structured-rule-results
      timeoutMs: 1000
      retry:
        maxAttempts: 1
        retryOn: []
      parameters:
        fixtureSet: quickstart
        fixtureVersion: v1
  routing:
    primary: fixture-primary
    fallbacks: []
    confidence:
      enabled: false
    terminal:
      onIndeterminate: failure
      onFailure: failure
`
