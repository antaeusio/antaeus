package runner

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/antaeusio/antaeus/decision"
	"github.com/antaeusio/antaeus/evaluator"
	"github.com/antaeusio/antaeus/evaluator/localbinding"
	"github.com/antaeusio/antaeus/evaluator/profile"
	"github.com/antaeusio/antaeus/policy"
	jsonschema "github.com/santhosh-tekuri/jsonschema/v6"
)

type validator func(json.RawMessage) error

func (v validator) ValidateParameters(data json.RawMessage) error { return v(data) }

type fakeTime struct {
	current time.Time
	delays  []time.Duration
}

func (c *fakeTime) timing() timing {
	return timing{func() time.Time { return c.current }, func(ctx context.Context, d time.Duration) error {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		c.delays = append(c.delays, d)
		c.current = c.current.Add(d)
		return nil
	}, func(d time.Duration) time.Duration { return d }}
}

func inputFixture(t *testing.T) Input {
	t.Helper()
	p, err := policy.LoadFile("../../contracts/examples/v0alpha1/policy/vendor-onboarding.json")
	if err != nil {
		t.Fatal(err)
	}
	profileArtifact, err := profile.LoadFile("../../contracts/examples/v0alpha1/evaluator-profile/semantic-routing.json")
	if err != nil {
		t.Fatal(err)
	}
	b, err := localbinding.LoadFile("../../contracts/examples/v0alpha1/local-secret-bindings/development.json")
	if err != nil {
		t.Fatal(err)
	}
	credentials, err := localbinding.Preflight(profileArtifact, b, localbinding.EnvironmentFunc(func(string) (string, bool) { return "PRIVATE_TOKEN", true }))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(credentials.Clear)
	return Input{Policy: p, Profile: profileArtifact, CanonicalInput: json.RawMessage(`{"input":true}`), CorrelationID: "test-correlation", Credentials: credentials}
}

func installed(fn func(context.Context, evaluator.Request, Configuration) (evaluator.Result, error)) Registry {
	return Registry{{ID: "io.example.semantic", Version: "1.0.0"}: {Mode: profile.ModeSemantic, Protocol: profile.ComponentIdentity{ID: "io.antaeus.rule-match", Version: "v0alpha1"}, Capabilities: []string{"json-input", "structured-rule-results", "confidence-scores"}, Parameters: validator(func(json.RawMessage) error { return nil }), Evaluate: fn}}
}

func evidence(r evaluator.Request, c Configuration, confidence float64) evaluator.Result {
	results := make([]evaluator.RuleResult, len(r.Rules))
	for i, rule := range r.Rules {
		score := confidence
		results[i] = evaluator.RuleResult{RuleID: rule.ID, Status: decision.RuleNotMatched, Confidence: &score, ReasonCodes: []string{"test.evidence"}}
	}
	return evaluator.Result{RuleResults: results, Metadata: evaluator.Metadata{AdapterID: c.Evaluator.Adapter.ID, AdapterVersion: c.Evaluator.Adapter.Version, Mode: evaluator.ModeSemantic, Provider: value(c.Evaluator.Provider), Model: value(c.Evaluator.Model)}}
}

func traceOf(t *testing.T, d decision.Decision) Trace {
	t.Helper()
	var trace Trace
	if err := json.Unmarshal(d.Extensions[TraceExtension], &trace); err != nil {
		t.Fatal(err)
	}
	var instance any
	if err := json.Unmarshal(d.Extensions[TraceExtension], &instance); err != nil {
		t.Fatal(err)
	}
	if err := traceSchema(t).Validate(instance); err != nil {
		t.Fatal(err)
	}
	return trace
}

func traceSchema(t *testing.T) *jsonschema.Schema {
	t.Helper()
	schemaData, err := os.ReadFile("../../contracts/schemas/v0alpha1/execution-trace.schema.json")
	if err != nil {
		t.Fatal(err)
	}
	var schemaDocument any
	if err := json.Unmarshal(schemaData, &schemaDocument); err != nil {
		t.Fatal(err)
	}
	compiler := jsonschema.NewCompiler()
	const schemaURL = "https://antaeus.io/contracts/v0alpha1/execution-trace.schema.json"
	if err := compiler.AddResource(schemaURL, schemaDocument); err != nil {
		t.Fatal(err)
	}
	schema, err := compiler.Compile(schemaURL)
	if err != nil {
		t.Fatal(err)
	}
	return schema
}

