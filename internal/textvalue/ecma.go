// Package textvalue implements field-specific portable text predicates.
package textvalue

import "unicode/utf8"

// ECMANonBlank matches the non-whitespace requirement of the published
// unanchored ECMA-262 pattern .*\S.*. It does not normalize or trim text.
// This is NOT Unicode White_Space: U+FEFF is whitespace; U+0085 is not.
func ECMANonBlank(value string) bool {
	for _, r := range value {
		switch r {
		case '\t', '\n', '\v', '\f', '\r', ' ', '\u00a0', '\u1680',
			'\u2000', '\u2001', '\u2002', '\u2003', '\u2004', '\u2005',
			'\u2006', '\u2007', '\u2008', '\u2009', '\u200a',
			'\u2028', '\u2029', '\u202f', '\u205f', '\u3000', '\ufeff':
		default:
			return true
		}
	}
	return false
}

// ValidECMAText also enforces UTF-8 and the field's Unicode code-point bound.
func ValidECMAText(value string, maxRunes int) bool {
	return utf8.ValidString(value) && utf8.RuneCountInString(value) <= maxRunes && ECMANonBlank(value)
}
