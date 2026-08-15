package methods

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"

	"github.com/google/uuid"

	"github.com/nextlevelbuilder/goclaw/internal/gateway"
	"github.com/nextlevelbuilder/goclaw/internal/i18n"
	"github.com/nextlevelbuilder/goclaw/internal/permissions"
	"github.com/nextlevelbuilder/goclaw/internal/store"
	"github.com/nextlevelbuilder/goclaw/internal/workstation"
	"github.com/nextlevelbuilder/goclaw/pkg/protocol"
)

// WorkstationsMethods handles workstations.* RPC methods over WebSocket.
// Routes are only registered when !edition.IsLite() — callers must gate at registration.
type WorkstationsMethods struct {
	wsStore       store.WorkstationStore
	linkStore     store.AgentWorkstationLinkStore
	permStore     store.WorkstationPermissionStore     // may be nil if Phase 6 not wired
	activityStore store.WorkstationActivityStore       // may be nil if Phase 7 not wired
}

// NewWorkstationsMethods creates WorkstationsMethods with the given stores.
func NewWorkstationsMethods(wsStore store.WorkstationStore, linkStore store.AgentWorkstationLinkStore) *WorkstationsMethods {
	return &WorkstationsMethods{wsStore: wsStore, linkStore: linkStore}
}

// SetPermStore wires the permission store for allowlist CRUD methods.
func (m *WorkstationsMethods) SetPermStore(ps store.WorkstationPermissionStore) {
	m.permStore = ps
}

// SetActivityStore wires the activity store for audit log methods (Phase 7).
func (m *WorkstationsMethods) SetActivityStore(as store.WorkstationActivityStore) {
	m.activityStore = as
}

// Register wires the workstations.* methods onto the router.
// MUST only be called when edition is Standard (caller enforces the gate).
func (m *WorkstationsMethods) Register(router *gateway.MethodRouter) {
	router.Register(protocol.MethodWorkstationsList, m.adminOnly(m.handleList))
	router.Register(protocol.MethodWorkstationsGet, m.adminOnly(m.handleGet))
	router.Register(protocol.MethodWorkstationsCreate, m.adminOnly(m.handleCreate))
	router.Register(protocol.MethodWorkstationsUpdate, m.adminOnly(m.handleUpdate))
	router.Register(protocol.MethodWorkstationsDelete, m.adminOnly(m.handleDelete))
	router.Register(protocol.MethodWorkstationsTest, m.adminOnly(m.handleTestConnection))
	router.Register(protocol.MethodWorkstationsLinkAgent, m.adminOnly(m.handleLinkAgent))
	router.Register(protocol.MethodWorkstationsUnlinkAgent, m.adminOnly(m.handleUnlinkAgent))
	// Phase 6: permission allowlist CRUD
	router.Register(protocol.MethodWorkstationsPermList, m.adminOnly(m.handlePermList))
	router.Register(protocol.MethodWorkstationsPermAdd, m.adminOnly(m.handlePermAdd))
	router.Register(protocol.MethodWorkstationsPermRemove, m.adminOnly(m.handlePermRemove))
	router.Register(protocol.MethodWorkstationsPermToggle, m.adminOnly(m.handlePermToggle))
	// Phase 7: activity audit log
	router.Register(protocol.MethodWorkstationsListActivity, m.adminOnly(m.handleListActivity))
}

// adminOnly is a middleware that requires at least RoleAdmin on the WS client.
func (m *WorkstationsMethods) adminOnly(next gateway.MethodHandler) gateway.MethodHandler {
	return func(ctx context.Context, client *gateway.Client, req *protocol.RequestFrame) {
		if !permissions.HasMinRole(client.Role(), permissions.RoleAdmin) {
			locale := store.LocaleFromContext(ctx)
			client.SendResponse(protocol.NewErrorResponse(req.ID, protocol.ErrUnauthorized,
				i18n.T(locale, i18n.MsgPermissionDenied, req.Method)))
			return
		}
		next(ctx, client, req)
	}
}

func (m *WorkstationsMethods) handleList(ctx context.Context, client *gateway.Client, req *protocol.RequestFrame) {
	locale := store.LocaleFromContext(ctx)
	wss, err := m.wsStore.List(ctx)
	if err != nil {
		client.SendResponse(protocol.NewErrorResponse(req.ID, protocol.ErrInternal,
			i18n.T(locale, i18n.MsgFailedToList, "workstations")))
		return
	}
	views := make([]*store.SanitizedWorkstation, len(wss))
	for i := range wss {
		views[i] = wss[i].SanitizedView()
	}
	client.SendResponse(protocol.NewOKResponse(req.ID, map[string]any{"workstations": views}))
}

