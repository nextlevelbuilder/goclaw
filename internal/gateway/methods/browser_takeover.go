package methods

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"

	"github.com/nextlevelbuilder/goclaw/internal/browsertakeover"
	"github.com/nextlevelbuilder/goclaw/internal/gateway"
	"github.com/nextlevelbuilder/goclaw/internal/i18n"
	"github.com/nextlevelbuilder/goclaw/internal/store"
	"github.com/nextlevelbuilder/goclaw/pkg/protocol"
)

// BrowserTakeoverMethods lets the user who owns a run finish or dismiss a
// take over request. Every call is bound to the caller's tenant and user.
type BrowserTakeoverMethods struct {
	mgr *browsertakeover.Manager
}

func NewBrowserTakeoverMethods(mgr *browsertakeover.Manager) *BrowserTakeoverMethods {
	return &BrowserTakeoverMethods{mgr: mgr}
}

func (m *BrowserTakeoverMethods) Register(router *gateway.MethodRouter) {
	router.Register(protocol.MethodBrowserTakeoverDone, m.handle(false))
	router.Register(protocol.MethodBrowserTakeoverCancel, m.handle(true))
}

func (m *BrowserTakeoverMethods) handle(cancel bool) gateway.MethodHandler {
	return func(ctx context.Context, client *gateway.Client, req *protocol.RequestFrame) {
		locale := store.LocaleFromContext(ctx)
		var params struct {
			ID string `json:"id"`
		}
		if req.Params != nil {
			_ = json.Unmarshal(req.Params, &params)
		}
		if params.ID == "" {
			client.SendResponse(protocol.NewErrorResponse(req.ID, protocol.ErrInvalidRequest, i18n.T(locale, i18n.MsgRequired, "id")))
			return
		}
		var err error
		if cancel {
			err = m.mgr.Cancel(client.TenantID(), client.UserID(), params.ID)
		} else {
			err = m.mgr.Done(client.TenantID(), client.UserID(), params.ID)
		}
		switch {
		case err == nil:
			client.SendResponse(protocol.NewOKResponse(req.ID, map[string]any{"ok": true}))
		case errors.Is(err, browsertakeover.ErrRequestNotFound):
			client.SendResponse(protocol.NewErrorResponse(req.ID, protocol.ErrNotFound, i18n.T(locale, i18n.MsgBrowserTakeoverNotFound)))
		default:
			slog.Warn("browser.takeover.answer_failed")
			client.SendResponse(protocol.NewErrorResponse(req.ID, protocol.ErrInternal, i18n.T(locale, i18n.MsgInternalError, "browser take over")))
		}
	}
}
