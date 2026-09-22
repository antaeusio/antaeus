// Package decision defines the portable Antaeus request and decision contracts.
package decision

import (
	"encoding/json"

	"github.com/antaeusio/antaeus/policy"
)

const (
	APIVersion           = "decision.antaeus.io/v0alpha1"
	KindRequest          = "DecisionRequest"
	KindDecision         = "Decision"
	MaxRuleResults       = policy.MaxRules
	MaxReasonCodes       = 16
	MaxMessageLength     = 4096
	MaxExtensions        = 32
	MaxEvaluatorRoute    = 16
	MaxEvaluatorAttempts = 64
)

// Outcome is the terminal result of an accepted evaluation.
type Outcome string

const (
	OutcomeAllow   Outcome = "allow"
	OutcomeReview  Outcome = "review"
	OutcomeDeny    Outcome = "deny"
	OutcomeFailure Outcome = "failure"
)

// RuleStatus records the evaluator state for one policy rule.
type RuleStatus string

const (
	RuleMatched       RuleStatus = "matched"
	RuleNotMatched    RuleStatus = "not_matched"
	RuleIndeterminate RuleStatus = "indeterminate"
	RuleFailed        RuleStatus = "failed"
)

// EvaluatorMode distinguishes semantic evidence from synthetic fixture data.
type EvaluatorMode string

const (
	EvaluatorModeSemantic             EvaluatorMode = "semantic"
	EvaluatorModeDeterministicFixture EvaluatorMode = "deterministic-fixture"
)

// DecisionRequest is the portable synchronous evaluation request.
type DecisionRequest struct {
	APIVersion string                     `json:"apiVersion"`
	Kind       string                     `json:"kind"`
	Policy     PolicySelector             `json:"policy"`
	Input      map[string]json.RawMessage `json:"input"`
}

// PolicySelector contains exactly one inline artifact or immutable reference.
type PolicySelector struct {
	Inline    *policy.Artifact `json:"inline,omitempty"`
	Reference *PolicyIdentity  `json:"reference,omitempty"`
}

// PolicyIdentity identifies validated canonical policy content.
type PolicyIdentity struct {
	Name    string `json:"name"`
	Digest  string `json:"digest"`
	Version string `json:"version,omitempty"`
}

// Decision is the provider-neutral terminal evaluation record.
type Decision struct {
	APIVersion  string                     `json:"apiVersion"`
	Kind        string                     `json:"kind"`
	Outcome     Outcome                    `json:"outcome"`
	Policy      PolicyIdentity             `json:"policy"`
	RuleResults []RuleResult               `json:"ruleResults"`
	ReasonCodes []string                   `json:"reasonCodes"`
	Failure     *Failure                   `json:"failure,omitempty"`
	Evaluator   *Evaluator                 `json:"evaluator,omitempty"`
	Extensions  map[string]json.RawMessage `json:"extensions,omitempty"`
}

// RuleResult records one rule evaluation in policy order.
type RuleResult struct {
	RuleID      string          `json:"ruleId"`
	Status      RuleStatus      `json:"status"`
	Outcome     *policy.Outcome `json:"outcome,omitempty"`
	Confidence  *float64        `json:"confidence,omitempty"`
	ReasonCodes []string        `json:"reasonCodes"`
	Message     string          `json:"message,omitempty"`
}

// Failure explains why evaluation did not produce a policy judgment.
type Failure struct {
	Code      string `json:"code"`
	Stage     string `json:"stage"`
	Retryable bool   `json:"retryable"`
	Message   string `json:"message,omitempty"`
}

// Evaluator identifies the immutable execution profile and effective route.
type Evaluator struct {
	ProfileDigest  string        `json:"profileDigest"`
	ProfileVersion string        `json:"profileVersion,omitempty"`
	Adapter        string        `json:"adapter"`
	Mode           EvaluatorMode `json:"mode"`
	Synthetic      bool          `json:"synthetic"`
	Provider       string        `json:"provider,omitempty"`
	Model          string        `json:"model,omitempty"`
	FixtureSet     string        `json:"fixtureSet,omitempty"`
	FixtureVersion string        `json:"fixtureVersion,omitempty"`
	Route          []string      `json:"route"`
	Attempts       int           `json:"attempts"`
	Fallback       bool          `json:"fallback,omitempty"`
}

// Valid reports whether the outcome is legal in a Decision.
func (o Outcome) Valid() bool {
	switch o {
	case OutcomeAllow, OutcomeReview, OutcomeDeny, OutcomeFailure:
		return true
	default:
		return false
	}
}

// Valid reports whether the rule status is legal in a Decision.
func (s RuleStatus) Valid() bool {
	switch s {
	case RuleMatched, RuleNotMatched, RuleIndeterminate, RuleFailed:
		return true
	default:
		return false
	}
}

// Valid reports whether the evaluator mode is part of v0alpha1.
func (m EvaluatorMode) Valid() bool {
	return m == EvaluatorModeSemantic || m == EvaluatorModeDeterministicFixture
}
