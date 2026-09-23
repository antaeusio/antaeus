package runner

import (
	"context"
	"testing"

	"github.com/antaeusio/antaeus/decision"
	"github.com/antaeusio/antaeus/evaluator"
	"github.com/antaeusio/antaeus/evaluator/profile"
	"github.com/antaeusio/antaeus/internal/schematest"
)

func TestResolvedTextConformance(t *testing.T) {
	for _, tt := range schematest.TextCases(t, "../../contracts/conformance/v0alpha1/text/whitespace.json") {
		for _, field := range []string{"model", "revision"} {
			t.Run(tt.Name+"/"+field, func(t *testing.T) {
				in := inputFixture(t)
				for i := range in.Profile.Spec.Evaluators {
					in.Profile.Spec.Evaluators[i].ModelRevision = nil
				}
				d, err := Run(context.Background(), in, installed(func(_ context.Context, r evaluator.Request, c Configuration) (evaluator.Result, error) {
					result := evidence(r, c, 1)
					if field == "model" {
						result.Metadata.Model = tt.Value
					} else {
						result.Metadata.ModelRevision = tt.Value
					}
					return result, nil
				}))
				if err != nil {
					t.Fatal(err)
				}
				trace := traceOf(t, d)
				if tt.ECMANonBlank || tt.Value == "" {
					if d.Outcome != decision.OutcomeReview || trace.Terminal != "completed" {
						t.Fatalf("valid text rejected: %+v", d)
					}
					if field == "model" && (trace.Attempts[0].ResolvedModel != tt.Value || d.Evaluator.Model != tt.Value) {
						t.Fatal("model changed")
					}
					if field == "revision" && (trace.Attempts[0].ResolvedRevision != tt.Value || trace.Attempts[0].RevisionAvailable != (tt.Value != "")) {
						t.Fatal("revision changed")
					}
				} else {
					assertDecisionFailure(t, d, "evaluation.invalid_result", false)
					if trace.Attempts[0].ResolvedModel != "" || trace.Attempts[0].ResolvedRevision != "" {
						t.Fatal("invalid metadata leaked into trace")
					}
				}
			})
		}
	}
}

func TestBlankExecutionVersionFailsBeforeAcceptance(t *testing.T) {
	for _, fixture := range []bool{false, true} {
		in := inputFixture(t)
		registry := installed(func(context.Context, evaluator.Request, Configuration) (evaluator.Result, error) {
			t.Fatal("adapter invoked")
			return evaluator.Result{}, nil
		})
		if fixture {
			p, err := profile.LoadFile("../../contracts/examples/v0alpha1/evaluator-profile/quickstart-fixture.json")
			if err != nil {
				t.Fatal(err)
			}
			p.Spec.Evaluators[0].Parameters["fixtureVersion"] = "\ufeff"
			in.Profile = p
			in.AllowSyntheticFixtures = true
			entry := p.Spec.Evaluators[0]
			registry = Registry{entry.Adapter: {Mode: entry.Mode, Protocol: entry.Protocol, Capabilities: entry.RequiredCapabilities, FixtureVersion: "\ufeff", Evaluate: func(context.Context, evaluator.Request, Configuration) (evaluator.Result, error) {
				t.Fatal("fixture invoked")
				return evaluator.Result{}, nil
			}}}
		} else {
			oldKey := in.Profile.Spec.Evaluators[0].Adapter
			adapter := registry[oldKey]
			delete(registry, oldKey)
			for i := range in.Profile.Spec.Evaluators {
				in.Profile.Spec.Evaluators[i].Adapter.Version = "\ufeff"
			}
			registry[in.Profile.Spec.Evaluators[0].Adapter] = adapter
		}
		// Authored profiles keep their distinct, already-published whitespace rule.
		if err := in.Profile.Validate(); err != nil {
			t.Fatal(err)
		}
		d, err := Run(context.Background(), in, registry)
		if err == nil || err.Error() != "invalid installed adapter metadata" || d.Kind != "" {
			t.Fatalf("fixture=%t decision=%+v err=%v", fixture, d, err)
		}
	}
}
