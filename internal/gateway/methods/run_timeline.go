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

// RunTimelineMethods handles archived run timeline reads + durable run records.
type RunTimelineMethods struct {
	timeline store.RunTimelineStore
	runs     store.RunsStore
	cfg      *config.Config
}

func NewRunTimelineMethods(timeline store.RunTimelineStore, cfg *config.Config) *RunTimelineMethods {
	return &RunTimelineMethods{timeline: timeline, cfg: cfg}
}

// SetRunsStore attaches the durable run-record store, enabling the
// runs.get/runs.list/runs.events methods. Nil-safe: those methods report an
// unavailable error when the store is not wired.
func (m *RunTimelineMethods) SetRunsStore(runs store.RunsStore) { m.runs = runs }

func (m *RunTimelineMethods) Register(router *gateway.MethodRouter) {
	router.Register(protocol.MethodRunTimelineGet, m.handleGet)
	router.Register(protocol.MethodRunsGet, m.handleRunsGet)
	router.Register(protocol.MethodRunsList, m.handleRunsList)
	router.Register(protocol.MethodRunsEvents, m.handleRunsEvents)
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

// runs.get reads one durable run record by run_id.
func (m *RunTimelineMethods) handleRunsGet(ctx context.Context, client *gateway.Client, req *protocol.RequestFrame) {
	locale := store.LocaleFromContext(ctx)
	if m.runs == nil {
		client.SendResponse(protocol.NewErrorResponse(req.ID, protocol.ErrUnavailable, i18n.T(locale, i18n.MsgRunsUnavailable)))
		return
	}
	var params struct {
		RunID string `json:"runId"`
	}
	if req.Params != nil {
		if err := json.Unmarshal(req.Params, &params); err != nil {
			client.SendResponse(protocol.NewErrorResponse(req.ID, protocol.ErrInvalidRequest, i18n.T(locale, i18n.MsgInvalidJSON)))
			return
		}
	}
	if params.RunID == "" {
		client.SendResponse(protocol.NewErrorResponse(req.ID, protocol.ErrInvalidRequest, i18n.T(locale, i18n.MsgRequired, "runId")))
		return
	}
	run, err := m.runs.GetRun(ctx, params.RunID)
	if err != nil {
		slog.Warn("runs.get_failed", "run_id", params.RunID, "error", err)
		client.SendResponse(protocol.NewErrorResponse(req.ID, protocol.ErrNotFound, i18n.T(locale, i18n.MsgNotFound, "run", params.RunID)))
		return
	}
	// Viewer-role clients may only read their own run records (parity with the
	// HTTP handler and run.timeline.get). Return not-found so run existence is
	// not leaked to unauthorized callers.
	if !canSeeAll(client.Role(), m.cfg.Gateway.OwnerIDs, client.UserID()) && run.UserID != client.UserID() {
		client.SendResponse(protocol.NewErrorResponse(req.ID, protocol.ErrNotFound, i18n.T(locale, i18n.MsgNotFound, "run", params.RunID)))
		return
	}
	client.SendResponse(protocol.NewOKResponse(req.ID, map[string]any{
		"run": run,
	}))
}

// runs.list lists durable run records, optionally scoped to a session key or status.
func (m *RunTimelineMethods) handleRunsList(ctx context.Context, client *gateway.Client, req *protocol.RequestFrame) {
	locale := store.LocaleFromContext(ctx)
	if m.runs == nil {
		client.SendResponse(protocol.NewErrorResponse(req.ID, protocol.ErrUnavailable, i18n.T(locale, i18n.MsgRunsUnavailable)))
		return
	}
	var params struct {
		SessionKey string `json:"sessionKey"`
		Status     string `json:"status"`
		Limit      int    `json:"limit"`
		Offset     int    `json:"offset"`
	}
	if req.Params != nil {
		if err := json.Unmarshal(req.Params, &params); err != nil {
			client.SendResponse(protocol.NewErrorResponse(req.ID, protocol.ErrInvalidRequest, i18n.T(locale, i18n.MsgInvalidJSON)))
			return
		}
	}
	if params.Limit <= 0 || params.Limit > 500 {
		params.Limit = 100
	}
	if params.Offset < 0 {
		client.SendResponse(protocol.NewErrorResponse(req.ID, protocol.ErrInvalidRequest, i18n.T(locale, i18n.MsgInvalidRequest, "offset must be non-negative")))
		return
	}
	// Mirror the CLI validation: reject unknown status values instead of
	// silently returning an empty list (the store treats status as an equality
	// filter, so an invalid value would just match nothing).
	if params.Status != "" && !store.ValidAgentRunStatus(params.Status) {
		client.SendResponse(protocol.NewErrorResponse(req.ID, protocol.ErrInvalidRequest, i18n.T(locale, i18n.MsgInvalidRequest, "invalid status; use pending|running|compacting|completed|failed|cancelled")))
		return
	}
	runs, err := m.runs.ListRuns(ctx, store.RunListOpts{
		SessionKey: params.SessionKey,
		Status:     params.Status,
		Limit:      params.Limit,
		Offset:     params.Offset,
	})
	if err != nil {
		slog.Warn("runs.list_failed", "session_key", params.SessionKey, "status", params.Status, "error", err)
		client.SendResponse(protocol.NewErrorResponse(req.ID, protocol.ErrInternal, i18n.T(locale, i18n.MsgInternalError, "run list")))
		return
	}
	// Viewer-role clients may only list their own run records (parity with the
	// HTTP handler and run.timeline.get).
	if !canSeeAll(client.Role(), m.cfg.Gateway.OwnerIDs, client.UserID()) {
		callerID := client.UserID()
		if callerID == "" {
			runs = nil
		} else {
			filtered := runs[:0]
			for _, r := range runs {
				if r.UserID == callerID {
					filtered = append(filtered, r)
				}
			}
			runs = filtered
		}
	}
	client.SendResponse(protocol.NewOKResponse(req.ID, map[string]any{
		"runs":   runs,
		"limit":  params.Limit,
		"offset": params.Offset,
	}))
}

// handleRunsEvents replays run timeline items after a seq cursor (for resync).
// Uses the same event journal as run.timeline.get, so viewer-role users are
// still filtered to their own items.
func (m *RunTimelineMethods) handleRunsEvents(ctx context.Context, client *gateway.Client, req *protocol.RequestFrame) {
	locale := store.LocaleFromContext(ctx)
	if m.timeline == nil {
		client.SendResponse(protocol.NewErrorResponse(req.ID, protocol.ErrUnavailable, i18n.T(locale, i18n.MsgRunTimelineUnavailable)))
		return
	}
	var params struct {
		RunID    string `json:"runId"`
		AfterSeq int    `json:"afterSeq"`
		Limit    int    `json:"limit"`
	}
	if req.Params != nil {
		if err := json.Unmarshal(req.Params, &params); err != nil {
			client.SendResponse(protocol.NewErrorResponse(req.ID, protocol.ErrInvalidRequest, i18n.T(locale, i18n.MsgInvalidJSON)))
			return
		}
	}
	if params.RunID == "" {
		client.SendResponse(protocol.NewErrorResponse(req.ID, protocol.ErrInvalidRequest, i18n.T(locale, i18n.MsgRequired, "runId")))
		return
	}
	if params.AfterSeq < 0 {
		client.SendResponse(protocol.NewErrorResponse(req.ID, protocol.ErrInvalidRequest, i18n.T(locale, i18n.MsgInvalidRequest, "afterSeq must be non-negative")))
		return
	}
	if params.Limit <= 0 || params.Limit > 500 {
		params.Limit = 200
	}
	items, err := m.timeline.ListRunTimelineItems(ctx, store.RunTimelineListOpts{
		RunID:    params.RunID,
		AfterSeq: params.AfterSeq,
		Limit:    params.Limit,
	})
	if err != nil {
		slog.Warn("runs.events_failed", "run_id", params.RunID, "after_seq", params.AfterSeq, "error", err)
		client.SendResponse(protocol.NewErrorResponse(req.ID, protocol.ErrInternal, i18n.T(locale, i18n.MsgInternalError, "run events")))
		return
	}
	if !canSeeAll(client.Role(), m.cfg.Gateway.OwnerIDs, client.UserID()) {
		items = filterRunTimelineItemsByUser(items, client.UserID())
	}
	client.SendResponse(protocol.NewOKResponse(req.ID, map[string]any{
		"runId":     params.RunID,
		"afterSeq":  params.AfterSeq,
		"items":     items,
		"limit":     params.Limit,
		"nextAfter": nextAfterSeq(items, params.AfterSeq),
	}))
}

// nextAfterSeq computes the cursor to pass for the next page: the seq of the
// last item returned (or the input cursor when no items were returned).
func nextAfterSeq(items []store.RunTimelineItem, after int) int {
	if len(items) == 0 {
		return after
	}
	last := items[len(items)-1].Seq
	if last > after {
		return last
	}
	return after
}
