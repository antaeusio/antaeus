package evaluator

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/antaeusio/antaeus/decision"
	"github.com/antaeusio/antaeus/policy"
)

// DecisionInput binds a validated policy and canonical input to one evaluator
// profile for a single local evaluation attempt.
type DecisionInput struct {
	Policy         policy.Artifact
	PolicyVersion  string
	CanonicalInput json.RawMessage
	Deadline       time.Time
	CorrelationID  string
	ProfileDigest  string
	ProfileVersion string
}

// Decide obtains provider-neutral evidence, attaches policy-authored outcomes,
// applies deterministic reduction, and returns a validated portable Decision.
func Decide(ctx context.Context, adapter Evaluator, input DecisionInput) (decision.Decision, error) {
	if adapter == nil {
		return decision.Decision{}, fmt.Errorf("evaluator is required")
	}
	if err := input.Policy.Validate(); err != nil {
		return decision.Decision{}, fmt.Errorf("validate policy: %w", err)
	}
	policyDigest, err := input.Policy.Digest()
	if err != nil {
		return decision.Decision{}, fmt.Errorf("digest policy: %w", err)
	}
	rules := make([]Rule, len(input.Policy.Spec.Rules))
	for i, rule := range input.Policy.Spec.Rules {
		rules[i] = Rule{ID: rule.ID, When: rule.When}
	}
	request := Request{
		PolicyName:     input.Policy.Metadata.Name,
		PolicyDigest:   policyDigest,
		CanonicalInput: input.CanonicalInput,
		Rules:          rules,
		Deadline:       input.Deadline,
		CorrelationID:  input.CorrelationID,
		ProfileDigest:  input.ProfileDigest,
	}
	if err := request.Validate(); err != nil {
		return decision.Decision{}, fmt.Errorf("validate evaluator request: %w", err)
	}

	evidence, err := adapter.Evaluate(ctx, request)
	if err != nil {
		return decision.Decision{}, fmt.Errorf("evaluate: %w", err)
	}
	if err := ValidateResult(request, evidence); err != nil {
		return decision.Decision{}, fmt.Errorf("validate evaluator result: %w", err)
	}

	ruleResults := make([]decision.RuleResult, len(evidence.RuleResults))
	for i, result := range evidence.RuleResults {
		ruleResults[i] = decision.RuleResult{
			RuleID:      result.RuleID,
			Status:      result.Status,
			Confidence:  cloneConfidence(result.Confidence),
			ReasonCodes: append([]string(nil), result.ReasonCodes...),
			Message:     result.Message,
		}
		if result.Status == decision.RuleMatched {
			outcome := input.Policy.Spec.Rules[i].Outcome
			ruleResults[i].Outcome = &outcome
		}
	}

	reduction, err := decision.Reduce(input.Policy, ruleResults)
	if err != nil {
		return decision.Decision{}, fmt.Errorf("reduce decision: %w", err)
	}
	result := decision.Decision{
		APIVersion:  decision.APIVersion,
		Kind:        decision.KindDecision,
		Outcome:     reduction.Outcome,
		Policy:      decision.PolicyIdentity{Name: input.Policy.Metadata.Name, Digest: policyDigest, Version: input.PolicyVersion},
		RuleResults: ruleResults,
		ReasonCodes: append([]string(nil), reduction.ReasonCodes...),
		Failure:     cloneFailure(reduction.Failure),
		Evaluator: &decision.Evaluator{
			ProfileDigest:  input.ProfileDigest,
			ProfileVersion: input.ProfileVersion,
			Adapter:        evidence.Metadata.AdapterID,
			Mode:           decision.EvaluatorMode(evidence.Metadata.Mode),
			Synthetic:      evidence.Metadata.Synthetic,
			Provider:       evidence.Metadata.Provider,
			Model:          evidence.Metadata.Model,
			FixtureSet:     evidence.Metadata.FixtureSet,
			FixtureVersion: evidence.Metadata.FixtureVersion,
			Route:          []string{evidence.Metadata.AdapterID},
			Attempts:       1,
		},
	}
	if err := result.ValidateAgainst(input.Policy); err != nil {
		return decision.Decision{}, fmt.Errorf("validate decision: %w", err)
	}
	return result, nil
}

func cloneConfidence(source *float64) *float64 {
	if source == nil {
		return nil
	}
	clone := *source
	return &clone
}

func cloneFailure(source *decision.Failure) *decision.Failure {
	if source == nil {
		return nil
	}
	clone := *source
	return &clone
}
