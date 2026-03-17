package methods

import (
	"context"
	"encoding/json"
	"log/slog"

	"github.com/nextlevelbuilder/goclaw/internal/bus"
	"github.com/nextlevelbuilder/goclaw/internal/gateway"
	"github.com/nextlevelbuilder/goclaw/internal/i18n"
	"github.com/nextlevelbuilder/goclaw/internal/store"
	"github.com/nextlevelbuilder/goclaw/pkg/protocol"
)

// PairingApproveCallback is called after a pairing is approved.
// channel is the channel name (e.g., "telegram"), chatID is the chat to notify,
// senderID identifies the paired entity (e.g., "group:XXXX" for group pairings).
type PairingApproveCallback func(ctx context.Context, channel, chatID, senderID string)

// PairingMethods handles device.pair.request, device.pair.approve, device.pair.list, device.pair.revoke.
type PairingMethods struct {
	service     store.PairingStore
	msgBus      *bus.MessageBus
	onApprove   PairingApproveCallback
	broadcaster func(protocol.EventFrame)
	rateLimiter *gateway.RateLimiter
}

func NewPairingMethods(service store.PairingStore, msgBus *bus.MessageBus, rateLimiter *gateway.RateLimiter) *PairingMethods {
	return &PairingMethods{service: service, msgBus: msgBus, rateLimiter: rateLimiter}
}

// SetOnApprove sets a callback that fires after a pairing is approved.
func (m *PairingMethods) SetOnApprove(cb PairingApproveCallback) {
	m.onApprove = cb
}

// SetBroadcaster sets a function to broadcast events to all WS clients.
func (m *PairingMethods) SetBroadcaster(fn func(protocol.EventFrame)) {
	m.broadcaster = fn
}

func (m *PairingMethods) Register(router *gateway.MethodRouter) {
	router.Register(protocol.MethodPairingRequest, m.handleRequest)
	router.Register(protocol.MethodPairingApprove, m.handleApprove)
	router.Register(protocol.MethodPairingDeny, m.handleDeny)
	router.Register(protocol.MethodPairingList, m.handleList)
	router.Register(protocol.MethodPairingRevoke, m.handleRevoke)
	router.Register(protocol.MethodBrowserPairingStatus, m.handleBrowserPairingStatus)
}

func (m *PairingMethods) handleRequest(ctx context.Context, client *gateway.Client, req *protocol.RequestFrame) {
	locale := store.LocaleFromContext(ctx)
	var params struct {
		SenderID  string `json:"senderId"`
		Channel   string `json:"channel"`
		ChatID    string `json:"chatId"`
		AccountID string `json:"accountId"`
	}
	if req.Params != nil {
		json.Unmarshal(req.Params, &params)
	}

	if params.SenderID == "" || params.Channel == "" {
		client.SendResponse(protocol.NewErrorResponse(req.ID, protocol.ErrInvalidRequest, i18n.T(locale, i18n.MsgSenderChannelRequired)))
		return
	}

	if params.AccountID == "" {
		params.AccountID = "default"
	}

	code, err := m.service.RequestPairing(params.SenderID, params.Channel, params.ChatID, params.AccountID, nil)
	if err != nil {
		client.SendResponse(protocol.NewErrorResponse(req.ID, protocol.ErrInvalidRequest, err.Error()))
		return
	}

	client.SendResponse(protocol.NewOKResponse(req.ID, map[string]any{
		"code": code,
	}))
}

func (m *PairingMethods) handleApprove(ctx context.Context, client *gateway.Client, req *protocol.RequestFrame) {
	locale := store.LocaleFromContext(ctx)
	var params struct {
		Code       string `json:"code"`
		ApprovedBy string `json:"approvedBy"`
	}
	if req.Params != nil {
		json.Unmarshal(req.Params, &params)
	}

	if params.Code == "" {
		client.SendResponse(protocol.NewErrorResponse(req.ID, protocol.ErrInvalidRequest, i18n.T(locale, i18n.MsgCodeRequired)))
		return
	}
	if params.ApprovedBy == "" {
		params.ApprovedBy = "operator"
	}

	paired, err := m.service.ApprovePairing(params.Code, params.ApprovedBy)
	if err != nil {
		client.SendResponse(protocol.NewErrorResponse(req.ID, protocol.ErrNotFound, err.Error()))
		return
	}

	// Notify the user via channel (matching TS notifyPairingApproved).
	// Use Background context: the CLI client may disconnect before the notification is sent.
	if m.onApprove != nil && paired != nil {
		go m.onApprove(context.Background(), paired.Channel, paired.ChatID, paired.SenderID)
	}

	if m.broadcaster != nil {
		m.broadcaster(*protocol.NewEvent(protocol.EventDevicePairRes, map[string]any{"action": "approved"}))
	}

	emitAudit(m.msgBus, client, "pairing.approved", "pairing", params.Code)
	client.SendResponse(protocol.NewOKResponse(req.ID, map[string]any{
		"paired": paired,
	}))
}

