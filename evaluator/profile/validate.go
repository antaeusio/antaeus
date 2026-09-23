package profile

import (
	"encoding/json"
	"fmt"
	"math"
	"regexp"
	"strings"
	"unicode/utf8"
)

var (
	namePattern       = regexp.MustCompile(`^[a-z][a-z0-9-]{0,62}$`)
	localIDPattern    = regexp.MustCompile(`^[a-z][a-z0-9._-]{0,63}$`)
	namespacedPattern = regexp.MustCompile(`^[a-z0-9](?:[a-z0-9-]*[a-z0-9])?(?:\.[a-z0-9](?:[a-z0-9-]*[a-z0-9])?)+$`)
	providerPattern   = regexp.MustCompile(`^[a-z0-9][a-z0-9._-]{0,127}$`)
	modelPattern      = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._:/@+-]{0,255}$`)
	digestPattern     = regexp.MustCompile(`^sha256:[0-9a-f]{64}$`)
)

type ValidationError struct {
	Path    string
	Code    string
	Message string
}

func (e *ValidationError) Error() string {
	return fmt.Sprintf("%s: %s", e.Path, e.Message)
}

// Validate checks the closed v0alpha1 structure and relational invariants.
// Adapter-specific semantic parameters require ValidateParameters before use.
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
		if err := validateText("$.metadata.description", *a.Metadata.Description, MaxDescriptionLength); err != nil {
			return err
		}
	}
	if a.Spec.TotalTimeoutMS < 1 || a.Spec.TotalTimeoutMS > MaxTotalTimeoutMS {
		return invalid("$.spec.totalTimeoutMs", "timeout.invalid", fmt.Sprintf("must be between 1 and %d", MaxTotalTimeoutMS))
	}
	if a.Spec.CredentialSlots == nil || len(a.Spec.CredentialSlots) > MaxCredentialSlots {
		return invalid("$.spec.credentialSlots", "credential_slots.invalid", fmt.Sprintf("must contain 0 to %d entries", MaxCredentialSlots))
	}
	slots := make(map[string]struct{}, len(a.Spec.CredentialSlots))
	for i, slot := range a.Spec.CredentialSlots {
		path := fmt.Sprintf("$.spec.credentialSlots[%d].name", i)
		if !localIDPattern.MatchString(slot.Name) {
			return invalid(path, "credential_slot.invalid", "must be a lowercase local identifier")
		}
		if _, exists := slots[slot.Name]; exists {
			return invalid(path, "credential_slot.duplicate", "must be unique within the profile")
		}
		slots[slot.Name] = struct{}{}
	}
	if a.Spec.Evaluators == nil || len(a.Spec.Evaluators) == 0 || len(a.Spec.Evaluators) > MaxEvaluators {
		return invalid("$.spec.evaluators", "evaluators.invalid", fmt.Sprintf("must contain 1 to %d entries", MaxEvaluators))
	}
	evaluators := make(map[string]Evaluator, len(a.Spec.Evaluators))
	var profileMode Mode
	for i, evaluator := range a.Spec.Evaluators {
		path := fmt.Sprintf("$.spec.evaluators[%d]", i)
		if err := validateEvaluator(path, evaluator, a.Spec.TotalTimeoutMS, slots); err != nil {
			return err
		}
		if _, exists := evaluators[evaluator.ID]; exists {
			return invalid(path+".id", "evaluator_id.duplicate", "must be unique within the profile")
		}
		evaluators[evaluator.ID] = evaluator
		if i == 0 {
			profileMode = evaluator.Mode
		} else if evaluator.Mode != profileMode {
			return invalid(path+".mode", "evaluator_mode.mixed", "semantic and deterministic-fixture evaluators cannot be mixed")
		}
	}
	return validateRouting(a.Spec.Routing, evaluators)
}

// ValidateParameters validates semantic parameter objects against the exact
// adapter ID and version. It is required before executing a semantic profile.
func (a Artifact) ValidateParameters(validator ParameterValidator) error {
	if err := a.Validate(); err != nil {
		return err
	}
	hasSemantic := false
	for _, evaluator := range a.Spec.Evaluators {
		if evaluator.Mode == ModeSemantic {
			hasSemantic = true
			break
		}
	}
	if !hasSemantic {
		return nil
	}
	if validator == nil {
		return invalid("$.spec.evaluators", "adapter_schema.missing", "semantic profiles require an adapter parameter validator")
	}
	for i, evaluator := range a.Spec.Evaluators {
		if evaluator.Mode != ModeSemantic {
			continue
		}
		encoded, err := json.Marshal(evaluator.Parameters)
		if err != nil {
			return invalid(fmt.Sprintf("$.spec.evaluators[%d].parameters", i), "parameters.invalid", err.Error())
		}
		if err := validator.ValidateParameters(evaluator.Adapter, encoded); err != nil {
			return invalid(fmt.Sprintf("$.spec.evaluators[%d].parameters", i), "parameters.adapter_invalid", err.Error())
		}
	}
	return nil
}

func validateEvaluator(path string, evaluator Evaluator, totalTimeout int, slots map[string]struct{}) error {
	if !localIDPattern.MatchString(evaluator.ID) {
		return invalid(path+".id", "evaluator_id.invalid", "must be a lowercase local identifier")
	}
	if evaluator.Mode != ModeSemantic && evaluator.Mode != ModeDeterministicFixture {
		return invalid(path+".mode", "evaluator_mode.invalid", "must be semantic or deterministic-fixture")
	}
	if err := validateComponent(path+".adapter", evaluator.Adapter); err != nil {
		return err
	}
	if err := validateComponent(path+".protocol", evaluator.Protocol); err != nil {
		return err
	}
	if evaluator.RequiredCapabilities == nil || len(evaluator.RequiredCapabilities) == 0 || len(evaluator.RequiredCapabilities) > MaxCapabilities {
		return invalid(path+".requiredCapabilities", "capabilities.invalid", fmt.Sprintf("must contain 1 to %d entries", MaxCapabilities))
	}
	capabilities := make(map[string]struct{}, len(evaluator.RequiredCapabilities))
	for i, capability := range evaluator.RequiredCapabilities {
		capabilityPath := fmt.Sprintf("%s.requiredCapabilities[%d]", path, i)
		if !localIDPattern.MatchString(capability) {
			return invalid(capabilityPath, "capability.invalid", "must be a lowercase local identifier")
		}
		if _, exists := capabilities[capability]; exists {
			return invalid(capabilityPath, "capability.duplicate", "must be unique within the evaluator")
		}
		capabilities[capability] = struct{}{}
	}
	if evaluator.TimeoutMS < 1 || evaluator.TimeoutMS > MaxAttemptTimeoutMS || evaluator.TimeoutMS > totalTimeout {
		return invalid(path+".timeoutMs", "timeout.invalid", "must be positive and no greater than the per-attempt or total timeout limit")
	}
	if err := validateRetry(path+".retry", evaluator.Retry); err != nil {
		return err
	}
	if evaluator.Provider != nil && !providerPattern.MatchString(*evaluator.Provider) {
		return invalid(path+".provider", "provider.invalid", "must be a lowercase provider identifier")
	}
	for field, value := range map[string]*string{"model": evaluator.Model, "modelRevision": evaluator.ModelRevision} {
		if value != nil && (!modelPattern.MatchString(*value) || strings.Contains(*value, "://")) {
			return invalid(path+"."+field, "model.invalid", "must be a bounded model identifier, not a URL")
		}
	}
	if evaluator.InstructionTemplate != nil {
		if evaluator.InstructionTemplate.ID != nil && !validNamespacedID(*evaluator.InstructionTemplate.ID) {
			return invalid(path+".instructionTemplate.id", "template_id.invalid", "must be a namespaced identifier")
		}
		if !digestPattern.MatchString(evaluator.InstructionTemplate.Digest) {
			return invalid(path+".instructionTemplate.digest", "digest.invalid", "must be a lowercase SHA-256 digest")
		}
	}
	if evaluator.CredentialSlot != nil {
		if !localIDPattern.MatchString(*evaluator.CredentialSlot) {
			return invalid(path+".credentialSlot", "credential_slot.invalid", "must be a lowercase local identifier")
		}
		if _, exists := slots[*evaluator.CredentialSlot]; !exists {
			return invalid(path+".credentialSlot", "credential_slot.unknown", "must reference a declared credential slot")
		}
	}
	if evaluator.Parameters == nil || len(evaluator.Parameters) > MaxParameters {
		return invalid(path+".parameters", "parameters.invalid", fmt.Sprintf("must be an object with at most %d properties", MaxParameters))
	}
	if evaluator.Mode == ModeDeterministicFixture {
		return validateFixtureEvaluator(path, evaluator)
	}
	if evaluator.Adapter.ID == "io.antaeus.fixture" {
		return invalid(path+".adapter.id", "adapter.invalid", "the fixture adapter requires deterministic-fixture mode")
	}
	return nil
}

func validateFixtureEvaluator(path string, evaluator Evaluator) error {
	if evaluator.Adapter.ID != "io.antaeus.fixture" {
		return invalid(path+".adapter.id", "fixture_adapter.invalid", "deterministic-fixture mode requires io.antaeus.fixture")
	}
	if evaluator.Provider != nil || evaluator.Model != nil || evaluator.ModelRevision != nil || evaluator.InstructionTemplate != nil || evaluator.CredentialSlot != nil {
		return invalid(path, "fixture_configuration.invalid", "deterministic fixtures cannot declare provider, model, template, or credential fields")
	}
	if len(evaluator.Parameters) != 2 {
		return invalid(path+".parameters", "fixture_parameters.invalid", "must contain only fixtureSet and fixtureVersion")
	}
	fixtureSet, setOK := evaluator.Parameters["fixtureSet"].(string)
	fixtureVersion, versionOK := evaluator.Parameters["fixtureVersion"].(string)
	if !setOK || !localIDPattern.MatchString(fixtureSet) || !versionOK || !validVersion(fixtureVersion) {
		return invalid(path+".parameters", "fixture_parameters.invalid", "fixtureSet and fixtureVersion are required and must be valid strings")
	}
	return nil
}

func validateRetry(path string, retry RetryPolicy) error {
	if retry.MaxAttempts < 1 || retry.MaxAttempts > MaxAttempts || retry.RetryOn == nil {
		return invalid(path, "retry.invalid", fmt.Sprintf("maxAttempts must be 1 to %d and retryOn is required", MaxAttempts))
	}
	seen := make(map[TransientFailure]struct{}, len(retry.RetryOn))
	if len(retry.RetryOn) > 3 {
		return invalid(path+".retryOn", "retry_classes.limit", "must contain at most 3 entries")
	}
	for i, failure := range retry.RetryOn {
		if !failure.valid() {
			return invalid(fmt.Sprintf("%s.retryOn[%d]", path, i), "retry_class.invalid", "must be timeout, unavailable, or throttled")
		}
		if _, exists := seen[failure]; exists {
			return invalid(fmt.Sprintf("%s.retryOn[%d]", path, i), "retry_class.duplicate", "must be unique")
		}
		seen[failure] = struct{}{}
	}
	if retry.MaxAttempts == 1 {
		if len(retry.RetryOn) != 0 || retry.InitialBackoffMS != nil || retry.MaxBackoffMS != nil || retry.Multiplier != nil {
			return invalid(path, "retry.single_attempt", "one attempt requires empty retryOn and no backoff fields")
		}
		return nil
	}
	if len(retry.RetryOn) == 0 || retry.InitialBackoffMS == nil || retry.MaxBackoffMS == nil || retry.Multiplier == nil {
		return invalid(path, "retry.schedule_missing", "multiple attempts require retryOn and a complete backoff schedule")
	}
	if *retry.InitialBackoffMS < 0 || *retry.InitialBackoffMS > MaxBackoffMS || *retry.MaxBackoffMS < 0 || *retry.MaxBackoffMS > MaxBackoffMS || *retry.InitialBackoffMS > *retry.MaxBackoffMS {
		return invalid(path, "retry.backoff_invalid", fmt.Sprintf("backoffs must be between 0 and %d with initial no greater than maximum", MaxBackoffMS))
	}
	if math.IsNaN(*retry.Multiplier) || math.IsInf(*retry.Multiplier, 0) || *retry.Multiplier < 1 || *retry.Multiplier > 10 {
		return invalid(path+".multiplier", "retry.multiplier_invalid", "must be finite and between 1 and 10")
	}
	return nil
}

func validateRouting(routing Routing, evaluators map[string]Evaluator) error {
	if routing.Fallbacks == nil {
		return invalid("$.spec.routing.fallbacks", "fallbacks.missing", "is required")
	}
	positions := make(map[string]string)
	if err := addRoutePosition(positions, evaluators, routing.Primary, "$.spec.routing.primary"); err != nil {
		return err
	}
	if len(routing.Fallbacks) > MaxFallbacks {
		return invalid("$.spec.routing.fallbacks", "fallbacks.limit", fmt.Sprintf("must contain at most %d entries", MaxFallbacks))
	}
	if routing.Escalation != nil {
		if err := addRoutePosition(positions, evaluators, *routing.Escalation, "$.spec.routing.escalation"); err != nil {
			return err
		}
	}
	for i, id := range routing.Fallbacks {
		if err := addRoutePosition(positions, evaluators, id, fmt.Sprintf("$.spec.routing.fallbacks[%d]", i)); err != nil {
			return err
		}
	}
	if len(routing.Fallbacks) == 0 {
		if routing.FallbackOn != nil {
			return invalid("$.spec.routing.fallbackOn", "fallback_classes.unused", "must be omitted when no fallback is configured")
		}
	} else if len(routing.FallbackOn) == 0 || len(routing.FallbackOn) > 3 {
		return invalid("$.spec.routing.fallbackOn", "fallback_classes.invalid", "must contain 1 to 3 entries when fallbacks are configured")
	}
	seenFailures := make(map[TransientFailure]struct{}, len(routing.FallbackOn))
	for i, failure := range routing.FallbackOn {
		if !failure.valid() {
			return invalid(fmt.Sprintf("$.spec.routing.fallbackOn[%d]", i), "fallback_class.invalid", "must be timeout, unavailable, or throttled")
		}
		if _, exists := seenFailures[failure]; exists {
			return invalid(fmt.Sprintf("$.spec.routing.fallbackOn[%d]", i), "fallback_class.duplicate", "must be unique")
		}
		seenFailures[failure] = struct{}{}
	}
	if err := validateConfidence(routing, evaluators, positions); err != nil {
		return err
	}
	if routing.Terminal.OnIndeterminate != "failure" || routing.Terminal.OnFailure != "failure" {
		return invalid("$.spec.routing.terminal", "terminal.invalid", "v0alpha1 requires failure for indeterminate and failed evidence")
	}
	return nil
}

func validateConfidence(routing Routing, evaluators map[string]Evaluator, positions map[string]string) error {
	confidence := routing.Confidence
	if !confidence.Enabled {
		if confidence.MinimumAccepted != nil || confidence.OnLowConfidence != nil || routing.Escalation != nil {
			return invalid("$.spec.routing.confidence", "confidence.disabled", "disabled confidence cannot declare threshold, action, or escalation")
		}
		return nil
	}
	if confidence.MinimumAccepted == nil || math.IsNaN(*confidence.MinimumAccepted) || math.IsInf(*confidence.MinimumAccepted, 0) || *confidence.MinimumAccepted < 0 || *confidence.MinimumAccepted > 1 {
		return invalid("$.spec.routing.confidence.minimumAccepted", "confidence.threshold_invalid", "must be finite and between 0 and 1")
	}
	if confidence.OnLowConfidence == nil || (*confidence.OnLowConfidence != LowConfidenceIndeterminate && *confidence.OnLowConfidence != LowConfidenceEscalate) {
		return invalid("$.spec.routing.confidence.onLowConfidence", "confidence.action_invalid", "must be indeterminate or escalate")
	}
	if *confidence.OnLowConfidence == LowConfidenceEscalate && routing.Escalation == nil {
		return invalid("$.spec.routing.escalation", "escalation.missing", "is required when low confidence escalates")
	}
	if *confidence.OnLowConfidence != LowConfidenceEscalate && routing.Escalation != nil {
		return invalid("$.spec.routing.escalation", "escalation.unused", "must be omitted unless low confidence escalates")
	}
	for id, path := range positions {
		if !hasCapability(evaluators[id], "confidence-scores") {
			return invalid(path, "confidence.capability_missing", "every routed evaluator must declare confidence-scores")
		}
	}
	return nil
}

func addRoutePosition(positions map[string]string, evaluators map[string]Evaluator, id, path string) error {
	if !localIDPattern.MatchString(id) {
		return invalid(path, "route.invalid", "must be a lowercase evaluator identifier")
	}
	if _, exists := evaluators[id]; !exists {
		return invalid(path, "route.unknown", "must reference a declared evaluator")
	}
	if previous, exists := positions[id]; exists {
		return invalid(path, "route.duplicate", "evaluator is already used at "+previous)
	}
	positions[id] = path
	return nil
}

func validateComponent(path string, component ComponentIdentity) error {
	if !validNamespacedID(component.ID) {
		return invalid(path+".id", "component_id.invalid", "must be a namespaced identifier")
	}
	if !validVersion(component.Version) {
		return invalid(path+".version", "version.invalid", "must contain 1 to 128 non-whitespace Unicode code points")
	}
	return nil
}

func validNamespacedID(value string) bool {
	return len(value) <= 128 && namespacedPattern.MatchString(value)
}

func validVersion(value string) bool {
	return utf8.ValidString(value) && strings.TrimSpace(value) != "" && utf8.RuneCountInString(value) <= 128
}

func validateText(path, value string, maxRunes int) error {
	if !utf8.ValidString(value) || strings.TrimSpace(value) == "" {
		return invalid(path, "string.empty", "must contain non-whitespace UTF-8 text")
	}
	if utf8.RuneCountInString(value) > maxRunes {
		return invalid(path, "string.limit", fmt.Sprintf("must contain at most %d Unicode code points", maxRunes))
	}
	return nil
}

func hasCapability(evaluator Evaluator, capability string) bool {
	for _, candidate := range evaluator.RequiredCapabilities {
		if candidate == capability {
			return true
		}
	}
	return false
}

func (f TransientFailure) valid() bool {
	return f == FailureTimeout || f == FailureUnavailable || f == FailureThrottled
}

func invalid(path, code, message string) *ValidationError {
	return &ValidationError{Path: path, Code: code, Message: message}
}
