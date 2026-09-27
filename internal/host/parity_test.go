package host

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"reflect"
	"strings"
	"testing"
)

// The parity cases run through the native engine here and through the
// WebAssembly build in scripts/check-wasm.mjs; both compare with the same
// expected Decisions. Regenerate them with:
//
//	go test ./internal/host -run TestParityCases -update-parity
var updateParity = flag.Bool("update-parity", false, "rewrite testdata/parity/expected.json")

type parityCases struct {
	Policy        string          `json:"policy"`
	Profile       json.RawMessage `json:"profile"`
	Input         json.RawMessage `json:"input"`
	CorrelationID string          `json:"correlationId"`
	Cases         []struct {
		Name    string             `json:"name"`
		Status  int                `json:"status"`
		Answers map[string]float64 `json:"answers"`
	} `json:"cases"`
}

// normalizeDecision removes the only fields that legitimately differ between
// hosts: the profile digest, which covers the test server's endpoint, and
// measured attempt latency.
func normalizeDecision(t *testing.T, decision json.RawMessage) map[string]any {
	t.Helper()
	var d map[string]any
	if err := json.Unmarshal(decision, &d); err != nil {
		t.Fatal(err)
	}
	if evaluator, ok := d["evaluator"].(map[string]any); ok {
		delete(evaluator, "profileDigest")
	}
	if extensions, ok := d["extensions"].(map[string]any); ok {
		if trace, ok := extensions["io.antaeus.execution"].(map[string]any); ok {
			if attempts, ok := trace["attempts"].([]any); ok {
				for _, attempt := range attempts {
					delete(attempt.(map[string]any), "latencyMs")
				}
			}
		}
	}
	return d
}

func TestParityCases(t *testing.T) {
	raw, err := os.ReadFile("testdata/parity/cases.json")
	if err != nil {
		t.Fatal(err)
	}
	var suite parityCases
	if err := json.Unmarshal(raw, &suite); err != nil {
		t.Fatal(err)
	}
	got := map[string]map[string]any{}
	for _, c := range suite.Cases {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			if c.Status != http.StatusOK {
				w.WriteHeader(c.Status)
				return
			}
			parts := make([]string, 0, len(c.Answers))
			for id, p := range c.Answers {
				parts = append(parts, fmt.Sprintf(`%q:{"type":"noul","noul":%v}`, id, p))
			}
			_, _ = fmt.Fprintf(w, `{"model":"parity-model","answers":{%s}}`, strings.Join(parts, ","))
		}))
		profile := strings.Replace(string(suite.Profile), "ENDPOINT", server.URL, 1)
		request, _ := json.Marshal(map[string]any{
			"policy": suite.Policy, "policyFormat": "yaml", "profile": profile, "profileFormat": "json",
			"input": suite.Input, "correlationId": suite.CorrelationID, "credentials": []any{},
		})
		var r struct {
			OK       bool            `json:"ok"`
			Decision json.RawMessage `json:"decision"`
			Error    *Error          `json:"error"`
		}
		if err := json.Unmarshal(Evaluate(context.Background(), request, Registry()), &r); err != nil || !r.OK {
			t.Fatalf("%s: err = %v, error = %+v", c.Name, err, r.Error)
		}
		server.Close()
		got[c.Name] = normalizeDecision(t, r.Decision)
	}
	if *updateParity {
		out, _ := json.MarshalIndent(got, "", "  ")
		if err := os.WriteFile("testdata/parity/expected.json", append(out, '\n'), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	expectedRaw, err := os.ReadFile("testdata/parity/expected.json")
	if err != nil {
		t.Fatal(err)
	}
	var expected map[string]map[string]any
	if err := json.Unmarshal(expectedRaw, &expected); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got, expected) {
		out, _ := json.MarshalIndent(got, "", "  ")
		t.Fatalf("native Decisions differ from testdata/parity/expected.json:\n%s", out)
	}
}
