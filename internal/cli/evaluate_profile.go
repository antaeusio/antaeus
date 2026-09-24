package cli

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/antaeusio/antaeus/adapters/openai"
	"github.com/antaeusio/antaeus/evaluator"
	"github.com/antaeusio/antaeus/evaluator/fixture"
	"github.com/antaeusio/antaeus/evaluator/localbinding"
	"github.com/antaeusio/antaeus/evaluator/localconfig"
	"github.com/antaeusio/antaeus/evaluator/profile"
	"github.com/antaeusio/antaeus/evaluator/runner"
	"github.com/antaeusio/antaeus/policy"
)

const evaluateProfileUsage = `Usage: antaeus evaluate-profile --policy <file> --input <file>
       [--profile <file>] [--bindings <file>]
       [--fixture-set <file> --case <name>]

Select a profile through explicit flags, the current project's .antaeus/config.json,
or the OS user-config directory's antaeus/config.json (in that order).

Installed adapters:
  io.antaeus.fixture@0.1.0  deterministic synthetic fixture; requires --fixture-set
                            and --case. Output is synthetic test evidence, never
                            semantic inference. No network or credential access.
  io.antaeus.openai@0.1.0   experimental OpenAI Responses semantic evaluator. Sends
                            the policy conditions and input to OpenAI. Reads the
                            credential bound to slot openai-api-key from the process
                            environment (see docs/openai-adapter.md). Project-supplied
                            configuration that reads credentials requires
                            antaeus config trust first.

A profile must use only one of these adapters. Confidence routing is not supported
by either installed adapter.
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
		projectDir: projectDir, userDir: filepath.Join(userDir, "antaeus"), environment: localbinding.EnvironmentFunc(os.LookupEnv),
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
	missing := requiredFlags([]string{"--policy", "--input"}, map[string]string{"--policy": policyPath, "--input": inputPath})
	if missing != "" {
		return usageError(stderr, "evaluate-profile requires "+missing)
	}
	resolved, p, kind, err := resolveExecutionProfile(runtime, profilePath, bindingsPath)
	if err != nil {
		return commandError(stderr, "evaluate-profile", err)
	}
	fixtureFlags := fixturePath != "" || caseName != ""
	if kind == fixtureProfile {
		if missing := requiredFlags([]string{"--fixture-set", "--case"}, map[string]string{"--fixture-set": fixturePath, "--case": caseName}); missing != "" {
			return usageError(stderr, "fixture profiles require "+missing)
		}
	} else if fixtureFlags {
		return usageError(stderr, "--fixture-set and --case apply only to deterministic fixture profiles")
	}
	artifact, err := policy.LoadFile(policyPath)
	if err != nil {
		return commandError(stderr, "evaluate-profile policy", err)
	}
	input, err := loadCanonicalInput(inputPath)
	if err != nil {
		return commandError(stderr, "evaluate-profile input", err)
	}
	var registry runner.Registry
	var correlationID string
	if kind == fixtureProfile {
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
		registry = runner.Registry{
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
		correlationID = "cli-fixture-" + caseName
	} else {
		registry = runner.Registry{openai.Identity: runtime.openAIAdapter()}
		correlationID, err = randomCorrelationID()
		if err != nil {
			return commandError(stderr, "evaluate-profile", err)
		}
	}
	// Credentials are read once, immediately before evaluation. Fixture
	// profiles have no credential slots, so preflight performs no lookup.
	credentials, err := resolved.Preflight(runtime.environment)
	if err != nil {
		return commandError(stderr, "evaluate-profile", credentialError(err))
	}
	defer credentials.Clear()
	result, err := runner.Run(context.Background(), runner.Input{
		Policy: artifact, Profile: p, CanonicalInput: input,
		CorrelationID: correlationID, Credentials: credentials,
		// Only profiles made entirely of the installed fixture adapter may run
		// synthetic evidence. Semantic profiles never receive this permission.
		AllowSyntheticFixtures: kind == fixtureProfile,
	}, registry)
	if err != nil {
		return commandError(stderr, "evaluate-profile", err)
	}
	return writeJSON(stdout, stderr, "evaluate-profile", result)
}

type profileKind int

const (
	unsupportedProfile profileKind = iota
	fixtureProfile
	openAIProfile
)

var errAdapterNotInstalled = errors.New("the selected profile uses an evaluator adapter that is not installed; installed adapters are " +
	fixture.AdapterID + "@" + fixture.AdapterVersion + " and " + openai.AdapterID + "@" + openai.AdapterVersion)

// classifyProfile accepts a profile only when every evaluator uses the same
// installed adapter family and, for OpenAI, the adapter's required fields.
func classifyProfile(p profile.Artifact) (profileKind, error) {
	fixtureIdentity := profile.ComponentIdentity{ID: fixture.AdapterID, Version: fixture.AdapterVersion}
	kind := unsupportedProfile
	for _, entry := range p.Spec.Evaluators {
		next := unsupportedProfile
		switch {
		case entry.Mode == profile.ModeDeterministicFixture && entry.Adapter == fixtureIdentity:
			next = fixtureProfile
		case entry.Mode == profile.ModeSemantic && entry.Adapter == openai.Identity:
			if err := openai.ValidateEvaluator(entry); err != nil {
				return unsupportedProfile, fmt.Errorf("evaluator %q: %w", entry.ID, err)
			}
			next = openAIProfile
		}
		if next == unsupportedProfile || (kind != unsupportedProfile && kind != next) {
			return unsupportedProfile, errAdapterNotInstalled
		}
		kind = next
	}
	if kind == unsupportedProfile {
		return unsupportedProfile, errAdapterNotInstalled
	}
	return kind, nil
}

// resolveExecutionProfile selects configuration without consulting saved trust
// unless selection itself requires credential authority from the project. Only
// then, and only for an installed semantic profile, is saved trust read. It
// never grants trust or prompts for credentials a profile cannot use.
func resolveExecutionProfile(runtime configRuntime, profilePath, bindingsPath string) (*localconfig.Resolved, profile.Artifact, profileKind, error) {
	resolved, err := resolveEvaluationConfig(runtime, profilePath, bindingsPath, "")
	var trust *localconfig.TrustRequiredError
	var missing *localbinding.MissingCredentialError
	switch {
	case errors.As(err, &missing):
		// Installed adapters always have a default reference, so an unbound
		// slot belongs to an adapter this binary cannot run.
		return nil, profile.Artifact{}, unsupportedProfile, errAdapterNotInstalled
	case errors.As(err, &trust):
		// Resolve with a temporary approval only to identify the profile.
		inspected, inspectErr := resolveEvaluationConfig(runtime, profilePath, bindingsPath, trust.Digest)
		if inspectErr != nil {
			return nil, profile.Artifact{}, unsupportedProfile, inspectErr
		}
		p, profileErr := inspected.Profile()
		if profileErr != nil {
			return nil, profile.Artifact{}, unsupportedProfile, profileErr
		}
		if kind, classifyErr := classifyProfile(p); classifyErr != nil || kind != openAIProfile {
			return nil, profile.Artifact{}, unsupportedProfile, errAdapterNotInstalled
		}
		trusted, trustErr := projectTrusted(runtime, trust.Digest)
		if trustErr != nil {
			return nil, profile.Artifact{}, unsupportedProfile, fmt.Errorf("trust store: %w", trustErr)
		}
		if !trusted {
			return nil, profile.Artifact{}, unsupportedProfile, fmt.Errorf("project configuration reads credentials and requires trust; review it with antaeus config inspect, then run antaeus config trust --digest %s", trust.Digest)
		}
		resolved, err = inspected, nil
	}
	if err != nil {
		return nil, profile.Artifact{}, unsupportedProfile, err
	}
	p, err := resolved.Profile()
	if err != nil {
		return nil, profile.Artifact{}, unsupportedProfile, err
	}
	kind, err := classifyProfile(p)
	if err != nil {
		return nil, profile.Artifact{}, unsupportedProfile, err
	}
	return resolved, p, kind, nil
}

func credentialError(err error) error {
	var missing *localbinding.MissingCredentialError
	if errors.As(err, &missing) {
		return fmt.Errorf("%w; set the referenced environment variable (see docs/openai-adapter.md for the adapter default) or configure a reference with --bindings", missing)
	}
	return err
}

func randomCorrelationID() (string, error) {
	var b [12]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", errors.New("cannot generate a correlation ID")
	}
	return "cli-" + hex.EncodeToString(b[:]), nil
}

// installedDefaults are the documented credential references of installed
// adapters, keyed by exact adapter identity. Explicit bindings still win.
func installedDefaults() map[profile.ComponentIdentity]map[string]localbinding.Reference {
	return map[profile.ComponentIdentity]map[string]localbinding.Reference{openai.Identity: openai.DefaultReferences()}
}

// Reuse the configuration commands' strict loaders. trustedDigest is empty
// unless the caller has verified saved trust (or is only identifying the
// profile, in which case the result must not be used for preflight).
func resolveEvaluationConfig(runtime configRuntime, profilePath, bindingsPath, trustedDigest string) (*localconfig.Resolved, error) {
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
	return localconfig.Resolve(localconfig.Options{CLI: cli, Project: project, User: user, TrustedProjectDigest: trustedDigest, Defaults: installedDefaults()})
}
