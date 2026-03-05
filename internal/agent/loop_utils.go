package agent

import "strings"

// sanitizePathSegment makes a userID safe for use as a directory name.
// Replaces colons, spaces, and other unsafe chars with underscores.
func sanitizePathSegment(s string) string {
	var b strings.Builder
	for _, r := range s {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '-' || r == '_' {
			b.WriteRune(r)
		} else {
			b.WriteByte('_')
		}
	}
	return b.String()
}

// InvalidateUserWorkspace clears the cached workspace for a user,
// forcing the next request to re-read from user_agent_profiles.
func (l *Loop) InvalidateUserWorkspace(userID string) {
	l.userWorkspaces.Delete(userID)
}

// ProviderName returns the name of this agent's LLM provider (e.g. "anthropic", "openai").
func (l *Loop) ProviderName() string {
	if l.provider == nil {
		return ""
	}
	return l.provider.Name()
}
