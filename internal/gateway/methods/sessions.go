package methods

import (
	"context"
	"encoding/json"

	"github.com/nextlevelbuilder/goclaw/internal/bus"
	"github.com/nextlevelbuilder/goclaw/internal/config"
	"github.com/nextlevelbuilder/goclaw/internal/gateway"
	httpapi "github.com/nextlevelbuilder/goclaw/internal/http"
	"github.com/nextlevelbuilder/goclaw/internal/i18n"
	"github.com/nextlevelbuilder/goclaw/internal/store"
	"github.com/nextlevelbuilder/goclaw/pkg/protocol"
)

// SessionsMethods handles sessions.list, sessions.preview, sessions.patch, sessions.delete, sessions.reset.
type SessionsMethods struct {
	sessions store.SessionStore
	eventBus bus.EventPublisher
	cfg      *config.Config
}

func NewSessionsMethods(sess store.SessionStore, eventBus bus.EventPublisher, cfg *config.Config) *SessionsMethods {
	return &SessionsMethods{sessions: sess, eventBus: eventBus, cfg: cfg}
}

func (m *SessionsMethods) Register(router *gateway.MethodRouter) {
	router.Register(protocol.MethodSessionsList, m.handleList)
	router.Register(protocol.MethodSessionsPreview, m.handlePreview)
	router.Register(protocol.MethodSessionsPatch, m.handlePatch)
	router.Register(protocol.MethodSessionsDelete, m.handleDelete)
	router.Register(protocol.MethodSessionsReset, m.handleReset)
}

type sessionsListParams struct {
	AgentID string `json:"agentId"`
	Channel string `json:"channel"` // optional: filter by channel prefix ("ws", "telegram")
	Limit   int    `json:"limit"`
	Offset  int    `json:"offset"`
}

func (m *SessionsMethods) handleList(ctx context.Context, client *gateway.Client, req *protocol.RequestFrame) {
	var params sessionsListParams
	if req.Params != nil {
		json.Unmarshal(req.Params, &params)
	}

	if params.Limit <= 0 {
		params.Limit = 20
	}

	opts := store.SessionListOpts{
		AgentID:  params.AgentID,
		Channel:  params.Channel,
		Limit:    params.Limit,
		Offset:   params.Offset,
		TenantID: store.TenantIDFromContext(ctx),
	}
	// Role-based filtering: admins/owners see all sessions; regular users see only their own.
	// Tenant scope is always applied above — admin sees all sessions within the tenant.
	if !canSeeAll(client.Role(), m.cfg.Gateway.OwnerIDs, client.UserID()) {
		opts.UserID = client.UserID()
	}

	result := m.sessions.ListPagedRich(ctx, opts)
	client.SendResponse(protocol.NewOKResponse(req.ID, map[string]any{
		"sessions": result.Sessions,
		"total":    result.Total,
		"limit":    params.Limit,
		"offset":   params.Offset,
	}))
}

type sessionKeyParams struct {
	Key string `json:"key"`
}

func (m *SessionsMethods) handlePreview(ctx context.Context, client *gateway.Client, req *protocol.RequestFrame) {
	locale := store.LocaleFromContext(ctx)
	var params sessionKeyParams
	if err := json.Unmarshal(req.Params, &params); err != nil {
		client.SendResponse(protocol.NewErrorResponse(req.ID, protocol.ErrInvalidRequest, i18n.T(locale, i18n.MsgInvalidJSON)))
		return
	}

	if !canSeeAll(client.Role(), m.cfg.Gateway.OwnerIDs, client.UserID()) {
		sess := m.sessions.Get(ctx, params.Key)
		if sess == nil {
			client.SendResponse(protocol.NewErrorResponse(req.ID, protocol.ErrNotFound, i18n.T(locale, i18n.MsgNotFound, "session", params.Key)))
			return
		}
		if sess.UserID != client.UserID() {
			client.SendResponse(protocol.NewErrorResponse(req.ID, protocol.ErrUnauthorized, i18n.T(locale, i18n.MsgPermissionDenied, "session")))
			return
		}
	}

	history := m.sessions.GetHistory(ctx, params.Key)
	summary := m.sessions.GetSummary(ctx, params.Key)

	// Sign file URLs before delivery — sessions store clean paths.
	for i := range history {
		history[i].Content = httpapi.SignFileURLs(history[i].Content, httpapi.FileSigningKey())
	}
	summary = httpapi.SignFileURLs(summary, httpapi.FileSigningKey())

	client.SendResponse(protocol.NewOKResponse(req.ID, map[string]any{
		"key":      params.Key,
		"messages": history,
		"summary":  summary,
	}))
}

