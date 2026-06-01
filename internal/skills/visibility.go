package skills

import (
	"fmt"
	"strings"
)

// Skill visibility values.
const (
	VisibilityPrivate  = "private"
	VisibilityInternal = "internal"
	VisibilityPublic   = "public"
)

// DefaultVisibility is assigned when a caller does not specify one.
// Private matches the historical hardcoded default and is the safer choice.
const DefaultVisibility = VisibilityPrivate

// validVisibilities enumerates the accepted enum values. System skills use
// "public"; grant-scoped skills use "internal"; user-published skills default
// to "private".
var validVisibilities = map[string]struct{}{
	VisibilityPrivate:  {},
	VisibilityInternal: {},
	VisibilityPublic:   {},
}

// NormalizeVisibility lowercases + trims the input and returns the default
// when empty. It does not validate — pair with ValidateVisibility.
func NormalizeVisibility(v string) string {
	v = strings.ToLower(strings.TrimSpace(v))
	if v == "" {
		return DefaultVisibility
	}
	return v
}

// ValidateVisibility returns an error if v is not one of the supported enum
// values. An empty string is treated as valid (caller applies the default).
func ValidateVisibility(v string) error {
	if v == "" {
		return nil
	}
	if _, ok := validVisibilities[strings.ToLower(strings.TrimSpace(v))]; !ok {
		return fmt.Errorf("invalid visibility %q: must be one of private, internal, public", v)
	}
	return nil
}

// IsValidVisibility reports whether v is a recognized enum value. Empty is false.
func IsValidVisibility(v string) bool {
	_, ok := validVisibilities[strings.ToLower(strings.TrimSpace(v))]
	return ok
}
