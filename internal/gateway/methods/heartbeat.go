package methods

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"log/slog"
	"time"

	"github.com/google/uuid"

	"github.com/nextlevelbuilder/goclaw/internal/bus"
	"github.com/nextlevelbuilder/goclaw/internal/gateway"
	"github.com/nextlevelbuilder/goclaw/internal/i18n"
	"github.com/nextlevelbuilder/goclaw/internal/store"
	"github.com/nextlevelbuilder/goclaw/pkg/protocol"
)

// HeartbeatMethods handles heartbeat.get/set/toggle/test/logs/checklist RPC methods.
type HeartbeatMethods struct {
	hbStore       store.HeartbeatStore
	agentStore    store.AgentStore
	providerStore store.ProviderStore
	eventBus      bus.EventPublisher
	wakeFn        func(uuid.UUID) // triggers immediate heartbeat run
}

func NewHeartbeatMethods(hb store.HeartbeatStore, eventBus bus.EventPublisher) *HeartbeatMethods {
	return &HeartbeatMethods{hbStore: hb, eventBus: eventBus}
}

// SetAgentStore sets the agent store for HEARTBEAT.md read/write via RPC.
func (m *HeartbeatMethods) SetAgentStore(as store.AgentStore) {
	m.agentStore = as
}

// SetProviderStore sets the provider store for resolving provider names to UUIDs.
func (m *HeartbeatMethods) SetProviderStore(ps store.ProviderStore) {
	m.providerStore = ps
}

// SetWakeFn sets the function called when "heartbeat.test" triggers an immediate run.
func (m *HeartbeatMethods) SetWakeFn(fn func(uuid.UUID)) {
	m.wakeFn = fn
}

func (m *HeartbeatMethods) Register(router *gateway.MethodRouter) {
	router.Register(protocol.MethodHeartbeatGet, m.handleGet)
	router.Register(protocol.MethodHeartbeatSet, m.handleSet)
	router.Register(protocol.MethodHeartbeatToggle, m.handleToggle)
	router.Register(protocol.MethodHeartbeatTest, m.handleTest)
	router.Register(protocol.MethodHeartbeatLogs, m.handleLogs)
	router.Register(protocol.MethodHeartbeatChecklistGet, m.handleChecklistGet)
	router.Register(protocol.MethodHeartbeatChecklistSet, m.handleChecklistSet)
	router.Register(protocol.MethodHeartbeatTargets, m.handleTargets)
}

func (m *HeartbeatMethods) handleGet(ctx context.Context, client *gateway.Client, req *protocol.RequestFrame) {
	locale := store.LocaleFromContext(ctx)
	var params struct {
		AgentID string `json:"agentId"`
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

	hb, err := m.hbStore.Get(ctx, agentUUID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			client.SendResponse(protocol.NewOKResponse(req.ID, map[string]any{"heartbeat": nil}))
			return
		}
		client.SendResponse(protocol.NewErrorResponse(req.ID, protocol.ErrInternal, heartbeatInternalErr("get", err)))
		return
	}

	client.SendResponse(protocol.NewOKResponse(req.ID, map[string]any{"heartbeat": hb}))
}

