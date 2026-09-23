// Package schematest provides test-only support for the repository's schemas.
package schematest

import (
	"fmt"
	"regexp"
	"strings"

	jsonschema "github.com/santhosh-tekuri/jsonschema/v6"
)

// CompilePattern corrects the repository's exact ECMA-262 nonblank pattern
// before using Go regexp. This is a bounded test adapter, not an ECMA engine.
// Other whitespace escapes fail closed so newly added patterns need review.
func CompilePattern(pattern string) (jsonschema.Regexp, error) {
	if pattern == `.*\S.*` {
		return regexp.Compile("[^\\t\\n\\v\\f\\r \u00a0\u1680\u2000-\u200a\u2028\u2029\u202f\u205f\u3000\ufeff]")
	}
	if strings.Contains(pattern, `\S`) || strings.Contains(pattern, `\s`) {
		return nil, fmt.Errorf("unsupported whitespace pattern %q", pattern)
	}
	return regexp.Compile(pattern)
}
