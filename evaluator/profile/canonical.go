package profile

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"

	"github.com/gowebpki/jcs"
)

func (a Artifact) CanonicalJSON() ([]byte, error) {
	if err := a.Validate(); err != nil {
		return nil, err
	}
	encoded, err := json.Marshal(a)
	if err != nil {
		return nil, fmt.Errorf("marshal evaluator profile: %w", err)
	}
	canonical, err := jcs.Transform(encoded)
	if err != nil {
		return nil, fmt.Errorf("canonicalize evaluator profile: %w", err)
	}
	return canonical, nil
}

func (a Artifact) Digest() (string, error) {
	canonical, err := a.CanonicalJSON()
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(canonical)
	return "sha256:" + hex.EncodeToString(sum[:]), nil
}
