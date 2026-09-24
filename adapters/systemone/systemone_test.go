package systemone

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/antaeusio/antaeus/decision"
	"github.com/antaeusio/antaeus/evaluator"
	"github.com/antaeusio/antaeus/evaluator/localbinding"
	"github.com/antaeusio/antaeus/evaluator/profile"
	"github.com/antaeusio/antaeus/evaluator/runner"
	"github.com/antaeusio/antaeus/internal/jsonvalue"
	"github.com/antaeusio/antaeus/internal/remote"
	"github.com/antaeusio/antaeus/policy"
)

const testKey = "clm-test-secret"

func loadProfile(t *testing.T, name string) profile.Artifact {
	t.Helper()
	p, err := profile.LoadFile("../../examples/clm/" + name)
	if err != nil {
		t.Fatal(err)
	}
	return p
}

func request(t *testing.T) evaluator.Request {
	t.Helper()
	return evaluator.Request{
		PolicyName:     "marketplace-listing",
		PolicyDigest:   "sha256:" + strings.Repeat("a", 64),
		ProfileDigest:  "sha256:" + strings.Repeat("b", 64),
		CanonicalInput: json.RawMessage(`{"description":"Looks identical to the original brand.","title":"Luxury watch, 1:1 replica"}`),
		Rules: []evaluator.Rule{
			{ID: "prohibited-item", When: "The listing offers weapons, counterfeit goods, or prescription drugs."},
			{ID: "complete-listing", When: "The listing clearly describes a legal item with its condition and price."},
		},
		Deadline:      time.Now().Add(time.Minute),
		CorrelationID: "test-correlation",
	}
}

// serve starts a fake System One server and returns a configuration whose
// endpoint points at it (loopback http is permitted).
func serve(t *testing.T, handler http.HandlerFunc) (runner.Adapter, runner.Configuration) {
	t.Helper()
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)
	entry := loadProfile(t, "confidence-gated.json").Spec.Evaluators[0]
	entry.Parameters = map[string]any{"endpoint": server.URL}
	return registration(remote.NewClient()), runner.Configuration{Evaluator: entry}
}

func answers(pairs ...any) string {
	var b strings.Builder
	b.WriteString(`{"model":"clm-latest","answers":{`)
	for i := 0; i < len(pairs); i += 2 {
		if i > 0 {
			b.WriteString(",")
		}
		fmt.Fprintf(&b, `%q:{"type":"noul","noul":%v}`, pairs[i], pairs[i+1])
	}
	b.WriteString(`},"usage":{"billing_units":2,"input_tokens":40,"output_tokens":0}}`)
	return b.String()
}

func evaluationError(t *testing.T, err error) *evaluator.Error {
	t.Helper()
	var failure *evaluator.Error
	if !errors.As(err, &failure) {
		t.Fatalf("error = %v, want *evaluator.Error", err)
	}
	return failure
}

func TestExampleProfilesValidate(t *testing.T) {
	for _, name := range []string{"confidence-gated.json", "openai-fallback.json"} {
		p := loadProfile(t, name)
		validators := profile.ParameterValidators{Identity: parameterValidator{}}
		for _, e := range p.Spec.Evaluators {
			if e.Adapter == Identity {
				if err := ValidateEvaluator(e); err != nil {
					t.Fatalf("%s: %v", name, err)
				}
			} else {
				validators[e.Adapter] = acceptAll{}
			}
		}
		if err := p.ValidateParameters(validators); err != nil {
			t.Fatalf("%s: %v", name, err)
		}
	}
}

type acceptAll struct{}

func (acceptAll) ValidateParameters(json.RawMessage) error { return nil }

