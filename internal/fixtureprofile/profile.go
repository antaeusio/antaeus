// Package fixtureprofile defines the shared local deterministic profile identity.
package fixtureprofile

import (
	"crypto/sha256"
	"fmt"
)

const (
	Preimage = "antaeus.local.fixture/v0alpha1"
	Version  = "v0alpha1"
)

// Digest returns the SHA-256 identity of the exact profile preimage.
func Digest() string {
	digest := sha256.Sum256([]byte(Preimage))
	return fmt.Sprintf("sha256:%x", digest)
}
