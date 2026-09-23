package cli

import (
	"bytes"
	"encoding/json"
	"os"
	"strings"
	"testing"

	"github.com/antaeusio/antaeus/evaluator/fixture"
	"github.com/antaeusio/antaeus/regression"
)

func TestLegacyCommandsRejectECMABlankFixtureResultVersion(t *testing.T) {
	source, err := os.ReadFile(contractPath("fixture-set", "quickstart.json"))
	if err != nil {
		t.Fatal(err)
	}
	path := writeCLIFile(t, "fixture.json", strings.Replace(string(source), `"version": "v1"`, `"version": "\ufeff"`, 1))
	set, err := fixture.LoadFile(path)
	if err != nil || set.Metadata.Version != "\ufeff" {
		t.Fatalf("authoring rules changed: version=%q err=%v", set.Metadata.Version, err)
	}
	suite, err := regression.LoadFile(contractPath("regression-suite", "quickstart.json"))
	if err != nil {
		t.Fatal(err)
	}
	suite.FixtureSet.Version = set.Metadata.Version
	if err := suite.Validate(); err != nil {
		t.Fatal(err)
	}
	suiteData, err := json.Marshal(suite)
	if err != nil {
		t.Fatal(err)
	}
	suitePath := writeCLIFile(t, "suite.json", string(suiteData))
	for _, command := range []string{"evaluate", "test"} {
		t.Run(command, func(t *testing.T) {
			args := []string{command, "--policy", contractPath("policy", "vendor-onboarding.yaml"), "--fixture-set", path}
			if command == "evaluate" {
				args = append(args, "--input", contractPath("input", "aggregate-analytics.json"), "--case", "aggregate-analytics")
			} else {
				args = append(args, "--suite", suitePath)
			}
			var out, diagnostic bytes.Buffer
			code := Run(args, &out, &diagnostic)
			if code != 1 || out.Len() != 0 || !strings.Contains(diagnostic.String(), "fixture.mapping_invalid: fixture version") {
				t.Fatalf("exit=%d stdout=%q stderr=%q", code, out.String(), diagnostic.String())
			}
		})
	}
}
