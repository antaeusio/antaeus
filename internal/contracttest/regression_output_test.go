package contracttest

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/antaeusio/antaeus/decision"
	"github.com/antaeusio/antaeus/evaluator/fixture"
	"github.com/antaeusio/antaeus/policy"
	"github.com/antaeusio/antaeus/regression"
)

func TestGeneratedRegressionResultsConformToSchemaAndExample(t *testing.T) {
	artifact, err := policy.LoadFile(contractsPath(filepath.Join("examples", "v0alpha1", "policy", "vendor-onboarding.yaml")))
	if err != nil {
		t.Fatalf("LoadFile(policy) error = %v", err)
	}
	set, err := fixture.LoadFile(contractsPath(filepath.Join("examples", "v0alpha1", "fixture-set", "quickstart.json")))
	if err != nil {
		t.Fatalf("LoadFile(fixture) error = %v", err)
	}
	suite, err := regression.LoadFile(contractsPath(filepath.Join("examples", "v0alpha1", "regression-suite", "quickstart.json")))
	if err != nil {
		t.Fatalf("LoadFile(suite) error = %v", err)
	}

	passed, err := regression.Run(context.Background(), artifact, set, suite)
	if err != nil {
		t.Fatalf("Run(passed) error = %v", err)
	}
	validateGeneratedResult(t, passed)

	exampleData, err := os.ReadFile(contractsPath(filepath.Join("examples", "v0alpha1", "regression-result-set", "quickstart-passed.json")))
	if err != nil {
		t.Fatalf("ReadFile(example) error = %v", err)
	}
	var example any
	if err := json.Unmarshal(exampleData, &example); err != nil {
		t.Fatalf("Unmarshal(example) error = %v", err)
	}
	if actual := resultDocument(t, passed); !reflect.DeepEqual(actual, example) {
		t.Fatalf("generated passed result does not match published example\ngot:  %#v\nwant: %#v", actual, example)
	}

	suite.Cases[0].Expect.Outcome = decision.OutcomeAllow
	failed, err := regression.Run(context.Background(), artifact, set, suite)
	if err != nil {
		t.Fatalf("Run(failed) error = %v", err)
	}
	if failed.Passed {
		t.Fatal("failed result Passed = true")
	}
	validateGeneratedResult(t, failed)
}

func validateGeneratedResult(t *testing.T, result regression.ResultSet) {
	t.Helper()
	schema, err := newCompiler(t).Compile(schemaBase + "regression-result-set.schema.json")
	if err != nil {
		t.Fatalf("compile result schema: %v", err)
	}
	if err := schema.Validate(resultDocument(t, result)); err != nil {
		t.Fatalf("Validate(generated result) error = %v", err)
	}
}

func resultDocument(t *testing.T, result regression.ResultSet) any {
	t.Helper()
	encoded, err := json.Marshal(result)
	if err != nil {
		t.Fatalf("Marshal(result) error = %v", err)
	}
	var document any
	if err := json.Unmarshal(encoded, &document); err != nil {
		t.Fatalf("Unmarshal(result) error = %v", err)
	}
	return document
}
