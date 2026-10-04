package systemone

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"reflect"
	"testing"
	"time"

	"github.com/antaeusio/antaeus/evaluator"
	"github.com/antaeusio/antaeus/evaluator/localbinding"
	"github.com/antaeusio/antaeus/evaluator/runner"
	"github.com/antaeusio/antaeus/policy"
)

func TestUsageReportingIsVersioned(t *testing.T) {
	for _, version := range []string{LegacyAdapterVersion, PreviousAdapterVersion, AdapterVersion, UsageAdapterVersion} {
		t.Run(version, func(t *testing.T) {
			config := runner.Configuration{Evaluator: drexProfile(t).Spec.Evaluators[0], Credential: []byte(drexTestKey)}
			// Use the same transport with each version; legacy versions use CLM.
			config.Evaluator.Adapter.Version = version
			if version == LegacyAdapterVersion || version == PreviousAdapterVersion {
				provider, slot := ProviderCLM, CredentialSlot
				config.Evaluator.Provider, config.Evaluator.CredentialSlot = &provider, &slot
			}
			adapter := usageTestAdapter(t, version, drexFixture(t, "success-200.json"), http.StatusOK)
			result, err := adapter.evaluate(context.Background(), drexRequest(t), config)
			if err != nil {
				t.Fatal(err)
			}
			if version != UsageAdapterVersion {
				if result.Usage != nil {
					t.Fatalf("old version emitted usage: %+v", result.Usage)
				}
			} else if result.Usage == nil || result.Usage.Status != "reported" || *result.Usage.InputTokens != 31 || *result.Usage.OutputTokens != 24 {
				t.Fatalf("usage = %+v", result.Usage)
			}
		})
	}
}

func usageTestAdapter(t *testing.T, version string, payload []byte, status int) *adapter {
	t.Helper()
	return &adapter{version: version, now: time.Now, client: &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: status, Header: http.Header{}, Body: io.NopCloser(bytes.NewReader(payload)), Request: r}, nil
	})}}
}

func TestUsageCounters(t *testing.T) {
	for _, tt := range []struct {
		name, usage string
		status      evaluator.UsageStatus
	}{
		{"missing", "", "unavailable"},
		{"reported", `{"input_tokens":31,"output_tokens":24,"billing_units":2}`, "reported"},
		{"zero", `{"input_tokens":0,"output_tokens":0}`, "reported"},
		{"optional output", `{"input_tokens":31}`, "reported"},
		{"null usage", `null`, "invalid"},
		{"array", `[]`, "invalid"},
		{"missing input", `{"output_tokens":24}`, "invalid"},
		{"null input", `{"input_tokens": null }`, "invalid"},
		{"negative", `{"input_tokens":-1}`, "invalid"},
		{"fraction", `{"input_tokens":1.5}`, "invalid"},
		{"decimal spelling", `{"input_tokens":1.0}`, "invalid"},
		{"exponent spelling", `{"input_tokens":1e3}`, "invalid"},
		{"string", `{"input_tokens":"31"}`, "invalid"},
		{"boolean", `{"input_tokens":true}`, "invalid"},
		{"overflow", `{"input_tokens":9223372036854775808}`, "invalid"},
		{"unsafe", `{"input_tokens":9007199254740992}`, "invalid"},
		{"max", `{"input_tokens":9007199254740991}`, "reported"},
		{"duplicate", `{"input_tokens":31,"input_tokens":32}`, "invalid"},
		{"case", `{"Input_tokens":31}`, "invalid"},
		{"invalid output", `{"input_tokens":31,"output_tokens":null}`, "invalid"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			var payload map[string]json.RawMessage
			_ = json.Unmarshal(drexFixture(t, "success-200.json"), &payload)
			delete(payload, "usage")
			if tt.usage != "" {
				payload["usage"] = json.RawMessage(tt.usage)
			}
			body, _ := json.Marshal(payload)
			config := runner.Configuration{Evaluator: drexProfile(t).Spec.Evaluators[0], Credential: []byte(drexTestKey)}
			config.Evaluator.Adapter = UsageIdentity
			result, err := usageTestAdapter(t, UsageAdapterVersion, body, 200).evaluate(context.Background(), drexRequest(t), config)
			if err != nil || result.Usage == nil || result.Usage.Status != tt.status {
				t.Fatalf("result %+v, err %v", result, err)
			}
			if tt.status != "reported" && (result.Usage.InputTokens != nil || result.Usage.OutputTokens != nil) {
				t.Fatal("unknown usage carries counters")
			}
		})
	}
}

func TestUsageSurvivesInvalidAnswers(t *testing.T) {
	config := runner.Configuration{Evaluator: drexProfile(t).Spec.Evaluators[0], Credential: []byte(drexTestKey)}
	config.Evaluator.Adapter = UsageIdentity
	result, err := usageTestAdapter(t, UsageAdapterVersion, []byte(`{"answers":{},"usage":{"input_tokens":41}}`), 200).evaluate(context.Background(), drexRequest(t), config)
	var failure *evaluator.Error
	if !errors.As(err, &failure) || failure.Code != "systemone.output_invalid" || result.Usage == nil || *result.Usage.InputTokens != 41 {
		t.Fatalf("result %+v, err %v", result, err)
	}
	result, err = usageTestAdapter(t, UsageAdapterVersion, []byte(`{"usage":{"input_tokens":999}}`), 503).evaluate(context.Background(), drexRequest(t), config)
	if err == nil || result.Usage.Status != "unavailable" {
		t.Fatalf("status failure: %+v %v", result, err)
	}
}