func TestEvaluateSendsNoulQuestionsAndNormalizesProbabilities(t *testing.T) {
	var body map[string]any
	var header http.Header
	var path string
	adapter, config := serve(t, func(w http.ResponseWriter, r *http.Request) {
		path, header = r.URL.Path, r.Header.Clone()
		payload, _ := io.ReadAll(r.Body)
		if err := json.Unmarshal(payload, &body); err != nil {
			t.Errorf("request is not JSON: %v", err)
		}
		_, _ = io.WriteString(w, answers("complete-listing", 0.2, "prohibited-item", 0.93))
	})
	result, err := adapter.Evaluate(context.Background(), request(t), config)
	if err != nil {
		t.Fatal(err)
	}
	if err := evaluator.ValidateResult(request(t), result); err != nil {
		t.Fatal(err)
	}
	first, second := result.RuleResults[0], result.RuleResults[1]
	if first.RuleID != "prohibited-item" || first.Status != decision.RuleMatched || *first.Confidence != 0.93 ||
		second.Status != decision.RuleNotMatched || *second.Confidence != 0.8 {
		t.Fatalf("unexpected results %+v %+v", first, second)
	}
	m := result.Metadata
	if m.AdapterID != AdapterID || m.Provider != ProviderCLM || m.Model != "clm-latest" || m.Mode != evaluator.ModeSemantic || m.Synthetic {
		t.Fatalf("unexpected metadata %+v", m)
	}
	if path != "/v1/systemone" || header.Get("Authorization") != "" {
		t.Fatalf("path %q authorization %q", path, header.Get("Authorization"))
	}
	if body["model"] != "clm-latest" {
		t.Fatalf("model %v", body["model"])
	}
	state := body["state"].(map[string]any)
	if state["title"] != "Luxury watch, 1:1 replica" {
		t.Fatalf("state %v", state)
	}
	questions := body["questions"].(map[string]any)
	q := questions["prohibited-item"].(map[string]any)
	if len(questions) != 2 || q["type"] != "noul" || q["instructions"] != request(t).Rules[0].When || len(q) != 2 {
		t.Fatalf("questions %v", questions)
	}
	encoded, _ := json.Marshal(body)
	for _, forbidden := range []string{`"deny"`, `"allow"`, `"outcome"`, "marketplace-listing"} {
		if strings.Contains(string(encoded), forbidden) {
			t.Fatalf("request leaks %s: %s", forbidden, encoded)
		}
	}
}

func TestProbabilityBoundaries(t *testing.T) {
	cases := []struct {
		p          float64
		status     decision.RuleStatus
		confidence float64
	}{
		{1, decision.RuleMatched, 1}, {0.5000001, decision.RuleMatched, 0.5000001},
		{0.5, decision.RuleIndeterminate, 0.5}, {0.4999999, decision.RuleNotMatched, 0.5000001}, {0, decision.RuleNotMatched, 1},
	}
	for _, c := range cases {
		results, _, err := parseResponse([]byte(answers("prohibited-item", c.p, "complete-listing", 0.1)), request(t).Rules)
		if err != nil {
			t.Fatal(err)
		}
		if results[0].Status != c.status || fmt.Sprintf("%.7f", *results[0].Confidence) != fmt.Sprintf("%.7f", c.confidence) {
			t.Fatalf("p=%v: %+v", c.p, results[0])
		}
	}
}

