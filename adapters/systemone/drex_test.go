package systemone

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"sync/atomic"
	"testing"
	"time"

	"github.com/antaeusio/antaeus/decision"
	"github.com/antaeusio/antaeus/evaluator"
	"github.com/antaeusio/antaeus/evaluator/localbinding"
	"github.com/antaeusio/antaeus/evaluator/profile"
	"github.com/antaeusio/antaeus/evaluator/runner"
)

const drexTestKey = "nace_sk_test"

func drexFixture(t *testing.T, name string) []byte {
	t.Helper()
	body, err := os.ReadFile("testdata/drex/" + name)
	if err != nil {
		t.Fatal(err)
	}
	return body
}

func drexProfile(t *testing.T) profile.Artifact {
	t.Helper()
	p, err := profile.LoadFile("../../examples/drex/profile.json")
	if err != nil {
		t.Fatal(err)
	}
	return p
}

// serveDrex answers requests addressed to DrexEndpoint from a local TLS
// server, so the adapter runs against the real Drex URL and validation.
func serveDrex(t *testing.T, handler http.HandlerFunc) (runner.Adapter, runner.Configuration) {
	t.Helper()
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Host != "drex.nace.ai" || r.URL.Path != "/v1/systemone" {
			t.Errorf("request to %s%s", r.Host, r.URL.Path)
		}
		handler(w, r)
	}))
	t.Cleanup(server.Close)
	local, _ := url.Parse(server.URL)
	client := server.Client()
	transport := client.Transport.(*http.Transport).Clone()
	client.Transport = roundTripFunc(func(r *http.Request) (*http.Response, error) {
		r = r.Clone(r.Context())
		r.Host = r.URL.Host
		r.URL.Host = local.Host
		return transport.RoundTrip(r)
	})
	return registration(client, AdapterVersion), runner.Configuration{Evaluator: drexProfile(t).Spec.Evaluators[0], Credential: []byte(drexTestKey)}
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func drexRequest(t *testing.T) evaluator.Request {
	t.Helper()
	r := request(t)
	r.Rules = []evaluator.Rule{{ID: "described_item", When: "The item is described in detail, including its condition."}}
	return r
}

func TestDrexCapturedSuccess(t *testing.T) {
	var body struct {
		Model     string              `json:"model"`
		Questions map[string]question `json:"questions"`
	}
	var auth string
	adapter, config := serveDrex(t, func(w http.ResponseWriter, r *http.Request) {
		auth = r.Header.Get("Authorization")
		_ = json.NewDecoder(r.Body).Decode(&body)
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("X-Request-Id", "req_00000000000000000000000000000001")
		_, _ = w.Write(drexFixture(t, "success-200.json"))
	})
	result, err := adapter.Evaluate(context.Background(), drexRequest(t), config)
	if err != nil {
		t.Fatal(err)
	}
	if auth != "Bearer "+drexTestKey || body.Model != "drex-latest" || body.Questions["described_item"] != (question{Type: "noul", Instructions: "The item is described in detail, including its condition."}) {
		t.Fatalf("authorization %q body %+v", auth, body)
	}
	r := result.RuleResults[0]
	if r.Status != decision.RuleMatched || *r.Confidence != 0.9037 {
		t.Fatalf("result = %+v", r)
	}
	want := evaluator.Metadata{AdapterID: AdapterID, AdapterVersion: AdapterVersion, Mode: evaluator.ModeSemantic, Provider: ProviderDrex, Model: "drex-latest", RequestID: "req_00000000000000000000000000000001"}
	if result.Metadata != want {
		t.Fatalf("metadata = %+v", result.Metadata)
	}
}

