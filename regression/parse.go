package regression

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"os"

	"github.com/antaeusio/antaeus/internal/exactjson"
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
	if err := jsonvalue.ValidateUnicode(data); err != nil {
		return Suite{}, fmt.Errorf("decode regression suite: %w", err)
	}
	if err := validateExactSuiteKeys(data); err != nil {
		return Suite{}, err
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
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

func validateExactSuiteKeys(data []byte) error {
	root, err := exactjson.DecodeObject(data, "regression suite", []string{"apiVersion", "kind", "metadata", "policy", "fixtureSet", "cases"})
	if err != nil {
		return err
	}
	for _, nested := range []struct {
		name string
		key  string
		keys []string
	}{
		{name: "metadata", key: "metadata", keys: []string{"name", "version"}},
		{name: "policy", key: "policy", keys: []string{"name", "digest"}},
		{name: "fixtureSet", key: "fixtureSet", keys: []string{"name", "version"}},
	} {
		if encoded, exists := root[nested.key]; exists {
			if _, err := exactjson.DecodeObject(encoded, nested.name, nested.keys); err != nil {
				return err
			}
		}
	}
	if encodedCases, exists := root["cases"]; exists {
		var cases []json.RawMessage
		if err := json.Unmarshal(encodedCases, &cases); err != nil {
			return fmt.Errorf("decode regression cases: %w", err)
		}
		for index, encodedCase := range cases {
			caseObject, err := exactjson.DecodeObject(encodedCase, fmt.Sprintf("case %d", index), []string{"name", "description", "fixtureCase", "input", "expect"})
			if err != nil {
				return err
			}
			if description, exists := caseObject["description"]; exists && bytes.Equal(bytes.TrimSpace(description), []byte("null")) {
				return fmt.Errorf("decode case %d: description must be a string when present", index)
			}
			if encodedExpectation, exists := caseObject["expect"]; exists {
				if _, err := exactjson.DecodeObject(encodedExpectation, fmt.Sprintf("case %d expectation", index), []string{"outcome", "reasonCodes"}); err != nil {
					return err
				}
			}
		}
	}
	return nil
}