func TestEvaluateRejectsInvalidProviderOutput(t *testing.T) {
	cases := map[string]struct {
		body string
		code string
	}{
		"not json":         {`{`, "systemone.response_malformed"},
		"no answers":       {`{"model":"clm-latest"}`, "systemone.response_malformed"},
		"missing rule":     {answers("prohibited-item", 0.9), "systemone.output_invalid"},
		"unknown rule":     {answers("prohibited-item", 0.9, "other", 0.1), "systemone.output_invalid"},
		"extra rule":       {answers("prohibited-item", 0.9, "complete-listing", 0.1, "other", 0.1), "systemone.output_invalid"},
		"wrong type":       {`{"answers":{"prohibited-item":{"type":"choice","noul":0.9},"complete-listing":{"type":"noul","noul":0.1}}}`, "systemone.output_invalid"},
		"missing noul":     {`{"answers":{"prohibited-item":{"type":"noul"},"complete-listing":{"type":"noul","noul":0.1}}}`, "systemone.output_invalid"},
		"string noul":      {`{"answers":{"prohibited-item":{"type":"noul","noul":"0.9"},"complete-listing":{"type":"noul","noul":0.1}}}`, "systemone.output_invalid"},
		"above one":        {answers("prohibited-item", 1.2, "complete-listing", 0.1), "systemone.output_invalid"},
		"negative":         {answers("prohibited-item", -0.1, "complete-listing", 0.1), "systemone.output_invalid"},
		"oversized":        {`{"pad":"` + strings.Repeat("x", MaxResponseBytes) + `"}`, "systemone.response_too_large"},
		"answers is array": {`{"answers":[]}`, "systemone.response_malformed"},
		"duplicate rule":   {`{"answers":{"prohibited-item":{"type":"noul","noul":0.9},"prohibited-item":{"type":"noul","noul":0.1},"complete-listing":{"type":"noul","noul":0.1}}}`, "systemone.response_malformed"},
		"duplicate field":  {`{"answers":{"prohibited-item":{"type":"noul","noul":0.9,"noul":0.1},"complete-listing":{"type":"noul","noul":0.1}}}`, "systemone.output_invalid"},
		"case variant":     {`{"Answers":{"prohibited-item":{"type":"noul","noul":0.9},"complete-listing":{"type":"noul","noul":0.1}}}`, "systemone.response_malformed"},
		"case field":       {`{"answers":{"prohibited-item":{"TYPE":"noul","noul":0.9},"complete-listing":{"type":"noul","noul":0.1}}}`, "systemone.output_invalid"},
		"null noul":        {`{"answers":{"prohibited-item":{"type":"noul","noul":null},"complete-listing":{"type":"noul","noul":0.1}}}`, "systemone.output_invalid"},
		"extra field":      {`{"answers":{"prohibited-item":{"type":"noul","noul":0.9,"confidence":1},"complete-listing":{"type":"noul","noul":0.1}}}`, "systemone.output_invalid"},
		"trailing data":    {answers("prohibited-item", 0.9, "complete-listing", 0.1) + `{}`, "systemone.response_malformed"},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			adapter, config := serve(t, func(w http.ResponseWriter, _ *http.Request) { _, _ = io.WriteString(w, c.body) })
			_, err := adapter.Evaluate(context.Background(), request(t), config)
			if failure := evaluationError(t, err); failure.Code != c.code || failure.Retryable {
				t.Fatalf("failure = %+v, want non-retryable %s", failure, c.code)
			}
		})
	}
}

func TestEvaluateClassifiesHTTPStatus(t *testing.T) {
	cases := []struct {
		status    int
		code      string
		retryable bool
	}{
		{401, "systemone.credential_rejected", false}, {403, "systemone.credential_rejected", false},
		{400, "systemone.request_rejected", false}, {404, "systemone.request_rejected", false}, {422, "systemone.request_rejected", false},
		{408, "evaluator.timeout", true}, {429, "evaluator.throttled", true},
		{500, "evaluator.unavailable", true}, {502, "evaluator.unavailable", true}, {503, "evaluator.unavailable", true}, {504, "evaluator.unavailable", true},
		{201, "systemone.response_malformed", false}, {302, "systemone.unexpected_status", false}, {418, "systemone.unexpected_status", false},
	}
	for _, c := range cases {
		t.Run(fmt.Sprint(c.status), func(t *testing.T) {
			var redirected atomic.Bool
			adapter, config := serve(t, func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path == "/elsewhere" {
					redirected.Store(true)
					return
				}
				if c.status == 302 {
					w.Header().Set("Location", "/elsewhere")
				}
				w.WriteHeader(c.status)
				_, _ = io.WriteString(w, `{"detail":"invalid API key clm-leak"}`)
			})
			_, err := adapter.Evaluate(context.Background(), request(t), config)
			failure := evaluationError(t, err)
			if failure.Code != c.code || failure.Retryable != c.retryable || strings.Contains(failure.Error(), "clm-leak") || redirected.Load() {
				t.Fatalf("failure = %+v redirected=%v", failure, redirected.Load())
			}
		})
	}
}

