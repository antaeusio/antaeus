package schematest

import (
	"encoding/json"
	"os"
	"testing"
)

type TextCase struct {
	Name            string `json:"name"`
	Value           string `json:"value"`
	ECMANonBlank    bool   `json:"ecmaNonBlank"`
	UnicodeNonBlank bool   `json:"unicodeNonBlank"`
}

func TextCases(t *testing.T, path string) []TextCase {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var cases []TextCase
	if err := json.Unmarshal(data, &cases); err != nil {
		t.Fatal(err)
	}
	if len(cases) == 0 {
		t.Fatal("empty text conformance cases")
	}
	return cases
}
