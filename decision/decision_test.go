package decision

import (
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/antaeusio/antaeus/policy"
)

func TestDecisionExamplesValidate(t *testing.T) {
	artifact := loadPolicyExample(t)
	for _, name := range []string{"allow.json", "review.json", "deny.json", "failure.json"} {
		t.Run(name, func(t *testing.T) {
			var decision Decision
			decodeFixture(t, filepath.Join("examples", "v0alpha1", "decision", name), &decision)
			if err := decision.Validate(); err != nil {
				t.Fatalf("Validate() error = %v", err)
			}
			if err := decision.ValidateAgainst(artifact); err != nil {
				t.Fatalf("ValidateAgainst() error = %v", err)
			}
		})
	}
}

func TestDecisionRequestExampleValidates(t *testing.T) {
	var request DecisionRequest
	decodeFixture(t, filepath.Join("examples", "v0alpha1", "request", "reference.json"), &request)
	if err := request.Validate(); err != nil {
		t.Fatalf("Validate() error = %v", err)
	}
}

func TestDecisionRequestRequiresExactlyOnePolicySource(t *testing.T) {
	request := DecisionRequest{
		APIVersion: APIVersion,
		Kind:       KindRequest,
		Input:      map[string]json.RawMessage{},
	}
	if err := request.Validate(); err == nil {
		t.Fatal("Validate() error = nil with no policy source")
	}

	artifact := loadPolicyExample(t)
	request.Policy.Inline = &artifact
	request.Policy.Reference = &PolicyIdentity{
		Name:   artifact.Metadata.Name,
		Digest: "sha256:2378b5a1806bb11c618bd3a78122e773ec93bdfc45b751a18887d40f4ea536a2",
	}
	if err := request.Validate(); err == nil {
		t.Fatal("Validate() error = nil with both policy sources")
	}
}

func TestDecisionRejectsFailureMismatch(t *testing.T) {
	var decision Decision
	decodeFixture(t, filepath.Join("examples", "v0alpha1", "decision", "failure.json"), &decision)
	decision.Failure = nil

	var validationError *ValidationError
	if err := decision.Validate(); !errors.As(err, &validationError) {
		t.Fatalf("Validate() error = %v, want ValidationError", err)
	}
	if validationError.Code != "failure.missing" {
		t.Fatalf("ValidationError.Code = %q, want failure.missing", validationError.Code)
	}
}

func TestDecisionRejectsMissingFailureReasonCode(t *testing.T) {
	var decision Decision
	decodeFixture(t, filepath.Join("examples", "v0alpha1", "decision", "failure.json"), &decision)
	decision.ReasonCodes = []string{"evaluation.failed"}

	var validationError *ValidationError
	if err := decision.Validate(); !errors.As(err, &validationError) {
		t.Fatalf("Validate() error = %v, want ValidationError", err)
	}
	if validationError.Code != "failure_code.missing" {
		t.Fatalf("ValidationError.Code = %q, want failure_code.missing", validationError.Code)
	}
}

func TestDecisionRejectsInvalidConfidence(t *testing.T) {
	confidence := 1.1
	decision := validDecision()
	decision.RuleResults[0].Confidence = &confidence

	var validationError *ValidationError
	if err := decision.Validate(); !errors.As(err, &validationError) {
		t.Fatalf("Validate() error = %v, want ValidationError", err)
	}
	if validationError.Code != "confidence.invalid" {
		t.Fatalf("ValidationError.Code = %q, want confidence.invalid", validationError.Code)
	}
}

func TestDecisionRejectsDuplicateRuleResult(t *testing.T) {
	decision := validDecision()
	decision.RuleResults = append(decision.RuleResults, decision.RuleResults[0])

	var validationError *ValidationError
	if err := decision.Validate(); !errors.As(err, &validationError) {
		t.Fatalf("Validate() error = %v, want ValidationError", err)
	}
	if validationError.Code != "rule_id.duplicate" {
		t.Fatalf("ValidationError.Code = %q, want rule_id.duplicate", validationError.Code)
	}
}

func TestDecisionRejectsEmptyReasonCodes(t *testing.T) {
	decision := validDecision()
	decision.RuleResults[0].ReasonCodes = nil

	var validationError *ValidationError
	if err := decision.Validate(); !errors.As(err, &validationError) {
		t.Fatalf("Validate() error = %v, want ValidationError", err)
	}
	if validationError.Code != "reason_codes.limit" {
		t.Fatalf("ValidationError.Code = %q, want reason_codes.limit", validationError.Code)
	}
}

