package policy

import (
	"bytes"
	"encoding/json"
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

// Format identifies one supported policy authoring syntax.
type Format = strictsource.Format

const (
	FormatJSON = strictsource.FormatJSON
	FormatYAML = strictsource.FormatYAML
)

// ParseError reports a bounded source error. Line and Column are one-based
// Unicode character positions, excluding an optional UTF-8 byte-order mark,
// when the parser can identify a source location and zero otherwise.
type ParseError = strictsource.Error

// Parse decodes, structurally constrains, and semantically validates one
// policy source document. FormatJSON accepts exact JSON; FormatYAML accepts the
// constrained YAML 1.2 core-schema subset documented in contracts/README.md.
func Parse(source []byte, format Format) (Artifact, error) {
	data, err := strictsource.Decode(source, format, MaxSourceBytes, "policy")
	if err != nil {
		return Artifact{}, err
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

// LoadFile reads and parses a .json, .yaml, or .yml policy without allocating
// beyond the published source-size limit.
func LoadFile(path string) (Artifact, error) {
	format, err := formatForPath(path)
	if err != nil {
		return Artifact{}, err
	}
	file, err := os.Open(path)
	if err != nil {
		return Artifact{}, fmt.Errorf("open policy: %w", err)
	}
	defer file.Close()

	source, err := io.ReadAll(io.LimitReader(file, MaxSourceBytes+1))
	if err != nil {
		return Artifact{}, fmt.Errorf("read policy: %w", err)
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
		return "", parseError("source.format", "policy file must use .json, .yaml, or .yml", 0, 0)
	}
}

func decodeArtifact(data []byte) (Artifact, error) {
	if err := validatePolicyObjectShape(data); err != nil {
		return Artifact{}, err
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	var artifact Artifact
	if err := decoder.Decode(&artifact); err != nil {
		return Artifact{}, parseError("source.schema", err.Error(), 0, 0)
	}
	var trailing any
	if err := decoder.Decode(&trailing); err != io.EOF {
		return Artifact{}, parseError("source.trailing", "source must contain exactly one document", 0, 0)
	}
	return artifact, nil
}

func validatePolicyObjectShape(data []byte) error {
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.UseNumber()
	var document any
	if err := decoder.Decode(&document); err != nil {
		return parseError("source.schema", err.Error(), 0, 0)
	}
	root, ok := document.(map[string]any)
	if !ok {
		return parseError("source.schema", "policy source must be an object", 0, 0)
	}
	if err := validateExactKeys("$", root, "apiVersion", "kind", "metadata", "spec"); err != nil {
		return err
	}
	if err := rejectNullValues("$", root); err != nil {
		return err
	}

	if raw, exists := root["metadata"]; exists {
		metadata, ok := raw.(map[string]any)
		if !ok {
			return parseError("source.schema", "$.metadata must be an object", 0, 0)
		}
		if err := validateExactKeys("$.metadata", metadata, "name", "description"); err != nil {
			return err
		}
	}
	if raw, exists := root["spec"]; exists {
		spec, ok := raw.(map[string]any)
		if !ok {
			return parseError("source.schema", "$.spec must be an object", 0, 0)
		}
		if err := validateExactKeys("$.spec", spec, "defaultOutcome", "rules"); err != nil {
			return err
		}
		if rawRules, exists := spec["rules"]; exists {
			rules, ok := rawRules.([]any)
			if !ok {
				return parseError("source.schema", "$.spec.rules must be an array", 0, 0)
			}
			for i, rawRule := range rules {
				rule, ok := rawRule.(map[string]any)
				if !ok {
					return parseError("source.schema", fmt.Sprintf("$.spec.rules[%d] must be an object", i), 0, 0)
				}
				if err := validateExactKeys(fmt.Sprintf("$.spec.rules[%d]", i), rule, "id", "description", "when", "outcome"); err != nil {
					return err
				}
			}
		}
	}
	return nil
}

func validateExactKeys(path string, object map[string]any, allowed ...string) error {
	allowedSet := make(map[string]struct{}, len(allowed))
	for _, name := range allowed {
		allowedSet[name] = struct{}{}
	}
	for name := range object {
		if _, exists := allowedSet[name]; !exists {
			return parseError("source.schema", fmt.Sprintf("%s contains unknown property %q", path, name), 0, 0)
		}
	}
	return nil
}

func rejectNullValues(path string, value any) error {
	switch typed := value.(type) {
	case nil:
		return parseError("source.schema", path+" must not be null", 0, 0)
	case map[string]any:
		for name, child := range typed {
			if err := rejectNullValues(path+"."+name, child); err != nil {
				return err
			}
		}
	case []any:
		for i, child := range typed {
			if err := rejectNullValues(fmt.Sprintf("%s[%d]", path, i), child); err != nil {
				return err
			}
		}
	}
	return nil
}

func parseError(code, message string, line, column int) *ParseError {
	return strictsource.NewError(code, message, line, column)
}
