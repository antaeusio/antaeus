package runner

import (
	"bytes"
	"context"
	"encoding/json"
	"reflect"
	"testing"
	"time"

	"github.com/antaeusio/antaeus/decision"
	"github.com/antaeusio/antaeus/evaluator"
	"github.com/antaeusio/antaeus/evaluator/localbinding"
	"github.com/antaeusio/antaeus/evaluator/profile"
)

func TestPrimaryAndFallbackEligibilityTransitions(t *testing.T) {
	for _, tt := range []struct {
		name, primaryCode, fallbackCode string
		on                              []profile.TransientFailure
		wantIDs                         []string
		wantTerminal                    string
	}{
		{"primary class excluded", "evaluator.unavailable", "", []profile.TransientFailure{profile.FailureTimeout}, []string{"semantic-primary"}, "evaluator.unavailable"},
		{"fallback class excluded", "evaluator.timeout", "evaluator.unavailable", []profile.TransientFailure{profile.FailureTimeout}, []string{"semantic-primary", "fallback-one"}, "evaluator.unavailable"},
		{"two eligible fallbacks", "evaluator.timeout", "evaluator.throttled", []profile.TransientFailure{profile.FailureTimeout, profile.FailureThrottled}, []string{"semantic-primary", "fallback-one", "fallback-two"}, "completed"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			in := inputFixture(t)
			primary := in.Profile.Spec.Evaluators[0]
			primary.Retry = profile.RetryPolicy{MaxAttempts: 1, RetryOn: []profile.TransientFailure{}}
			one, two := primary, primary
			one.ID, two.ID = "fallback-one", "fallback-two"
			in.Profile.Spec.Evaluators = []profile.Evaluator{primary, one, two}
			in.Profile.Spec.Routing.Escalation = nil
			in.Profile.Spec.Routing.Confidence = profile.Confidence{Enabled: false}
			in.Profile.Spec.Routing.Fallbacks = []string{one.ID, two.ID}
			in.Profile.Spec.Routing.FallbackOn = tt.on
			var seen []string
			d, err := Run(context.Background(), in, installed(func(_ context.Context, r evaluator.Request, c Configuration) (evaluator.Result, error) {
				seen = append(seen, c.Evaluator.ID)
				if len(r.Rules) != len(in.Policy.Spec.Rules) {
					t.Fatal("fallback lost rules")
				}
				switch c.Evaluator.ID {
				case primary.ID:
					return evaluator.Result{}, &evaluator.Error{Code: tt.primaryCode, Retryable: true}
				case one.ID:
					return evaluator.Result{}, &evaluator.Error{Code: tt.fallbackCode, Retryable: true}
				default:
					return evidence(r, c, 1), nil
				}
			}))
			if err != nil {
				t.Fatal(err)
			}
			trace := traceOf(t, d)
			if !reflect.DeepEqual(seen, tt.wantIDs) || !reflect.DeepEqual(d.Evaluator.Route, tt.wantIDs) || trace.Terminal != tt.wantTerminal || len(trace.Attempts) != len(seen) || d.Evaluator.Fallback != (len(seen) > 1) {
				t.Fatalf("seen=%v trace=%+v evaluator=%+v", seen, trace, d.Evaluator)
			}
			wantOutcome := decision.OutcomeFailure
			if tt.wantTerminal == "completed" {
				wantOutcome = decision.Outcome(in.Policy.Spec.DefaultOutcome)
			}
			if d.Outcome != wantOutcome {
				t.Fatalf("outcome=%s want=%s", d.Outcome, wantOutcome)
			}
			for i, attempt := range trace.Attempts {
				wantCode := []string{tt.primaryCode, tt.fallbackCode, "evaluation.succeeded"}[i]
				wantRoute := "fallback"
				if i == 0 {
					wantRoute = "primary"
				}
				if attempt.Code != wantCode || attempt.EvaluatorID != seen[i] || attempt.Route != wantRoute || attempt.Attempt != 1 || !reflect.DeepEqual(attempt.RuleIndexes, []int{0, 1}) {
					t.Fatalf("unexpected attempt: %+v", attempt)
				}
			}
		})
	}
}