func TestCredentialHandling(t *testing.T) {
	var got string
	adapter, config := serve(t, func(w http.ResponseWriter, r *http.Request) {
		got = r.Header.Get("Authorization")
		_, _ = io.WriteString(w, answers("prohibited-item", 0.9, "complete-listing", 0.1))
	})
	slot := CredentialSlot
	config.Evaluator.CredentialSlot = &slot
	config.Credential = []byte(testKey)
	if _, err := adapter.Evaluate(context.Background(), request(t), config); err != nil {
		t.Fatal(err)
	}
	if got != "Bearer "+testKey {
		t.Fatalf("authorization %q", got)
	}
	for name, credential := range map[string][]byte{"missing": nil, "newline": []byte(testKey + "\n")} {
		config.Credential = credential
		_, err := adapter.Evaluate(context.Background(), request(t), config)
		if failure := evaluationError(t, err); failure.Retryable || !strings.HasPrefix(failure.Code, "systemone.credential_") || strings.Contains(failure.Error(), testKey) {
			t.Fatalf("%s: %+v", name, failure)
		}
	}
}

func TestEndpointValidation(t *testing.T) {
	valid := []string{"https://clm.example.com", "https://clm.example.com:8443/base/", "http://127.0.0.1:8700", "http://localhost:8700",
		"http://[::1]:8700", "http://127.8.9.10", "HTTPS://clm.example.com", "http://LOCALHOST:8700", "http://[::ffff:127.0.0.1]:8700"}
	invalid := []string{"", "clm.example.com", "http://clm.example.com:8700", "http://10.0.0.5:8700", "ftp://clm.example.com",
		"https://user:pass@clm.example.com", "https://clm.example.com?x=1", "https://clm.example.com/?", "https://clm.example.com/#frag",
		"https://", "http://localhost.evil.example", "https:" + strings.Repeat("a", MaxEndpointBytes),
		"http://localhost.:8700", "http://[::1%25lo0]:8700", "http://[fe80::1]:8700", "http://0.0.0.0:8700"}
	for _, e := range valid {
		if _, err := endpointURL(e); err != nil {
			t.Errorf("%q rejected: %v", e, err)
		}
	}
	for _, e := range invalid {
		if _, err := endpointURL(e); err == nil {
			t.Errorf("%q accepted", e)
		}
	}
	if got, _ := endpointURL("https://clm.example.com/base/"); got != "https://clm.example.com/base/v1/systemone" {
		t.Fatalf("target %q", got)
	}
	if err := (parameterValidator{}).ValidateParameters(json.RawMessage(`{"endpoint":"http://127.0.0.1:8700","temperature":1}`)); err == nil {
		t.Fatal("extra parameter accepted")
	}
}

