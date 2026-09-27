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

func TestRetryBudgetBoundaries(t *testing.T) {
	for _, tt := range []struct {
		name      string
		remaining time.Duration
		fallback  bool
		stop      string
		wantIDs   []string
		terminal  string
		sleeps    int
		draws     int
	}{
		{"greater delay", 50 * time.Millisecond, true, "", []string{"semantic-primary", "semantic-fallback"}, "completed", 0, 1},
		{"equal delay", 100 * time.Millisecond, true, "", []string{"semantic-primary", "semantic-fallback"}, "completed", 0, 1},
		{"positive retry budget", 101 * time.Millisecond, true, "", []string{"semantic-primary", "semantic-primary"}, "completed", 1, 1},
		{"no fallback", 50 * time.Millisecond, false, "", []string{"semantic-primary"}, "evaluator.timeout", 0, 1},
		{"deadline exhausted", 0, true, "", []string{"semantic-primary"}, "evaluation.deadline_exceeded", 0, 0},
		{"cancel during jitter", 50 * time.Millisecond, true, "cancel", []string{"semantic-primary"}, "evaluation.cancelled", 0, 1},
		{"cancel during jitter without fallback", 50 * time.Millisecond, false, "cancel", []string{"semantic-primary"}, "evaluation.cancelled", 0, 1},
		{"deadline during jitter wins cancellation", 50 * time.Millisecond, true, "both", []string{"semantic-primary"}, "evaluation.deadline_exceeded", 0, 1},
		{"deadline during jitter without fallback", 50 * time.Millisecond, false, "both", []string{"semantic-primary"}, "evaluation.deadline_exceeded", 0, 1},
	} {
		t.Run(tt.name, func(t *testing.T) {
			in := inputFixture(t)
			in.Profile.Spec.TotalTimeoutMS = 2000
			in.Profile.Spec.Routing.Escalation = nil
			in.Profile.Spec.Routing.Confidence = profile.Confidence{Enabled: false}
			if !tt.fallback {
				in.Profile.Spec.Routing.Fallbacks = []string{}
				in.Profile.Spec.Routing.FallbackOn = nil
			}
			clock := &fakeTime{current: time.Now().Add(time.Hour)}
			end := clock.current.Add(2 * time.Second)
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			timing := clock.timing()
			draws := 0
			timing.jitter = func(d time.Duration) time.Duration {
				draws++
				if tt.stop != "" {
					cancel()
				}
				if tt.stop == "both" {
					clock.current = end
				}
				return d
			}
			var ids []string
			d, err := run(ctx, in, installed(func(_ context.Context, r evaluator.Request, c Configuration) (evaluator.Result, error) {
				ids = append(ids, c.Evaluator.ID)
				if len(ids) == 1 {
					clock.current = end.Add(-tt.remaining)
					return evaluator.Result{}, &evaluator.Error{Code: "evaluator.timeout", Retryable: true}
				}
				if !r.Deadline.Equal(end) {
					t.Fatal("recovery attempt exceeded the total deadline")
				}
				return evidence(r, c, 1), nil
			}), timing)
			if err != nil {
				t.Fatal(err)
			}
			trace := traceOf(t, d)
			if !reflect.DeepEqual(ids, tt.wantIDs) || trace.Terminal != tt.terminal || len(trace.Attempts) != len(ids) || len(clock.delays) != tt.sleeps || draws != tt.draws {
				t.Fatalf("ids=%v trace=%+v delays=%v draws=%d", ids, trace, clock.delays, draws)
			}
			wantOutcome := decision.OutcomeFailure
			if tt.terminal == "completed" {
				wantOutcome = decision.Outcome(in.Policy.Spec.DefaultOutcome)
			}
			if d.Outcome != wantOutcome {
				t.Fatal(d.Outcome)
			}
			if tt.sleeps == 1 && clock.delays[0] != 100*time.Millisecond {
				t.Fatal(clock.delays)
			}
			if tt.name == "no fallback" && end.Sub(clock.current) != tt.remaining {
				t.Fatal("return consumed the remaining budget")
			}
		})
	}
}

func TestFutileEscalationRetryPreservesSubsetAndPrimaryDeny(t *testing.T) {
	in := inputFixture(t)
	in.Profile.Spec.TotalTimeoutMS = 2000
	in.Profile.Spec.Evaluators[1].Retry = in.Profile.Spec.Evaluators[0].Retry
	clock := &fakeTime{current: time.Now().Add(time.Hour)}
	var ids []string
	d, err := run(context.Background(), in, installed(func(_ context.Context, r evaluator.Request, c Configuration) (evaluator.Result, error) {
		ids = append(ids, c.Evaluator.ID)
		if c.Evaluator.ID == "semantic-primary" {
			out := evidence(r, c, 1)
			out.RuleResults[0].Status = decision.RuleMatched
			out.RuleResults[1].Confidence = nil
			return out, nil
		}
		if len(r.Rules) != 1 || r.Rules[0].ID != in.Policy.Spec.Rules[1].ID {
			t.Fatal("recovery changed the escalation subset")
		}
		if c.Evaluator.ID == "semantic-escalation" {
			clock.current = clock.current.Add(1950 * time.Millisecond)
			return evaluator.Result{}, &evaluator.Error{Code: "evaluator.timeout", Retryable: true}
		}
		return evidence(r, c, 1), nil
	}), clock.timing())
	if err != nil {
		t.Fatal(err)
	}
	trace := traceOf(t, d)
	if !reflect.DeepEqual(ids, []string{"semantic-primary", "semantic-escalation", "semantic-fallback"}) || len(clock.delays) != 0 || d.Outcome != decision.OutcomeDeny || d.RuleResults[0].Status != decision.RuleMatched || d.RuleResults[1].Status != decision.RuleNotMatched || trace.Terminal != "completed" || !reflect.DeepEqual(trace.Attempts[2].RuleIndexes, []int{1}) {
		t.Fatalf("ids=%v delays=%v decision=%+v trace=%+v", ids, clock.delays, d, trace)
	}
}

