package profile

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
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

// Parse decodes and validates one evaluator profile. It enforces the portable
// structure and relational invariants; semantic adapter parameters still
// require ValidateParameters before execution.
func Parse(source []byte, format Format) (Artifact, error) {
	data, err := strictsource.Decode(source, strictsource.Format(format), MaxSourceBytes, "evaluator profile")
	if err != nil {
		return Artifact{}, profileSourceError(err)
	}
	artifact, err := decodeArtifact(data)
	if err != nil {
		return Artifact{}, err
	}
	if err := artifact.Validate(); err != nil {
		return Artifact{}, err
	}
	return artifact, nil
}

func LoadFile(path string) (Artifact, error) {
	format, err := formatForPath(path)
	if err != nil {
		return Artifact{}, err
	}
	file, err := os.Open(path)
	if err != nil {
		return Artifact{}, fmt.Errorf("open evaluator profile: %w", err)
	}
	defer file.Close()
	source, err := io.ReadAll(io.LimitReader(file, MaxSourceBytes+1))
	if err != nil {
		return Artifact{}, fmt.Errorf("read evaluator profile: %w", err)
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
		return "", parseError("source.format", "evaluator profile file must use .json, .yaml, or .yml", 0, 0)
	}
}

func decodeArtifact(data []byte) (Artifact, error) {
	if err := validateProfileShape(data); err != nil {
		return Artifact{}, err
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	decoder.UseNumber()
	var artifact Artifact
	if err := decoder.Decode(&artifact); err != nil {
		return Artifact{}, parseError("source.schema", err.Error(), 0, 0)
	}
	if err := strictsource.EnsureJSONEOF(decoder, "evaluator profile"); err != nil {
		return Artifact{}, profileSourceError(err)
	}
	return artifact, nil
}

func validateProfileShape(data []byte) error {
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.UseNumber()
	var document any
	if err := decoder.Decode(&document); err != nil {
		return parseError("source.schema", err.Error(), 0, 0)
	}
	root, err := objectAt(document, "$", "apiVersion", "kind", "metadata", "spec")
	if err != nil {
		return err
	}
	if err := requireKeys(root, "$", "apiVersion", "kind", "metadata", "spec"); err != nil {
		return err
	}
	if metadata, exists := root["metadata"]; exists {
		object, err := objectAt(metadata, "$.metadata", "name", "description")
		if err != nil {
			return err
		}
		if err := requireKeys(object, "$.metadata", "name"); err != nil {
			return err
		}
	}
	if rawSpec, exists := root["spec"]; exists {
		spec, err := objectAt(rawSpec, "$.spec", "totalTimeoutMs", "credentialSlots", "evaluators", "routing")
		if err != nil {
			return err
		}
		if err := requireKeys(spec, "$.spec", "totalTimeoutMs", "credentialSlots", "evaluators", "routing"); err != nil {
			return err
		}
		if err := validateObjectArray(spec["credentialSlots"], "$.spec.credentialSlots", func(value any, path string) error {
			object, err := objectAt(value, path, "name")
			if err != nil {
				return err
			}
			return requireKeys(object, path, "name")
		}); err != nil {
			return err
		}
		if err := validateObjectArray(spec["evaluators"], "$.spec.evaluators", validateEvaluatorShape); err != nil {
			return err
		}
		if rawRouting, exists := spec["routing"]; exists {
			if err := validateRoutingShape(rawRouting); err != nil {
				return err
			}
		}
	}
	return nil
}

func validateEvaluatorShape(value any, path string) error {
	evaluator, err := objectAt(value, path, "id", "mode", "adapter", "protocol", "requiredCapabilities", "timeoutMs", "retry", "provider", "model", "modelRevision", "instructionTemplate", "credentialSlot", "parameters")
	if err != nil {
		return err
	}
	if err := requireKeys(evaluator, path, "id", "mode", "adapter", "protocol", "requiredCapabilities", "timeoutMs", "retry", "parameters"); err != nil {
		return err
	}
	for _, nested := range []struct {
		key  string
		keys []string
	}{
		{key: "adapter", keys: []string{"id", "version"}},
		{key: "protocol", keys: []string{"id", "version"}},
		{key: "retry", keys: []string{"maxAttempts", "retryOn", "initialBackoffMs", "maxBackoffMs", "multiplier"}},
		{key: "instructionTemplate", keys: []string{"id", "digest"}},
	} {
		if raw, exists := evaluator[nested.key]; exists {
			object, err := objectAt(raw, path+"."+nested.key, nested.keys...)
			if err != nil {
				return err
			}
			required := nested.keys
			if nested.key == "retry" {
				required = []string{"maxAttempts", "retryOn"}
			} else if nested.key == "instructionTemplate" {
				required = []string{"digest"}
			}
			if err := requireKeys(object, path+"."+nested.key, required...); err != nil {
				return err
			}
		}
	}
	if raw, exists := evaluator["parameters"]; exists {
		if raw == nil {
			return parseError("source.schema", path+".parameters must not be null", 0, 0)
		}
		if _, ok := raw.(map[string]any); !ok {
			return parseError("source.schema", path+".parameters must be an object", 0, 0)
		}
	}
	return nil
}

func validateRoutingShape(value any) error {
	routing, err := objectAt(value, "$.spec.routing", "primary", "escalation", "fallbacks", "fallbackOn", "confidence", "terminal")
	if err != nil {
		return err
	}
	if err := requireKeys(routing, "$.spec.routing", "primary", "fallbacks", "confidence", "terminal"); err != nil {
		return err
	}
	if raw, exists := routing["confidence"]; exists {
		object, err := objectAt(raw, "$.spec.routing.confidence", "enabled", "minimumAccepted", "onLowConfidence")
		if err != nil {
			return err
		}
		if err := requireKeys(object, "$.spec.routing.confidence", "enabled"); err != nil {
			return err
		}
	}
	if raw, exists := routing["terminal"]; exists {
		object, err := objectAt(raw, "$.spec.routing.terminal", "onIndeterminate", "onFailure")
		if err != nil {
			return err
		}
		if err := requireKeys(object, "$.spec.routing.terminal", "onIndeterminate", "onFailure"); err != nil {
			return err
		}
	}
	return nil
}

func requireKeys(object map[string]any, path string, required ...string) error {
	for _, key := range required {
		if _, exists := object[key]; !exists {
			return parseError("source.schema", fmt.Sprintf("%s is missing required property %q", path, key), 0, 0)
		}
	}
	return nil
}

func objectAt(value any, path string, allowed ...string) (map[string]any, error) {
	object, ok := value.(map[string]any)
	if !ok {
		return nil, parseError("source.schema", path+" must be an object", 0, 0)
	}
	allowedSet := make(map[string]struct{}, len(allowed))
	for _, key := range allowed {
		allowedSet[key] = struct{}{}
	}
	keys := make([]string, 0, len(object))
	for key := range object {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		child := object[key]
		if _, exists := allowedSet[key]; !exists {
			return nil, parseError("source.schema", fmt.Sprintf("%s contains unknown property %q", path, key), 0, 0)
		}
		if child == nil {
			return nil, parseError("source.schema", path+"."+key+" must not be null", 0, 0)
		}
	}
	return object, nil
}

func validateObjectArray(value any, path string, validate func(any, string) error) error {
	if value == nil {
		return nil
	}
	items, ok := value.([]any)
	if !ok {
		return parseError("source.schema", path+" must be an array", 0, 0)
	}
	for i, item := range items {
		if err := validate(item, fmt.Sprintf("%s[%d]", path, i)); err != nil {
			return err
		}
	}
	return nil
}

func parseError(code, message string, line, column int) *ParseError {
	err := strictsource.NewError(code, message, line, column)
	return &ParseError{Code: err.Code, Message: err.Message, Line: err.Line, Column: err.Column}
}

func profileSourceError(err error) error {
	var sourceErr *strictsource.Error
	if !errors.As(err, &sourceErr) {
		return err
	}
	return &ParseError{Code: sourceErr.Code, Message: sourceErr.Message, Line: sourceErr.Line, Column: sourceErr.Column}
}
