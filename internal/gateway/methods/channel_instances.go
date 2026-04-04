package methods

import (
	"context"
	"encoding/json"
	"log/slog"

	"github.com/google/uuid"

	"github.com/nextlevelbuilder/goclaw/internal/bus"
	"github.com/nextlevelbuilder/goclaw/internal/gateway"
	"github.com/nextlevelbuilder/goclaw/internal/i18n"
	"github.com/nextlevelbuilder/goclaw/internal/store"
	"github.com/nextlevelbuilder/goclaw/pkg/protocol"
)

// channelInstanceAllowed mirrors the HTTP allowlist in internal/http/validate.go.
var channelInstanceAllowed = map[string]bool{
	"channel_type": true, "credentials": true, "agent_id": true,
	"enabled": true, "group_policy": true, "allow_from": true,
	"metadata": true, "webhook_secret": true, "config": true,
	"display_name": true,
}

// ChannelInstancesMethods handles channel instance CRUD via WebSocket RPC.
type ChannelInstancesMethods struct {
	store    store.ChannelInstanceStore
	msgBus   *bus.MessageBus
	eventBus bus.EventPublisher
}

// NewChannelInstancesMethods creates a new handler for channel instance management.
func NewChannelInstancesMethods(s store.ChannelInstanceStore, msgBus *bus.MessageBus, eventBus bus.EventPublisher) *ChannelInstancesMethods {
	return &ChannelInstancesMethods{store: s, msgBus: msgBus, eventBus: eventBus}
}

// Register registers all channel instance RPC methods.
func (m *ChannelInstancesMethods) Register(router *gateway.MethodRouter) {
	router.Register(protocol.MethodChannelInstancesList, m.handleList)
	router.Register(protocol.MethodChannelInstancesGet, m.handleGet)
	router.Register(protocol.MethodChannelInstancesCreate, m.handleCreate)
	router.Register(protocol.MethodChannelInstancesUpdate, m.handleUpdate)
	router.Register(protocol.MethodChannelInstancesDelete, m.handleDelete)
}

func (m *ChannelInstancesMethods) emitCacheInvalidate() {
	if m.msgBus == nil {
		return
	}
	m.msgBus.Broadcast(bus.Event{
		Name:    protocol.EventCacheInvalidate,
		Payload: bus.CacheInvalidatePayload{Kind: bus.CacheKindChannelInstances},
	})
}

func (m *ChannelInstancesMethods) handleList(ctx context.Context, client *gateway.Client, req *protocol.RequestFrame) {
	locale := store.LocaleFromContext(ctx)
	instances, err := m.store.ListAll(ctx)
	if err != nil {
		slog.Error("channels.instances.list", "error", err)
		client.SendResponse(protocol.NewErrorResponse(req.ID, protocol.ErrInternal, i18n.T(locale, i18n.MsgFailedToList, "channel instances")))
		return
	}

	// Mask credentials in response — never expose secrets via WS.
	result := make([]map[string]any, 0, len(instances))
	for _, inst := range instances {
		result = append(result, maskInstance(inst))
	}

	client.SendResponse(protocol.NewOKResponse(req.ID, map[string]any{
		"instances": result,
	}))
}

func (m *ChannelInstancesMethods) handleGet(ctx context.Context, client *gateway.Client, req *protocol.RequestFrame) {
	locale := store.LocaleFromContext(ctx)
	var params struct {
		ID string `json:"id"`
	}
	if req.Params != nil {
		json.Unmarshal(req.Params, &params)
	}

	id, err := uuid.Parse(params.ID)
	if err != nil {
		client.SendResponse(protocol.NewErrorResponse(req.ID, protocol.ErrInvalidRequest, i18n.T(locale, i18n.MsgInvalidID, "instance")))
		return
	}

	inst, err := m.store.Get(ctx, id)
	if err != nil {
		client.SendResponse(protocol.NewErrorResponse(req.ID, protocol.ErrNotFound, i18n.T(locale, i18n.MsgInstanceNotFound)))
		return
	}

	client.SendResponse(protocol.NewOKResponse(req.ID, maskInstance(*inst)))
}

func (m *ChannelInstancesMethods) handleCreate(ctx context.Context, client *gateway.Client, req *protocol.RequestFrame) {
	locale := store.LocaleFromContext(ctx)
	var params struct {
		Name        string          `json:"name"`
		DisplayName string          `json:"display_name"`
		ChannelType string          `json:"channel_type"`
		AgentID     string          `json:"agent_id"`
		Credentials json.RawMessage `json:"credentials"`
		Config      json.RawMessage `json:"config"`
		Enabled     *bool           `json:"enabled"`
	}
	if req.Params != nil {
		json.Unmarshal(req.Params, &params)
	}

	if params.Name == "" || params.ChannelType == "" || params.AgentID == "" {
		client.SendResponse(protocol.NewErrorResponse(req.ID, protocol.ErrInvalidRequest, i18n.T(locale, i18n.MsgRequired, "name, channel_type, and agent_id")))
		return
	}

	if !isValidChannelType(params.ChannelType) {
		client.SendResponse(protocol.NewErrorResponse(req.ID, protocol.ErrInvalidRequest, i18n.T(locale, i18n.MsgInvalidChannelType)))
		return
	}

	agentID, err := uuid.Parse(params.AgentID)
	if err != nil {
		client.SendResponse(protocol.NewErrorResponse(req.ID, protocol.ErrInvalidRequest, i18n.T(locale, i18n.MsgInvalidID, "agent_id")))
		return
	}

	enabled := true
	if params.Enabled != nil {
		enabled = *params.Enabled
	}

	inst := &store.ChannelInstanceData{
		Name:        params.Name,
		DisplayName: params.DisplayName,
		ChannelType: params.ChannelType,
		AgentID:     agentID,
		Credentials: params.Credentials,
		Config:      params.Config,
		Enabled:     enabled,
	}

	if err := m.store.Create(ctx, inst); err != nil {
		slog.Error("channels.instances.create", "error", err)
		client.SendResponse(protocol.NewErrorResponse(req.ID, protocol.ErrInternal, i18n.T(locale, i18n.MsgFailedToCreate, "instance", err.Error())))
		return
	}

	m.emitCacheInvalidate()
	emitAudit(m.eventBus, client, "channel_instance.created", "channel_instance", inst.ID.String())
	client.SendResponse(protocol.NewOKResponse(req.ID, maskInstance(*inst)))
}

