package http

import (
	"net/http"

	"github.com/google/uuid"

	"github.com/nextlevelbuilder/goclaw/internal/i18n"
	"github.com/nextlevelbuilder/goclaw/internal/store"
	"github.com/nextlevelbuilder/goclaw/pkg/protocol"
)

// resolveTenant resolves tenant_id or tenant_slug from request query params.
// Writes an error response and returns (uuid.Nil, "", false) on failure.
func (h *TenantBackupHandler) resolveTenant(w http.ResponseWriter, r *http.Request) (uuid.UUID, string, bool) {
	locale := extractLocale(r)
	q := r.URL.Query()

	if raw := q.Get("tenant_id"); raw != "" {
		id, err := uuid.Parse(raw)
		if err != nil {
			writeError(w, http.StatusBadRequest, protocol.ErrInvalidRequest,
				i18n.T(locale, i18n.MsgInvalidRequest, "tenant_id"))
			return uuid.Nil, "", false
		}
		return id, q.Get("tenant_slug"), true
	}

	if slug := q.Get("tenant_slug"); slug != "" {
		if h.tenants == nil {
			writeError(w, http.StatusInternalServerError, protocol.ErrInternal, "tenant store unavailable")
			return uuid.Nil, "", false
		}
		tenant, err := h.tenants.GetTenantBySlug(r.Context(), slug)
		if err != nil {
			writeError(w, http.StatusNotFound, protocol.ErrNotFound,
				i18n.T(locale, i18n.MsgNotFound, "tenant", slug))
			return uuid.Nil, "", false
		}
		return tenant.ID, tenant.Slug, true
	}

	writeError(w, http.StatusBadRequest, protocol.ErrInvalidRequest,
		i18n.T(locale, i18n.MsgRequired, "tenant_id or tenant_slug"))
	return uuid.Nil, "", false
}

// authorised returns true if the user is the system owner or a tenant admin/owner.
func (h *TenantBackupHandler) authorised(r *http.Request, userID string, tenantID uuid.UUID) bool {
	if h.isOwnerUser(userID) {
		return true
	}
	if h.tenants == nil {
		return false
	}
	role, err := h.tenants.GetUserRole(r.Context(), tenantID, userID)
	if err != nil {
		return false
	}
	return role == store.TenantRoleOwner || role == store.TenantRoleAdmin
}

// isOwnerUser returns true if userID is a configured system owner.
func (h *TenantBackupHandler) isOwnerUser(userID string) bool {
	return userID != "" && h.isOwner != nil && h.isOwner(userID)
}
