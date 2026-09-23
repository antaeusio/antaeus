// Package localbinding loads non-secret local credential references and
// preflights the environment values required by an evaluator profile.
package localbinding

const (
	APIVersion = "config.antaeus.io/v0alpha1"
	Kind       = "LocalSecretBindings"

	MaxSourceBytes = 1 << 20
	MaxAdapters    = 16
	MaxSlots       = 16
)

type Artifact struct {
	APIVersion     string                          `json:"apiVersion"`
	Kind           string                          `json:"kind"`
	SecretBindings map[string]map[string]Reference `json:"secretBindings"`
}

type Reference struct {
	Source string `json:"source"`
	Name   string `json:"name"`
}

type Key struct {
	AdapterID string
	Slot      string
}
