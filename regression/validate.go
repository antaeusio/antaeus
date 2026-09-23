package regression

import (
	"fmt"
	"regexp"
	"slices"
	"strings"
	"unicode/utf8"

	"github.com/antaeusio/antaeus/decision"
	"github.com/antaeusio/antaeus/internal/jsonvalue"
)

var (
	namePattern       = regexp.MustCompile(`^[a-z][a-z0-9._-]{0,63}$`)
	policyNamePattern = regexp.MustCompile(`^[a-z][a-z0-9-]{0,62}$`)
	digestPattern     = regexp.MustCompile(`^sha256:[0-9a-f]{64}$`)
	reasonCodePattern = regexp.MustCompile(`^[a-z][a-z0-9_.-]{0,127}$`)
)

// Validate checks a programmatically constructed suite and every inline input.
func (s Suite) Validate() error {
	if s.APIVersion != APIVersion {
		return fmt.Errorf("apiVersion must equal %s", APIVersion)
	}
	if s.Kind != KindSuite {
		return fmt.Errorf("kind must equal %s", KindSuite)
	}
	if err := validateIdentity("metadata", s.Metadata); err != nil {
		return err
	}
	if !policyNamePattern.MatchString(s.Policy.Name) {
		return fmt.Errorf("policy name is invalid")
	}
	if !digestPattern.MatchString(s.Policy.Digest) {
		return fmt.Errorf("policy digest is invalid")
	}
	if err := validateIdentity("fixtureSet", s.FixtureSet); err != nil {
		return err
	}
	if len(s.Cases) == 0 || len(s.Cases) > MaxCases {
		return fmt.Errorf("cases must contain 1 to %d entries", MaxCases)
	}
	seen := make(map[string]struct{}, len(s.Cases))
	for index, testCase := range s.Cases {
		if !namePattern.MatchString(testCase.Name) {
			return fmt.Errorf("case %d name is invalid", index)
		}
		if _, exists := seen[testCase.Name]; exists {
			return fmt.Errorf("case name %q is duplicated", testCase.Name)
		}
		seen[testCase.Name] = struct{}{}
		if testCase.Description != "" && (!utf8.ValidString(testCase.Description) || strings.TrimSpace(testCase.Description) == "" || utf8.RuneCountInString(testCase.Description) > MaxDescription) {
			return fmt.Errorf("case %q description must contain 1 to %d non-whitespace Unicode code points", testCase.Name, MaxDescription)
		}
		if !namePattern.MatchString(testCase.FixtureCase) {
			return fmt.Errorf("case %q fixtureCase is invalid", testCase.Name)
		}
		if _, err := jsonvalue.CanonicalObject(testCase.Input); err != nil {
			return fmt.Errorf("case %q input: %w", testCase.Name, err)
		}
		if err := testCase.Expect.validate(); err != nil {
			return fmt.Errorf("case %q expect: %w", testCase.Name, err)
		}
	}
	return nil
}

func validateIdentity(path string, identity Identity) error {
	if !namePattern.MatchString(identity.Name) {
		return fmt.Errorf("%s name is invalid", path)
	}
	if !utf8.ValidString(identity.Version) || strings.TrimSpace(identity.Version) == "" || utf8.RuneCountInString(identity.Version) > 128 {
		return fmt.Errorf("%s version must contain 1 to 128 non-whitespace Unicode code points", path)
	}
	return nil
}

func (e Expectation) validate() error {
	if !e.Outcome.Valid() {
		return fmt.Errorf("outcome must be allow, review, deny, or failure")
	}
	if len(e.ReasonCodes) == 0 || len(e.ReasonCodes) > decision.MaxReasonCodes {
		return fmt.Errorf("reasonCodes must contain 1 to %d entries", decision.MaxReasonCodes)
	}
	seen := make(map[string]struct{}, len(e.ReasonCodes))
	for _, code := range e.ReasonCodes {
		if !reasonCodePattern.MatchString(code) {
			return fmt.Errorf("reason code %q is invalid", code)
		}
		if _, exists := seen[code]; exists {
			return fmt.Errorf("reason code %q is duplicated", code)
		}
		seen[code] = struct{}{}
	}
	return nil
}

func expectationMatches(expected Expectation, actual decision.Decision) bool {
	return expected.Outcome == actual.Outcome && slices.Equal(expected.ReasonCodes, actual.ReasonCodes)
}
