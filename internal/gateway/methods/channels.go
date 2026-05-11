package methods

import (
	"context"
	"encoding/json"

	"github.com/nextlevelbuilder/goclaw/internal/channels"
	"github.com/nextlevelbuilder/goclaw/internal/gateway"
	"github.com/nextlevelbuilder/goclaw/internal/i18n"
	"github.com/nextlevelbuilder/goclaw/internal/store"
	"github.com/nextlevelbuilder/goclaw/pkg/protocol"
)

// ChannelsMethods handles channels.list, channels.status, channels.toggle.
type ChannelsMethods struct {
	manager *channels.Manager
}

func NewChannelsMethods(manager *channels.Manager) *ChannelsMethods {
	return &ChannelsMethods{manager: manager}
}

func (m *ChannelsMethods) Register(router *gateway.MethodRouter) {
	router.Register(protocol.MethodChannelsList, m.handleList)
	router.Register(protocol.MethodChannelsStatus, m.handleStatus)
	router.Register(protocol.MethodChannelsToggle, m.handleToggle)
}

func (m *ChannelsMethods) handleList(_ context.Context, client *gateway.Client, req *protocol.RequestFrame) {
	enabled := m.manager.GetEnabledChannels()

	client.SendResponse(protocol.NewOKResponse(req.ID, map[string]any{
		"channels": enabled,
	}))
}

func (m *ChannelsMethods) handleStatus(_ context.Context, client *gateway.Client, req *protocol.RequestFrame) {
	status := m.manager.GetStatus()

	client.SendResponse(protocol.NewOKResponse(req.ID, map[string]any{
		"channels": status,
	}))
}

func (m *ChannelsMethods) handleToggle(ctx context.Context, client *gateway.Client, req *protocol.RequestFrame) {
	locale := store.LocaleFromContext(ctx)

	// Parse params
	var params struct {
		Channel string `json:"channel"`
		Enabled bool   `json:"enabled"`
	}
	if err := json.Unmarshal(req.Params, &params); err != nil {
		client.SendResponse(protocol.NewErrorResponse(req.ID, protocol.ErrInvalidRequest, i18n.T(locale, i18n.MsgInvalidJSON)))
		return
	}

	if params.Channel == "" {
		client.SendResponse(protocol.NewErrorResponse(req.ID, protocol.ErrInvalidRequest, "channel is required"))
		return
	}

	// Check if channel exists
	channel, ok := m.manager.GetChannel(params.Channel)
	if !ok {
		client.SendResponse(protocol.NewErrorResponse(req.ID, protocol.ErrNotFound, "channel not found"))
		return
	}

	// Check current state
	isRunning := channel.IsRunning()

	// If already in desired state, return success
	if params.Enabled && isRunning {
		client.SendResponse(protocol.NewOKResponse(req.ID, map[string]any{
			"channel": params.Channel,
			"enabled": true,
			"status":  "already_running",
		}))
		return
	}
	if !params.Enabled && !isRunning {
		client.SendResponse(protocol.NewOKResponse(req.ID, map[string]any{
			"channel": params.Channel,
			"enabled": false,
			"status":  "already_stopped",
		}))
		return
	}

	// Toggle the channel
	var err error
	if params.Enabled {
		err = m.manager.StartChannel(ctx, params.Channel)
	} else {
		err = m.manager.StopChannel(ctx, params.Channel)
	}

	if err != nil {
		client.SendResponse(protocol.NewErrorResponse(req.ID, protocol.ErrInternal, err.Error()))
		return
	}

	client.SendResponse(protocol.NewOKResponse(req.ID, map[string]any{
		"channel": params.Channel,
		"enabled": params.Enabled,
		"status":  "ok",
	}))
}
