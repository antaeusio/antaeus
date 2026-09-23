package regression

import (
	"context"
	"fmt"
	"time"

	"github.com/antaeusio/antaeus/decision"
	"github.com/antaeusio/antaeus/evaluator"
	"github.com/antaeusio/antaeus/evaluator/fixture"
	"github.com/antaeusio/antaeus/internal/fixtureprofile"
	"github.com/antaeusio/antaeus/internal/jsonvalue"
	"github.com/antaeusio/antaeus/policy"
)

const caseTimeout = 30 * time.Second

// Run executes every suite case through the deterministic fixture evaluator.
// Configuration and evaluator errors abort the run; expectation mismatches are
// represented by failed case results and a false ResultSet.Passed value.
func Run(ctx context.Context, artifact policy.Artifact, set fixture.Set, suite Suite) (ResultSet, error) {
	if err := suite.Validate(); err != nil {
		return ResultSet{}, fmt.Errorf("validate regression suite: %w", err)
	}
	if err := artifact.Validate(); err != nil {
		return ResultSet{}, fmt.Errorf("validate policy: %w", err)
	}
	policyDigest, err := artifact.Digest()
	if err != nil {
		return ResultSet{}, fmt.Errorf("digest policy: %w", err)
	}
	if artifact.Metadata.Name != suite.Policy.Name || policyDigest != suite.Policy.Digest {
		return ResultSet{}, fmt.Errorf("regression policy identity does not match loaded policy")
	}
	if err := set.Validate(); err != nil {
		return ResultSet{}, fmt.Errorf("validate fixture set: %w", err)
	}
	if set.Metadata.Name != suite.FixtureSet.Name || set.Metadata.Version != suite.FixtureSet.Version {
		return ResultSet{}, fmt.Errorf("regression fixture identity does not match loaded fixture set")
	}

	result := ResultSet{
		APIVersion: APIVersion,
		Kind:       KindResultSet,
		Suite:      suite.Metadata,
		Policy:     decision.PolicyIdentity{Name: artifact.Metadata.Name, Digest: policyDigest},
		FixtureSet: suite.FixtureSet,
		Passed:     true,
		Results:    make([]CaseResult, 0, len(suite.Cases)),
	}
	for index, testCase := range suite.Cases {
		if err := ctx.Err(); err != nil {
			return ResultSet{}, fmt.Errorf("run regression suite: %w", err)
		}
		input, err := jsonvalue.CanonicalObject(testCase.Input)
		if err != nil {
			return ResultSet{}, fmt.Errorf("canonicalize case %q input: %w", testCase.Name, err)
		}
		adapter, err := fixture.New(set, testCase.FixtureCase)
		if err != nil {
			return ResultSet{}, fmt.Errorf("select fixture for case %q: %w", testCase.Name, err)
		}
		decisionResult, err := evaluator.Decide(ctx, adapter, evaluator.DecisionInput{
			Policy:         artifact,
			CanonicalInput: input,
			Deadline:       time.Now().Add(caseTimeout),
			CorrelationID:  fmt.Sprintf("regression-%d", index+1),
			ProfileDigest:  fixtureprofile.Digest(),
			ProfileVersion: fixtureprofile.Version,
		})
		if err != nil {
			return ResultSet{}, fmt.Errorf("evaluate case %q: %w", testCase.Name, err)
		}
		status := StatusPassed
		if !expectationMatches(testCase.Expect, decisionResult) {
			status = StatusFailed
			result.Passed = false
		}
		result.Results = append(result.Results, CaseResult{
			Name:        testCase.Name,
			FixtureCase: testCase.FixtureCase,
			Status:      status,
			Expected: Expectation{
				Outcome:     testCase.Expect.Outcome,
				ReasonCodes: append([]string(nil), testCase.Expect.ReasonCodes...),
			},
			Decision: decisionResult,
		})
	}
	return result, nil
}
