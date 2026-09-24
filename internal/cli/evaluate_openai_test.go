package cli

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/antaeusio/antaeus/adapters/openai"
	"github.com/antaeusio/antaeus/adapters/systemone"
	"github.com/antaeusio/antaeus/decision"
	"github.com/antaeusio/antaeus/evaluator"
	"github.com/antaeusio/antaeus/evaluator/localbinding"
	"github.com/antaeusio/antaeus/evaluator/runner"
)

const openAITestKey = "sk-cli-test-secret"

func openAIProfilePath() string {
	return filepath.Join("..", "..", "examples", "openai", "profile.json")
}

func openAIArgs() []string {
	return []string{
		"--policy", contractPath("policy", "vendor-onboarding.yaml"),
		"--input", contractPath("input", "aggregate-analytics.json"),
	}
}

// openAIRuntime installs the real adapter registration with a fake transport
// function, recording which credential and request each attempt received.
func openAIRuntime(t *testing.T, lookups *[]string, calls *int) configRuntime {
	t.Helper()
	adapter := openai.Registration()
	adapter.Evaluate = func(_ context.Context, request evaluator.Request, config runner.Configuration) (evaluator.Result, error) {
		*calls++
		if string(config.Credential) != openAITestKey || config.Evaluator.Adapter != openai.Identity {
			t.Errorf("unexpected configuration %+v", config.Evaluator)
		}
		results := make([]evaluator.RuleResult, len(request.Rules))
		for i, rule := range request.Rules {
			status := decision.RuleNotMatched
			if rule.ID == "complete-low-risk-submission" {
				status = decision.RuleMatched
			}
			results[i] = evaluator.RuleResult{RuleID: rule.ID, Status: status, ReasonCodes: []string{"openai." + string(status)}}
		}
		return evaluator.Result{RuleResults: results, Metadata: evaluator.Metadata{
			AdapterID: openai.AdapterID, AdapterVersion: openai.AdapterVersion, Mode: evaluator.ModeSemantic,
			Provider: openai.Provider, Model: "gpt-5.4-mini-2026-03-17", RequestID: "req_cli",
		}}, nil
	}
	return configRuntime{
		projectDir: t.TempDir(), userDir: t.TempDir(), openAI: &adapter,
		environment: localbinding.EnvironmentFunc(func(name string) (string, bool) {
			*lookups = append(*lookups, name)
			if name == openai.DefaultCredentialVariable {
				return openAITestKey, true
			}
			return "", false
		}),
	}
}

func TestEvaluateProfileOpenAIUsesAdapterDefaultCredential(t *testing.T) {
	var lookups []string
	calls := 0
	r := openAIRuntime(t, &lookups, &calls)
	output := evaluateProfileCommand(t, r, 0, append(openAIArgs(), "--profile", openAIProfilePath()))
	var d decision.Decision
	if err := json.Unmarshal([]byte(output), &d); err != nil {
		t.Fatal(err)
	}
	if d.Outcome != decision.OutcomeAllow || d.Evaluator == nil || *d.Evaluator.Synthetic || d.Evaluator.Mode != decision.EvaluatorMode(evaluator.ModeSemantic) ||
		d.Evaluator.Adapter != openai.AdapterID || d.Evaluator.Model != "gpt-5.4-mini-2026-03-17" || d.Evaluator.FixtureSet != nil {
		t.Fatalf("unexpected decision %s", output)
	}
	if calls != 1 || len(lookups) != 1 || lookups[0] != openai.DefaultCredentialVariable || strings.Contains(output, openAITestKey) {
		t.Fatalf("calls %d lookups %v", calls, lookups)
	}
}

func TestEvaluateProfileOpenAIMissingCredentialDoesNotCallProvider(t *testing.T) {
	var lookups []string
	calls := 0
	r := openAIRuntime(t, &lookups, &calls)
	r.environment = localbinding.EnvironmentFunc(func(name string) (string, bool) { lookups = append(lookups, name); return "", true })
	diagnostic := evaluateProfileCommand(t, r, 1, append(openAIArgs(), "--profile", openAIProfilePath()))
	if calls != 0 || !strings.Contains(diagnostic, `"openai-api-key"`) || strings.Contains(diagnostic, openai.DefaultCredentialVariable) {
		t.Fatalf("calls %d diagnostic %s", calls, diagnostic)
	}
}

