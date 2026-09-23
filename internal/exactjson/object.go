// Package exactjson rejects unknown, case-variant, and duplicate object keys.
package exactjson

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
)

// DecodeObject returns the raw values of one object whose keys exactly match
// the allowed set. It rejects unknown, case-variant, and duplicate properties.
func DecodeObject(data []byte, name string, allowedKeys []string) (map[string]json.RawMessage, error) {
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
