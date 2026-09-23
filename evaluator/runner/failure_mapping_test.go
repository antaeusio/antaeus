package runner

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	"github.com/antaeusio/antaeus/decision"
	"github.com/antaeusio/antaeus/evaluator"
	"github.com/antaeusio/antaeus/evaluator/profile"
	"github.com/antaeusio/antaeus/policy"
)

type failureExpectation struct {
	Outcome     decision.Outcome  `json:"outcome"`
	ReasonCodes []string          `json:"reasonCodes"`
	Failure     *decision.Failure `json:"failure"`
}

func assertFailureMapping(t *testing.T, got decision.Reduction, want failureExpectation) {
	t.Helper()
	if got.Outcome != want.Outcome || !reflect.DeepEqual(got.ReasonCodes, want.ReasonCodes) || !reflect.DeepEqual(got.Failure, want.Failure) {
		t.Fatalf("got outcome=%s reasons=%v failure=%+v; want %+v failure=%+v", got.Outcome, got.ReasonCodes, got.Failure, want, want.Failure)
	}
}

func assertDecisionFailure(t *testing.T, d decision.Decision, code string, retryable bool) {
	t.Helper()
	assertFailureMapping(t, decision.Reduction{Outcome: d.Outcome, ReasonCodes: d.ReasonCodes, Failure: d.Failure}, failureExpectation{
		Outcome: decision.OutcomeFailure, ReasonCodes: []string{"evaluation.unresolved_rule", code}, Failure: &decision.Failure{Code: code, Stage: "evaluation", Retryable: retryable},
	})
	if traceOf(t, d).Terminal != code {
		t.Fatalf("terminal does not match core failure: %+v", d)
	}
}

func TestFailureMappingConformance(t *testing.T) {
	const path = "../../contracts/conformance/v0alpha1/runner/failure-mapping.json"
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var fixtures struct {
		Policy  string `json:"policy"`
		Profile string `json:"profile"`
		Cases   []struct {
			Name     string                `json:"name"`
			Terminal string                `json:"terminal"`
			Rules    []decision.RuleResult `json:"rules"`
			Expected failureExpectation    `json:"expected"`
		} `json:"cases"`
		CallerDeadlines []struct {
			Name             string             `json:"name"`
			CallerBudgetMS   int                `json:"callerBudgetMs"`
			AdapterElapsedMS int                `json:"adapterElapsedMs"`
			RetryDelayMS     int                `json:"retryDelayMs"`
			Expected         failureExpectation `json:"expected"`
		} `json:"callerDeadlines"`
	}
	if err := json.Unmarshal(data, &fixtures); err != nil {
		t.Fatal(err)
	}
	p, err := policy.LoadFile(filepath.Join(filepath.Dir(path), fixtures.Policy))
	if err != nil {
		t.Fatal(err)
	}
	for _, tt := range fixtures.Cases {
		t.Run(tt.Name, func(t *testing.T) {
			if len(tt.Rules) != len(p.Spec.Rules) {
				t.Fatal("fixture rule count differs from policy")
			}
			for i := range tt.Rules {
				tt.Rules[i].RuleID = p.Spec.Rules[i].ID
				if tt.Rules[i].Status == decision.RuleMatched {
					tt.Rules[i].Outcome = &p.Spec.Rules[i].Outcome
				}
			}
			reduced, err := decision.Reduce(p, tt.Rules)
			if err != nil {
				t.Fatal(err)
			}
			assertFailureMapping(t, operationalFailure(reduced, tt.Rules, tt.Terminal), tt.Expected)
		})
	}
	for _, tt := range fixtures.CallerDeadlines {
		t.Run(tt.Name, func(t *testing.T) {
			in := inputFixture(t)
			in.Policy = p
			in.Profile, err = profile.LoadFile(filepath.Join(filepath.Dir(path), fixtures.Profile))
			if err != nil {
				t.Fatal(err)
			}
			in.Profile.Spec.Routing.Fallbacks = []string{}
			in.Profile.Spec.Routing.FallbackOn = nil
			in.Profile.Spec.Evaluators[0].Retry = profile.RetryPolicy{MaxAttempts: 2, RetryOn: []profile.TransientFailure{profile.FailureThrottled}, InitialBackoffMS: &tt.RetryDelayMS, MaxBackoffMS: &tt.RetryDelayMS, Multiplier: new(1.0)}
			clock := &fakeTime{current: time.Now().Add(time.Hour)}
			ctx, cancel := context.WithDeadline(context.Background(), clock.current.Add(time.Duration(tt.CallerBudgetMS)*time.Millisecond))
			defer cancel()
			calls := 0
			d, err := run(ctx, in, installed(func(context.Context, evaluator.Request, Configuration) (evaluator.Result, error) {
				calls++
				clock.current = clock.current.Add(time.Duration(tt.AdapterElapsedMS) * time.Millisecond)
				return evaluator.Result{}, &evaluator.Error{Code: "evaluator.throttled", Retryable: true}
			}), clock.timing())
			if err != nil {
				t.Fatal(err)
			}
			if calls != 1 || len(clock.delays) != 0 {
				t.Fatalf("calls=%d delays=%v", calls, clock.delays)
			}
			assertFailureMapping(t, decision.Reduction{Outcome: d.Outcome, ReasonCodes: d.ReasonCodes, Failure: d.Failure}, tt.Expected)
			if traceOf(t, d).Terminal != tt.Expected.Failure.Code {
				t.Fatal("trace disagrees with failure")
			}
		})
	}
}

