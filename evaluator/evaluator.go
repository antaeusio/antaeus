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
	// Usage is provider accounting, independent of rule validity and outcome.
	// Adapters may return it alongside an error. Nil means no usage reporting.
	Usage *Usage
}

// MaxUsageTokens is the largest interoperable JSON integer.
const MaxUsageTokens int64 = 1<<53 - 1

type UsageStatus string

const (
	UsageReported    UsageStatus = "reported"
	UsageUnavailable UsageStatus = "unavailable"
	UsageInvalid     UsageStatus = "invalid"
)

// Usage contains provider-reported token counts for one attempt, never estimates.
// Status is reported, unavailable, or invalid. Only reported carries counters;
// input is required, output is optional. A reported zero differs from unknown.
type Usage struct {
	Status       UsageStatus `json:"status"`
	InputTokens  *int64      `json:"inputTokens,omitempty"`
	OutputTokens *int64      `json:"outputTokens,omitempty"`
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
	ModelRevision  string
	RequestID      string
	FixtureSet     string
	FixtureVersion string
}

// Error is a stable adapter failure safe for routing and bounded diagnostics.
// Code must be a fixed, public diagnostic identifier, never derived from input
// or credentials. The usage extension may publish it for any traced attempt.
type Error struct {
	Code      string
	Retryable bool
	Message   string
	// RetryAfter is the minimum wait the provider requires before another
	// attempt, or zero when it states none. The runner never retries sooner
	// and skips a retry that cannot start within the remaining deadline.
	RetryAfter time.Duration
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
