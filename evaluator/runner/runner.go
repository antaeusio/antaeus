// Package runner executes immutable evaluator profiles with bounded routing.
package runner

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"math/rand/v2"
	"slices"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/antaeusio/antaeus/decision"
	"github.com/antaeusio/antaeus/evaluator"
	"github.com/antaeusio/antaeus/evaluator/localbinding"
	"github.com/antaeusio/antaeus/evaluator/profile"
	"github.com/antaeusio/antaeus/internal/jsonvalue"
	"github.com/antaeusio/antaeus/policy"
)

const TraceExtension = "io.antaeus.execution"

// Adapter is an installed implementation, keyed by exact identity in Registry.
// Evaluate must honor context cancellation and must not retain request buffers,
// configuration, or credentials after returning. Calls are synchronous.
type Adapter struct {
	Mode           profile.Mode
	Protocol       profile.ComponentIdentity
	Capabilities   []string
	Parameters     profile.ParameterValidator
	FixtureVersion string
	Evaluate       func(context.Context, evaluator.Request, Configuration) (evaluator.Result, error)
}

type Registry map[profile.ComponentIdentity]Adapter

// Configuration is a per-attempt copy. Credentials are provided only to the
// invoked adapter and cleared on return. Installed adapters own approved origins.
type Configuration struct {
	Evaluator  profile.Evaluator
	Credential []byte
}

type Input struct {
	Policy         policy.Artifact
	PolicyVersion  string
	Profile        profile.Artifact
	ProfileVersion string
	CanonicalInput json.RawMessage
	CorrelationID  string
	Enforcement    bool
	// Credentials must be obtained after configuration selection and project
	// trust. The caller owns and clears this set; Run clears its own copies.
	Credentials *localbinding.Credentials
}

type Attempt struct {
	EvaluatorID       string `json:"evaluatorId"`
	AdapterID         string `json:"adapterId"`
	AdapterVersion    string `json:"adapterVersion"`
	Route             string `json:"route"`
	Attempt           int    `json:"attempt"`
	RuleIndexes       []int  `json:"ruleIndexes"`
	Code              string `json:"code"`
	LatencyMS         int64  `json:"latencyMs"`
	Provider          string `json:"provider,omitempty"`
	RequestedModel    string `json:"requestedModel,omitempty"`
	RequestedRevision string `json:"requestedRevision,omitempty"`
	ResolvedModel     string `json:"resolvedModel,omitempty"`
	ResolvedRevision  string `json:"resolvedRevision,omitempty"`
	RevisionAvailable bool   `json:"revisionAvailable"`
}

type Trace struct {
	Version  string    `json:"version"`
	Attempts []Attempt `json:"attempts"`
	Terminal string    `json:"terminal"`
}

type timing struct {
	now    func() time.Time
	sleep  func(context.Context, time.Duration) error
	jitter func(time.Duration) time.Duration
}

// Run validates all configuration before accepting an evaluation. Thereafter,
// exhausted adapter, deadline, and cancellation failures become failed evidence
// and a typed Decision. A matched deny still wins the deterministic reducer.
func Run(ctx context.Context, input Input, registry Registry) (decision.Decision, error) {
	return run(ctx, input, registry, timing{time.Now, sleep, func(base time.Duration) time.Duration {
		return base/2 + time.Duration(rand.Int64N(int64(base-base/2)+1))
	}})
}

