package http

import "net/http"

// resolveHTTPRole determines the caller's role from the HTTP request context.
// - Bearer token matches configured gateway token → admin
// - Keycloak JWT (reflected via X-GoClaw-User-Id being set) → operator
// - No token configured → operator (backward compat)
// - Missing/invalid token → viewer (read-only fallback)
func resolveHTTPRole(r *http.Request, configToken string) string {
	if configToken == "" {
		return "operator"
	}
	bearer := extractBearerToken(r)
	if bearer == configToken {
		return "admin"
	}
	if extractUserID(r) != "" {
		return "operator"
	}
	return "viewer"
}
