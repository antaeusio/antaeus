package cli

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/antaeusio/antaeus/evaluator"
	"github.com/antaeusio/antaeus/evaluator/fixture"
	"github.com/antaeusio/antaeus/evaluator/localbinding"
	"github.com/antaeusio/antaeus/evaluator/localconfig"
	"github.com/antaeusio/antaeus/evaluator/profile"
	"github.com/antaeusio/antaeus/evaluator/runner"
	"github.com/antaeusio/antaeus/policy"
)

const evaluateProfileUsage = `Usage: antaeus evaluate-profile --policy <file> --input <file>
       --fixture-set <file> --case <name> [--profile <file>] [--bindings <file>]

Select a profile through explicit flags, the current project's .antaeus/config.json,
or the OS user-config directory's antaeus/config.json (in that order).
Only the deterministic fixture adapter is installed; output is synthetic evidence,
not semantic inference or enforcement. No network or credential lookup is performed.
The profile's fixtureSet and fixtureVersion must match the supplied fixture set.
Confidence routing is not supported by the installed fixture adapter.
`

func runEvaluateProfile(args []string, stdout, stderr io.Writer) int {
	// Help needs neither configuration discovery nor a usable home directory.
	if len(args) == 1 && (args[0] == "--help" || args[0] == "-h") {
		_, _ = io.WriteString(stdout, evaluateProfileUsage)
		return 0
	}
	projectDir, err := os.Getwd()
	if err != nil {
		return commandError(stderr, "evaluate-profile", errors.New("cannot locate current project directory"))
	}
	userDir, err := os.UserConfigDir()
	if err != nil {
		return commandError(stderr, "evaluate-profile", errors.New("cannot locate OS user-config directory"))
	}
	return runEvaluateProfileWith(args, stdout, stderr, configRuntime{
		projectDir, filepath.Join(userDir, "antaeus"), localbinding.EnvironmentFunc(os.LookupEnv),
	})
}

