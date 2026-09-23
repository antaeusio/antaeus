package runner

import (
	"bytes"
	"context"
	"encoding/json"
	"math"
	"reflect"
	"testing"
	"time"

	"github.com/antaeusio/antaeus/decision"
	"github.com/antaeusio/antaeus/evaluator"
	"github.com/antaeusio/antaeus/evaluator/profile"
	"github.com/antaeusio/antaeus/internal/jsonvalue"
)

func canonicalConfiguration(t *testing.T, value any) []byte {
	t.Helper()
	encoded, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	canonical, err := jsonvalue.CanonicalObject(encoded)
	if err != nil {
		t.Fatal(err)
	}
	return canonical
}

func TestConfigurationCopiesPreserveValidatedValuesAndIsolateMutation(t *testing.T) {
	in := inputFixture(t)
	entry := &in.Profile.Spec.Evaluators[0]
	entry.Parameters = map[string]any{
		"numbers": []any{
			json.Number("9007199254740991"), json.Number("-9007199254740991"),
			json.Number("0.10000000000000001"), json.Number("5e-324"),
			json.Number("1e-7"), json.Number("1e-6"), json.Number("-0"),
		},
		"nested": map[string]any{"items": []any{map[string]any{"text": "<safe> 😀", "enabled": true, "optional": nil}}},
	}
	wantEntry := canonicalConfiguration(t, *entry)
	wantParameters := canonicalConfiguration(t, entry.Parameters)
	wantProfile, err := in.Profile.CanonicalJSON()
	if err != nil {
		t.Fatal(err)
	}
	wantDigest, err := in.Profile.Digest()
	if err != nil {
		t.Fatal(err)
	}
	wantInput := bytes.Clone(in.CanonicalInput)
	wantRevision := *entry.ModelRevision
	calls, validations := 0, 0
	registry := installed(func(_ context.Context, r evaluator.Request, c Configuration) (evaluator.Result, error) {
		calls++
		if validations != len(in.Profile.Spec.Evaluators) {
			t.Fatal("adapter invoked before all parameter objects were validated")
		}
		if got := canonicalConfiguration(t, c.Evaluator); !bytes.Equal(got, wantEntry) {
			t.Fatalf("attempt %d changed evaluator configuration: %s", calls, got)
		}
		if got := canonicalConfiguration(t, c.Evaluator.Parameters); !bytes.Equal(got, wantParameters) {
			t.Fatalf("attempt %d parameters differ from validated canonical bytes", calls)
		}
		if r.ProfileDigest != wantDigest || !bytes.Equal(r.CanonicalInput, wantInput) || r.Rules[0].ID != in.Policy.Spec.Rules[0].ID {
			t.Fatal("attempt did not receive the immutable input snapshot")
		}
		result := evidence(r, c, 1)
		result.Metadata.ModelRevision = wantRevision
		// Mutate every reference-bearing evaluator field. None of these writes
		// may affect the next attempt, routing, trace, or caller-owned profile.
		c.Evaluator.RequiredCapabilities[0] = "changed"
		c.Evaluator.Retry.RetryOn[0] = profile.FailureUnavailable
		*c.Evaluator.Retry.InitialBackoffMS = 999
		*c.Evaluator.Retry.MaxBackoffMS = 999
		*c.Evaluator.Retry.Multiplier = 99
		*c.Evaluator.Provider = "changed"
		*c.Evaluator.Model = "changed"
		*c.Evaluator.ModelRevision = "changed"
		*c.Evaluator.CredentialSlot = "changed"
		*c.Evaluator.InstructionTemplate.ID = "changed"
		c.Evaluator.InstructionTemplate.Digest = "changed"
		c.Evaluator.Parameters["numbers"].([]any)[0] = "changed"
		items := c.Evaluator.Parameters["nested"].(map[string]any)["items"].([]any)
		items[0].(map[string]any)["text"] = "changed"
		c.Evaluator.Parameters["added"] = true
		delete(c.Evaluator.Parameters, "nested")
		r.Rules[0].ID = "changed"
		r.CanonicalInput[0] = '!'
		if calls == 1 {
			return evaluator.Result{}, &evaluator.Error{Code: "evaluator.timeout", Retryable: true}
		}
		return result, nil
	})
	key := entry.Adapter
	a := registry[key]
	a.Parameters = validator(func(data json.RawMessage) error {
		validations++
		if validations == 1 && !bytes.Equal(data, wantParameters) {
			t.Fatalf("validator received unexpected canonical parameters: %s", data)
		}
		// Validators receive a byte copy, not the runner's configuration.
		data[0] = '!'
		return nil
	})
	registry[key] = a
	clock := &fakeTime{current: time.Now().Add(time.Hour)}
	d, err := run(context.Background(), in, registry, clock.timing())
	if err != nil {
		t.Fatal(err)
	}
	if calls != 2 || d.Outcome != decision.Outcome(in.Policy.Spec.DefaultOutcome) || d.Evaluator.ProfileDigest != wantDigest || !reflect.DeepEqual(clock.delays, []time.Duration{100 * time.Millisecond}) {
		t.Fatalf("calls=%d decision=%+v delays=%v", calls, d, clock.delays)
	}
	trace := traceOf(t, d)
	if len(trace.Attempts) != 2 || trace.Terminal != "completed" {
		t.Fatal(trace)
	}
	for _, attempt := range trace.Attempts {
		if attempt.Provider != *entry.Provider || attempt.RequestedModel != *entry.Model || attempt.RequestedRevision != wantRevision {
			t.Fatalf("configuration mutation escaped into trace: %+v", attempt)
		}
	}
	gotProfile, err := in.Profile.CanonicalJSON()
	if err != nil || !bytes.Equal(gotProfile, wantProfile) || !bytes.Equal(in.CanonicalInput, wantInput) {
		t.Fatalf("caller-owned input was mutated: %v", err)
	}
}

func TestNonJSONParametersFailBeforeValidationOrExecution(t *testing.T) {
	for _, tt := range []struct {
		name  string
		value any
	}{
		{"channel", make(chan int)},
		{"function", func() {}},
		{"non-finite", math.Inf(1)},
		{"nan", math.NaN()},
		{"invalid number", json.Number("not-a-number")},
		{"overflow", json.Number("1e400")},
		{"unsafe integer", json.Number("9007199254740992")},
	} {
		t.Run(tt.name, func(t *testing.T) {
			in := inputFixture(t)
			in.Profile.Spec.Evaluators[0].Parameters = map[string]any{"invalid": tt.value}
			registry := installed(func(context.Context, evaluator.Request, Configuration) (evaluator.Result, error) {
				t.Fatal("adapter invoked with invalid parameters")
				return evaluator.Result{}, nil
			})
			key := in.Profile.Spec.Evaluators[0].Adapter
			a := registry[key]
			a.Parameters = validator(func(json.RawMessage) error {
				t.Fatal("parameter validator invoked before valid JSON snapshot")
				return nil
			})
			registry[key] = a
			d, err := Run(context.Background(), in, registry)
			if err == nil || d.Kind != "" {
				t.Fatalf("decision=%+v error=%v", d, err)
			}
		})
	}
}