func (m *HeartbeatMethods) handleSet(ctx context.Context, client *gateway.Client, req *protocol.RequestFrame) {
	locale := store.LocaleFromContext(ctx)
	var params struct {
		AgentID         string  `json:"agentId"`
		Enabled         *bool   `json:"enabled"`
		IntervalSec     *int    `json:"intervalSec"`
		Prompt          *string `json:"prompt"`
		ProviderName    *string `json:"providerName"`
		Model           *string `json:"model"`
		IsolatedSession *bool   `json:"isolatedSession"`
		LightContext    *bool   `json:"lightContext"`
		AckMaxChars     *int    `json:"ackMaxChars"`
		MaxRetries      *int    `json:"maxRetries"`
		ActiveHoursStart *string `json:"activeHoursStart"`
		ActiveHoursEnd   *string `json:"activeHoursEnd"`
		Timezone         *string `json:"timezone"`
		Channel          *string `json:"channel"`
		ChatID           *string `json:"chatId"`
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

	// Load existing or create new.
	hb, err := m.hbStore.Get(ctx, agentUUID)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		client.SendResponse(protocol.NewErrorResponse(req.ID, protocol.ErrInternal, heartbeatInternalErr("set.load", err)))
		return
	}
	if hb == nil {
		hb = &store.AgentHeartbeat{
			AgentID:         agentUUID,
			IntervalSec:     1800,
			IsolatedSession: true,
			AckMaxChars:     300,
			MaxRetries:      2,
		}
	}

	if params.Enabled != nil {
		hb.Enabled = *params.Enabled
	}
	if params.IntervalSec != nil {
		if *params.IntervalSec < 300 {
			client.SendResponse(protocol.NewErrorResponse(req.ID, protocol.ErrInvalidRequest, "minimum interval is 300 seconds"))
			return
		}
		hb.IntervalSec = *params.IntervalSec
	}
	if params.Prompt != nil {
		hb.Prompt = params.Prompt
	}
	if params.ProviderName != nil {
		if *params.ProviderName == "" {
			hb.ProviderID = nil // clear override
		} else if m.providerStore != nil {
			prov, err := m.providerStore.GetProviderByName(ctx, *params.ProviderName)
			if err != nil {
				client.SendResponse(protocol.NewErrorResponse(req.ID, protocol.ErrInvalidRequest, "provider not found: "+*params.ProviderName))
				return
			}
			hb.ProviderID = &prov.ID
		}
	}
	if params.Model != nil {
		if *params.Model == "" {
			hb.Model = nil // clear override
		} else {
			hb.Model = params.Model
		}
	}
	if params.IsolatedSession != nil {
		hb.IsolatedSession = *params.IsolatedSession
	}
	if params.LightContext != nil {
		hb.LightContext = *params.LightContext
	}
	if params.AckMaxChars != nil {
		if *params.AckMaxChars < 0 {
			client.SendResponse(protocol.NewErrorResponse(req.ID, protocol.ErrInvalidRequest, "ackMaxChars must be >= 0"))
			return
		}
		hb.AckMaxChars = *params.AckMaxChars
	}
	if params.MaxRetries != nil {
		if *params.MaxRetries < 0 || *params.MaxRetries > 10 {
			client.SendResponse(protocol.NewErrorResponse(req.ID, protocol.ErrInvalidRequest, "maxRetries must be 0-10"))
			return
		}
		hb.MaxRetries = *params.MaxRetries
	}
	if params.ActiveHoursStart != nil {
		hb.ActiveHoursStart = params.ActiveHoursStart
	}
	if params.ActiveHoursEnd != nil {
		hb.ActiveHoursEnd = params.ActiveHoursEnd
	}
	if params.Timezone != nil {
		hb.Timezone = params.Timezone
	}
	if params.Channel != nil {
		hb.Channel = params.Channel
	}
	if params.ChatID != nil {
		hb.ChatID = params.ChatID
	}

	if hb.Enabled && hb.NextRunAt == nil {
		nextRun := time.Now().Add(time.Duration(hb.IntervalSec)*time.Second + store.StaggerOffset(hb.AgentID, hb.IntervalSec))
		hb.NextRunAt = &nextRun
	}

	if err := m.hbStore.Upsert(ctx, hb); err != nil {
		client.SendResponse(protocol.NewErrorResponse(req.ID, protocol.ErrInternal, heartbeatInternalErr("set.upsert", err)))
		return
	}

	client.SendResponse(protocol.NewOKResponse(req.ID, map[string]any{"heartbeat": hb}))
	m.emitCacheInvalidate(hb.AgentID.String())
	emitAudit(m.eventBus, client, "heartbeat.set", "heartbeat", hb.AgentID.String())
}

func (m *HeartbeatMethods) handleToggle(ctx context.Context, client *gateway.Client, req *protocol.RequestFrame) {
	locale := store.LocaleFromContext(ctx)
	var params struct {
		AgentID string `json:"agentId"`
		Enabled bool   `json:"enabled"`
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

	hb, err := m.hbStore.Get(ctx, agentUUID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			client.SendResponse(protocol.NewErrorResponse(req.ID, protocol.ErrNotFound, "heartbeat not configured"))
			return
		}
		client.SendResponse(protocol.NewErrorResponse(req.ID, protocol.ErrInternal, heartbeatInternalErr("op", err)))
		return
	}

	hb.Enabled = params.Enabled
	if params.Enabled && hb.NextRunAt == nil {
		nextRun := time.Now().Add(time.Duration(hb.IntervalSec) * time.Second)
		hb.NextRunAt = &nextRun
	}

	if err := m.hbStore.Upsert(ctx, hb); err != nil {
		client.SendResponse(protocol.NewErrorResponse(req.ID, protocol.ErrInternal, heartbeatInternalErr("op", err)))
		return
	}

	client.SendResponse(protocol.NewOKResponse(req.ID, map[string]any{
		"agentId": params.AgentID,
		"enabled": params.Enabled,
	}))
	m.emitCacheInvalidate(params.AgentID)
	emitAudit(m.eventBus, client, "heartbeat.toggled", "heartbeat", params.AgentID)
}

