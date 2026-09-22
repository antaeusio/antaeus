package policy

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestParseJSONAndYAMLProduceSameArtifactIdentity(t *testing.T) {
	jsonSource := readFixture(t, "examples", "v0alpha1", "policy", "vendor-onboarding.json")
	yamlSource := readFixture(t, "examples", "v0alpha1", "policy", "vendor-onboarding.yaml")

	jsonArtifact, err := Parse(jsonSource, FormatJSON)
	if err != nil {
		t.Fatalf("Parse(JSON) error = %v", err)
	}
	yamlArtifact, err := Parse(yamlSource, FormatYAML)
	if err != nil {
		t.Fatalf("Parse(YAML) error = %v", err)
	}
	jsonCanonical, err := jsonArtifact.CanonicalJSON()
	if err != nil {
		t.Fatalf("JSON CanonicalJSON() error = %v", err)
	}
	yamlCanonical, err := yamlArtifact.CanonicalJSON()
	if err != nil {
		t.Fatalf("YAML CanonicalJSON() error = %v", err)
	}
	if !bytes.Equal(jsonCanonical, yamlCanonical) {
		t.Fatalf("canonical JSON differs:\nJSON: %s\nYAML: %s", jsonCanonical, yamlCanonical)
	}
	jsonDigest, _ := jsonArtifact.Digest()
	yamlDigest, _ := yamlArtifact.Digest()
	if jsonDigest != yamlDigest {
		t.Fatalf("digests differ: JSON %q, YAML %q", jsonDigest, yamlDigest)
	}
}

func TestParseJSONRejectsUnsafeOrAmbiguousInput(t *testing.T) {
	valid := string(readFixture(t, "examples", "v0alpha1", "policy", "vendor-onboarding.json"))
	tests := []struct {
		name   string
		source []byte
		code   string
	}{
		{
			name:   "duplicate key",
			source: []byte(strings.Replace(valid, `"kind": "Policy"`, `"kind": "Policy", "kind": "Policy"`, 1)),
			code:   "source.duplicate_key",
		},
		{
			name:   "unknown property",
			source: []byte(strings.Replace(valid, `"kind": "Policy"`, `"kind": "Policy", "extra": true`, 1)),
			code:   "source.schema",
		},
		{
			name:   "null optional description",
			source: []byte(strings.Replace(valid, `"description": "Route vendor submissions according to review policy."`, `"description": null`, 1)),
			code:   "source.schema",
		},
		{
			name:   "case variant property",
			source: []byte(strings.Replace(valid, `"kind": "Policy"`, `"Kind": "Policy"`, 1)),
			code:   "source.schema",
		},
		{
			name:   "case variant duplicate property",
			source: []byte(strings.Replace(valid, `"description": "Route vendor submissions according to review policy."`, `"description": "first", "Description": "second"`, 1)),
			code:   "source.schema",
		},
		{
			name:   "lone high surrogate",
			source: []byte(strings.Replace(valid, `"description": "Route vendor submissions according to review policy."`, `"description": "\ud800"`, 1)),
			code:   "source.invalid_unicode",
		},
		{
			name:   "lone low surrogate",
			source: []byte(strings.Replace(valid, `"description": "Route vendor submissions according to review policy."`, `"description": "\udc00"`, 1)),
			code:   "source.invalid_unicode",
		},
		{
			name:   "trailing document",
			source: []byte(valid + `{}`),
			code:   "source.multiple_documents",
		},
		{
			name:   "invalid UTF-8",
			source: []byte{0xff},
			code:   "source.invalid_utf8",
		},
		{
			name:   "too deep",
			source: []byte(strings.Repeat("[", MaxNestingDepth+1) + "null" + strings.Repeat("]", MaxNestingDepth+1)),
			code:   "source.depth",
		},
		{
			name:   "too many nodes",
			source: []byte("[" + strings.Repeat("null,", MaxParsedNodes) + "null]"),
			code:   "source.nodes",
		},
		{
			name:   "too large",
			source: bytes.Repeat([]byte{' '}, MaxSourceBytes+1),
			code:   "source.too_large",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_, err := Parse(test.source, FormatJSON)
			assertParseErrorCode(t, err, test.code)
		})
	}
}

