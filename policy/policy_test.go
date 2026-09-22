package policy

import (
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestExampleCanonicalization(t *testing.T) {
	artifact := loadExample(t)
	if err := artifact.Validate(); err != nil {
		t.Fatalf("Validate() error = %v", err)
	}

	wantCanonical := readFixture(t, "conformance", "v0alpha1", "canonical", "vendor-onboarding.canonical.json")
	gotCanonical, err := artifact.CanonicalJSON()
	if err != nil {
		t.Fatalf("CanonicalJSON() error = %v", err)
	}
	if !bytes.Equal(gotCanonical, bytes.TrimSuffix(wantCanonical, []byte{'\n'})) {
		t.Fatalf("CanonicalJSON() = %s, want %s", gotCanonical, wantCanonical)
	}

	wantDigest := strings.TrimSpace(string(readFixture(t, "conformance", "v0alpha1", "canonical", "vendor-onboarding.sha256")))
	gotDigest, err := artifact.Digest()
	if err != nil {
		t.Fatalf("Digest() error = %v", err)
	}
	if gotDigest != wantDigest {
		t.Fatalf("Digest() = %q, want %q", gotDigest, wantDigest)
	}
}

func TestValidateRejectsSemanticViolations(t *testing.T) {
	description := "description"
	tests := []struct {
		name   string
		mutate func(*Artifact)
		code   string
	}{
		{
			name: "unsupported version",
			mutate: func(a *Artifact) {
				a.APIVersion = "policy.antaeus.io/v2"
			},
			code: "version.unsupported",
		},
		{
			name: "duplicate rule id",
			mutate: func(a *Artifact) {
				a.Spec.Rules[1].ID = a.Spec.Rules[0].ID
			},
			code: "rule_id.duplicate",
		},
		{
			name: "blank condition",
			mutate: func(a *Artifact) {
				a.Spec.Rules[0].When = " \n\t"
			},
			code: "string.empty",
		},
		{
			name: "invalid outcome",
			mutate: func(a *Artifact) {
				a.Spec.Rules[0].Outcome = "permit"
			},
			code: "outcome.invalid",
		},
		{
			name: "oversized description",
			mutate: func(a *Artifact) {
				value := strings.Repeat(description, MaxDescriptionLength)
				a.Metadata.Description = &value
			},
			code: "string.limit",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			artifact := loadExample(t)
			test.mutate(&artifact)
			var validationError *ValidationError
			if err := artifact.Validate(); !errors.As(err, &validationError) {
				t.Fatalf("Validate() error = %v, want ValidationError", err)
			}
			if validationError.Code != test.code {
				t.Fatalf("ValidationError.Code = %q, want %q", validationError.Code, test.code)
			}
		})
	}
}

func TestCanonicalJSONPreservesJCSStringData(t *testing.T) {
	description := "<>&\u2028\u2029\u0001"
	artifact := Artifact{
		APIVersion: APIVersion,
		Kind:       Kind,
		Metadata: Metadata{
			Name:        "unicode-policy",
			Description: &description,
		},
		Spec: Spec{
			DefaultOutcome: OutcomeReview,
			Rules: []Rule{{
				ID:      "check",
				When:    "café",
				Outcome: OutcomeAllow,
			}},
		},
	}

	got, err := artifact.CanonicalJSON()
	if err != nil {
		t.Fatalf("CanonicalJSON() error = %v", err)
	}
	if bytes.Contains(got, []byte(`\u003c`)) || bytes.Contains(got, []byte(`\u2028`)) {
		t.Fatalf("CanonicalJSON() escaped preserved Unicode or HTML characters: %s", got)
	}
	if !bytes.Contains(got, []byte(`\u0001`)) {
		t.Fatalf("CanonicalJSON() did not escape control character: %s", got)
	}
}

func TestSemanticFixtureRejectsDuplicateRuleID(t *testing.T) {
	data := readFixture(t, "conformance", "v0alpha1", "policy", "invalid-duplicate-rule-id.json")
	var artifact Artifact
	if err := json.Unmarshal(data, &artifact); err != nil {
		t.Fatalf("decode fixture: %v", err)
	}
	var validationError *ValidationError
	if err := artifact.Validate(); !errors.As(err, &validationError) {
		t.Fatalf("Validate() error = %v, want ValidationError", err)
	}
	if validationError.Code != "rule_id.duplicate" {
		t.Fatalf("ValidationError.Code = %q, want rule_id.duplicate", validationError.Code)
	}
}

func loadExample(t *testing.T) Artifact {
	t.Helper()
	data := readFixture(t, "examples", "v0alpha1", "policy", "vendor-onboarding.json")
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	var artifact Artifact
	if err := decoder.Decode(&artifact); err != nil {
		t.Fatalf("decode example: %v", err)
	}
	return artifact
}

func readFixture(t *testing.T, elements ...string) []byte {
	t.Helper()
	path := filepath.Join(append([]string{"..", "contracts"}, elements...)...)
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	return data
}
