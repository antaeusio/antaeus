package policy_test

import (
	"errors"
	"fmt"

	"github.com/antaeusio/antaeus/policy"
)

func ExampleLoadFile() {
	for _, extension := range []string{"yaml", "json"} {
		artifact, err := policy.LoadFile("../contracts/examples/v0alpha1/policy/vendor-onboarding." + extension)
		if err != nil {
			panic(err)
		}
		digest, err := artifact.Digest()
		if err != nil {
			panic(err)
		}
		fmt.Println(extension, artifact.Metadata.Name, len(artifact.Spec.Rules), digest)
	}
	// Output:
	// yaml vendor-onboarding 2 sha256:2378b5a1806bb11c618bd3a78122e773ec93bdfc45b751a18887d40f4ea536a2
	// json vendor-onboarding 2 sha256:2378b5a1806bb11c618bd3a78122e773ec93bdfc45b751a18887d40f4ea536a2
}

func ExampleLoadFile_rejected() {
	for _, path := range []string{
		"policy-source/invalid-duplicate-key.yaml",
		"policy/invalid-unknown-property.json",
		"policy/invalid-duplicate-rule-id.json",
	} {
		_, err := policy.LoadFile("../contracts/conformance/v0alpha1/" + path)
		var source *policy.ParseError
		var validation *policy.ValidationError
		switch {
		case errors.As(err, &source):
			fmt.Println(source.Code)
		case errors.As(err, &validation):
			fmt.Println(validation.Code, validation.Path)
		default:
			panic(fmt.Sprintf("expected a typed policy rejection, got %v", err))
		}
	}
	// Output:
	// source.duplicate_key
	// source.schema
	// rule_id.duplicate $.spec.rules[1].id
}