func TestEvaluateRejectsConfigurationBeforeNetwork(t *testing.T) {
	var calls atomic.Int32
	adapter, base := serve(t, func(http.ResponseWriter, *http.Request) { calls.Add(1) })
	mutate := map[string]func(*runner.Configuration){
		"provider": func(c *runner.Configuration) { v := "typesafe"; c.Evaluator.Provider = &v },
		"no model": func(c *runner.Configuration) { c.Evaluator.Model = nil },
		"revision": func(c *runner.Configuration) { v := "x"; c.Evaluator.ModelRevision = &v },
		"template": func(c *runner.Configuration) {
			c.Evaluator.InstructionTemplate = &profile.InstructionTemplate{Digest: "sha256:" + strings.Repeat("0", 64)}
		},
		"slot": func(c *runner.Configuration) { v := "other"; c.Evaluator.CredentialSlot = &v },
		"remote http": func(c *runner.Configuration) {
			c.Evaluator.Parameters = map[string]any{"endpoint": "http://clm.example.com"}
		},
		"no endpoint":   func(c *runner.Configuration) { c.Evaluator.Parameters = map[string]any{} },
		"adapter":       func(c *runner.Configuration) { c.Evaluator.Adapter.Version = "9.9.9" },
		"too many rule": func(c *runner.Configuration) {},
	}
	for name, change := range mutate {
		t.Run(name, func(t *testing.T) {
			config := base
			config.Evaluator.Parameters = map[string]any{"endpoint": base.Evaluator.Parameters["endpoint"]}
			change(&config)
			req := request(t)
			if name == "too many rule" {
				req.Rules = nil
				for i := 0; i < 256; i++ {
					req.Rules = append(req.Rules, evaluator.Rule{ID: fmt.Sprintf("rule-%03d", i), When: strings.Repeat("é", 16_000)})
				}
			}
			_, err := adapter.Evaluate(context.Background(), req, config)
			if failure := evaluationError(t, err); failure.Retryable || !strings.HasPrefix(failure.Code, "systemone.") {
				t.Fatalf("failure = %+v", failure)
			}
		})
	}
	if calls.Load() != 0 {
		t.Fatalf("provider called %d times", calls.Load())
	}
}

func TestEvaluateTransportFailures(t *testing.T) {
	t.Run("deadline", func(t *testing.T) {
		release := make(chan struct{})
		adapter, config := serve(t, func(_ http.ResponseWriter, r *http.Request) {
			_, _ = io.Copy(io.Discard, r.Body)
			select {
			case <-r.Context().Done():
			case <-release:
			}
		})
		t.Cleanup(func() { close(release) })
		ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
		defer cancel()
		_, err := adapter.Evaluate(ctx, request(t), config)
		if failure := evaluationError(t, err); failure.Code != "evaluator.timeout" || !failure.Retryable {
			t.Fatalf("failure = %+v", failure)
		}
	})
	t.Run("connection refused", func(t *testing.T) {
		adapter, config := serve(t, func(http.ResponseWriter, *http.Request) {})
		server := httptest.NewServer(http.NotFoundHandler())
		config.Evaluator.Parameters = map[string]any{"endpoint": server.URL}
		server.Close()
		_, err := adapter.Evaluate(context.Background(), request(t), config)
		if failure := evaluationError(t, err); failure.Code != "evaluator.unavailable" || !failure.Retryable {
			t.Fatalf("failure = %+v", failure)
		}
	})
	t.Run("untrusted certificate", func(t *testing.T) {
		adapter, config := serve(t, func(http.ResponseWriter, *http.Request) {})
		server := httptest.NewTLSServer(http.NotFoundHandler())
		defer server.Close()
		config.Evaluator.Parameters = map[string]any{"endpoint": server.URL}
		_, err := adapter.Evaluate(context.Background(), request(t), config)
		if failure := evaluationError(t, err); failure.Code != "systemone.tls_failed" || failure.Retryable {
			t.Fatalf("failure = %+v", failure)
		}
	})
}

func runnerInput(t *testing.T, p profile.Artifact) runner.Input {
	t.Helper()
	artifact, err := policy.LoadFile("../../examples/marketplace/listing-policy.yaml")
	if err != nil {
		t.Fatal(err)
	}
	input, err := jsonvalue.CanonicalObject([]byte(`{"title":"Luxury watch, 1:1 replica"}`))
	if err != nil {
		t.Fatal(err)
	}
	return runner.Input{Policy: artifact, Profile: p, CanonicalInput: input, CorrelationID: "runner-test", Credentials: &localbinding.Credentials{}}
}

