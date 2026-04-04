package http

import (
	"context"
	"crypto/subtle"
	"log/slog"
	"net/http"
	"slices"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/nextlevelbuilder/goclaw/internal/bus"
	"github.com/nextlevelbuilder/goclaw/internal/crypto"
	"github.com/nextlevelbuilder/goclaw/internal/i18n"
	"github.com/nextlevelbuilder/goclaw/internal/permissions"
	"github.com/nextlevelbuilder/goclaw/internal/store"
	"github.com/nextlevelbuilder/goclaw/pkg/protocol"
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

var pkgGatewayToken string
var pkgAPIKeyCache *apiKeyCache
var pkgPairingStore store.PairingStore
var pkgTenantCache *tenantCache
var pkgOwnerIDs []string

// InitGatewayToken sets the gateway bearer token for HTTP auth.
// Must be called once during server startup before handling requests.
func InitGatewayToken(token string) {
	pkgGatewayToken = token
}

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
// Owners get RoleOwner with gateway token; others get RoleAdmin scoped to their tenant.
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
	return slices.Contains(ownerIDs, userID)
}

// InitTenantStore sets the tenant cache for HTTP auth with TTL and pubsub invalidation.
// Tenant scoping is used by owner/system-key callers and for membership validation.
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
	TenantID      uuid.UUID         // resolved tenant; always concrete after resolution
	TenantSlug    string            // resolved tenant slug for filesystem paths
}

// resolveAuth determines the caller's role from the request.
// Priority: gateway token → API key → no-auth fallback.
func resolveAuth(r *http.Request) authResult {
	return resolveAuthWithBearer(r, extractBearerToken(r))
}

// resolveAuthWithBearer is like resolveAuth but accepts a pre-extracted bearer token.
// Useful for handlers that also accept tokens from query params.
func resolveAuthWithBearer(r *http.Request, bearer string) authResult {
	// Gateway token → admin.
	// Only configured owner IDs get unrestricted tenant scoping; other callers may
	// only narrow to tenants where the supplied user already has membership.
	if pkgGatewayToken != "" && tokenMatch(bearer, pkgGatewayToken) {
		userID := extractUserID(r)
		isOwner := isHTTPOwnerID(userID, pkgOwnerIDs)
		role := permissions.RoleAdmin
		if isOwner {
			role = permissions.RoleOwner
		}
		res := authResult{Role: role, Authenticated: true}
		tenantVal := r.Header.Get("X-GoClaw-Tenant-Id")
		if isOwner {
			res.TenantID = resolveScopedTenant(r.Context(), tenantVal)
		} else {
			tenantID, allowed := resolveTenantHint(r.Context(), tenantVal, userID)
			if !allowed {
				return authResult{}
			}
			res.TenantID = tenantID
		}
		if res.TenantID == uuid.Nil {
			res.TenantID = store.MasterTenantID
		}
		res.TenantSlug = resolveTenantSlug(r.Context(), res.TenantID)
		return res
	}
	// API key → role from scopes
	if keyData, role := ResolveAPIKey(r.Context(), bearer); role != "" {
		res := authResult{Role: role, Authenticated: true, KeyData: keyData}
		if keyData.TenantID == uuid.Nil {
			// System-level API keys keep their scope-derived role. They may
			// optionally scope a request to a tenant, but they do not become owner.
			res.TenantID = resolveScopedTenant(r.Context(), r.Header.Get("X-GoClaw-Tenant-Id"))
			if res.TenantID == uuid.Nil {
				res.TenantID = store.MasterTenantID
			}
			res.TenantSlug = resolveTenantSlug(r.Context(), res.TenantID)
		} else {
			res.TenantID = keyData.TenantID
			res.TenantSlug = resolveTenantSlug(r.Context(), keyData.TenantID)
		}
		return res
	}
	// Browser pairing → operator (via X-GoClaw-Sender-Id header)
	if senderID := r.Header.Get("X-GoClaw-Sender-Id"); senderID != "" && pkgPairingStore != nil {
		paired, err := pkgPairingStore.IsPaired(r.Context(), senderID, "browser")
		if err == nil && paired {
			tenantID, allowed := resolveTenantHint(r.Context(), r.Header.Get("X-GoClaw-Tenant-Id"), extractUserID(r))
			if !allowed {
				return authResult{}
			}
			return authResult{
				Role:          permissions.RoleOperator,
				Authenticated: true,
				TenantID:      tenantID,
				TenantSlug:    resolveTenantSlug(r.Context(), tenantID),
			}
		}
		if err != nil {
			slog.Warn("security.http_pairing_check_failed", "sender_id", senderID, "error", err)
		} else {
			slog.Warn("security.http_pairing_auth_failed", "sender_id", senderID, "ip", r.RemoteAddr)
		}
	}
	// No auth configured → admin (no token = dev/single-user mode, full access)
	if pkgGatewayToken == "" {
		return authResult{Role: permissions.RoleAdmin, Authenticated: true, TenantID: store.MasterTenantID}
	}
	return authResult{}
}

