package cli

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/antaeusio/antaeus/decision"
	"github.com/antaeusio/antaeus/internal/fixtureprofile"
)

func TestRun(t *testing.T) {
	tests := []struct {
		name       string
		args       []string
		wantCode   int
		wantStdout string
		wantStderr string
	}{
		{
			name:       "validate requires policy",
			args:       []string{"validate"},
			wantCode:   64,
			wantStderr: "validate requires exactly one policy file",
		},
		{
			name:       "evaluate requires flags",
			args:       []string{"evaluate"},
			wantCode:   64,
			wantStderr: "evaluate requires --policy, --input, --fixture-set, --case",
		},
		{
			name:       "test requires flags",
			args:       []string{"test"},
			wantCode:   64,
			wantStderr: "test requires --policy, --suite, --fixture-set",
		},
		{
			name:       "evaluate rejects unknown flag",
			args:       []string{"evaluate", "--unknown"},
			wantCode:   64,
			wantStderr: "flag provided but not defined",
		},
		{
			name:       "evaluate rejects positional argument",
			args:       []string{"evaluate", "extra"},
			wantCode:   64,
			wantStderr: "does not accept positional arguments",
		},
		{
			name:       "no arguments shows help",
			wantCode:   0,
			wantStdout: "Usage: antaeus <command>",
		},
		{
			name:       "help command",
			args:       []string{"help"},
			wantCode:   0,
			wantStdout: "Commands:",
		},
		{
			name:       "version command",
			args:       []string{"version"},
			wantCode:   0,
			wantStdout: "antaeus dev",
		},
		{
			name:       "version flag",
			args:       []string{"--version"},
			wantCode:   0,
			wantStdout: "antaeus dev",
		},
		{
			name:       "unknown command",
			args:       []string{"unknown"},
			wantCode:   64,
			wantStderr: `unknown command "unknown"`,
		},
		{
			name:       "unexpected argument",
			args:       []string{"version", "extra"},
			wantCode:   64,
			wantStderr: "version does not accept arguments",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			var stdout bytes.Buffer
			var stderr bytes.Buffer

			if got := Run(test.args, &stdout, &stderr); got != test.wantCode {
				t.Fatalf("Run() = %d, want %d", got, test.wantCode)
			}
			if !strings.Contains(stdout.String(), test.wantStdout) {
				t.Errorf("stdout = %q, want substring %q", stdout.String(), test.wantStdout)
			}
			if !strings.Contains(stderr.String(), test.wantStderr) {
				t.Errorf("stderr = %q, want substring %q", stderr.String(), test.wantStderr)
			}
		})
	}
}

func TestLocalFixtureProfileDigestHasDocumentedPreimage(t *testing.T) {
	const want = "sha256:0956d00418fadb9d1dd95aed13f5e0499093d47ae9d2550ac1cb8da611c0545f"
	if got := fixtureprofile.Digest(); got != want {
		t.Fatalf("fixtureprofile.Digest() = %q, want %q", got, want)
	}
}

func TestRunValidateExample(t *testing.T) {
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	policyPath := contractPath("policy", "vendor-onboarding.yaml")
	if got := Run([]string{"validate", policyPath}, &stdout, &stderr); got != 0 {
		t.Fatalf("Run() = %d, stderr = %q", got, stderr.String())
	}
	const want = `{"name":"vendor-onboarding","digest":"sha256:2378b5a1806bb11c618bd3a78122e773ec93bdfc45b751a18887d40f4ea536a2","rules":2}` + "\n"
	if stdout.String() != want {
		t.Fatalf("stdout = %q, want %q", stdout.String(), want)
	}
}

func TestRunEvaluateQuickstartFixture(t *testing.T) {
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	args := []string{
		"evaluate",
		"--policy", contractPath("policy", "vendor-onboarding.yaml"),
		"--input", contractPath("input", "aggregate-analytics.json"),
		"--fixture-set", contractPath("fixture-set", "quickstart.json"),
		"--case", "aggregate-analytics",
	}
	if got := Run(args, &stdout, &stderr); got != 0 {
		t.Fatalf("Run() = %d, stderr = %q", got, stderr.String())
	}
	var got decision.Decision
	if err := json.Unmarshal(stdout.Bytes(), &got); err != nil {
		t.Fatalf("decode stdout: %v", err)
	}
	if got.Outcome != decision.OutcomeReview {
		t.Fatalf("Decision.Outcome = %q, want review", got.Outcome)
	}
	if got.Evaluator == nil || got.Evaluator.Synthetic == nil || !*got.Evaluator.Synthetic || got.Evaluator.FixtureSet == nil || *got.Evaluator.FixtureSet != "quickstart" {
		t.Fatalf("Decision.Evaluator = %#v, want quickstart synthetic fixture", got.Evaluator)
	}
}