func (m *HeartbeatMethods) handleTest(ctx context.Context, client *gateway.Client, req *protocol.RequestFrame) {
	locale := store.LocaleFromContext(ctx)
	var params struct {
		AgentID string `json:"agentId"`
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

	if m.wakeFn == nil {
		client.SendResponse(protocol.NewErrorResponse(req.ID, protocol.ErrInternal, "heartbeat ticker not available"))
		return
	}

	client.SendResponse(protocol.NewOKResponse(req.ID, map[string]any{"ok": true}))
	emitAudit(m.eventBus, client, "heartbeat.test", "heartbeat", params.AgentID)

	go func() {
		m.wakeFn(agentUUID)
		slog.Info("heartbeat.test triggered", "agent_id", params.AgentID)
	}()
}

func (m *HeartbeatMethods) handleLogs(ctx context.Context, client *gateway.Client, req *protocol.RequestFrame) {
	locale := store.LocaleFromContext(ctx)
	var params struct {
		AgentID string `json:"agentId"`
		Limit   int    `json:"limit"`
		Offset  int    `json:"offset"`
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

	logs, total, err := m.hbStore.ListLogs(ctx, agentUUID, params.Limit, params.Offset)
	if err != nil {
		client.SendResponse(protocol.NewErrorResponse(req.ID, protocol.ErrInternal, heartbeatInternalErr("op", err)))
		return
	}

	client.SendResponse(protocol.NewOKResponse(req.ID, map[string]any{
		"logs":  logs,
		"total": total,
	}))
}

func (m *HeartbeatMethods) handleChecklistGet(ctx context.Context, client *gateway.Client, req *protocol.RequestFrame) {
	locale := store.LocaleFromContext(ctx)
	var params struct {
		AgentID string `json:"agentId"`
	}
	if req.Params != nil {
		json.Unmarshal(req.Params, &params)
	}
	if params.AgentID == "" {
		client.SendResponse(protocol.NewErrorResponse(req.ID, protocol.ErrInvalidRequest, i18n.T(locale, i18n.MsgRequired, "agentId")))
		return
	}
	if m.agentStore == nil {
		client.SendResponse(protocol.NewErrorResponse(req.ID, protocol.ErrInternal, "agent store not configured"))
		return
	}

	agentUUID, err := uuid.Parse(params.AgentID)
	if err != nil {
		client.SendResponse(protocol.NewErrorResponse(req.ID, protocol.ErrInvalidRequest, "invalid agentId"))
		return
	}

	files, err := m.agentStore.GetAgentContextFiles(ctx, agentUUID)
	if err != nil {
		client.SendResponse(protocol.NewErrorResponse(req.ID, protocol.ErrInternal, heartbeatInternalErr("op", err)))
		return
	}

	var content string
	for _, f := range files {
		if f.FileName == "HEARTBEAT.md" {
			content = f.Content
			break
		}
	}

	client.SendResponse(protocol.NewOKResponse(req.ID, map[string]any{
		"content": content,
	}))
}

func (m *HeartbeatMethods) handleChecklistSet(ctx context.Context, client *gateway.Client, req *protocol.RequestFrame) {
	locale := store.LocaleFromContext(ctx)
	var params struct {
		AgentID string `json:"agentId"`
		Content string `json:"content"`
	}
	if req.Params != nil {
		json.Unmarshal(req.Params, &params)
	}
	if params.AgentID == "" {
		client.SendResponse(protocol.NewErrorResponse(req.ID, protocol.ErrInvalidRequest, i18n.T(locale, i18n.MsgRequired, "agentId")))
		return
	}
	if m.agentStore == nil {
		client.SendResponse(protocol.NewErrorResponse(req.ID, protocol.ErrInternal, "agent store not configured"))
		return
	}

	agentUUID, err := uuid.Parse(params.AgentID)
	if err != nil {
		client.SendResponse(protocol.NewErrorResponse(req.ID, protocol.ErrInvalidRequest, "invalid agentId"))
		return
	}

	if err := m.agentStore.SetAgentContextFile(ctx, agentUUID, "HEARTBEAT.md", params.Content); err != nil {
		client.SendResponse(protocol.NewErrorResponse(req.ID, protocol.ErrInternal, heartbeatInternalErr("op", err)))
		return
	}

	client.SendResponse(protocol.NewOKResponse(req.ID, map[string]any{
		"ok":      true,
		"length":  len([]rune(params.Content)),
	}))
	emitAudit(m.eventBus, client, "heartbeat.checklist.set", "heartbeat", params.AgentID)
}

func (m *HeartbeatMethods) handleTargets(ctx context.Context, client *gateway.Client, req *protocol.RequestFrame) {
	locale := store.LocaleFromContext(ctx)
	var params struct {
		AgentID string `json:"agentId"`
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

	targets, err := m.hbStore.ListDeliveryTargets(ctx, agentUUID)
	if err != nil {
		client.SendResponse(protocol.NewErrorResponse(req.ID, protocol.ErrInternal, heartbeatInternalErr("targets", err)))
		return
	}

	client.SendResponse(protocol.NewOKResponse(req.ID, map[string]any{
		"targets": targets,
	}))
}

// heartbeatInternalErr logs the real error and returns a safe message for the client.
func heartbeatInternalErr(action string, err error) string {
	slog.Error("heartbeat RPC error", "action", action, "error", err)
	return "internal error"
}

func (m *HeartbeatMethods) emitCacheInvalidate(agentID string) {
	m.eventBus.Broadcast(bus.Event{
		Name: protocol.EventCacheInvalidate,
		Payload: bus.CacheInvalidatePayload{
			Kind: bus.CacheKindHeartbeat,
			Key:  agentID,
		},
	})
}
