package localbinding

import (
	"fmt"
	"slices"
	"sync"

	"github.com/antaeusio/antaeus/evaluator/profile"
)

type Environment interface {
	LookupEnv(name string) (string, bool)
}

type EnvironmentFunc func(string) (string, bool)

func (f EnvironmentFunc) LookupEnv(name string) (string, bool) {
	return f(name)
}

type MissingCredentialError struct {
	EvaluatorID string
	AdapterID   string
	Slot        string
}

func (e *MissingCredentialError) Error() string {
	return fmt.Sprintf("evaluator %q adapter %q credential slot %q requires a non-empty environment binding", e.EvaluatorID, e.AdapterID, e.Slot)
}

// Credentials contains the values captured once immediately before an
// evaluation. Call Clear as soon as the evaluation ends.
type Credentials struct {
	mu     sync.RWMutex
	values map[Key][]byte
	clear  bool
}

func Preflight(evaluatorProfile profile.Artifact, bindings Artifact, environment Environment) (*Credentials, error) {
	if err := evaluatorProfile.Validate(); err != nil {
		return nil, fmt.Errorf("validate evaluator profile: %w", err)
	}
	if err := bindings.Validate(); err != nil {
		return nil, fmt.Errorf("validate local secret bindings: %w", err)
	}
	if environment == nil {
		return nil, fmt.Errorf("environment lookup is required")
	}
	evaluators := make(map[string]profile.Evaluator, len(evaluatorProfile.Spec.Evaluators))
	for _, evaluator := range evaluatorProfile.Spec.Evaluators {
		evaluators[evaluator.ID] = evaluator
	}
	route := []string{evaluatorProfile.Spec.Routing.Primary}
	if evaluatorProfile.Spec.Routing.Escalation != nil {
		route = append(route, *evaluatorProfile.Spec.Routing.Escalation)
	}
	route = append(route, evaluatorProfile.Spec.Routing.Fallbacks...)
	credentials := &Credentials{values: make(map[Key][]byte)}
	for _, evaluatorID := range route {
		evaluator := evaluators[evaluatorID]
		if evaluator.CredentialSlot == nil {
			continue
		}
		key := Key{AdapterID: evaluator.Adapter.ID, Slot: *evaluator.CredentialSlot}
		if _, exists := credentials.values[key]; exists {
			continue
		}
		reference, exists := bindings.SecretBindings[key.AdapterID][key.Slot]
		if !exists {
			credentials.Clear()
			return nil, &MissingCredentialError{EvaluatorID: evaluator.ID, AdapterID: key.AdapterID, Slot: key.Slot}
		}
		value, exists := environment.LookupEnv(reference.Name)
		if !exists || value == "" {
			credentials.Clear()
			return nil, &MissingCredentialError{EvaluatorID: evaluator.ID, AdapterID: key.AdapterID, Slot: key.Slot}
		}
		credentials.values[key] = []byte(value)
	}
	return credentials, nil
}

// Credential returns a copy of one captured value. It returns false after Clear.
func (c *Credentials) Credential(adapterID, slot string) ([]byte, bool) {
	if c == nil {
		return nil, false
	}
	c.mu.RLock()
	defer c.mu.RUnlock()
	if c.clear {
		return nil, false
	}
	value, exists := c.values[Key{AdapterID: adapterID, Slot: slot}]
	return slices.Clone(value), exists
}

// Clear overwrites captured bytes and permanently closes this credential set.
func (c *Credentials) Clear() {
	if c == nil {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.clear {
		return
	}
	for key, value := range c.values {
		clear(value)
		delete(c.values, key)
	}
	c.clear = true
}