func (m *WorkstationsMethods) handleGet(ctx context.Context, client *gateway.Client, req *protocol.RequestFrame) {
	locale := store.LocaleFromContext(ctx)
	var params struct {
		ID string `json:"id"`
	}
	if req.Params != nil {
		if err := json.Unmarshal(req.Params, &params); err != nil {
			client.SendResponse(protocol.NewErrorResponse(req.ID, protocol.ErrInvalidRequest, "invalid params"))
			return
		}
	}
	id, err := uuid.Parse(params.ID)
	if err != nil {
		client.SendResponse(protocol.NewErrorResponse(req.ID, protocol.ErrInvalidRequest,
			i18n.T(locale, i18n.MsgInvalidID, "workstation")))
		return
	}
	ws, err := m.wsStore.GetByID(ctx, id)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			client.SendResponse(protocol.NewErrorResponse(req.ID, protocol.ErrNotFound,
				i18n.T(locale, i18n.MsgWorkstationNotFound, params.ID)))
			return
		}
		client.SendResponse(protocol.NewErrorResponse(req.ID, protocol.ErrInternal,
			i18n.T(locale, i18n.MsgInternalError, err.Error())))
		return
	}
	client.SendResponse(protocol.NewOKResponse(req.ID, map[string]any{"workstation": ws.SanitizedView()}))
}

func (m *WorkstationsMethods) handleCreate(ctx context.Context, client *gateway.Client, req *protocol.RequestFrame) {
	locale := store.LocaleFromContext(ctx)
	var params struct {
		WorkstationKey string                     `json:"workstationKey"`
		Name           string                     `json:"name"`
		BackendType    store.WorkstationBackend   `json:"backendType"`
		Metadata       json.RawMessage            `json:"metadata"`
		DefaultCWD     string                     `json:"defaultCwd"`
		DefaultEnv     json.RawMessage            `json:"defaultEnv"`
		CreatedBy      string                     `json:"createdBy"`
	}
	if req.Params != nil {
		if err := json.Unmarshal(req.Params, &params); err != nil {
			client.SendResponse(protocol.NewErrorResponse(req.ID, protocol.ErrInvalidRequest, "invalid params"))
			return
		}
	}

	if params.WorkstationKey == "" {
		client.SendResponse(protocol.NewErrorResponse(req.ID, protocol.ErrInvalidRequest,
			i18n.T(locale, i18n.MsgRequired, "workstationKey")))
		return
	}
	if !workstation.ValidateWorkstationKey(params.WorkstationKey) {
		client.SendResponse(protocol.NewErrorResponse(req.ID, protocol.ErrInvalidRequest,
			i18n.T(locale, i18n.MsgInvalidSlug, "workstationKey")))
		return
	}
	if !workstation.ValidateBackend(params.BackendType) {
		client.SendResponse(protocol.NewErrorResponse(req.ID, protocol.ErrInvalidRequest,
			i18n.T(locale, i18n.MsgInvalidBackend, string(params.BackendType))))
		return
	}
	metaBytes := []byte(params.Metadata)
	if err := store.ValidateMetadata(params.BackendType, metaBytes); err != nil {
		client.SendResponse(protocol.NewErrorResponse(req.ID, protocol.ErrInvalidRequest,
			i18n.T(locale, i18n.MsgInvalidMetadataShape, string(params.BackendType), err.Error())))
		return
	}
	envBytes := []byte(params.DefaultEnv)
	if len(envBytes) == 0 {
		envBytes = []byte("{}")
	}

	ws := &store.Workstation{
		WorkstationKey: params.WorkstationKey,
		Name:           params.Name,
		BackendType:    params.BackendType,
		Metadata:       metaBytes,
		DefaultCWD:     params.DefaultCWD,
		DefaultEnv:     envBytes,
		Active:         true,
		CreatedBy:      client.UserID(),
	}
	if err := m.wsStore.Create(ctx, ws); err != nil {
		client.SendResponse(protocol.NewErrorResponse(req.ID, protocol.ErrInvalidRequest,
			i18n.T(locale, i18n.MsgFailedToCreate, "workstation", err.Error())))
		return
	}
	client.SendResponse(protocol.NewOKResponse(req.ID, map[string]any{"workstation": ws.SanitizedView()}))
}

