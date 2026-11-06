package systemone

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/antaeusio/antaeus/decision"
	"github.com/antaeusio/antaeus/evaluator"
	"github.com/antaeusio/antaeus/evaluator/runner"
)

// Synthetic wire examples, not a captured provider response. The official
// API example cited in docs/systemone-adapter.md
// includes confidence alongside type and noul. Historical versions stay strict.
func TestDrexConfidenceCompatibility(t *testing.T) {
	for _, tt := range []struct {
		name, answer string
		valid        bool
	}{
		{"original", `{"type":"noul","noul":0.9}`, true},
		{"documented field", `{"type":"noul","noul":0.9,"confidence":0.9}`, true},
		{"metadata upper endpoint", `{"type":"noul","noul":0.9,"confidence":1}`, true},
		{"metadata does not replace probability", `{"type":"noul","noul":0.9,"confidence":0.1}`, true},
		{"unknown field", `{"type":"noul","noul":0.9,"confidence":0.9,"other":1}`, false},
		{"wrong field case", `{"type":"noul","noul":0.9,"Confidence":0.9}`, false},
		{"null metadata", `{"type":"noul","noul":0.9,"confidence":null}`, false},
		{"string metadata", `{"type":"noul","noul":0.9,"confidence":"0.9"}`, false},
		{"boolean metadata", `{"type":"noul","noul":0.9,"confidence":true}`, false},
		{"low metadata", `{"type":"noul","noul":0.9,"confidence":-0.1}`, false},
		{"high metadata", `{"type":"noul","noul":0.9,"confidence":1.1}`, false},
		{"overflow metadata", `{"type":"noul","noul":0.9,"confidence":1e999}`, false},
		{"duplicate metadata", `{"type":"noul","noul":0.9,"confidence":0.9,"confidence":0.1}`, false},
		{"missing probability", `{"type":"noul","confidence":0.9}`, false},
		{"null probability", `{"type":"noul","noul":null,"confidence":0.9}`, false},
		{"string probability", `{"type":"noul","noul":"0.9","confidence":0.9}`, false},
		{"wrong type", `{"type":"choice","noul":0.9,"confidence":0.9}`, false},
		{"high probability", `{"type":"noul","noul":1.1,"confidence":0.9}`, false},
	} {
		t.Run(tt.name, func(t *testing.T) {
			body := []byte(fmt.Sprintf(`{"model":"drex-v1.5","answers":{"described_item":%s},"usage":{"input_tokens":31,"output_tokens":24}}`, tt.answer))
			config := runner.Configuration{Evaluator: drexProfile(t).Spec.Evaluators[0], Credential: []byte(drexTestKey)}
			config.Evaluator.Adapter.Version = "0.5.0"
			a := usageTestAdapter(t, "0.5.0", body, http.StatusOK)
			result, err := a.evaluate(context.Background(), drexRequest(t), config)
			if tt.valid {
				if err != nil {
					t.Fatal(err)
				}
				if len(result.RuleResults) != 1 || result.RuleResults[0].Status != decision.RuleMatched || *result.RuleResults[0].Confidence != 0.9 {
					t.Fatalf("unexpected rule result: %+v", result.RuleResults)
				}
				if result.Metadata.AdapterVersion != "0.5.0" || result.Metadata.Model != "drex-v1.5" {
					t.Fatalf("unexpected metadata: %+v", result.Metadata)
				}
			} else if failure := evaluationError(t, err); failure.Code != "systemone.output_invalid" || failure.Retryable {
				t.Fatalf("unexpected failure: %+v", failure)
			}
			if result.Usage == nil || result.Usage.Status != "reported" || *result.Usage.InputTokens != 31 || *result.Usage.OutputTokens != 24 {
				t.Fatalf("lost accounting on response: %+v", result.Usage)
			}
		})
	}
}

