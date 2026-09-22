package evaluator

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/antaeusio/antaeus/decision"
)

func TestRequestRejectsDuplicateRules(t *testing.T) {
	request := validRequest()
	request.Rules = append(request.Rules, request.Rules[0])
	if err := request.Validate(); err == nil {
		t.Fatal("Validate() error = nil, want duplicate rule rejection")
	}
}

func TestRequestRequiresObjectInput(t *testing.T) {
	request := validRequest()
	request.CanonicalInput = json.RawMessage(`[]`)
	if err := request.Validate(); err == nil {
		t.Fatal("Validate() error = nil, want non-object rejection")
	}
}

func TestValidateResultRequiresExactOrder(t *testing.T) {
	request := validRequest()
	result := validResult()
	result.RuleResults[0].RuleID = "different"
	if err := ValidateResult(request, result); err == nil {
		t.Fatal("ValidateResult() error = nil, want rule identity rejection")
	}
}

func TestValidateResultRequiresExactCount(t *testing.T) {
	request := validRequest()
	result := validResult()
	result.RuleResults = nil
	if err := ValidateResult(request, result); err == nil {
		t.Fatal("ValidateResult() error = nil, want result count rejection")
	}
}

func TestValidateResultRequiresSyntheticFixtureMarker(t *testing.T) {
	request := validRequest()
	result := validResult()
	result.Metadata.Synthetic = false
	if err := ValidateResult(request, result); err == nil {
		t.Fatal("ValidateResult() error = nil, want synthetic marker rejection")
	}
}

func TestInputDigestUsesExactCanonicalBytes(t *testing.T) {
	request := validRequest()
	got, err := request.InputDigest()
	if err != nil {
		t.Fatalf("InputDigest() error = %v", err)
	}
	const want = "sha256:ecf59a2696ca44a417e20e2a7eabb1b26e82c779f8546bea354a2cc80e8e1eed"
	if got != want {
		t.Fatalf("InputDigest() = %q, want %q", got, want)
	}
}

func validRequest() Request {
	return Request{
		PolicyName:     "example",
		PolicyDigest:   "sha256:0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef",
		CanonicalInput: json.RawMessage(`{"answer":42}`),
		Rules:          []Rule{{ID: "check", When: "The condition applies."}},
		Deadline:       time.Now().Add(time.Minute),
		CorrelationID:  "test-request",
		ProfileDigest:  "sha256:abcdef0123456789abcdef0123456789abcdef0123456789abcdef0123456789",
	}
}

func validResult() Result {
	return Result{
		RuleResults: []RuleResult{{
			RuleID:      "check",
			Status:      decision.RuleNotMatched,
			ReasonCodes: []string{"evaluation.test"},
		}},
		Metadata: Metadata{
			AdapterID:      "io.antaeus.fixture",
			AdapterVersion: "0.1.0",
			Mode:           ModeDeterministicFixture,
			Synthetic:      true,
			FixtureSet:     "test",
			FixtureVersion: "v1",
		},
	}
}