func TestRunQuickstartRegressionSuite(t *testing.T) {
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	args := []string{
		"test",
		"--policy", contractPath("policy", "vendor-onboarding.yaml"),
		"--suite", contractPath("regression-suite", "quickstart.json"),
		"--fixture-set", contractPath("fixture-set", "quickstart.json"),
	}
	if got := Run(args, &stdout, &stderr); got != 0 {
		t.Fatalf("Run() = %d, stderr = %q", got, stderr.String())
	}
	var got struct {
		Passed  bool `json:"passed"`
		Results []struct {
			Status string `json:"status"`
		} `json:"results"`
	}
	if err := json.Unmarshal(stdout.Bytes(), &got); err != nil {
		t.Fatalf("decode stdout: %v", err)
	}
	if !got.Passed || len(got.Results) != 1 || got.Results[0].Status != "passed" {
		t.Fatalf("result = %#v, want passed suite", got)
	}
}

func TestRunRegressionMismatchReturnsResultAndFailure(t *testing.T) {
	source, err := os.ReadFile(contractPath("regression-suite", "quickstart.json"))
	if err != nil {
		t.Fatalf("ReadFile() error = %v", err)
	}
	suitePath := writeCLIFile(t, "mismatch.json", strings.Replace(string(source), `"outcome": "review"`, `"outcome": "allow"`, 1))
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	args := []string{
		"test",
		"--policy", contractPath("policy", "vendor-onboarding.yaml"),
		"--suite", suitePath,
		"--fixture-set", contractPath("fixture-set", "quickstart.json"),
	}
	if got := Run(args, &stdout, &stderr); got != 2 {
		t.Fatalf("Run() = %d, want 2", got)
	}
	if stderr.Len() != 0 {
		t.Fatalf("stderr = %q, want empty", stderr.String())
	}
	if !strings.Contains(stdout.String(), `"passed":false`) || !strings.Contains(stdout.String(), `"status":"failed"`) {
		t.Fatalf("stdout = %q, want failed machine-readable result", stdout.String())
	}
}

func TestRunRegressionOperationalErrorReturnsNoResult(t *testing.T) {
	source, err := os.ReadFile(contractPath("regression-suite", "quickstart.json"))
	if err != nil {
		t.Fatalf("ReadFile() error = %v", err)
	}
	suitePath := writeCLIFile(t, "missing-fixture.json", strings.Replace(string(source), `"fixtureCase": "aggregate-analytics"`, `"fixtureCase": "missing"`, 1))
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	args := []string{
		"test",
		"--policy", contractPath("policy", "vendor-onboarding.yaml"),
		"--suite", suitePath,
		"--fixture-set", contractPath("fixture-set", "quickstart.json"),
	}
	if got := Run(args, &stdout, &stderr); got != 1 {
		t.Fatalf("Run() = %d, want 1", got)
	}
	if stdout.Len() != 0 {
		t.Fatalf("stdout = %q, want empty", stdout.String())
	}
	if !strings.Contains(stderr.String(), "fixture.case_missing") {
		t.Fatalf("stderr = %q, want fixture.case_missing", stderr.String())
	}
}

func TestRunEvaluateRejectsFixtureIdentityMismatch(t *testing.T) {
	directory := t.TempDir()
	inputPath := filepath.Join(directory, "input.json")
	if err := os.WriteFile(inputPath, []byte(`{"different":true}`), 0o600); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	args := []string{
		"evaluate",
		"--policy", contractPath("policy", "vendor-onboarding.yaml"),
		"--input", inputPath,
		"--fixture-set", contractPath("fixture-set", "quickstart.json"),
		"--case", "aggregate-analytics",
	}
	if got := Run(args, &stdout, &stderr); got != 1 {
		t.Fatalf("Run() = %d, want 1", got)
	}
	if !strings.Contains(stderr.String(), "fixture.identity_mismatch") {
		t.Fatalf("stderr = %q, want fixture identity mismatch", stderr.String())
	}
	if stdout.Len() != 0 {
		t.Fatalf("stdout = %q, want empty", stdout.String())
	}
}

func TestRunEvaluateConfigurationFailures(t *testing.T) {
	tests := []struct {
		name       string
		policyPath string
		caseName   string
		want       string
	}{
		{
			name:       "unknown case",
			policyPath: contractPath("policy", "vendor-onboarding.yaml"),
			caseName:   "missing",
			want:       "fixture.case_missing",
		},
		{
			name:       "invalid policy",
			policyPath: writeCLIFile(t, "invalid.yaml", "kind: Policy\n"),
			caseName:   "aggregate-analytics",
			want:       "apiVersion",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			var stdout bytes.Buffer
			var stderr bytes.Buffer
			args := []string{
				"evaluate",
				"--policy", test.policyPath,
				"--input", contractPath("input", "aggregate-analytics.json"),
				"--fixture-set", contractPath("fixture-set", "quickstart.json"),
				"--case", test.caseName,
			}
			if got := Run(args, &stdout, &stderr); got != 1 {
				t.Fatalf("Run() = %d, want 1", got)
			}
			if stdout.Len() != 0 {
				t.Fatalf("stdout = %q, want empty", stdout.String())
			}
			if !strings.Contains(stderr.String(), test.want) {
				t.Fatalf("stderr = %q, want substring %q", stderr.String(), test.want)
			}
		})
	}
}

func writeCLIFile(t *testing.T, name, content string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), name)
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}
	return path
}

func contractPath(kind, name string) string {
	return filepath.Join("..", "..", "contracts", "examples", "v0alpha1", kind, name)
}