func TestDrexCompatibilityCoverageAndAccounting(t *testing.T) {
	for name, answers := range map[string]string{
		"missing": `{}`,
		"unknown": `{"other":{"type":"noul","noul":0.9,"confidence":0.9}}`,
		"extra":   `{"described_item":{"type":"noul","noul":0.9,"confidence":0.9},"other":{"type":"noul","noul":0.9}}`,
	} {
		t.Run(name, func(t *testing.T) {
			config := runner.Configuration{Evaluator: drexProfile(t).Spec.Evaluators[0], Credential: []byte(drexTestKey)}
			config.Evaluator.Adapter = DrexCompatibilityIdentity
			body := []byte(fmt.Sprintf(`{"model":"drex-v1.5","answers":%s,"usage":{"input_tokens":31}}`, answers))
			result, err := usageTestAdapter(t, DrexCompatibilityVersion, body, http.StatusOK).evaluate(context.Background(), drexRequest(t), config)
			if failure := evaluationError(t, err); failure.Code != "systemone.output_invalid" || failure.Retryable {
				t.Fatalf("unexpected failure: %+v", failure)
			}
			if result.Usage == nil || result.Usage.Status != "reported" || *result.Usage.InputTokens != 31 || len(result.RuleResults) != 0 {
				t.Fatalf("invalid response leaked evidence or lost usage: %+v", result)
			}
		})
	}
}

func TestDrexCompatibilityProbabilityBoundaries(t *testing.T) {
	for _, tc := range []struct {
		p, expectedConfidence float64
		status                decision.RuleStatus
	}{
		{0, 1, decision.RuleNotMatched},
		{0.5, 0.5, decision.RuleIndeterminate},
		{1, 1, decision.RuleMatched},
	} {
		t.Run(fmt.Sprint(tc.p), func(t *testing.T) {
			body := []byte(fmt.Sprintf(`{"model":"drex-v1.5","answers":{"described_item":{"type":"noul","noul":%v,"confidence":0}}}`, tc.p))
			results, _, err := parseVersionedResponse(body, drexRequest(t).Rules, true)
			if err != nil || len(results) != 1 || results[0].Status != tc.status || *results[0].Confidence != tc.expectedConfidence {
				t.Fatalf("normalization changed: %+v %v", results, err)
			}
		})
	}
}

func TestDrexCompatibilityMixedAnswerMetadata(t *testing.T) {
	config := runner.Configuration{Evaluator: drexProfile(t).Spec.Evaluators[0], Credential: []byte(drexTestKey)}
	config.Evaluator.Adapter = DrexCompatibilityIdentity
	body := []byte(`{"model":"drex-v1.5","answers":{"prohibited-item":{"type":"noul","noul":0.1},"complete-listing":{"type":"noul","noul":0.9,"confidence":1}}}`)
	result, err := usageTestAdapter(t, DrexCompatibilityVersion, body, http.StatusOK).evaluate(context.Background(), request(t), config)
	if err != nil || len(result.RuleResults) != 2 || result.RuleResults[0].Status != decision.RuleNotMatched || result.RuleResults[1].Status != decision.RuleMatched {
		t.Fatalf("mixed optional metadata failed: %+v %v", result.RuleResults, err)
	}
	for _, rule := range result.RuleResults {
		if rule.Confidence == nil || *rule.Confidence != 0.9 {
			t.Fatalf("metadata replaced noul confidence: %+v", rule)
		}
	}
}

func TestDrexCompatibilityProviderAndEndpointFences(t *testing.T) {
	entry := drexProfile(t).Spec.Evaluators[0]
	entry.Adapter = DrexCompatibilityIdentity
	if err := ValidateEvaluator(entry); err != nil {
		t.Fatal(err)
	}
	for provider, slot := range map[string]string{ProviderCLM: CredentialSlot, ProviderAntaeus: AntaeusCredentialSlot} {
		entry.Provider, entry.CredentialSlot = &provider, &slot
		entry.Parameters = map[string]any{"endpoint": "http://127.0.0.1:8080"}
		if err := ValidateEvaluator(entry); err == nil || err.Error() != `io.antaeus.systemone@0.5.0 supports only provider "drex"` {
			t.Fatalf("provider fence for %s: %v", provider, err)
		}
	}
	provider := ProviderDrex
	entry.Provider = &provider
	slot := DrexCredentialSlot
	entry.CredentialSlot = &slot
	entry.Parameters = map[string]any{"endpoint": "https://example.com"}
	if err := ValidateEvaluator(entry); err == nil {
		t.Fatal("accepted unpinned endpoint")
	}
	entry.Parameters = map[string]any{"endpoint": DrexEndpoint}
	entry.CredentialSlot = nil
	if err := ValidateEvaluator(entry); err == nil {
		t.Fatal("accepted missing credential slot")
	}
}

