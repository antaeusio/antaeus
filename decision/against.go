package decision

import (
	"fmt"

	"github.com/antaeusio/antaeus/policy"
)

// ValidateAgainst verifies the Decision's policy identity, rule binding, and
// terminal outcome against one validated policy artifact.
func (d Decision) ValidateAgainst(artifact policy.Artifact) error {
	if err := d.Validate(); err != nil {
		return err
	}
	if err := artifact.Validate(); err != nil {
		return fmt.Errorf("validate policy: %w", err)
	}
	digest, err := artifact.Digest()
	if err != nil {
		return fmt.Errorf("digest policy: %w", err)
	}
	if d.Policy.Name != artifact.Metadata.Name {
		return fmt.Errorf("decision policy name %q does not match artifact name %q", d.Policy.Name, artifact.Metadata.Name)
	}
	if d.Policy.Digest != digest {
		return fmt.Errorf("decision policy digest %q does not match artifact digest %q", d.Policy.Digest, digest)
	}
	reduction, err := Reduce(artifact, d.RuleResults)
	if err != nil {
		return err
	}
	if d.Outcome != reduction.Outcome {
		return fmt.Errorf("decision outcome %q does not match reduced outcome %q", d.Outcome, reduction.Outcome)
	}
	for _, code := range reduction.ReasonCodes {
		if !contains(d.ReasonCodes, code) {
			return fmt.Errorf("decision reason codes do not include reduced reason %q", code)
		}
	}
	return nil
}
