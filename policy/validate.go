package policy

import (
	"fmt"
	"regexp"
	"strings"
	"unicode/utf8"
)

var (
	namePattern   = regexp.MustCompile(`^[a-z][a-z0-9-]{0,62}$`)
	ruleIDPattern = regexp.MustCompile(`^[a-z][a-z0-9._-]{0,63}$`)
)

// ValidationError identifies one contract violation at a stable JSON path.
type ValidationError struct {
	Path    string
	Code    string
	Message string
}

func (e *ValidationError) Error() string {
	return fmt.Sprintf("%s: %s", e.Path, e.Message)
}

// Validate checks semantic invariants that JSON Schema cannot express, as well
// as the structural invariants needed by programmatically constructed values.
func (a Artifact) Validate() error {
	if a.APIVersion != APIVersion {
		return invalid("$.apiVersion", "version.unsupported", "must equal "+APIVersion)
	}
	if a.Kind != Kind {
		return invalid("$.kind", "kind.invalid", "must equal "+Kind)
	}
	if !namePattern.MatchString(a.Metadata.Name) {
		return invalid("$.metadata.name", "name.invalid", "must match [a-z][a-z0-9-]{0,62}")
	}
	if a.Metadata.Description != nil {
		if err := validateText("$.metadata.description", *a.Metadata.Description, MaxDescriptionLength, false); err != nil {
			return err
		}
	}
	if !a.Spec.DefaultOutcome.Valid() {
		return invalid("$.spec.defaultOutcome", "outcome.invalid", "must be allow, review, or deny")
	}
	if len(a.Spec.Rules) == 0 {
		return invalid("$.spec.rules", "rules.empty", "must contain at least one rule")
	}
	if len(a.Spec.Rules) > MaxRules {
		return invalid("$.spec.rules", "rules.limit", fmt.Sprintf("must contain at most %d rules", MaxRules))
	}

	seen := make(map[string]struct{}, len(a.Spec.Rules))
	for i, rule := range a.Spec.Rules {
		path := fmt.Sprintf("$.spec.rules[%d]", i)
		if !ruleIDPattern.MatchString(rule.ID) {
			return invalid(path+".id", "rule_id.invalid", "must match [a-z][a-z0-9._-]{0,63}")
		}
		if _, exists := seen[rule.ID]; exists {
			return invalid(path+".id", "rule_id.duplicate", "must be unique within the policy")
		}
		seen[rule.ID] = struct{}{}
		if rule.Description != nil {
			if err := validateText(path+".description", *rule.Description, MaxDescriptionLength, false); err != nil {
				return err
			}
		}
		if err := validateText(path+".when", rule.When, MaxConditionLength, true); err != nil {
			return err
		}
		if !rule.Outcome.Valid() {
			return invalid(path+".outcome", "outcome.invalid", "must be allow, review, or deny")
		}
	}
	return nil
}

func validateText(path, value string, maxRunes int, nonBlank bool) error {
	if !utf8.ValidString(value) {
		return invalid(path, "string.invalid_utf8", "must contain valid UTF-8")
	}
	if nonBlank && strings.TrimSpace(value) == "" {
		return invalid(path, "string.empty", "must contain non-whitespace text")
	}
	if utf8.RuneCountInString(value) > maxRunes {
		return invalid(path, "string.limit", fmt.Sprintf("must contain at most %d Unicode code points", maxRunes))
	}
	return nil
}

func invalid(path, code, message string) *ValidationError {
	return &ValidationError{Path: path, Code: code, Message: message}
}
