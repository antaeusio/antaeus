package schematest

import "testing"

func TestECMAPatternConformance(t *testing.T) {
	re, err := CompilePattern(`.*\S.*`)
	if err != nil {
		t.Fatal(err)
	}
	for _, tt := range TextCases(t, "../../contracts/conformance/v0alpha1/text/whitespace.json") {
		if got := re.MatchString(tt.Value); got != tt.ECMANonBlank {
			t.Errorf("%s: got %t want %t", tt.Name, got, tt.ECMANonBlank)
		}
	}
	for _, pattern := range []string{`\s`, `^\S+$`} {
		if _, err := CompilePattern(pattern); err == nil {
			t.Errorf("unsupported pattern accepted: %s", pattern)
		}
	}
}
