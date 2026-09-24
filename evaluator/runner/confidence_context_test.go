package runner

import (
	"context"
	"reflect"
	"testing"
	"time"

	"github.com/antaeusio/antaeus/decision"
	"github.com/antaeusio/antaeus/evaluator"
	"github.com/antaeusio/antaeus/evaluator/profile"
)

func TestConfidenceWithoutEscalation(t *testing.T) {
	for _, tt := range []struct {
		name       string
		confidence *float64
		unresolved bool
	}{
		{"missing", nil, true},
		{"below threshold", new(0.74), true},
		{"at threshold", new(0.75), false},
	} {
		t.Run(tt.name, func(t *testing.T) {
			in := inputFixture(t)
			in.Profile.Spec.Routing.Escalation = nil
			action := profile.LowConfidenceIndeterminate
			in.Profile.Spec.Routing.Confidence.OnLowConfidence = &action
			var seen []string
			d, err := Run(context.Background(), in, installed(func(_ context.Context, r evaluator.Request, c Configuration) (evaluator.Result, error) {
				seen = append(seen, c.Evaluator.ID)
				out := evidence(r, c, 1)
				out.RuleResults[1].Confidence = tt.confidence
				return out, nil
			}))
			if err != nil {
				t.Fatal(err)
			}
			trace := traceOf(t, d)
			if !reflect.DeepEqual(seen, []string{"semantic-primary"}) || len(trace.Attempts) != 1 || d.Evaluator.Fallback {
				t.Fatalf("confidence started an operational route: seen=%v trace=%+v", seen, trace)
			}
			wantStatus, wantTerminal := decision.RuleNotMatched, "completed"
			wantOutcome := decision.Outcome(in.Policy.Spec.DefaultOutcome)
			wantReasons := []string{"test.evidence"}
			wantDecisionReasons := []string{"policy.default_outcome"}
			var wantFailure *decision.Failure
			if tt.unresolved {
				wantStatus, wantTerminal, wantOutcome = decision.RuleIndeterminate, "evaluation.unresolved_rule", decision.OutcomeFailure
				wantReasons = []string{"evaluation.low_confidence"}
				wantDecisionReasons = []string{"evaluation.unresolved_rule"}
				wantFailure = &decision.Failure{Code: "evaluation.unresolved_rule", Stage: "evaluation", Retryable: false}
			}
			if d.RuleResults[0].Status != decision.RuleNotMatched || d.RuleResults[1].Status != wantStatus || d.Outcome != wantOutcome || trace.Terminal != wantTerminal || !reflect.DeepEqual(d.RuleResults[1].ReasonCodes, wantReasons) || !reflect.DeepEqual(d.RuleResults[1].Confidence, tt.confidence) {
				t.Fatalf("decision=%+v trace=%+v", d, trace)
			}
			if !reflect.DeepEqual(d.ReasonCodes, wantDecisionReasons) || !reflect.DeepEqual(d.Failure, wantFailure) {
				t.Fatalf("decision reasons=%v failure=%+v; want reasons=%v failure=%+v", d.ReasonCodes, d.Failure, wantDecisionReasons, wantFailure)
			}
		})
	}
}

