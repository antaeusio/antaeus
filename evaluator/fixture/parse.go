package fixture

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"os"

	"github.com/antaeusio/antaeus/internal/exactjson"
)

const MaxSourceBytes = 1 << 20

// LoadFile reads and parses a fixture set without allocating beyond the
// published source-size limit.
func LoadFile(path string) (Set, error) {
	file, err := os.Open(path)
	if err != nil {
		return Set{}, fmt.Errorf("open fixture set: %w", err)
	}
	defer file.Close()
	data, err := io.ReadAll(io.LimitReader(file, MaxSourceBytes+1))
	if err != nil {
		return Set{}, fmt.Errorf("read fixture set: %w", err)
	}
	return Parse(data)
}

// Parse strictly decodes and validates one JSON fixture set. Unknown fields,
// trailing JSON values, and oversized sources are rejected.
func Parse(data []byte) (Set, error) {
	if len(data) == 0 || len(data) > MaxSourceBytes {
		return Set{}, fmt.Errorf("fixture source must contain 1 to %d bytes", MaxSourceBytes)
	}
	if err := validateExactFixtureKeys(data); err != nil {
		return Set{}, err
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

func validateExactFixtureKeys(data []byte) error {
	root, err := exactjson.DecodeObject(data, "fixture set", []string{"apiVersion", "kind", "metadata", "cases"})
	if err != nil {
		return err
	}
	if metadata, exists := root["metadata"]; exists {
		if _, err := exactjson.DecodeObject(metadata, "metadata", []string{"name", "version"}); err != nil {
			return err
		}
	}
	if encodedCases, exists := root["cases"]; exists {
		var cases []json.RawMessage
		if err := json.Unmarshal(encodedCases, &cases); err != nil {
			return fmt.Errorf("decode fixture cases: %w", err)
		}
		for i, encodedCase := range cases {
			fixtureCase, err := exactjson.DecodeObject(encodedCase, fmt.Sprintf("case %d", i), []string{"name", "policyName", "policyDigest", "inputDigest", "ruleResults"})
			if err != nil {
				return err
			}
			if encodedResults, exists := fixtureCase["ruleResults"]; exists {
				var results []json.RawMessage
				if err := json.Unmarshal(encodedResults, &results); err != nil {
					return fmt.Errorf("decode case %d rule results: %w", i, err)
				}
				for resultIndex, encodedResult := range results {
					if _, err := exactjson.DecodeObject(encodedResult, fmt.Sprintf("case %d rule result %d", i, resultIndex), []string{"ruleId", "status", "confidence", "reasonCodes", "message"}); err != nil {
						return err
					}
				}
			}
		}
	}
	return nil
}
