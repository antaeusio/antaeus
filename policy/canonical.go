package policy

import (
	"crypto/sha256"
	"encoding/hex"
)

// CanonicalJSON validates the artifact and returns its RFC 8785 canonical JSON.
// The v0alpha1 schema contains only objects, arrays, and strings, so this
// encoder deliberately implements that closed data model rather than a wider
// number serializer.
func (a Artifact) CanonicalJSON() ([]byte, error) {
	if err := a.Validate(); err != nil {
		return nil, err
	}

	dst := make([]byte, 0, 512)
	dst = append(dst, `{"apiVersion":`...)
	dst = appendString(dst, a.APIVersion)
	dst = append(dst, `,"kind":`...)
	dst = appendString(dst, a.Kind)
	dst = append(dst, `,"metadata":{`...)
	if a.Metadata.Description != nil {
		dst = append(dst, `"description":`...)
		dst = appendString(dst, *a.Metadata.Description)
		dst = append(dst, ',')
	}
	dst = append(dst, `"name":`...)
	dst = appendString(dst, a.Metadata.Name)
	dst = append(dst, `},"spec":{"defaultOutcome":`...)
	dst = appendString(dst, string(a.Spec.DefaultOutcome))
	dst = append(dst, `,"rules":[`...)
	for i, rule := range a.Spec.Rules {
		if i > 0 {
			dst = append(dst, ',')
		}
		dst = append(dst, '{')
		if rule.Description != nil {
			dst = append(dst, `"description":`...)
			dst = appendString(dst, *rule.Description)
			dst = append(dst, ',')
		}
		dst = append(dst, `"id":`...)
		dst = appendString(dst, rule.ID)
		dst = append(dst, `,"outcome":`...)
		dst = appendString(dst, string(rule.Outcome))
		dst = append(dst, `,"when":`...)
		dst = appendString(dst, rule.When)
		dst = append(dst, '}')
	}
	dst = append(dst, `]}}`...)
	return dst, nil
}

// Digest returns the lowercase SHA-256 content identifier for CanonicalJSON.
func (a Artifact) Digest() (string, error) {
	canonical, err := a.CanonicalJSON()
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(canonical)
	return "sha256:" + hex.EncodeToString(sum[:]), nil
}

func appendString(dst []byte, value string) []byte {
	const hexDigits = "0123456789abcdef"

	dst = append(dst, '"')
	for _, r := range value {
		switch r {
		case '"', '\\':
			dst = append(dst, '\\', byte(r))
		case '\b':
			dst = append(dst, `\b`...)
		case '\t':
			dst = append(dst, `\t`...)
		case '\n':
			dst = append(dst, `\n`...)
		case '\f':
			dst = append(dst, `\f`...)
		case '\r':
			dst = append(dst, `\r`...)
		default:
			if r >= 0 && r <= 0x1f {
				dst = append(dst, '\\', 'u', '0', '0', hexDigits[byte(r)>>4], hexDigits[byte(r)&0x0f])
				continue
			}
			dst = append(dst, string(r)...)
		}
	}
	return append(dst, '"')
}