func TestDrexErrorResponses(t *testing.T) {
	for _, c := range []struct {
		fixture    string
		status     int
		headers    map[string]string
		code       string
		retryable  bool
		retryAfter time.Duration
	}{
		{"credential-rejected-401.json", 401, nil, "systemone.credential_rejected", false, 0},
		{"request-rejected-422.json", 422, nil, "systemone.request_rejected", false, 0},
		{"throttled-429.json", 429, map[string]string{"Retry-After": "1", "Retry-After-Ms": "1000"}, "evaluator.throttled", true, time.Second},
		{"overloaded-529.json", 529, map[string]string{"Retry-After": "2", "Retry-After-Ms": "2000"}, "evaluator.unavailable", true, 2 * time.Second},
		// A non-retryable status never carries a wait, even with the header.
		{"credential-rejected-401.json", 401, map[string]string{"Retry-After": "5"}, "systemone.credential_rejected", false, 0},
	} {
		t.Run(c.fixture, func(t *testing.T) {
			adapter, config := serveDrex(t, func(w http.ResponseWriter, _ *http.Request) {
				for k, v := range c.headers {
					w.Header().Set(k, v)
				}
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(c.status)
				_, _ = w.Write(drexFixture(t, c.fixture))
			})
			_, err := adapter.Evaluate(context.Background(), drexRequest(t), config)
			failure := evaluationError(t, err)
			if failure.Code != c.code || failure.Retryable != c.retryable || failure.RetryAfter != c.retryAfter {
				t.Fatalf("failure = %+v", failure)
			}
		})
	}
}

func TestDrexRequiresItsEndpoint(t *testing.T) {
	for _, endpoint := range []string{"https://drex.nace.ai.example.com", "https://evil.example", "http://127.0.0.1:8080", "https://drex.nace.ai/proxy"} {
		e := drexProfile(t).Spec.Evaluators[0]
		e.Parameters = map[string]any{"endpoint": endpoint}
		if err := ValidateEvaluator(e); err == nil {
			t.Fatalf("endpoint %q accepted", endpoint)
		}
	}
	e := drexProfile(t).Spec.Evaluators[0]
	e.Parameters = map[string]any{"endpoint": DrexEndpoint + "/"}
	if err := ValidateEvaluator(e); err != nil {
		t.Fatal(err)
	}
	// Evaluation refuses a redirected endpoint before any request is sent.
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { calls.Add(1) }))
	defer server.Close()
	config := runner.Configuration{Evaluator: drexProfile(t).Spec.Evaluators[0], Credential: []byte(drexTestKey)}
	config.Evaluator.Parameters = map[string]any{"endpoint": server.URL}
	_, err := Registration().Evaluate(context.Background(), drexRequest(t), config)
	if failure := evaluationError(t, err); failure.Code != "systemone.configuration_invalid" || calls.Load() != 0 {
		t.Fatalf("failure = %+v, calls = %d", failure, calls.Load())
	}
}

func TestDrexRunnerHonorsRetryAfter(t *testing.T) {
	var calls atomic.Int32
	adapter, _ := serveDrex(t, func(w http.ResponseWriter, _ *http.Request) {
		if calls.Add(1) == 1 {
			w.Header().Set("Retry-After-Ms", "300")
			w.WriteHeader(429)
			_, _ = w.Write(drexFixture(t, "throttled-429.json"))
			return
		}
		_, _ = w.Write([]byte(`{"model":"drex-latest","answers":{"prohibited-item":{"type":"noul","noul":0.95},"complete-listing":{"type":"noul","noul":0.9}}}`))
	})
	p := drexProfile(t)
	bindings, err := localbinding.Parse([]byte(`{"apiVersion":"config.antaeus.io/v0alpha1","kind":"LocalSecretBindings","secretBindings":{"io.antaeus.systemone":{"drex-api-key":{"source":"environment","name":"DREX_API_KEY"}}}}`), localbinding.FormatJSON)
	if err != nil {
		t.Fatal(err)
	}
	credentials, err := localbinding.Preflight(p, bindings, localbinding.EnvironmentFunc(func(string) (string, bool) { return drexTestKey, true }))
	if err != nil {
		t.Fatal(err)
	}
	defer credentials.Clear()
	in := runnerInput(t, p)
	in.Credentials = credentials
	start := time.Now()
	d, err := runner.Run(context.Background(), in, runner.Registry{Identity: adapter})
	if err != nil {
		t.Fatal(err)
	}
	// The profile's 250ms backoff is jittered down to as little as 125ms; the
	// provider's 300ms wait is the floor.
	if elapsed := time.Since(start); calls.Load() != 2 || elapsed < 300*time.Millisecond || d.Outcome != decision.OutcomeDeny {
		t.Fatalf("calls = %d, elapsed = %v, outcome = %s", calls.Load(), elapsed, d.Outcome)
	}
}

