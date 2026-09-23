package runner_test

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/antaeusio/antaeus/evaluator"
	"github.com/antaeusio/antaeus/evaluator/fixture"
	"github.com/antaeusio/antaeus/evaluator/profile"
	"github.com/antaeusio/antaeus/evaluator/runner"
	"github.com/antaeusio/antaeus/policy"
)

func ExampleRun() {
	p, err := policy.LoadFile("../../contracts/examples/v0alpha1/policy/vendor-onboarding.yaml")
	if err != nil {
		panic(err)
	}
	configuration, err := profile.LoadFile("../../contracts/examples/v0alpha1/evaluator-profile/quickstart-fixture.json")
	if err != nil {
		panic(err)
	}
	set, err := fixture.LoadFile("../../contracts/examples/v0alpha1/fixture-set/quickstart.json")
	if err != nil {
		panic(err)
	}
	adapter, err := fixture.New(set, "aggregate-analytics")
	if err != nil {
		panic(err)
	}
	registry := runner.Registry{
		{ID: fixture.AdapterID, Version: fixture.AdapterVersion}: {
			Mode:           profile.ModeDeterministicFixture,
			Protocol:       profile.ComponentIdentity{ID: "io.antaeus.rule-match", Version: "v0alpha1"},
			Capabilities:   []string{"json-input", "structured-rule-results"},
			FixtureVersion: set.Metadata.Version,
			Evaluate: func(ctx context.Context, request evaluator.Request, _ runner.Configuration) (evaluator.Result, error) {
				return adapter.Evaluate(ctx, request)
			},
		},
	}
	// This literal is already JCS-canonical and matches the selected fixture.
	// Arbitrary input must be strictly parsed and canonicalized by the caller.
	input := json.RawMessage(`{"description":"Processes aggregate product events.","serviceCategory":"analytics"}`)
	d, err := runner.Run(context.Background(), runner.Input{
		Policy:                 p,
		Profile:                configuration,
		CanonicalInput:         input,
		CorrelationID:          "fixture-example",
		AllowSyntheticFixtures: true,
	}, registry)
	if err != nil {
		panic(err)
	}
	fmt.Println(d.Outcome, d.Evaluator.Mode, *d.Evaluator.Synthetic)
	fmt.Println(d.Evaluator.Route, d.Evaluator.Attempts)
	// Output:
	// review deterministic-fixture true
	// [fixture-primary] 1
}
