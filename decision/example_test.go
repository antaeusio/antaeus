package decision_test

import (
	"fmt"

	"github.com/antaeusio/antaeus/decision"
	"github.com/antaeusio/antaeus/policy"
)

func ExampleReduce() {
	artifact, err := policy.LoadFile("../contracts/examples/v0alpha1/policy/vendor-onboarding.yaml")
	if err != nil {
		panic(err)
	}
	// Construct synthetic evidence to demonstrate reduction, not inference.
	results := []decision.RuleResult{
		{RuleID: artifact.Spec.Rules[0].ID, Status: decision.RuleNotMatched, ReasonCodes: []string{"example.not_matched"}},
		{RuleID: artifact.Spec.Rules[1].ID, Status: decision.RuleIndeterminate, ReasonCodes: []string{"example.unresolved"}},
	}
	reduced, err := decision.Reduce(artifact, results)
	if err != nil {
		panic(err)
	}
	fmt.Println(reduced.Outcome, reduced.Failure.Code, reduced.Failure.Retryable)

	// A matched policy-authored deny takes precedence; the other rule stays
	// unresolved. No application action is performed by the reducer.
	deny := artifact.Spec.Rules[0].Outcome
	results[0].Status = decision.RuleMatched
	results[0].Outcome = &deny
	results[0].ReasonCodes = []string{"example.matched"}
	reduced, err = decision.Reduce(artifact, results)
	if err != nil {
		panic(err)
	}
	fmt.Println(reduced.Outcome, reduced.ReasonCodes[0], reduced.Failure == nil)
	fmt.Println(results[1].Status)
	// Output:
	// failure evaluation.unresolved_rule false
	// deny policy.deny_rule_matched true
	// indeterminate
}
