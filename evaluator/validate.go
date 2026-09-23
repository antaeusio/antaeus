package evaluator

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"math"
	"regexp"
	"strings"
	"unicode/utf8"
)

var (
	policyNamePattern = regexp.MustCompile(`^[a-z][a-z0-9-]{0,62}$`)
	ruleIDPattern     = regexp.MustCompile(`^[a-z][a-z0-9._-]{0,63}$`)
	digestPattern     = regexp.MustCompile(`^sha256:[0-9a-f]{64}$`)
	reasonCodePattern = regexp.MustCompile(`^[a-z][a-z0-9_.-]{0,127}$`)
)

// Validate checks a programmatically constructed evaluator request.
func (r Request) Validate() error {
	if !policyNamePattern.MatchString(r.PolicyName) {
		return fmt.Errorf("policy name must match [a-z][a-z0-9-]{0,62}")
	}
	if !digestPattern.MatchString(r.PolicyDigest) {
		return fmt.Errorf("policy digest must be lowercase sha256:<64 hex digits>")
	}
	if !digestPattern.MatchString(r.ProfileDigest) {
		return fmt.Errorf("profile digest must be lowercase sha256:<64 hex digits>")
	}
	if len(r.CanonicalInput) == 0 || len(r.CanonicalInput) > MaxInputBytes || !json.Valid(r.CanonicalInput) {
		return fmt.Errorf("canonical input must be valid JSON within %d bytes", MaxInputBytes)
	}
	var inputObject map[string]json.RawMessage
	if err := json.Unmarshal(r.CanonicalInput, &inputObject); err != nil || inputObject == nil {
		return fmt.Errorf("canonical input must be a JSON object")
	}
	if len(r.Rules) == 0 || len(r.Rules) > MaxRules {
		return fmt.Errorf("rules must contain 1 to %d entries", MaxRules)
	}
	seen := make(map[string]struct{}, len(r.Rules))
	for i, rule := range r.Rules {
		if !ruleIDPattern.MatchString(rule.ID) {
			return fmt.Errorf("rule %d id is invalid", i)
		}
		if _, exists := seen[rule.ID]; exists {
			return fmt.Errorf("rule id %q is duplicated", rule.ID)
		}
		seen[rule.ID] = struct{}{}
		if !utf8.ValidString(rule.When) || strings.TrimSpace(rule.When) == "" || utf8.RuneCountInString(rule.When) > MaxConditionLength {
			return fmt.Errorf("rule %q condition must contain 1 to %d non-whitespace Unicode code points", rule.ID, MaxConditionLength)
		}
	}
	if r.Deadline.IsZero() {
		return fmt.Errorf("deadline is required")
	}
	if err := validateNonBlank("correlation id", r.CorrelationID, 128); err != nil {
		return err
	}
	return nil
}

// InputDigest returns the SHA-256 identity of the exact canonical input bytes.
func (r Request) InputDigest() (string, error) {
	if err := r.Validate(); err != nil {
		return "", err
	}
	sum := sha256.Sum256(r.CanonicalInput)
	return "sha256:" + hex.EncodeToString(sum[:]), nil
}

// ValidateResult verifies exact rule coverage, order, and bounded metadata.
func ValidateResult(request Request, result Result) error {
	if err := request.Validate(); err != nil {
		return fmt.Errorf("validate request: %w", err)
	}
	if len(result.RuleResults) != len(request.Rules) {
		return fmt.Errorf("result count %d does not match request rule count %d", len(result.RuleResults), len(request.Rules))
	}
	for i, got := range result.RuleResults {
		if got.RuleID != request.Rules[i].ID {
			return fmt.Errorf("result %d has rule id %q, want %q", i, got.RuleID, request.Rules[i].ID)
		}
		if !got.Status.Valid() {
			return fmt.Errorf("result %d has invalid status %q", i, got.Status)
		}
		if got.Confidence != nil && (math.IsNaN(*got.Confidence) || math.IsInf(*got.Confidence, 0) || *got.Confidence < 0 || *got.Confidence > 1) {
			return fmt.Errorf("result %d confidence must be a finite number from 0 through 1", i)
		}
		if err := validateReasonCodes(got.ReasonCodes); err != nil {
			return fmt.Errorf("result %d: %w", i, err)
		}
		if !utf8.ValidString(got.Message) || utf8.RuneCountInString(got.Message) > MaxMessageLength {
			return fmt.Errorf("result %d message must contain at most %d Unicode code points", i, MaxMessageLength)
		}
	}
	if err := validateNonBlank("adapter id", result.Metadata.AdapterID, 128); err != nil {
		return err
	}
	if err := validateNonBlank("adapter version", result.Metadata.AdapterVersion, 128); err != nil {
		return err
	}
	if !result.Metadata.Mode.Valid() {
		return fmt.Errorf("evaluator mode %q is invalid", result.Metadata.Mode)
	}
	if result.Metadata.Mode == ModeDeterministicFixture && !result.Metadata.Synthetic {
		return fmt.Errorf("deterministic fixture results must be synthetic")
	}
	if result.Metadata.Mode == ModeSemantic && result.Metadata.Synthetic {
		return fmt.Errorf("semantic evaluator results cannot be marked synthetic")
	}
	if result.Metadata.Mode == ModeDeterministicFixture {
		if err := validateNonBlank("fixture set", result.Metadata.FixtureSet, 64); err != nil {
			return err
		}
		if err := validateNonBlank("fixture version", result.Metadata.FixtureVersion, 128); err != nil {
			return err
		}
	} else if result.Metadata.FixtureSet != "" || result.Metadata.FixtureVersion != "" {
		return fmt.Errorf("fixture identity is allowed only for deterministic fixture results")
	}
	if result.Metadata.Provider != "" {
		if err := validateNonBlank("provider", result.Metadata.Provider, 128); err != nil {
			return err
		}
	}
	if result.Metadata.Model != "" {
		if err := validateNonBlank("model", result.Metadata.Model, 256); err != nil {
			return err
		}
	}
	if result.Metadata.RequestID != "" {
		if err := validateNonBlank("request id", result.Metadata.RequestID, 128); err != nil {
			return err
		}
	}
	if result.Metadata.ModelRevision != "" {
		if err := validateNonBlank("model revision", result.Metadata.ModelRevision, 256); err != nil {
			return err
		}
	}
	return nil
}

func validateReasonCodes(codes []string) error {
	if len(codes) == 0 || len(codes) > MaxReasonCodes {
		return fmt.Errorf("reason codes must contain 1 to %d entries", MaxReasonCodes)
	}
	seen := make(map[string]struct{}, len(codes))
	for _, code := range codes {
		if !reasonCodePattern.MatchString(code) {
			return fmt.Errorf("reason code %q is invalid", code)
		}
		if _, exists := seen[code]; exists {
			return fmt.Errorf("reason code %q is duplicated", code)
		}
		seen[code] = struct{}{}
	}
	return nil
}

func validateNonBlank(name, value string, maxRunes int) error {
	if !utf8.ValidString(value) || strings.TrimSpace(value) == "" || utf8.RuneCountInString(value) > maxRunes {
		return fmt.Errorf("%s must contain 1 to %d non-whitespace Unicode code points", name, maxRunes)
	}
	return nil
}
