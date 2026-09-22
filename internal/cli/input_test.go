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

func TestLoadCanonicalInputRejectsInvalidSources(t *testing.T) {
	tests := []struct {
		name   string
		source []byte
		want   string
	}{
		{name: "array", source: []byte(`[]`), want: "JSON object"},
		{name: "duplicate key", source: []byte(`{"a":1,"a":2}`), want: "duplicate object key"},
		{name: "lone surrogate", source: []byte(`{"a":"\ud800"}`), want: "unpaired high surrogate"},
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

func writeInput(t *testing.T, name, source string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), name)
	if err := os.WriteFile(path, []byte(source), 0o600); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}
	return path
}
