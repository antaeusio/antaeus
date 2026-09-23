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
	"github.com/antaeusio/antaeus/internal/fixtureprofile"
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
	if got := fixtureprofile.Digest(); got != "sha256:0956d00418fadb9d1dd95aed13f5e0499093d47ae9d2550ac1cb8da611c0545f" {
		t.Fatalf("fixtureprofile.Digest() = %q", got)
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

func TestRunRejectsOperationalErrors(t *testing.T) {
	artifact, err := policy.LoadFile(contractPath("policy", "vendor-onboarding.yaml"))
	if err != nil {
		t.Fatalf("LoadFile(policy) error = %v", err)
	}
	set, err := fixture.LoadFile(contractPath("fixture-set", "quickstart.json"))
	if err != nil {
		t.Fatalf("LoadFile(fixture) error = %v", err)
	}
	tests := []struct {
		name    string
		context func() context.Context
		mutate  func(*Suite)
		want    string
	}{
		{
			name:    "missing fixture case",
			context: context.Background,
			mutate:  func(s *Suite) { s.Cases[0].FixtureCase = "missing" },
			want:    "fixture.case_missing",
		},
		{
			name: "cancelled context",
			context: func() context.Context {
				ctx, cancel := context.WithCancel(context.Background())
				cancel()
				return ctx
			},
			mutate: func(*Suite) {},
			want:   "context canceled",
		},
		{
			name:    "invalid inline input",
			context: context.Background,
			mutate:  func(s *Suite) { s.Cases[0].Input = json.RawMessage(`[]`) },
			want:    "JSON object",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			suite := loadQuickstartSuite(t)
			test.mutate(&suite)
			_, err := Run(test.context(), artifact, set, suite)
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
		{name: "unknown property", source: strings.Replace(string(valid), `"kind": "RegressionSuite"`, `"kind": "RegressionSuite", "unknown": true`, 1), want: "unknown property"},
		{name: "duplicate property", source: strings.Replace(string(valid), `"kind": "RegressionSuite"`, `"kind": "RegressionSuite", "kind": "RegressionSuite"`, 1), want: "duplicate property"},
		{name: "case variant root", source: strings.Replace(string(valid), `"kind": "RegressionSuite"`, `"KIND": "RegressionSuite"`, 1), want: `unknown property "KIND"`},
		{name: "case variant case", source: strings.Replace(string(valid), `"fixtureCase": "aggregate-analytics"`, `"FixtureCase": "aggregate-analytics"`, 1), want: `unknown property "FixtureCase"`},
		{name: "case variant duplicate", source: strings.Replace(string(valid), `"input": {`, `"Input": {}, "input": {`, 1), want: `unknown property "Input"`},
		{name: "empty description", source: strings.Replace(string(valid), `"description": "A complete deterministic fixture check for the documented quickstart input."`, `"description": ""`, 1), want: "description"},
		{name: "whitespace description", source: strings.Replace(string(valid), `"description": "A complete deterministic fixture check for the documented quickstart input."`, `"description": "   "`, 1), want: "description"},
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

func TestParseAppliesStructuralLimitsToEachInput(t *testing.T) {
	suite := loadQuickstartSuite(t)
	suite.Cases[0].Input = json.RawMessage(`{"a":` + strings.Repeat("[", policy.MaxNestingDepth-1) + "null" + strings.Repeat("]", policy.MaxNestingDepth-1) + `}`)
	encoded, err := json.Marshal(suite)
	if err != nil {
		t.Fatalf("Marshal() error = %v", err)
	}
	if _, err := Parse(encoded); err != nil {
		t.Fatalf("Parse() exact per-input depth error = %v", err)
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
