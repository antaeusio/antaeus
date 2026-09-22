package fixture

import (
	"context"
	"encoding/json"
	"errors"
	"math"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/antaeusio/antaeus/decision"
	"github.com/antaeusio/antaeus/evaluator"
	"github.com/antaeusio/antaeus/policy"
)

func TestQuickstartFixtureEvaluatesExactCase(t *testing.T) {
	set := loadQuickstartSet(t)
	adapter, err := New(set, "aggregate-analytics")
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	request := quickstartRequest()
	result, err := adapter.Evaluate(context.Background(), request)
	if err != nil {
		t.Fatalf("Evaluate() error = %v", err)
	}
	if err := evaluator.ValidateResult(request, result); err != nil {
		t.Fatalf("ValidateResult() error = %v", err)
	}
	if !result.Metadata.Synthetic || result.Metadata.Mode != evaluator.ModeDeterministicFixture {
		t.Fatalf("metadata = %#v, want visibly synthetic fixture", result.Metadata)
	}
	if result.Metadata.FixtureSet != "quickstart" || result.Metadata.FixtureVersion != "v1" {
		t.Fatalf("fixture identity = %q@%q, want quickstart@v1", result.Metadata.FixtureSet, result.Metadata.FixtureVersion)
	}
	if result.Metadata.RequestID != request.CorrelationID {
		t.Fatalf("request id = %q, want %q", result.Metadata.RequestID, request.CorrelationID)
	}
}

func TestFixtureRejectsIdentityMismatch(t *testing.T) {
	adapter, err := New(loadQuickstartSet(t), "aggregate-analytics")
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	request := quickstartRequest()
	request.CanonicalInput = json.RawMessage(`{"description":"different","serviceCategory":"analytics"}`)

	_, err = adapter.Evaluate(context.Background(), request)
	var adapterError *evaluator.Error
	if !errors.As(err, &adapterError) || adapterError.Code != "fixture.identity_mismatch" {
		t.Fatalf("Evaluate() error = %v, want fixture.identity_mismatch", err)
	}
}

func TestFixtureRejectsPolicyIdentityMismatch(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(*evaluator.Request)
	}{
		{
			name: "policy name",
			mutate: func(request *evaluator.Request) {
				request.PolicyName = "different"
			},
		},
		{
			name: "policy digest",
			mutate: func(request *evaluator.Request) {
				request.PolicyDigest = "sha256:0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			adapter, err := New(loadQuickstartSet(t), "aggregate-analytics")
			if err != nil {
				t.Fatalf("New() error = %v", err)
			}
			request := quickstartRequest()
			test.mutate(&request)
			_, err = adapter.Evaluate(context.Background(), request)
			requireAdapterCode(t, err, "fixture.identity_mismatch")
		})
	}
}

func TestFixtureRejectsMismatchedRuleMappings(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(*evaluator.Request)
	}{
		{
			name: "extra rule",
			mutate: func(request *evaluator.Request) {
				request.Rules = append(request.Rules, evaluator.Rule{ID: "extra", When: "An extra condition applies."})
			},
		},
		{
			name: "missing rule",
			mutate: func(request *evaluator.Request) {
				request.Rules = request.Rules[:1]
			},
		},
		{
			name: "different order",
			mutate: func(request *evaluator.Request) {
				request.Rules[0], request.Rules[1] = request.Rules[1], request.Rules[0]
			},
		},
		{
			name: "unknown rule",
			mutate: func(request *evaluator.Request) {
				request.Rules[1].ID = "unknown"
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			adapter, err := New(loadQuickstartSet(t), "aggregate-analytics")
			if err != nil {
				t.Fatalf("New() error = %v", err)
			}
			request := quickstartRequest()
			test.mutate(&request)
			_, err = adapter.Evaluate(context.Background(), request)
			requireAdapterCode(t, err, "fixture.mapping_invalid")
		})
	}
}

func TestFixtureRejectsInvalidRequest(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(*evaluator.Request)
	}{
		{
			name: "non-object input",
			mutate: func(request *evaluator.Request) {
				request.CanonicalInput = json.RawMessage(`[]`)
			},
		},
		{
			name: "duplicate rule",
			mutate: func(request *evaluator.Request) {
				request.Rules[1].ID = request.Rules[0].ID
			},
		},
		{
			name: "bad digest",
			mutate: func(request *evaluator.Request) {
				request.ProfileDigest = "bad"
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			adapter, err := New(loadQuickstartSet(t), "aggregate-analytics")
			if err != nil {
				t.Fatalf("New() error = %v", err)
			}
			request := quickstartRequest()
			test.mutate(&request)
			_, err = adapter.Evaluate(context.Background(), request)
			requireAdapterCode(t, err, "fixture.request_invalid")
		})
	}
}

