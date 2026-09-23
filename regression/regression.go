// Package regression defines and runs portable deterministic regression suites.
package regression

import (
	"encoding/json"

	"github.com/antaeusio/antaeus/decision"
)

const (
	APIVersion     = "regression.antaeus.io/v0alpha1"
	KindSuite      = "RegressionSuite"
	KindResultSet  = "RegressionResultSet"
	MaxCases       = 256
	MaxSourceBytes = 1 << 20
	MaxDescription = 4096
)

// Suite binds named inputs and expectations to exact policy and fixture identities.
type Suite struct {
	APIVersion string         `json:"apiVersion"`
	Kind       string         `json:"kind"`
	Metadata   Identity       `json:"metadata"`
	Policy     PolicyIdentity `json:"policy"`
	FixtureSet Identity       `json:"fixtureSet"`
	Cases      []Case         `json:"cases"`
}

// Identity names one immutable version of a suite or fixture set.
type Identity struct {
	Name    string `json:"name"`
	Version string `json:"version"`
}

// PolicyIdentity binds a suite to canonical policy content.
type PolicyIdentity struct {
	Name   string `json:"name"`
	Digest string `json:"digest"`
}

// Case is one named canonical input and exact terminal expectation.
type Case struct {
	Name        string          `json:"name"`
	Description string          `json:"description,omitempty"`
	FixtureCase string          `json:"fixtureCase"`
	Input       json.RawMessage `json:"input"`
	Expect      Expectation     `json:"expect"`
}

// Expectation is the exact portable result compared by the runner.
type Expectation struct {
	Outcome     decision.Outcome `json:"outcome"`
	ReasonCodes []string         `json:"reasonCodes"`
}

// ResultSet contains the complete, machine-readable result of one suite run.
type ResultSet struct {
	APIVersion string                  `json:"apiVersion"`
	Kind       string                  `json:"kind"`
	Suite      Identity                `json:"suite"`
	Policy     decision.PolicyIdentity `json:"policy"`
	FixtureSet Identity                `json:"fixtureSet"`
	Passed     bool                    `json:"passed"`
	Results    []CaseResult            `json:"results"`
}

// Status identifies whether one Decision matched its exact expectation.
type Status string

const (
	StatusPassed Status = "passed"
	StatusFailed Status = "failed"
)

// CaseResult records one expected result and the complete actual Decision.
type CaseResult struct {
	Name        string            `json:"name"`
	FixtureCase string            `json:"fixtureCase"`
	Status      Status            `json:"status"`
	Expected    Expectation       `json:"expected"`
	Decision    decision.Decision `json:"decision"`
}
