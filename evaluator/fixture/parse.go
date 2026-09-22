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
	root, err := decodeExactObject(data, "fixture set", []string{"apiVersion", "kind", "metadata", "cases"})
	if err != nil {
		return err
	}
	if metadata, exists := root["metadata"]; exists {
		if _, err := decodeExactObject(metadata, "metadata", []string{"name", "version"}); err != nil {
			return err
		}
	}
	if encodedCases, exists := root["cases"]; exists {
		var cases []json.RawMessage
		if err := json.Unmarshal(encodedCases, &cases); err != nil {
			return fmt.Errorf("decode fixture cases: %w", err)
		}
		for i, encodedCase := range cases {
			fixtureCase, err := decodeExactObject(encodedCase, fmt.Sprintf("case %d", i), []string{"name", "policyName", "policyDigest", "inputDigest", "ruleResults"})
			if err != nil {
				return err
			}
			if encodedResults, exists := fixtureCase["ruleResults"]; exists {
				var results []json.RawMessage
				if err := json.Unmarshal(encodedResults, &results); err != nil {
					return fmt.Errorf("decode case %d rule results: %w", i, err)
				}
				for resultIndex, encodedResult := range results {
					if _, err := decodeExactObject(encodedResult, fmt.Sprintf("case %d rule result %d", i, resultIndex), []string{"ruleId", "status", "confidence", "reasonCodes", "message"}); err != nil {
						return err
					}
				}
			}
		}
	}
	return nil
}

func decodeExactObject(data []byte, name string, allowedKeys []string) (map[string]json.RawMessage, error) {
	allowed := make(map[string]struct{}, len(allowedKeys))
	for _, key := range allowedKeys {
		allowed[key] = struct{}{}
	}

	decoder := json.NewDecoder(bytes.NewReader(data))
	opening, err := decoder.Token()
	if err != nil {
		return nil, fmt.Errorf("decode %s: %w", name, err)
	}
	if delimiter, ok := opening.(json.Delim); !ok || delimiter != '{' {
		return nil, fmt.Errorf("decode %s: expected object", name)
	}
	values := make(map[string]json.RawMessage, len(allowedKeys))
	for decoder.More() {
		token, err := decoder.Token()
		if err != nil {
			return nil, fmt.Errorf("decode %s property: %w", name, err)
		}
		key, ok := token.(string)
		if !ok {
			return nil, fmt.Errorf("decode %s: property name is not a string", name)
		}
		if _, exists := allowed[key]; !exists {
			return nil, fmt.Errorf("decode %s: unknown property %q", name, key)
		}
		if _, exists := values[key]; exists {
			return nil, fmt.Errorf("decode %s: duplicate property %q", name, key)
		}
		var value json.RawMessage
		if err := decoder.Decode(&value); err != nil {
			return nil, fmt.Errorf("decode %s property %q: %w", name, key, err)
		}
		values[key] = value
	}
	closing, err := decoder.Token()
	if err != nil {
		return nil, fmt.Errorf("decode %s: %w", name, err)
	}
	if delimiter, ok := closing.(json.Delim); !ok || delimiter != '}' {
		return nil, fmt.Errorf("decode %s: expected closing object", name)
	}
	var trailing any
	if err := decoder.Decode(&trailing); err != io.EOF {
		if err == nil {
			return nil, fmt.Errorf("decode %s: trailing JSON value", name)
		}
		return nil, fmt.Errorf("decode %s trailing data: %w", name, err)
	}
	return values, nil
}