func (m *WorkstationsMethods) handleUpdate(ctx context.Context, client *gateway.Client, req *protocol.RequestFrame) {
	locale := store.LocaleFromContext(ctx)
	var params struct {
		ID      string         `json:"id"`
		Updates map[string]any `json:"updates"`
	}
	if req.Params != nil {
		if err := json.Unmarshal(req.Params, &params); err != nil {
			client.SendResponse(protocol.NewErrorResponse(req.ID, protocol.ErrInvalidRequest, "invalid params"))
			return
		}
	}
	id, err := uuid.Parse(params.ID)
	if err != nil {
		client.SendResponse(protocol.NewErrorResponse(req.ID, protocol.ErrInvalidRequest,
			i18n.T(locale, i18n.MsgInvalidID, "workstation")))
		return
	}
	if len(params.Updates) == 0 {
		client.SendResponse(protocol.NewErrorResponse(req.ID, protocol.ErrInvalidRequest,
			i18n.T(locale, i18n.MsgNoUpdatesProvided)))
		return
	}
	// I2 fix: validate metadata shape when metadata is being updated.
	// Fetch current workstation to obtain backend_type for validation.
	if _, hasMetadata := params.Updates["metadata"]; hasMetadata {
		current, err := m.wsStore.GetByID(ctx, id)
		if err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				client.SendResponse(protocol.NewErrorResponse(req.ID, protocol.ErrNotFound,
					i18n.T(locale, i18n.MsgWorkstationNotFound, params.ID)))
				return
			}
			client.SendResponse(protocol.NewErrorResponse(req.ID, protocol.ErrInternal,
				i18n.T(locale, i18n.MsgInternalError, err.Error())))
			return
		}
		metaBytes, err := json.Marshal(params.Updates["metadata"])
		if err != nil {
			client.SendResponse(protocol.NewErrorResponse(req.ID, protocol.ErrInvalidRequest,
				i18n.T(locale, i18n.MsgInvalidMetadataShape, string(current.BackendType), err.Error())))
			return
		}
		if err := store.ValidateMetadata(current.BackendType, metaBytes); err != nil {
			client.SendResponse(protocol.NewErrorResponse(req.ID, protocol.ErrInvalidRequest,
				i18n.T(locale, i18n.MsgInvalidMetadataShape, string(current.BackendType), err.Error())))
			return
		}
	}
	if err := m.wsStore.Update(ctx, id, params.Updates); err != nil {
		client.SendResponse(protocol.NewErrorResponse(req.ID, protocol.ErrInternal,
			i18n.T(locale, i18n.MsgFailedToUpdate, "workstation", err.Error())))
		return
	}
	client.SendResponse(protocol.NewOKResponse(req.ID, map[string]any{"id": id}))
}

func (m *WorkstationsMethods) handleDelete(ctx context.Context, client *gateway.Client, req *protocol.RequestFrame) {
	locale := store.LocaleFromContext(ctx)
	var params struct {
		ID string `json:"id"`
	}
	if req.Params != nil {
		if err := json.Unmarshal(req.Params, &params); err != nil {
			client.SendResponse(protocol.NewErrorResponse(req.ID, protocol.ErrInvalidRequest, "invalid params"))
			return
		}
	}
	id, err := uuid.Parse(params.ID)
	if err != nil {
		client.SendResponse(protocol.NewErrorResponse(req.ID, protocol.ErrInvalidRequest,
			i18n.T(locale, i18n.MsgInvalidID, "workstation")))
		return
	}
	if err := m.wsStore.Delete(ctx, id); err != nil {
		client.SendResponse(protocol.NewErrorResponse(req.ID, protocol.ErrInternal,
			i18n.T(locale, i18n.MsgFailedToDelete, "workstation", err.Error())))
		return
	}
	client.SendResponse(protocol.NewOKResponse(req.ID, map[string]any{"id": id}))
}

// handleTestConnection is a stub — real implementation in Phase 2/3.
func (m *WorkstationsMethods) handleTestConnection(ctx context.Context, client *gateway.Client, req *protocol.RequestFrame) {
	locale := store.LocaleFromContext(ctx)
	client.SendResponse(protocol.NewErrorResponse(req.ID, protocol.ErrNotImplemented,
		i18n.T(locale, i18n.MsgNotImplemented, "workstations.testConnection")))
}