func TestFixtureRejectsElapsedDeadline(t *testing.T) {
	adapter, err := New(loadQuickstartSet(t), "aggregate-analytics")
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	request := quickstartRequest()
	request.Deadline = time.Now().Add(-time.Second)
	_, err = adapter.Evaluate(context.Background(), request)
	requireAdapterCode(t, err, "evaluation.deadline_exceeded")
}

func TestFixtureRejectsMissingCase(t *testing.T) {
	_, err := New(loadQuickstartSet(t), "missing")
	var adapterError *evaluator.Error
	if !errors.As(err, &adapterError) || adapterError.Code != "fixture.case_missing" {
		t.Fatalf("New() error = %v, want fixture.case_missing", err)
	}
}

func TestFixtureHonorsCancellation(t *testing.T) {
	adapter, err := New(loadQuickstartSet(t), "aggregate-analytics")
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err = adapter.Evaluate(ctx, quickstartRequest())
	var adapterError *evaluator.Error
	if !errors.As(err, &adapterError) || adapterError.Code != "evaluation.cancelled" {
		t.Fatalf("Evaluate() error = %v, want evaluation.cancelled", err)
	}
}

func TestFixtureClassifiesContextDeadline(t *testing.T) {
	adapter, err := New(loadQuickstartSet(t), "aggregate-analytics")
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	ctx, cancel := context.WithDeadline(context.Background(), time.Now().Add(-time.Second))
	defer cancel()
	_, err = adapter.Evaluate(ctx, quickstartRequest())
	requireAdapterCode(t, err, "evaluation.deadline_exceeded")
}

func TestSetValidateRejectsInvalidData(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(*Set)
	}{
		{name: "wrong api version", mutate: func(set *Set) { set.APIVersion = "fixture.antaeus.io/v2" }},
		{name: "wrong kind", mutate: func(set *Set) { set.Kind = "Other" }},
		{name: "duplicate case", mutate: func(set *Set) { set.Cases = append(set.Cases, set.Cases[0]) }},
		{name: "invalid input digest", mutate: func(set *Set) { set.Cases[0].InputDigest = "bad" }},
		{name: "empty reason codes", mutate: func(set *Set) { set.Cases[0].RuleResults[0].ReasonCodes = nil }},
		{name: "duplicate reason code", mutate: func(set *Set) {
			set.Cases[0].RuleResults[0].ReasonCodes = []string{"fixture.same", "fixture.same"}
		}},
		{name: "invalid status", mutate: func(set *Set) { set.Cases[0].RuleResults[0].Status = "unknown" }},
		{name: "confidence above one", mutate: func(set *Set) {
			confidence := 1.1
			set.Cases[0].RuleResults[0].Confidence = &confidence
		}},
		{name: "confidence nan", mutate: func(set *Set) {
			confidence := math.NaN()
			set.Cases[0].RuleResults[0].Confidence = &confidence
		}},
		{name: "oversized message", mutate: func(set *Set) {
			set.Cases[0].RuleResults[0].Message = strings.Repeat("x", evaluator.MaxMessageLength+1)
		}},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			set := loadQuickstartSet(t)
			test.mutate(&set)
			if err := set.Validate(); err == nil {
				t.Fatal("Validate() error = nil, want rejection")
			}
		})
	}
}

func TestParseRejectsUnknownFieldsAndTrailingJSON(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(map[string]any)
	}{
		{name: "top level", mutate: func(document map[string]any) { document["unknown"] = true }},
		{name: "metadata", mutate: func(document map[string]any) {
			document["metadata"].(map[string]any)["unknown"] = true
		}},
		{name: "case", mutate: func(document map[string]any) {
			document["cases"].([]any)[0].(map[string]any)["unknown"] = true
		}},
		{name: "rule result", mutate: func(document map[string]any) {
			document["cases"].([]any)[0].(map[string]any)["ruleResults"].([]any)[0].(map[string]any)["unknown"] = true
		}},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			var document map[string]any
			if err := json.Unmarshal(readQuickstartSet(t), &document); err != nil {
				t.Fatalf("decode valid fixture: %v", err)
			}
			test.mutate(document)
			source, err := json.Marshal(document)
			if err != nil {
				t.Fatalf("encode mutated fixture: %v", err)
			}
			if _, err := Parse(source); err == nil {
				t.Fatal("Parse() error = nil, want unknown-field rejection")
			}
		})
	}

	if _, err := Parse(append(readQuickstartSet(t), []byte(` {}`)...)); err == nil {
		t.Fatal("Parse() error = nil, want trailing JSON rejection")
	}
}