func TestLateAttemptSuccessIsTracedAsTimeoutBeforeRetry(t *testing.T) {
	in := inputFixture(t)
	// Keep fake deadlines ahead of the real context clock, even on a busy host.
	clock := &fakeTime{current: time.Now().Add(time.Hour)}
	calls := 0
	d, err := run(context.Background(), in, installed(func(_ context.Context, r evaluator.Request, c Configuration) (evaluator.Result, error) {
		calls++
		if calls == 1 {
			clock.current = r.Deadline
		}
		return evidence(r, c, 1), nil
	}), clock.timing())
	if err != nil {
		t.Fatal(err)
	}
	trace := traceOf(t, d)
	if calls != 2 || d.Outcome != decision.Outcome(in.Policy.Spec.DefaultOutcome) || trace.Terminal != "completed" || len(trace.Attempts) != 2 {
		t.Fatalf("calls=%d decision=%+v trace=%+v", calls, d, trace)
	}
	if trace.Attempts[0].Code != "evaluator.timeout" || trace.Attempts[0].Attempt != 1 || trace.Attempts[0].LatencyMS != int64(in.Profile.Spec.Evaluators[0].TimeoutMS) || trace.Attempts[1].Code != "evaluation.succeeded" || trace.Attempts[1].Attempt != 2 || d.Evaluator.Fallback {
		t.Fatalf("incorrect timeout retry trace: %+v", trace)
	}
	if !reflect.DeepEqual(clock.delays, []time.Duration{100 * time.Millisecond}) {
		t.Fatal(clock.delays)
	}
}

func TestRegistryRequiresExactAdapterVersion(t *testing.T) {
	in := inputFixture(t)
	registry := installed(func(context.Context, evaluator.Request, Configuration) (evaluator.Result, error) {
		t.Fatal("wrong adapter version invoked")
		return evaluator.Result{}, nil
	})
	key := in.Profile.Spec.Evaluators[0].Adapter
	a := registry[key]
	delete(registry, key)
	key.Version = "2.0.0"
	registry[key] = a
	d, err := Run(context.Background(), in, registry)
	if err == nil || err.Error() != "installed adapter parameter validation failed" || d.Kind != "" {
		t.Fatalf("decision=%+v error=%v", d, err)
	}
	// Fixtures skip semantic parameter validation, so exercise the separate
	// exact routed-identity check, including a dormant fallback.
	for _, fallbackOnly := range []bool{false, true} {
		name := "fixture primary"
		if fallbackOnly {
			name = "fixture fallback"
		}
		t.Run(name, func(t *testing.T) {
			in := inputFixture(t)
			p, err := profile.LoadFile("../../contracts/examples/v0alpha1/evaluator-profile/quickstart-fixture.json")
			if err != nil {
				t.Fatal(err)
			}
			key := p.Spec.Evaluators[0].Adapter
			if fallbackOnly {
				fallback := p.Spec.Evaluators[0]
				fallback.ID = "fixture-fallback"
				fallback.Adapter.Version = "2.0.0"
				p.Spec.Evaluators = append(p.Spec.Evaluators, fallback)
				p.Spec.Routing.Fallbacks = []string{fallback.ID}
				p.Spec.Routing.FallbackOn = []profile.TransientFailure{profile.FailureTimeout}
			} else {
				key.Version = "2.0.0"
			}
			if err := p.Validate(); err != nil {
				t.Fatal(err)
			}
			in.Profile = p
			registry := Registry{key: {Mode: profile.ModeDeterministicFixture, Protocol: p.Spec.Evaluators[0].Protocol, Capabilities: p.Spec.Evaluators[0].RequiredCapabilities, FixtureVersion: "v1", Evaluate: func(context.Context, evaluator.Request, Configuration) (evaluator.Result, error) {
				t.Fatal("wrong routed adapter version accepted")
				return evaluator.Result{}, nil
			}}}
			d, err := Run(context.Background(), in, registry)
			if err == nil || err.Error() != "routed adapter identity, mode, or protocol is unsupported" || d.Kind != "" {
				t.Fatalf("decision=%+v error=%v", d, err)
			}
		})
	}
}

