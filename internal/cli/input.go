package cli

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strconv"
	"unicode/utf8"

	"github.com/antaeusio/antaeus/evaluator"
	"github.com/antaeusio/antaeus/policy"
	"github.com/gowebpki/jcs"
)

func loadCanonicalInput(path string) (json.RawMessage, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("open: %w", err)
	}
	defer file.Close()
	source, err := io.ReadAll(io.LimitReader(file, evaluator.MaxInputBytes+1))
	if err != nil {
		return nil, fmt.Errorf("read: %w", err)
	}
	if len(source) > evaluator.MaxInputBytes {
		return nil, fmt.Errorf("must not exceed %d bytes", evaluator.MaxInputBytes)
	}
	if !utf8.Valid(source) {
		return nil, fmt.Errorf("must contain valid UTF-8")
	}
	input := bytes.TrimSpace(source)
	if len(input) == 0 {
		return nil, fmt.Errorf("must contain one canonical JSON object")
	}
	if err := validateInputUnicodeEscapes(input); err != nil {
		return nil, err
	}
	decoder := json.NewDecoder(bytes.NewReader(input))
	decoder.UseNumber()
	nodes := 0
	root, err := validateInputValue(decoder, 0, &nodes)
	if err != nil {
		return nil, err
	}
	if root != '{' {
		return nil, fmt.Errorf("must contain a JSON object")
	}
	var trailing any
	if err := decoder.Decode(&trailing); err != io.EOF {
		if err == nil {
			return nil, fmt.Errorf("must contain exactly one JSON object")
		}
		return nil, fmt.Errorf("trailing data: %w", err)
	}
	canonical, err := jcs.Transform(input)
	if err != nil {
		return nil, fmt.Errorf("canonicalize JSON: %w", err)
	}
	return json.RawMessage(canonical), nil
}

func validateInputValue(decoder *json.Decoder, containerDepth int, nodes *int) (json.Delim, error) {
	*nodes++
	if *nodes > policy.MaxParsedNodes {
		return 0, fmt.Errorf("must not exceed %d parsed nodes", policy.MaxParsedNodes)
	}
	token, err := decoder.Token()
	if err != nil {
		return 0, fmt.Errorf("decode JSON: %w", err)
	}
	delimiter, isDelimiter := token.(json.Delim)
	if !isDelimiter {
		return 0, nil
	}
	containerDepth++
	if containerDepth > policy.MaxNestingDepth {
		return 0, fmt.Errorf("nesting must not exceed %d containers", policy.MaxNestingDepth)
	}
	switch delimiter {
	case '{':
		seen := make(map[string]struct{})
		for decoder.More() {
			keyToken, err := decoder.Token()
			if err != nil {
				return 0, fmt.Errorf("decode object key: %w", err)
			}
			key, ok := keyToken.(string)
			if !ok {
				return 0, fmt.Errorf("object key must be a string")
			}
			*nodes++
			if *nodes > policy.MaxParsedNodes {
				return 0, fmt.Errorf("must not exceed %d parsed nodes", policy.MaxParsedNodes)
			}
			if _, exists := seen[key]; exists {
				return 0, fmt.Errorf("duplicate object key %q", key)
			}
			seen[key] = struct{}{}
			if _, err := validateInputValue(decoder, containerDepth, nodes); err != nil {
				return 0, err
			}
		}
	case '[':
		for decoder.More() {
			if _, err := validateInputValue(decoder, containerDepth, nodes); err != nil {
				return 0, err
			}
		}
	default:
		return 0, fmt.Errorf("unexpected JSON delimiter %q", delimiter)
	}
	if _, err := decoder.Token(); err != nil {
		return 0, fmt.Errorf("close JSON container: %w", err)
	}
	return delimiter, nil
}

func validateInputUnicodeEscapes(source []byte) error {
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
			code, ok := inputHexCodeUnit(source, i+2)
			if !ok {
				continue
			}
			if code >= 0xdc00 && code <= 0xdfff {
				return fmt.Errorf("contains an unpaired low surrogate")
			}
			if code >= 0xd800 && code <= 0xdbff {
				if i+12 > len(source) || source[i+6] != '\\' || source[i+7] != 'u' {
					return fmt.Errorf("contains an unpaired high surrogate")
				}
				low, valid := inputHexCodeUnit(source, i+8)
				if !valid || low < 0xdc00 || low > 0xdfff {
					return fmt.Errorf("contains an unpaired high surrogate")
				}
				i += 11
				continue
			}
			i += 5
		}
	}
	return nil
}

func inputHexCodeUnit(source []byte, start int) (uint16, bool) {
	if start+4 > len(source) {
		return 0, false
	}
	value, err := strconv.ParseUint(string(source[start:start+4]), 16, 16)
	return uint16(value), err == nil
}