func resolveScopedTenant(ctx context.Context, tenantVal string) uuid.UUID {
	if tenantVal == "" || pkgTenantCache == nil {
		return uuid.Nil
	}
	if tid, err := uuid.Parse(tenantVal); err == nil {
		if t, err := pkgTenantCache.GetTenant(ctx, tid); err == nil && t != nil {
			return t.ID
		}
	} else if t, err := pkgTenantCache.GetTenantBySlug(ctx, tenantVal); err == nil && t != nil {
		return t.ID
	}
	slog.Debug("security.http_tenant_scope_unresolved", "tenant", tenantVal)
	return uuid.Nil
}

func resolveTenantSlug(ctx context.Context, tenantID uuid.UUID) string {
	if tenantID == uuid.Nil || pkgTenantCache == nil {
		return ""
	}
	if tenant, err := pkgTenantCache.GetTenant(ctx, tenantID); err == nil && tenant != nil {
		return tenant.Slug
	}
	return ""
}

func resolveTenantHint(ctx context.Context, hint, userID string) (uuid.UUID, bool) {
	if hint == "" || pkgTenantCache == nil {
		return store.MasterTenantID, true
	}
	tid := resolveScopedTenant(ctx, hint)
	if tid == uuid.Nil {
		return store.MasterTenantID, true
	}
	if userID == "" {
		slog.Warn("security.http_tenant_hint_denied_anonymous", "hint", hint, "tenant_id", tid)
		return uuid.Nil, false
	}
	role, err := pkgTenantCache.store.GetUserRole(ctx, tid, userID)
	if err != nil {
		slog.Warn("security.http_tenant_access_revoked",
			"hint", hint,
			"user", userID,
			"tenant_id", tid,
			"error", err,
			"code", protocol.ErrTenantAccessRevoked,
		)
		return uuid.Nil, false
	}
	if role == "" {
		slog.Warn("security.http_tenant_no_membership",
			"hint", hint,
			"user", userID,
			"tenant_id", tid,
			"code", protocol.ErrTenantAccessRevoked,
		)
		return uuid.Nil, false
	}
	return tid, true
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

// enrichContext injects locale, role, userID, and tenantID from authResult into ctx.
// Used by requireAuth middleware and ServeHTTP handlers that do their own auth checks.
func enrichContext(ctx context.Context, r *http.Request, auth authResult) context.Context {
	ctx = store.WithLocale(ctx, extractLocale(r))
	ctx = store.WithRole(ctx, string(auth.Role))
	userID := extractUserID(r)
	// Security: In dev mode (no gateway token configured), do not trust the
	// X-GoClaw-User-Id header — force "system" to prevent identity spoofing.
	if pkgGatewayToken == "" && auth.KeyData == nil && userID != "" {
		slog.Warn("security.user_id_header_ignored_no_auth",
			"attempted_user_id", userID,
			"ip", r.RemoteAddr,
		)
		userID = "system"
	}
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
	tenantID := auth.TenantID
	if tenantID == uuid.Nil {
		tenantID = store.MasterTenantID
	}
	ctx = store.WithTenantID(ctx, tenantID)
	if auth.TenantSlug != "" {
		ctx = store.WithTenantSlug(ctx, auth.TenantSlug)
	}
	slog.Debug("security.http_auth_resolved",
		"path", r.URL.Path,
		"role", string(auth.Role),
		"tenant_id", tenantID.String(),
	)
	return ctx
}

// requireAuth is a middleware that checks authentication and minimum role.
// Pass "" for minRole to auto-detect from HTTP method (GET→Viewer, POST→Operator).
// Injects locale, role, userID and tenantID into request context.
func requireAuth(minRole permissions.Role, next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		locale := extractLocale(r)
		auth := resolveAuth(r)

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
				"error": i18n.T(locale, i18n.MsgPermissionDenied, r.URL.Path+" requires "+string(required)+" role"),
			})
			return
		}

		ctx := enrichContext(r.Context(), r, auth)
		next(w, r.WithContext(ctx))
	}
}

// requireAuthBearer is like requireAuth but accepts a pre-extracted bearer token.
// Used by handlers that accept tokens from query params (files, media).
// Returns the authenticated request with user context applied (owner_id enforcement included).
func requireAuthBearer(minRole permissions.Role, bearer string, w http.ResponseWriter, r *http.Request) (*http.Request, bool) {
	locale := extractLocale(r)
	auth := resolveAuthWithBearer(r, bearer)

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
			"error": i18n.T(locale, i18n.MsgPermissionDenied, r.URL.Path+" requires "+string(required)+" role"),
		})
		return r, false
	}

	ctx := enrichContext(r.Context(), r, auth)
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