func TestDrexCompatibilityTransportAndRetryWait(t *testing.T) {
	config := runner.Configuration{Evaluator: drexProfile(t).Spec.Evaluators[0], Credential: []byte(drexTestKey)}
	config.Evaluator.Adapter = DrexCompatibilityIdentity
	for _, tc := range []struct {
		name, code string
		err        error
	}{
		{"deadline", "evaluator.timeout", context.DeadlineExceeded},
		{"connection", "evaluator.unavailable", errors.New("PRIVATE_CONNECTION_ERROR")},
	} {
		t.Run(tc.name, func(t *testing.T) {
			a := usageTestAdapter(t, DrexCompatibilityVersion, nil, http.StatusOK)
			a.client.Transport = roundTripFunc(func(*http.Request) (*http.Response, error) { return nil, tc.err })
			result, err := a.evaluate(context.Background(), drexRequest(t), config)
			failure := evaluationError(t, err)
			if failure.Code != tc.code || !failure.Retryable || strings.Contains(failure.Message, "PRIVATE_") {
				t.Fatalf("unexpected failure: %+v", failure)
			}
			if result.Usage == nil || result.Usage.Status != evaluator.UsageUnavailable || result.Usage.InputTokens != nil {
				t.Fatalf("unexpected usage: %+v", result.Usage)
			}
		})
	}
	a := usageTestAdapter(t, DrexCompatibilityVersion, nil, http.StatusTooManyRequests)
	a.client.Transport = roundTripFunc(func(r *http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: 429, Header: http.Header{"Retry-After-Ms": []string{"1250"}}, Body: io.NopCloser(strings.NewReader(`PRIVATE_PROVIDER_MESSAGE`)), Request: r}, nil
	})
	result, err := a.evaluate(context.Background(), drexRequest(t), config)
	failure := evaluationError(t, err)
	if failure.Code != "evaluator.throttled" || !failure.Retryable || failure.RetryAfter != 1250*time.Millisecond {
		t.Fatalf("retry wait changed: %+v", failure)
	}
	if result.Usage == nil || result.Usage.Status != evaluator.UsageUnavailable {
		t.Fatalf("unexpected usage: %+v", result.Usage)
	}
}

func TestDrexCompatibilityHTTPFailuresRetainUnknownUsage(t *testing.T) {
	for status, code := range map[int]string{401: "systemone.credential_rejected", 422: "systemone.request_rejected", 429: "evaluator.throttled", 500: "evaluator.unavailable", 529: "evaluator.unavailable"} {
		t.Run(fmt.Sprint(status), func(t *testing.T) {
			config := runner.Configuration{Evaluator: drexProfile(t).Spec.Evaluators[0], Credential: []byte(drexTestKey)}
			config.Evaluator.Adapter = DrexCompatibilityIdentity
			result, err := usageTestAdapter(t, DrexCompatibilityVersion, []byte(`PRIVATE_PROVIDER_MESSAGE`), status).evaluate(context.Background(), drexRequest(t), config)
			failure := evaluationError(t, err)
			if failure.Code != code || failure.Retryable != (status == 429 || status == 500 || status == 529) {
				t.Fatalf("unexpected failure: %+v", failure)
			}
			if result.Usage == nil || result.Usage.Status != evaluator.UsageUnavailable {
				t.Fatalf("fabricated usage: %+v", result.Usage)
			}
		})
	}
}

