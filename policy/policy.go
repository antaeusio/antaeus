// Package policy defines the portable Antaeus policy artifact.
package policy

const (
	// APIVersion is the first published policy contract version.
	APIVersion = "policy.antaeus.io/v0alpha1"
	// Kind is the policy contract discriminator.
	Kind = "Policy"

	MaxSourceBytes       = 1 << 20
	MaxRules             = 256
	MaxDescriptionLength = 4096
	MaxConditionLength   = 16384
)

// Outcome is a policy-authored terminal judgment.
type Outcome string

const (
	OutcomeAllow  Outcome = "allow"
	OutcomeReview Outcome = "review"
	OutcomeDeny   Outcome = "deny"
)

// Artifact is the provider-neutral policy contract.
type Artifact struct {
	APIVersion string   `json:"apiVersion" yaml:"apiVersion"`
	Kind       string   `json:"kind" yaml:"kind"`
	Metadata   Metadata `json:"metadata" yaml:"metadata"`
	Spec       Spec     `json:"spec" yaml:"spec"`
}

// Metadata identifies a policy independently of a registry version.
type Metadata struct {
	Name        string  `json:"name" yaml:"name"`
	Description *string `json:"description,omitempty" yaml:"description,omitempty"`
}

// Spec contains the complete v0alpha1 policy behavior.
type Spec struct {
	DefaultOutcome Outcome `json:"defaultOutcome" yaml:"defaultOutcome"`
	Rules          []Rule  `json:"rules" yaml:"rules"`
}

// Rule binds a stable identifier and provider-neutral condition to an outcome.
type Rule struct {
	ID          string  `json:"id" yaml:"id"`
	Description *string `json:"description,omitempty" yaml:"description,omitempty"`
	When        string  `json:"when" yaml:"when"`
	Outcome     Outcome `json:"outcome" yaml:"outcome"`
}

// Valid reports whether the outcome is legal in a policy artifact.
func (o Outcome) Valid() bool {
	switch o {
	case OutcomeAllow, OutcomeReview, OutcomeDeny:
		return true
	default:
		return false
	}
}
