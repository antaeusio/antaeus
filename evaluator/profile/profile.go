// Package profile defines immutable, provider-neutral evaluator profiles.
package profile

import "encoding/json"

const (
	APIVersion = "evaluator.antaeus.io/v0alpha1"
	Kind       = "EvaluatorProfile"

	MaxSourceBytes       = 1 << 20
	MaxDescriptionLength = 4096
	MaxCredentialSlots   = 16
	MaxEvaluators        = 16
	MaxCapabilities      = 32
	MaxParameters        = 32
	MaxTotalTimeoutMS    = 300_000
	MaxAttemptTimeoutMS  = 120_000
	MaxAttempts          = 5
	MaxBackoffMS         = 60_000
	MaxFallbacks         = 15
)

type Mode string

const (
	ModeSemantic             Mode = "semantic"
	ModeDeterministicFixture Mode = "deterministic-fixture"
)

type TransientFailure string

const (
	FailureTimeout     TransientFailure = "timeout"
	FailureUnavailable TransientFailure = "unavailable"
	FailureThrottled   TransientFailure = "throttled"
)

type LowConfidenceAction string

const (
	LowConfidenceIndeterminate LowConfidenceAction = "indeterminate"
	LowConfidenceEscalate      LowConfidenceAction = "escalate"
)

type Artifact struct {
	APIVersion string   `json:"apiVersion"`
	Kind       string   `json:"kind"`
	Metadata   Metadata `json:"metadata"`
	Spec       Spec     `json:"spec"`
}

type Metadata struct {
	Name        string  `json:"name"`
	Description *string `json:"description,omitempty"`
}

type Spec struct {
	TotalTimeoutMS  int              `json:"totalTimeoutMs"`
	CredentialSlots []CredentialSlot `json:"credentialSlots"`
	Evaluators      []Evaluator      `json:"evaluators"`
	Routing         Routing          `json:"routing"`
}

type CredentialSlot struct {
	Name string `json:"name"`
}

type Evaluator struct {
	ID                   string               `json:"id"`
	Mode                 Mode                 `json:"mode"`
	Adapter              ComponentIdentity    `json:"adapter"`
	Protocol             ComponentIdentity    `json:"protocol"`
	RequiredCapabilities []string             `json:"requiredCapabilities"`
	TimeoutMS            int                  `json:"timeoutMs"`
	Retry                RetryPolicy          `json:"retry"`
	Provider             *string              `json:"provider,omitempty"`
	Model                *string              `json:"model,omitempty"`
	ModelRevision        *string              `json:"modelRevision,omitempty"`
	InstructionTemplate  *InstructionTemplate `json:"instructionTemplate,omitempty"`
	CredentialSlot       *string              `json:"credentialSlot,omitempty"`
	Parameters           map[string]any       `json:"parameters"`
}

type ComponentIdentity struct {
	ID      string `json:"id"`
	Version string `json:"version"`
}

type RetryPolicy struct {
	MaxAttempts      int                `json:"maxAttempts"`
	RetryOn          []TransientFailure `json:"retryOn"`
	InitialBackoffMS *int               `json:"initialBackoffMs,omitempty"`
	MaxBackoffMS     *int               `json:"maxBackoffMs,omitempty"`
	Multiplier       *float64           `json:"multiplier,omitempty"`
}

type InstructionTemplate struct {
	ID     *string `json:"id,omitempty"`
	Digest string  `json:"digest"`
}

type Routing struct {
	Primary    string             `json:"primary"`
	Escalation *string            `json:"escalation,omitempty"`
	Fallbacks  []string           `json:"fallbacks"`
	FallbackOn []TransientFailure `json:"fallbackOn,omitempty"`
	Confidence Confidence         `json:"confidence"`
	Terminal   Terminal           `json:"terminal"`
}

type Confidence struct {
	Enabled         bool                 `json:"enabled"`
	MinimumAccepted *float64             `json:"minimumAccepted,omitempty"`
	OnLowConfidence *LowConfidenceAction `json:"onLowConfidence,omitempty"`
}

type Terminal struct {
	OnIndeterminate string `json:"onIndeterminate"`
	OnFailure       string `json:"onFailure"`
}

// ParameterValidator validates one semantic adapter's JCS-canonical parameter
// object.
type ParameterValidator interface {
	ValidateParameters(parameters json.RawMessage) error
}

// ParameterRegistry resolves validators by exact adapter ID and version.
// Execution must not begin until every semantic evaluator has a registered
// validator and its parameters pass validation.
type ParameterRegistry interface {
	ValidatorFor(adapter ComponentIdentity) (ParameterValidator, bool)
}

// ParameterValidators is an exact adapter/version validator registry.
type ParameterValidators map[ComponentIdentity]ParameterValidator

func (v ParameterValidators) ValidatorFor(adapter ComponentIdentity) (ParameterValidator, bool) {
	validator, exists := v[adapter]
	return validator, exists && validator != nil
}