func TestQuickstartFixtureMatchesContractExamples(t *testing.T) {
	set := loadQuickstartSet(t)
	fixtureCase := set.Cases[0]

	var artifact policy.Artifact
	if err := json.Unmarshal(readContractFile(t, "policy", "vendor-onboarding.json"), &artifact); err != nil {
		t.Fatalf("decode policy example: %v", err)
	}
	digest, err := artifact.Digest()
	if err != nil {
		t.Fatalf("Digest() error = %v", err)
	}
	if fixtureCase.PolicyName != artifact.Metadata.Name || fixtureCase.PolicyDigest != digest {
		t.Fatalf("fixture policy identity = %s %s, want %s %s", fixtureCase.PolicyName, fixtureCase.PolicyDigest, artifact.Metadata.Name, digest)
	}

	var decisionRequest decision.DecisionRequest
	if err := json.Unmarshal(readContractFile(t, "request", "reference.json"), &decisionRequest); err != nil {
		t.Fatalf("decode request example: %v", err)
	}
	canonicalInput := json.RawMessage(`{"description":"Processes aggregate product events.","serviceCategory":"analytics"}`)
	var canonicalValue any
	if err := json.Unmarshal(canonicalInput, &canonicalValue); err != nil {
		t.Fatalf("decode canonical input: %v", err)
	}
	var requestValue any
	encodedRequestInput, err := json.Marshal(decisionRequest.Input)
	if err != nil {
		t.Fatalf("encode request input: %v", err)
	}
	if err := json.Unmarshal(encodedRequestInput, &requestValue); err != nil {
		t.Fatalf("decode request input: %v", err)
	}
	if !reflect.DeepEqual(canonicalValue, requestValue) {
		t.Fatalf("canonical fixture input does not match request example")
	}
	rules := make([]evaluator.Rule, len(artifact.Spec.Rules))
	for i, rule := range artifact.Spec.Rules {
		rules[i] = evaluator.Rule{ID: rule.ID, When: rule.When}
		if fixtureCase.RuleResults[i].RuleID != rule.ID {
			t.Fatalf("fixture rule %d = %q, want %q", i, fixtureCase.RuleResults[i].RuleID, rule.ID)
		}
	}
	request := quickstartRequest()
	request.CanonicalInput = canonicalInput
	request.Rules = rules
	inputDigest, err := request.InputDigest()
	if err != nil {
		t.Fatalf("InputDigest() error = %v", err)
	}
	if fixtureCase.InputDigest != inputDigest {
		t.Fatalf("fixture input digest = %q, want %q", fixtureCase.InputDigest, inputDigest)
	}
}

func loadQuickstartSet(t *testing.T) Set {
	t.Helper()
	set, err := Parse(readQuickstartSet(t))
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}
	return set
}

func readQuickstartSet(t *testing.T) []byte {
	t.Helper()
	path := filepath.Join("..", "..", "contracts", "examples", "v0alpha1", "fixture-set", "quickstart.json")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	return data
}

func readContractFile(t *testing.T, directory, name string) []byte {
	t.Helper()
	path := filepath.Join("..", "..", "contracts", "examples", "v0alpha1", directory, name)
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	return data
}

func requireAdapterCode(t *testing.T, err error, code string) {
	t.Helper()
	var adapterError *evaluator.Error
	if !errors.As(err, &adapterError) || adapterError.Code != code {
		t.Fatalf("error = %v, want %s", err, code)
	}
}

func quickstartRequest() evaluator.Request {
	return evaluator.Request{
		PolicyName:     "vendor-onboarding",
		PolicyDigest:   "sha256:2378b5a1806bb11c618bd3a78122e773ec93bdfc45b751a18887d40f4ea536a2",
		ProfileDigest:  "sha256:abcdef0123456789abcdef0123456789abcdef0123456789abcdef0123456789",
		CanonicalInput: json.RawMessage(`{"description":"Processes aggregate product events.","serviceCategory":"analytics"}`),
		Rules: []evaluator.Rule{
			{ID: "prohibited-service", When: "The submitted service belongs to a prohibited category."},
			{ID: "complete-low-risk-submission", When: "The submission is complete and contains no material risk indicators."},
		},
		Deadline:      time.Now().Add(time.Minute),
		CorrelationID: "quickstart-test",
	}
}