// handlePatch updates session metadata fields.
// Matching TS sessions.patch (src/gateway/server-methods/sessions.ts:237-287).
func (m *SessionsMethods) handlePatch(ctx context.Context, client *gateway.Client, req *protocol.RequestFrame) {
	locale := store.LocaleFromContext(ctx)
	var params struct {
		Key      string            `json:"key"`
		Label    *string           `json:"label,omitempty"`
		Model    *string           `json:"model,omitempty"`
		Metadata map[string]string `json:"metadata,omitempty"`
	}
	if err := json.Unmarshal(req.Params, &params); err != nil {
		client.SendResponse(protocol.NewErrorResponse(req.ID, protocol.ErrInvalidRequest, i18n.T(locale, i18n.MsgInvalidJSON)))
		return
	}

	if params.Key == "" {
		client.SendResponse(protocol.NewErrorResponse(req.ID, protocol.ErrInvalidRequest, i18n.T(locale, i18n.MsgRequired, "key")))
		return
	}

	if !canSeeAll(client.Role(), m.cfg.Gateway.OwnerIDs, client.UserID()) {
		sess := m.sessions.Get(ctx, params.Key)
		if sess == nil {
			client.SendResponse(protocol.NewErrorResponse(req.ID, protocol.ErrNotFound, i18n.T(locale, i18n.MsgNotFound, "session", params.Key)))
			return
		}
		if sess.UserID != client.UserID() {
			client.SendResponse(protocol.NewErrorResponse(req.ID, protocol.ErrUnauthorized, i18n.T(locale, i18n.MsgPermissionDenied, "session")))
			return
		}
	}

	// Apply label patch
	if params.Label != nil {
		m.sessions.SetLabel(ctx, params.Key, *params.Label)
	}

	// Apply model patch
	if params.Model != nil {
		m.sessions.UpdateMetadata(ctx, params.Key, *params.Model, "", "")
	}

	// Apply metadata patch
	if len(params.Metadata) > 0 {
		m.sessions.SetSessionMetadata(ctx, params.Key, params.Metadata)
	}

	// Save changes to DB
	m.sessions.Save(ctx, params.Key)

	client.SendResponse(protocol.NewOKResponse(req.ID, map[string]any{
		"ok":  true,
		"key": params.Key,
	}))
	emitAudit(m.eventBus, client, "session.patched", "session", params.Key)
}

func (m *SessionsMethods) handleDelete(ctx context.Context, client *gateway.Client, req *protocol.RequestFrame) {
	locale := store.LocaleFromContext(ctx)
	var params sessionKeyParams
	if err := json.Unmarshal(req.Params, &params); err != nil {
		client.SendResponse(protocol.NewErrorResponse(req.ID, protocol.ErrInvalidRequest, i18n.T(locale, i18n.MsgInvalidJSON)))
		return
	}

	if !canSeeAll(client.Role(), m.cfg.Gateway.OwnerIDs, client.UserID()) {
		sess := m.sessions.Get(ctx, params.Key)
		if sess == nil {
			client.SendResponse(protocol.NewErrorResponse(req.ID, protocol.ErrNotFound, i18n.T(locale, i18n.MsgNotFound, "session", params.Key)))
			return
		}
		if sess.UserID != client.UserID() {
			client.SendResponse(protocol.NewErrorResponse(req.ID, protocol.ErrUnauthorized, i18n.T(locale, i18n.MsgPermissionDenied, "session")))
			return
		}
	}

	if err := m.sessions.Delete(ctx, params.Key); err != nil {
		client.SendResponse(protocol.NewErrorResponse(req.ID, protocol.ErrInternal, err.Error()))
		return
	}

	client.SendResponse(protocol.NewOKResponse(req.ID, map[string]any{
		"ok": true,
	}))
	emitAudit(m.eventBus, client, "session.deleted", "session", params.Key)
}

func (m *SessionsMethods) handleReset(ctx context.Context, client *gateway.Client, req *protocol.RequestFrame) {
	locale := store.LocaleFromContext(ctx)
	var params sessionKeyParams
	if err := json.Unmarshal(req.Params, &params); err != nil {
		client.SendResponse(protocol.NewErrorResponse(req.ID, protocol.ErrInvalidRequest, i18n.T(locale, i18n.MsgInvalidJSON)))
		return
	}

	if !canSeeAll(client.Role(), m.cfg.Gateway.OwnerIDs, client.UserID()) {
		sess := m.sessions.Get(ctx, params.Key)
		if sess == nil {
			client.SendResponse(protocol.NewErrorResponse(req.ID, protocol.ErrNotFound, i18n.T(locale, i18n.MsgNotFound, "session", params.Key)))
			return
		}
		if sess.UserID != client.UserID() {
			client.SendResponse(protocol.NewErrorResponse(req.ID, protocol.ErrUnauthorized, i18n.T(locale, i18n.MsgPermissionDenied, "session")))
			return
		}
	}

	m.sessions.Reset(ctx, params.Key)

	client.SendResponse(protocol.NewOKResponse(req.ID, map[string]any{
		"ok": true,
	}))
	emitAudit(m.eventBus, client, "session.reset", "session", params.Key)
}
