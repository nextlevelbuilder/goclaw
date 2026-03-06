package http

import (
	"crypto/subtle"
	"log/slog"
	"net/http"
	"strings"

	"github.com/nextlevelbuilder/goclaw/internal/store"
)

// extractBearerToken extracts a bearer token from the Authorization header.
func extractBearerToken(r *http.Request) string {
	auth := r.Header.Get("Authorization")
	if auth == "" {
		return ""
	}
	if !strings.HasPrefix(auth, "Bearer ") {
		return ""
	}
	return strings.TrimPrefix(auth, "Bearer ")
}

// tokenMatch performs a constant-time comparison of a provided token against the expected token.
// Returns true if expected is empty (no auth configured) or if tokens match.
func tokenMatch(provided, expected string) bool {
	if expected == "" {
		return true
	}
	return subtle.ConstantTimeCompare([]byte(provided), []byte(expected)) == 1
}

// extractUserID extracts the external user ID from the request header.
// Returns "" if no user ID is provided (anonymous).
// Rejects IDs exceeding MaxUserIDLength (VARCHAR(255) DB constraint).
func extractUserID(r *http.Request) string {
	id := r.Header.Get("X-GoClaw-User-Id")
	if id == "" {
		return ""
	}
	if err := store.ValidateUserID(id); err != nil {
		slog.Warn("security.user_id_too_long", "length", len(id), "max", store.MaxUserIDLength)
		return ""
	}
	return id
}

// extractAgentID determines the target agent from the request.
// Checks model field, headers, and falls back to "default".
func extractAgentID(r *http.Request, model string) string {
	// From model field: "goclaw:<agentId>" or "agent:<agentId>"
	if strings.HasPrefix(model, "goclaw:") {
		return strings.TrimPrefix(model, "goclaw:")
	}
	if strings.HasPrefix(model, "agent:") {
		return strings.TrimPrefix(model, "agent:")
	}

	// From headers
	if id := r.Header.Get("X-GoClaw-Agent-Id"); id != "" {
		return id
	}
	if id := r.Header.Get("X-GoClaw-Agent"); id != "" {
		return id
	}

	return "default"
}
