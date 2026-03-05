package methods

import (
	"context"
	"encoding/json"

	"github.com/nextlevelbuilder/goclaw/internal/bus"
	"github.com/nextlevelbuilder/goclaw/internal/gateway"
	"github.com/nextlevelbuilder/goclaw/internal/store"
	"github.com/nextlevelbuilder/goclaw/pkg/protocol"
)

// PairingApproveCallback is called after a pairing is approved.
// channel is the channel name (e.g., "telegram"), chatID is the chat to notify.
type PairingApproveCallback func(ctx context.Context, channel, chatID string)

// PairingMethods handles device.pair.request, device.pair.approve, device.pair.list, device.pair.revoke.
type PairingMethods struct {
	service   store.PairingStore
	msgBus    *bus.MessageBus
	onApprove PairingApproveCallback
}

func NewPairingMethods(service store.PairingStore, msgBus *bus.MessageBus) *PairingMethods {
	return &PairingMethods{service: service, msgBus: msgBus}
}

// SetOnApprove sets a callback that fires after a pairing is approved.
func (m *PairingMethods) SetOnApprove(cb PairingApproveCallback) {
	m.onApprove = cb
}

func (m *PairingMethods) Register(router *gateway.MethodRouter) {
	router.Register(protocol.MethodPairingRequest, m.handleRequest)
	router.Register(protocol.MethodPairingApprove, m.handleApprove)
	router.Register(protocol.MethodPairingDeny, m.handleDeny)
	router.Register(protocol.MethodPairingList, m.handleList)
	router.Register(protocol.MethodPairingRevoke, m.handleRevoke)
	router.Register(protocol.MethodBrowserPairingStatus, m.handleBrowserPairingStatus)
}

func (m *PairingMethods) handleRequest(_ context.Context, client *gateway.Client, req *protocol.RequestFrame) {
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
		client.SendResponse(protocol.NewErrorResponse(req.ID, protocol.ErrInvalidRequest, "senderId and channel are required"))
		return
	}

	if params.AccountID == "" {
		params.AccountID = "default"
	}

	code, err := m.service.RequestPairing(params.SenderID, params.Channel, params.ChatID, params.AccountID)
	if err != nil {
		client.SendResponse(protocol.NewErrorResponse(req.ID, protocol.ErrInvalidRequest, err.Error()))
		return
	}

	client.SendResponse(protocol.NewOKResponse(req.ID, map[string]interface{}{
		"code": code,
	}))
}

func (m *PairingMethods) handleApprove(ctx context.Context, client *gateway.Client, req *protocol.RequestFrame) {
	var params struct {
		Code       string `json:"code"`
		ApprovedBy string `json:"approvedBy"`
	}
	if req.Params != nil {
		json.Unmarshal(req.Params, &params)
	}

	if params.Code == "" {
		client.SendResponse(protocol.NewErrorResponse(req.ID, protocol.ErrInvalidRequest, "code is required"))
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
		go m.onApprove(context.Background(), paired.Channel, paired.ChatID)
	}

	client.SendResponse(protocol.NewOKResponse(req.ID, map[string]interface{}{
		"paired": paired,
	}))
}

func (m *PairingMethods) handleDeny(_ context.Context, client *gateway.Client, req *protocol.RequestFrame) {
	var params struct {
		Code string `json:"code"`
	}
	if req.Params != nil {
		json.Unmarshal(req.Params, &params)
	}

	if params.Code == "" {
		client.SendResponse(protocol.NewErrorResponse(req.ID, protocol.ErrInvalidRequest, "code is required"))
		return
	}

	if err := m.service.DenyPairing(params.Code); err != nil {
		client.SendResponse(protocol.NewErrorResponse(req.ID, protocol.ErrNotFound, err.Error()))
		return
	}

	client.SendResponse(protocol.NewOKResponse(req.ID, map[string]interface{}{
		"denied": true,
	}))
}

func (m *PairingMethods) handleList(_ context.Context, client *gateway.Client, req *protocol.RequestFrame) {
	pending := m.service.ListPending()
	paired := m.service.ListPaired()

	client.SendResponse(protocol.NewOKResponse(req.ID, map[string]interface{}{
		"pending": pending,
		"paired":  paired,
	}))
}

func (m *PairingMethods) handleRevoke(_ context.Context, client *gateway.Client, req *protocol.RequestFrame) {
	var params struct {
		SenderID string `json:"senderId"`
		Channel  string `json:"channel"`
	}
	if req.Params != nil {
		json.Unmarshal(req.Params, &params)
	}

	if params.SenderID == "" || params.Channel == "" {
		client.SendResponse(protocol.NewErrorResponse(req.ID, protocol.ErrInvalidRequest, "senderId and channel are required"))
		return
	}

	if err := m.service.RevokePairing(params.SenderID, params.Channel); err != nil {
		client.SendResponse(protocol.NewErrorResponse(req.ID, protocol.ErrNotFound, err.Error()))
		return
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

	client.SendResponse(protocol.NewOKResponse(req.ID, map[string]interface{}{
		"revoked": true,
	}))
}

// handleBrowserPairingStatus lets a pending browser client check if its pairing code has been approved.
// Called by unauthenticated clients during the browser pairing flow.
func (m *PairingMethods) handleBrowserPairingStatus(_ context.Context, client *gateway.Client, req *protocol.RequestFrame) {
	var params struct {
		SenderID string `json:"sender_id"`
	}
	if req.Params != nil {
		json.Unmarshal(req.Params, &params)
	}

	if params.SenderID == "" {
		client.SendResponse(protocol.NewErrorResponse(req.ID, protocol.ErrInvalidRequest, "sender_id is required"))
		return
	}

	if m.service.IsPaired(params.SenderID, "browser") {
		client.SendResponse(protocol.NewOKResponse(req.ID, map[string]interface{}{
			"status": "approved",
		}))
		return
	}

	// Check if the pairing request still exists (not expired)
	pending := m.service.ListPending()
	for _, p := range pending {
		if p.SenderID == params.SenderID && p.Channel == "browser" {
			client.SendResponse(protocol.NewOKResponse(req.ID, map[string]interface{}{
				"status": "pending",
			}))
			return
		}
	}

	client.SendResponse(protocol.NewOKResponse(req.ID, map[string]interface{}{
		"status": "expired",
	}))
}
