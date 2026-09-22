package decision

import (
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
	extensionPattern  = regexp.MustCompile(`^[a-z0-9](?:[a-z0-9-]*[a-z0-9])?(?:\.[a-z0-9](?:[a-z0-9-]*[a-z0-9])?)+$`)
	fixtureSetPattern = regexp.MustCompile(`^[a-z][a-z0-9._-]{0,63}$`)
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

// Validate checks a programmatically constructed DecisionRequest.
func (r DecisionRequest) Validate() error {
	if r.APIVersion != APIVersion {
		return invalid("$.apiVersion", "version.unsupported", "must equal "+APIVersion)
	}
	if r.Kind != KindRequest {
		return invalid("$.kind", "kind.invalid", "must equal "+KindRequest)
	}
	if (r.Policy.Inline == nil) == (r.Policy.Reference == nil) {
		return invalid("$.policy", "policy_selector.invalid", "must contain exactly one of inline or reference")
	}
	if r.Policy.Inline != nil {
		if err := r.Policy.Inline.Validate(); err != nil {
			return invalid("$.policy.inline", "policy.invalid", err.Error())
		}
	}
	if r.Policy.Reference != nil {
		if err := r.Policy.Reference.validate("$.policy.reference"); err != nil {
			return err
		}
	}
	if r.Input == nil {
		return invalid("$.input", "input.missing", "must be a JSON object")
	}
	for name, raw := range r.Input {
		if !utf8.ValidString(name) || !json.Valid(raw) {
			return invalid("$.input", "input.invalid", "must contain valid JSON names and values")
		}
	}
	return nil
}

// Validate checks a programmatically constructed Decision.
func (d Decision) Validate() error {
	if d.APIVersion != APIVersion {
		return invalid("$.apiVersion", "version.unsupported", "must equal "+APIVersion)
	}
	if d.Kind != KindDecision {
		return invalid("$.kind", "kind.invalid", "must equal "+KindDecision)
	}
	if !d.Outcome.Valid() {
		return invalid("$.outcome", "outcome.invalid", "must be allow, review, deny, or failure")
	}
	if err := d.Policy.validate("$.policy"); err != nil {
		return err
	}
	if len(d.RuleResults) == 0 || len(d.RuleResults) > MaxRuleResults {
		return invalid("$.ruleResults", "rule_results.limit", fmt.Sprintf("must contain 1 to %d results", MaxRuleResults))
	}
	seenRuleIDs := make(map[string]struct{}, len(d.RuleResults))
	for i := range d.RuleResults {
		if err := d.RuleResults[i].validate(fmt.Sprintf("$.ruleResults[%d]", i)); err != nil {
			return err
		}
		if _, exists := seenRuleIDs[d.RuleResults[i].RuleID]; exists {
			return invalid(fmt.Sprintf("$.ruleResults[%d].ruleId", i), "rule_id.duplicate", "must be unique within the Decision")
		}
		seenRuleIDs[d.RuleResults[i].RuleID] = struct{}{}
	}
	if err := validateReasonCodes("$.reasonCodes", d.ReasonCodes); err != nil {
		return err
	}
	if d.Outcome == OutcomeFailure {
		if d.Failure == nil {
			return invalid("$.failure", "failure.missing", "is required when outcome is failure")
		}
		if err := d.Failure.validate("$.failure"); err != nil {
			return err
		}
		if !contains(d.ReasonCodes, d.Failure.Code) {
			return invalid("$.reasonCodes", "failure_code.missing", "must include the structured failure code")
		}
	} else if d.Failure != nil {
		return invalid("$.failure", "failure.unexpected", "is allowed only when outcome is failure")
	}
	if d.Evaluator != nil {
		if err := d.Evaluator.validate("$.evaluator"); err != nil {
			return err
		}
	}
	if len(d.Extensions) > MaxExtensions {
		return invalid("$.extensions", "extensions.limit", fmt.Sprintf("must contain at most %d entries", MaxExtensions))
	}
	for name, raw := range d.Extensions {
		if !extensionPattern.MatchString(name) {
			return invalid("$.extensions", "extension_name.invalid", "keys must be reverse-DNS names")
		}
		if !json.Valid(raw) {
			return invalid("$.extensions."+name, "extension.invalid", "must contain valid JSON")
		}
	}
	return nil
}

func (p PolicyIdentity) validate(path string) error {
	if !policyNamePattern.MatchString(p.Name) {
		return invalid(path+".name", "name.invalid", "must match [a-z][a-z0-9-]{0,62}")
	}
	if !digestPattern.MatchString(p.Digest) {
		return invalid(path+".digest", "digest.invalid", "must be lowercase sha256:<64 hex digits>")
	}
	if p.Version != "" {
		if !utf8.ValidString(p.Version) || utf8.RuneCountInString(p.Version) > 128 {
			return invalid(path+".version", "version.invalid", "must contain 1 to 128 Unicode code points")
		}
	}
	return nil
}

func (r RuleResult) validate(path string) error {
	if !ruleIDPattern.MatchString(r.RuleID) {
		return invalid(path+".ruleId", "rule_id.invalid", "must match [a-z][a-z0-9._-]{0,63}")
	}
	if !r.Status.Valid() {
		return invalid(path+".status", "rule_status.invalid", "must be matched, not_matched, indeterminate, or failed")
	}
	if r.Status == RuleMatched {
		if r.Outcome == nil || !r.Outcome.Valid() {
			return invalid(path+".outcome", "outcome.invalid", "is required and must be allow, review, or deny for a matched rule")
		}
	} else if r.Outcome != nil {
		return invalid(path+".outcome", "outcome.unexpected", "is allowed only for a matched rule")
	}
	if r.Confidence != nil && (math.IsNaN(*r.Confidence) || math.IsInf(*r.Confidence, 0) || *r.Confidence < 0 || *r.Confidence > 1) {
		return invalid(path+".confidence", "confidence.invalid", "must be a finite number from 0 through 1")
	}
	if err := validateReasonCodes(path+".reasonCodes", r.ReasonCodes); err != nil {
		return err
	}
	return validateMessage(path+".message", r.Message)
}

func (f Failure) validate(path string) error {
	if !reasonCodePattern.MatchString(f.Code) {
		return invalid(path+".code", "failure_code.invalid", "must be a stable reason code")
	}
	switch f.Stage {
	case "configuration", "policy_resolution", "evaluation", "reduction", "internal":
	default:
		return invalid(path+".stage", "failure_stage.invalid", "is not a supported failure stage")
	}
	return validateMessage(path+".message", f.Message)
}

func (e Evaluator) validate(path string) error {
	if !digestPattern.MatchString(e.ProfileDigest) {
		return invalid(path+".profileDigest", "digest.invalid", "must be lowercase sha256:<64 hex digits>")
	}
	if e.ProfileVersion != "" && (!utf8.ValidString(e.ProfileVersion) || utf8.RuneCountInString(e.ProfileVersion) > 128) {
		return invalid(path+".profileVersion", "version.invalid", "must contain at most 128 Unicode code points")
	}
	if err := validateBoundedNonBlank(path+".adapter", e.Adapter, 128); err != nil {
		return err
	}
	if err := validateBoundedNonBlank(path+".adapterVersion", e.AdapterVersion, 128); err != nil {
		return err
	}
	if !e.Mode.Valid() {
		return invalid(path+".mode", "evaluator_mode.invalid", "must be semantic or deterministic-fixture")
	}
	if e.Synthetic == nil {
		return invalid(path+".synthetic", "synthetic.missing", "is required")
	}
	if e.Mode == EvaluatorModeDeterministicFixture {
		if !*e.Synthetic {
			return invalid(path+".synthetic", "synthetic.required", "must be true for deterministic fixture results")
		}
		if e.FixtureSet == nil || !fixtureSetPattern.MatchString(*e.FixtureSet) {
			return invalid(path+".fixtureSet", "fixture_set.invalid", "must match [a-z][a-z0-9._-]{0,63}")
		}
		if e.FixtureVersion == nil {
			return invalid(path+".fixtureVersion", "fixture_version.missing", "is required for deterministic fixture results")
		}
		if err := validateBoundedNonBlank(path+".fixtureVersion", *e.FixtureVersion, 128); err != nil {
			return err
		}
	} else {
		if *e.Synthetic {
			return invalid(path+".synthetic", "synthetic.unexpected", "must be false for semantic evaluator results")
		}
		if e.FixtureSet != nil {
			return invalid(path+".fixtureSet", "fixture_identity.unexpected", "is allowed only for deterministic fixture results")
		}
		if e.FixtureVersion != nil {
			return invalid(path+".fixtureVersion", "fixture_identity.unexpected", "is allowed only for deterministic fixture results")
		}
	}
	if e.Provider != "" {
		if err := validateBoundedNonBlank(path+".provider", e.Provider, 128); err != nil {
			return err
		}
	}
	if e.Model != "" {
		if err := validateBoundedNonBlank(path+".model", e.Model, 256); err != nil {
			return err
		}
	}
	if len(e.Route) == 0 || len(e.Route) > MaxEvaluatorRoute {
		return invalid(path+".route", "route.limit", fmt.Sprintf("must contain 1 to %d entries", MaxEvaluatorRoute))
	}
	for i, route := range e.Route {
		if err := validateBoundedNonBlank(fmt.Sprintf("%s.route[%d]", path, i), route, 128); err != nil {
			return err
		}
	}
	if e.Attempts < 1 || e.Attempts > MaxEvaluatorAttempts {
		return invalid(path+".attempts", "attempts.limit", fmt.Sprintf("must be from 1 through %d", MaxEvaluatorAttempts))
	}
	return nil
}

func validateReasonCodes(path string, codes []string) error {
	if len(codes) == 0 || len(codes) > MaxReasonCodes {
		return invalid(path, "reason_codes.limit", fmt.Sprintf("must contain 1 to %d entries", MaxReasonCodes))
	}
	seen := make(map[string]struct{}, len(codes))
	for i, code := range codes {
		if !reasonCodePattern.MatchString(code) {
			return invalid(fmt.Sprintf("%s[%d]", path, i), "reason_code.invalid", "must be a stable reason code")
		}
		if _, exists := seen[code]; exists {
			return invalid(fmt.Sprintf("%s[%d]", path, i), "reason_code.duplicate", "must be unique")
		}
		seen[code] = struct{}{}
	}
	return nil
}

func validateMessage(path, message string) error {
	if message == "" {
		return nil
	}
	if !utf8.ValidString(message) || utf8.RuneCountInString(message) > MaxMessageLength {
		return invalid(path, "message.invalid", fmt.Sprintf("must contain at most %d valid Unicode code points", MaxMessageLength))
	}
	return nil
}

func validateBoundedNonBlank(path, value string, maxRunes int) error {
	if !utf8.ValidString(value) || strings.TrimSpace(value) == "" || utf8.RuneCountInString(value) > maxRunes {
		return invalid(path, "string.invalid", fmt.Sprintf("must contain 1 to %d non-whitespace Unicode code points", maxRunes))
	}
	return nil
}

func invalid(path, code, message string) *ValidationError {
	return &ValidationError{Path: path, Code: code, Message: message}
}

func contains(values []string, wanted string) bool {
	for _, value := range values {
		if value == wanted {
			return true
		}
	}
	return false
}
