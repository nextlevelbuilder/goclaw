package methods

import (
	"context"
	"encoding/json"
	"log/slog"

	"github.com/nextlevelbuilder/goclaw/internal/config"
	"github.com/nextlevelbuilder/goclaw/internal/gateway"
	"github.com/nextlevelbuilder/goclaw/internal/i18n"
	"github.com/nextlevelbuilder/goclaw/internal/store"
	"github.com/nextlevelbuilder/goclaw/pkg/protocol"
)

// RunTimelineMethods handles archived run timeline reads.
type RunTimelineMethods struct {
	timeline store.RunTimelineStore
	cfg      *config.Config
}

func NewRunTimelineMethods(timeline store.RunTimelineStore, cfg *config.Config) *RunTimelineMethods {
	return &RunTimelineMethods{timeline: timeline, cfg: cfg}
}

func (m *RunTimelineMethods) Register(router *gateway.MethodRouter) {
	router.Register(protocol.MethodRunTimelineGet, m.handleGet)
}

type runTimelineGetParams struct {
	RunID      string `json:"runId"`
	SessionKey string `json:"sessionKey"`
	Limit      int    `json:"limit"`
	Offset     int    `json:"offset"`
}

func (m *RunTimelineMethods) handleGet(ctx context.Context, client *gateway.Client, req *protocol.RequestFrame) {
	locale := store.LocaleFromContext(ctx)
	if m.timeline == nil {
		client.SendResponse(protocol.NewErrorResponse(req.ID, protocol.ErrInvalidRequest, i18n.T(locale, i18n.MsgRunTimelineUnavailable)))
		return
	}
	var params runTimelineGetParams
	if req.Params != nil {
		if err := json.Unmarshal(req.Params, &params); err != nil {
			client.SendResponse(protocol.NewErrorResponse(req.ID, protocol.ErrInvalidRequest, i18n.T(locale, i18n.MsgInvalidJSON)))
			return
		}
	}
	if params.RunID == "" && params.SessionKey == "" {
		client.SendResponse(protocol.NewErrorResponse(req.ID, protocol.ErrInvalidRequest, i18n.T(locale, i18n.MsgRequired, "runId or sessionKey")))
		return
	}
	if params.Limit <= 0 || params.Limit > 500 {
		params.Limit = 200
	}
	if params.Offset < 0 {
		client.SendResponse(protocol.NewErrorResponse(req.ID, protocol.ErrInvalidRequest, i18n.T(locale, i18n.MsgInvalidRequest, "offset must be non-negative")))
		return
	}
	items, err := m.timeline.ListRunTimelineItems(ctx, store.RunTimelineListOpts{
		RunID:      params.RunID,
		SessionKey: params.SessionKey,
		Limit:      params.Limit,
		Offset:     params.Offset,
	})
	if err != nil {
		slog.Warn("run_timeline.get_failed", "run_id", params.RunID, "session_key", params.SessionKey, "error", err)
		client.SendResponse(protocol.NewErrorResponse(req.ID, protocol.ErrInternal, i18n.T(locale, i18n.MsgInternalError, "run timeline")))
		return
	}
	if !canSeeAll(client.Role(), m.cfg.Gateway.OwnerIDs, client.UserID()) {
		items = filterRunTimelineItemsByUser(items, client.UserID())
	}
	client.SendResponse(protocol.NewOKResponse(req.ID, map[string]any{
		"runId":      params.RunID,
		"sessionKey": params.SessionKey,
		"items":      items,
		"limit":      params.Limit,
		"offset":     params.Offset,
	}))
}

func filterRunTimelineItemsByUser(items []store.RunTimelineItem, userID string) []store.RunTimelineItem {
	if userID == "" {
		return nil
	}
	filtered := items[:0]
	for _, item := range items {
		if item.UserID == userID {
			filtered = append(filtered, item)
		}
	}
	return filtered
}
