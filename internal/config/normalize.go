package config

import (
	"regexp"
	"strings"
)

const DefaultAgentID = "default"

var (
	validIDRe    = regexp.MustCompile(`^[a-z0-9][a-z0-9_-]{0,63}$`)
	invalidChars = regexp.MustCompile(`[^a-z0-9_-]+`)
	leadingDash  = regexp.MustCompile(`^-+`)
	trailingDash = regexp.MustCompile(`-+$`)
)

// NormalizeAgentID converts a user-provided name into a valid agent ID.
// Matching TS normalizeAgentId() from routing/session-key.ts:
//   - Lowercase, max 64 chars
//   - Only [a-z0-9_-] allowed
//   - Invalid chars replaced with "-"
//   - Leading/trailing dashes stripped
//   - Empty result defaults to "default"
func NormalizeAgentID(name string) string {
	trimmed := strings.TrimSpace(name)
	if trimmed == "" {
		return DefaultAgentID
	}

	lower := strings.ToLower(trimmed)
	if validIDRe.MatchString(lower) {
		return lower
	}

	// Best-effort: collapse invalid chars to "-"
	result := invalidChars.ReplaceAllString(lower, "-")
	result = leadingDash.ReplaceAllString(result, "")
	result = trailingDash.ReplaceAllString(result, "")

	if len(result) > 64 {
		result = result[:64]
	}

	if result == "" {
		return DefaultAgentID
	}
	return result
}
