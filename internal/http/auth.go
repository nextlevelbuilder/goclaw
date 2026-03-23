package http

import (
	"context"
	"crypto/subtle"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/google/uuid"
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
var pkgPairingStore store.PairingStore
var pkgTenantCache *tenantCache
var pkgOwnerIDs []string

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

// InitPairingAuth sets the pairing store for HTTP auth.
// Allows browser-paired users to access HTTP APIs via X-GoClaw-Sender-Id header.
func InitPairingAuth(ps store.PairingStore) {
	pkgPairingStore = ps
}

// InitOwnerIDs sets the configured owner user IDs for HTTP auth.
// Only owners get cross-tenant access with gateway token; others are tenant-scoped.
func InitOwnerIDs(ids []string) {
	pkgOwnerIDs = ids
}

// isHTTPOwnerID checks if the user ID is a configured owner.
// If no owner IDs configured, only "system" is treated as owner (fail-closed).
func isHTTPOwnerID(userID string, ownerIDs []string) bool {
	if userID == "" {
		return false
	}
	if len(ownerIDs) == 0 {
		return userID == "system"
	}
	for _, id := range ownerIDs {
		if id == userID {
			return true
		}
	}
	return false
}

// InitTenantStore sets the tenant cache for HTTP auth with TTL and pubsub invalidation.
// Allows gateway token requests to scope to a specific tenant via X-GoClaw-Tenant-Id header.
func InitTenantStore(ts store.TenantStore, mb *bus.MessageBus) {
	pkgTenantCache = newTenantCache(ts, 5*time.Minute)
	if mb != nil {
		mb.Subscribe("http-tenant-cache", func(e bus.Event) {
			if p, ok := e.Payload.(bus.CacheInvalidatePayload); ok && p.Kind == bus.CacheKindTenants {
				pkgTenantCache.invalidateAll()
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
	KeyData       *store.APIKeyData // non-nil when authenticated via API key
	TenantID      uuid.UUID         // resolved tenant; uuid.Nil for cross-tenant
	CrossTenant   bool              // true for owner/system admin
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
	// Only owner IDs get cross-tenant access; others are tenant-scoped.
	// If no owner IDs configured, all gateway token users get cross-tenant (backward compat).
	if gatewayToken != "" && tokenMatch(bearer, gatewayToken) {
		userID := extractUserID(r)
		isOwner := isHTTPOwnerID(userID, pkgOwnerIDs)
		res := authResult{Role: permissions.RoleAdmin, Authenticated: true, CrossTenant: isOwner}
		tenantVal := r.Header.Get("X-GoClaw-Tenant-Id")
		if isOwner && tenantVal != "" && pkgTenantCache != nil {
			// Cross-tenant admin can narrow scope via header
			if tid, err := uuid.Parse(tenantVal); err == nil {
				if t, err := pkgTenantCache.GetTenant(r.Context(), tid); err == nil && t != nil {
					res.TenantID = t.ID
				}
			} else if t, err := pkgTenantCache.GetTenantBySlug(r.Context(), tenantVal); err == nil && t != nil {
				res.TenantID = t.ID
			}
		} else if !isOwner && pkgTenantCache != nil {
			// Non-owner with gateway token: resolve tenant from header or fallback to master
			if tenantVal != "" {
				if tid, err := uuid.Parse(tenantVal); err == nil {
					if t, err := pkgTenantCache.GetTenant(r.Context(), tid); err == nil && t != nil {
						res.TenantID = t.ID
					}
				} else if t, err := pkgTenantCache.GetTenantBySlug(r.Context(), tenantVal); err == nil && t != nil {
					res.TenantID = t.ID
				}
			}
			if res.TenantID == uuid.Nil {
				res.TenantID = store.MasterTenantID
			}
		}
		return res
	}
	// API key → role from scopes
	if keyData, role := ResolveAPIKey(r.Context(), bearer); role != "" {
		res := authResult{Role: role, Authenticated: true, KeyData: keyData}
		if keyData.TenantID == uuid.Nil {
			res.CrossTenant = true
		} else {
			res.TenantID = keyData.TenantID
		}
		return res
	}
	// Browser pairing → operator (via X-GoClaw-Sender-Id header)
	if senderID := r.Header.Get("X-GoClaw-Sender-Id"); senderID != "" && pkgPairingStore != nil {
		paired, err := pkgPairingStore.IsPaired(r.Context(), senderID, "browser")
		if err == nil && paired {
			return authResult{Role: permissions.RoleOperator, Authenticated: true, TenantID: store.MasterTenantID}
		}
		if err != nil {
			slog.Warn("security.http_pairing_check_failed", "sender_id", senderID, "error", err)
		} else {
			slog.Warn("security.http_pairing_auth_failed", "sender_id", senderID, "ip", r.RemoteAddr)
		}
	}
	// No auth configured → admin (no token = dev/single-user mode, full access)
	if gatewayToken == "" {
		return authResult{Role: permissions.RoleAdmin, Authenticated: true, TenantID: store.MasterTenantID}
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
		userID := extractUserID(r)
		// If the API key has a bound owner, force user_id to owner regardless of header.
		if auth.KeyData != nil && auth.KeyData.OwnerID != "" {
			if userID != "" && userID != auth.KeyData.OwnerID {
				slog.Warn("security.api_key_owner_override",
					"header_user_id", userID,
					"owner_id", auth.KeyData.OwnerID,
				)
			}
			userID = auth.KeyData.OwnerID
		}
		if userID != "" {
			ctx = store.WithUserID(ctx, userID)
		}
		if auth.CrossTenant && auth.TenantID != uuid.Nil {
			// Cross-tenant admin with tenant scope: filter data by chosen tenant
			ctx = store.WithTenantID(ctx, auth.TenantID)
			slog.Debug("security.http_auth_resolved",
				"path", r.URL.Path,
				"role", string(auth.Role),
				"tenant_scope", auth.TenantID.String(),
			)
		} else if auth.CrossTenant {
			// Auto-scope to MasterTenantID so all operations use a concrete tenant.
			ctx = store.WithTenantID(ctx, store.MasterTenantID)
			slog.Debug("security.http_auth_resolved",
				"path", r.URL.Path,
				"role", string(auth.Role),
				"cross_tenant", true,
				"auto_scope", store.MasterTenantID.String(),
			)
		} else if auth.TenantID != uuid.Nil {
			ctx = store.WithTenantID(ctx, auth.TenantID)
			slog.Debug("security.http_auth_resolved",
				"path", r.URL.Path,
				"role", string(auth.Role),
				"tenant_id", auth.TenantID.String(),
			)
		} else {
			ctx = store.WithTenantID(ctx, store.MasterTenantID)
			slog.Debug("security.http_auth_resolved",
				"path", r.URL.Path,
				"role", string(auth.Role),
				"tenant_id", store.MasterTenantID.String(),
			)
		}
		next(w, r.WithContext(ctx))
	}
}

// requireAuthBearer is like requireAuth but accepts a pre-extracted bearer token.
// Used by handlers that accept tokens from query params (files, media).
// Returns the authenticated request with user context applied (owner_id enforcement included).
func requireAuthBearer(token string, minRole permissions.Role, bearer string, w http.ResponseWriter, r *http.Request) (*http.Request, bool) {
	locale := extractLocale(r)
	auth := resolveAuthBearer(r, token, bearer)

	if !auth.Authenticated {
		writeJSON(w, http.StatusUnauthorized, map[string]string{
			"error": i18n.T(locale, i18n.MsgUnauthorized),
		})
		return r, false
	}

	required := minRole
	if required == "" {
		required = httpMinRole(r.Method)
	}

	if !permissions.HasMinRole(auth.Role, required) {
		writeJSON(w, http.StatusForbidden, map[string]string{
			"error": i18n.T(locale, i18n.MsgPermissionDenied, r.URL.Path),
		})
		return r, false
	}

	// Apply user context + owner_id enforcement (same as requireAuth).
	ctx := store.WithLocale(r.Context(), locale)
	userID := extractUserID(r)
	if auth.KeyData != nil && auth.KeyData.OwnerID != "" {
		if userID != "" && userID != auth.KeyData.OwnerID {
			slog.Warn("security.api_key_owner_override",
				"header_user_id", userID,
				"owner_id", auth.KeyData.OwnerID,
				"path", r.URL.Path,
			)
		}
		userID = auth.KeyData.OwnerID
	}
	if userID != "" {
		ctx = store.WithUserID(ctx, userID)
	}
	if auth.CrossTenant {
		ctx = store.WithTenantID(ctx, store.MasterTenantID)
	} else if auth.TenantID != uuid.Nil {
		ctx = store.WithTenantID(ctx, auth.TenantID)
	} else {
		ctx = store.WithTenantID(ctx, store.MasterTenantID)
	}
	return r.WithContext(ctx), true
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
