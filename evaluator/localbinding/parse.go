package localbinding

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/antaeusio/antaeus/internal/strictsource"
)

const (
	MaxNestingDepth = strictsource.MaxNestingDepth
	MaxParsedNodes  = strictsource.MaxParsedNodes
)

type Format string

const (
	FormatJSON Format = "json"
	FormatYAML Format = "yaml"
)

type ParseError struct {
	Code    string
	Message string
	Line    int
	Column  int
}

func (e *ParseError) Error() string {
	if e.Line > 0 {
		return fmt.Sprintf("line %d, column %d: %s", e.Line, e.Column, e.Message)
	}
	return e.Message
}

func Parse(source []byte, format Format) (Artifact, error) {
	data, err := strictsource.Decode(source, strictsource.Format(format), MaxSourceBytes, "local secret bindings")
	if err != nil {
		return Artifact{}, bindingSourceError(err)
	}
	if err := validateShape(data); err != nil {
		return Artifact{}, err
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	var artifact Artifact
	if err := decoder.Decode(&artifact); err != nil {
		return Artifact{}, parseError("source.schema", err.Error(), 0, 0)
	}
	if err := strictsource.EnsureJSONEOF(decoder, "local secret bindings"); err != nil {
		return Artifact{}, bindingSourceError(err)
	}
	if err := artifact.Validate(); err != nil {
		return Artifact{}, err
	}
	return artifact, nil
}

// LoadFile loads bindings only from the explicit path supplied by the caller.
// It performs no parent-directory search and never loads .env files.
func LoadFile(path string) (Artifact, error) {
	format, err := formatForPath(path)
	if err != nil {
		return Artifact{}, err
	}
	file, err := os.Open(path)
	if err != nil {
		return Artifact{}, fmt.Errorf("open local secret bindings: %w", err)
	}
	defer file.Close()
	source, err := io.ReadAll(io.LimitReader(file, MaxSourceBytes+1))
	if err != nil {
		return Artifact{}, fmt.Errorf("read local secret bindings: %w", err)
	}
	return Parse(source, format)
}

func formatForPath(path string) (Format, error) {
	switch strings.ToLower(filepath.Ext(path)) {
	case ".json":
		return FormatJSON, nil
	case ".yaml", ".yml":
		return FormatYAML, nil
	default:
		return "", parseError("source.format", "local secret bindings file must use .json, .yaml, or .yml", 0, 0)
	}
}

func validateShape(data []byte) error {
	decoder := json.NewDecoder(bytes.NewReader(data))
	var document any
	if err := decoder.Decode(&document); err != nil {
		return parseError("source.schema", err.Error(), 0, 0)
	}
	root, ok := document.(map[string]any)
	if !ok {
		return parseError("source.schema", "local secret bindings source must be an object", 0, 0)
	}
	if err := exactKeys(root, "$", "apiVersion", "kind", "secretBindings"); err != nil {
		return err
	}
	for _, key := range []string{"apiVersion", "kind", "secretBindings"} {
		value, exists := root[key]
		if !exists {
			return parseError("source.schema", fmt.Sprintf("$ is missing required property %q", key), 0, 0)
		}
		if value == nil {
			return parseError("source.schema", "$."+key+" must not be null", 0, 0)
		}
	}
	bindings, ok := root["secretBindings"].(map[string]any)
	if !ok {
		return parseError("source.schema", "$.secretBindings must be an object", 0, 0)
	}
	for adapterID, rawSlots := range bindings {
		slots, ok := rawSlots.(map[string]any)
		if !ok {
			return parseError("source.schema", fmt.Sprintf("$.secretBindings[%q] must be an object", adapterID), 0, 0)
		}
		for slot, rawReference := range slots {
			reference, ok := rawReference.(map[string]any)
			path := fmt.Sprintf("$.secretBindings[%q][%q]", adapterID, slot)
			if !ok {
				return parseError("source.schema", path+" must be an object", 0, 0)
			}
			if err := exactKeys(reference, path, "source", "name"); err != nil {
				return err
			}
			for _, key := range []string{"source", "name"} {
				value, exists := reference[key]
				if !exists {
					return parseError("source.schema", fmt.Sprintf("%s is missing required property %q", path, key), 0, 0)
				}
				if value == nil {
					return parseError("source.schema", path+"."+key+" must not be null", 0, 0)
				}
			}
		}
	}
	return nil
}

func exactKeys(object map[string]any, path string, allowed ...string) error {
	set := make(map[string]struct{}, len(allowed))
	for _, key := range allowed {
		set[key] = struct{}{}
	}
	for key := range object {
		if _, exists := set[key]; !exists {
			return parseError("source.schema", fmt.Sprintf("%s contains unknown property %q", path, key), 0, 0)
		}
	}
	return nil
}

func parseError(code, message string, line, column int) *ParseError {
	err := strictsource.NewError(code, message, line, column)
	return &ParseError{Code: err.Code, Message: err.Message, Line: err.Line, Column: err.Column}
}

func bindingSourceError(err error) error {
	var sourceErr *strictsource.Error
	if !errors.As(err, &sourceErr) {
		return err
	}
	return &ParseError{Code: sourceErr.Code, Message: sourceErr.Message, Line: sourceErr.Line, Column: sourceErr.Column}
}
