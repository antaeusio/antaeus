package localbinding

import (
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/antaeusio/antaeus/evaluator/profile"
)

func TestParseExampleAndYAML(t *testing.T) {
	jsonArtifact, err := Parse(readContract(t, "examples", "v0alpha1", "local-secret-bindings", "development.json"), FormatJSON)
	if err != nil {
		t.Fatalf("Parse(JSON) error = %v", err)
	}
	yamlArtifact, err := Parse([]byte(bindingsYAML), FormatYAML)
	if err != nil {
		t.Fatalf("Parse(YAML) error = %v", err)
	}
	jsonEncoded, _ := json.Marshal(jsonArtifact)
	yamlEncoded, _ := json.Marshal(yamlArtifact)
	if !bytes.Equal(jsonEncoded, yamlEncoded) {
		t.Fatalf("artifacts differ\nJSON: %s\nYAML: %s", jsonEncoded, yamlEncoded)
	}
}

func TestParseRejectsConformanceFixtures(t *testing.T) {
	directory := contractsPath("conformance", "v0alpha1", "local-secret-bindings")
	entries, err := os.ReadDir(directory)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) == 0 {
		t.Fatal("no conformance fixtures")
	}
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasPrefix(entry.Name(), "invalid-") {
			continue
		}
		t.Run(entry.Name(), func(t *testing.T) {
			if _, err := Parse(readContract(t, "conformance", "v0alpha1", "local-secret-bindings", entry.Name()), FormatJSON); err == nil {
				t.Fatal("Parse() error = nil, want rejection")
			}
		})
	}
}

func TestParseRejectsDuplicateAndCaseVariantKeys(t *testing.T) {
	for _, test := range []struct {
		name   string
		source string
		code   string
	}{
		{name: "duplicate", source: `{"apiVersion":"config.antaeus.io/v0alpha1","apiVersion":"config.antaeus.io/v0alpha1"}`, code: "source.duplicate_key"},
		{name: "case variant", source: `{"apiVersion":"config.antaeus.io/v0alpha1","kind":"LocalSecretBindings","SecretBindings":{}}`, code: "source.schema"},
	} {
		t.Run(test.name, func(t *testing.T) {
			_, err := Parse([]byte(test.source), FormatJSON)
			var parseErr *ParseError
			if !errors.As(err, &parseErr) || parseErr.Code != test.code {
				t.Fatalf("error = %v, want code %q", err, test.code)
			}
		})
	}
}

func TestPreflightCapturesOnlyConfiguredRouteOnce(t *testing.T) {
	evaluatorProfile := readProfile(t)
	bindings := readBindings(t)
	bindings.SecretBindings["io.example.unused"] = map[string]Reference{
		"unused-key": {Source: "environment", Name: "UNRELATED_SECRET"},
	}
	lookups := make(map[string]int)
	environment := EnvironmentFunc(func(name string) (string, bool) {
		lookups[name]++
		if name == "EXAMPLE_API_KEY" {
			return "  exact value  ", true
		}
		return "", false
	})
	credentials, err := Preflight(evaluatorProfile, bindings, environment)
	if err != nil {
		t.Fatalf("Preflight() error = %v", err)
	}
	defer credentials.Clear()
	if lookups["EXAMPLE_API_KEY"] != 1 {
		t.Fatalf("EXAMPLE_API_KEY lookups = %d, want 1", lookups["EXAMPLE_API_KEY"])
	}
	if lookups["UNRELATED_SECRET"] != 0 {
		t.Fatalf("unused secret lookups = %d, want 0", lookups["UNRELATED_SECRET"])
	}
	value, exists := credentials.Credential("io.example.semantic", "provider-api-key")
	if !exists || string(value) != "  exact value  " {
		t.Fatalf("credential = %q, %v", value, exists)
	}
	value[0] = 'X'
	again, _ := credentials.Credential("io.example.semantic", "provider-api-key")
	if string(again) != "  exact value  " {
		t.Fatalf("stored credential was mutated: %q", again)
	}
	credentials.Clear()
	if _, exists := credentials.Credential("io.example.semantic", "provider-api-key"); exists {
		t.Fatal("Credential() exists after Clear")
	}
}

