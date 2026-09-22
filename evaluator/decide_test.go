package evaluator_test

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/antaeusio/antaeus/decision"
	"github.com/antaeusio/antaeus/evaluator"
	"github.com/antaeusio/antaeus/evaluator/fixture"
	"github.com/antaeusio/antaeus/policy"
)

func TestDecideWithQuickstartFixture(t *testing.T) {
	artifact, set := loadContracts(t)
	adapter, err := fixture.New(set, "aggregate-analytics")
	if err != nil {
		t.Fatalf("fixture.New() error = %v", err)
	}

	got, err := evaluator.Decide(context.Background(), adapter, decisionInput(artifact))
	if err != nil {
		t.Fatalf("Decide() error = %v", err)
	}
	if got.Outcome != decision.OutcomeReview {
		t.Fatalf("Decision.Outcome = %q, want review", got.Outcome)
	}
	if got.Evaluator == nil || !got.Evaluator.Synthetic || got.Evaluator.Mode != decision.EvaluatorModeDeterministicFixture {
		t.Fatalf("Decision.Evaluator = %#v, want visibly synthetic fixture", got.Evaluator)
	}
	if got.Evaluator.FixtureSet != "quickstart" || got.Evaluator.FixtureVersion != "v1" {
		t.Fatalf("fixture identity = %q@%q, want quickstart@v1", got.Evaluator.FixtureSet, got.Evaluator.FixtureVersion)
	}
	if err := got.ValidateAgainst(artifact); err != nil {
		t.Fatalf("ValidateAgainst() error = %v", err)
	}
}

func TestDecideAttachesPolicyOutcomeAfterEvaluation(t *testing.T) {
	artifact, set := loadContracts(t)
	set.Cases[0].RuleResults[0].Status = decision.RuleMatched
	adapter, err := fixture.New(set, "aggregate-analytics")
	if err != nil {
		t.Fatalf("fixture.New() error = %v", err)
	}

	got, err := evaluator.Decide(context.Background(), adapter, decisionInput(artifact))
	if err != nil {
		t.Fatalf("Decide() error = %v", err)
	}
	if got.Outcome != decision.OutcomeDeny {
		t.Fatalf("Decision.Outcome = %q, want deny", got.Outcome)
	}
	if got.RuleResults[0].Outcome == nil || *got.RuleResults[0].Outcome != policy.OutcomeDeny {
		t.Fatalf("matched rule outcome = %v, want policy-authored deny", got.RuleResults[0].Outcome)
	}
	if got.RuleResults[1].Outcome != nil {
		t.Fatalf("non-matched rule outcome = %v, want nil", got.RuleResults[1].Outcome)
	}
}

func TestDecidePreservesUnresolvedEvidenceAsFailure(t *testing.T) {
	artifact, set := loadContracts(t)
	set.Cases[0].RuleResults[1].Status = decision.RuleIndeterminate
	adapter, err := fixture.New(set, "aggregate-analytics")
	if err != nil {
		t.Fatalf("fixture.New() error = %v", err)
	}

	got, err := evaluator.Decide(context.Background(), adapter, decisionInput(artifact))
	if err != nil {
		t.Fatalf("Decide() error = %v", err)
	}
	if got.Outcome != decision.OutcomeFailure || got.Failure == nil || got.Failure.Code != "evaluation.unresolved_rule" {
		t.Fatalf("Decision = %#v, want unresolved failure", got)
	}
}

func TestDecideRejectsMalformedAdapterResult(t *testing.T) {
	artifact, _ := loadContracts(t)
	adapter := evaluatorFunc(func(context.Context, evaluator.Request) (evaluator.Result, error) {
		return evaluator.Result{
			RuleResults: []evaluator.RuleResult{{
				RuleID:      "unknown",
				Status:      decision.RuleNotMatched,
				ReasonCodes: []string{"evaluation.test"},
			}},
			Metadata: evaluator.Metadata{
				AdapterID:      "io.antaeus.test",
				AdapterVersion: "0.1.0",
				Mode:           evaluator.ModeSemantic,
			},
		}, nil
	})
	if _, err := evaluator.Decide(context.Background(), adapter, decisionInput(artifact)); err == nil {
		t.Fatal("Decide() error = nil, want malformed adapter result rejection")
	}
}

type evaluatorFunc func(context.Context, evaluator.Request) (evaluator.Result, error)

func (f evaluatorFunc) Evaluate(ctx context.Context, request evaluator.Request) (evaluator.Result, error) {
	return f(ctx, request)
}

func decisionInput(artifact policy.Artifact) evaluator.DecisionInput {
	return evaluator.DecisionInput{
		Policy:         artifact,
		CanonicalInput: json.RawMessage(`{"description":"Processes aggregate product events.","serviceCategory":"analytics"}`),
		Deadline:       time.Now().Add(time.Minute),
		CorrelationID:  "decision-test",
		ProfileDigest:  "sha256:abcdef0123456789abcdef0123456789abcdef0123456789abcdef0123456789",
	}
}

func loadContracts(t *testing.T) (policy.Artifact, fixture.Set) {
	t.Helper()
	var artifact policy.Artifact
	policyData := readContract(t, "policy", "vendor-onboarding.json")
	if err := json.Unmarshal(policyData, &artifact); err != nil {
		t.Fatalf("decode policy: %v", err)
	}
	fixtureData := readContract(t, "fixture-set", "quickstart.json")
	set, err := fixture.Parse(fixtureData)
	if err != nil {
		t.Fatalf("fixture.Parse() error = %v", err)
	}
	return artifact, set
}

func readContract(t *testing.T, directory, name string) []byte {
	t.Helper()
	path := filepath.Join("..", "contracts", "examples", "v0alpha1", directory, name)
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	return data
}
