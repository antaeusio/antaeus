package fixture

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
)

const MaxSourceBytes = 1 << 20

// Parse strictly decodes and validates one JSON fixture set. Unknown fields,
// trailing JSON values, and oversized sources are rejected.
func Parse(data []byte) (Set, error) {
	if len(data) == 0 || len(data) > MaxSourceBytes {
		return Set{}, fmt.Errorf("fixture source must contain 1 to %d bytes", MaxSourceBytes)
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	var set Set
	if err := decoder.Decode(&set); err != nil {
		return Set{}, fmt.Errorf("decode fixture set: %w", err)
	}
	var trailing any
	if err := decoder.Decode(&trailing); err != io.EOF {
		if err == nil {
			return Set{}, fmt.Errorf("decode fixture set: trailing JSON value")
		}
		return Set{}, fmt.Errorf("decode fixture set trailing data: %w", err)
	}
	if err := set.Validate(); err != nil {
		return Set{}, fmt.Errorf("validate fixture set: %w", err)
	}
	return set, nil
}
