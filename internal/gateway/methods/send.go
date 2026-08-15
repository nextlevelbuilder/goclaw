package methods

import (
	"context"
	"encoding/json"

	"github.com/nextlevelbuilder/goclaw/internal/bus"
	"github.com/nextlevelbuilder/goclaw/internal/gateway"
	"github.com/nextlevelbuilder/goclaw/internal/i18n"
	"github.com/nextlevelbuilder/goclaw/internal/store"
	"github.com/nextlevelbuilder/goclaw/pkg/protocol"
)

// SendMethods handles the "send" RPC for routing outbound messages to channels.
// Matching TS src/gateway/server-methods/send.ts.
type SendMethods struct {
	msgBus *bus.MessageBus
}

func NewSendMethods(msgBus *bus.MessageBus) *SendMethods {
	return &SendMethods{msgBus: msgBus}
}

func (m *SendMethods) Register(router *gateway.MethodRouter) {
	router.Register(protocol.MethodSend, m.handleSend)
}

func (m *SendMethods) handleSend(ctx context.Context, client *gateway.Client, req *protocol.RequestFrame) {
	locale := store.LocaleFromContext(ctx)
	var params struct {
		Channel string `json:"channel"`
		To      string `json:"to"`
		Message string `json:"message"`
	}
	if req.Params != nil {
		json.Unmarshal(req.Params, &params)
	}

	if params.Channel == "" {
		client.SendResponse(protocol.NewErrorResponse(req.ID, protocol.ErrInvalidRequest, i18n.T(locale, i18n.MsgRequired, "channel")))
		return
	}
	if params.To == "" {
		client.SendResponse(protocol.NewErrorResponse(req.ID, protocol.ErrInvalidRequest, i18n.T(locale, i18n.MsgRequired, "to")))
		return
	}
	if params.Message == "" {
		client.SendResponse(protocol.NewErrorResponse(req.ID, protocol.ErrInvalidRequest, i18n.T(locale, i18n.MsgMsgRequired)))
		return
	}

	m.msgBus.PublishOutbound(bus.OutboundMessage{
		Channel: params.Channel,
		ChatID:  params.To,
		Content: params.Message,
	})

	client.SendResponse(protocol.NewOKResponse(req.ID, map[string]any{
		"ok":      true,
		"channel": params.Channel,
		"to":      params.To,
	}))
}
