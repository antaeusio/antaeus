package cli

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/antaeusio/antaeus/evaluator"
	"github.com/antaeusio/antaeus/policy"
)

func TestLoadCanonicalInput(t *testing.T) {
	path := writeInput(t, "input.json", "\n  { \"z\": 4.50, \"answer\": 42, \"nested\": {\"b\": 2, \"a\": 1} }\n")
	got, err := loadCanonicalInput(path)
	if err != nil {
		t.Fatalf("loadCanonicalInput() error = %v", err)
	}
	if string(got) != `{"answer":42,"nested":{"a":1,"b":2},"z":4.5}` {
		t.Fatalf("input = %s, want RFC 8785 canonical object", got)
	}
}

func TestLoadCanonicalInputHandlesEscapedQuoteBeforeUnicodeEscape(t *testing.T) {
	path := writeInput(t, "input.json", `{"text":"quoted: \"; emoji: \ud83d\ude00"}`)
	got, err := loadCanonicalInput(path)
	if err != nil {
		t.Fatalf("loadCanonicalInput() error = %v", err)
	}
	if string(got) != `{"text":"quoted: \"; emoji: 😀"}` {
		t.Fatalf("input = %s, want canonical Unicode string", got)
	}
}

func TestLoadCanonicalInputRejectsInvalidSources(t *testing.T) {
	tests := []struct {
		name   string
		source []byte
		want   string
	}{
		{name: "array", source: []byte(`[]`), want: "JSON object"},
		{name: "string root", source: []byte(`"x"`), want: "JSON object"},
		{name: "number root", source: []byte(`1`), want: "JSON object"},
		{name: "null root", source: []byte(`null`), want: "JSON object"},
		{name: "duplicate key", source: []byte(`{"a":1,"a":2}`), want: "duplicate object key"},
		{name: "trailing second value", source: []byte(`{} {}`), want: "exactly one JSON object"},
		{name: "trailing garbage", source: []byte(`{} garbage`), want: "trailing data"},
		{name: "trailing non-JSON whitespace", source: []byte("{}\u00a0"), want: "trailing data"},
		{name: "leading form feed", source: []byte("\f{}"), want: "decode JSON"},
		{name: "invalid UTF-8", source: []byte{0xff}, want: "valid UTF-8"},
		{name: "lone surrogate", source: []byte(`{"a":"\ud800"}`), want: "unpaired high surrogate"},
		{name: "lone low surrogate", source: []byte(`{"a":"\udc00"}`), want: "unpaired low surrogate"},
		{name: "high surrogate with non-low", source: []byte(`{"a":"\ud800\u0041"}`), want: "unpaired high surrogate"},
		{name: "number outside float64", source: []byte(`{"a":1e400}`), want: "canonicalize JSON"},
		{name: "too deep", source: []byte(strings.Repeat("[", policy.MaxNestingDepth+1) + "null" + strings.Repeat("]", policy.MaxNestingDepth+1)), want: "nesting"},
		{name: "too many nodes", source: []byte("[" + strings.Repeat("null,", policy.MaxParsedNodes-1) + "null]"), want: "parsed nodes"},
		{name: "too large", source: bytes.Repeat([]byte{' '}, evaluator.MaxInputBytes+1), want: "must not exceed"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			path := writeInput(t, "input.json", string(test.source))
			_, err := loadCanonicalInput(path)
			if err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("loadCanonicalInput() error = %v, want substring %q", err, test.want)
			}
		})
	}
}

func TestLoadCanonicalInputAcceptsExactStructuralLimits(t *testing.T) {
	depthSource := `{"a":` + strings.Repeat("[", policy.MaxNestingDepth-1) + "null" + strings.Repeat("]", policy.MaxNestingDepth-1) + `}`
	if _, err := loadCanonicalInput(writeInput(t, "depth.json", depthSource)); err != nil {
		t.Fatalf("exact depth error = %v", err)
	}

	const fixedNodes = 3 // root object, key, and value array
	nodeSource := `{"a":[` + strings.Repeat("null,", policy.MaxParsedNodes-fixedNodes-1) + `null]}`
	if _, err := loadCanonicalInput(writeInput(t, "nodes.json", nodeSource)); err != nil {
		t.Fatalf("exact node count error = %v", err)
	}
}

func writeInput(t *testing.T, name, source string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), name)
	if err := os.WriteFile(path, []byte(source), 0o600); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}
	return path
}