func TestRetryAfterParsing(t *testing.T) {
	now := time.Date(2026, 9, 27, 12, 0, 0, 0, time.UTC)
	for _, c := range []struct {
		name    string
		headers map[string]string
		want    time.Duration
	}{
		{"none", nil, 0},
		{"milliseconds", map[string]string{"Retry-After-Ms": "1500"}, 1500 * time.Millisecond},
		{"fractional milliseconds", map[string]string{"Retry-After-Ms": "2.5"}, 2500 * time.Microsecond},
		{"seconds", map[string]string{"Retry-After": "3"}, 3 * time.Second},
		{"milliseconds win", map[string]string{"Retry-After-Ms": "500", "Retry-After": "3"}, 500 * time.Millisecond},
		{"malformed milliseconds fall back", map[string]string{"Retry-After-Ms": "soon", "Retry-After": "3"}, 3 * time.Second},
		{"negative milliseconds fall back", map[string]string{"Retry-After-Ms": "-5", "Retry-After": "3"}, 3 * time.Second},
		{"non-finite milliseconds fall back", map[string]string{"Retry-After-Ms": "NaN", "Retry-After": "3"}, 3 * time.Second},
		{"http date", map[string]string{"Retry-After": "Sun, 27 Sep 2026 12:00:05 GMT"}, 5 * time.Second},
		{"obsolete http date", map[string]string{"Retry-After": "Sunday, 27-Sep-26 12:00:07 GMT"}, 7 * time.Second},
		{"past http date", map[string]string{"Retry-After": "Sun, 27 Sep 2026 11:59:00 GMT"}, 0},
		{"negative seconds", map[string]string{"Retry-After": "-1"}, 0},
		{"decimal seconds", map[string]string{"Retry-After": "1.5"}, 0},
		{"malformed", map[string]string{"Retry-After": "later"}, 0},
		{"huge seconds", map[string]string{"Retry-After": "99999999999999999999999"}, maxRetryAfter},
		{"huge milliseconds", map[string]string{"Retry-After-Ms": "1e300"}, maxRetryAfter},
		{"far http date", map[string]string{"Retry-After": "Fri, 31 Dec 2100 23:59:59 GMT"}, maxRetryAfter},
	} {
		t.Run(c.name, func(t *testing.T) {
			header := http.Header{}
			for k, v := range c.headers {
				header.Set(k, v)
			}
			if got := retryAfter(header, now); got != c.want {
				t.Fatalf("retryAfter = %v, want %v", got, c.want)
			}
		})
	}
}

// Version 0.2.0 keeps its original behavior: HTTP 529 is unexpected, no
// provider wait is reported, and no request ID is recorded.
func TestPreviousVersionKeepsStatusHandling(t *testing.T) {
	for _, c := range []struct {
		status    int
		code      string
		retryable bool
	}{
		{529, "systemone.unexpected_status", false},
		{429, "evaluator.throttled", true},
		{503, "evaluator.unavailable", true},
	} {
		adapter, config := serveAntaeus(t, func(w http.ResponseWriter, _ *http.Request) {
			w.Header().Set("Retry-After", "2")
			w.WriteHeader(c.status)
		})
		config.Credential = []byte(testKey)
		_, err := adapter.Evaluate(context.Background(), request(t), config)
		if failure := evaluationError(t, err); failure.Code != c.code || failure.Retryable != c.retryable || failure.RetryAfter != 0 {
			t.Fatalf("HTTP %d: failure = %+v", c.status, failure)
		}
	}
	adapter, config := serveAntaeus(t, func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("X-Request-Id", "req_1")
		_, _ = w.Write([]byte(`{"model":"antaeus-local","answers":{"prohibited-item":{"type":"noul","noul":0.9},"complete-listing":{"type":"noul","noul":0.2}}}`))
	})
	config.Credential = []byte(testKey)
	result, err := adapter.Evaluate(context.Background(), request(t), config)
	if err != nil || result.Metadata.RequestID != "" {
		t.Fatalf("err = %v, metadata = %+v", err, result.Metadata)
	}
}
