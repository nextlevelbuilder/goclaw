package http

import (
	"context"
	"crypto/subtle"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/nextlevelbuilder/goclaw/internal/bus"
	"github.com/nextlevelbuilder/goclaw/internal/crypto"
	"github.com/nextlevelbuilder/goclaw/internal/i18n"
	"github.com/nextlevelbuilder/goclaw/internal/permissions"
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
	if after, ok := strings.CutPrefix(model, "goclaw:"); ok {
		return after
	}
	if after, ok := strings.CutPrefix(model, "agent:"); ok {
		return after
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

// --- Package-level API key cache for shared auth ---

var pkgAPIKeyCache *apiKeyCache

// InitAPIKeyCache initializes the shared API key cache with TTL and pubsub invalidation.
// Must be called once during server startup before handling requests.
func InitAPIKeyCache(s store.APIKeyStore, mb *bus.MessageBus) {
	pkgAPIKeyCache = newAPIKeyCache(s, 5*time.Minute)
	if mb != nil {
		mb.Subscribe("http-api-key-cache", func(e bus.Event) {
			if p, ok := e.Payload.(bus.CacheInvalidatePayload); ok && p.Kind == bus.CacheKindAPIKeys {
				pkgAPIKeyCache.invalidateAll()
			}
		})
	}
}

// ResolveAPIKey checks if the bearer token is a valid API key using the shared cache.
// Returns the key data and derived role, or nil if not found/expired/revoked.
func ResolveAPIKey(ctx context.Context, token string) (*store.APIKeyData, permissions.Role) {
	if pkgAPIKeyCache == nil || token == "" {
		return nil, ""
	}
	hash := crypto.HashAPIKey(token)
	return pkgAPIKeyCache.getOrFetch(ctx, hash)
}

// authResult holds the resolved authentication state for an HTTP request.
type authResult struct {
	Role          permissions.Role
	Authenticated bool
}

// resolveAuth determines the caller's role from the request.
// Priority: gateway token → API key → no-auth fallback.
func resolveAuth(r *http.Request, gatewayToken string) authResult {
	return resolveAuthBearer(r, gatewayToken, extractBearerToken(r))
}

// resolveAuthBearer is like resolveAuth but accepts a pre-extracted bearer token.
// Useful for handlers that also accept tokens from query params.
func resolveAuthBearer(r *http.Request, gatewayToken, bearer string) authResult {
	// Gateway token → admin
	if gatewayToken != "" && tokenMatch(bearer, gatewayToken) {
		return authResult{Role: permissions.RoleAdmin, Authenticated: true}
	}
	// API key → role from scopes
	if _, role := ResolveAPIKey(r.Context(), bearer); role != "" {
		return authResult{Role: role, Authenticated: true}
	}
	// No auth configured → operator (backward compat)
	if gatewayToken == "" {
		return authResult{Role: permissions.RoleOperator, Authenticated: true}
	}
	return authResult{}
}

// httpMinRole returns the minimum role required for an HTTP endpoint based on HTTP method.
func httpMinRole(method string) permissions.Role {
	switch method {
	case http.MethodGet, http.MethodHead, http.MethodOptions:
		return permissions.RoleViewer
	default: // POST, PUT, PATCH, DELETE
		return permissions.RoleOperator
	}
}

// requireAuth is a middleware that checks authentication and minimum role.
// Pass "" for minRole to auto-detect from HTTP method (GET→Viewer, POST→Operator).
// Injects locale and userID into request context.
func requireAuth(token string, minRole permissions.Role, next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		locale := extractLocale(r)
		auth := resolveAuth(r, token)

		if !auth.Authenticated {
			writeJSON(w, http.StatusUnauthorized, map[string]string{
				"error": i18n.T(locale, i18n.MsgUnauthorized),
			})
			return
		}

		required := minRole
		if required == "" {
			required = httpMinRole(r.Method)
		}

		if !permissions.HasMinRole(auth.Role, required) {
			writeJSON(w, http.StatusForbidden, map[string]string{
				"error": i18n.T(locale, i18n.MsgPermissionDenied, r.URL.Path),
			})
			return
		}

		ctx := store.WithLocale(r.Context(), locale)
		if userID := extractUserID(r); userID != "" {
			ctx = store.WithUserID(ctx, userID)
		}
		next(w, r.WithContext(ctx))
	}
}

// requireAuthBearer is like requireAuth but accepts a pre-extracted bearer token.
// Used by handlers that accept tokens from query params (files, media).
func requireAuthBearer(token string, minRole permissions.Role, bearer string, w http.ResponseWriter, r *http.Request) bool {
	locale := extractLocale(r)
	auth := resolveAuthBearer(r, token, bearer)

	if !auth.Authenticated {
		writeJSON(w, http.StatusUnauthorized, map[string]string{
			"error": i18n.T(locale, i18n.MsgUnauthorized),
		})
		return false
	}

	required := minRole
	if required == "" {
		required = httpMinRole(r.Method)
	}

	if !permissions.HasMinRole(auth.Role, required) {
		writeJSON(w, http.StatusForbidden, map[string]string{
			"error": i18n.T(locale, i18n.MsgPermissionDenied, r.URL.Path),
		})
		return false
	}
	return true
}

// extractLocale parses the Accept-Language header and returns a supported locale.
// Falls back to "en" if no supported language is found.
func extractLocale(r *http.Request) string {
	accept := r.Header.Get("Accept-Language")
	if accept == "" {
		return i18n.DefaultLocale
	}
	// Simple parser: take the first language tag before comma or semicolon
	for part := range strings.SplitSeq(accept, ",") {
		tag := strings.TrimSpace(strings.SplitN(part, ";", 2)[0])
		locale := i18n.Normalize(tag)
		if locale != i18n.DefaultLocale || strings.HasPrefix(tag, "en") {
			return locale
		}
	}
	return i18n.DefaultLocale
}