func TestProviderRetryDelayIsAFloor(t *testing.T) {
	for _, tt := range []struct {
		name       string
		totalMS    int
		retryAfter time.Duration
		fallback   bool
		cancel     bool
		wantIDs    []string
		terminal   string
		delays     []time.Duration
	}{
		// Jitter halves the 100ms backoff; the provider delay still wins.
		{"longer than jittered backoff", 2000, 500 * time.Millisecond, true, false, []string{"semantic-primary", "semantic-primary"}, "completed", []time.Duration{500 * time.Millisecond}},
		// The profile caps backoff at 1s; the provider delay is not capped.
		{"longer than backoff cap", 10000, 5 * time.Second, true, false, []string{"semantic-primary", "semantic-primary"}, "completed", []time.Duration{5 * time.Second}},
		{"shorter than backoff", 2000, time.Millisecond, true, false, []string{"semantic-primary", "semantic-primary"}, "completed", []time.Duration{50 * time.Millisecond}},
		// Five seconds required with two remaining: no wait, no retry.
		{"exceeds remaining budget uses fallback", 2000, 5 * time.Second, true, false, []string{"semantic-primary", "semantic-fallback"}, "completed", nil},
		{"exceeds remaining budget without fallback", 2000, 5 * time.Second, false, false, []string{"semantic-primary"}, "evaluator.throttled", nil},
		{"cancel during provider wait", 10000, 5 * time.Second, true, true, []string{"semantic-primary"}, "evaluation.cancelled", nil},
	} {
		t.Run(tt.name, func(t *testing.T) {
			in := inputFixture(t)
			in.Profile.Spec.TotalTimeoutMS = tt.totalMS
			in.Profile.Spec.Routing.Escalation = nil
			in.Profile.Spec.Routing.Confidence = profile.Confidence{Enabled: false}
			if !tt.fallback {
				in.Profile.Spec.Routing.Fallbacks = []string{}
				in.Profile.Spec.Routing.FallbackOn = nil
			} else {
				in.Profile.Spec.Routing.FallbackOn = []profile.TransientFailure{profile.FailureThrottled}
			}
			clock := &fakeTime{current: time.Now().Add(time.Hour)}
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			timing := clock.timing()
			timing.jitter = func(d time.Duration) time.Duration { return d / 2 }
			if tt.cancel {
				sleep := timing.sleep
				timing.sleep = func(ctx context.Context, d time.Duration) error {
					cancel()
					return sleep(ctx, d)
				}
			}
			var ids []string
			d, err := run(ctx, in, installed(func(_ context.Context, r evaluator.Request, c Configuration) (evaluator.Result, error) {
				ids = append(ids, c.Evaluator.ID)
				if len(ids) == 1 {
					return evaluator.Result{}, &evaluator.Error{Code: "evaluator.throttled", Retryable: true, RetryAfter: tt.retryAfter}
				}
				return evidence(r, c, 1), nil
			}), timing)
			if err != nil {
				t.Fatal(err)
			}
			trace := traceOf(t, d)
			if !reflect.DeepEqual(ids, tt.wantIDs) || trace.Terminal != tt.terminal || !reflect.DeepEqual(clock.delays, tt.delays) {
				t.Fatalf("ids=%v terminal=%s delays=%v", ids, trace.Terminal, clock.delays)
			}
		})
	}
}

func TestProviderRetryDelayIgnoredWhenNotRetryable(t *testing.T) {
	in := inputFixture(t)
	in.Profile.Spec.Routing.Escalation = nil
	in.Profile.Spec.Routing.Confidence = profile.Confidence{Enabled: false}
	clock := &fakeTime{current: time.Now().Add(time.Hour)}
	calls := 0
	d, err := run(context.Background(), in, installed(func(_ context.Context, r evaluator.Request, c Configuration) (evaluator.Result, error) {
		calls++
		return evaluator.Result{}, &evaluator.Error{Code: "evaluator.throttled", Retryable: false, RetryAfter: time.Second}
	}), clock.timing())
	if err != nil {
		t.Fatal(err)
	}
	if calls != 1 || len(clock.delays) != 0 || d.Outcome != decision.OutcomeFailure {
		t.Fatalf("calls=%d delays=%v outcome=%s", calls, clock.delays, d.Outcome)
	}
}