func (m *WorkstationsMethods) handleLinkAgent(ctx context.Context, client *gateway.Client, req *protocol.RequestFrame) {
	locale := store.LocaleFromContext(ctx)
	var params struct {
		AgentID       string `json:"agentId"`
		WorkstationID string `json:"workstationId"`
		IsDefault     bool   `json:"isDefault"`
	}
	if req.Params != nil {
		if err := json.Unmarshal(req.Params, &params); err != nil {
			client.SendResponse(protocol.NewErrorResponse(req.ID, protocol.ErrInvalidRequest, "invalid params"))
			return
		}
	}
	agentID, err := uuid.Parse(params.AgentID)
	if err != nil {
		client.SendResponse(protocol.NewErrorResponse(req.ID, protocol.ErrInvalidRequest,
			i18n.T(locale, i18n.MsgInvalidID, "agent")))
		return
	}
	wsID, err := uuid.Parse(params.WorkstationID)
	if err != nil {
		client.SendResponse(protocol.NewErrorResponse(req.ID, protocol.ErrInvalidRequest,
			i18n.T(locale, i18n.MsgInvalidID, "workstation")))
		return
	}
	link := &store.AgentWorkstationLink{
		AgentID:       agentID,
		WorkstationID: wsID,
		IsDefault:     params.IsDefault,
	}
	if err := m.linkStore.Link(ctx, link); err != nil {
		client.SendResponse(protocol.NewErrorResponse(req.ID, protocol.ErrInternal,
			i18n.T(locale, i18n.MsgFailedToCreate, "agent_workstation_link", err.Error())))
		return
	}
	client.SendResponse(protocol.NewOKResponse(req.ID, map[string]any{"linked": true}))
}

func (m *WorkstationsMethods) handleUnlinkAgent(ctx context.Context, client *gateway.Client, req *protocol.RequestFrame) {
	locale := store.LocaleFromContext(ctx)
	var params struct {
		AgentID       string `json:"agentId"`
		WorkstationID string `json:"workstationId"`
	}
	if req.Params != nil {
		if err := json.Unmarshal(req.Params, &params); err != nil {
			client.SendResponse(protocol.NewErrorResponse(req.ID, protocol.ErrInvalidRequest, "invalid params"))
			return
		}
	}
	agentID, err := uuid.Parse(params.AgentID)
	if err != nil {
		client.SendResponse(protocol.NewErrorResponse(req.ID, protocol.ErrInvalidRequest,
			i18n.T(locale, i18n.MsgInvalidID, "agent")))
		return
	}
	wsID, err := uuid.Parse(params.WorkstationID)
	if err != nil {
		client.SendResponse(protocol.NewErrorResponse(req.ID, protocol.ErrInvalidRequest,
			i18n.T(locale, i18n.MsgInvalidID, "workstation")))
		return
	}
	if err := m.linkStore.Unlink(ctx, agentID, wsID); err != nil {
		client.SendResponse(protocol.NewErrorResponse(req.ID, protocol.ErrInternal,
			i18n.T(locale, i18n.MsgFailedToDelete, "agent_workstation_link", err.Error())))
		return
	}
	client.SendResponse(protocol.NewOKResponse(req.ID, map[string]any{"unlinked": true}))
}

// --- Phase 6: workstation permission allowlist CRUD ---

func (m *WorkstationsMethods) requirePermStore(locale string, client *gateway.Client, req *protocol.RequestFrame) bool {
	if m.permStore == nil {
		client.SendResponse(protocol.NewErrorResponse(req.ID, protocol.ErrNotImplemented,
			i18n.T(locale, i18n.MsgNotImplemented, "workstations.permissions")))
		return false
	}
	return true
}

