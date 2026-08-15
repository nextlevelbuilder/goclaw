package http

import (
	"database/sql"
	"encoding/json"
	"errors"
	"net/http"
	"strconv"

	"github.com/google/uuid"

	"github.com/nextlevelbuilder/goclaw/internal/i18n"
	"github.com/nextlevelbuilder/goclaw/internal/permissions"
	"github.com/nextlevelbuilder/goclaw/internal/store"
	"github.com/nextlevelbuilder/goclaw/internal/workstation"
	"github.com/nextlevelbuilder/goclaw/pkg/protocol"
)

// WorkstationsHandler handles HTTP CRUD for workstations.
// Routes are only registered when edition is Standard — callers MUST gate.
type WorkstationsHandler struct {
	wsStore       store.WorkstationStore
	linkStore     store.AgentWorkstationLinkStore
	tenantStore   store.TenantStore
	permStore     store.WorkstationPermissionStore     // Phase 6; may be nil
	activityStore store.WorkstationActivityStore       // Phase 7; may be nil
}

// NewWorkstationsHandler creates a WorkstationsHandler.
func NewWorkstationsHandler(
	wsStore store.WorkstationStore,
	linkStore store.AgentWorkstationLinkStore,
	tenantStore store.TenantStore,
) *WorkstationsHandler {
	return &WorkstationsHandler{wsStore: wsStore, linkStore: linkStore, tenantStore: tenantStore}
}

// SetPermStore wires the permission store for allowlist CRUD endpoints.
func (h *WorkstationsHandler) SetPermStore(ps store.WorkstationPermissionStore) {
	h.permStore = ps
}

// SetActivityStore wires the activity store for audit log endpoints (Phase 7).
func (h *WorkstationsHandler) SetActivityStore(as store.WorkstationActivityStore) {
	h.activityStore = as
}

// RegisterRoutes registers all workstation endpoints onto mux.
// MUST only be called after edition gate check — never in Lite builds.
func (h *WorkstationsHandler) RegisterRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /v1/workstations", h.auth(h.handleList))
	mux.HandleFunc("POST /v1/workstations", h.auth(h.handleCreate))
	mux.HandleFunc("GET /v1/workstations/{id}", h.auth(h.handleGet))
	mux.HandleFunc("PUT /v1/workstations/{id}", h.auth(h.handleUpdate))
	mux.HandleFunc("DELETE /v1/workstations/{id}", h.auth(h.handleDelete))
	mux.HandleFunc("POST /v1/workstations/{id}/test", h.auth(h.handleTest))
	// Phase 6: permission allowlist CRUD
	mux.HandleFunc("GET /v1/workstations/{id}/permissions", h.auth(h.handlePermList))
	mux.HandleFunc("POST /v1/workstations/{id}/permissions", h.auth(h.handlePermAdd))
	mux.HandleFunc("DELETE /v1/workstations/{id}/permissions/{permId}", h.auth(h.handlePermRemove))
	mux.HandleFunc("PUT /v1/workstations/{id}/permissions/{permId}/toggle", h.auth(h.handlePermToggle))
	// Phase 7: activity audit log
	mux.HandleFunc("GET /v1/workstations/{id}/activity", h.auth(h.handleActivityList))
}

func (h *WorkstationsHandler) auth(next http.HandlerFunc) http.HandlerFunc {
	return requireAuth(permissions.RoleAdmin, next)
}

func (h *WorkstationsHandler) handleList(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	locale := store.LocaleFromContext(ctx)
	if !requireTenantAdmin(w, r, h.tenantStore) {
		return
	}
	wss, err := h.wsStore.List(ctx)
	if err != nil {
		writeError(w, http.StatusInternalServerError, protocol.ErrInternal,
			i18n.T(locale, i18n.MsgFailedToList, "workstations"))
		return
	}
	views := make([]*store.SanitizedWorkstation, len(wss))
	for i := range wss {
		views[i] = wss[i].SanitizedView()
	}
	writeJSON(w, http.StatusOK, map[string]any{"workstations": views})
}

func (h *WorkstationsHandler) handleGet(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	locale := store.LocaleFromContext(ctx)
	if !requireTenantAdmin(w, r, h.tenantStore) {
		return
	}
	idStr := r.PathValue("id")
	id, err := uuid.Parse(idStr)
	if err != nil {
		writeError(w, http.StatusBadRequest, protocol.ErrInvalidRequest,
			i18n.T(locale, i18n.MsgInvalidID, "workstation"))
		return
	}
	ws, err := h.wsStore.GetByID(ctx, id)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			writeError(w, http.StatusNotFound, protocol.ErrNotFound,
				i18n.T(locale, i18n.MsgWorkstationNotFound, idStr))
			return
		}
		writeError(w, http.StatusInternalServerError, protocol.ErrInternal,
			i18n.T(locale, i18n.MsgInternalError, err.Error()))
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"workstation": ws.SanitizedView()})
}

