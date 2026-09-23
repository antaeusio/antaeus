package runner

import (
	"context"
	"fmt"
	"testing"

	"github.com/antaeusio/antaeus/decision"
	"github.com/antaeusio/antaeus/evaluator"
	"github.com/antaeusio/antaeus/evaluator/profile"
)

func TestSyntheticPermissionAndEnforcementMatrix(t *testing.T) {
	for _, fallback := range []bool{false, true} {
		for _, allow := range []bool{false, true} {
			for _, enforcement := range []bool{false, true} {
				t.Run(fmt.Sprintf("fallback=%t/allow=%t/enforcement=%t", fallback, allow, enforcement), func(t *testing.T) {
					in := inputFixture(t)
					p, err := profile.LoadFile("../../contracts/examples/v0alpha1/evaluator-profile/quickstart-fixture.json")
					if err != nil {
						t.Fatal(err)
					}
					if fallback {
						entry := p.Spec.Evaluators[0]
						entry.ID = "dormant-fallback"
						p.Spec.Evaluators = append(p.Spec.Evaluators, entry)
						p.Spec.Routing.Fallbacks = []string{entry.ID}
						p.Spec.Routing.FallbackOn = []profile.TransientFailure{profile.FailureTimeout}
					}
					in.Profile, in.AllowSyntheticFixtures, in.Enforcement = p, allow, enforcement
					calls := 0
					entry := p.Spec.Evaluators[0]
					registry := Registry{entry.Adapter: {
						Mode: entry.Mode, Protocol: entry.Protocol, Capabilities: entry.RequiredCapabilities, FixtureVersion: "v1",
						Evaluate: func(_ context.Context, r evaluator.Request, c Configuration) (evaluator.Result, error) {
							calls++
							if !allow || enforcement || c.Evaluator.ID != entry.ID {
								t.Fatal("unpermitted or dormant fixture invoked")
							}
							out := evidence(r, c, 1)
							out.Metadata.Mode, out.Metadata.Synthetic = evaluator.ModeDeterministicFixture, true
							out.Metadata.FixtureSet = c.Evaluator.Parameters["fixtureSet"].(string)
							out.Metadata.FixtureVersion = "v1"
							return out, nil
						},
					}}
					d, err := Run(context.Background(), in, registry)
					if enforcement || !allow {
						want := "deterministic fixtures require explicit synthetic execution permission"
						if enforcement {
							want = "deterministic fixtures cannot run in enforcement mode"
						}
						if err == nil || err.Error() != want || calls != 0 || d.Kind != "" {
							t.Fatalf("error=%v calls=%d decision=%+v", err, calls, d)
						}
						return
					}
					if err != nil || calls != 1 || d.Outcome != decision.Outcome(in.Policy.Spec.DefaultOutcome) || d.Evaluator.Synthetic == nil || !*d.Evaluator.Synthetic {
						t.Fatalf("error=%v calls=%d decision=%+v", err, calls, d)
					}
					traceOf(t, d)
				})
			}
		}
	}
}

func TestSemanticExecutionDoesNotRequireSyntheticPermission(t *testing.T) {
	for _, enforcement := range []bool{false, true} {
		in := inputFixture(t)
		in.Enforcement = enforcement
		calls := 0
		d, err := Run(context.Background(), in, installed(func(_ context.Context, r evaluator.Request, c Configuration) (evaluator.Result, error) {
			calls++
			return evidence(r, c, 1), nil
		}))
		if err != nil || calls != 1 || d.Outcome != decision.Outcome(in.Policy.Spec.DefaultOutcome) {
			t.Fatalf("enforcement=%t error=%v calls=%d decision=%+v", enforcement, err, calls, d)
		}
	}
}