func (m *WorkstationsMethods) handlePermList(ctx context.Context, client *gateway.Client, req *protocol.RequestFrame) {
	locale := store.LocaleFromContext(ctx)
	if !m.requirePermStore(locale, client, req) {
		return
	}
	var params struct {
		WorkstationID string `json:"workstationId"`
	}
	if req.Params != nil {
		if err := json.Unmarshal(req.Params, &params); err != nil {
			client.SendResponse(protocol.NewErrorResponse(req.ID, protocol.ErrInvalidRequest, "invalid params"))
			return
		}
	}
	wsID, err := uuid.Parse(params.WorkstationID)
	if err != nil {
		client.SendResponse(protocol.NewErrorResponse(req.ID, protocol.ErrInvalidRequest,
			i18n.T(locale, i18n.MsgInvalidID, "workstation")))
		return
	}
	// Ownership check: verify workstation belongs to caller's tenant before listing perms.
	// GetByID scopes the query by tenant_id — returns ErrNoRows for a different tenant.
	if _, err := m.wsStore.GetByID(ctx, wsID); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			client.SendResponse(protocol.NewErrorResponse(req.ID, protocol.ErrNotFound,
				i18n.T(locale, i18n.MsgWorkstationNotFound, params.WorkstationID)))
			return
		}
		client.SendResponse(protocol.NewErrorResponse(req.ID, protocol.ErrInternal,
			i18n.T(locale, i18n.MsgInternalError, err.Error())))
		return
	}
	perms, err := m.permStore.ListForWorkstation(ctx, wsID)
	if err != nil {
		client.SendResponse(protocol.NewErrorResponse(req.ID, protocol.ErrInternal,
			i18n.T(locale, i18n.MsgFailedToList, "permissions")))
		return
	}
	client.SendResponse(protocol.NewOKResponse(req.ID, map[string]any{"permissions": perms}))
}

func (m *WorkstationsMethods) handlePermAdd(ctx context.Context, client *gateway.Client, req *protocol.RequestFrame) {
	locale := store.LocaleFromContext(ctx)
	if !m.requirePermStore(locale, client, req) {
		return
	}
	var params struct {
		WorkstationID string `json:"workstationId"`
		Pattern       string `json:"pattern"`
	}
	if req.Params != nil {
		if err := json.Unmarshal(req.Params, &params); err != nil {
			client.SendResponse(protocol.NewErrorResponse(req.ID, protocol.ErrInvalidRequest, "invalid params"))
			return
		}
	}
	wsID, err := uuid.Parse(params.WorkstationID)
	if err != nil {
		client.SendResponse(protocol.NewErrorResponse(req.ID, protocol.ErrInvalidRequest,
			i18n.T(locale, i18n.MsgInvalidID, "workstation")))
		return
	}
	// I5 fix: verify workstation belongs to caller's tenant before adding permission.
	// GetByID scopes the query by tenant_id in the WHERE clause — returns ErrNoRows if
	// the workstation exists in a different tenant.
	if _, err := m.wsStore.GetByID(ctx, wsID); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			client.SendResponse(protocol.NewErrorResponse(req.ID, protocol.ErrNotFound,
				i18n.T(locale, i18n.MsgWorkstationNotFound, params.WorkstationID)))
			return
		}
		client.SendResponse(protocol.NewErrorResponse(req.ID, protocol.ErrInternal,
			i18n.T(locale, i18n.MsgInternalError, err.Error())))
		return
	}
	if params.Pattern == "" {
		client.SendResponse(protocol.NewErrorResponse(req.ID, protocol.ErrInvalidRequest,
			i18n.T(locale, i18n.MsgRequired, "pattern")))
		return
	}
	perm := &store.WorkstationPermission{
		WorkstationID: wsID,
		Pattern:       params.Pattern,
		Enabled:       true,
		CreatedBy:     client.UserID(),
	}
	if err := m.permStore.Add(ctx, perm); err != nil {
		client.SendResponse(protocol.NewErrorResponse(req.ID, protocol.ErrInternal,
			i18n.T(locale, i18n.MsgFailedToCreate, "permission", err.Error())))
		return
	}
	client.SendResponse(protocol.NewOKResponse(req.ID, map[string]any{"permission": perm}))
}

func (m *WorkstationsMethods) handlePermRemove(ctx context.Context, client *gateway.Client, req *protocol.RequestFrame) {
	locale := store.LocaleFromContext(ctx)
	if !m.requirePermStore(locale, client, req) {
		return
	}
	var params struct {
		ID string `json:"id"`
	}
	if req.Params != nil {
		if err := json.Unmarshal(req.Params, &params); err != nil {
			client.SendResponse(protocol.NewErrorResponse(req.ID, protocol.ErrInvalidRequest, "invalid params"))
			return
		}
	}
	id, err := uuid.Parse(params.ID)
	if err != nil {
		client.SendResponse(protocol.NewErrorResponse(req.ID, protocol.ErrInvalidRequest,
			i18n.T(locale, i18n.MsgInvalidID, "permission")))
		return
	}
	if err := m.permStore.Remove(ctx, id); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			client.SendResponse(protocol.NewErrorResponse(req.ID, protocol.ErrNotFound,
				i18n.T(locale, i18n.MsgWorkstationPermNotFound, params.ID)))
			return
		}
		client.SendResponse(protocol.NewErrorResponse(req.ID, protocol.ErrInternal,
			i18n.T(locale, i18n.MsgFailedToDelete, "permission", err.Error())))
		return
	}
	client.SendResponse(protocol.NewOKResponse(req.ID, map[string]any{"id": id}))
}