func TestPrimaryDecisionAndCredentialLifetime(t *testing.T) {
	in := inputFixture(t)
	var held []byte
	registry := installed(func(ctx context.Context, r evaluator.Request, c Configuration) (evaluator.Result, error) {
		if string(c.Credential) != "PRIVATE_TOKEN" {
			t.Fatal("missing exact credential")
		}
		held = c.Credential
		deadline, _ := ctx.Deadline()
		if !deadline.Equal(r.Deadline) {
			t.Fatal("request and context deadlines differ")
		}
		result := evidence(r, c, 1)
		result.RuleResults[1].Status = decision.RuleMatched
		result.RuleResults[1].Message = "RAW_PROVIDER_PAYLOAD"
		result.Metadata.ModelRevision = "2026-09-01"
		return result, nil
	})
	d, err := Run(context.Background(), in, registry)
	if err != nil {
		t.Fatal(err)
	}
	if d.Outcome != decision.OutcomeAllow || d.Evaluator.Attempts != 1 || d.Evaluator.Synthetic == nil || *d.Evaluator.Synthetic {
		t.Fatalf("decision=%+v", d)
	}
	if err := d.ValidateAgainst(in.Policy); err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(held, make([]byte, len(held))) {
		t.Fatal("attempt credential not cleared")
	}
	trace := traceOf(t, d)
	if !trace.Attempts[0].RevisionAvailable || trace.Attempts[0].ResolvedRevision != "2026-09-01" {
		t.Fatal(trace)
	}
	encoded, _ := json.Marshal(d)
	if bytes.Contains(encoded, []byte("PRIVATE_TOKEN")) || bytes.Contains(encoded, []byte("RAW_PROVIDER_PAYLOAD")) {
		t.Fatal("output leaked adapter data")
	}
}

func TestRetryIdentityBackoffAndFailClosedErrors(t *testing.T) {
	for _, code := range []string{"evaluator.timeout", "evaluator.unavailable", "evaluator.throttled"} {
		t.Run(code, func(t *testing.T) {
			in := inputFixture(t)
			in.Profile.Spec.Evaluators[0].Retry.RetryOn = []profile.TransientFailure{transient(code)}
			clock := &fakeTime{current: time.Now()}
			calls := 0
			var first evaluator.Request
			registry := installed(func(_ context.Context, r evaluator.Request, c Configuration) (evaluator.Result, error) {
				calls++
				if calls == 1 {
					first = r
					return evaluator.Result{}, &evaluator.Error{Code: code, Retryable: true, Message: "SECRET_ERROR"}
				}
				if first.PolicyDigest != r.PolicyDigest || first.ProfileDigest != r.ProfileDigest || first.CorrelationID != r.CorrelationID || !reflect.DeepEqual(first.Rules, r.Rules) || !bytes.Equal(first.CanonicalInput, r.CanonicalInput) {
					t.Fatal("retry changed evidence inputs")
				}
				return evidence(r, c, 1), nil
			})
			d, err := run(context.Background(), in, registry, clock.timing())
			if err != nil {
				t.Fatal(err)
			}
			if calls != 2 || d.Evaluator.Attempts != 2 || len(clock.delays) != 1 || clock.delays[0] != 100*time.Millisecond {
				t.Fatalf("calls=%d delays=%v", calls, clock.delays)
			}
		})
	}
	for _, failure := range []error{errors.New("SECRET_ERROR"), &evaluator.Error{Code: "evaluator.timeout", Retryable: false}, &evaluator.Error{Code: "credential.rejected", Retryable: true}} {
		in := inputFixture(t)
		calls := 0
		d, err := Run(context.Background(), in, installed(func(context.Context, evaluator.Request, Configuration) (evaluator.Result, error) {
			calls++
			return evaluator.Result{}, failure
		}))
		if err != nil || calls != 1 || d.Outcome != decision.OutcomeFailure {
			t.Fatalf("calls=%d decision=%+v error=%v", calls, d, err)
		}
		encoded, _ := json.Marshal(d)
		if bytes.Contains(encoded, []byte("SECRET_ERROR")) {
			t.Fatal("error message leaked")
		}
	}
}

