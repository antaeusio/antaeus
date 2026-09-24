package profile

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/antaeusio/antaeus/internal/strictsource"
	"github.com/gowebpki/jcs"
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
	Cause   error
}

func (e *ValidationError) Unwrap() error {
	return e.Cause
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
	if err := validateRouting(a.Spec.Routing, evaluators); err != nil {
		return err
	}
	return validateEncodedArtifact(a)
}

// ValidateParameters validates semantic parameter objects against the exact
// adapter ID and version. It is required before executing a semantic profile.
func (a Artifact) ValidateParameters(registry ParameterRegistry) error {
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
	if registry == nil {
		return invalid("$.spec.evaluators", "adapter_schema.missing", "semantic profiles require an adapter parameter validator")
	}
	for i, evaluator := range a.Spec.Evaluators {
		if evaluator.Mode != ModeSemantic {
			continue
		}
		validator, exists := registry.ValidatorFor(evaluator.Adapter)
		if !exists || validator == nil {
			return invalid(fmt.Sprintf("$.spec.evaluators[%d].adapter", i), "adapter_schema.missing", fmt.Sprintf("no parameter validator is registered for %s@%s", evaluator.Adapter.ID, evaluator.Adapter.Version))
		}
		encoded, err := canonicalParameters(evaluator.Parameters)
		if err != nil {
			return invalid(fmt.Sprintf("$.spec.evaluators[%d].parameters", i), "parameters.invalid", err.Error())
		}
		if err := validator.ValidateParameters(encoded); err != nil {
			return invalidCause(fmt.Sprintf("$.spec.evaluators[%d].parameters", i), "parameters.adapter_invalid", err)
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
	for _, field := range []struct {
		name  string
		value *string
	}{
		{name: "model", value: evaluator.Model},
		{name: "modelRevision", value: evaluator.ModelRevision},
	} {
		if field.value != nil && (!modelPattern.MatchString(*field.value) || strings.Contains(*field.value, "://")) {
			return invalid(path+"."+field.name, "model.invalid", "must be a bounded model identifier, not a URL")
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
	if err := validateParameterData(evaluator.Parameters); err != nil {
		return invalid(path+".parameters", "parameters.invalid", err.Error())
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
	if err := validateConfidence(routing, evaluators); err != nil {
		return err
	}
	if routing.Terminal.OnIndeterminate != "failure" || routing.Terminal.OnFailure != "failure" {
		return invalid("$.spec.routing.terminal", "terminal.invalid", "v0alpha1 requires failure for indeterminate and failed evidence")
	}
	return nil
}

func validateConfidence(routing Routing, evaluators map[string]Evaluator) error {
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
	routes := []struct{ id, path string }{{routing.Primary, "$.spec.routing.primary"}}
	if routing.Escalation != nil {
		routes = append(routes, struct{ id, path string }{*routing.Escalation, "$.spec.routing.escalation"})
	}
	for i, id := range routing.Fallbacks {
		routes = append(routes, struct{ id, path string }{id, fmt.Sprintf("$.spec.routing.fallbacks[%d]", i)})
	}
	for _, route := range routes {
		if !hasCapability(evaluators[route.id], "confidence-scores") {
			return invalid(route.path, "confidence.capability_missing", "every routed evaluator must declare confidence-scores")
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
	return &ValidationError{Path: path, Code: code, Message: boundedMessage(message)}
}

func invalidCause(path, code string, cause error) *ValidationError {
	return &ValidationError{Path: path, Code: code, Message: boundedMessage(cause.Error()), Cause: cause}
}

func boundedMessage(message string) string {
	const max = 512
	if len(message) <= max {
		return message
	}
	// Keep the existing byte budget, but do not split a UTF-8 character in
	// otherwise valid text. This bounds diagnostics; it is not a sanitizer
	// for malformed text returned by an adapter.
	end := max - 3
	for end > 0 && !utf8.RuneStart(message[end]) {
		end--
	}
	return message[:end] + "..."
}

func canonicalParameters(parameters map[string]any) (json.RawMessage, error) {
	encoded, err := json.Marshal(parameters)
	if err != nil {
		return nil, err
	}
	canonical, err := jcs.Transform(encoded)
	if err != nil {
		return nil, err
	}
	return canonical, nil
}

func validateParameterData(parameters map[string]any) error {
	nodes := 0
	return validateJSONValue(parameters, 0, &nodes)
}

func validateEncodedArtifact(artifact Artifact) error {
	var buffer bytes.Buffer
	encoder := json.NewEncoder(&buffer)
	encoder.SetEscapeHTML(false)
	if err := encoder.Encode(artifact); err != nil {
		return invalid("$", "source.schema", err.Error())
	}
	encoded := bytes.TrimSuffix(buffer.Bytes(), []byte{'\n'})
	normalized, err := strictsource.Decode(encoded, strictsource.FormatJSON, len(encoded), "evaluator profile")
	if err != nil {
		var sourceErr *strictsource.Error
		if errors.As(err, &sourceErr) {
			return invalid("$", sourceErr.Code, sourceErr.Message)
		}
		return invalid("$", "source.schema", err.Error())
	}
	canonical, err := jcs.Transform(normalized)
	if err != nil {
		return invalid("$", "source.number", err.Error())
	}
	if len(canonical) > MaxSourceBytes {
		return invalid("$", "source.too_large", fmt.Sprintf("canonical evaluator profile must not exceed %d bytes", MaxSourceBytes))
	}
	return nil
}

func validateJSONValue(value any, depth int, nodes *int) error {
	*nodes++
	if *nodes > MaxParsedNodes {
		return fmt.Errorf("must not exceed %d parsed nodes", MaxParsedNodes)
	}
	switch typed := value.(type) {
	case nil, bool:
		return nil
	case string:
		if !utf8.ValidString(typed) {
			return fmt.Errorf("contains invalid UTF-8")
		}
		return nil
	case json.Number:
		_, err := strictsource.NormalizeNumber(string(typed))
		return err
	case float64:
		_, err := strictsource.NormalizeNumber(strconv.FormatFloat(typed, 'g', -1, 64))
		return err
	case float32:
		_, err := strictsource.NormalizeNumber(strconv.FormatFloat(float64(typed), 'g', -1, 32))
		return err
	case int:
		return validateInteger(int64(typed))
	case int8:
		return validateInteger(int64(typed))
	case int16:
		return validateInteger(int64(typed))
	case int32:
		return validateInteger(int64(typed))
	case int64:
		return validateInteger(typed)
	case uint:
		return validateUnsigned(uint64(typed))
	case uint8:
		return nil
	case uint16:
		return nil
	case uint32:
		return nil
	case uint64:
		return validateUnsigned(typed)
	case map[string]any:
		depth++
		if depth > MaxNestingDepth {
			return fmt.Errorf("nesting must not exceed %d containers", MaxNestingDepth)
		}
		keys := make([]string, 0, len(typed))
		for key := range typed {
			keys = append(keys, key)
		}
		sort.Strings(keys)
		for _, key := range keys {
			if !utf8.ValidString(key) {
				return fmt.Errorf("contains an invalid UTF-8 object key")
			}
			*nodes++
			if err := validateJSONValue(typed[key], depth, nodes); err != nil {
				return err
			}
		}
		return nil
	case []any:
		depth++
		if depth > MaxNestingDepth {
			return fmt.Errorf("nesting must not exceed %d containers", MaxNestingDepth)
		}
		for _, child := range typed {
			if err := validateJSONValue(child, depth, nodes); err != nil {
				return err
			}
		}
		return nil
	default:
		return fmt.Errorf("contains non-JSON value of type %T", value)
	}
}

func validateInteger(value int64) error {
	const maxSafe = int64(9_007_199_254_740_991)
	if value < -maxSafe || value > maxSafe {
		return fmt.Errorf("integer exceeds the interoperable IEEE-754 safe range")
	}
	return nil
}

func validateUnsigned(value uint64) error {
	const maxSafe = uint64(9_007_199_254_740_991)
	if value > maxSafe {
		return fmt.Errorf("integer exceeds the interoperable IEEE-754 safe range")
	}
	return nil
}
