package decision

import (
	"encoding/json"
	"reflect"
	"testing"

	"github.com/antaeusio/antaeus/policy"
)

func TestUncertaintyOverridesEveryPolicyDefault(t *testing.T) {
	for _, fallback := range []policy.Outcome{policy.OutcomeAllow, policy.OutcomeReview, policy.OutcomeDeny} {
		t.Run(string(fallback), func(t *testing.T) {
			artifact := reductionPolicy()
			artifact.Spec.DefaultOutcome = fallback
			rules := []RuleResult{result("deny", RuleIndeterminate, nil), result("review", RuleNotMatched, nil), result("allow", RuleNotMatched, nil)}
			rules[0].Confidence = new(0.6)
			rules[0].ReasonCodes = []string{"evaluation.low_confidence"}
			reduced, err := Reduce(artifact, rules)
			if err != nil {
				t.Fatal(err)
			}
			if reduced.Outcome != OutcomeReview || reduced.Failure != nil || !reflect.DeepEqual(reduced.ReasonCodes, []string{"evaluation.indeterminate_rule"}) {
				t.Fatalf("unexpected reduction: %+v", reduced)
			}
			digest, _ := artifact.Digest()
			d := Decision{APIVersion: APIVersion, Kind: KindDecision, Policy: PolicyIdentity{Name: artifact.Metadata.Name, Digest: digest}, Outcome: reduced.Outcome, RuleResults: rules, ReasonCodes: reduced.ReasonCodes}
			if err := d.ValidateAgainst(artifact); err != nil {
				t.Fatal(err)
			}
			d.Outcome = OutcomeFailure
			d.Failure = &Failure{Code: "evaluation.unresolved_rule", Stage: "evaluation"}
			d.ReasonCodes = []string{"evaluation.unresolved_rule"}
			if err := d.ValidateAgainst(artifact); err == nil {
				t.Fatal("accepted old failure semantics under v0alpha2")
			}
		})
	}
}

func TestRequestRejectsLegacyVersion(t *testing.T) {
	artifact := reductionPolicy()
	r := DecisionRequest{APIVersion: "decision.antaeus.io/v0alpha1", Kind: KindRequest, Policy: PolicySelector{Inline: &artifact}, Input: map[string]json.RawMessage{}}
	if err := r.Validate(); err == nil {
		t.Fatal("accepted legacy DecisionRequest")
	}
	r.APIVersion = APIVersion
	if err := r.Validate(); err != nil {
		t.Fatal(err)
	}
}