func TestEscalationFallbackPreservesPrimaryDeny(t *testing.T) {
	in := inputFixture(t)
	var seen []string
	registry := installed(func(_ context.Context, r evaluator.Request, c Configuration) (evaluator.Result, error) {
		seen = append(seen, c.Evaluator.ID)
		if c.Evaluator.ID == "semantic-primary" {
			out := evidence(r, c, 1)
			out.RuleResults[0].Status = decision.RuleMatched
			out.RuleResults[1].Confidence = nil
			return out, nil
		}
		if len(r.Rules) != 1 || r.Rules[0].ID != in.Policy.Spec.Rules[1].ID {
			t.Fatal("escalation/fallback overwrote accepted primary evidence")
		}
		return evaluator.Result{}, &evaluator.Error{Code: "evaluator.unavailable", Retryable: true}
	})
	d, err := Run(context.Background(), in, registry)
	if err != nil {
		t.Fatal(err)
	}
	if d.Outcome != decision.OutcomeDeny || d.RuleResults[1].Status != decision.RuleFailed || !d.Evaluator.Fallback {
		t.Fatalf("decision=%+v", d)
	}
	if !reflect.DeepEqual(seen, []string{"semantic-primary", "semantic-escalation", "semantic-fallback"}) {
		t.Fatal(seen)
	}
	trace := traceOf(t, d)
	if !reflect.DeepEqual(trace.Attempts[2].RuleIndexes, []int{1}) {
		t.Fatal(trace)
	}
}

func TestFallbackAfterPrimaryFailureUsesAllRules(t *testing.T) {
	in := inputFixture(t)
	clock := &fakeTime{current: time.Now()}
	var seen []string
	d, err := run(context.Background(), in, installed(func(_ context.Context, r evaluator.Request, c Configuration) (evaluator.Result, error) {
		seen = append(seen, c.Evaluator.ID)
		if c.Evaluator.ID == "semantic-primary" {
			return evaluator.Result{}, &evaluator.Error{Code: "evaluator.timeout", Retryable: true}
		}
		if c.Evaluator.ID != "semantic-fallback" || len(r.Rules) != 2 {
			t.Fatal("unexpected recovery route")
		}
		return evidence(r, c, 0.1), nil
	}), clock.timing())
	if err != nil {
		t.Fatal(err)
	}
	if len(seen) != 3 || d.Outcome != decision.OutcomeFailure || d.RuleResults[0].Status != decision.RuleIndeterminate {
		t.Fatalf("seen=%v decision=%+v", seen, d)
	}
	if traceOf(t, d).Terminal != "evaluation.unresolved_rule" {
		t.Fatal("low fallback confidence not retained")
	}
}

func TestLowEscalationConfidenceDoesNotRecurse(t *testing.T) {
	in := inputFixture(t)
	calls := 0
	d, err := Run(context.Background(), in, installed(func(_ context.Context, r evaluator.Request, c Configuration) (evaluator.Result, error) {
		calls++
		return evidence(r, c, 0), nil
	}))
	if err != nil || calls != 2 || d.Outcome != decision.OutcomeFailure {
		t.Fatalf("calls=%d outcome=%s err=%v", calls, d.Outcome, err)
	}
}

func TestDeadlineAndCancellationBecomeFailedEvidence(t *testing.T) {
	for _, total := range []bool{false, true} {
		in := inputFixture(t)
		clock := &fakeTime{current: time.Now()}
		calls := 0
		d, err := run(context.Background(), in, installed(func(_ context.Context, r evaluator.Request, c Configuration) (evaluator.Result, error) {
			calls++
			if total {
				clock.current = clock.current.Add(11 * time.Second)
			} else {
				clock.current = r.Deadline
			}
			return evidence(r, c, 1), nil // a late success cannot be accepted
		}), clock.timing())
		if err != nil || d.Outcome != decision.OutcomeFailure {
			t.Fatalf("decision=%+v err=%v", d, err)
		}
		if total && calls != 1 {
			t.Fatal("total deadline retried")
		}
	}
	in := inputFixture(t)
	ctx, cancel := context.WithCancel(context.Background())
	d, err := Run(ctx, in, installed(func(_ context.Context, r evaluator.Request, c Configuration) (evaluator.Result, error) {
		cancel()
		return evidence(r, c, 1), nil
	}))
	if err != nil || d.Outcome != decision.OutcomeFailure || traceOf(t, d).Terminal != "evaluation.cancelled" {
		t.Fatalf("decision=%+v err=%v", d, err)
	}
	if _, err := Run(ctx, in, installed(nil)); err == nil {
		t.Fatal("pre-cancelled invocation accepted")
	}
}