func TestEvaluateProfileOpenAIRejectsFixtureFlags(t *testing.T) {
	var lookups []string
	calls := 0
	r := openAIRuntime(t, &lookups, &calls)
	args := append(openAIArgs(), "--profile", openAIProfilePath(), "--case", "aggregate-analytics")
	if diagnostic := evaluateProfileCommand(t, r, 64, args); !strings.Contains(diagnostic, "only to deterministic fixture profiles") || calls != 0 || len(lookups) != 0 {
		t.Fatal(diagnostic)
	}
	// Fixture profiles still require their fixture flags.
	if diagnostic := evaluateProfileCommand(t, r, 64, append(openAIArgs(), "--profile", contractPath("evaluator-profile", "quickstart-fixture.json"))); !strings.Contains(diagnostic, "--fixture-set") {
		t.Fatal(diagnostic)
	}
}

func TestEvaluateProfileOpenAIRejectsInvalidAdapterFieldsBeforeCredentials(t *testing.T) {
	var lookups []string
	calls := 0
	r := openAIRuntime(t, &lookups, &calls)
	data, err := os.ReadFile(openAIProfilePath())
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(r.projectDir, "drift.json")
	writeConfigTestFile(t, path, strings.Replace(string(data), openai.TemplateDigest, "sha256:"+strings.Repeat("0", 64), 1))
	diagnostic := evaluateProfileCommand(t, r, 1, append(openAIArgs(), "--profile", path))
	if !strings.Contains(diagnostic, "instructionTemplate.digest") || calls != 0 || len(lookups) != 0 {
		t.Fatalf("diagnostic %s lookups %v", diagnostic, lookups)
	}
}

func TestEvaluateProfileOpenAIProjectConfigurationRequiresSavedTrust(t *testing.T) {
	var lookups []string
	calls := 0
	r := openAIRuntime(t, &lookups, &calls)
	data, err := os.ReadFile(openAIProfilePath())
	if err != nil {
		t.Fatal(err)
	}
	writeConfigTestFile(t, filepath.Join(r.projectDir, "profile.json"), string(data))
	writeConfigTestFile(t, filepath.Join(r.projectDir, "bindings.json"), `{"apiVersion":"config.antaeus.io/v0alpha1","kind":"LocalSecretBindings","secretBindings":{"io.antaeus.openai":{"openai-api-key":{"source":"environment","name":"OPENAI_API_KEY"}}}}`)
	writeConfigTestFile(t, filepath.Join(r.projectDir, ".antaeus", "config.json"), `{"apiVersion":"config.antaeus.io/v0alpha1","kind":"LocalConfiguration","profileFile":"../profile.json","bindingsFile":"../bindings.json"}`)
	digest := projectTestDigest(t, r)
	diagnostic := evaluateProfileCommand(t, r, 1, openAIArgs())
	if !strings.Contains(diagnostic, "config trust --digest "+digest) || calls != 0 || len(lookups) != 0 {
		t.Fatalf("diagnostic %s lookups %v", diagnostic, lookups)
	}
	if _, err := os.Stat(filepath.Join(r.userDir, "trust")); !os.IsNotExist(err) {
		t.Fatalf("execution created trust storage: %v", err)
	}
	configCommand(t, r, 0, "trust", "--digest", digest)
	evaluateProfileCommand(t, r, 0, openAIArgs())
	if calls != 1 || len(lookups) != 1 {
		t.Fatalf("calls %d lookups %v", calls, lookups)
	}
	configCommand(t, r, 0, "revoke", "--digest", digest)
	evaluateProfileCommand(t, r, 1, openAIArgs())
	if calls != 1 {
		t.Fatal("revoked trust still invoked the provider")
	}
}

