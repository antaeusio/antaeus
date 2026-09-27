package host

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/antaeusio/antaeus/decision"
)

const secret = "host-test-secret"

func read(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile("../../" + path)
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

// antaeusProfile is the nli-server example moved to adapter 0.3.0 and pointed
// at a loopback test server.
func antaeusProfile(t *testing.T, endpoint string) string {
	t.Helper()
	var p map[string]any
	if err := json.Unmarshal([]byte(read(t, "examples/antaeus/nli-server.json")), &p); err != nil {
		t.Fatal(err)
	}
	e := p["spec"].(map[string]any)["evaluators"].([]any)[0].(map[string]any)
	e["adapter"].(map[string]any)["version"] = "0.3.0"
	e["parameters"] = map[string]any{"endpoint": endpoint}
	out, _ := json.Marshal(p)
	return string(out)
}

func server(t *testing.T, calls *atomic.Int32) *httptest.Server {
	t.Helper()
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		if r.Header.Get("Authorization") != "Bearer "+secret {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		_, _ = w.Write([]byte(`{"model":"antaeus-local","answers":{"prohibited-item":{"type":"noul","noul":0.95},"complete-listing":{"type":"noul","noul":0.1}}}`))
	}))
	t.Cleanup(s.Close)
	return s
}

func request(t *testing.T, endpoint string, mutate func(map[string]any)) []byte {
	t.Helper()
	r := map[string]any{
		"policy":        read(t, "examples/marketplace/listing-policy.yaml"),
		"policyFormat":  "yaml",
		"profile":       antaeusProfile(t, endpoint),
		"profileFormat": "json",
		"input":         map[string]any{"title": "Luxury watch, 1:1 replica"},
		"correlationId": "host-test",
		"credentials":   []map[string]string{{"adapterId": "io.antaeus.systemone", "slot": "antaeus-api-key", "value": secret}},
	}
	if mutate != nil {
		mutate(r)
	}
	out, _ := json.Marshal(r)
	return out
}

func response(t *testing.T, raw []byte) Response {
	t.Helper()
	if strings.Contains(string(raw), secret) {
		t.Fatalf("response contains the credential: %s", raw)
	}
	var r Response
	if err := json.Unmarshal(raw, &r); err != nil {
		t.Fatalf("response is not JSON: %s", raw)
	}
	return r
}

func TestValidate(t *testing.T) {
	raw, _ := json.Marshal(ValidateRequest{Policy: read(t, "examples/marketplace/listing-policy.yaml"), PolicyFormat: "yaml"})
	r := response(t, Validate(raw))
	if !r.OK || r.Policy == nil || r.Policy.Name != "marketplace-listing" || r.Policy.Rules != 2 || !strings.HasPrefix(r.Policy.Digest, "sha256:") {
		t.Fatalf("response = %+v error = %+v", r, r.Error)
	}
	for name, c := range map[string]struct{ request, code string }{
		"unknown field":  {`{"policy":"x","policyFormat":"yaml","extra":1}`, CodeRequestInvalid},
		"bad format":     {`{"policy":"x","policyFormat":"toml"}`, CodeRequestInvalid},
		"trailing data":  {`{"policy":"x","policyFormat":"yaml"} {}`, CodeRequestInvalid},
		"invalid policy": {`{"policy":"not: a policy","policyFormat":"yaml"}`, CodePolicyInvalid},
	} {
		r := response(t, Validate([]byte(c.request)))
		if r.OK || r.Error == nil || r.Error.Code != c.code || r.Policy != nil {
			t.Fatalf("%s: response = %+v error = %+v", name, r, r.Error)
		}
	}
}

func TestEvaluateReturnsDecision(t *testing.T) {
	var calls atomic.Int32
	s := server(t, &calls)
	r := response(t, Evaluate(context.Background(), request(t, s.URL, nil), Registry()))
	if !r.OK || r.Decision == nil || r.Decision.Outcome != decision.OutcomeDeny || calls.Load() != 1 {
		t.Fatalf("response = %+v error = %+v, calls = %d", r, r.Error, calls.Load())
	}
	if r.Decision.Evaluator == nil || r.Decision.Evaluator.AdapterVersion != "0.3.0" {
		t.Fatalf("evaluator = %+v", r.Decision.Evaluator)
	}
}