func TestPreflightRequiresEveryConfiguredRouteCredential(t *testing.T) {
	evaluatorProfile := readProfile(t)
	fallback := &evaluatorProfile.Spec.Evaluators[2]
	fallback.Adapter.ID = "io.example.fallback"
	bindings := readBindings(t)
	environment := EnvironmentFunc(func(name string) (string, bool) { return "primary", true })
	_, err := Preflight(evaluatorProfile, bindings, environment)
	var missing *MissingCredentialError
	if !errors.As(err, &missing) {
		t.Fatalf("error = %v, want MissingCredentialError", err)
	}
	if missing.EvaluatorID != "semantic-fallback" || missing.AdapterID != "io.example.fallback" || missing.Slot != "provider-api-key" {
		t.Fatalf("missing error = %#v", missing)
	}
}

func TestPreflightRedactsUnsetAndEmptyReferences(t *testing.T) {
	evaluatorProfile := readProfile(t)
	bindings := readBindings(t)
	for _, test := range []struct {
		name   string
		lookup EnvironmentFunc
	}{
		{name: "unset", lookup: func(string) (string, bool) { return "", false }},
		{name: "empty", lookup: func(string) (string, bool) { return "", true }},
	} {
		t.Run(test.name, func(t *testing.T) {
			_, err := Preflight(evaluatorProfile, bindings, test.lookup)
			var missing *MissingCredentialError
			if !errors.As(err, &missing) {
				t.Fatalf("error = %v, want MissingCredentialError", err)
			}
			if strings.Contains(err.Error(), "EXAMPLE_API_KEY") {
				t.Fatalf("error disclosed reference name: %v", err)
			}
		})
	}
}

func TestPreflightRejectsUnboundSlotWithoutEnvironmentLookup(t *testing.T) {
	evaluatorProfile := readProfile(t)
	bindings := readBindings(t)
	delete(bindings.SecretBindings["io.example.semantic"], "provider-api-key")
	// Keep the artifact structurally valid while leaving the routed tuple unbound.
	bindings.SecretBindings["io.example.semantic"]["other-key"] = Reference{Source: "environment", Name: "OTHER_KEY"}
	called := false
	_, err := Preflight(evaluatorProfile, bindings, EnvironmentFunc(func(string) (string, bool) {
		called = true
		return "", false
	}))
	if err == nil || called {
		t.Fatalf("Preflight() error = %v, environment called = %v", err, called)
	}
}

func TestLoadFileRequiresExplicitSupportedPath(t *testing.T) {
	directory := t.TempDir()
	path := filepath.Join(directory, "bindings.yaml")
	if err := os.WriteFile(path, []byte(bindingsYAML), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadFile(path); err != nil {
		t.Fatalf("LoadFile() error = %v", err)
	}
	_, err := LoadFile(filepath.Join(directory, "bindings.env"))
	var parseErr *ParseError
	if !errors.As(err, &parseErr) || parseErr.Code != "source.format" {
		t.Fatalf("error = %v, want source.format", err)
	}
}

func readProfile(t *testing.T) profile.Artifact {
	t.Helper()
	artifact, err := profile.Parse(readContract(t, "examples", "v0alpha1", "evaluator-profile", "semantic-routing.json"), profile.FormatJSON)
	if err != nil {
		t.Fatal(err)
	}
	return artifact
}

func readBindings(t *testing.T) Artifact {
	t.Helper()
	artifact, err := Parse(readContract(t, "examples", "v0alpha1", "local-secret-bindings", "development.json"), FormatJSON)
	if err != nil {
		t.Fatal(err)
	}
	return artifact
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

const bindingsYAML = `apiVersion: config.antaeus.io/v0alpha1
kind: LocalSecretBindings
secretBindings:
  io.example.semantic:
    provider-api-key:
      source: environment
      name: EXAMPLE_API_KEY
`
