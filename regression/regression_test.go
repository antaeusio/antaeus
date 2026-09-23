package regression

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/antaeusio/antaeus/decision"
	"github.com/antaeusio/antaeus/evaluator/fixture"
	"github.com/antaeusio/antaeus/policy"
)

func TestRunQuickstartSuite(t *testing.T) {
	suite := loadQuickstartSuite(t)
	artifact, err := policy.LoadFile(contractPath("policy", "vendor-onboarding.yaml"))
	if err != nil {
		t.Fatalf("LoadFile(policy) error = %v", err)
	}
	set, err := fixture.LoadFile(contractPath("fixture-set", "quickstart.json"))
	if err != nil {
		t.Fatalf("LoadFile(fixture) error = %v", err)
	}

	result, err := Run(context.Background(), artifact, set, suite)
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if !result.Passed || len(result.Results) != 1 || result.Results[0].Status != StatusPassed {
		t.Fatalf("Run() result = %#v, want one passed case", result)
	}
	if result.Results[0].Decision.Outcome != decision.OutcomeReview {
		t.Fatalf("Decision.Outcome = %q, want review", result.Results[0].Decision.Outcome)
	}
	if result.Results[0].Decision.Evaluator == nil || result.Results[0].Decision.Evaluator.Synthetic == nil || !*result.Results[0].Decision.Evaluator.Synthetic {
		t.Fatalf("Decision.Evaluator = %#v, want synthetic fixture", result.Results[0].Decision.Evaluator)
	}
	if got := profileDigest(); got != "sha256:b10200864cb409eba5daa4b4dd790fc644719548067ae155a296d680a0317b5c" {
		t.Fatalf("profileDigest() = %q", got)
	}
}

func TestRunReportsExpectationMismatch(t *testing.T) {
	suite := loadQuickstartSuite(t)
	suite.Cases[0].Expect.Outcome = decision.OutcomeAllow
	artifact, err := policy.LoadFile(contractPath("policy", "vendor-onboarding.yaml"))
	if err != nil {
		t.Fatalf("LoadFile(policy) error = %v", err)
	}
	set, err := fixture.LoadFile(contractPath("fixture-set", "quickstart.json"))
	if err != nil {
		t.Fatalf("LoadFile(fixture) error = %v", err)
	}

	result, err := Run(context.Background(), artifact, set, suite)
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if result.Passed || result.Results[0].Status != StatusFailed {
		t.Fatalf("Run() result = %#v, want failed expectation", result)
	}
	if result.Results[0].Decision.Outcome != decision.OutcomeReview {
		t.Fatalf("actual outcome = %q, want review", result.Results[0].Decision.Outcome)
	}
}

func TestRunRejectsIdentityMismatches(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(*Suite)
		want   string
	}{
		{name: "policy", mutate: func(s *Suite) {
			s.Policy.Digest = "sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
		}, want: "policy identity"},
		{name: "fixture", mutate: func(s *Suite) { s.FixtureSet.Version = "v2" }, want: "fixture identity"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			suite := loadQuickstartSuite(t)
			test.mutate(&suite)
			artifact, err := policy.LoadFile(contractPath("policy", "vendor-onboarding.yaml"))
			if err != nil {
				t.Fatalf("LoadFile(policy) error = %v", err)
			}
			set, err := fixture.LoadFile(contractPath("fixture-set", "quickstart.json"))
			if err != nil {
				t.Fatalf("LoadFile(fixture) error = %v", err)
			}
			_, err = Run(context.Background(), artifact, set, suite)
			if err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("Run() error = %v, want %q", err, test.want)
			}
		})
	}
}

func TestParseRejectsStrictnessViolations(t *testing.T) {
	valid, err := os.ReadFile(contractPath("regression-suite", "quickstart.json"))
	if err != nil {
		t.Fatalf("ReadFile() error = %v", err)
	}
	tests := []struct {
		name   string
		source string
		want   string
	}{
		{name: "unknown property", source: strings.Replace(string(valid), `"kind": "RegressionSuite"`, `"kind": "RegressionSuite", "unknown": true`, 1), want: "unknown field"},
		{name: "duplicate property", source: strings.Replace(string(valid), `"kind": "RegressionSuite"`, `"kind": "RegressionSuite", "kind": "RegressionSuite"`, 1), want: "duplicate object key"},
		{name: "array input", source: strings.Replace(string(valid), "\"input\": {\n        \"description\": \"Processes aggregate product events.\",\n        \"serviceCategory\": \"analytics\"\n      }", `"input": []`, 1), want: "JSON object"},
		{name: "duplicate case", source: "", want: "duplicated"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			source := test.source
			if test.name == "duplicate case" {
				var suite Suite
				if err := json.Unmarshal(valid, &suite); err != nil {
					t.Fatalf("Unmarshal() error = %v", err)
				}
				suite.Cases = append(suite.Cases, suite.Cases[0])
				encoded, err := json.Marshal(suite)
				if err != nil {
					t.Fatalf("Marshal() error = %v", err)
				}
				source = string(encoded)
			}
			_, err := Parse([]byte(source))
			if err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("Parse() error = %v, want %q", err, test.want)
			}
		})
	}
}

func loadQuickstartSuite(t *testing.T) Suite {
	t.Helper()
	suite, err := LoadFile(contractPath("regression-suite", "quickstart.json"))
	if err != nil {
		t.Fatalf("LoadFile(suite) error = %v", err)
	}
	return suite
}

func contractPath(kind, name string) string {
	return filepath.Join("..", "contracts", "examples", "v0alpha1", kind, name)
}
