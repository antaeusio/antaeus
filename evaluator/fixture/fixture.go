// Package fixture implements the credential-free deterministic evaluator.
package fixture

import (
	"context"
	"fmt"
	"time"

	"github.com/antaeusio/antaeus/evaluator"
)

const (
	APIVersion     = "fixture.antaeus.io/v0alpha1"
	Kind           = "FixtureSet"
	AdapterID      = "io.antaeus.fixture"
	AdapterVersion = "0.1.0"
	MaxCases       = 256
)

// Set is one versioned collection of exact synthetic evaluator results.
type Set struct {
	APIVersion string   `json:"apiVersion"`
	Kind       string   `json:"kind"`
	Metadata   Metadata `json:"metadata"`
	Cases      []Case   `json:"cases"`
}

// Metadata identifies the fixture set independently from evaluator profiles.
type Metadata struct {
	Name    string `json:"name"`
	Version string `json:"version"`
}

// Case binds exact policy and input identities to normalized rule evidence.
type Case struct {
	Name         string                 `json:"name"`
	PolicyName   string                 `json:"policyName"`
	PolicyDigest string                 `json:"policyDigest"`
	InputDigest  string                 `json:"inputDigest"`
	RuleResults  []evaluator.RuleResult `json:"ruleResults"`
}

// Evaluator returns one selected case without network or credential access.
type Evaluator struct {
	setName    string
	setVersion string
	caseData   Case
}

// New validates a set and selects one named case.
func New(set Set, caseName string) (*Evaluator, error) {
	if err := set.Validate(); err != nil {
		return nil, err
	}
	for _, candidate := range set.Cases {
		if candidate.Name == caseName {
			return &Evaluator{
				setName:    set.Metadata.Name,
				setVersion: set.Metadata.Version,
				caseData:   cloneCase(candidate),
			}, nil
		}
	}
	return nil, &evaluator.Error{Code: "fixture.case_missing", Message: fmt.Sprintf("case %q is not defined", caseName)}
}

// Evaluate returns the selected exact synthetic result. It performs no network
// access, reads no credentials, and makes no inference from policy text.
func (e *Evaluator) Evaluate(ctx context.Context, request evaluator.Request) (evaluator.Result, error) {
	if err := ctx.Err(); err != nil {
		return evaluator.Result{}, &evaluator.Error{Code: "evaluation.cancelled", Retryable: false, Message: err.Error()}
	}
	if err := request.Validate(); err != nil {
		return evaluator.Result{}, &evaluator.Error{Code: "fixture.request_invalid", Retryable: false, Message: err.Error()}
	}
	if !request.Deadline.After(time.Now()) {
		return evaluator.Result{}, &evaluator.Error{Code: "evaluation.deadline_exceeded", Retryable: false, Message: "request deadline has elapsed"}
	}
	inputDigest, err := request.InputDigest()
	if err != nil {
		return evaluator.Result{}, &evaluator.Error{Code: "fixture.request_invalid", Retryable: false, Message: err.Error()}
	}
	if request.PolicyName != e.caseData.PolicyName || request.PolicyDigest != e.caseData.PolicyDigest || inputDigest != e.caseData.InputDigest {
		return evaluator.Result{}, &evaluator.Error{Code: "fixture.identity_mismatch", Retryable: false, Message: "policy or canonical input does not match the selected fixture case"}
	}

	result := evaluator.Result{
		RuleResults: cloneResults(e.caseData.RuleResults),
		Metadata: evaluator.Metadata{
			AdapterID:      AdapterID,
			AdapterVersion: AdapterVersion,
			Mode:           evaluator.ModeDeterministicFixture,
			Synthetic:      true,
			RequestID:      request.CorrelationID,
			FixtureSet:     e.setName,
			FixtureVersion: e.setVersion,
		},
	}
	if err := evaluator.ValidateResult(request, result); err != nil {
		return evaluator.Result{}, &evaluator.Error{Code: "fixture.mapping_invalid", Retryable: false, Message: err.Error()}
	}
	return result, nil
}

func cloneCase(source Case) Case {
	clone := source
	clone.RuleResults = cloneResults(source.RuleResults)
	return clone
}

func cloneResults(source []evaluator.RuleResult) []evaluator.RuleResult {
	clone := make([]evaluator.RuleResult, len(source))
	for i, result := range source {
		clone[i] = result
		clone[i].ReasonCodes = append([]string(nil), result.ReasonCodes...)
		if result.Confidence != nil {
			confidence := *result.Confidence
			clone[i].Confidence = &confidence
		}
	}
	return clone
}