func TestDrexCompatibilityExistingFixtures(t *testing.T) {
	for _, tc := range []struct {
		fixture string
		status  int
		code    string
	}{
		{"success-200.json", 200, ""},
		{"credential-rejected-401.json", 401, "systemone.credential_rejected"},
		{"request-rejected-422.json", 422, "systemone.request_rejected"},
		{"throttled-429.json", 429, "evaluator.throttled"},
		{"overloaded-529.json", 529, "evaluator.unavailable"},
	} {
		t.Run(tc.fixture, func(t *testing.T) {
			calls := 0
			adapter, config := serveDrex(t, func(w http.ResponseWriter, r *http.Request) {
				calls++
				if r.Header.Get("Authorization") != "Bearer "+drexTestKey || r.URL.Path != "/v1/systemone" || r.Host != "drex.nace.ai" {
					t.Error("provider request lost its endpoint or credential fence")
				}
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(tc.status)
				_, _ = w.Write(drexFixture(t, tc.fixture))
			}, DrexCompatibilityVersion)
			result, err := adapter.Evaluate(context.Background(), drexRequest(t), config)
			if calls != 1 {
				t.Fatalf("unexpected provider calls: %d", calls)
			}
			if tc.code == "" {
				if err != nil || len(result.RuleResults) != 1 || result.RuleResults[0].Status != decision.RuleMatched || *result.RuleResults[0].Confidence != 0.9037 {
					t.Fatalf("recorded success changed: %+v %v", result, err)
				}
				if result.Metadata.AdapterVersion != DrexCompatibilityVersion || result.Metadata.Model != "drex-latest" {
					t.Fatalf("metadata drift: %+v", result.Metadata)
				}
			} else if failure := evaluationError(t, err); failure.Code != tc.code {
				t.Fatalf("recorded failure changed: %+v", failure)
			}
		})
	}
}

// Replay the allowlisted answer and usage fields of a real synthetic request.
// The same valid provider response must fail under 0.4.0 and pass under 0.5.0.
func TestDrexCapturedConfidenceResponse(t *testing.T) {
	for _, version := range []string{UsageAdapterVersion, DrexCompatibilityVersion} {
		t.Run(version, func(t *testing.T) {
			adapter, config := serveDrex(t, func(w http.ResponseWriter, r *http.Request) {
				if r.Header.Get("Authorization") != "Bearer "+drexTestKey {
					t.Error("credential fence changed")
				}
				_, _ = w.Write(drexFixture(t, "success-confidence-200.json"))
			}, version)
			model := "drex-v1.5"
			config.Evaluator.Model = &model
			result, err := adapter.Evaluate(context.Background(), drexRequest(t), config)
			if version == UsageAdapterVersion {
				failure := evaluationError(t, err)
				if failure.Code != "systemone.output_invalid" || failure.Retryable {
					t.Fatalf("historical failure changed: %+v", failure)
				}
			} else {
				if err != nil || len(result.RuleResults) != 1 || result.RuleResults[0].Status != decision.RuleMatched || *result.RuleResults[0].Confidence != 0.959 {
					t.Fatalf("captured valid answer failed: %+v %v", result.RuleResults, err)
				}
				if result.Metadata.Model != model || result.Metadata.AdapterVersion != version {
					t.Fatalf("unexpected metadata: %+v", result.Metadata)
				}
			}
			if result.Usage == nil || result.Usage.Status != evaluator.UsageReported || *result.Usage.InputTokens != 30 || *result.Usage.OutputTokens != 23 {
				t.Fatalf("captured accounting lost: %+v", result.Usage)
			}
		})
	}
}

func TestHistoricalVersionsRejectConfidenceField(t *testing.T) {
	for _, version := range []string{"0.1.0", "0.2.0", "0.3.0", "0.4.0"} {
		t.Run(version, func(t *testing.T) {
			config := runner.Configuration{Evaluator: drexProfile(t).Spec.Evaluators[0], Credential: []byte(drexTestKey)}
			config.Evaluator.Adapter.Version = version
			if version == "0.1.0" || version == "0.2.0" {
				provider, slot := ProviderCLM, CredentialSlot
				config.Evaluator.Provider, config.Evaluator.CredentialSlot = &provider, &slot
			}
			body := []byte(`{"model":"drex-v1.5","answers":{"described_item":{"type":"noul","noul":0.9,"confidence":0.9}}}`)
			_, err := usageTestAdapter(t, version, body, http.StatusOK).evaluate(context.Background(), drexRequest(t), config)
			if failure := evaluationError(t, err); failure.Code != "systemone.output_invalid" || failure.Retryable {
				t.Fatalf("historical behavior changed: %+v", failure)
			}
		})
	}
}
