package methods

import (
	"context"
	"encoding/json"
	"log/slog"

	"github.com/google/uuid"
	"github.com/nextlevelbuilder/goclaw/internal/gateway"
	"github.com/nextlevelbuilder/goclaw/internal/i18n"
	"github.com/nextlevelbuilder/goclaw/internal/store"
	"github.com/nextlevelbuilder/goclaw/pkg/protocol"
)

// ConfigPermissionsMethods handles config.permissions.* RPC methods.
type ConfigPermissionsMethods struct {
	permStore  store.ConfigPermissionStore
	agentStore store.AgentStore
}

func NewConfigPermissionsMethods(ps store.ConfigPermissionStore, as store.AgentStore) *ConfigPermissionsMethods {
	return &ConfigPermissionsMethods{permStore: ps, agentStore: as}
}

func (m *ConfigPermissionsMethods) Register(router *gateway.MethodRouter) {
	router.Register(protocol.MethodConfigPermissionsList, m.handleList)
	router.Register(protocol.MethodConfigPermissionsGrant, m.handleGrant)
	router.Register(protocol.MethodConfigPermissionsRevoke, m.handleRevoke)
}

func (m *ConfigPermissionsMethods) handleList(ctx context.Context, client *gateway.Client, req *protocol.RequestFrame) {
	locale := store.LocaleFromContext(ctx)
	var params struct {
		AgentID    string `json:"agentId"`
		ConfigType string `json:"configType"`
	}
	if req.Params != nil {
		json.Unmarshal(req.Params, &params)
	}
	if params.AgentID == "" {
		client.SendResponse(protocol.NewErrorResponse(req.ID, protocol.ErrInvalidRequest, i18n.T(locale, i18n.MsgRequired, "agentId")))
		return
	}

	agentUUID, err := uuid.Parse(params.AgentID)
	if err != nil {
		client.SendResponse(protocol.NewErrorResponse(req.ID, protocol.ErrInvalidRequest, "invalid agentId"))
		return
	}

	perms, err := m.permStore.List(ctx, agentUUID, params.ConfigType, "")
	if err != nil {
		client.SendResponse(protocol.NewErrorResponse(req.ID, protocol.ErrInternal, configPermInternalErr("list", err)))
		return
	}

	client.SendResponse(protocol.NewOKResponse(req.ID, map[string]any{"permissions": perms}))
}

func (m *ConfigPermissionsMethods) handleGrant(ctx context.Context, client *gateway.Client, req *protocol.RequestFrame) {
	locale := store.LocaleFromContext(ctx)
	var params struct {
		AgentID    string          `json:"agentId"`
		Scope      string          `json:"scope"`
		ConfigType string          `json:"configType"`
		UserID     string          `json:"userId"`
		Permission string          `json:"permission"`
		GrantedBy  *string         `json:"grantedBy,omitempty"`
		Metadata   json.RawMessage `json:"metadata,omitempty"`
	}
	if req.Params != nil {
		json.Unmarshal(req.Params, &params)
	}

	switch {
	case params.AgentID == "":
		client.SendResponse(protocol.NewErrorResponse(req.ID, protocol.ErrInvalidRequest, i18n.T(locale, i18n.MsgRequired, "agentId")))
		return
	case params.Scope == "":
		client.SendResponse(protocol.NewErrorResponse(req.ID, protocol.ErrInvalidRequest, i18n.T(locale, i18n.MsgRequired, "scope")))
		return
	case params.ConfigType == "":
		client.SendResponse(protocol.NewErrorResponse(req.ID, protocol.ErrInvalidRequest, i18n.T(locale, i18n.MsgRequired, "configType")))
		return
	case params.UserID == "":
		client.SendResponse(protocol.NewErrorResponse(req.ID, protocol.ErrInvalidRequest, i18n.T(locale, i18n.MsgRequired, "userId")))
		return
	case params.Permission == "":
		client.SendResponse(protocol.NewErrorResponse(req.ID, protocol.ErrInvalidRequest, i18n.T(locale, i18n.MsgRequired, "permission")))
		return
	}

	agentUUID, err := uuid.Parse(params.AgentID)
	if err != nil {
		client.SendResponse(protocol.NewErrorResponse(req.ID, protocol.ErrInvalidRequest, "invalid agentId"))
		return
	}

	// Auto-fill grantedBy from the caller's identity if not explicitly provided.
	grantedBy := params.GrantedBy
	if grantedBy == nil {
		if caller := store.UserIDFromContext(ctx); caller != "" {
			grantedBy = &caller
		}
	}

	perm := &store.ConfigPermission{
		AgentID:    agentUUID,
		Scope:      params.Scope,
		ConfigType: params.ConfigType,
		UserID:     params.UserID,
		Permission: params.Permission,
		GrantedBy:  grantedBy,
		Metadata:   params.Metadata,
	}

	if err := m.permStore.Grant(ctx, perm); err != nil {
		client.SendResponse(protocol.NewErrorResponse(req.ID, protocol.ErrInternal, configPermInternalErr("grant", err)))
		return
	}

	client.SendResponse(protocol.NewOKResponse(req.ID, map[string]any{"ok": true}))
}

func (m *ConfigPermissionsMethods) handleRevoke(ctx context.Context, client *gateway.Client, req *protocol.RequestFrame) {
	locale := store.LocaleFromContext(ctx)
	var params struct {
		AgentID    string `json:"agentId"`
		Scope      string `json:"scope"`
		ConfigType string `json:"configType"`
		UserID     string `json:"userId"`
	}
	if req.Params != nil {
		json.Unmarshal(req.Params, &params)
	}

	switch {
	case params.AgentID == "":
		client.SendResponse(protocol.NewErrorResponse(req.ID, protocol.ErrInvalidRequest, i18n.T(locale, i18n.MsgRequired, "agentId")))
		return
	case params.Scope == "":
		client.SendResponse(protocol.NewErrorResponse(req.ID, protocol.ErrInvalidRequest, i18n.T(locale, i18n.MsgRequired, "scope")))
		return
	case params.ConfigType == "":
		client.SendResponse(protocol.NewErrorResponse(req.ID, protocol.ErrInvalidRequest, i18n.T(locale, i18n.MsgRequired, "configType")))
		return
	case params.UserID == "":
		client.SendResponse(protocol.NewErrorResponse(req.ID, protocol.ErrInvalidRequest, i18n.T(locale, i18n.MsgRequired, "userId")))
		return
	}

	agentUUID, err := uuid.Parse(params.AgentID)
	if err != nil {
		client.SendResponse(protocol.NewErrorResponse(req.ID, protocol.ErrInvalidRequest, "invalid agentId"))
		return
	}

	if err := m.permStore.Revoke(ctx, agentUUID, params.Scope, params.ConfigType, params.UserID); err != nil {
		client.SendResponse(protocol.NewErrorResponse(req.ID, protocol.ErrInternal, configPermInternalErr("revoke", err)))
		return
	}

	client.SendResponse(protocol.NewOKResponse(req.ID, map[string]any{"ok": true}))
}

func configPermInternalErr(action string, err error) string {
	slog.Error("config.permissions RPC error", "action", action, "error", err)
	return "internal error"
}
