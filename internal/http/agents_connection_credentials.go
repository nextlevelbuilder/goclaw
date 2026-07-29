package http

import "strings"

// The per-agent connected-agent credential handlers that used to live here were
// removed when the tenant-level connection catalogue replaced them; see
// connections.go. What remains is the provider→injection mapping, which the
// tenant-level handlers (and the login flow) share.

// injectForConnection maps (provider, credType) to the sandbox injection
// descriptor. Empty = unsupported combination.
func injectForConnection(provider, credType string) string {
	switch strings.ToLower(provider) {
	case "claude_code", "claude", "claudecode":
		switch credType {
		case "api_key":
			return "env:ANTHROPIC_API_KEY"
		case "oauth":
			return "env:CLAUDE_CODE_OAUTH_TOKEN"
		}
	}
	return ""
}