func TestMalformedMetadataAndMutatedRequestFailClosed(t *testing.T) {
	for _, mutate := range []func(*evaluator.Request, *evaluator.Result){
		func(_ *evaluator.Request, r *evaluator.Result) { r.Metadata.AdapterID = "io.example.other" },
		func(_ *evaluator.Request, r *evaluator.Result) { r.Metadata.Synthetic = true },
		func(_ *evaluator.Request, r *evaluator.Result) { r.Metadata.ModelRevision = "different-revision" },
		func(_ *evaluator.Request, r *evaluator.Result) { r.RuleResults[0].Confidence = new(2.0) },
		func(req *evaluator.Request, r *evaluator.Result) {
			req.Rules[0].ID = "changed"
			r.RuleResults[0].RuleID = "changed"
		},
	} {
		in := inputFixture(t)
		calls := 0
		d, err := Run(context.Background(), in, installed(func(_ context.Context, r evaluator.Request, c Configuration) (evaluator.Result, error) {
			calls++
			result := evidence(r, c, 1)
			mutate(&r, &result)
			return result, nil
		}))
		if err != nil || calls != 1 || d.Outcome != decision.OutcomeFailure || traceOf(t, d).Terminal != "evaluation.invalid_result" {
			t.Fatalf("calls=%d decision=%+v err=%v", calls, d, err)
		}
	}
}

func TestPreflightRejectsBeforeAdapterCalls(t *testing.T) {
	for _, test := range []string{"credentials", "parameter-schema", "protocol", "capability", "canonical-input", "version-label"} {
		t.Run(test, func(t *testing.T) {
			in := inputFixture(t)
			registry := installed(func(context.Context, evaluator.Request, Configuration) (evaluator.Result, error) {
				t.Fatal("adapter invoked before valid configuration")
				return evaluator.Result{}, nil
			})
			key := in.Profile.Spec.Evaluators[0].Adapter
			a := registry[key]
			switch test {
			case "credentials":
				in.Credentials = nil
			case "parameter-schema":
				a.Parameters = nil
			case "protocol":
				a.Protocol.Version = "other"
			case "capability":
				a.Capabilities = nil
			case "canonical-input":
				in.CanonicalInput = []byte(`{ "input": true }`)
			case "version-label":
				in.PolicyVersion = "  "
			}
			registry[key] = a
			if _, err := Run(context.Background(), in, registry); err == nil {
				t.Fatal("invalid configuration accepted")
			}
		})
	}
}

func TestGlobalAttemptLimit(t *testing.T) {
	in := inputFixture(t)
	base := in.Profile.Spec.Evaluators[0]
	base.CredentialSlot = nil
	base.Retry = profile.RetryPolicy{MaxAttempts: 5, RetryOn: []profile.TransientFailure{profile.FailureTimeout}, InitialBackoffMS: new(1), MaxBackoffMS: new(1), Multiplier: new(1.0)}
	in.Profile.Spec.Evaluators = nil
	in.Profile.Spec.Routing.Escalation = nil
	in.Profile.Spec.Routing.Confidence = profile.Confidence{Enabled: false}
	in.Profile.Spec.Routing.Fallbacks = []string{}
	for i := 0; i < 16; i++ {
		entry := base
		entry.ID = fmt.Sprintf("route-%d", i)
		in.Profile.Spec.Evaluators = append(in.Profile.Spec.Evaluators, entry)
		if i == 0 {
			in.Profile.Spec.Routing.Primary = entry.ID
		} else {
			in.Profile.Spec.Routing.Fallbacks = append(in.Profile.Spec.Routing.Fallbacks, entry.ID)
		}
	}
	clock := &fakeTime{current: time.Now()}
	calls := 0
	d, err := run(context.Background(), in, installed(func(context.Context, evaluator.Request, Configuration) (evaluator.Result, error) {
		calls++
		return evaluator.Result{}, &evaluator.Error{Code: "evaluator.timeout", Retryable: true}
	}), clock.timing())
	if err != nil {
		t.Fatal(err)
	}
	if calls != 64 || d.Evaluator.Attempts != 64 || traceOf(t, d).Terminal != "evaluation.attempt_limit" {
		t.Fatalf("calls=%d trace=%+v", calls, traceOf(t, d))
	}
	assertDecisionFailure(t, d, "evaluation.attempt_limit", false)
}

