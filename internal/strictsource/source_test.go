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
		{name: "JSON", parse: func(source []byte) error { return validateJSONDocument(source, "test") }, suffix: ""},
		{name: "YAML", parse: func(source []byte) error { _, err := decodeYAMLDocument(source, "test"); return err }, suffix: "\n"},
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

func TestDecodeParameters(t *testing.T) {
	t.Run("size limit", func(t *testing.T) {
		if _, err := Decode([]byte("0"), FormatJSON, 1, "example"); err != nil {
			t.Fatalf("Decode(exact limit) error = %v", err)
		}
		_, err := Decode([]byte("00"), FormatJSON, 1, "example")
		assertErrorCode(t, err, "source.too_large")
	})

	t.Run("subject", func(t *testing.T) {
		_, err := Decode(nil, FormatYAML, 1, "evaluator profile")
		var sourceErr *Error
		if !errors.As(err, &sourceErr) {
			t.Fatalf("error = %v, want Error", err)
		}
		if sourceErr.Message != "evaluator profile source must contain one document" {
			t.Fatalf("message = %q", sourceErr.Message)
		}
	})

	t.Run("unsupported format", func(t *testing.T) {
		_, err := Decode([]byte("0"), Format("toml"), 1, "example")
		assertErrorCode(t, err, "source.format")
	})

	t.Run("bounded error", func(t *testing.T) {
		err := NewError("source.schema", strings.Repeat("x", 600), 0, 0)
		if len(err.Message) > 512 {
			t.Fatalf("message length = %d, want at most 512", len(err.Message))
		}
	})
}

func TestDecodeNormalizesPortableNumbers(t *testing.T) {
	for _, test := range []struct {
		name   string
		source string
		format Format
		want   string
	}{
		{name: "JSON integral decimal", source: `{"value":1000.0}`, format: FormatJSON, want: `{"value":1000}`},
		{name: "JSON integral exponent", source: `{"value":1e3}`, format: FormatJSON, want: `{"value":1000}`},
		{name: "YAML integral decimal", source: "value: 1000.0\n", format: FormatYAML, want: `{"value":1000}`},
		{name: "decimal", source: `{"value":0.1}`, format: FormatJSON, want: `{"value":0.1}`},
	} {
		t.Run(test.name, func(t *testing.T) {
			got, err := Decode([]byte(test.source), test.format, 1024, "test")
			if err != nil {
				t.Fatalf("Decode() error = %v", err)
			}
			if string(got) != test.want {
				t.Fatalf("Decode() = %s, want %s", got, test.want)
			}
		})
	}
}

func TestDecodeRejectsNonPortableNumbers(t *testing.T) {
	for _, source := range []string{
		`{"value":1e400}`,
		`{"value":9007199254740993}`,
		`{"value":9007199254740993.5}`,
		"value: 9007199254740993\n",
		"value: 9007199254740993.5\n",
	} {
		format := FormatJSON
		if strings.HasPrefix(source, "value:") {
			format = FormatYAML
		}
		_, err := Decode([]byte(source), format, 1024, "test")
		assertErrorCode(t, err, "source.number")
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
