package http

import (
	"database/sql"
	"errors"
	"log/slog"
	"net/http"
	"strings"

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
		if h.tenants == nil {
			writeError(w, http.StatusInternalServerError, protocol.ErrInternal, "tenant store unavailable")
			return uuid.Nil, "", false
		}
		tenant, err := h.tenants.GetTenant(r.Context(), id)
		if err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				writeError(w, http.StatusNotFound, protocol.ErrNotFound,
					i18n.T(locale, i18n.MsgNotFound, "tenant", raw))
				return uuid.Nil, "", false
			}
			slog.Error("tenant.resolve_by_id_failed", "tenant_id", raw, "error", err)
			writeError(w, http.StatusInternalServerError, protocol.ErrInternal,
				i18n.T(locale, i18n.MsgInternalError, "tenant lookup failed"))
			return uuid.Nil, "", false
		}
		return tenant.ID, tenant.Slug, true
	}

	if slug := q.Get("tenant_slug"); slug != "" {
		if h.tenants == nil {
			writeError(w, http.StatusInternalServerError, protocol.ErrInternal, "tenant store unavailable")
			return uuid.Nil, "", false
		}
		tenant, err := h.tenants.GetTenantBySlug(r.Context(), slug)
		if err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				writeError(w, http.StatusNotFound, protocol.ErrNotFound,
					i18n.T(locale, i18n.MsgNotFound, "tenant", slug))
				return uuid.Nil, "", false
			}
			slog.Error("tenant.resolve_by_slug_failed", "tenant_slug", slug, "error", err)
			writeError(w, http.StatusInternalServerError, protocol.ErrInternal,
				i18n.T(locale, i18n.MsgInternalError, "tenant lookup failed"))
			return uuid.Nil, "", false
		}
		return tenant.ID, tenant.Slug, true
	}

	writeError(w, http.StatusBadRequest, protocol.ErrInvalidRequest,
		i18n.T(locale, i18n.MsgRequired, "tenant_id or tenant_slug"))
	return uuid.Nil, "", false
}

// resolveRestoreTarget resolves the restore target for the requested mode.
// New mode uses a slug-only target (the new tenant's slug); other modes resolve an existing tenant.
// tenant_id is rejected for mode=new because the new tenant's UUID is generated server-side.
func (h *TenantBackupHandler) resolveRestoreTarget(w http.ResponseWriter, r *http.Request, mode string) (uuid.UUID, string, bool) {
	if mode == "new" {
		q := r.URL.Query()
		slug := strings.TrimSpace(q.Get("tenant_slug"))
		tid := strings.TrimSpace(q.Get("tenant_id"))
		locale := extractLocale(r)

		// mode=new generates a fresh tenant UUID server-side; any caller-supplied
		// tenant_id is rejected regardless of whether tenant_slug is also present,
		// matching the CLI contract and preventing ambiguous "which one wins" semantics.
		if tid != "" {
			writeError(w, http.StatusBadRequest, protocol.ErrInvalidRequest,
				i18n.T(locale, i18n.MsgRestoreNewModeRejectsTenantID))
			return uuid.Nil, "", false
		}
		if slug == "" {
			writeError(w, http.StatusBadRequest, protocol.ErrInvalidRequest,
				i18n.T(locale, i18n.MsgRequired, "tenant_slug"))
			return uuid.Nil, "", false
		}
		return uuid.Nil, slug, true
	}

	return h.resolveTenant(w, r)
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