func TestConfigCheckUsesOpenAIAdapterDefault(t *testing.T) {
	var lookups []string
	calls := 0
	r := openAIRuntime(t, &lookups, &calls)
	output := configCommand(t, r, 0, "check", "--profile", openAIProfilePath())
	if !strings.Contains(output, `"source":"adapter-default"`) && !strings.Contains(output, `"source": "adapter-default"`) {
		t.Fatalf("unexpected output %s", output)
	}
	if strings.Contains(output, openai.DefaultCredentialVariable) || strings.Contains(output, openAITestKey) || len(lookups) != 1 {
		t.Fatalf("output %s lookups %v", output, lookups)
	}
}

func TestEvaluateProfileOpenAIProjectProfileWithAdapterDefaultRequiresTrust(t *testing.T) {
	var lookups []string
	calls := 0
	r := openAIRuntime(t, &lookups, &calls)
	data, err := os.ReadFile(openAIProfilePath())
	if err != nil {
		t.Fatal(err)
	}
	writeConfigTestFile(t, filepath.Join(r.projectDir, "profile.json"), string(data))
	writeConfigTestFile(t, filepath.Join(r.projectDir, ".antaeus", "config.json"), `{"apiVersion":"config.antaeus.io/v0alpha1","kind":"LocalConfiguration","profileFile":"../profile.json"}`)
	digest := projectTestDigest(t, r)
	if diagnostic := evaluateProfileCommand(t, r, 1, openAIArgs()); !strings.Contains(diagnostic, "config trust --digest "+digest) || calls != 0 || len(lookups) != 0 {
		t.Fatalf("diagnostic %s lookups %v", diagnostic, lookups)
	}
	configCommand(t, r, 0, "trust", "--digest", digest)
	evaluateProfileCommand(t, r, 0, openAIArgs())
	if calls != 1 || len(lookups) != 1 {
		t.Fatalf("calls %d lookups %v", calls, lookups)
	}
}

func TestEvaluateProfileInvalidProjectOpenAIProfileIsNotReportedAsUninstalled(t *testing.T) {
	var lookups []string
	calls := 0
	r := openAIRuntime(t, &lookups, &calls)
	data, err := os.ReadFile(openAIProfilePath())
	if err != nil {
		t.Fatal(err)
	}
	writeConfigTestFile(t, filepath.Join(r.projectDir, "profile.json"), strings.Replace(string(data), openai.TemplateDigest, "sha256:"+strings.Repeat("0", 64), 1))
	writeConfigTestFile(t, filepath.Join(r.projectDir, ".antaeus", "config.json"), `{"apiVersion":"config.antaeus.io/v0alpha1","kind":"LocalConfiguration","profileFile":"../profile.json"}`)
	diagnostic := evaluateProfileCommand(t, r, 1, openAIArgs())
	if !strings.Contains(diagnostic, "semantic profile is invalid") || strings.Contains(diagnostic, "config trust") || calls != 0 || len(lookups) != 0 {
		t.Fatalf("diagnostic %s", diagnostic)
	}
}

func TestEvaluateProfileRunsSystemOneWithOpenAIFallback(t *testing.T) {
	var lookups []string
	calls := 0
	r := openAIRuntime(t, &lookups, &calls)
	clm := systemone.Registration()
	clmCalls := 0
	clm.Evaluate = func(context.Context, evaluator.Request, runner.Configuration) (evaluator.Result, error) {
		clmCalls++
		return evaluator.Result{}, &evaluator.Error{Code: "evaluator.unavailable", Retryable: true}
	}
	r.systemOne = &clm
	output := evaluateProfileCommand(t, r, 0, append(openAIArgs(), "--profile", filepath.Join("..", "..", "examples", "clm", "openai-fallback.json")))
	var d decision.Decision
	if err := json.Unmarshal([]byte(output), &d); err != nil {
		t.Fatal(err)
	}
	if clmCalls != 1 || calls != 1 || !d.Evaluator.Fallback || d.Outcome != decision.OutcomeAllow || len(d.Evaluator.Route) != 2 {
		t.Fatalf("clm %d openai %d decision %s", clmCalls, calls, output)
	}
}

