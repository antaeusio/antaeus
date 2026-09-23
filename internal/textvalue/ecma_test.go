package textvalue

import (
	"strings"
	"testing"

	"github.com/antaeusio/antaeus/internal/schematest"
)

func TestPortableTextCases(t *testing.T) {
	for _, tt := range schematest.TextCases(t, "../../contracts/conformance/v0alpha1/text/whitespace.json") {
		t.Run(tt.Name, func(t *testing.T) {
			if got := ValidECMAText(tt.Value, 256); got != tt.ECMANonBlank {
				t.Fatalf("ECMA=%t want %t", got, tt.ECMANonBlank)
			}
			if got := strings.TrimSpace(tt.Value) != ""; got != tt.UnicodeNonBlank {
				t.Fatalf("Unicode=%t want %t", got, tt.UnicodeNonBlank)
			}
		})
	}
	for _, limit := range []int{128, 256} {
		if !ValidECMAText(strings.Repeat("😀", limit), limit) || ValidECMAText(strings.Repeat("😀", limit+1), limit) {
			t.Fatal("code-point bounds differ")
		}
	}
	for _, value := range []string{"\xff", "model\xff", "\xed\xa0\x80"} {
		if ValidECMAText(value, 256) {
			t.Fatal("invalid UTF-8 accepted")
		}
	}
}