func TestRegistryFixtureCannotEnforce(t *testing.T) {
	in := inputFixture(t)
	p, err := profile.LoadFile("../../contracts/examples/v0alpha1/evaluator-profile/quickstart-fixture.json")
	if err != nil {
		t.Fatal(err)
	}
	in.Profile = p
	in.Enforcement = true
	in.AllowSyntheticFixtures = true // enforcement wins even with explicit opt-in
	registry := Registry{p.Spec.Evaluators[0].Adapter: {Mode: profile.ModeDeterministicFixture, Protocol: p.Spec.Evaluators[0].Protocol, Capabilities: p.Spec.Evaluators[0].RequiredCapabilities, FixtureVersion: "v1", Evaluate: func(context.Context, evaluator.Request, Configuration) (evaluator.Result, error) {
		t.Fatal("enforcement fixture executed")
		return evaluator.Result{}, nil
	}}}
	if _, err := Run(context.Background(), in, registry); err == nil {
		t.Fatal("enforcement accepted fixture")
	}
}

func TestFixtureVersionMustMatchEveryRoutedProfileEntry(t *testing.T) {
	for _, tt := range []struct {
		name, registered string
		fallback         bool
	}{
		{"wrong registration", "v2", false},
		{"missing registration version", "", false},
		{"mismatched fallback pin", "v1", true},
	} {
		t.Run(tt.name, func(t *testing.T) {
			in := inputFixture(t)
			p, err := profile.LoadFile("../../contracts/examples/v0alpha1/evaluator-profile/quickstart-fixture.json")
			if err != nil {
				t.Fatal(err)
			}
			if tt.fallback {
				fallback := p.Spec.Evaluators[0]
				fallback.ID = "fixture-fallback"
				fallback.Parameters = map[string]any{"fixtureSet": "quickstart", "fixtureVersion": "v2"}
				p.Spec.Evaluators = append(p.Spec.Evaluators, fallback)
				p.Spec.Routing.Fallbacks = []string{fallback.ID}
				p.Spec.Routing.FallbackOn = []profile.TransientFailure{profile.FailureUnavailable}
			}
			if err := p.Validate(); err != nil {
				t.Fatal(err)
			}
			in.Profile = p
			in.AllowSyntheticFixtures = true
			registry := Registry{p.Spec.Evaluators[0].Adapter: {
				Mode: profile.ModeDeterministicFixture, Protocol: p.Spec.Evaluators[0].Protocol,
				Capabilities: p.Spec.Evaluators[0].RequiredCapabilities, FixtureVersion: tt.registered,
				Evaluate: func(context.Context, evaluator.Request, Configuration) (evaluator.Result, error) {
					t.Fatal("fixture version mismatch must fail before any adapter call")
					return evaluator.Result{}, nil
				},
			}}
			d, err := Run(context.Background(), in, registry)
			if err == nil || err.Error() != "routed fixture version does not match the profile" || d.Kind != "" {
				t.Fatalf("decision=%+v error=%v", d, err)
			}
		})
	}
}

func TestNoSecretOrErrorPayloadInTrace(t *testing.T) {
	in := inputFixture(t)
	d, err := Run(context.Background(), in, installed(func(context.Context, evaluator.Request, Configuration) (evaluator.Result, error) {
		return evaluator.Result{}, errors.New("PRIVATE_TOKEN provider-body")
	}))
	if err != nil {
		t.Fatal(err)
	}
	data, err := json.Marshal(d)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(data), "PRIVATE_TOKEN") || strings.Contains(string(data), "provider-body") {
		t.Fatal("error body leaked")
	}
	// Keep an ordinary Go byte-level assertion alongside schema validation tests.
	if len(data) > 1<<20 {
		t.Fatal("decision exceeds portable byte limit")
	}
}

