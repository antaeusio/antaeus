// Package strictsource decodes the constrained JSON/YAML data model shared by portable contracts.
package strictsource

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
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

// Format identifies one supported portable-contract authoring syntax.
type Format string

const (
	FormatJSON Format = "json"
	FormatYAML Format = "yaml"
)

// Error reports a bounded source error. Line and Column are one-based Unicode
// character positions, excluding an optional UTF-8 byte-order mark, when known.
type Error struct {
	Code    string
	Message string
	Line    int
	Column  int
}

func (e *Error) Error() string {
	if e.Line > 0 {
		return fmt.Sprintf("line %d, column %d: %s", e.Line, e.Column, e.Message)
	}
	return e.Message
}

// Decode validates one source document and returns its exact JSON data model.
func Decode(source []byte, format Format, maxBytes int, subject string) ([]byte, error) {
	if len(source) > maxBytes {
		return nil, parseError("source.too_large", fmt.Sprintf("%s source must not exceed %d bytes", subject, maxBytes), 0, 0)
	}
	if !utf8.Valid(source) {
		return nil, parseError("source.invalid_utf8", subject+" source must contain valid UTF-8", 0, 0)
	}

	switch format {
	case FormatJSON:
		if err := validateJSONUnicodeEscapes(source); err != nil {
			return nil, err
		}
		if err := validateJSONDocument(source); err != nil {
			return nil, err
		}
		return source, nil
	case FormatYAML:
		return decodeYAMLDocument(source)
	default:
		return nil, parseError("source.format", fmt.Sprintf("unsupported %s format %q", subject, format), 0, 0)
	}
}

// NewError constructs a bounded source error for a contract-specific decoder.
func NewError(code, message string, line, column int) *Error {
	return parseError(code, message, line, column)
}

func validateJSONUnicodeEscapes(source []byte) error {
	inString := false
	for i := 0; i < len(source); i++ {
		switch source[i] {
		case '"':
			inString = !inString
		case '\\':
			if !inString || i+1 >= len(source) {
				continue
			}
			if source[i+1] != 'u' {
				i++
				continue
			}
			code, ok := jsonHexCodeUnit(source, i+2)
			if !ok {
				continue
			}
			if code >= 0xdc00 && code <= 0xdfff {
				return parseErrorAtOffset("source.invalid_unicode", "JSON string contains an unpaired low surrogate", source, int64(i))
			}
			if code >= 0xd800 && code <= 0xdbff {
				if i+12 > len(source) || source[i+6] != '\\' || source[i+7] != 'u' {
					return parseErrorAtOffset("source.invalid_unicode", "JSON string contains an unpaired high surrogate", source, int64(i))
				}
				low, valid := jsonHexCodeUnit(source, i+8)
				if !valid || low < 0xdc00 || low > 0xdfff {
					return parseErrorAtOffset("source.invalid_unicode", "JSON string contains an unpaired high surrogate", source, int64(i))
				}
				i += 11
				continue
			}
			i += 5
		}
	}
	return nil
}

func jsonHexCodeUnit(source []byte, start int) (uint16, bool) {
	if start+4 > len(source) {
		return 0, false
	}
	value, err := strconv.ParseUint(string(source[start:start+4]), 16, 16)
	return uint16(value), err == nil
}

func validateJSONDocument(source []byte) error {
	decoder := json.NewDecoder(bytes.NewReader(source))
	decoder.UseNumber()
	nodes := 0
	if err := parseJSONValue(decoder, source, 0, &nodes); err != nil {
		return err
	}
	return ensureJSONEOF(decoder)
}

