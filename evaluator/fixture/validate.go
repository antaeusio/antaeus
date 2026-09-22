package fixture

import (
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/antaeusio/antaeus/evaluator"
)

var (
	namePattern   = regexp.MustCompile(`^[a-z][a-z0-9._-]{0,63}$`)
	digestPattern = regexp.MustCompile(`^sha256:[0-9a-f]{64}$`)
)

// Validate checks fixture-set structure and exact rule mappings.
func (s Set) Validate() error {
	if s.APIVersion != APIVersion {
		return fmt.Errorf("apiVersion must equal %s", APIVersion)
	}
	if s.Kind != Kind {
		return fmt.Errorf("kind must equal %s", Kind)
	}
	if !namePattern.MatchString(s.Metadata.Name) {
		return fmt.Errorf("metadata name is invalid")
	}
	if !utf8.ValidString(s.Metadata.Version) || strings.TrimSpace(s.Metadata.Version) == "" || utf8.RuneCountInString(s.Metadata.Version) > 128 {
		return fmt.Errorf("metadata version must contain 1 to 128 non-whitespace Unicode code points")
	}
	if len(s.Cases) == 0 || len(s.Cases) > MaxCases {
		return fmt.Errorf("cases must contain 1 to %d entries", MaxCases)
	}
	seenCases := make(map[string]struct{}, len(s.Cases))
	for i, fixtureCase := range s.Cases {
		if !namePattern.MatchString(fixtureCase.Name) {
			return fmt.Errorf("case %d name is invalid", i)
		}
		if _, exists := seenCases[fixtureCase.Name]; exists {
			return fmt.Errorf("case name %q is duplicated", fixtureCase.Name)
		}
		seenCases[fixtureCase.Name] = struct{}{}
		if err := validateCase(fixtureCase); err != nil {
			return fmt.Errorf("case %q: %w", fixtureCase.Name, err)
		}
	}
	return nil
}

func validateCase(fixtureCase Case) error {
	rules := make([]evaluator.Rule, len(fixtureCase.RuleResults))
	for i, result := range fixtureCase.RuleResults {
		rules[i] = evaluator.Rule{ID: result.RuleID, When: "Synthetic fixture condition."}
	}
	request := evaluator.Request{
		PolicyName:     fixtureCase.PolicyName,
		PolicyDigest:   fixtureCase.PolicyDigest,
		ProfileDigest:  "sha256:0000000000000000000000000000000000000000000000000000000000000000",
		CanonicalInput: json.RawMessage(`{}`),
		Rules:          rules,
		Deadline:       time.Unix(1, 0),
		CorrelationID:  "fixture-validation",
	}
	if err := evaluator.ValidateResult(request, evaluator.Result{
		RuleResults: fixtureCase.RuleResults,
		Metadata: evaluator.Metadata{
			AdapterID:      AdapterID,
			AdapterVersion: AdapterVersion,
			Mode:           evaluator.ModeDeterministicFixture,
			Synthetic:      true,
			FixtureSet:     "validation",
			FixtureVersion: "v1",
		},
	}); err != nil {
		return err
	}
	if !digestPattern.MatchString(fixtureCase.InputDigest) {
		return fmt.Errorf("input digest is invalid")
	}
	return nil
}