func (h *WorkstationsHandler) handleCreate(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	locale := store.LocaleFromContext(ctx)
	if !requireTenantAdmin(w, r, h.tenantStore) {
		return
	}

	var body struct {
		WorkstationKey string                   `json:"workstationKey"`
		Name           string                   `json:"name"`
		BackendType    store.WorkstationBackend `json:"backendType"`
		Metadata       json.RawMessage          `json:"metadata"`
		DefaultCWD     string                   `json:"defaultCwd"`
		DefaultEnv     json.RawMessage          `json:"defaultEnv"`
	}
	if !bindJSON(w, r, locale, &body) {
		return
	}

	if body.WorkstationKey == "" {
		writeError(w, http.StatusBadRequest, protocol.ErrInvalidRequest,
			i18n.T(locale, i18n.MsgRequired, "workstationKey"))
		return
	}
	if !workstation.ValidateWorkstationKey(body.WorkstationKey) {
		writeError(w, http.StatusBadRequest, protocol.ErrInvalidRequest,
			i18n.T(locale, i18n.MsgInvalidSlug, "workstationKey"))
		return
	}
	if !workstation.ValidateBackend(body.BackendType) {
		writeError(w, http.StatusBadRequest, protocol.ErrInvalidRequest,
			i18n.T(locale, i18n.MsgInvalidBackend, string(body.BackendType)))
		return
	}
	metaBytes := []byte(body.Metadata)
	if err := store.ValidateMetadata(body.BackendType, metaBytes); err != nil {
		writeError(w, http.StatusBadRequest, protocol.ErrInvalidRequest,
			i18n.T(locale, i18n.MsgInvalidMetadataShape, string(body.BackendType), err.Error()))
		return
	}
	envBytes := []byte(body.DefaultEnv)
	if len(envBytes) == 0 {
		envBytes = []byte("{}")
	}

	userID := store.UserIDFromContext(ctx)
	ws := &store.Workstation{
		WorkstationKey: body.WorkstationKey,
		Name:           body.Name,
		BackendType:    body.BackendType,
		Metadata:       metaBytes,
		DefaultCWD:     body.DefaultCWD,
		DefaultEnv:     envBytes,
		Active:         true,
		CreatedBy:      userID,
	}
	if err := h.wsStore.Create(ctx, ws); err != nil {
		writeError(w, http.StatusInternalServerError, protocol.ErrInternal,
			i18n.T(locale, i18n.MsgFailedToCreate, "workstation", err.Error()))
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{"workstation": ws.SanitizedView()})
}

func (h *WorkstationsHandler) handleUpdate(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	locale := store.LocaleFromContext(ctx)
	if !requireTenantAdmin(w, r, h.tenantStore) {
		return
	}
	idStr := r.PathValue("id")
	id, err := uuid.Parse(idStr)
	if err != nil {
		writeError(w, http.StatusBadRequest, protocol.ErrInvalidRequest,
			i18n.T(locale, i18n.MsgInvalidID, "workstation"))
		return
	}
	var updates map[string]any
	if !bindJSON(w, r, locale, &updates) {
		return
	}
	if len(updates) == 0 {
		writeError(w, http.StatusBadRequest, protocol.ErrInvalidRequest,
			i18n.T(locale, i18n.MsgNoUpdatesProvided))
		return
	}
	// I2 fix: validate metadata shape when metadata is being updated.
	// Fetch current workstation to obtain backend_type for validation.
	if _, hasMetadata := updates["metadata"]; hasMetadata {
		current, err := h.wsStore.GetByID(ctx, id)
		if err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				writeError(w, http.StatusNotFound, protocol.ErrNotFound,
					i18n.T(locale, i18n.MsgWorkstationNotFound, idStr))
				return
			}
			writeError(w, http.StatusInternalServerError, protocol.ErrInternal,
				i18n.T(locale, i18n.MsgInternalError, err.Error()))
			return
		}
		metaBytes, err := json.Marshal(updates["metadata"])
		if err != nil {
			writeError(w, http.StatusBadRequest, protocol.ErrInvalidRequest,
				i18n.T(locale, i18n.MsgInvalidMetadataShape, string(current.BackendType), err.Error()))
			return
		}
		if err := store.ValidateMetadata(current.BackendType, metaBytes); err != nil {
			writeError(w, http.StatusBadRequest, protocol.ErrInvalidRequest,
				i18n.T(locale, i18n.MsgInvalidMetadataShape, string(current.BackendType), err.Error()))
			return
		}
	}
	if err := h.wsStore.Update(ctx, id, updates); err != nil {
		writeError(w, http.StatusInternalServerError, protocol.ErrInternal,
			i18n.T(locale, i18n.MsgFailedToUpdate, "workstation", err.Error()))
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"id": id})
}

