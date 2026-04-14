package http

import (
	"log/slog"
	"net/http"

	"github.com/google/uuid"

	"github.com/nextlevelbuilder/goclaw/internal/i18n"
	"github.com/nextlevelbuilder/goclaw/internal/store"
)

// requireTenantAdmin verifies the caller has owner or admin role within the
// specified tenant. System-wide owners (IsOwnerRole) bypass the check.
// Returns true if authorized, false if an error response was written.
//
// WARNING: the system-owner bypass does NOT guarantee a non-nil tenant ID
// downstream. A system owner can reach tenant-config handlers with
// tid == uuid.Nil. Handlers that branch on tenant ID (e.g. emitting
// tenant-scoped cache invalidate events) must therefore guard against
// uuid.Nil explicitly — don't rely on this helper to enforce it.
func requireTenantAdmin(w http.ResponseWriter, r *http.Request, ts store.TenantStore) bool {
	ctx := r.Context()

	// System-wide owner bypasses tenant membership check.
	if store.IsOwnerRole(ctx) {
		return true
	}

	if ts == nil {
		writeJSON(w, http.StatusNotImplemented, map[string]string{"error": "tenant store not available"})
		return false
	}

	tid := store.TenantIDFromContext(ctx)
	if tid == uuid.Nil {
		locale := store.LocaleFromContext(ctx)
		writeJSON(w, http.StatusBadRequest, map[string]string{
			"error": i18n.T(locale, i18n.MsgPermissionDenied, "tenant config"),
		})
		return false
	}

	userID := store.UserIDFromContext(ctx)
	// GetUserRole returns ("", nil) when the user has no membership in this tenant,
	// which correctly falls through to the role check below (denied).
	role, err := ts.GetUserRole(ctx, tid, userID)
	if err != nil || (role != store.TenantRoleOwner && role != store.TenantRoleAdmin) {
		locale := store.LocaleFromContext(ctx)
		writeJSON(w, http.StatusForbidden, map[string]string{
			"error": i18n.T(locale, i18n.MsgPermissionDenied, "tenant config"),
		})
		return false
	}
	return true
}

// requireMasterScope guards endpoints that write to global (non-tenant-scoped)
// tables or execute server-wide side effects (shell, filesystem). Rejects
// callers whose ctx is scoped to a non-master tenant even if they hold
// RoleAdmin in that tenant. System owners bypass.
//
// Symmetric counterpart to requireTenantAdmin: one guards tenant-scoped
// writes, the other guards global writes. Use on any PUT/POST/DELETE handler
// touching builtin_tools, disk config, package management, or similar global
// state. Shares the predicate with WS layer via store.IsMasterScope so rules
// cannot drift between transports.
//
// Returns true on allow, false on deny (in which case a 403 response has
// already been written). Emits security.tenant_scope_violation slog on deny.
func requireMasterScope(w http.ResponseWriter, r *http.Request) bool {
	ctx := r.Context()
	if store.IsMasterScope(ctx) {
		return true
	}
	slog.Warn("security.tenant_scope_violation",
		"path", r.URL.Path,
		"method", r.Method,
		"tenant_id", store.TenantIDFromContext(ctx),
		"user_id", store.UserIDFromContext(ctx),
	)
	locale := store.LocaleFromContext(ctx)
	writeJSON(w, http.StatusForbidden, map[string]string{
		"error": i18n.T(locale, i18n.MsgMasterScopeRequired),
	})
	return false
}
