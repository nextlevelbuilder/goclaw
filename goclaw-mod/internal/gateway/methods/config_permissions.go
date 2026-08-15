package methods

import (
	"context"
	"encoding/json"
	"log/slog"

	"github.com/nextlevelbuilder/goclaw/internal/agent"
	"github.com/nextlevelbuilder/goclaw/internal/channels"
	"github.com/nextlevelbuilder/goclaw/internal/gateway"
	"github.com/nextlevelbuilder/goclaw/internal/i18n"
	"github.com/nextlevelbuilder/goclaw/internal/store"
	"github.com/nextlevelbuilder/goclaw/pkg/protocol"
)

// ConfigPermissionsMethods handles config.permissions.* RPC methods.
type ConfigPermissionsMethods struct {
	permStore      store.ConfigPermissionStore
	agentStore     store.AgentStore
	agentRouter    *agent.Router           // cache-aware agent resolver; nil = DB-only fallback
	memberResolver channels.MemberResolver // optional — enriches file_writer metadata on grant
}

func NewConfigPermissionsMethods(ps store.ConfigPermissionStore, as store.AgentStore) *ConfigPermissionsMethods {
	return &ConfigPermissionsMethods{permStore: ps, agentStore: as}
}

// SetAgentRouter wires the agent router for cache-aware agent_key resolution.
func (m *ConfigPermissionsMethods) SetAgentRouter(r *agent.Router) {
	m.agentRouter = r
}

// SetMemberResolver wires a channel member resolver so Grant can auto-enrich
// file_writer metadata when the caller supplies none (e.g. Web UI path).
func (m *ConfigPermissionsMethods) SetMemberResolver(r channels.MemberResolver) {
	m.memberResolver = r
}

func (m *ConfigPermissionsMethods) Register(router *gateway.MethodRouter) {
	router.Register(protocol.MethodConfigPermissionsList, m.handleList)
	router.Register(protocol.MethodConfigPermissionsCheck, m.handleCheck)
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
	if params.ConfigType != "" && !store.ValidConfigType(params.ConfigType) {
		client.SendResponse(protocol.NewErrorResponse(req.ID, protocol.ErrInvalidRequest, "invalid configType"))
		return
	}

	agentUUID, err := resolveAgentUUIDCached(ctx, m.agentRouter, m.agentStore, params.AgentID)
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

func (m *ConfigPermissionsMethods) handleCheck(ctx context.Context, client *gateway.Client, req *protocol.RequestFrame) {
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

	if errMsg := validateConfigPermissionParams(locale, params.AgentID, params.Scope, params.ConfigType, params.UserID, "allow", false); errMsg != "" {
		client.SendResponse(protocol.NewErrorResponse(req.ID, protocol.ErrInvalidRequest, errMsg))
		return
	}

	agentUUID, err := resolveAgentUUIDCached(ctx, m.agentRouter, m.agentStore, params.AgentID)
	if err != nil {
		client.SendResponse(protocol.NewErrorResponse(req.ID, protocol.ErrInvalidRequest, "invalid agentId"))
		return
	}

	decision, err := store.CheckConfigPermissionDecision(ctx, m.permStore, agentUUID, params.Scope, params.ConfigType, params.UserID)
	if err != nil {
		client.SendResponse(protocol.NewErrorResponse(req.ID, protocol.ErrInternal, configPermInternalErr("check", err)))
		return
	}

	client.SendResponse(protocol.NewOKResponse(req.ID, map[string]any{"decision": decision}))
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

	if errMsg := validateConfigPermissionParams(locale, params.AgentID, params.Scope, params.ConfigType, params.UserID, params.Permission, true); errMsg != "" {
		client.SendResponse(protocol.NewErrorResponse(req.ID, protocol.ErrInvalidRequest, errMsg))
		return
	}

	agentUUID, err := resolveAgentUUIDCached(ctx, m.agentRouter, m.agentStore, params.AgentID)
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

	// Auto-enrich file_writer metadata for group scopes when caller supplied none.
	// Best-effort: failure (bot offline, user left group, channel not supported)
	// leaves params.Metadata as-is and the store's own fallback ("{}") applies.
	metadata := params.Metadata
	if params.ConfigType == store.ConfigTypeFileWriter && channels.IsEmptyWriterMetadata(metadata) {
		if enriched, ok := channels.EnrichFileWriterMetadata(ctx, m.memberResolver, params.Scope, params.UserID); ok {
			metadata = enriched
		}
	}

	perm := &store.ConfigPermission{
		AgentID:    agentUUID,
		Scope:      params.Scope,
		ConfigType: params.ConfigType,
		UserID:     params.UserID,
		Permission: params.Permission,
		GrantedBy:  grantedBy,
		Metadata:   metadata,
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

	if errMsg := validateConfigPermissionParams(locale, params.AgentID, params.Scope, params.ConfigType, params.UserID, "allow", false); errMsg != "" {
		client.SendResponse(protocol.NewErrorResponse(req.ID, protocol.ErrInvalidRequest, errMsg))
		return
	}

	agentUUID, err := resolveAgentUUIDCached(ctx, m.agentRouter, m.agentStore, params.AgentID)
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

func validateConfigPermissionParams(locale, agentID, scope, configType, userID, permission string, validatePermission bool) string {
	switch {
	case agentID == "":
		return i18n.T(locale, i18n.MsgRequired, "agentId")
	case scope == "":
		return i18n.T(locale, i18n.MsgRequired, "scope")
	case configType == "":
		return i18n.T(locale, i18n.MsgRequired, "configType")
	case userID == "":
		return i18n.T(locale, i18n.MsgRequired, "userId")
	case !store.ValidConfigScope(scope):
		return "invalid scope"
	case !store.ValidConfigType(configType):
		return "invalid configType"
	case validatePermission && permission == "":
		return i18n.T(locale, i18n.MsgRequired, "permission")
	case validatePermission && !store.ValidConfigPermission(permission):
		return "invalid permission"
	}
	return ""
}
