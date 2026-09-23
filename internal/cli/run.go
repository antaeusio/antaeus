// Package cli wires the Antaeus command without exposing CLI concerns to the
// reusable engine packages.
package cli

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"time"

	"github.com/antaeusio/antaeus/evaluator"
	"github.com/antaeusio/antaeus/evaluator/fixture"
	"github.com/antaeusio/antaeus/internal/buildinfo"
	"github.com/antaeusio/antaeus/internal/fixtureprofile"
	"github.com/antaeusio/antaeus/policy"
	"github.com/antaeusio/antaeus/regression"
)

const usage = `Usage: antaeus <command>

Commands:
  validate <policy-file>
           Validate a JSON/YAML policy and print its canonical identity
  evaluate --policy <file> --input <file> --fixture-set <file> --case <name>
           Execute one exact synthetic fixture case and print a Decision
  test --policy <file> --suite <file> --fixture-set <file>
           Run an exact synthetic regression suite and print its result set
  version  Print version information
  help     Print this help
`

// Run executes the command and returns a process exit code.
func Run(args []string, stdout, stderr io.Writer) int {
	if len(args) == 0 {
		_, _ = io.WriteString(stdout, usage)
		return 0
	}

	switch args[0] {
	case "validate":
		return runValidate(args[1:], stdout, stderr)
	case "evaluate":
		return runEvaluate(args[1:], stdout, stderr)
	case "test":
		return runTest(args[1:], stdout, stderr)
	case "help", "-h", "--help":
		if len(args) != 1 {
			return usageError(stderr, fmt.Sprintf("%s does not accept arguments", args[0]))
		}
		_, _ = io.WriteString(stdout, usage)
		return 0
	case "version", "--version":
		if len(args) != 1 {
			return usageError(stderr, fmt.Sprintf("%s does not accept arguments", args[0]))
		}
		_, _ = fmt.Fprintf(stdout, "antaeus %s\n", buildinfo.Current())
		return 0
	default:
		return usageError(stderr, fmt.Sprintf("unknown command %q", args[0]))
	}
}

func runValidate(args []string, stdout, stderr io.Writer) int {
	if len(args) != 1 {
		return usageError(stderr, "validate requires exactly one policy file")
	}
	artifact, err := policy.LoadFile(args[0])
	if err != nil {
		return commandError(stderr, "validate", err)
	}
	digest, err := artifact.Digest()
	if err != nil {
		return commandError(stderr, "validate", err)
	}
	result := struct {
		Name   string `json:"name"`
		Digest string `json:"digest"`
		Rules  int    `json:"rules"`
	}{Name: artifact.Metadata.Name, Digest: digest, Rules: len(artifact.Spec.Rules)}
	return writeJSON(stdout, stderr, "validate", result)
}

func runEvaluate(args []string, stdout, stderr io.Writer) int {
	flags := flag.NewFlagSet("evaluate", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	policyPath := flags.String("policy", "", "JSON or YAML policy file")
	inputPath := flags.String("input", "", "JSON object input file")
	fixturePath := flags.String("fixture-set", "", "JSON fixture-set file")
	caseName := flags.String("case", "", "fixture case name")
	if err := flags.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			_, _ = io.WriteString(stdout, evaluateUsage())
			return 0
		}
		return usageError(stderr, "evaluate: "+err.Error())
	}
	if flags.NArg() != 0 {
		return usageError(stderr, "evaluate does not accept positional arguments")
	}
	missing := requiredFlags([]string{"--policy", "--input", "--fixture-set", "--case"}, map[string]string{
		"--case":        *caseName,
		"--fixture-set": *fixturePath,
		"--input":       *inputPath,
		"--policy":      *policyPath,
	})
	if missing != "" {
		return usageError(stderr, "evaluate requires "+missing)
	}

	artifact, err := policy.LoadFile(*policyPath)
	if err != nil {
		return commandError(stderr, "evaluate policy", err)
	}
	input, err := loadCanonicalInput(*inputPath)
	if err != nil {
		return commandError(stderr, "evaluate input", err)
	}
	set, err := fixture.LoadFile(*fixturePath)
	if err != nil {
		return commandError(stderr, "evaluate fixture set", err)
	}
	adapter, err := fixture.New(set, *caseName)
	if err != nil {
		return commandError(stderr, "evaluate fixture case", err)
	}
	result, err := evaluator.Decide(context.Background(), adapter, evaluator.DecisionInput{
		Policy:         artifact,
		CanonicalInput: input,
		Deadline:       time.Now().Add(30 * time.Second),
		CorrelationID:  "cli-fixture-" + *caseName,
		ProfileDigest:  fixtureprofile.Digest(),
		ProfileVersion: fixtureprofile.Version,
	})
	if err != nil {
		return commandError(stderr, "evaluate", err)
	}
	return writeJSON(stdout, stderr, "evaluate", result)
}

func runTest(args []string, stdout, stderr io.Writer) int {
	flags := flag.NewFlagSet("test", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	policyPath := flags.String("policy", "", "JSON or YAML policy file")
	suitePath := flags.String("suite", "", "JSON regression-suite file")
	fixturePath := flags.String("fixture-set", "", "JSON fixture-set file")
	if err := flags.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			_, _ = io.WriteString(stdout, testUsage())
			return 0
		}
		return usageError(stderr, "test: "+err.Error())
	}
	if flags.NArg() != 0 {
		return usageError(stderr, "test does not accept positional arguments")
	}
	missing := requiredFlags([]string{"--policy", "--suite", "--fixture-set"}, map[string]string{
		"--fixture-set": *fixturePath,
		"--policy":      *policyPath,
		"--suite":       *suitePath,
	})
	if missing != "" {
		return usageError(stderr, "test requires "+missing)
	}

	artifact, err := policy.LoadFile(*policyPath)
	if err != nil {
		return commandError(stderr, "test policy", err)
	}
	suite, err := regression.LoadFile(*suitePath)
	if err != nil {
		return commandError(stderr, "test suite", err)
	}
	set, err := fixture.LoadFile(*fixturePath)
	if err != nil {
		return commandError(stderr, "test fixture set", err)
	}
	result, err := regression.Run(context.Background(), artifact, set, suite)
	if err != nil {
		return commandError(stderr, "test", err)
	}
	if code := writeJSON(stdout, stderr, "test", result); code != 0 {
		return code
	}
	if !result.Passed {
		return 2
	}
	return 0
}

func requiredFlags(ordered []string, values map[string]string) string {
	missing := ""
	for _, name := range ordered {
		if values[name] == "" {
			if missing != "" {
				missing += ", "
			}
			missing += name
		}
	}
	return missing
}

func writeJSON(stdout, stderr io.Writer, command string, value any) int {
	encoded, err := json.Marshal(value)
	if err != nil {
		return commandError(stderr, command, err)
	}
	encoded = append(encoded, '\n')
	if _, err := stdout.Write(encoded); err != nil {
		return commandError(stderr, command, err)
	}
	return 0
}

func commandError(stderr io.Writer, command string, err error) int {
	_, _ = fmt.Fprintf(stderr, "antaeus: %s: %v\n", command, err)
	return 1
}

func evaluateUsage() string {
	return "Usage: antaeus evaluate --policy <file> --input <file> --fixture-set <file> --case <name>\n"
}

func testUsage() string {
	return "Usage: antaeus test --policy <file> --suite <file> --fixture-set <file>\n"
}

func usageError(stderr io.Writer, message string) int {
	_, _ = fmt.Fprintf(stderr, "antaeus: %s\n\n%s", message, usage)
	return 64
}