func (m *PairingMethods) handleDeny(ctx context.Context, client *gateway.Client, req *protocol.RequestFrame) {
	locale := store.LocaleFromContext(ctx)
	var params struct {
		Code string `json:"code"`
	}
	if req.Params != nil {
		json.Unmarshal(req.Params, &params)
	}

	if params.Code == "" {
		client.SendResponse(protocol.NewErrorResponse(req.ID, protocol.ErrInvalidRequest, i18n.T(locale, i18n.MsgCodeRequired)))
		return
	}

	if err := m.service.DenyPairing(params.Code); err != nil {
		client.SendResponse(protocol.NewErrorResponse(req.ID, protocol.ErrNotFound, err.Error()))
		return
	}

	if m.broadcaster != nil {
		m.broadcaster(*protocol.NewEvent(protocol.EventDevicePairRes, map[string]any{"action": "denied"}))
	}

	emitAudit(m.msgBus, client, "pairing.denied", "pairing", params.Code)
	client.SendResponse(protocol.NewOKResponse(req.ID, map[string]any{
		"denied": true,
	}))
}

func (m *PairingMethods) handleList(_ context.Context, client *gateway.Client, req *protocol.RequestFrame) {
	pending := m.service.ListPending()
	paired := m.service.ListPaired()

	client.SendResponse(protocol.NewOKResponse(req.ID, map[string]any{
		"pending": pending,
		"paired":  paired,
	}))
}

func (m *PairingMethods) handleRevoke(ctx context.Context, client *gateway.Client, req *protocol.RequestFrame) {
	locale := store.LocaleFromContext(ctx)
	var params struct {
		SenderID string `json:"senderId"`
		Channel  string `json:"channel"`
	}
	if req.Params != nil {
		json.Unmarshal(req.Params, &params)
	}

	if params.SenderID == "" || params.Channel == "" {
		client.SendResponse(protocol.NewErrorResponse(req.ID, protocol.ErrInvalidRequest, i18n.T(locale, i18n.MsgSenderChannelRequired)))
		return
	}

	if err := m.service.RevokePairing(params.SenderID, params.Channel); err != nil {
		client.SendResponse(protocol.NewErrorResponse(req.ID, protocol.ErrNotFound, err.Error()))
		return
	}

	if m.broadcaster != nil {
		m.broadcaster(*protocol.NewEvent(protocol.EventDevicePairRes, map[string]any{"action": "revoked"}))
	}

	// Broadcast revocation so the server can force-disconnect the active session.
	if m.msgBus != nil {
		m.msgBus.Broadcast(bus.Event{
			Name: bus.EventPairingRevoked,
			Payload: bus.PairingRevokedPayload{
				SenderID: params.SenderID,
				Channel:  params.Channel,
			},
		})
	}

	emitAudit(m.msgBus, client, "pairing.revoked", "pairing", params.SenderID)
	client.SendResponse(protocol.NewOKResponse(req.ID, map[string]any{
		"revoked": true,
	}))
}

// handleBrowserPairingStatus lets a pending browser client check if its pairing code has been approved.
// Called by unauthenticated clients during the browser pairing flow.
func (m *PairingMethods) handleBrowserPairingStatus(ctx context.Context, client *gateway.Client, req *protocol.RequestFrame) {
	// Rate-limit unauthenticated polling to prevent sender_id enumeration.
	if m.rateLimiter != nil && m.rateLimiter.Enabled() && !m.rateLimiter.Allow("pairing:"+client.RemoteAddr()) {
		client.SendResponse(protocol.NewErrorResponse(req.ID, protocol.ErrResourceExhausted, "rate limited"))
		return
	}
	locale := store.LocaleFromContext(ctx)
	var params struct {
		SenderID string `json:"sender_id"`
	}
	if req.Params != nil {
		json.Unmarshal(req.Params, &params)
	}

	if params.SenderID == "" {
		client.SendResponse(protocol.NewErrorResponse(req.ID, protocol.ErrInvalidRequest, i18n.T(locale, i18n.MsgSenderIDRequired)))
		return
	}

	paired, pairErr := m.service.IsPaired(params.SenderID, "browser")
	if pairErr != nil {
		slog.Warn("security.pairing_check_failed", "sender_id", params.SenderID, "error", pairErr)
	}
	if paired {
		client.SendResponse(protocol.NewOKResponse(req.ID, map[string]any{
			"status": "approved",
		}))
		return
	}

	// Check if the pairing request still exists (not expired)
	pending := m.service.ListPending()
	for _, p := range pending {
		if p.SenderID == params.SenderID && p.Channel == "browser" {
			client.SendResponse(protocol.NewOKResponse(req.ID, map[string]any{
				"status": "pending",
			}))
			return
		}
	}

	client.SendResponse(protocol.NewOKResponse(req.ID, map[string]any{
		"status": "expired",
	}))
}