func TestReducePrecedence(t *testing.T) {
	deny := policy.OutcomeDeny
	review := policy.OutcomeReview
	allow := policy.OutcomeAllow
	artifact := reductionPolicy()

	tests := []struct {
		name    string
		results []RuleResult
		want    Outcome
	}{
		{
			name: "deny precedes unresolved",
			results: []RuleResult{
				result("deny", RuleMatched, &deny),
				result("review", RuleIndeterminate, nil),
				result("allow", RuleNotMatched, nil),
			},
			want: OutcomeDeny,
		},
		{
			name: "unresolved precedes review",
			results: []RuleResult{
				result("deny", RuleNotMatched, nil),
				result("review", RuleMatched, &review),
				result("allow", RuleFailed, nil),
			},
			want: OutcomeFailure,
		},
		{
			name: "review precedes allow",
			results: []RuleResult{
				result("deny", RuleNotMatched, nil),
				result("review", RuleMatched, &review),
				result("allow", RuleMatched, &allow),
			},
			want: OutcomeReview,
		},
		{
			name: "allow precedes default",
			results: []RuleResult{
				result("deny", RuleNotMatched, nil),
				result("review", RuleNotMatched, nil),
				result("allow", RuleMatched, &allow),
			},
			want: OutcomeAllow,
		},
		{
			name: "default",
			results: []RuleResult{
				result("deny", RuleNotMatched, nil),
				result("review", RuleNotMatched, nil),
				result("allow", RuleNotMatched, nil),
			},
			want: OutcomeReview,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got, err := Reduce(artifact, test.results)
			if err != nil {
				t.Fatalf("Reduce() error = %v", err)
			}
			if got.Outcome != test.want {
				t.Fatalf("Reduce().Outcome = %q, want %q", got.Outcome, test.want)
			}
			if got.Outcome == OutcomeFailure && got.Failure == nil {
				t.Fatal("Reduce().Failure is nil for failure outcome")
			}
		})
	}
}

func TestReduceRejectsMismatchedRuleIdentity(t *testing.T) {
	artifact := reductionPolicy()
	results := []RuleResult{
		result("wrong", RuleNotMatched, nil),
		result("review", RuleNotMatched, nil),
		result("allow", RuleNotMatched, nil),
	}
	if _, err := Reduce(artifact, results); err == nil {
		t.Fatal("Reduce() error = nil, want identity mismatch")
	}
}

func TestValidateAgainstRejectsTamperedPolicyDigest(t *testing.T) {
	artifact := loadPolicyExample(t)
	var decision Decision
	decodeFixture(t, filepath.Join("examples", "v0alpha1", "decision", "review.json"), &decision)
	decision.Policy.Digest = "sha256:0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"

	if err := decision.ValidateAgainst(artifact); err == nil {
		t.Fatal("ValidateAgainst() error = nil, want digest mismatch")
	}
}

func validDecision() Decision {
	return Decision{
		APIVersion: APIVersion,
		Kind:       KindDecision,
		Outcome:    OutcomeReview,
		Policy: PolicyIdentity{
			Name:   "example",
			Digest: "sha256:0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef",
		},
		RuleResults: []RuleResult{result("check", RuleNotMatched, nil)},
		ReasonCodes: []string{"policy.default_outcome"},
	}
}

func reductionPolicy() policy.Artifact {
	return policy.Artifact{
		APIVersion: policy.APIVersion,
		Kind:       policy.Kind,
		Metadata:   policy.Metadata{Name: "precedence"},
		Spec: policy.Spec{
			DefaultOutcome: policy.OutcomeReview,
			Rules: []policy.Rule{
				{ID: "deny", When: "deny condition", Outcome: policy.OutcomeDeny},
				{ID: "review", When: "review condition", Outcome: policy.OutcomeReview},
				{ID: "allow", When: "allow condition", Outcome: policy.OutcomeAllow},
			},
		},
	}
}

func result(ruleID string, status RuleStatus, outcome *policy.Outcome) RuleResult {
	return RuleResult{
		RuleID:      ruleID,
		Status:      status,
		Outcome:     outcome,
		ReasonCodes: []string{"evaluation.fixture"},
	}
}

func decodeFixture(t *testing.T, relative string, target any) {
	t.Helper()
	path := filepath.Join("..", "contracts", relative)
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		t.Fatalf("decode %s: %v", path, err)
	}
}

func loadPolicyExample(t *testing.T) policy.Artifact {
	t.Helper()
	var artifact policy.Artifact
	decodeFixture(t, filepath.Join("examples", "v0alpha1", "policy", "vendor-onboarding.json"), &artifact)
	return artifact
}
