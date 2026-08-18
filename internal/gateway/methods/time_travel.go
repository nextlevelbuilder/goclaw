package methods

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"

	"github.com/nextlevelbuilder/goclaw/internal/agent"
	"github.com/nextlevelbuilder/goclaw/internal/config"
	"github.com/nextlevelbuilder/goclaw/internal/gateway"
	"github.com/nextlevelbuilder/goclaw/internal/i18n"
	"github.com/nextlevelbuilder/goclaw/internal/store"
	"github.com/nextlevelbuilder/goclaw/pkg/protocol"
)

// TimeTravelMethods handles checkpoint-snapshot history reads + replay rewind
// (running a durable run from an earlier checkpoint). Nil-safe: handlers report
// an unavailable error when the snapshot store or the replay entrypoint is not
// wired.
type TimeTravelMethods struct {
	snaps store.CheckpointSnapshotStore
	runs  store.RunsStore
	cfg   *config.Config

	// replay rewinds a run to snapshot seq and re-drives the owning agent's loop.
	// Nil = runs.replay reports unavailable.
	replay func(ctx context.Context, runID string, seq int) (*agent.RunResult, error)
}

func NewTimeTravelMethods(snaps store.CheckpointSnapshotStore, cfg *config.Config) *TimeTravelMethods {
	return &TimeTravelMethods{snaps: snaps, cfg: cfg}
}

// SetRunsStore attaches the durable run-record store, enabling the viewer-scope
// ownership check for replay (parity with runs.resume).
func (m *TimeTravelMethods) SetRunsStore(runs store.RunsStore) { m.runs = runs }

// SetReplay wires the replay entrypoint for runs.replay. Nil-safe: the method
// reports unavailable until the loop is attached.
func (m *TimeTravelMethods) SetReplay(replay func(ctx context.Context, runID string, seq int) (*agent.RunResult, error)) {
	m.replay = replay
}

func (m *TimeTravelMethods) Register(router *gateway.MethodRouter) {
	router.Register(protocol.MethodRunsCheckpointsList, m.handleCheckpointsList)
	router.Register(protocol.MethodRunsReplay, m.handleReplay)
}

// handleCheckpointsList lists one run's checkpoint-snapshot history, newest
// first (runs.checkpoints.list). Params: { runId, limit, offset }.
func (m *TimeTravelMethods) handleCheckpointsList(ctx context.Context, client *gateway.Client, req *protocol.RequestFrame) {
	locale := store.LocaleFromContext(ctx)
	if m.snaps == nil {
		client.SendResponse(protocol.NewErrorResponse(req.ID, protocol.ErrUnavailable, i18n.T(locale, i18n.MsgRunsCheckpointsUnavailable)))
		return
	}
	var params struct {
		RunID  string `json:"runId"`
		Limit  int    `json:"limit"`
		Offset int    `json:"offset"`
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
	if params.Offset < 0 {
		client.SendResponse(protocol.NewErrorResponse(req.ID, protocol.ErrInvalidRequest, i18n.T(locale, i18n.MsgInvalidRequest, "offset must be non-negative")))
		return
	}
	if params.Limit <= 0 || params.Limit > 500 {
		params.Limit = 100
	}
	items, err := m.snaps.ListCheckpointSnapshots(ctx, store.CheckpointSnapshotListOpts{
		RunID:  params.RunID,
		Limit:  params.Limit,
		Offset: params.Offset,
	})
	if err != nil {
		slog.Warn("runs.checkpoints.list_failed", "run_id", params.RunID, "error", err)
		client.SendResponse(protocol.NewErrorResponse(req.ID, protocol.ErrInternal, i18n.T(locale, i18n.MsgInternalError, "checkpoint history")))
		return
	}
	client.SendResponse(protocol.NewOKResponse(req.ID, map[string]any{
		"runId":  params.RunID,
		"items":  items,
		"limit":  params.Limit,
		"offset": params.Offset,
	}))
}

// handleReplay rewinds a durable run to an earlier checkpoint snapshot and
// re-drives the owning agent's loop (runs.replay). Runs synchronously; the
// replayed run finalizes itself (completed/compacting/failed).
func (m *TimeTravelMethods) handleReplay(ctx context.Context, client *gateway.Client, req *protocol.RequestFrame) {
	locale := store.LocaleFromContext(ctx)
	if m.replay == nil {
		client.SendResponse(protocol.NewErrorResponse(req.ID, protocol.ErrUnavailable, i18n.T(locale, i18n.MsgRunsReplayUnavailable)))
		return
	}
	var params struct {
		RunID string `json:"runId"`
		Seq   int    `json:"seq"`
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
	if params.Seq <= 0 {
		client.SendResponse(protocol.NewErrorResponse(req.ID, protocol.ErrInvalidRequest, i18n.T(locale, i18n.MsgInvalidRequest, "seq must be positive")))
		return
	}
	// Viewer-role clients may only replay their own runs (parity with runs.resume).
	if !canSeeAll(client.Role(), m.cfg.Gateway.OwnerIDs, client.UserID()) {
		if m.runs == nil {
			client.SendResponse(protocol.NewErrorResponse(req.ID, protocol.ErrUnavailable, i18n.T(locale, i18n.MsgRunsUnavailable)))
			return
		}
		run, err := m.runs.GetRun(ctx, params.RunID)
		if err != nil || run == nil {
			client.SendResponse(protocol.NewErrorResponse(req.ID, protocol.ErrNotFound, i18n.T(locale, i18n.MsgNotFound, "run", params.RunID)))
			return
		}
		if run.UserID != client.UserID() {
			client.SendResponse(protocol.NewErrorResponse(req.ID, protocol.ErrNotFound, i18n.T(locale, i18n.MsgNotFound, "run", params.RunID)))
			return
		}
	}
	result, err := m.replay(ctx, params.RunID, params.Seq)
	if err != nil {
		slog.Warn("runs.replay_failed", "run_id", params.RunID, "seq", params.Seq, "error", err)
		if errors.Is(err, agent.ErrRunReplayNotFound) {
			client.SendResponse(protocol.NewErrorResponse(req.ID, protocol.ErrNotFound, i18n.T(locale, i18n.MsgNotFound, "run", params.RunID)))
			return
		}
		if errors.Is(err, agent.ErrRunReplayUnavailable) {
			client.SendResponse(protocol.NewErrorResponse(req.ID, protocol.ErrUnavailable, i18n.T(locale, i18n.MsgRunsReplayUnavailable)))
			return
		}
		client.SendResponse(protocol.NewErrorResponse(req.ID, protocol.ErrInternal, i18n.T(locale, i18n.MsgInternalError, "replay run")))
		return
	}
	client.SendResponse(protocol.NewOKResponse(req.ID, map[string]any{
		"runId":  params.RunID,
		"seq":    params.Seq,
		"result": result,
	}))
}