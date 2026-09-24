package profile

import (
	"bytes"
	"encoding/json"
	"math"
	"path/filepath"
	"strings"
	"testing"
)

func TestNumericConformance(t *testing.T) {
	read := func(name string) []byte {
		return readContract(t, "conformance", "v0alpha1", "evaluator-profile", name)
	}
	var cases struct {
		Valid             []string
		Canonical, Digest string
		Invalid           []struct{ File, Number, Rejection string }
	}
	if err := json.Unmarshal(read("numeric-cases.json"), &cases); err != nil {
		t.Fatal(err)
	}
	if len(cases.Valid) != 3 || len(cases.Invalid) != 2 {
		t.Fatal("numeric conformance case inventory changed")
	}
	wantCanonical := bytes.TrimSuffix(read(cases.Canonical), []byte{'\n'})
	wantDigest := strings.TrimSpace(string(read(cases.Digest)))
	schema := profileSchema(t)
	for _, name := range cases.Valid {
		t.Run(name, func(t *testing.T) {
			format := FormatJSON
			if filepath.Ext(name) == ".yaml" {
				format = FormatYAML
			}
			a, err := Parse(read(name), format)
			if err != nil {
				t.Fatal(err)
			}
			canonical, err := a.CanonicalJSON()
			if err != nil {
				t.Fatal(err)
			}
			if !bytes.Equal(canonical, wantCanonical) {
				t.Fatalf("canonical mismatch: %s", canonical)
			}
			digest, err := a.Digest()
			if err != nil || digest != wantDigest {
				t.Fatalf("digest=%s error=%v", digest, err)
			}
			if err := schema.Validate(decodeSchemaInstance(t, canonical)); err != nil {
				t.Fatal(err)
			}
		})
	}
	for _, c := range cases.Invalid {
		t.Run(c.File, func(t *testing.T) {
			if c.Rejection != "numeric-portability" {
				t.Fatal("unexpected rejection category")
			}
			source := read(c.File)
			// An arbitrary-precision JSON Schema decoder must accept the
			// structure. Numeric portability, not a closed parameter object,
			// is the sole rejection reason imposed by the source contract.
			if err := schema.Validate(decodeSchemaInstance(t, source)); err != nil {
				t.Fatal(err)
			}
			_, err := Parse(source, FormatJSON)
			assertParseCode(t, err, "source.number")
			if bytes.Count(source, []byte(c.Number)) != 1 {
				t.Fatal("ambiguous numeric mutation")
			}
			control := bytes.Replace(source, []byte(c.Number), []byte("1000"), 1)
			a, err := Parse(control, FormatJSON)
			if err != nil {
				t.Fatalf("replacing only the number must pass: %v", err)
			}
			digest, err := a.Digest()
			if err != nil || digest != wantDigest {
				t.Fatalf("control digest=%s error=%v", digest, err)
			}
		})
	}
}

func TestProgrammaticParameterNumbers(t *testing.T) {
	for _, tc := range []struct {
		name  string
		value any
		code  string
	}{
		{"int", int(1000), ""}, {"int8", int8(-128), ""}, {"int16", int16(32767), ""}, {"int32", int32(-2147483648), ""},
		{"int64 safe", int64(9007199254740991), ""}, {"int64 unsafe", int64(9007199254740992), "parameters.invalid"},
		{"int64 negative unsafe", int64(-9007199254740992), "parameters.invalid"},
		{"uint", uint(1000), ""}, {"uint8", uint8(255), ""}, {"uint16", uint16(65535), ""}, {"uint32", uint32(4294967295), ""},
		{"uint64 safe", uint64(9007199254740991), ""}, {"uint64 unsafe", uint64(9007199254740992), "parameters.invalid"},
		{"float32", float32(0.5), ""}, {"float32 infinity", float32(math.Inf(1)), "parameters.invalid"},
		{"float64", float64(0.5), ""}, {"float64 nan", math.NaN(), "parameters.invalid"}, {"float64 infinity", math.Inf(-1), "parameters.invalid"},
		{"number", json.Number("1e3"), ""}, {"number unsafe", json.Number("9007199254740993"), "parameters.invalid"},
		{"number malformed", json.Number("0x1p4"), "source.schema"}, {"unsupported", []string{"value"}, "parameters.invalid"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			a := mustParseExample(t, "semantic-routing.json")
			a.Spec.Evaluators[0].Parameters = map[string]any{"value": tc.value}
			err := a.Validate()
			if tc.code != "" {
				if err == nil || contractErrorCode(err) != tc.code {
					t.Fatalf("error=%v want %s", err, tc.code)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			canonical, err := a.CanonicalJSON()
			if err != nil {
				t.Fatal(err)
			}
			if _, err := Parse(canonical, FormatJSON); err != nil {
				t.Fatalf("valid canonical roundtrip: %v", err)
			}
		})
	}
}

func TestYAMLNumericBasePortability(t *testing.T) {
	source := readContract(t, "conformance", "v0alpha1", "evaluator-profile", "valid-number-decimal.yaml")
	for _, tc := range []struct{ number, want string }{
		{"0x3e8", "1000"}, {"0o1750", "1000"}, {"010", "10"}, {".5", "0.5"}, {"1.", "1"},
		{"0x20000000000001", ""}, {"0o400000000000000001", ""},
	} {
		t.Run(tc.number, func(t *testing.T) {
			a, err := Parse(bytes.Replace(source, []byte("value: 1000.0"), []byte("value: "+tc.number), 1), FormatYAML)
			if tc.want == "" {
				assertParseCode(t, err, "source.number")
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			canonical, err := canonicalParameters(a.Spec.Evaluators[0].Parameters)
			if err != nil || string(canonical) != `{"value":`+tc.want+`}` {
				t.Fatalf("canonical=%s err=%v", canonical, err)
			}
		})
	}
}
