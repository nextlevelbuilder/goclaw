package methods

import (
	"context"
	"encoding/json"
	"log/slog"

	"github.com/google/uuid"

	"github.com/nextlevelbuilder/goclaw/internal/bus"
	"github.com/nextlevelbuilder/goclaw/internal/channels"
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

// OrphanChannelCleaner runs channel-type-specific cleanup when a delete
// arrives for a channel that is no longer loaded in the runtime Manager
// (e.g. admin disabled it earlier). Closure injected from cmd/gateway.go.
// See identical type in internal/http/channel_instances.go.
type OrphanChannelCleaner func(ctx context.Context, tenantID uuid.UUID, configJSON []byte) error

// ChannelInstancesMethods handles channel instance CRUD via WebSocket RPC.
// agentStore is held so the create/update handlers can resolve agent_key or
// UUID input via resolveAgentUUIDCached.
type ChannelInstancesMethods struct {
	store      store.ChannelInstanceStore
	agentStore store.AgentStore
	msgBus     *bus.MessageBus
	eventBus   bus.EventPublisher
	channelMgr *channels.Manager // optional — enables ChannelDestroyer hook on delete
	// orphanCleaners is keyed by channel_type; called when channelMgr.GetChannel
	// returns false (channel was unloaded — typically due to disable).
	orphanCleaners map[string]OrphanChannelCleaner
}

// NewChannelInstancesMethods creates a new handler for channel instance management.
// channelMgr is optional; when non-nil and the channel's runtime impl
// satisfies channels.ChannelDestroyer, handleDelete invokes Destroy() before
// removing the DB row so external resources (e.g. Bitrix24 bots) get cleaned.
func NewChannelInstancesMethods(s store.ChannelInstanceStore, as store.AgentStore, msgBus *bus.MessageBus, eventBus bus.EventPublisher, channelMgr *channels.Manager) *ChannelInstancesMethods {
	return &ChannelInstancesMethods{store: s, agentStore: as, msgBus: msgBus, eventBus: eventBus, channelMgr: channelMgr}
}

// RegisterOrphanCleaner registers a per-channel-type cleanup function that
// fires when handleDelete sees a channel NOT loaded in Manager (typically
// because admin disabled it). See HTTP twin for full rationale.
func (m *ChannelInstancesMethods) RegisterOrphanCleaner(channelType string, fn OrphanChannelCleaner) {
	if m.orphanCleaners == nil {
		m.orphanCleaners = make(map[string]OrphanChannelCleaner)
	}
	m.orphanCleaners[channelType] = fn
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

	// Accept both agent_key and UUID via resolveAgentUUIDCached. Router is nil
	// here because channel_instances methods are not wired to the router —
	// falls back to a pure DB lookup, acceptable given create is a rare op.
	agentID, err := resolveAgentUUIDCached(ctx, nil, m.agentStore, params.AgentID)
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

	// Best-effort: notify the channel impl so external resources (e.g. the
	// Bitrix24 imbot.register'd bot) get cleaned up BEFORE the DB row is
	// removed. Mirror of HTTP handler — see internal/http/channel_instances.go
	// for full rationale on the two branches.
	if m.channelMgr != nil {
		if ch, ok := m.channelMgr.GetChannel(inst.Name); ok {
			if destroyer, ok := ch.(channels.ChannelDestroyer); ok {
				if err := destroyer.Destroy(ctx); err != nil {
					slog.Warn("channels.instances.delete: destroyer failed — proceeding with DB delete",
						"name", inst.Name, "tenant_id", inst.TenantID, "type", inst.ChannelType, "err", err)
				}
			}
		} else if cleaner, ok := m.orphanCleaners[inst.ChannelType]; ok && cleaner != nil {
			if err := cleaner(ctx, inst.TenantID, inst.Config); err != nil {
				slog.Warn("channels.instances.delete: orphan cleaner failed — proceeding with DB delete",
					"name", inst.Name, "tenant_id", inst.TenantID, "type", inst.ChannelType, "err", err)
			}
		}
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
//
// Keep this list in sync with the HTTP twin in internal/http/channel_instances.go
// and with CHANNEL_TYPES in ui/web/src/constants/channels.ts. When the two
// backend switches drift (as happened with facebook/pancake/bitrix24), the
// WS-driven UI rejects channels the HTTP API accepts, and the dropdown offers
// channels neither API accepts.
func isValidChannelType(ct string) bool {
	switch ct {
	case "telegram", "discord", "slack", "whatsapp", "zalo_oa", "zalo_personal", "feishu", "facebook", "pancake", "bitrix24":
		return true
	}
	return false
}