func runEvaluateProfileWith(args []string, stdout, stderr io.Writer, runtime configRuntime) int {
	flags := flag.NewFlagSet("evaluate-profile", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	var policyPath, inputPath, fixturePath, caseName, profilePath, bindingsPath string
	for _, f := range []struct {
		name  string
		value *string
	}{
		{"policy", &policyPath}, {"input", &inputPath}, {"fixture-set", &fixturePath},
		{"case", &caseName}, {"profile", &profilePath}, {"bindings", &bindingsPath},
	} {
		uniqueConfigFlag(flags, f.name, f.value)
	}
	if err := flags.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			_, _ = io.WriteString(stdout, evaluateProfileUsage)
			return 0
		}
		return usageError(stderr, "invalid or repeated evaluate-profile flag; run antaeus evaluate-profile --help")
	}
	if flags.NArg() != 0 {
		return usageError(stderr, "evaluate-profile does not accept positional arguments")
	}
	missing := requiredFlags([]string{"--policy", "--input", "--fixture-set", "--case"}, map[string]string{
		"--policy": policyPath, "--input": inputPath, "--fixture-set": fixturePath, "--case": caseName,
	})
	if missing != "" {
		return usageError(stderr, "evaluate-profile requires "+missing)
	}
	resolved, err := resolveEvaluationConfig(runtime, profilePath, bindingsPath)
	if err != nil {
		return profileExecutionConfigError(stderr, err)
	}
	p, err := resolved.Profile()
	if err != nil {
		return profileExecutionConfigError(stderr, err)
	}
	// This binary deliberately installs no remote adapters. Reject before
	// preflight, so even a trusted semantic profile cannot read credentials.
	for _, entry := range p.Spec.Evaluators {
		if entry.Mode != profile.ModeDeterministicFixture || entry.Adapter != (profile.ComponentIdentity{ID: fixture.AdapterID, Version: fixture.AdapterVersion}) {
			return commandError(stderr, "evaluate-profile", errFixtureOnly)
		}
	}
	artifact, err := policy.LoadFile(policyPath)
	if err != nil {
		return commandError(stderr, "evaluate-profile policy", err)
	}
	input, err := loadCanonicalInput(inputPath)
	if err != nil {
		return commandError(stderr, "evaluate-profile input", err)
	}
	set, err := fixture.LoadFile(fixturePath)
	if err != nil {
		return commandError(stderr, "evaluate-profile fixture set", err)
	}
	for _, entry := range p.Spec.Evaluators {
		if entry.Parameters["fixtureSet"] != set.Metadata.Name || entry.Parameters["fixtureVersion"] != set.Metadata.Version {
			return commandError(stderr, "evaluate-profile", errors.New("profile fixture identity does not match the supplied fixture set"))
		}
	}
	adapter, err := fixture.New(set, caseName)
	if err != nil {
		return commandError(stderr, "evaluate-profile fixture case", err)
	}
	registry := runner.Registry{
		{ID: fixture.AdapterID, Version: fixture.AdapterVersion}: {
			Mode:           profile.ModeDeterministicFixture,
			Protocol:       profile.ComponentIdentity{ID: "io.antaeus.rule-match", Version: "v0alpha1"},
			Capabilities:   []string{"json-input", "structured-rule-results"},
			FixtureVersion: set.Metadata.Version,
			Evaluate: func(ctx context.Context, request evaluator.Request, _ runner.Configuration) (evaluator.Result, error) {
				return adapter.Evaluate(ctx, request)
			},
		},
	}
	// Fixture profiles have no credential slots; preflight must not perform any
	// lookup. Keep the same ownership discipline as credential-bearing callers.
	credentials, err := resolved.Preflight(runtime.environment)
	if err != nil {
		return profileExecutionConfigError(stderr, err)
	}
	defer credentials.Clear()
	result, err := runner.Run(context.Background(), runner.Input{
		Policy: artifact, Profile: p, CanonicalInput: input,
		CorrelationID: "cli-fixture-" + caseName, Credentials: credentials,
		// This command installs and accepts only synthetic fixture adapters.
		// Revisit this opt-in before adding semantic execution to the command.
		AllowSyntheticFixtures: true,
	}, registry)
	if err != nil {
		return commandError(stderr, "evaluate-profile", err)
	}
	return writeJSON(stdout, stderr, "evaluate-profile", result)
}

var errFixtureOnly = errors.New("only the deterministic fixture adapter is installed; remote evaluation is unavailable")

func profileExecutionConfigError(stderr io.Writer, err error) int {
	// Valid fixture profiles cannot use credential slots. Never prompt users to
	// grant lasting trust or provision secrets for a profile we cannot execute.
	var trust *localconfig.TrustRequiredError
	var missing *localbinding.MissingCredentialError
	if errors.As(err, &trust) || errors.As(err, &missing) {
		err = errFixtureOnly
	}
	return commandError(stderr, "evaluate-profile", err)
}

// Reuse the configuration commands' strict loaders. This fixture-only command
// needs no credential authority: do not read or grant project trust. Remote
// adapters must be integrated with saved trust before they can be installed.
func resolveEvaluationConfig(runtime configRuntime, profilePath, bindingsPath string) (*localconfig.Resolved, error) {
	if err := independentUserConfig(runtime); err != nil {
		return nil, err
	}
	project, err := loadConfigManifest(filepath.Join(runtime.projectDir, ".antaeus", "config.json"), runtime.projectDir)
	if err != nil {
		return nil, fmt.Errorf("project configuration: %w", err)
	}
	user, err := loadConfigManifest(filepath.Join(runtime.userDir, "config.json"), "")
	if err != nil {
		return nil, fmt.Errorf("user configuration: %w", err)
	}
	cli, err := loadConfigLayer(profilePath, bindingsPath)
	if err != nil {
		return nil, fmt.Errorf("explicit configuration: %w", err)
	}
	return localconfig.Resolve(localconfig.Options{CLI: cli, Project: project, User: user})
}
