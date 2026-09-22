package fixture

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/antaeusio/antaeus/evaluator"
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

func loadQuickstartSet(t *testing.T) Set {
	t.Helper()
	path := filepath.Join("..", "..", "contracts", "examples", "v0alpha1", "fixture-set", "quickstart.json")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	var set Set
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&set); err != nil {
		t.Fatalf("decode %s: %v", path, err)
	}
	return set
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
