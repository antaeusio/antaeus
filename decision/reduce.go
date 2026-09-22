package decision

import (
	"fmt"

	"github.com/antaeusio/antaeus/policy"
)

// Reduction is the deterministic policy result derived from completed rule
// evaluations. It does not perform fallback routing or application side effects.
type Reduction struct {
	Outcome     Outcome
	ReasonCodes []string
	Failure     *Failure
}

// Reduce applies the v0alpha1 precedence rules to one result per policy rule.
func Reduce(artifact policy.Artifact, results []RuleResult) (Reduction, error) {
	if err := artifact.Validate(); err != nil {
		return Reduction{}, fmt.Errorf("validate policy: %w", err)
	}
	if len(results) != len(artifact.Spec.Rules) {
		return Reduction{}, fmt.Errorf("rule result count %d does not match policy rule count %d", len(results), len(artifact.Spec.Rules))
	}

	for i := range results {
		if err := results[i].validate(fmt.Sprintf("$.ruleResults[%d]", i)); err != nil {
			return Reduction{}, err
		}
		rule := artifact.Spec.Rules[i]
		if results[i].RuleID != rule.ID {
			return Reduction{}, fmt.Errorf("rule result %d has id %q, want %q", i, results[i].RuleID, rule.ID)
		}
		if results[i].Status == RuleMatched && *results[i].Outcome != rule.Outcome {
			return Reduction{}, fmt.Errorf("matched rule %q reports outcome %q, want policy outcome %q", rule.ID, *results[i].Outcome, rule.Outcome)
		}
	}

	if anyMatched(results, policy.OutcomeDeny) {
		return Reduction{Outcome: OutcomeDeny, ReasonCodes: []string{"policy.deny_rule_matched"}}, nil
	}
	if anyUnresolved(results) {
		failure := &Failure{
			Code:      "evaluation.unresolved_rule",
			Stage:     "evaluation",
			Retryable: false,
		}
		return Reduction{Outcome: OutcomeFailure, ReasonCodes: []string{failure.Code}, Failure: failure}, nil
	}
	if anyMatched(results, policy.OutcomeReview) {
		return Reduction{Outcome: OutcomeReview, ReasonCodes: []string{"policy.review_rule_matched"}}, nil
	}
	if anyMatched(results, policy.OutcomeAllow) {
		return Reduction{Outcome: OutcomeAllow, ReasonCodes: []string{"policy.allow_rule_matched"}}, nil
	}
	return Reduction{
		Outcome:     outcomeFromPolicy(artifact.Spec.DefaultOutcome),
		ReasonCodes: []string{"policy.default_outcome"},
	}, nil
}

func anyMatched(results []RuleResult, outcome policy.Outcome) bool {
	for _, result := range results {
		if result.Status == RuleMatched && result.Outcome != nil && *result.Outcome == outcome {
			return true
		}
	}
	return false
}

func anyUnresolved(results []RuleResult) bool {
	for _, result := range results {
		if result.Status == RuleIndeterminate || result.Status == RuleFailed {
			return true
		}
	}
	return false
}

func outcomeFromPolicy(outcome policy.Outcome) Outcome {
	return Outcome(outcome)
}