func TestLowConfidenceFallbackAfterEscalationFailure(t *testing.T) {
	for _, confidence := range []*float64{nil, new(0.1)} {
		name := "missing"
		if confidence != nil {
			name = "low"
		}
		t.Run(name, func(t *testing.T) {
			in := inputFixture(t)
			var seen []string
			d, err := Run(context.Background(), in, installed(func(_ context.Context, r evaluator.Request, c Configuration) (evaluator.Result, error) {
				seen = append(seen, c.Evaluator.ID)
				out := evidence(r, c, 1)
				if c.Evaluator.ID == "semantic-primary" {
					out.RuleResults[0].Status = decision.RuleMatched
					out.RuleResults[1].Confidence = nil
					return out, nil
				}
				if len(r.Rules) != 1 || r.Rules[0].ID != in.Policy.Spec.Rules[1].ID {
					t.Fatal("recovery did not preserve accepted primary subset")
				}
				if c.Evaluator.ID == "semantic-escalation" {
					return evaluator.Result{}, &evaluator.Error{Code: "evaluator.unavailable", Retryable: true}
				}
				out.RuleResults[0].Confidence = confidence
				return out, nil
			}))
			if err != nil {
				t.Fatal(err)
			}
			trace := traceOf(t, d)
			if !reflect.DeepEqual(seen, []string{"semantic-primary", "semantic-escalation", "semantic-fallback"}) || len(trace.Attempts) != 3 || !d.Evaluator.Fallback || trace.Terminal != "evaluation.unresolved_rule" {
				t.Fatalf("seen=%v trace=%+v", seen, trace)
			}
			if d.Outcome != decision.OutcomeDeny || d.Failure != nil || d.RuleResults[0].Status != decision.RuleMatched || !reflect.DeepEqual(d.RuleResults[0].Confidence, new(1.0)) || !reflect.DeepEqual(d.RuleResults[0].ReasonCodes, []string{"test.evidence"}) || d.RuleResults[1].Status != decision.RuleIndeterminate || !reflect.DeepEqual(d.RuleResults[1].Confidence, confidence) || !reflect.DeepEqual(d.RuleResults[1].ReasonCodes, []string{"evaluation.low_confidence"}) {
				t.Fatalf("decision=%+v", d)
			}
			for i, attempt := range trace.Attempts {
				wantIndexes := []int{1}
				if i == 0 {
					wantIndexes = []int{0, 1}
				}
				if !reflect.DeepEqual(attempt.RuleIndexes, wantIndexes) || attempt.Code != []string{"evaluation.succeeded", "evaluator.unavailable", "evaluation.succeeded"}[i] {
					t.Fatalf("attempt=%+v", attempt)
				}
			}
		})
	}
}

func TestAdapterPanicCancelsAttemptAndTotalContexts(t *testing.T) {
	in := inputFixture(t)
	// This tests cleanup, not deadline expiry. Context deadlines use real time
	// even though retry scheduling below uses the injected clock.
	in.Profile.Spec.TotalTimeoutMS = 300000
	for i := range in.Profile.Spec.Evaluators {
		in.Profile.Spec.Evaluators[i].TimeoutMS = 100000
	}
	clock := &fakeTime{current: time.Now()}
	timer := clock.timing()
	var total context.Context
	timer.sleep = func(ctx context.Context, _ time.Duration) error {
		total = ctx
		if ctx.Err() != nil {
			t.Fatalf("total context already done: %v", ctx.Err())
		}
		return nil
	}
	caller, cancel := context.WithCancel(context.Background())
	defer cancel()
	var attempts []context.Context
	const panicValue = "synthetic adapter panic"
	var recovered any
	func() {
		defer func() {
			recovered = recover()
		}()
		_, _ = run(caller, in, installed(func(ctx context.Context, _ evaluator.Request, _ Configuration) (evaluator.Result, error) {
			attempts = append(attempts, ctx)
			if ctx.Err() != nil {
				t.Fatalf("attempt already done: %v", ctx.Err())
			}
			if len(attempts) == 1 {
				return evaluator.Result{}, &evaluator.Error{Code: "evaluator.timeout", Retryable: true}
			}
			panic(panicValue)
		}), timer)
	}()
	if recovered != panicValue {
		t.Fatalf("panic=%v want %q", recovered, panicValue)
	}
	if total == nil || len(attempts) != 2 {
		t.Fatalf("missing observed contexts: total=%v attempts=%d", total != nil, len(attempts))
	}
	for _, ctx := range append(attempts, total) {
		if ctx.Err() != context.Canceled {
			t.Fatalf("runner-owned context not cancelled: %v", ctx.Err())
		}
		select {
		case <-ctx.Done():
		default:
			t.Fatal("runner-owned context Done is not closed")
		}
	}
	if caller.Err() != nil {
		t.Fatalf("runner cancelled caller context: %v", caller.Err())
	}
}