func pointProfile(t *testing.T, name, endpoint string) profile.Artifact {
	t.Helper()
	p := loadProfile(t, name)
	for i := range p.Spec.Evaluators {
		if p.Spec.Evaluators[i].Adapter == Identity {
			p.Spec.Evaluators[i].Parameters = map[string]any{"endpoint": endpoint}
		}
	}
	return p
}

func TestRunnerConfidenceGating(t *testing.T) {
	cases := map[string]struct {
		prohibited, complete float64
		outcome              decision.Outcome
	}{
		"confident deny":          {0.95, 0.05, decision.OutcomeDeny},
		"confident allow":         {0.05, 0.9, decision.OutcomeAllow},
		"unsure becomes failure":  {0.3, 0.6, decision.OutcomeFailure},
		"matched deny still wins": {0.9, 0.55, decision.OutcomeDeny},
		"confident none matched":  {0.05, 0.1, decision.OutcomeReview},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				_, _ = io.WriteString(w, answers("prohibited-item", c.prohibited, "complete-listing", c.complete))
			}))
			defer server.Close()
			d, err := runner.Run(context.Background(), runnerInput(t, pointProfile(t, "confidence-gated.json", server.URL)), runner.Registry{Identity: registration(remote.NewClient())})
			if err != nil {
				t.Fatal(err)
			}
			if d.Outcome != c.outcome || d.Evaluator.Provider != ProviderCLM {
				encoded, _ := json.Marshal(d)
				t.Fatalf("outcome %s, want %s: %s", d.Outcome, c.outcome, encoded)
			}
		})
	}
}

func TestRunnerFallsBackToSecondEvaluatorWhenServerIsDown(t *testing.T) {
	down := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusBadGateway) }))
	defer down.Close()
	p := pointProfile(t, "openai-fallback.json", down.URL)
	var fallbackCalls atomic.Int32
	fallback := runner.Adapter{
		Mode: profile.ModeSemantic, Protocol: Protocol, Capabilities: []string{"json-input", "structured-rule-results"},
		Parameters: acceptAll{},
		Evaluate: func(_ context.Context, r evaluator.Request, c runner.Configuration) (evaluator.Result, error) {
			fallbackCalls.Add(1)
			results := make([]evaluator.RuleResult, len(r.Rules))
			for i, rule := range r.Rules {
				status := decision.RuleNotMatched
				if rule.ID == "prohibited-item" {
					status = decision.RuleMatched
				}
				results[i] = evaluator.RuleResult{RuleID: rule.ID, Status: status, ReasonCodes: []string{"openai." + string(status)}}
			}
			return evaluator.Result{RuleResults: results, Metadata: evaluator.Metadata{AdapterID: c.Evaluator.Adapter.ID, AdapterVersion: c.Evaluator.Adapter.Version, Mode: evaluator.ModeSemantic, Provider: "openai", Model: *c.Evaluator.Model}}, nil
		},
	}
	openaiIdentity := p.Spec.Evaluators[1].Adapter
	input := runnerInput(t, p)
	credentials, err := localbinding.Preflight(p, localbinding.Artifact{APIVersion: "config.antaeus.io/v0alpha1", Kind: "LocalSecretBindings", SecretBindings: map[string]map[string]localbinding.Reference{
		openaiIdentity.ID: {"openai-api-key": {Source: "environment", Name: "OPENAI_API_KEY"}},
	}}, localbinding.EnvironmentFunc(func(string) (string, bool) { return "sk-test", true }))
	if err != nil {
		t.Fatal(err)
	}
	defer credentials.Clear()
	input.Credentials = credentials
	d, err := runner.Run(context.Background(), input, runner.Registry{Identity: registration(remote.NewClient()), openaiIdentity: fallback})
	if err != nil {
		t.Fatal(err)
	}
	if d.Outcome != decision.OutcomeDeny || !d.Evaluator.Fallback || fallbackCalls.Load() != 1 || d.Evaluator.Provider != "openai" {
		encoded, _ := json.Marshal(d)
		t.Fatalf("unexpected decision %s", encoded)
	}
}
