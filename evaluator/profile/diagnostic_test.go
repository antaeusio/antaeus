package profile

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"unicode/utf8"
)

func TestBoundedMessageUTF8(t *testing.T) {
	for _, tc := range []struct{ name, input, want string }{
		{"empty", "", ""},
		{"short", "rejected café 世界 🛑", "rejected café 世界 🛑"},
		{"ascii exact", strings.Repeat("a", 512), strings.Repeat("a", 512)},
		{"ascii over", strings.Repeat("a", 513), strings.Repeat("a", 509) + "..."},
		{"two-byte exact", strings.Repeat("é", 256), strings.Repeat("é", 256)},
		{"two-byte cut", strings.Repeat("é", 257), strings.Repeat("é", 254) + "..."},
		{"three-byte cut", strings.Repeat("界", 171), strings.Repeat("界", 169) + "..."},
		{"four-byte exact", strings.Repeat("🛑", 128), strings.Repeat("🛑", 128)},
		{"four-byte cut", strings.Repeat("🛑", 129), strings.Repeat("🛑", 127) + "..."},
		{"cut at rune start", strings.Repeat("a", 509) + "🛑", strings.Repeat("a", 509) + "..."},
		{"preserve complete rune", strings.Repeat("a", 505) + "🛑" + "tail", strings.Repeat("a", 505) + "🛑..."},
		{"replacement character", strings.Repeat("\uFFFD", 171), strings.Repeat("\uFFFD", 169) + "..."},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := boundedMessage(tc.input)
			if got != tc.want || len(got) > 512 || !utf8.ValidString(got) {
				t.Fatalf("got %q (%d bytes), want %q", got, len(got), tc.want)
			}
			if boundedMessage(got) != got {
				t.Fatal("bounding an already bounded message changed it")
			}
		})
	}
	// All possible cut positions within 2-, 3- and 4-byte characters.
	for _, character := range []string{"é", "界", "🛑"} {
		for prefix := 505; prefix <= 510; prefix++ {
			input := strings.Repeat("a", prefix) + character + "suffix"
			got := boundedMessage(input)
			if len(got) > 512 || !utf8.ValidString(got) || !strings.HasSuffix(got, "...") ||
				!strings.HasPrefix(input, strings.TrimSuffix(got, "...")) {
				t.Fatalf("character=%q prefix=%d output=%q", character, prefix, got)
			}
		}
	}
}

func TestAdapterDiagnosticRetainsUTF8AndCause(t *testing.T) {
	a := mustParseExample(t, "semantic-routing.json")
	cause := errors.New(strings.Repeat("界", 171))
	calls := 0
	registry := ParameterValidators{
		a.Spec.Evaluators[0].Adapter: parameterValidatorFunc(func(json.RawMessage) error {
			calls++
			return cause
		}),
	}
	err := a.ValidateParameters(registry)
	var diagnostic *ValidationError
	if !errors.As(err, &diagnostic) || !errors.Is(err, cause) {
		t.Fatalf("expected typed rejection wrapping cause: %v", err)
	}
	if calls != 1 || diagnostic.Code != "parameters.adapter_invalid" || diagnostic.Path != "$.spec.evaluators[0].parameters" {
		t.Fatalf("calls=%d diagnostic=%+v", calls, diagnostic)
	}
	if diagnostic.Message != strings.Repeat("界", 169)+"..." || !utf8.ValidString(err.Error()) || len(diagnostic.Message) > 512 {
		t.Fatalf("invalid/truncated diagnostic: %q", diagnostic.Message)
	}
	if cause.Error() != strings.Repeat("界", 171) {
		t.Fatal("original cause changed")
	}
}

func TestBoundedMessageMalformedInputIsNotSanitized(t *testing.T) {
	// Preserve the preexisting short-message behavior. The helper only
	// guarantees that truncation does not break otherwise valid UTF-8.
	input := string([]byte{0xff, 'a'})
	if got := boundedMessage(input); got != input {
		t.Fatalf("short malformed input changed: %q", got)
	}
	// A malformed continuation-only prefix still terminates safely.
	if got := boundedMessage(strings.Repeat("\x80", 600)); got != "..." {
		t.Fatalf("unexpected malformed-prefix output: %q", got)
	}
}