func (m *WorkstationsMethods) handlePermToggle(ctx context.Context, client *gateway.Client, req *protocol.RequestFrame) {
	locale := store.LocaleFromContext(ctx)
	if !m.requirePermStore(locale, client, req) {
		return
	}
	var params struct {
		ID      string `json:"id"`
		Enabled bool   `json:"enabled"`
	}
	if req.Params != nil {
		if err := json.Unmarshal(req.Params, &params); err != nil {
			client.SendResponse(protocol.NewErrorResponse(req.ID, protocol.ErrInvalidRequest, "invalid params"))
			return
		}
	}
	id, err := uuid.Parse(params.ID)
	if err != nil {
		client.SendResponse(protocol.NewErrorResponse(req.ID, protocol.ErrInvalidRequest,
			i18n.T(locale, i18n.MsgInvalidID, "permission")))
		return
	}
	if err := m.permStore.SetEnabled(ctx, id, params.Enabled); err != nil {
		client.SendResponse(protocol.NewErrorResponse(req.ID, protocol.ErrInternal,
			i18n.T(locale, i18n.MsgFailedToUpdate, "permission", err.Error())))
		return
	}
	client.SendResponse(protocol.NewOKResponse(req.ID, map[string]any{"id": id, "enabled": params.Enabled}))
}

// --- Phase 7: activity audit log ---

func (m *WorkstationsMethods) handleListActivity(ctx context.Context, client *gateway.Client, req *protocol.RequestFrame) {
	locale := store.LocaleFromContext(ctx)
	if m.activityStore == nil {
		client.SendResponse(protocol.NewErrorResponse(req.ID, protocol.ErrNotImplemented,
			i18n.T(locale, i18n.MsgNotImplemented, "workstations.activity.list")))
		return
	}
	var params struct {
		WorkstationID string `json:"workstationId"`
		Limit         int    `json:"limit"`
		Cursor        string `json:"cursor"`
	}
	if req.Params != nil {
		if err := json.Unmarshal(req.Params, &params); err != nil {
			client.SendResponse(protocol.NewErrorResponse(req.ID, protocol.ErrInvalidRequest, "invalid params"))
			return
		}
	}
	wsID, err := uuid.Parse(params.WorkstationID)
	if err != nil {
		client.SendResponse(protocol.NewErrorResponse(req.ID, protocol.ErrInvalidRequest,
			i18n.T(locale, i18n.MsgInvalidID, "workstation")))
		return
	}
	// Ownership check: verify the workstation belongs to the caller's tenant.
	// GetByID scopes by tenant_id — returns ErrNoRows if workstation is in a different tenant.
	if _, err := m.wsStore.GetByID(ctx, wsID); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			client.SendResponse(protocol.NewErrorResponse(req.ID, protocol.ErrNotFound,
				i18n.T(locale, i18n.MsgWorkstationNotFound, params.WorkstationID)))
			return
		}
		client.SendResponse(protocol.NewErrorResponse(req.ID, protocol.ErrInternal,
			i18n.T(locale, i18n.MsgInternalError, err.Error())))
		return
	}
	limit := params.Limit
	if limit <= 0 || limit > 200 {
		limit = 50
	}
	var cursor *uuid.UUID
	if params.Cursor != "" {
		if cID, err := uuid.Parse(params.Cursor); err == nil {
			cursor = &cID
		}
	}
	rows, nextCursor, err := m.activityStore.List(ctx, wsID, limit, cursor)
	if err != nil {
		client.SendResponse(protocol.NewErrorResponse(req.ID, protocol.ErrInternal,
			i18n.T(locale, i18n.MsgFailedToList, "activity")))
		return
	}
	resp := map[string]any{"activity": rows}
	if nextCursor != nil {
		resp["nextCursor"] = nextCursor.String()
	}
	client.SendResponse(protocol.NewOKResponse(req.ID, resp))
}