func sleep(ctx context.Context, d time.Duration) error {
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

type execution struct {
	input       Input
	p           profile.Artifact
	registry    Registry
	entries     map[string]profile.Evaluator
	credentials map[localbinding.Key][]byte
	request     evaluator.Request
	trace       Trace
	route       []string
	time        timing
	last        evaluator.Metadata
	fallback    bool
}

func run(ctx context.Context, input Input, registry Registry, clock timing) (decision.Decision, error) {
	if ctx == nil {
		return decision.Decision{}, errors.New("context is required")
	}
	if err := ctx.Err(); err != nil {
		return decision.Decision{}, err
	}
	// Snapshot caller-owned artifacts before giving any buffers to adapters.
	policyJSON, err := input.Policy.CanonicalJSON()
	if err != nil {
		return decision.Decision{}, fmt.Errorf("invalid policy: %w", err)
	}
	input.Policy, err = policy.Parse(policyJSON, policy.FormatJSON)
	if err != nil {
		return decision.Decision{}, err
	}
	profileJSON, err := input.Profile.CanonicalJSON()
	if err != nil {
		return decision.Decision{}, fmt.Errorf("invalid profile: %w", err)
	}
	p, err := profile.Parse(profileJSON, profile.FormatJSON)
	if err != nil {
		return decision.Decision{}, err
	}
	canonical, err := jsonvalue.CanonicalObject(input.CanonicalInput)
	if err != nil || !bytes.Equal(canonical, input.CanonicalInput) {
		return decision.Decision{}, errors.New("input must be a bounded canonical JSON object")
	}
	input.CanonicalInput = canonical
	validators := profile.ParameterValidators{}
	snapshot := Registry{}
	for identity, a := range registry {
		a.Capabilities = slices.Clone(a.Capabilities)
		snapshot[identity] = a
		validators[identity] = a.Parameters
	}
	if err := p.ValidateParameters(validators); err != nil {
		return decision.Decision{}, errors.New("installed adapter parameter validation failed")
	}
	x := &execution{input: input, p: p, registry: snapshot, entries: map[string]profile.Evaluator{}, credentials: map[localbinding.Key][]byte{}, time: clock, trace: Trace{Version: "v0alpha1", Attempts: []Attempt{}}}
	defer func() {
		for _, value := range x.credentials {
			clear(value)
		}
	}()
	for _, entry := range p.Spec.Evaluators {
		x.entries[entry.ID] = entry
	}
	route := []string{p.Spec.Routing.Primary}
	if p.Spec.Routing.Escalation != nil {
		route = append(route, *p.Spec.Routing.Escalation)
	}
	route = append(route, p.Spec.Routing.Fallbacks...)
	for _, id := range route {
		entry := x.entries[id]
		a, ok := snapshot[entry.Adapter]
		if !ok || a.Evaluate == nil || a.Mode != entry.Mode || a.Protocol != entry.Protocol {
			return decision.Decision{}, errors.New("routed adapter identity, mode, or protocol is unsupported")
		}
		for _, capability := range entry.RequiredCapabilities {
			if !slices.Contains(a.Capabilities, capability) {
				return decision.Decision{}, errors.New("routed adapter lacks a required capability")
			}
		}
		if input.Enforcement && entry.Mode == profile.ModeDeterministicFixture {
			return decision.Decision{}, errors.New("deterministic fixtures cannot run in enforcement mode")
		}
		if entry.CredentialSlot != nil {
			key := localbinding.Key{AdapterID: entry.Adapter.ID, Slot: *entry.CredentialSlot}
			if _, exists := x.credentials[key]; !exists {
				value, exists := input.Credentials.Credential(key.AdapterID, key.Slot)
				if !exists || len(value) == 0 {
					return decision.Decision{}, &localbinding.MissingCredentialError{EvaluatorID: id, AdapterID: key.AdapterID, Slot: key.Slot}
				}
				x.credentials[key] = value
			}
		}
	}
	deadline := clock.now().Add(time.Duration(p.Spec.TotalTimeoutMS) * time.Millisecond)
	if parent, ok := ctx.Deadline(); ok && parent.Before(deadline) {
		deadline = parent
	}
	profileDigest, _ := p.Digest()
	policyDigest, _ := input.Policy.Digest()
	rules := make([]evaluator.Rule, len(input.Policy.Spec.Rules))
	indexes := make([]int, len(rules))
	for i, rule := range input.Policy.Spec.Rules {
		rules[i] = evaluator.Rule{ID: rule.ID, When: rule.When}
		indexes[i] = i
	}
	x.request = evaluator.Request{PolicyName: input.Policy.Metadata.Name, PolicyDigest: policyDigest, CanonicalInput: canonical, Rules: rules, Deadline: deadline, CorrelationID: input.CorrelationID, ProfileDigest: profileDigest}
	if err := x.request.Validate(); err != nil {
		return decision.Decision{}, err
	}
	// Validate registry fixture metadata and optional labels before acceptance.
	for _, id := range route {
		probe := evaluator.Result{RuleResults: failedRules(x.request.Rules, "evaluation.preflight"), Metadata: x.metadata(x.entries[id])}
		if err := evaluator.ValidateResult(x.request, probe); err != nil {
			return decision.Decision{}, errors.New("invalid installed adapter metadata")
		}
	}
	for _, label := range []string{input.PolicyVersion, input.ProfileVersion} {
		if label != "" && (!utf8.ValidString(label) || strings.TrimSpace(label) == "" || utf8.RuneCountInString(label) > 128) {
			return decision.Decision{}, errors.New("registry version labels must contain 1 to 128 non-whitespace Unicode code points")
		}
	}
	total, cancel := context.WithDeadline(ctx, deadline)
	defer cancel()
	evidence, code := x.invoke(total, p.Spec.Routing.Primary, "primary", indexes)
	if len(x.trace.Attempts) == 0 {
		return decision.Decision{}, errors.New("evaluation cancelled or deadline elapsed before first attempt")
	}
	if code != "" {
		evidence, code = x.fallbacks(total, indexes, evidence, code)
	} else {
		low := x.applyConfidence(evidence)
		if len(low) > 0 && p.Spec.Routing.Escalation != nil {
			replacement, escalationCode := x.invoke(total, *p.Spec.Routing.Escalation, "escalation", low)
			if escalationCode != "" {
				replacement, escalationCode = x.fallbacks(total, low, replacement, escalationCode)
			} else {
				x.applyConfidence(replacement)
			}
			for i, index := range low {
				evidence[index] = replacement[i]
			}
			code = escalationCode
		}
	}
	x.trace.Terminal = "completed"
	if code != "" {
		x.trace.Terminal = code
	} else {
		for _, r := range evidence {
			if r.Status == decision.RuleIndeterminate || r.Status == decision.RuleFailed {
				x.trace.Terminal = "evaluation.unresolved_rule"
				break
			}
		}
	}
	return x.decide(evidence)
}

func (x *execution) metadata(e profile.Evaluator) evaluator.Metadata {
	m := evaluator.Metadata{AdapterID: e.Adapter.ID, AdapterVersion: e.Adapter.Version, Mode: evaluator.Mode(e.Mode), Synthetic: e.Mode == profile.ModeDeterministicFixture, Provider: value(e.Provider), Model: value(e.Model)}
	if m.Synthetic {
		m.FixtureSet, _ = e.Parameters["fixtureSet"].(string)
		m.FixtureVersion = x.registry[e.Adapter].FixtureVersion
	}
	return m
}

func (x *execution) invoke(ctx context.Context, id, route string, indexes []int) ([]evaluator.RuleResult, string) {
	e := x.entries[id]
	rules := make([]evaluator.Rule, len(indexes))
	for i, index := range indexes {
		rules[i] = x.request.Rules[index]
	}
	failure := "evaluation.adapter_failed"
	for attempt := 1; attempt <= e.Retry.MaxAttempts; attempt++ {
		if code := x.stopped(ctx); code != "" {
			return failedRules(rules, code), code
		}
		if len(x.trace.Attempts) >= decision.MaxEvaluatorAttempts {
			return failedRules(rules, "evaluation.attempt_limit"), "evaluation.attempt_limit"
		}
		start := x.time.now()
		deadline := start.Add(time.Duration(e.TimeoutMS) * time.Millisecond)
		if x.request.Deadline.Before(deadline) {
			deadline = x.request.Deadline
		}
		request := x.request
		request.Deadline = deadline
		request.Rules = slices.Clone(rules)
		request.CanonicalInput = bytes.Clone(x.request.CanonicalInput)
		// Keep an independent request for result validation against adapter mutation.
		validation := request
		validation.Rules = slices.Clone(rules)
		validation.CanonicalInput = bytes.Clone(x.request.CanonicalInput)
		encoded, _ := json.Marshal(e)
		var entry profile.Evaluator
		_ = json.Unmarshal(encoded, &entry)
		var credential []byte
		if e.CredentialSlot != nil {
			credential = bytes.Clone(x.credentials[localbinding.Key{AdapterID: e.Adapter.ID, Slot: *e.CredentialSlot}])
		}
		attemptContext, cancel := context.WithDeadline(ctx, deadline)
		result, err, attemptErr := func() (evaluator.Result, error, error) {
			defer cancel()
			defer clear(credential)
			result, err := x.registry[e.Adapter].Evaluate(attemptContext, request, Configuration{entry, credential})
			return result, err, attemptContext.Err()
		}()
		x.last = x.metadata(e)
		if len(x.route) == 0 || x.route[len(x.route)-1] != id {
			x.route = append(x.route, id)
		}
		if route == "fallback" {
			x.fallback = true
		}
		failure = x.stopped(ctx)
		if failure == "" {
			if !x.time.now().Before(deadline) || errors.Is(attemptErr, context.DeadlineExceeded) {
				failure = "evaluator.timeout"
			} else if err != nil {
				failure = classify(err)
			} else if evaluator.ValidateResult(validation, result) != nil || !x.matches(e, result.Metadata) {
				failure = "evaluation.invalid_result"
			}
		}
		record := Attempt{EvaluatorID: id, AdapterID: e.Adapter.ID, AdapterVersion: e.Adapter.Version, Route: route, Attempt: attempt, RuleIndexes: slices.Clone(indexes), Code: failure, LatencyMS: max(0, x.time.now().Sub(start).Milliseconds()), Provider: value(e.Provider), RequestedModel: value(e.Model), RequestedRevision: value(e.ModelRevision)}
		if failure == "" {
			record.Code = "evaluation.succeeded"
			record.ResolvedModel = result.Metadata.Model
			record.ResolvedRevision = result.Metadata.ModelRevision
			record.RevisionAvailable = result.Metadata.ModelRevision != ""
			x.last = result.Metadata
		}
		x.trace.Attempts = append(x.trace.Attempts, record)
		if failure == "" {
			return cloneResults(result.RuleResults), ""
		}
		if attempt == e.Retry.MaxAttempts || !slices.Contains(e.Retry.RetryOn, transient(failure)) {
			break
		}
		base := math.Min(float64(*e.Retry.MaxBackoffMS), float64(*e.Retry.InitialBackoffMS)*math.Pow(*e.Retry.Multiplier, float64(attempt-1)))
		delay := x.time.jitter(time.Duration(base) * time.Millisecond)
		remaining := x.request.Deadline.Sub(x.time.now())
		if remaining <= 0 {
			return failedRules(rules, "evaluation.deadline_exceeded"), "evaluation.deadline_exceeded"
		}
		if delay > remaining {
			delay = remaining
		}
		if err := x.time.sleep(ctx, delay); err != nil {
			code := x.stopped(ctx)
			if code == "" {
				code = "evaluation.cancelled"
			}
			return failedRules(rules, code), code
		}
	}
	return failedRules(rules, failure), failure
}

func (x *execution) stopped(ctx context.Context) string {
	if !x.time.now().Before(x.request.Deadline) || errors.Is(ctx.Err(), context.DeadlineExceeded) {
		return "evaluation.deadline_exceeded"
	}
	if ctx.Err() != nil {
		return "evaluation.cancelled"
	}
	return ""
}

func classify(err error) string {
	var failure *evaluator.Error
	if errors.As(err, &failure) && failure != nil && failure.Retryable && transient(failure.Code) != "" {
		return failure.Code
	}
	return "evaluation.adapter_failed"
}

func transient(code string) profile.TransientFailure {
	switch code {
	case "evaluator.timeout":
		return profile.FailureTimeout
	case "evaluator.unavailable":
		return profile.FailureUnavailable
	case "evaluator.throttled":
		return profile.FailureThrottled
	}
	return ""
}

func (x *execution) fallbacks(ctx context.Context, indexes []int, evidence []evaluator.RuleResult, code string) ([]evaluator.RuleResult, string) {
	for _, id := range x.p.Spec.Routing.Fallbacks {
		if !slices.Contains(x.p.Spec.Routing.FallbackOn, transient(code)) {
			break
		}
		evidence, code = x.invoke(ctx, id, "fallback", indexes)
		if code == "" {
			x.applyConfidence(evidence)
			break
		}
	}
	return evidence, code
}

// Confidence is applied to all non-failed evidence. Only a primary call can
// invoke escalation; subsequent low-confidence evidence remains indeterminate.
func (x *execution) applyConfidence(results []evaluator.RuleResult) []int {
	var low []int
	if !x.p.Spec.Routing.Confidence.Enabled {
		return low
	}
	for i := range results {
		r := &results[i]
		if r.Status == decision.RuleFailed {
			continue
		}
		if r.Confidence == nil || *r.Confidence < *x.p.Spec.Routing.Confidence.MinimumAccepted {
			r.Status = decision.RuleIndeterminate
			r.ReasonCodes = []string{"evaluation.low_confidence"}
			low = append(low, i)
		}
	}
	return low
}

func (x *execution) matches(e profile.Evaluator, m evaluator.Metadata) bool {
	expected := x.metadata(e)
	return m.AdapterID == expected.AdapterID && m.AdapterVersion == expected.AdapterVersion && m.Mode == expected.Mode && m.Synthetic == expected.Synthetic && m.Provider == expected.Provider && m.FixtureSet == expected.FixtureSet && m.FixtureVersion == expected.FixtureVersion && (m.ModelRevision == "" || e.ModelRevision == nil || m.ModelRevision == *e.ModelRevision)
}

func failedRules(rules []evaluator.Rule, code string) []evaluator.RuleResult {
	results := make([]evaluator.RuleResult, len(rules))
	for i, r := range rules {
		results[i] = evaluator.RuleResult{RuleID: r.ID, Status: decision.RuleFailed, ReasonCodes: []string{code}}
	}
	return results
}

func cloneResults(source []evaluator.RuleResult) []evaluator.RuleResult {
	results := slices.Clone(source)
	for i := range results {
		results[i].ReasonCodes = slices.Clone(results[i].ReasonCodes)
		if results[i].Confidence != nil {
			c := *results[i].Confidence
			results[i].Confidence = &c
		}
		results[i].Message = ""
	}
	return results
}

func (x *execution) decide(evidence []evaluator.RuleResult) (decision.Decision, error) {
	rules := make([]decision.RuleResult, len(evidence))
	for i, r := range evidence {
		rules[i] = decision.RuleResult{RuleID: r.RuleID, Status: r.Status, Confidence: r.Confidence, ReasonCodes: r.ReasonCodes}
		if r.Status == decision.RuleMatched {
			outcome := x.input.Policy.Spec.Rules[i].Outcome
			rules[i].Outcome = &outcome
		}
	}
	reduction, err := decision.Reduce(x.input.Policy, rules)
	if err != nil {
		return decision.Decision{}, err
	}
	trace, err := json.Marshal(x.trace)
	if err != nil {
		return decision.Decision{}, err
	}
	m := x.last
	synthetic := m.Synthetic
	d := decision.Decision{APIVersion: decision.APIVersion, Kind: decision.KindDecision, Outcome: reduction.Outcome, Policy: decision.PolicyIdentity{Name: x.request.PolicyName, Digest: x.request.PolicyDigest, Version: x.input.PolicyVersion}, RuleResults: rules, ReasonCodes: reduction.ReasonCodes, Failure: reduction.Failure, Extensions: map[string]json.RawMessage{TraceExtension: trace}, Evaluator: &decision.Evaluator{ProfileDigest: x.request.ProfileDigest, ProfileVersion: x.input.ProfileVersion, Adapter: m.AdapterID, AdapterVersion: m.AdapterVersion, Mode: decision.EvaluatorMode(m.Mode), Synthetic: &synthetic, Provider: m.Provider, Model: m.Model, Route: x.route, Attempts: len(x.trace.Attempts), Fallback: x.fallback}}
	if m.Synthetic {
		d.Evaluator.FixtureSet = &m.FixtureSet
		d.Evaluator.FixtureVersion = &m.FixtureVersion
	}
	if err := d.ValidateAgainst(x.input.Policy); err != nil {
		return decision.Decision{}, err
	}
	return d, nil
}

func value(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}