func TestParseYAMLRejectsUnsupportedFeatures(t *testing.T) {
	tests := []struct {
		name   string
		source string
		code   string
	}{
		{name: "directive", source: "%YAML 1.2\n---\n" + validYAML(), code: "source.directive"},
		{name: "multiple documents", source: validYAML() + "\n---\n" + validYAML(), code: "source.multiple_documents"},
		{name: "duplicate key", source: strings.Replace(validYAML(), "kind: Policy", "kind: Policy\nkind: Policy", 1), code: "source.duplicate_key"},
		{name: "non-string key", source: strings.Replace(validYAML(), "kind: Policy", "kind: Policy\ntrue: rejected", 1), code: "source.non_string_key"},
		{name: "anchor", source: strings.Replace(validYAML(), "name: example", "name: &name example", 1), code: "source.alias"},
		{name: "alias", source: strings.Replace(validYAML(), "name: example", "name: &name example\n  description: *name", 1), code: "source.alias"},
		{name: "merge key", source: strings.Replace(validYAML(), "name: example", "<<: {name: example}", 1), code: "source.merge"},
		{name: "custom tag", source: strings.Replace(validYAML(), "name: example", "name: !custom example", 1), code: "source.tag"},
		{name: "explicit timestamp tag", source: strings.Replace(validYAML(), "name: example", "name: example\n  description: !!timestamp 2026-09-22", 1), code: "source.tag"},
		{name: "non-finite number", source: strings.Replace(validYAML(), "defaultOutcome: review", "defaultOutcome: .inf", 1), code: "source.number"},
		{name: "boolean in string field", source: strings.Replace(validYAML(), "defaultOutcome: review", "defaultOutcome: true", 1), code: "source.schema"},
		{name: "null optional description", source: strings.Replace(validYAML(), "name: example", "name: example\n  description:", 1), code: "source.schema"},
		{name: "core decimal with leading zero", source: strings.Replace(validYAML(), "defaultOutcome: review", "defaultOutcome: 0777", 1), code: "source.schema"},
		{name: "case variant property", source: strings.Replace(validYAML(), "kind: Policy", "Kind: Policy", 1), code: "source.schema"},
		{name: "case variant duplicate property", source: strings.Replace(validYAML(), "name: example", "name: example\n  Name: second", 1), code: "source.schema"},
		{name: "invalid explicit boolean", source: strings.Replace(validYAML(), "defaultOutcome: review", "defaultOutcome: !!bool yes", 1), code: "source.scalar"},
		{name: "invalid explicit null", source: strings.Replace(validYAML(), "defaultOutcome: review", "defaultOutcome: !!null review", 1), code: "source.scalar"},
		{name: "invalid explicit integer", source: strings.Replace(validYAML(), "defaultOutcome: review", "defaultOutcome: !!int 1.5", 1), code: "source.scalar"},
		{name: "bare carriage return", source: strings.ReplaceAll(validYAML(), "\n", "\r"), code: "source.line_break"},
		{name: "YAML 1.1 next-line character", source: strings.Replace(validYAML(), "The condition applies.", "The\u0085condition applies.", 1), code: "source.line_break"},
		{name: "YAML 1.1 line separator", source: strings.Replace(validYAML(), "The condition applies.", "The\u2028condition applies.", 1), code: "source.line_break"},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_, err := Parse([]byte(test.source), FormatYAML)
			assertParseErrorCode(t, err, test.code)
		})
	}
}

func TestParseYAMLUsesCoreSchemaStrings(t *testing.T) {
	tests := []string{"yes", "on", "2026-09-22", "tRUE", "nULL", "077legacy"}
	for _, value := range tests {
		t.Run(value, func(t *testing.T) {
			source := strings.Replace(validYAML(), "name: example", "name: example\n  description: "+value, 1)
			artifact, err := Parse([]byte(source), FormatYAML)
			if err != nil {
				t.Fatalf("Parse() error = %v", err)
			}
			if artifact.Metadata.Description == nil || *artifact.Metadata.Description != value {
				t.Fatalf("description = %v, want %q", artifact.Metadata.Description, value)
			}
		})
	}
}

func TestParseYAMLNonSpecificTagForcesString(t *testing.T) {
	source := strings.Replace(validYAML(), "name: example", "name: example\n  description: ! 123", 1)
	artifact, err := Parse([]byte(source), FormatYAML)
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}
	if artifact.Metadata.Description == nil || *artifact.Metadata.Description != "123" {
		t.Fatalf("description = %v, want 123", artifact.Metadata.Description)
	}
}

func TestParseYAMLNonSpecificTagUsesCharacterColumns(t *testing.T) {
	source := "\ufeffapiVersion: policy.antaeus.io/v0alpha1\n" +
		"kind: Policy\n" +
		"metadata: {name: example}\n" +
		"spec: {defaultOutcome: review, rules: [{id: check, description: café, when: ! 123, outcome: allow}]}\n"
	artifact, err := Parse([]byte(source), FormatYAML)
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}
	if artifact.Spec.Rules[0].When != "123" {
		t.Fatalf("when = %q, want 123", artifact.Spec.Rules[0].When)
	}
}

