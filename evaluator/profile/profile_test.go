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
}

func TestParseRejectsConformanceProfiles(t *testing.T) {
	entries, err := os.ReadDir(contractsPath("conformance", "v0alpha1", "evaluator-profile"))
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasPrefix(entry.Name(), "invalid-") {
			continue
		}
		t.Run(entry.Name(), func(t *testing.T) {
			_, err := Parse(readContract(t, "conformance", "v0alpha1", "evaluator-profile", entry.Name()), FormatJSON)
			if err == nil {
				t.Fatal("Parse() error = nil, want rejection")
			}
		})
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
	validator := parameterValidatorFunc(func(adapter ComponentIdentity, parameters json.RawMessage) error {
		calls++
		if adapter.ID != "io.example.semantic" || adapter.Version != "1.0.0" {
			return fmt.Errorf("unexpected adapter %s@%s", adapter.ID, adapter.Version)
		}
		if string(parameters) != "{}" {
			return fmt.Errorf("parameters = %s", parameters)
		}
		return nil
	})
	if err := semantic.ValidateParameters(validator); err != nil {
		t.Fatalf("ValidateParameters() error = %v", err)
	}
	if calls != len(semantic.Spec.Evaluators) {
		t.Fatalf("validator calls = %d, want %d", calls, len(semantic.Spec.Evaluators))
	}
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

type parameterValidatorFunc func(ComponentIdentity, json.RawMessage) error

func (f parameterValidatorFunc) ValidateParameters(adapter ComponentIdentity, parameters json.RawMessage) error {
	return f(adapter, parameters)
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