func TestCredentialsAreIsolatedByAdapterAndSlot(t *testing.T) {
	for _, separateAdapter := range []bool{false, true} {
		name := "same adapter different slots"
		if separateAdapter {
			name = "different adapters same slot"
		}
		t.Run(name, func(t *testing.T) {
			in := inputFixture(t)
			primary := in.Profile.Spec.Evaluators[0]
			primary.Retry = profile.RetryPolicy{MaxAttempts: 1, RetryOn: []profile.TransientFailure{}}
			fallback, unused := primary, primary
			fallback.ID, unused.ID = "fallback", "uninvoked-fallback"
			if separateAdapter {
				fallback.Adapter.ID, unused.Adapter.ID = "io.example.second", "io.example.third"
			} else {
				fallback.CredentialSlot, unused.CredentialSlot = new("second-key"), new("third-key")
				in.Profile.Spec.CredentialSlots = append(in.Profile.Spec.CredentialSlots, profile.CredentialSlot{Name: "second-key"}, profile.CredentialSlot{Name: "third-key"})
			}
			in.Profile.Spec.Evaluators = []profile.Evaluator{primary, fallback, unused}
			outside := primary
			outside.ID, outside.Adapter.ID, outside.CredentialSlot = "outside-route", "io.example.outside", new("unused-key")
			in.Profile.Spec.Evaluators = append(in.Profile.Spec.Evaluators, outside)
			in.Profile.Spec.CredentialSlots = append(in.Profile.Spec.CredentialSlots, profile.CredentialSlot{Name: "unused-key"})
			in.Profile.Spec.Routing.Escalation = nil
			in.Profile.Spec.Routing.Confidence = profile.Confidence{Enabled: false}
			in.Profile.Spec.Routing.Fallbacks = []string{fallback.ID, unused.ID}
			in.Profile.Spec.Routing.FallbackOn = []profile.TransientFailure{profile.FailureUnavailable}
			bindings := localbinding.Artifact{APIVersion: localbinding.APIVersion, Kind: localbinding.Kind, SecretBindings: map[string]map[string]localbinding.Reference{}}
			entries := []profile.Evaluator{primary, fallback, unused}
			names := []string{"TEST_PRIMARY_KEY", "TEST_FALLBACK_KEY", "TEST_UNUSED_KEY"}
			values := []string{"primary-only-synthetic-token", "fallback-only-synthetic-token", "unused-only-synthetic-token"}
			for i, entry := range entries {
				if bindings.SecretBindings[entry.Adapter.ID] == nil {
					bindings.SecretBindings[entry.Adapter.ID] = map[string]localbinding.Reference{}
				}
				bindings.SecretBindings[entry.Adapter.ID][*entry.CredentialSlot] = localbinding.Reference{Source: "environment", Name: names[i]}
			}
			reads := map[string]int{}
			bindings.SecretBindings["io.example.outside"] = map[string]localbinding.Reference{"unused-key": {Source: "environment", Name: "TEST_OUTSIDE_ROUTE_KEY"}}
			credentials, err := localbinding.Preflight(in.Profile, bindings, localbinding.EnvironmentFunc(func(name string) (string, bool) {
				reads[name]++
				for i, expected := range names {
					if name == expected {
						return values[i], true
					}
				}
				t.Fatal("unexpected reference lookup")
				return "", false
			}))
			if err != nil {
				t.Fatal(err)
			}
			defer credentials.Clear()
			if _, ok := credentials.Credential("io.example.outside", "unused-key"); ok {
				t.Fatal("outside-route credential captured")
			}
			in.Credentials = credentials
			var buffers [][]byte
			var seen []string
			fn := func(_ context.Context, r evaluator.Request, c Configuration) (evaluator.Result, error) {
				for _, previous := range buffers {
					if !bytes.Equal(previous, make([]byte, len(previous))) {
						t.Fatal("previous attempt retained its credential")
					}
				}
				seen = append(seen, c.Evaluator.ID)
				i := len(seen) - 1
				if i > 1 || c.Evaluator.ID != entries[i].ID || string(c.Credential) != values[i] {
					t.Fatal("wrong credential or evaluator invoked")
				}
				buffers = append(buffers, c.Credential)
				c.Credential[0] = 'X' // mutation must not affect the caller's set
				if i == 0 {
					return evaluator.Result{}, &evaluator.Error{Code: "evaluator.unavailable", Retryable: true}
				}
				return evidence(r, c, 1), nil
			}
			registry := Registry{}
			for _, entry := range in.Profile.Spec.Evaluators {
				registry[entry.Adapter] = Adapter{Mode: profile.ModeSemantic, Protocol: entry.Protocol, Capabilities: entry.RequiredCapabilities, Parameters: validator(func(json.RawMessage) error { return nil }), Evaluate: fn}
			}
			d, err := Run(context.Background(), in, registry)
			if err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(seen, []string{primary.ID, fallback.ID}) || len(traceOf(t, d).Attempts) != 2 {
				t.Fatal(seen)
			}
			for _, held := range buffers {
				if !bytes.Equal(held, make([]byte, len(held))) {
					t.Fatal("attempt credential survived completion")
				}
			}
			encoded, err := json.Marshal(d)
			if err != nil {
				t.Fatal(err)
			}
			for i, entry := range entries {
				if reads[names[i]] != 1 {
					t.Fatal("reference was not captured exactly once")
				}
				if bytes.Contains(encoded, []byte(names[i])) || bytes.Contains(encoded, []byte(values[i])) {
					t.Fatal("Decision exposed reference or value")
				}
				captured, ok := credentials.Credential(entry.Adapter.ID, *entry.CredentialSlot)
				if !ok || string(captured) != values[i] {
					t.Fatal("runner changed caller-owned credentials")
				}
				clear(captured)
			}
			credentials.Clear()
			for _, entry := range entries {
				if _, ok := credentials.Credential(entry.Adapter.ID, *entry.CredentialSlot); ok {
					t.Fatal("caller Clear retained a credential")
				}
			}
		})
	}
}