func TestBackoffPreservesRemainingBudgetForFallback(t *testing.T) {
	in := inputFixture(t)
	in.Profile.Spec.TotalTimeoutMS = 2000
	clock := &fakeTime{current: time.Now()}
	calls := 0
	d, err := run(context.Background(), in, installed(func(_ context.Context, r evaluator.Request, c Configuration) (evaluator.Result, error) {
		calls++
		if calls == 1 {
			clock.current = clock.current.Add(1950 * time.Millisecond)
			return evaluator.Result{}, &evaluator.Error{Code: "evaluator.timeout", Retryable: true}
		}
		if c.Evaluator.ID != "semantic-fallback" || r.Deadline.Sub(clock.current) != 50*time.Millisecond {
			t.Fatal("fallback did not receive remaining budget")
		}
		return evidence(r, c, 1), nil
	}), clock.timing())
	if err != nil {
		t.Fatal(err)
	}
	if calls != 2 || len(clock.delays) != 0 || traceOf(t, d).Terminal != "completed" || !d.Evaluator.Fallback {
		t.Fatalf("calls=%d delays=%v trace=%+v", calls, clock.delays, traceOf(t, d))
	}
}

func TestParentDeadlineAndFixtureLabeling(t *testing.T) {
	in := inputFixture(t)
	in.Profile.Spec.TotalTimeoutMS = 300000
	for i := range in.Profile.Spec.Evaluators {
		in.Profile.Spec.Evaluators[i].TimeoutMS = 100000
	}
	deadline := time.Now().Add(time.Minute)
	ctx, cancel := context.WithDeadline(context.Background(), deadline)
	defer cancel()
	_, err := Run(ctx, in, installed(func(_ context.Context, r evaluator.Request, c Configuration) (evaluator.Result, error) {
		if !r.Deadline.Equal(deadline) {
			t.Fatal("parent deadline not applied")
		}
		return evidence(r, c, 1), nil
	}))
	if err != nil {
		t.Fatal(err)
	}
	p, err := profile.LoadFile("../../contracts/examples/v0alpha1/evaluator-profile/quickstart-fixture.json")
	if err != nil {
		t.Fatal(err)
	}
	in.Profile = p
	in.AllowSyntheticFixtures = true
	registry := Registry{p.Spec.Evaluators[0].Adapter: {Mode: profile.ModeDeterministicFixture, Protocol: p.Spec.Evaluators[0].Protocol, Capabilities: p.Spec.Evaluators[0].RequiredCapabilities, FixtureVersion: "v1", Evaluate: func(_ context.Context, r evaluator.Request, c Configuration) (evaluator.Result, error) {
		if len(c.Credential) != 0 {
			t.Fatal("fixture received credential")
		}
		result := evidence(r, c, 1)
		result.Metadata.Mode = evaluator.ModeDeterministicFixture
		result.Metadata.Synthetic = true
		result.Metadata.FixtureSet = c.Evaluator.Parameters["fixtureSet"].(string)
		result.Metadata.FixtureVersion = "v1"
		return result, nil
	}}}
	d, err := Run(context.Background(), in, registry)
	if err != nil {
		t.Fatal(err)
	}
	if d.Evaluator.Synthetic == nil || !*d.Evaluator.Synthetic || d.Evaluator.FixtureVersion == nil {
		t.Fatal("fixture mislabeled")
	}
	traceOf(t, d)
}

func TestAttemptBufferClearedEvenWhenAdapterPanics(t *testing.T) {
	in := inputFixture(t)
	var held []byte
	func() {
		defer func() {
			if recover() == nil {
				t.Fatal("expected adapter panic")
			}
		}()
		_, _ = Run(context.Background(), in, installed(func(_ context.Context, _ evaluator.Request, c Configuration) (evaluator.Result, error) {
			held = c.Credential
			panic("adapter bug")
		}))
	}()
	if len(held) == 0 || !bytes.Equal(held, make([]byte, len(held))) {
		t.Fatal("panic retained attempt credential")
	}
}