func TestEvaluateRejectsConfigurationBeforeEvaluation(t *testing.T) {
	var calls atomic.Int32
	s := server(t, &calls)
	for _, c := range []struct {
		name   string
		mutate func(map[string]any)
		code   string
	}{
		{"unknown field", func(r map[string]any) { r["extra"] = true }, CodeRequestInvalid},
		{"bad profile format", func(r map[string]any) { r["profileFormat"] = "toml" }, CodeRequestInvalid},
		{"bad policy format", func(r map[string]any) { r["policyFormat"] = "toml" }, CodeRequestInvalid},
		{"invalid policy", func(r map[string]any) { r["policy"] = "nope" }, CodePolicyInvalid},
		{"invalid profile", func(r map[string]any) { r["profile"] = "{}" }, CodeProfileInvalid},
		{"input not an object", func(r map[string]any) { r["input"] = []int{1} }, CodeInputInvalid},
		{"missing credential", func(r map[string]any) { r["credentials"] = []any{} }, CodeCredentialMissing},
		{"empty credential", func(r map[string]any) {
			r["credentials"] = []map[string]string{{"adapterId": "io.antaeus.systemone", "slot": "antaeus-api-key", "value": ""}}
		}, CodeCredentialMissing},
		{"repeated credential", func(r map[string]any) {
			c := map[string]string{"adapterId": "io.antaeus.systemone", "slot": "antaeus-api-key", "value": secret}
			r["credentials"] = []map[string]string{c, c}
		}, CodeRequestInvalid},
		{"fixture adapter", func(r map[string]any) {
			r["profile"] = read(t, "contracts/examples/v0alpha1/evaluator-profile/quickstart-fixture.json")
		}, CodeAdapterNotInstalled},
		{"adapter rejects evaluator", func(r map[string]any) {
			r["profile"] = strings.Replace(antaeusProfile(t, s.URL), `"provider":"antaeus"`, `"provider":"drex"`, 1)
		}, CodeAdapterNotInstalled},
	} {
		t.Run(c.name, func(t *testing.T) {
			r := response(t, Evaluate(context.Background(), request(t, s.URL, c.mutate), Registry()))
			if r.OK || r.Error == nil || r.Error.Code != c.code || r.Decision != nil {
				t.Fatalf("response = %+v error = %+v", r, r.Error)
			}
		})
	}
	if calls.Load() != 0 {
		t.Fatalf("server called %d times", calls.Load())
	}
}

func TestEvaluateIgnoresCredentialsOffTheRoute(t *testing.T) {
	var calls atomic.Int32
	s := server(t, &calls)
	r := response(t, Evaluate(context.Background(), request(t, s.URL, func(r map[string]any) {
		r["credentials"] = append(r["credentials"].([]map[string]string), map[string]string{"adapterId": "io.antaeus.openai", "slot": "openai-api-key", "value": "unused"})
	}), Registry()))
	if !r.OK || r.Decision.Outcome != decision.OutcomeDeny {
		t.Fatalf("response = %+v error = %+v", r, r.Error)
	}
}

func TestEvaluateHonorsTheHostDeadline(t *testing.T) {
	var calls atomic.Int32
	s := server(t, &calls)
	past := time.Now().Add(-time.Second).UnixMilli()
	r := response(t, Evaluate(context.Background(), request(t, s.URL, func(r map[string]any) { r["deadlineUnixMs"] = past }), Registry()))
	if r.OK || r.Error == nil || r.Error.Code != CodeDeadlineExceeded || calls.Load() != 0 {
		t.Fatalf("response = %+v error = %+v, calls = %d", r, r.Error, calls.Load())
	}
}

func TestEvaluateRejectsOversizedRequests(t *testing.T) {
	r := response(t, Evaluate(context.Background(), make([]byte, MaxRequestBytes+1), Registry()))
	if r.OK || r.Error.Code != CodeRequestInvalid {
		t.Fatalf("response = %+v error = %+v", r, r.Error)
	}
}

func TestEvaluateWithoutCredentialSlots(t *testing.T) {
	var calls atomic.Int32
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		if r.Header.Get("Authorization") != "" {
			t.Error("credential sent without a slot")
		}
		_, _ = w.Write([]byte(`{"model":"antaeus-local","answers":{"prohibited-item":{"type":"noul","noul":0.05},"complete-listing":{"type":"noul","noul":0.9}}}`))
	}))
	defer s.Close()
	r := response(t, Evaluate(context.Background(), request(t, s.URL, func(r map[string]any) {
		var p map[string]any
		_ = json.Unmarshal([]byte(r["profile"].(string)), &p)
		p["spec"].(map[string]any)["credentialSlots"] = []any{}
		delete(p["spec"].(map[string]any)["evaluators"].([]any)[0].(map[string]any), "credentialSlot")
		out, _ := json.Marshal(p)
		r["profile"] = string(out)
		r["credentials"] = []any{}
	}), Registry()))
	if !r.OK || r.Decision.Outcome != decision.OutcomeAllow || calls.Load() != 1 {
		t.Fatalf("response = %+v error = %+v", r, r.Error)
	}
}