func TestRetryGetsFreshCredentialAfterPriorBufferMutationAndClear(t *testing.T) {
	in := inputFixture(t)
	clock := &fakeTime{current: time.Now().Add(time.Hour)}
	var buffers [][]byte
	calls := 0
	d, err := run(context.Background(), in, installed(func(_ context.Context, r evaluator.Request, c Configuration) (evaluator.Result, error) {
		calls++
		if string(c.Credential) != "PRIVATE_TOKEN" {
			t.Fatal("retry did not receive the original captured credential")
		}
		for _, prior := range buffers {
			if !bytes.Equal(prior, make([]byte, len(prior))) {
				t.Fatal("prior attempt buffer was not cleared before retry")
			}
		}
		buffers = append(buffers, c.Credential)
		c.Credential[0] = 'X'
		if calls == 1 {
			return evaluator.Result{}, &evaluator.Error{Code: "evaluator.throttled", Retryable: true}
		}
		return evidence(r, c, 1), nil
	}), clock.timing())
	if err != nil {
		t.Fatal(err)
	}
	trace := traceOf(t, d)
	if calls != 2 || len(buffers) != 2 || trace.Terminal != "completed" || len(trace.Attempts) != 2 {
		t.Fatalf("calls=%d trace=%+v", calls, trace)
	}
	for _, held := range buffers {
		if !bytes.Equal(held, make([]byte, len(held))) {
			t.Fatal("final attempt retained credential bytes")
		}
	}
}

func TestExecutionTraceSchemaRejectsInvalidRecords(t *testing.T) {
	in := inputFixture(t)
	d, err := Run(context.Background(), in, installed(func(_ context.Context, r evaluator.Request, c Configuration) (evaluator.Result, error) {
		return evidence(r, c, 1), nil
	}))
	if err != nil {
		t.Fatal(err)
	}
	traceOf(t, d) // establish that the unmodified baseline conforms
	schema := traceSchema(t)
	// Positive controls prove boundary-valid mutations reach schema validation
	// as JSON values, not unsupported Go slice types rejected by the library.
	var valid map[string]any
	if err := json.Unmarshal(d.Extensions[TraceExtension], &valid); err != nil {
		t.Fatal(err)
	}
	validAttempt := valid["attempts"].([]any)[0].(map[string]any)
	validAttempt["ruleIndexes"] = []any{0, 255}
	validAttempt["attempt"] = 5
	validAttempt["revisionAvailable"] = true
	validAttempt["resolvedRevision"] = "revision-1"
	if err := schema.Validate(valid); err != nil {
		t.Fatalf("valid boundary rejected: %v", err)
	}
	for _, tt := range []struct {
		name   string
		mutate func(map[string]any, map[string]any)
	}{
		{"revision flag without evidence", func(_, a map[string]any) { a["revisionAvailable"] = true }},
		{"revision evidence without flag", func(_, a map[string]any) { a["resolvedRevision"] = "revision-1" }},
		{"attempt above limit", func(_, a map[string]any) { a["attempt"] = 6 }},
		{"attempt below limit", func(_, a map[string]any) { a["attempt"] = 0 }},
		{"unknown attempt code", func(_, a map[string]any) { a["code"] = "evaluation.unknown" }},
		{"unknown terminal code", func(root, _ map[string]any) { root["terminal"] = "evaluation.unknown" }},
		{"duplicate indexes", func(_, a map[string]any) { a["ruleIndexes"] = []any{0, 0} }},
		{"index above limit", func(_, a map[string]any) { a["ruleIndexes"] = []any{256} }},
		{"extra root field", func(root, _ map[string]any) { root["message"] = "unrestricted payload" }},
		{"extra attempt field", func(_, a map[string]any) { a["credential"] = "synthetic secret" }},
		{"wrong version", func(root, _ map[string]any) { root["version"] = "v99" }},
		{"unknown route", func(_, a map[string]any) { a["route"] = "unconfigured" }},
		{"negative latency", func(_, a map[string]any) { a["latencyMs"] = -1 }},
		{"missing required field", func(_, a map[string]any) { delete(a, "adapterId") }},
		{"empty attempts", func(root, _ map[string]any) { root["attempts"] = []any{} }},
		{"attempts above limit", func(root, a map[string]any) {
			attempts := make([]any, 65)
			for i := range attempts {
				attempts[i] = a
			}
			root["attempts"] = attempts
		}},
	} {
		t.Run(tt.name, func(t *testing.T) {
			var instance map[string]any
			if err := json.Unmarshal(d.Extensions[TraceExtension], &instance); err != nil {
				t.Fatal(err)
			}
			attempt := instance["attempts"].([]any)[0].(map[string]any)
			tt.mutate(instance, attempt)
			if err := schema.Validate(instance); err == nil {
				t.Fatal("invalid trace accepted")
			}
		})
	}
}