func (h *WorkstationsHandler) handleDelete(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	locale := store.LocaleFromContext(ctx)
	if !requireTenantAdmin(w, r, h.tenantStore) {
		return
	}
	idStr := r.PathValue("id")
	id, err := uuid.Parse(idStr)
	if err != nil {
		writeError(w, http.StatusBadRequest, protocol.ErrInvalidRequest,
			i18n.T(locale, i18n.MsgInvalidID, "workstation"))
		return
	}
	if err := h.wsStore.Delete(ctx, id); err != nil {
		writeError(w, http.StatusInternalServerError, protocol.ErrInternal,
			i18n.T(locale, i18n.MsgFailedToDelete, "workstation", err.Error()))
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"id": id})
}

// handleTest is a stub — real implementation in Phase 2/3.
func (h *WorkstationsHandler) handleTest(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	locale := store.LocaleFromContext(ctx)
	if !requireTenantAdmin(w, r, h.tenantStore) {
		return
	}
	writeError(w, http.StatusNotImplemented, protocol.ErrNotImplemented,
		i18n.T(locale, i18n.MsgNotImplemented, "workstations.testConnection"))
}

// --- Phase 6: workstation permission allowlist CRUD ---

func (h *WorkstationsHandler) requirePermStore(w http.ResponseWriter, locale string) bool {
	if h.permStore == nil {
		writeError(w, http.StatusNotImplemented, protocol.ErrNotImplemented,
			i18n.T(locale, i18n.MsgNotImplemented, "workstations permissions"))
		return false
	}
	return true
}

func (h *WorkstationsHandler) handlePermList(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	locale := store.LocaleFromContext(ctx)
	if !requireTenantAdmin(w, r, h.tenantStore) || !h.requirePermStore(w, locale) {
		return
	}
	wsID, err := uuid.Parse(r.PathValue("id"))
	if err != nil {
		writeError(w, http.StatusBadRequest, protocol.ErrInvalidRequest,
			i18n.T(locale, i18n.MsgInvalidID, "workstation"))
		return
	}
	// Ownership check: verify workstation belongs to caller's tenant before listing perms.
	// GetByID scopes the query by tenant_id — returns ErrNoRows for a different tenant.
	if _, err := h.wsStore.GetByID(ctx, wsID); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			writeError(w, http.StatusNotFound, protocol.ErrNotFound,
				i18n.T(locale, i18n.MsgWorkstationNotFound, wsID.String()))
			return
		}
		writeError(w, http.StatusInternalServerError, protocol.ErrInternal,
			i18n.T(locale, i18n.MsgInternalError, err.Error()))
		return
	}
	perms, err := h.permStore.ListForWorkstation(ctx, wsID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, protocol.ErrInternal,
			i18n.T(locale, i18n.MsgFailedToList, "permissions"))
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"permissions": perms})
}

func (h *WorkstationsHandler) handlePermAdd(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	locale := store.LocaleFromContext(ctx)
	if !requireTenantAdmin(w, r, h.tenantStore) || !h.requirePermStore(w, locale) {
		return
	}
	wsID, err := uuid.Parse(r.PathValue("id"))
	if err != nil {
		writeError(w, http.StatusBadRequest, protocol.ErrInvalidRequest,
			i18n.T(locale, i18n.MsgInvalidID, "workstation"))
		return
	}
	// I5 fix: verify workstation belongs to caller's tenant before adding permission.
	// GetByID scopes the query by tenant_id in the WHERE clause — returns ErrNoRows if
	// the workstation exists in a different tenant.
	if _, err := h.wsStore.GetByID(ctx, wsID); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			writeError(w, http.StatusNotFound, protocol.ErrNotFound,
				i18n.T(locale, i18n.MsgWorkstationNotFound, wsID.String()))
			return
		}
		writeError(w, http.StatusInternalServerError, protocol.ErrInternal,
			i18n.T(locale, i18n.MsgInternalError, err.Error()))
		return
	}
	var body struct {
		Pattern string `json:"pattern"`
	}
	if !bindJSON(w, r, locale, &body) {
		return
	}
	if body.Pattern == "" {
		writeError(w, http.StatusBadRequest, protocol.ErrInvalidRequest,
			i18n.T(locale, i18n.MsgRequired, "pattern"))
		return
	}
	userID := store.UserIDFromContext(ctx)
	perm := &store.WorkstationPermission{
		WorkstationID: wsID,
		Pattern:       body.Pattern,
		Enabled:       true,
		CreatedBy:     userID,
	}
	if err := h.permStore.Add(ctx, perm); err != nil {
		writeError(w, http.StatusInternalServerError, protocol.ErrInternal,
			i18n.T(locale, i18n.MsgFailedToCreate, "permission", err.Error()))
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{"permission": perm})
}

