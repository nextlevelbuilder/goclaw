package http

import (
	"net/http"

	"github.com/google/uuid"

	"github.com/nextlevelbuilder/goclaw/internal/i18n"
	"github.com/nextlevelbuilder/goclaw/internal/store"
)

// requireTenantAdmin verifies the caller has owner or admin role within the
// specified tenant. System-wide owners (IsOwnerRole) bypass the check.
// Returns true if authorized, false if an error response was written.
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