func TestStructuralDepthAndNodeLimitBoundaries(t *testing.T) {
	for _, test := range []struct {
		name   string
		parse  func([]byte) error
		suffix string
	}{
		{name: "JSON", parse: validateJSONDocument, suffix: ""},
		{name: "YAML", parse: func(source []byte) error { _, err := decodeYAMLDocument(source); return err }, suffix: "\n"},
	} {
		t.Run(test.name+" depth", func(t *testing.T) {
			exact := []byte(strings.Repeat("[", MaxNestingDepth) + "null" + strings.Repeat("]", MaxNestingDepth) + test.suffix)
			if err := test.parse(exact); err != nil {
				t.Fatalf("exact depth error = %v", err)
			}
			over := []byte(strings.Repeat("[", MaxNestingDepth+1) + "null" + strings.Repeat("]", MaxNestingDepth+1) + test.suffix)
			assertParseErrorCode(t, test.parse(over), "source.depth")
		})
		t.Run(test.name+" nodes", func(t *testing.T) {
			exact := []byte("[" + strings.Repeat("null,", MaxParsedNodes-2) + "null]" + test.suffix)
			if err := test.parse(exact); err != nil {
				t.Fatalf("exact node count error = %v", err)
			}
			over := []byte("[" + strings.Repeat("null,", MaxParsedNodes-1) + "null]" + test.suffix)
			assertParseErrorCode(t, test.parse(over), "source.nodes")
		})
	}
}

func TestPolicySourceConformanceFixtures(t *testing.T) {
	valid := readFixture(t, "conformance", "v0alpha1", "policy-source", "valid-core-scalars.yaml")
	if _, err := Parse(valid, FormatYAML); err != nil {
		t.Fatalf("Parse(valid core scalars) error = %v", err)
	}

	tests := []struct {
		name string
		code string
	}{
		{name: "invalid-duplicate-key.yaml", code: "source.duplicate_key"},
		{name: "invalid-non-string-key.yaml", code: "source.non_string_key"},
		{name: "invalid-anchor.yaml", code: "source.alias"},
		{name: "invalid-alias.yaml", code: "source.alias"},
		{name: "invalid-merge.yaml", code: "source.merge"},
		{name: "invalid-directive.yaml", code: "source.directive"},
		{name: "invalid-custom-tag.yaml", code: "source.tag"},
		{name: "invalid-multiple-documents.yaml", code: "source.multiple_documents"},
		{name: "invalid-nonfinite.yaml", code: "source.number"},
		{name: "invalid-lone-surrogate.json", code: "source.invalid_unicode"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			source := readFixture(t, "conformance", "v0alpha1", "policy-source", test.name)
			format := FormatYAML
			if strings.HasSuffix(test.name, ".json") {
				format = FormatJSON
			}
			_, err := Parse(source, format)
			assertParseErrorCode(t, err, test.code)
		})
	}
}

func TestLoadFileSelectsFormatAndLimitsReads(t *testing.T) {
	directory := t.TempDir()
	yamlPath := filepath.Join(directory, "policy.YML")
	if err := os.WriteFile(yamlPath, []byte(validYAML()), 0o600); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}
	if _, err := LoadFile(yamlPath); err != nil {
		t.Fatalf("LoadFile(YAML) error = %v", err)
	}

	unsupportedPath := filepath.Join(directory, "policy.txt")
	if err := os.WriteFile(unsupportedPath, []byte(validYAML()), 0o600); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}
	_, err := LoadFile(unsupportedPath)
	assertParseErrorCode(t, err, "source.format")

	largePath := filepath.Join(directory, "large.json")
	if err := os.WriteFile(largePath, bytes.Repeat([]byte{' '}, MaxSourceBytes+1), 0o600); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}
	_, err = LoadFile(largePath)
	assertParseErrorCode(t, err, "source.too_large")
}

func TestParseErrorIncludesLocationWhenAvailable(t *testing.T) {
	source := strings.Replace(validYAML(), "kind: Policy", "kind: Policy\nkind: Policy", 1)
	_, err := Parse([]byte(source), FormatYAML)
	var parseErr *ParseError
	if !errors.As(err, &parseErr) {
		t.Fatalf("Parse() error = %v, want ParseError", err)
	}
	if parseErr.Line == 0 || parseErr.Column == 0 {
		t.Fatalf("ParseError location = %d:%d, want source location", parseErr.Line, parseErr.Column)
	}
}

func assertParseErrorCode(t *testing.T, err error, want string) {
	t.Helper()
	var parseErr *ParseError
	if !errors.As(err, &parseErr) {
		t.Fatalf("error = %v, want ParseError code %q", err, want)
	}
	if parseErr.Code != want {
		t.Fatalf("ParseError.Code = %q, want %q (error: %v)", parseErr.Code, want, err)
	}
}

func validYAML() string {
	return `apiVersion: policy.antaeus.io/v0alpha1
kind: Policy
metadata:
  name: example
spec:
  defaultOutcome: review
  rules:
    - id: check
      when: The condition applies.
      outcome: allow
`
}
