package regression

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"os"

	"github.com/antaeusio/antaeus/internal/jsonvalue"
)

// LoadFile reads and parses a suite within the published source-size limit.
func LoadFile(path string) (Suite, error) {
	file, err := os.Open(path)
	if err != nil {
		return Suite{}, fmt.Errorf("open regression suite: %w", err)
	}
	defer file.Close()
	data, err := io.ReadAll(io.LimitReader(file, MaxSourceBytes+1))
	if err != nil {
		return Suite{}, fmt.Errorf("read regression suite: %w", err)
	}
	return Parse(data)
}

// Parse strictly decodes and validates one JSON suite. Unknown and duplicate
// properties, trailing values, invalid Unicode, and oversized sources are rejected.
func Parse(data []byte) (Suite, error) {
	if len(data) == 0 || len(data) > MaxSourceBytes {
		return Suite{}, fmt.Errorf("regression suite source must contain 1 to %d bytes", MaxSourceBytes)
	}
	canonical, err := jsonvalue.CanonicalObject(data)
	if err != nil {
		return Suite{}, fmt.Errorf("decode regression suite: %w", err)
	}
	decoder := json.NewDecoder(bytes.NewReader(canonical))
	decoder.DisallowUnknownFields()
	var suite Suite
	if err := decoder.Decode(&suite); err != nil {
		return Suite{}, fmt.Errorf("decode regression suite: %w", err)
	}
	var trailing any
	if err := decoder.Decode(&trailing); err != io.EOF {
		return Suite{}, fmt.Errorf("decode regression suite trailing data: %w", err)
	}
	if err := suite.Validate(); err != nil {
		return Suite{}, fmt.Errorf("validate regression suite: %w", err)
	}
	return suite, nil
}
