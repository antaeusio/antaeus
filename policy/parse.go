package policy

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"unicode/utf8"

	"go.yaml.in/yaml/v3"
)

const (
	MaxNestingDepth = 32
	MaxParsedNodes  = 10_000
)

// Format identifies one supported policy authoring syntax.
type Format string

const (
	FormatJSON Format = "json"
	FormatYAML Format = "yaml"
)

// ParseError reports a bounded source error. Line and Column are one-based
// when the parser can identify a source location and zero otherwise.
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

// Parse decodes, structurally constrains, and semantically validates one
// policy source document. FormatJSON accepts exact JSON; FormatYAML accepts the
// constrained YAML 1.2 core-schema subset documented in contracts/README.md.
func Parse(source []byte, format Format) (Artifact, error) {
	if len(source) > MaxSourceBytes {
		return Artifact{}, parseError("source.too_large", fmt.Sprintf("policy source must not exceed %d bytes", MaxSourceBytes), 0, 0)
	}
	if !utf8.Valid(source) {
		return Artifact{}, parseError("source.invalid_utf8", "policy source must contain valid UTF-8", 0, 0)
	}

	var data []byte
	var err error
	switch format {
	case FormatJSON:
		if err := validateJSONDocument(source); err != nil {
			return Artifact{}, err
		}
		data = source
	case FormatYAML:
		data, err = decodeYAMLDocument(source)
		if err != nil {
			return Artifact{}, err
		}
	default:
		return Artifact{}, parseError("source.format", fmt.Sprintf("unsupported policy format %q", format), 0, 0)
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
	if err := rejectNullDescriptions(data); err != nil {
		return Artifact{}, err
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	var artifact Artifact
	if err := decoder.Decode(&artifact); err != nil {
		return Artifact{}, parseError("source.schema", boundedMessage(err.Error()), 0, 0)
	}
	if err := ensureJSONEOF(decoder); err != nil {
		return Artifact{}, err
	}
	return artifact, nil
}

func rejectNullDescriptions(data []byte) error {
	var envelope struct {
		Metadata struct {
			Description json.RawMessage `json:"description"`
		} `json:"metadata"`
		Spec struct {
			Rules []struct {
				Description json.RawMessage `json:"description"`
			} `json:"rules"`
		} `json:"spec"`
	}
	if err := json.Unmarshal(data, &envelope); err != nil {
		return parseError("source.schema", boundedMessage(err.Error()), 0, 0)
	}
	if bytes.Equal(bytes.TrimSpace(envelope.Metadata.Description), []byte("null")) {
		return parseError("source.schema", "metadata.description must be a string when present", 0, 0)
	}
	for i, rule := range envelope.Spec.Rules {
		if bytes.Equal(bytes.TrimSpace(rule.Description), []byte("null")) {
			return parseError("source.schema", fmt.Sprintf("spec.rules[%d].description must be a string when present", i), 0, 0)
		}
	}
	return nil
}

func validateJSONDocument(source []byte) error {
	decoder := json.NewDecoder(bytes.NewReader(source))
	decoder.UseNumber()
	nodes := 0
	if err := parseJSONValue(decoder, source, 1, &nodes); err != nil {
		return err
	}
	return ensureJSONEOF(decoder)
}

func parseJSONValue(decoder *json.Decoder, source []byte, depth int, nodes *int) error {
	if depth > MaxNestingDepth {
		return parseErrorAtOffset("source.depth", fmt.Sprintf("JSON nesting must not exceed %d", MaxNestingDepth), source, decoder.InputOffset())
	}
	*nodes++
	if *nodes > MaxParsedNodes {
		return parseErrorAtOffset("source.nodes", fmt.Sprintf("JSON must not exceed %d parsed nodes", MaxParsedNodes), source, decoder.InputOffset())
	}

	token, err := decoder.Token()
	if err != nil {
		return parseErrorAtOffset("source.syntax", boundedMessage(err.Error()), source, decoder.InputOffset())
	}
	delimiter, isDelimiter := token.(json.Delim)
	if !isDelimiter {
		return nil
	}

	switch delimiter {
	case '{':
		seen := make(map[string]struct{})
		for decoder.More() {
			keyToken, err := decoder.Token()
			if err != nil {
				return parseErrorAtOffset("source.syntax", boundedMessage(err.Error()), source, decoder.InputOffset())
			}
			key, ok := keyToken.(string)
			if !ok {
				return parseErrorAtOffset("source.syntax", "JSON object key must be a string", source, decoder.InputOffset())
			}
			*nodes++
			if *nodes > MaxParsedNodes {
				return parseErrorAtOffset("source.nodes", fmt.Sprintf("JSON must not exceed %d parsed nodes", MaxParsedNodes), source, decoder.InputOffset())
			}
			if _, exists := seen[key]; exists {
				return parseErrorAtOffset("source.duplicate_key", fmt.Sprintf("duplicate JSON object key %q", key), source, decoder.InputOffset())
			}
			seen[key] = struct{}{}
			if err := parseJSONValue(decoder, source, depth+1, nodes); err != nil {
				return err
			}
		}
	case '[':
		for decoder.More() {
			if err := parseJSONValue(decoder, source, depth+1, nodes); err != nil {
				return err
			}
		}
	default:
		return parseErrorAtOffset("source.syntax", "unexpected JSON delimiter", source, decoder.InputOffset())
	}
	if _, err := decoder.Token(); err != nil {
		return parseErrorAtOffset("source.syntax", boundedMessage(err.Error()), source, decoder.InputOffset())
	}
	return nil
}

func ensureJSONEOF(decoder *json.Decoder) error {
	var trailing any
	if err := decoder.Decode(&trailing); err == io.EOF {
		return nil
	} else if err != nil {
		return parseError("source.syntax", boundedMessage(err.Error()), 0, 0)
	}
	return parseError("source.multiple_documents", "policy source must contain exactly one document", 0, 0)
}

func decodeYAMLDocument(source []byte) ([]byte, error) {
	if line, column := yamlDirectiveLocation(source); line > 0 {
		return nil, parseError("source.directive", "YAML directives are not supported", line, column)
	}

	decoder := yaml.NewDecoder(bytes.NewReader(source))
	var document yaml.Node
	if err := decoder.Decode(&document); err != nil {
		if err == io.EOF {
			return nil, parseError("source.empty", "policy source must contain one document", 0, 0)
		}
		return nil, yamlSyntaxError(err)
	}
	var trailing yaml.Node
	if err := decoder.Decode(&trailing); err != io.EOF {
		if err != nil {
			return nil, yamlSyntaxError(err)
		}
		return nil, parseError("source.multiple_documents", "policy source must contain exactly one YAML document", trailing.Line, trailing.Column)
	}
	if document.Kind != yaml.DocumentNode || len(document.Content) != 1 {
		return nil, parseError("source.syntax", "policy source must contain one YAML document", document.Line, document.Column)
	}

	nodes := 0
	model, err := yamlNodeValue(document.Content[0], 1, &nodes)
	if err != nil {
		return nil, err
	}
	data, err := json.Marshal(model)
	if err != nil {
		return nil, parseError("source.syntax", boundedMessage(err.Error()), 0, 0)
	}
	return data, nil
}

func yamlNodeValue(node *yaml.Node, depth int, nodes *int) (any, error) {
	if depth > MaxNestingDepth {
		return nil, parseError("source.depth", fmt.Sprintf("YAML nesting must not exceed %d", MaxNestingDepth), node.Line, node.Column)
	}
	*nodes++
	if *nodes > MaxParsedNodes {
		return nil, parseError("source.nodes", fmt.Sprintf("YAML must not exceed %d parsed nodes", MaxParsedNodes), node.Line, node.Column)
	}
	if node.Anchor != "" || node.Alias != nil || node.Kind == yaml.AliasNode {
		return nil, parseError("source.alias", "YAML anchors and aliases are not supported", node.Line, node.Column)
	}
	if node.Style&yaml.TaggedStyle != 0 && !allowedExplicitYAMLTag(node.Tag) {
		return nil, parseError("source.tag", "custom and non-JSON YAML tags are not supported", node.Line, node.Column)
	}

	switch node.Kind {
	case yaml.MappingNode:
		if node.Tag != "!!map" && node.Tag != "tag:yaml.org,2002:map" {
			return nil, parseError("source.tag", "custom YAML mapping tags are not supported", node.Line, node.Column)
		}
		if len(node.Content)%2 != 0 {
			return nil, parseError("source.syntax", "YAML mapping is incomplete", node.Line, node.Column)
		}
		result := make(map[string]any, len(node.Content)/2)
		for i := 0; i < len(node.Content); i += 2 {
			keyNode := node.Content[i]
			if keyNode.Kind != yaml.ScalarNode || resolveYAMLScalarKind(keyNode) != "string" {
				return nil, parseError("source.non_string_key", "YAML mapping keys must be strings", keyNode.Line, keyNode.Column)
			}
			if keyNode.Tag == "!!merge" || keyNode.Tag == "tag:yaml.org,2002:merge" {
				return nil, parseError("source.merge", "YAML merge keys are not supported", keyNode.Line, keyNode.Column)
			}
			key := keyNode.Value
			if _, exists := result[key]; exists {
				return nil, parseError("source.duplicate_key", fmt.Sprintf("duplicate YAML mapping key %q", key), keyNode.Line, keyNode.Column)
			}
			*nodes++
			if *nodes > MaxParsedNodes {
				return nil, parseError("source.nodes", fmt.Sprintf("YAML must not exceed %d parsed nodes", MaxParsedNodes), keyNode.Line, keyNode.Column)
			}
			value, err := yamlNodeValue(node.Content[i+1], depth+1, nodes)
			if err != nil {
				return nil, err
			}
			result[key] = value
		}
		return result, nil
	case yaml.SequenceNode:
		if node.Tag != "!!seq" && node.Tag != "tag:yaml.org,2002:seq" {
			return nil, parseError("source.tag", "custom YAML sequence tags are not supported", node.Line, node.Column)
		}
		result := make([]any, len(node.Content))
		for i, child := range node.Content {
			value, err := yamlNodeValue(child, depth+1, nodes)
			if err != nil {
				return nil, err
			}
			result[i] = value
		}
		return result, nil
	case yaml.ScalarNode:
		return yamlScalarValue(node)
	default:
		return nil, parseError("source.syntax", "unsupported YAML node", node.Line, node.Column)
	}
}

var (
	yamlIntegerPattern = regexp.MustCompile(`^(?:[+-]?[0-9]+|0o[0-7]+|0x[0-9a-fA-F]+)$`)
	yamlFloatPattern   = regexp.MustCompile(`^[+-]?(?:(?:[0-9]+\.[0-9]*|\.[0-9]+)(?:[eE][+-]?[0-9]+)?|[0-9]+[eE][+-]?[0-9]+)$`)
)

func yamlScalarValue(node *yaml.Node) (any, error) {
	kind := resolveYAMLScalarKind(node)
	switch kind {
	case "string":
		return node.Value, nil
	case "null":
		return nil, nil
	case "boolean":
		return strings.EqualFold(node.Value, "true"), nil
	case "number":
		value := node.Value
		if strings.HasPrefix(value, "+") {
			value = value[1:]
		}
		if strings.ContainsAny(value, "xXoO") {
			integer, err := strconv.ParseInt(value, 0, 64)
			if err != nil {
				return nil, parseError("source.number", "YAML number is outside the supported JSON range", node.Line, node.Column)
			}
			return integer, nil
		}
		number, err := strconv.ParseFloat(value, 64)
		if err != nil || math.IsInf(number, 0) || math.IsNaN(number) {
			return nil, parseError("source.number", "YAML number must be finite and representable as JSON", node.Line, node.Column)
		}
		return number, nil
	default:
		return nil, parseError("source.tag", "custom and non-JSON YAML tags are not supported", node.Line, node.Column)
	}
}

func resolveYAMLScalarKind(node *yaml.Node) string {
	if node.Style&yaml.TaggedStyle != 0 {
		switch node.Tag {
		case "!!str", "tag:yaml.org,2002:str":
			return "string"
		case "!!null", "tag:yaml.org,2002:null":
			return "null"
		case "!!bool", "tag:yaml.org,2002:bool":
			return "boolean"
		case "!!int", "!!float", "tag:yaml.org,2002:int", "tag:yaml.org,2002:float":
			return "number"
		default:
			return "unsupported"
		}
	}
	if node.Style != 0 {
		return "string"
	}
	value := node.Value
	if value == "" || value == "~" || strings.EqualFold(value, "null") {
		return "null"
	}
	if strings.EqualFold(value, "true") || strings.EqualFold(value, "false") {
		return "boolean"
	}
	if yamlIntegerPattern.MatchString(value) || yamlFloatPattern.MatchString(value) {
		return "number"
	}
	if strings.EqualFold(value, ".inf") || strings.EqualFold(value, "+.inf") || strings.EqualFold(value, "-.inf") || strings.EqualFold(value, ".nan") {
		return "number"
	}
	return "string"
}

func allowedExplicitYAMLTag(tag string) bool {
	switch tag {
	case "!!str", "!!null", "!!bool", "!!int", "!!float",
		"tag:yaml.org,2002:str", "tag:yaml.org,2002:null", "tag:yaml.org,2002:bool",
		"tag:yaml.org,2002:int", "tag:yaml.org,2002:float",
		"!!map", "!!seq", "tag:yaml.org,2002:map", "tag:yaml.org,2002:seq":
		return true
	default:
		return false
	}
}

func yamlDirectiveLocation(source []byte) (int, int) {
	for index, line := range bytes.Split(source, []byte{'\n'}) {
		if index == 0 {
			line = bytes.TrimPrefix(line, []byte{0xef, 0xbb, 0xbf})
		}
		if len(line) > 0 && line[0] == '%' {
			return index + 1, 1
		}
	}
	return 0, 0
}

func yamlSyntaxError(err error) error {
	var typeError *yaml.TypeError
	if errors.As(err, &typeError) {
		return parseError("source.syntax", boundedMessage(typeError.Error()), 0, 0)
	}
	message := boundedMessage(err.Error())
	var line, column int
	_, _ = fmt.Sscanf(message, "yaml: line %d: column %d:", &line, &column)
	return parseError("source.syntax", message, line, column)
}

func parseErrorAtOffset(code, message string, source []byte, offset int64) *ParseError {
	if offset < 0 {
		offset = 0
	}
	if offset > int64(len(source)) {
		offset = int64(len(source))
	}
	line := 1 + bytes.Count(source[:offset], []byte{'\n'})
	lastNewline := bytes.LastIndexByte(source[:offset], '\n')
	column := int(offset) + 1
	if lastNewline >= 0 {
		column = int(offset) - lastNewline
	}
	return parseError(code, message, line, column)
}

func parseError(code, message string, line, column int) *ParseError {
	return &ParseError{Code: code, Message: boundedMessage(message), Line: line, Column: column}
}

func boundedMessage(message string) string {
	const max = 512
	if utf8.RuneCountInString(message) <= max {
		return message
	}
	runes := []rune(message)
	return string(runes[:max])
}
