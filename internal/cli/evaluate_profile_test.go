package cli

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/antaeusio/antaeus/decision"
	"github.com/antaeusio/antaeus/evaluator/localbinding"
	"github.com/antaeusio/antaeus/evaluator/profile"
	"github.com/antaeusio/antaeus/evaluator/runner"
)

func profileEvaluationArgs() []string {
	return []string{
		"--policy", contractPath("policy", "vendor-onboarding.yaml"),
		"--input", contractPath("input", "aggregate-analytics.json"),
		"--fixture-set", contractPath("fixture-set", "quickstart.json"),
		"--case", "aggregate-analytics",
	}
}

func fixtureProfileRuntime(t *testing.T) configRuntime {
	t.Helper()
	r := configRuntime{projectDir: t.TempDir(), userDir: t.TempDir()}
	r.environment = localbinding.EnvironmentFunc(func(string) (string, bool) {
		t.Fatal("fixture execution or rejected profile must not look up credentials")
		return "", false
	})
	return r
}

func evaluateProfileCommand(t *testing.T, r configRuntime, want int, args []string) string {
	t.Helper()
	var out, diagnostic bytes.Buffer
	code := runEvaluateProfileWith(args, &out, &diagnostic, r)
	if code != want {
		t.Fatalf("exit %d, want %d; output %s; diagnostic %s", code, want, out.String(), diagnostic.String())
	}
	if code != 0 {
		if out.Len() != 0 {
			t.Fatalf("failure wrote stdout: %s", out.String())
		}
		return diagnostic.String()
	}
	if diagnostic.Len() != 0 {
		t.Fatal(diagnostic.String())
	}
	return out.String()
}

func TestEvaluateProfileExplicitFixture(t *testing.T) {
	r := fixtureProfileRuntime(t)
	path := contractPath("evaluator-profile", "quickstart-fixture.json")
	output := evaluateProfileCommand(t, r, 0, append(profileEvaluationArgs(), "--profile", path))
	var d decision.Decision
	if err := json.Unmarshal([]byte(output), &d); err != nil {
		t.Fatal(err)
	}
	p, err := profile.LoadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	digest, err := p.Digest()
	if err != nil {
		t.Fatal(err)
	}
	if d.Outcome != decision.OutcomeReview || d.Evaluator == nil || d.Evaluator.ProfileDigest != digest ||
		d.Evaluator.Synthetic == nil || !*d.Evaluator.Synthetic || d.Evaluator.Attempts != 1 ||
		d.Evaluator.FixtureSet == nil || *d.Evaluator.FixtureSet != "quickstart" ||
		d.Evaluator.FixtureVersion == nil || *d.Evaluator.FixtureVersion != "v1" {
		t.Fatalf("unexpected decision: %s", output)
	}
	var trace runner.Trace
	if err := json.Unmarshal(d.Extensions[runner.TraceExtension], &trace); err != nil {
		t.Fatal(err)
	}
	if len(trace.Attempts) != 1 || trace.Attempts[0].EvaluatorID != "fixture-primary" || trace.Attempts[0].Route != "primary" {
		t.Fatalf("unexpected trace: %+v", trace)
	}
}

func TestEvaluateProfileSelectionAndNoImplicitDefault(t *testing.T) {
	r := fixtureProfileRuntime(t)
	args := profileEvaluationArgs()
	if diagnostic := evaluateProfileCommand(t, r, 1, args); !strings.Contains(diagnostic, "no evaluator profile selected") {
		t.Fatal(diagnostic)
	}
	// A user fixture profile is selected with no project configuration.
	copyConfigFixture(t, "evaluator-profile/quickstart-fixture.json", filepath.Join(r.userDir, "profile.json"))
	writeConfigTestFile(t, filepath.Join(r.userDir, "config.json"), `{"apiVersion":"config.antaeus.io/v0alpha1","kind":"LocalConfiguration","profileFile":"profile.json"}`)
	evaluateProfileCommand(t, r, 0, args)
	// Project selection overrides the user profile, without needing trust for
	// credential-free fixture execution. Its distinct digest reaches the output.
	data, err := os.ReadFile(filepath.Join(r.userDir, "profile.json"))
	if err != nil {
		t.Fatal(err)
	}
	projectProfile := filepath.Join(r.projectDir, "profile.json")
	writeConfigTestFile(t, projectProfile, strings.Replace(string(data), "quickstart-fixture", "project-fixture", 1))
	writeConfigTestFile(t, filepath.Join(r.projectDir, ".antaeus", "config.json"), `{"apiVersion":"config.antaeus.io/v0alpha1","kind":"LocalConfiguration","profileFile":"../profile.json"}`)
	p, err := profile.LoadFile(projectProfile)
	if err != nil {
		t.Fatal(err)
	}
	digest, err := p.Digest()
	if err != nil {
		t.Fatal(err)
	}
	if output := evaluateProfileCommand(t, r, 0, args); !strings.Contains(output, digest) {
		t.Fatal(output)
	}
	if output := evaluateProfileCommand(t, r, 0, append(args, "--profile", contractPath("evaluator-profile", "quickstart-fixture.json"))); strings.Contains(output, digest) {
		t.Fatal(output)
	}
	// Malformed lower-priority configuration still fails closed.
	writeConfigTestFile(t, filepath.Join(r.userDir, "config.json"), `{"rawSecret":"NEVER_ECHO_THIS"}`)
	diagnostic := evaluateProfileCommand(t, r, 1, append(args, "--profile", projectProfile))
	if strings.Contains(diagnostic, "NEVER_ECHO_THIS") || !strings.Contains(diagnostic, "manifest") {
		t.Fatal(diagnostic)
	}
}

