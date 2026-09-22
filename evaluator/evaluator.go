// Package evaluator defines the provider-neutral semantic evaluation boundary.
package evaluator

import (
	"context"
	"encoding/json"
	"time"

	"github.com/antaeusio/antaeus/decision"
)

const (
	MaxInputBytes      = 1 << 20
	MaxRules           = 256
	MaxConditionLength = 16_384
	MaxReasonCodes     = 16
	MaxMessageLength   = 4_096
)

// Mode identifies whether an evaluator performs semantic inference or returns
// synthetic fixture evidence.
type Mode string

const (
	ModeSemantic             Mode = "semantic"
	ModeDeterministicFixture Mode = "deterministic-fixture"
)

// Evaluator returns normalized evidence for every requested rule. It does not
// receive or choose policy outcomes and performs no policy reduction. An
// implementation must honor context cancellation and return promptly after the
// context is done; callers do not detach or abandon adapter goroutines.
type Evaluator interface {
	Evaluate(context.Context, Request) (Result, error)
}

// Request contains only provider-neutral evidence inputs. CanonicalInput must
// be verified canonical JSON supplied by the caller, and Rules must be derived
// in order from the same artifact identified by PolicyDigest.
type Request struct {
	PolicyName     string
	PolicyDigest   string
	CanonicalInput json.RawMessage
	Rules          []Rule
	Deadline       time.Time
	CorrelationID  string
	ProfileDigest  string
}

// Rule asks whether one provider-neutral policy condition applies. It excludes
// the rule's configured allow, review, or deny outcome by design.
type Rule struct {
	ID   string
	When string
}

// Result is one complete, normalized evaluator response.
type Result struct {
	RuleResults []RuleResult
	Metadata    Metadata
}

// RuleResult records evidence for one requested rule without a policy outcome.
type RuleResult struct {
	RuleID      string              `json:"ruleId"`
	Status      decision.RuleStatus `json:"status"`
	Confidence  *float64            `json:"confidence,omitempty"`
	ReasonCodes []string            `json:"reasonCodes"`
	Message     string              `json:"message,omitempty"`
}

// Metadata records the effective adapter without credentials or raw provider
// payloads.
type Metadata struct {
	AdapterID      string
	AdapterVersion string
	Mode           Mode
	Synthetic      bool
	Provider       string
	Model          string
	RequestID      string
	FixtureSet     string
	FixtureVersion string
}

// Error is a stable adapter failure safe for routing and bounded diagnostics.
type Error struct {
	Code      string
	Retryable bool
	Message   string
}

func (e *Error) Error() string {
	if e.Message == "" {
		return e.Code
	}
	return e.Code + ": " + e.Message
}

// Valid reports whether the evaluator mode is part of v0alpha1.
func (m Mode) Valid() bool {
	return m == ModeSemantic || m == ModeDeterministicFixture
}
