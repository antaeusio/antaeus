package runner

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/antaeusio/antaeus/decision"
	"github.com/antaeusio/antaeus/evaluator"
	"github.com/antaeusio/antaeus/evaluator/profile"
)

func TestUncertaintyEscalationErrorsRemainFailures(t *testing.T) {
	for _, fallback := range []bool{false, true} {
		for _, code := range []string{"evaluator.timeout", "evaluator.unavailable", "evaluator.throttled", "evaluation.adapter_failed", "evaluation.invalid_result", "evaluation.cancelled", "evaluation.deadline_exceeded"} {
			t.Run(code+map[bool]string{false: "/no-fallback", true: "/exhausted-fallback"}[fallback], func(t *testing.T) {
				in := inputFixture(t)
				if !fallback {
					in.Profile.Spec.Routing.Fallbacks = []string{}
					in.Profile.Spec.Routing.FallbackOn = nil
				}
				for i := range in.Profile.Spec.Evaluators {
					in.Profile.Spec.Evaluators[i].Retry = profile.RetryPolicy{MaxAttempts: 1, RetryOn: []profile.TransientFailure{}}
				}
				clock := &fakeTime{current: time.Now().Add(time.Hour)}
				ctx, cancel := context.WithCancel(context.Background())
				defer cancel()
				d, err := run(ctx, in, installed(func(_ context.Context, r evaluator.Request, c Configuration) (evaluator.Result, error) {
					if c.Evaluator.ID == in.Profile.Spec.Routing.Primary {
						return evidence(r, c, 0.1), nil
					}
					switch code {
					case "evaluation.adapter_failed":
						return evaluator.Result{}, errors.New("synthetic operational error")
					case "evaluation.invalid_result":
						return evaluator.Result{}, nil
					case "evaluation.cancelled":
						cancel()
						return evidence(r, c, 1), nil
					case "evaluation.deadline_exceeded":
						clock.current = clock.current.Add(time.Hour)
						return evidence(r, c, 1), nil
					default:
						return evaluator.Result{}, &evaluator.Error{Code: code, Retryable: true}
					}
				}), clock.timing())
				if err != nil {
					t.Fatal(err)
				}
				assertDecisionFailure(t, d, code, transient(code) != "")
				for _, r := range d.RuleResults {
					if r.Status != decision.RuleFailed {
						t.Fatalf("operational error retained uncertainty: %+v", r)
					}
				}
			})
		}
	}
}

func TestUncertaintyEscalationBackoffCancellation(t *testing.T) {
	in := inputFixture(t)
	in.Profile.Spec.Routing.Fallbacks = []string{}
	in.Profile.Spec.Routing.FallbackOn = nil
	for i := range in.Profile.Spec.Evaluators {
		if in.Profile.Spec.Evaluators[i].ID == *in.Profile.Spec.Routing.Escalation {
			delay := 10
			in.Profile.Spec.Evaluators[i].Retry = profile.RetryPolicy{MaxAttempts: 2, RetryOn: []profile.TransientFailure{profile.FailureTimeout}, InitialBackoffMS: &delay, MaxBackoffMS: &delay, Multiplier: new(1.0)}
		}
	}
	clock := &fakeTime{current: time.Now().Add(time.Hour)}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	timer := clock.timing()
	timer.sleep = func(context.Context, time.Duration) error {
		cancel()
		return context.Canceled
	}
	d, err := run(ctx, in, installed(func(_ context.Context, r evaluator.Request, c Configuration) (evaluator.Result, error) {
		if c.Evaluator.ID == in.Profile.Spec.Routing.Primary {
			return evidence(r, c, 0.1), nil
		}
		return evaluator.Result{}, &evaluator.Error{Code: "evaluator.timeout", Retryable: true}
	}), timer)
	if err != nil {
		t.Fatal(err)
	}
	assertDecisionFailure(t, d, "evaluation.cancelled", false)
}
