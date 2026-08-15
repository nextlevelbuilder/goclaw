package store

import "fmt"

// ValidateV3Flags checks that any v3 flag keys in the settings map have boolean values.
// Returns nil if no v3 keys are present or all are valid booleans.
func ValidateV3Flags(settings map[string]any) error {
	for key, val := range settings {
		if !IsV3FlagKey(key) {
			continue
		}
		if _, ok := val.(bool); !ok {
			return fmt.Errorf("v3 flag %q must be a boolean, got %T", key, val)
		}
	}
	return nil
}