func TestEvaluateProfileRejectsRemoteWithoutReadingCredentials(t *testing.T) {
	r := configFixture(t) // environment lookup fails the test
	args := profileEvaluationArgs()
	digest := projectTestDigest(t, r)
	if diagnostic := evaluateProfileCommand(t, r, 1, args); !strings.Contains(diagnostic, "config trust --digest "+digest) {
		t.Fatal(diagnostic)
	}
	configCommand(t, r, 0, "trust", "--digest", digest)
	if diagnostic := evaluateProfileCommand(t, r, 1, args); !strings.Contains(diagnostic, "only the deterministic fixture adapter is installed") {
		t.Fatal(diagnostic)
	}
	// Explicit semantic selections cannot bypass the installed-adapter boundary.
	evaluateProfileCommand(t, r, 1, append(args, "--profile", filepath.Join(r.projectDir, "profile.json"), "--bindings", filepath.Join(r.projectDir, "bindings.json")))
	// An explicit fixture remains credential-free even when unused project
	// bindings point at process variables and the project trust is revoked.
	configCommand(t, r, 0, "revoke", "--digest", digest)
	evaluateProfileCommand(t, r, 0, append(args, "--profile", contractPath("evaluator-profile", "quickstart-fixture.json")))
}

func TestEvaluateProfileRejectsUnsupportedFixtureConfiguration(t *testing.T) {
	source, err := os.ReadFile(contractPath("evaluator-profile", "quickstart-fixture.json"))
	if err != nil {
		t.Fatal(err)
	}
	for _, tt := range []struct{ name, old, replacement, diagnostic string }{
		{"set", `"fixtureSet": "quickstart"`, `"fixtureSet": "another"`, "fixture identity does not match"},
		{"version", `"fixtureVersion": "v1"`, `"fixtureVersion": "v2"`, "fixture identity does not match"},
		{"protocol", `"io.antaeus.rule-match"`, `"io.example.other"`, "protocol is unsupported"},
		{"capability", `"structured-rule-results"`, `"structured-rule-results", "confidence-scores"`, "lacks a required capability"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			r := fixtureProfileRuntime(t)
			path := filepath.Join(r.projectDir, "profile.json")
			writeConfigTestFile(t, path, strings.Replace(string(source), tt.old, tt.replacement, 1))
			diagnostic := evaluateProfileCommand(t, r, 1, append(profileEvaluationArgs(), "--profile", path))
			if !strings.Contains(diagnostic, tt.diagnostic) {
				t.Fatal(diagnostic)
			}
		})
	}
}

func TestEvaluateProfileAdapterFailureIsDecision(t *testing.T) {
	r := fixtureProfileRuntime(t)
	input := filepath.Join(r.projectDir, "input.json")
	writeConfigTestFile(t, input, `{"different":"NEVER_INCLUDE_RAW_INPUT"}`)
	args := profileEvaluationArgs()
	args[3] = input
	output := evaluateProfileCommand(t, r, 0, append(args, "--profile", contractPath("evaluator-profile", "quickstart-fixture.json")))
	var d decision.Decision
	if err := json.Unmarshal([]byte(output), &d); err != nil {
		t.Fatal(err)
	}
	if d.Outcome != decision.OutcomeFailure || d.Failure == nil || strings.Contains(output, "NEVER_INCLUDE_RAW_INPUT") {
		t.Fatal(output)
	}
}

func TestEvaluateProfileUsageDoesNotEchoValues(t *testing.T) {
	r := fixtureProfileRuntime(t)
	for _, args := range [][]string{
		nil, {"--profile", "SECRET", "--profile", "SECRET"}, {"--api-key", "SECRET"},
		{"--policy=SECRET", "--input="}, {"SECRET"},
	} {
		if diagnostic := evaluateProfileCommand(t, r, 64, args); strings.Contains(diagnostic, "SECRET") {
			t.Fatal(diagnostic)
		}
	}
	var out, diagnostic bytes.Buffer
	if code := Run([]string{"evaluate-profile", "--help"}, &out, &diagnostic); code != 0 || !strings.Contains(out.String(), "synthetic evidence") || diagnostic.Len() != 0 {
		t.Fatalf("help: exit %d output %s error %s", code, out.String(), diagnostic.String())
	}
}
