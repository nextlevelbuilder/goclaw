package methods

import (
	"context"
	"encoding/json"

	"github.com/nextlevelbuilder/goclaw/internal/channels"
	"github.com/nextlevelbuilder/goclaw/internal/config"
	"github.com/nextlevelbuilder/goclaw/internal/gateway"
	"github.com/nextlevelbuilder/goclaw/internal/i18n"
	"github.com/nextlevelbuilder/goclaw/internal/store"
	"github.com/nextlevelbuilder/goclaw/pkg/protocol"
)

// ChatBehaviorMethods handles dashboard previews for channel delivery behavior.
type ChatBehaviorMethods struct {
	cfg        *config.Config
	channelMgr *channels.Manager
}

func NewChatBehaviorMethods(cfg *config.Config, channelMgr *channels.Manager) *ChatBehaviorMethods {
	return &ChatBehaviorMethods{cfg: cfg, channelMgr: channelMgr}
}

func (m *ChatBehaviorMethods) Register(router *gateway.MethodRouter) {
	router.Register(protocol.MethodChatBehaviorPreview, m.requireMasterScope(m.requireOwner(m.handlePreview)))
}

func (m *ChatBehaviorMethods) requireOwner(next gateway.MethodHandler) gateway.MethodHandler {
	return func(ctx context.Context, client *gateway.Client, req *protocol.RequestFrame) {
		if !client.IsOwner() {
			locale := store.LocaleFromContext(ctx)
			client.SendResponse(protocol.NewErrorResponse(
				req.ID,
				protocol.ErrUnauthorized,
				i18n.T(locale, i18n.MsgPermissionDenied, req.Method),
			))
			return
		}
		next(ctx, client, req)
	}
}

func (m *ChatBehaviorMethods) requireMasterScope(next gateway.MethodHandler) gateway.MethodHandler {
	return func(ctx context.Context, client *gateway.Client, req *protocol.RequestFrame) {
		if !store.IsMasterScope(ctx) {
			locale := store.LocaleFromContext(ctx)
			client.SendResponse(protocol.NewErrorResponse(req.ID, protocol.ErrUnauthorized, i18n.T(locale, i18n.MsgConfigMasterScopeOnly)))
			return
		}
		next(ctx, client, req)
	}
}

func (m *ChatBehaviorMethods) handlePreview(_ context.Context, client *gateway.Client, req *protocol.RequestFrame) {
	var params struct {
		Channel      string                     `json:"channel"`
		Content      string                     `json:"content"`
		IsStreaming  bool                       `json:"isStreaming"`
		HasToolCalls bool                       `json:"hasToolCalls"`
		Config       *config.ChatBehaviorConfig `json:"config"`
	}
	if req.Params != nil {
		_ = json.Unmarshal(req.Params, &params)
	}

	var resolved channels.ResolvedChatBehavior
	if params.Config != nil {
		resolved = channels.ResolveChatBehavior(params.Config, nil)
	} else if m.channelMgr != nil {
		resolved = m.channelMgr.ResolveChatBehavior(params.Channel, m.cfg.Gateway.ChatBehavior)
	} else {
		resolved = channels.ResolveChatBehavior(m.cfg.Gateway.ChatBehavior, nil)
	}
	preview := channels.PreviewResolvedChatBehavior(resolved, channels.ChatBehaviorPreviewOptions{
		Content:      params.Content,
		IsStreaming:  params.IsStreaming,
		HasToolCalls: params.HasToolCalls,
	})
	client.SendResponse(protocol.NewOKResponse(req.ID, preview))
}