func TestDrexUsageThroughRunner(t *testing.T) {
	p := drexProfile(t)
	p.Spec.Evaluators[0].Adapter = UsageIdentity
	artifact, err := policy.LoadFile("../../examples/marketplace/listing-policy.yaml")
	if err != nil {
		t.Fatal(err)
	}
	bindings := localbinding.Artifact{APIVersion: localbinding.APIVersion, Kind: localbinding.Kind, SecretBindings: map[string]map[string]localbinding.Reference{AdapterID: {DrexCredentialSlot: {Source: "environment", Name: "TEST_DREX_KEY"}}}}
	credentials, err := localbinding.Preflight(p, bindings, localbinding.EnvironmentFunc(func(string) (string, bool) { return drexTestKey, true }))
	if err != nil {
		t.Fatal(err)
	}
	defer credentials.Clear()
	a := usageTestAdapter(t, UsageAdapterVersion, []byte(`{"model":"drex-latest","answers":{"prohibited-item":{"type":"noul","noul":0.05},"complete-listing":{"type":"noul","noul":0.9}},"usage":{"input_tokens":575,"output_tokens":48}}`), 200)
	registration := UsageRegistration()
	registration.Evaluate = a.evaluate
	d, err := runner.Run(context.Background(), runner.Input{Policy: artifact, Profile: p, CanonicalInput: json.RawMessage(`{"title":"fixture"}`), CorrelationID: "usage-test", Credentials: credentials}, runner.Registry{UsageIdentity: registration})
	if err != nil {
		t.Fatal(err)
	}
	var report runner.UsageReport
	if err := json.Unmarshal(d.Extensions[runner.UsageExtension], &report); err != nil {
		t.Fatal(err)
	}
	if len(report.Attempts) != 1 || report.Attempts[0].Status != evaluator.UsageReported || *report.Attempts[0].InputTokens != 575 || *report.Attempts[0].OutputTokens != 48 {
		t.Fatalf("report %+v", report)
	}
}

func TestUsageUnknownOnUnreadResponses(t *testing.T) {
	config := runner.Configuration{Evaluator: drexProfile(t).Spec.Evaluators[0], Credential: []byte(drexTestKey)}
	config.Evaluator.Adapter = UsageIdentity
	for _, code := range []error{context.Canceled, context.DeadlineExceeded} {
		a := usageTestAdapter(t, UsageAdapterVersion, nil, 200)
		a.client.Transport = roundTripFunc(func(*http.Request) (*http.Response, error) { return nil, code })
		result, err := a.evaluate(context.Background(), drexRequest(t), config)
		if err == nil || result.Usage == nil || result.Usage.Status != evaluator.UsageUnavailable || result.Usage.InputTokens != nil {
			t.Fatalf("result %+v err %v", result, err)
		}
	}
	a := usageTestAdapter(t, UsageAdapterVersion, bytes.Repeat([]byte(" "), MaxResponseBytes+1), 200)
	result, err := a.evaluate(context.Background(), drexRequest(t), config)
	if err == nil || result.Usage.Status != evaluator.UsageUnavailable {
		t.Fatalf("oversized response %+v err %v", result, err)
	}
}

func TestUnverifiedProviderUsageRemainsUnknown(t *testing.T) {
	for _, provider := range []string{ProviderAntaeus, ProviderCLM} {
		config := runner.Configuration{Evaluator: drexProfile(t).Spec.Evaluators[0]}
		config.Evaluator.Adapter = UsageIdentity
		config.Evaluator.Provider = &provider
		config.Evaluator.CredentialSlot = nil
		result, err := usageTestAdapter(t, UsageAdapterVersion, drexFixture(t, "success-200.json"), 200).evaluate(context.Background(), drexRequest(t), config)
		if err != nil || result.Usage.Status != evaluator.UsageUnavailable || result.Usage.InputTokens != nil {
			t.Fatalf("provider %s result %+v err %v", provider, result, err)
		}
	}
}

func TestUsageVersionPreservesEvaluationBehavior(t *testing.T) {
	for _, tt := range []struct {
		name   string
		status int
		body   []byte
	}{
		{"success", 200, drexFixture(t, "success-200.json")},
		{"overloaded", 529, nil},
		{"credential rejection", 401, nil},
		{"invalid answers", 200, []byte(`{"answers":{},"usage":{"input_tokens":31}}`)},
	} {
		t.Run(tt.name, func(t *testing.T) {
			var baseline evaluator.Result
			var baselineErr *evaluator.Error
			for _, version := range []string{AdapterVersion, UsageAdapterVersion} {
				config := runner.Configuration{Evaluator: drexProfile(t).Spec.Evaluators[0], Credential: []byte(drexTestKey)}
				config.Evaluator.Adapter.Version = version
				a := usageTestAdapter(t, version, tt.body, tt.status)
				transport := a.client.Transport
				a.client.Transport = roundTripFunc(func(r *http.Request) (*http.Response, error) {
					response, err := transport.RoundTrip(r)
					response.Header.Set("Retry-After-Ms", "350")
					response.Header.Set("X-Request-Id", "req_fixture")
					return response, err
				})
				result, err := a.evaluate(context.Background(), drexRequest(t), config)
				result.Usage = nil
				result.Metadata.AdapterVersion = "" // Only identity/accounting differ.
				var failure *evaluator.Error
				if err != nil && !errors.As(err, &failure) {
					t.Fatal(err)
				}
				if version == AdapterVersion {
					baseline, baselineErr = result, failure
				} else if !reflect.DeepEqual(baseline, result) || !reflect.DeepEqual(baselineErr, failure) {
					t.Fatalf("behavior changed: baseline %+v %v, usage version %+v %v", baseline, baselineErr, result, failure)
				}
			}
		})
	}
}
