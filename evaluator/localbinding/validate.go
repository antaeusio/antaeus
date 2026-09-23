package localbinding

import (
	"fmt"
	"regexp"
	"sort"
)

var (
	adapterIDPattern   = regexp.MustCompile(`^[a-z0-9](?:[a-z0-9-]*[a-z0-9])?(?:\.[a-z0-9](?:[a-z0-9-]*[a-z0-9])?)+$`)
	slotPattern        = regexp.MustCompile(`^[a-z][a-z0-9._-]{0,63}$`)
	environmentPattern = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]{0,127}$`)
)

type ValidationError struct {
	Path    string
	Code    string
	Message string
}

func (e *ValidationError) Error() string {
	return fmt.Sprintf("%s: %s", e.Path, e.Message)
}

func (a Artifact) Validate() error {
	if a.APIVersion != APIVersion {
		return invalid("$.apiVersion", "version.unsupported", "must equal "+APIVersion)
	}
	if a.Kind != Kind {
		return invalid("$.kind", "kind.invalid", "must equal "+Kind)
	}
	if len(a.SecretBindings) == 0 || len(a.SecretBindings) > MaxAdapters {
		return invalid("$.secretBindings", "bindings.invalid", fmt.Sprintf("must contain 1 to %d adapters", MaxAdapters))
	}
	adapters := sortedKeys(a.SecretBindings)
	for _, adapterID := range adapters {
		path := fmt.Sprintf("$.secretBindings[%q]", adapterID)
		if len(adapterID) > 128 || !adapterIDPattern.MatchString(adapterID) || adapterID == "io.antaeus.fixture" {
			return invalid(path, "adapter.invalid", "must be a non-fixture namespaced adapter ID")
		}
		slots := a.SecretBindings[adapterID]
		if len(slots) == 0 || len(slots) > MaxSlots {
			return invalid(path, "slots.invalid", fmt.Sprintf("must contain 1 to %d slots", MaxSlots))
		}
		for _, slot := range sortedKeys(slots) {
			referencePath := fmt.Sprintf("%s[%q]", path, slot)
			if !slotPattern.MatchString(slot) {
				return invalid(referencePath, "slot.invalid", "must be a lowercase local identifier")
			}
			reference := slots[slot]
			if reference.Source != "environment" {
				return invalid(referencePath+".source", "source.invalid", "must equal environment")
			}
			if !environmentPattern.MatchString(reference.Name) {
				return invalid(referencePath+".name", "environment_name.invalid", "must be a valid environment-variable name")
			}
		}
	}
	return nil
}

func sortedKeys[V any](values map[string]V) []string {
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

func invalid(path, code, message string) *ValidationError {
	return &ValidationError{Path: path, Code: code, Message: message}
}