func (h *WorkstationsHandler) handlePermRemove(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	locale := store.LocaleFromContext(ctx)
	if !requireTenantAdmin(w, r, h.tenantStore) || !h.requirePermStore(w, locale) {
		return
	}
	permID, err := uuid.Parse(r.PathValue("permId"))
	if err != nil {
		writeError(w, http.StatusBadRequest, protocol.ErrInvalidRequest,
			i18n.T(locale, i18n.MsgInvalidID, "permission"))
		return
	}
	if err := h.permStore.Remove(ctx, permID); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			writeError(w, http.StatusNotFound, protocol.ErrNotFound,
				i18n.T(locale, i18n.MsgWorkstationPermNotFound, permID.String()))
			return
		}
		writeError(w, http.StatusInternalServerError, protocol.ErrInternal,
			i18n.T(locale, i18n.MsgFailedToDelete, "permission", err.Error()))
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"id": permID})
}

func (h *WorkstationsHandler) handlePermToggle(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	locale := store.LocaleFromContext(ctx)
	if !requireTenantAdmin(w, r, h.tenantStore) || !h.requirePermStore(w, locale) {
		return
	}
	permID, err := uuid.Parse(r.PathValue("permId"))
	if err != nil {
		writeError(w, http.StatusBadRequest, protocol.ErrInvalidRequest,
			i18n.T(locale, i18n.MsgInvalidID, "permission"))
		return
	}
	var body struct {
		Enabled bool `json:"enabled"`
	}
	if !bindJSON(w, r, locale, &body) {
		return
	}
	if err := h.permStore.SetEnabled(ctx, permID, body.Enabled); err != nil {
		writeError(w, http.StatusInternalServerError, protocol.ErrInternal,
			i18n.T(locale, i18n.MsgFailedToUpdate, "permission", err.Error()))
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"id": permID, "enabled": body.Enabled})
}

// --- Phase 7: workstation activity audit log ---

func (h *WorkstationsHandler) handleActivityList(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	locale := store.LocaleFromContext(ctx)
	if !requireTenantAdmin(w, r, h.tenantStore) {
		return
	}
	if h.activityStore == nil {
		writeError(w, http.StatusNotImplemented, protocol.ErrNotImplemented,
			i18n.T(locale, i18n.MsgNotImplemented, "workstations activity"))
		return
	}
	wsID, err := uuid.Parse(r.PathValue("id"))
	if err != nil {
		writeError(w, http.StatusBadRequest, protocol.ErrInvalidRequest,
			i18n.T(locale, i18n.MsgInvalidID, "workstation"))
		return
	}

	// Ownership check: verify the workstation belongs to the caller's tenant.
	// GetByID scopes by tenant_id — returns ErrNoRows if workstation is in a different tenant.
	if _, err := h.wsStore.GetByID(ctx, wsID); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			writeError(w, http.StatusNotFound, protocol.ErrNotFound,
				i18n.T(locale, i18n.MsgWorkstationNotFound, wsID.String()))
			return
		}
		writeError(w, http.StatusInternalServerError, protocol.ErrInternal,
			i18n.T(locale, i18n.MsgInternalError, err.Error()))
		return
	}

	limit := 50
	if lStr := r.URL.Query().Get("limit"); lStr != "" {
		if l, err := strconv.Atoi(lStr); err == nil && l > 0 && l <= 200 {
			limit = l
		}
	}
	var cursor *uuid.UUID
	if cStr := r.URL.Query().Get("cursor"); cStr != "" {
		if cID, err := uuid.Parse(cStr); err == nil {
			cursor = &cID
		}
	}

	rows, nextCursor, err := h.activityStore.List(ctx, wsID, limit, cursor)
	if err != nil {
		writeError(w, http.StatusInternalServerError, protocol.ErrInternal,
			i18n.T(locale, i18n.MsgFailedToList, "activity"))
		return
	}

	resp := map[string]any{"activity": rows}
	if nextCursor != nil {
		resp["nextCursor"] = nextCursor.String()
	}
	writeJSON(w, http.StatusOK, resp)
}