func (m *ChannelInstancesMethods) handleUpdate(ctx context.Context, client *gateway.Client, req *protocol.RequestFrame) {
	locale := store.LocaleFromContext(ctx)
	var params struct {
		ID      string          `json:"id"`
		Updates json.RawMessage `json:"updates"`
	}
	if req.Params != nil {
		json.Unmarshal(req.Params, &params)
	}

	id, err := uuid.Parse(params.ID)
	if err != nil {
		client.SendResponse(protocol.NewErrorResponse(req.ID, protocol.ErrInvalidRequest, i18n.T(locale, i18n.MsgInvalidID, "instance")))
		return
	}

	var raw map[string]any
	if err := json.Unmarshal(params.Updates, &raw); err != nil {
		client.SendResponse(protocol.NewErrorResponse(req.ID, protocol.ErrInvalidRequest, i18n.T(locale, i18n.MsgInvalidUpdates)))
		return
	}

	// Allowlist: only permit known channel instance columns (matches HTTP handler).
	updates := make(map[string]any, len(raw))
	for k, v := range raw {
		if channelInstanceAllowed[k] {
			updates[k] = v
		} else {
			slog.Warn("security.filtered_unknown_field", "field", k, "handler", "channels.instances.update")
		}
	}

	if err := m.store.Update(ctx, id, updates); err != nil {
		slog.Error("channels.instances.update", "error", err)
		client.SendResponse(protocol.NewErrorResponse(req.ID, protocol.ErrInternal, i18n.T(locale, i18n.MsgFailedToUpdate, "instance", err.Error())))
		return
	}

	m.emitCacheInvalidate()
	emitAudit(m.eventBus, client, "channel_instance.updated", "channel_instance", id.String())
	client.SendResponse(protocol.NewOKResponse(req.ID, map[string]any{"status": "updated"}))
}

func (m *ChannelInstancesMethods) handleDelete(ctx context.Context, client *gateway.Client, req *protocol.RequestFrame) {
	locale := store.LocaleFromContext(ctx)
	var params struct {
		ID string `json:"id"`
	}
	if req.Params != nil {
		json.Unmarshal(req.Params, &params)
	}

	id, err := uuid.Parse(params.ID)
	if err != nil {
		client.SendResponse(protocol.NewErrorResponse(req.ID, protocol.ErrInvalidRequest, i18n.T(locale, i18n.MsgInvalidID, "instance")))
		return
	}

	// Look up instance to check if it's a default (seeded) instance.
	inst, err := m.store.Get(ctx, id)
	if err != nil {
		client.SendResponse(protocol.NewErrorResponse(req.ID, protocol.ErrInvalidRequest, i18n.T(locale, i18n.MsgInstanceNotFound)))
		return
	}
	if store.IsDefaultChannelInstance(inst.Name) {
		client.SendResponse(protocol.NewErrorResponse(req.ID, protocol.ErrInvalidRequest, i18n.T(locale, i18n.MsgCannotDeleteDefaultInst)))
		return
	}

	if err := m.store.Delete(ctx, id); err != nil {
		slog.Error("channels.instances.delete", "error", err)
		client.SendResponse(protocol.NewErrorResponse(req.ID, protocol.ErrInternal, i18n.T(locale, i18n.MsgFailedToDelete, "instance", err.Error())))
		return
	}

	m.emitCacheInvalidate()
	emitAudit(m.eventBus, client, "channel_instance.deleted", "channel_instance", id.String())
	client.SendResponse(protocol.NewOKResponse(req.ID, map[string]any{"status": "deleted"}))
}

// maskInstance returns a map representation with credentials masked.
func maskInstance(inst store.ChannelInstanceData) map[string]any {
	result := map[string]any{
		"id":              inst.ID,
		"name":            inst.Name,
		"display_name":    inst.DisplayName,
		"channel_type":    inst.ChannelType,
		"agent_id":        inst.AgentID,
		"config":          inst.Config,
		"enabled":         inst.Enabled,
		"is_default":      store.IsDefaultChannelInstance(inst.Name),
		"has_credentials": len(inst.Credentials) > 0,
		"created_by":      inst.CreatedBy,
		"created_at":      inst.CreatedAt,
		"updated_at":      inst.UpdatedAt,
	}

	// Mask credentials: show keys with "***" values
	if len(inst.Credentials) > 0 {
		var raw map[string]any
		if json.Unmarshal(inst.Credentials, &raw) == nil {
			masked := make(map[string]any, len(raw))
			for k := range raw {
				masked[k] = "***"
			}
			result["credentials"] = masked
		} else {
			result["credentials"] = map[string]string{}
		}
	} else {
		result["credentials"] = map[string]string{}
	}

	return result
}

// isValidChannelType checks if the channel type is supported.
func isValidChannelType(ct string) bool {
	switch ct {
	case "telegram", "discord", "slack", "whatsapp", "zalo_oa", "zalo_personal", "feishu":
		return true
	}
	return false
}