func TestEvaluateProfileSystemOneNeedsNoCredential(t *testing.T) {
	var lookups []string
	calls := 0
	r := openAIRuntime(t, &lookups, &calls)
	clm := systemone.Registration()
	clm.Evaluate = func(_ context.Context, request evaluator.Request, config runner.Configuration) (evaluator.Result, error) {
		if len(config.Credential) != 0 {
			t.Error("credential passed to a profile without a credential slot")
		}
		results := make([]evaluator.RuleResult, len(request.Rules))
		for i, rule := range request.Rules {
			confidence := 0.95
			results[i] = evaluator.RuleResult{RuleID: rule.ID, Status: decision.RuleNotMatched, Confidence: &confidence, ReasonCodes: []string{"systemone.not_matched"}}
		}
		return evaluator.Result{RuleResults: results, Metadata: evaluator.Metadata{AdapterID: systemone.AdapterID, AdapterVersion: systemone.AdapterVersion, Mode: evaluator.ModeSemantic, Provider: systemone.ProviderCLM, Model: "clm-latest"}}, nil
	}
	r.systemOne = &clm
	output := evaluateProfileCommand(t, r, 0, append(openAIArgs(), "--profile", filepath.Join("..", "..", "examples", "clm", "confidence-gated.json")))
	if len(lookups) != 0 || !strings.Contains(output, `"provider":"contrastive-lm"`) {
		t.Fatalf("lookups %v output %s", lookups, output)
	}
}

func TestEvaluateProfileProjectSystemOneWithoutCredentialsRequiresTrust(t *testing.T) {
	var lookups []string
	calls := 0
	r := openAIRuntime(t, &lookups, &calls)
	clm := systemone.Registration()
	clmCalls := 0
	clm.Evaluate = func(_ context.Context, request evaluator.Request, _ runner.Configuration) (evaluator.Result, error) {
		clmCalls++
		results := make([]evaluator.RuleResult, len(request.Rules))
		for i, rule := range request.Rules {
			confidence := 0.95
			results[i] = evaluator.RuleResult{RuleID: rule.ID, Status: decision.RuleNotMatched, Confidence: &confidence, ReasonCodes: []string{"systemone.not_matched"}}
		}
		return evaluator.Result{RuleResults: results, Metadata: evaluator.Metadata{AdapterID: systemone.AdapterID, AdapterVersion: systemone.AdapterVersion, Mode: evaluator.ModeSemantic, Provider: systemone.ProviderCLM, Model: "clm-latest"}}, nil
	}
	r.systemOne = &clm
	data, err := os.ReadFile(filepath.Join("..", "..", "examples", "clm", "confidence-gated.json"))
	if err != nil {
		t.Fatal(err)
	}
	// A cloned project points evaluation at an endpoint of its choosing.
	writeConfigTestFile(t, filepath.Join(r.projectDir, "profile.json"), strings.Replace(string(data), "http://127.0.0.1:8700", "https://collector.example.com", 1))
	writeConfigTestFile(t, filepath.Join(r.projectDir, ".antaeus", "config.json"), `{"apiVersion":"config.antaeus.io/v0alpha1","kind":"LocalConfiguration","profileFile":"../profile.json"}`)
	digest := projectTestDigest(t, r)
	diagnostic := evaluateProfileCommand(t, r, 1, openAIArgs())
	if !strings.Contains(diagnostic, "config trust --digest "+digest) || clmCalls != 0 || len(lookups) != 0 {
		t.Fatalf("diagnostic %s calls %d lookups %v", diagnostic, clmCalls, lookups)
	}
	configCommand(t, r, 0, "trust", "--digest", digest)
	evaluateProfileCommand(t, r, 0, openAIArgs())
	if clmCalls != 1 {
		t.Fatalf("trusted project did not evaluate: %d", clmCalls)
	}
	// Revoking trust blocks evaluation again.
	configCommand(t, r, 0, "revoke", "--digest", digest)
	evaluateProfileCommand(t, r, 1, openAIArgs())
	if clmCalls != 1 {
		t.Fatal("revoked trust still evaluated")
	}
}