func parseJSONValue(decoder *json.Decoder, source []byte, containerDepth int, nodes *int) error {
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
	containerDepth++
	if containerDepth > MaxNestingDepth {
		return parseErrorAtOffset("source.depth", fmt.Sprintf("JSON nesting must not exceed %d containers", MaxNestingDepth), source, decoder.InputOffset())
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
			if err := parseJSONValue(decoder, source, containerDepth, nodes); err != nil {
				return err
			}
		}
	case '[':
		for decoder.More() {
			if err := parseJSONValue(decoder, source, containerDepth, nodes); err != nil {
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
	if err := validateYAMLLineBreaks(source); err != nil {
		return nil, err
	}
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
	lines := bytes.Split(source, []byte{'\n'})
	model, err := yamlNodeValue(document.Content[0], 0, &nodes, lines)
	if err != nil {
		return nil, err
	}
	data, err := json.Marshal(model)
	if err != nil {
		return nil, parseError("source.syntax", boundedMessage(err.Error()), 0, 0)
	}
	return data, nil
}

func yamlNodeValue(node *yaml.Node, containerDepth int, nodes *int, sourceLines [][]byte) (any, error) {
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
		containerDepth++
		if containerDepth > MaxNestingDepth {
			return nil, parseError("source.depth", fmt.Sprintf("YAML nesting must not exceed %d containers", MaxNestingDepth), node.Line, node.Column)
		}
		if node.Tag != "!!map" && node.Tag != "tag:yaml.org,2002:map" {
			return nil, parseError("source.tag", "custom YAML mapping tags are not supported", node.Line, node.Column)
		}
		if len(node.Content)%2 != 0 {
			return nil, parseError("source.syntax", "YAML mapping is incomplete", node.Line, node.Column)
		}
		result := make(map[string]any, len(node.Content)/2)
		for i := 0; i < len(node.Content); i += 2 {
			keyNode := node.Content[i]
			if keyNode.Anchor != "" || keyNode.Alias != nil || keyNode.Kind == yaml.AliasNode {
				return nil, parseError("source.alias", "YAML anchors and aliases are not supported", keyNode.Line, keyNode.Column)
			}
			if keyNode.Kind != yaml.ScalarNode {
				return nil, parseError("source.non_string_key", "YAML mapping keys must be strings", keyNode.Line, keyNode.Column)
			}
			keyKind, err := resolveYAMLScalarKind(keyNode, sourceLines)
			if err != nil {
				return nil, err
			}
			if keyKind != "string" {
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
			value, err := yamlNodeValue(node.Content[i+1], containerDepth, nodes, sourceLines)
			if err != nil {
				return nil, err
			}
			result[key] = value
		}
		return result, nil
	case yaml.SequenceNode:
		containerDepth++
		if containerDepth > MaxNestingDepth {
			return nil, parseError("source.depth", fmt.Sprintf("YAML nesting must not exceed %d containers", MaxNestingDepth), node.Line, node.Column)
		}
		if node.Tag != "!!seq" && node.Tag != "tag:yaml.org,2002:seq" {
			return nil, parseError("source.tag", "custom YAML sequence tags are not supported", node.Line, node.Column)
		}
		result := make([]any, len(node.Content))
		for i, child := range node.Content {
			value, err := yamlNodeValue(child, containerDepth, nodes, sourceLines)
			if err != nil {
				return nil, err
			}
			result[i] = value
		}
		return result, nil
	case yaml.ScalarNode:
		return yamlScalarValue(node, sourceLines)
	default:
		return nil, parseError("source.syntax", "unsupported YAML node", node.Line, node.Column)
	}
}

var (
	yamlIntegerPattern = regexp.MustCompile(`^(?:[+-]?[0-9]+|0o[0-7]+|0x[0-9a-fA-F]+)$`)
	yamlFloatPattern   = regexp.MustCompile(`^[+-]?(?:(?:[0-9]+\.[0-9]*|\.[0-9]+)(?:[eE][+-]?[0-9]+)?|[0-9]+[eE][+-]?[0-9]+)$`)
)

func yamlScalarValue(node *yaml.Node, sourceLines [][]byte) (any, error) {
	kind, err := resolveYAMLScalarKind(node, sourceLines)
	if err != nil {
		return nil, err
	}
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

func resolveYAMLScalarKind(node *yaml.Node, sourceLines [][]byte) (string, error) {
	if hasNonSpecificTag(node, sourceLines) {
		return "string", nil
	}
	if node.Style&yaml.TaggedStyle != 0 {
		switch node.Tag {
		case "!!str", "tag:yaml.org,2002:str":
			return "string", nil
		case "!!null", "tag:yaml.org,2002:null":
			if !isYAMLNull(node.Value) {
				return "", parseError("source.scalar", "explicit YAML null tag has an invalid value", node.Line, node.Column)
			}
			return "null", nil
		case "!!bool", "tag:yaml.org,2002:bool":
			if !isYAMLBoolean(node.Value) {
				return "", parseError("source.scalar", "explicit YAML boolean tag has an invalid value", node.Line, node.Column)
			}
			return "boolean", nil
		case "!!int", "tag:yaml.org,2002:int":
			if !yamlIntegerPattern.MatchString(node.Value) {
				return "", parseError("source.scalar", "explicit YAML integer tag has an invalid value", node.Line, node.Column)
			}
			return "number", nil
		case "!!float", "tag:yaml.org,2002:float":
			if !yamlIntegerPattern.MatchString(node.Value) && !yamlFloatPattern.MatchString(node.Value) && !isYAMLNonFinite(node.Value) {
				return "", parseError("source.scalar", "explicit YAML float tag has an invalid value", node.Line, node.Column)
			}
			return "number", nil
		default:
			return "", parseError("source.tag", "custom and non-JSON YAML tags are not supported", node.Line, node.Column)
		}
	}
	if node.Style != 0 {
		return "string", nil
	}
	value := node.Value
	if isYAMLNull(value) {
		return "null", nil
	}
	if isYAMLBoolean(value) {
		return "boolean", nil
	}
	if yamlIntegerPattern.MatchString(value) || yamlFloatPattern.MatchString(value) {
		return "number", nil
	}
	if isYAMLNonFinite(value) {
		return "number", nil
	}
	return "string", nil
}

func hasNonSpecificTag(node *yaml.Node, sourceLines [][]byte) bool {
	if node.Line < 1 || node.Line > len(sourceLines) || node.Column < 1 {
		return false
	}
	line := sourceLines[node.Line-1]
	offset := yamlColumnByteOffset(line, node.Column, node.Line == 1)
	if offset >= len(line) || line[offset] != '!' {
		return false
	}
	if offset+1 == len(line) {
		return true
	}
	switch line[offset+1] {
	case ' ', '\t', '\r', ',', '[', ']', '{', '}':
		return true
	default:
		return false
	}
}

func yamlColumnByteOffset(line []byte, column int, firstLine bool) int {
	offset := 0
	if firstLine && bytes.HasPrefix(line, []byte{0xef, 0xbb, 0xbf}) {
		offset = 3
	}
	for character := 1; character < column && offset < len(line); character++ {
		_, size := utf8.DecodeRune(line[offset:])
		offset += size
	}
	return offset
}

func isYAMLNull(value string) bool {
	switch value {
	case "", "~", "null", "Null", "NULL":
		return true
	default:
		return false
	}
}

func isYAMLBoolean(value string) bool {
	switch value {
	case "true", "True", "TRUE", "false", "False", "FALSE":
		return true
	default:
		return false
	}
}

func isYAMLNonFinite(value string) bool {
	switch value {
	case ".inf", ".Inf", ".INF", "+.inf", "+.Inf", "+.INF", "-.inf", "-.Inf", "-.INF", ".nan", ".NaN", ".NAN":
		return true
	default:
		return false
	}
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

func validateYAMLLineBreaks(source []byte) error {
	for offset := 0; offset < len(source); {
		r, size := utf8.DecodeRune(source[offset:])
		if r == '\r' {
			if offset+1 >= len(source) || source[offset+1] != '\n' {
				return parseErrorAtOffset("source.line_break", "YAML source must use LF or CRLF line breaks", source, int64(offset))
			}
			offset += 2
			continue
		}
		if r == '\u0085' || r == '\u2028' || r == '\u2029' {
			return parseErrorAtOffset("source.line_break", "YAML source contains a line-break character not supported by YAML 1.2.2", source, int64(offset))
		}
		offset += size
	}
	return nil
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

func parseErrorAtOffset(code, message string, source []byte, offset int64) *Error {
	if offset < 0 {
		offset = 0
	}
	if offset > int64(len(source)) {
		offset = int64(len(source))
	}
	prefix := source[:offset]
	line := 1 + bytes.Count(prefix, []byte{'\n'})
	lineStart := bytes.LastIndexByte(prefix, '\n') + 1
	linePrefix := prefix[lineStart:]
	if line == 1 {
		linePrefix = bytes.TrimPrefix(linePrefix, []byte{0xef, 0xbb, 0xbf})
	}
	column := utf8.RuneCount(linePrefix) + 1
	return parseError(code, message, line, column)
}

func parseError(code, message string, line, column int) *Error {
	return &Error{Code: code, Message: boundedMessage(message), Line: line, Column: column}
}

func boundedMessage(message string) string {
	const max = 512
	if utf8.RuneCountInString(message) <= max {
		return message
	}
	runes := []rune(message)
	return string(runes[:max])
}
