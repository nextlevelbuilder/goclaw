package builtin

import "github.com/google/uuid"

// AllowlistFor returns the `mutable_fields` list for a registered builtin
// hook row keyed by the per-event UUID (see BuiltinEventID). Returns nil when
// the id is not a registered builtin — caller (dispatcher) treats nil as
// "strip all mutations" for defense-in-depth.
func AllowlistFor(id uuid.UUID) []string {
	regMu.RLock()
	defer regMu.RUnlock()
	if s, ok := eventIDSpec[id]; ok {
		return s.MutableFields
	}
	return nil
}

// IsBuiltin reports whether id belongs to a registered builtin row.
func IsBuiltin(id uuid.UUID) bool {
	regMu.RLock()
	defer regMu.RUnlock()
	_, ok := eventIDSpec[id]
	return ok
}
