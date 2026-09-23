package strictsource

import (
	"errors"
	"strings"
	"testing"

	"go.yaml.in/yaml/v3"
)

func TestHasNonSpecificTagRecognizesFlowTerminators(t *testing.T) {
	node := &yaml.Node{Line: 1, Column: 1}
	for _, terminator := range []byte{',', '[', ']', '{', '}'} {
		if !hasNonSpecificTag(node, [][]byte{{'!', terminator}}) {
			t.Errorf("terminator %q was not recognized", terminator)
		}
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
			assertErrorCode(t, test.parse(over), "source.depth")
		})
		t.Run(test.name+" nodes", func(t *testing.T) {
			exact := []byte("[" + strings.Repeat("null,", MaxParsedNodes-2) + "null]" + test.suffix)
			if err := test.parse(exact); err != nil {
				t.Fatalf("exact node count error = %v", err)
			}
			over := []byte("[" + strings.Repeat("null,", MaxParsedNodes-1) + "null]" + test.suffix)
			assertErrorCode(t, test.parse(over), "source.nodes")
		})
	}
}

func assertErrorCode(t *testing.T, err error, want string) {
	t.Helper()
	var sourceErr *Error
	if !errors.As(err, &sourceErr) {
		t.Fatalf("error = %v, want Error code %q", err, want)
	}
	if sourceErr.Code != want {
		t.Fatalf("Error.Code = %q, want %q (error: %v)", sourceErr.Code, want, err)
	}
}