func TestOperationalFailureMetadataThroughRunner(t *testing.T) {
	for _, code := range []string{"evaluator.timeout", "evaluator.unavailable", "evaluator.throttled", "evaluation.adapter_failed", "evaluation.invalid_result", "evaluation.cancelled", "evaluation.deadline_exceeded"} {
		t.Run(code, func(t *testing.T) {
			in := inputFixture(t)
			in.Profile.Spec.Routing.Fallbacks = []string{}
			in.Profile.Spec.Routing.FallbackOn = nil
			in.Profile.Spec.Evaluators[0].Retry = profile.RetryPolicy{MaxAttempts: 1, RetryOn: []profile.TransientFailure{}}
			clock := &fakeTime{current: time.Now().Add(time.Hour)}
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			d, err := run(ctx, in, installed(func(_ context.Context, r evaluator.Request, c Configuration) (evaluator.Result, error) {
				switch code {
				case "evaluation.adapter_failed":
					return evaluator.Result{}, errors.New("PRIVATE_ERROR")
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
		})
	}
}

func TestOperationalFailureWithRetainedPrimaryEvidence(t *testing.T) {
	for _, status := range []decision.RuleStatus{decision.RuleFailed, decision.RuleNotMatched, decision.RuleMatched} {
		t.Run(string(status), func(t *testing.T) {
			in := inputFixture(t)
			in.Profile.Spec.Routing.Fallbacks = []string{}
			in.Profile.Spec.Routing.FallbackOn = nil
			clock := &fakeTime{current: time.Now().Add(time.Hour)}
			d, err := run(context.Background(), in, installed(func(_ context.Context, r evaluator.Request, c Configuration) (evaluator.Result, error) {
				if c.Evaluator.ID != in.Profile.Spec.Routing.Primary {
					return evaluator.Result{}, &evaluator.Error{Code: "evaluator.throttled", Retryable: true}
				}
				result := evidence(r, c, 1)
				result.RuleResults[0].Status = status
				result.RuleResults[1].Confidence = new(0.0)
				return result, nil
			}), clock.timing())
			if err != nil {
				t.Fatal(err)
			}
			if status == decision.RuleMatched {
				if d.Outcome != decision.OutcomeDeny || d.Failure != nil || !reflect.DeepEqual(d.ReasonCodes, []string{"policy.deny_rule_matched"}) {
					t.Fatalf("deny changed: %+v", d)
				}
				if traceOf(t, d).Terminal != "evaluator.throttled" {
					t.Fatal("missing operational trace")
				}
			} else {
				assertDecisionFailure(t, d, "evaluator.throttled", status == decision.RuleNotMatched)
			}
		})
	}
}
