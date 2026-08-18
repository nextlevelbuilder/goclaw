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

// HibernateMethods handles intentional run suspension (runs.pause) and wake
// (runs.wake). It mirrors the nil-safe register + handler shape of
// RunTimelineMethods: each capability is wired through a Set* method and every
// handler reports an unavailable error until the store / suspend / resume
// entrypoints are attached. Wake intentionally reuses the shared resume closure
// (built by cmd.makeRunResumer → Loop.ResumeRun); no resume logic lives here.
type HibernateMethods struct {
	cfg *config.Config
	runs store.RunsStore

	// suspend writes a checkpoint + transitions the run to paused. Wired to
	// the owning agent's Loop.SuspendRun by cmd (nil = runs.pause unavailable).
	suspend func(ctx context.Context, runID string) error
	// resumer is the shared resume entrypoint (Loop.ResumeRun via
	// cmd.makeRunResumer). Nil = runs.wake reports unavailable.
	resumer func(ctx context.Context, runID string) (*agent.RunResult, error)
}

func NewHibernateMethods(cfg *config.Config) *HibernateMethods {
	return &HibernateMethods{cfg: cfg}
}

// SetRunsStore attaches the durable run-record store, enabling the ownership
// check for viewer-role clients. Nil-safe: the methods fall back to relying on
// the wired capability's own resolution.
func (m *HibernateMethods) SetRunsStore(runs store.RunsStore) { m.runs = runs }

// SetSuspendFn wires the suspend entrypoint for runs.pause. Nil-safe: the method
// reports unavailable until the loop is attached.
func (m *HibernateMethods) SetSuspendFn(suspend func(ctx context.Context, runID string) error) {
	m.suspend = suspend
}

// SetResumer wires the wake entrypoint for runs.wake — the same closure wired
// to runs.resume, so wake reuses the resume path without duplicating it.
func (m *HibernateMethods) SetResumer(resume func(ctx context.Context, runID string) (*agent.RunResult, error)) {
	m.resumer = resume
}

func (m *HibernateMethods) Register(router *gateway.MethodRouter) {
	router.Register(protocol.MethodRunsPause, m.handlePause)
	router.Register(protocol.MethodRunsWake, m.handleWake)
}

// handlePause suspends a run on demand (runs.pause): the owning agent loop
// writes the latest durable checkpoint and transitions the run record to
// paused. The run is woken later through runs.wake / runs.resume.
func (m *HibernateMethods) handlePause(ctx context.Context, client *gateway.Client, req *protocol.RequestFrame) {
	locale := store.LocaleFromContext(ctx)
	if m.suspend == nil {
		client.SendResponse(protocol.NewErrorResponse(req.ID, protocol.ErrUnavailable, i18n.T(locale, i18n.MsgRunsPauseUnavailable)))
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
	// Viewer-role clients may only suspend their own runs (parity with
	// runs.get / runs.resume). The store is consulted for the ownership check,
	// then the suspend fn is invoked (which re-reads the run for agent
	// resolution).
	if !canSeeAll(client.Role(), m.cfg.Gateway.OwnerIDs, client.UserID()) {
		if m.runs == nil {
			client.SendResponse(protocol.NewErrorResponse(req.ID, protocol.ErrUnavailable, i18n.T(locale, i18n.MsgRunsPauseUnavailable)))
			return
		}
		run, err := m.runs.GetRun(ctx, params.RunID)
		if err != nil {
			client.SendResponse(protocol.NewErrorResponse(req.ID, protocol.ErrNotFound, i18n.T(locale, i18n.MsgNotFound, "run", params.RunID)))
			return
		}
		if run.UserID != client.UserID() {
			client.SendResponse(protocol.NewErrorResponse(req.ID, protocol.ErrNotFound, i18n.T(locale, i18n.MsgNotFound, "run", params.RunID)))
			return
		}
	}
	if err := m.suspend(ctx, params.RunID); err != nil {
		slog.Warn("runs.pause_failed", "run_id", params.RunID, "error", err)
		if errors.Is(err, agent.ErrRunSuspendNotFound) {
			client.SendResponse(protocol.NewErrorResponse(req.ID, protocol.ErrNotFound, i18n.T(locale, i18n.MsgNotFound, "run", params.RunID)))
			return
		}
		if errors.Is(err, agent.ErrRunSuspendUnavailable) {
			client.SendResponse(protocol.NewErrorResponse(req.ID, protocol.ErrUnavailable, i18n.T(locale, i18n.MsgRunsPauseUnavailable)))
			return
		}
		client.SendResponse(protocol.NewErrorResponse(req.ID, protocol.ErrInternal, i18n.T(locale, i18n.MsgInternalError, "suspend run")))
		return
	}
	var run *store.AgentRun
	if m.runs != nil {
		fresh, err := m.runs.GetRun(ctx, params.RunID)
		if err != nil {
			slog.Warn("runs.pause_refetch_failed", "run_id", params.RunID, "error", err)
		} else {
			run = fresh
		}
	}
	client.SendResponse(protocol.NewOKResponse(req.ID, map[string]any{
		"runId":  params.RunID,
		"run":    run,
		"status": store.RunTimelineStatusPaused,
	}))
}

// handleWake wakes a paused run (runs.wake) by delegating to the shared resume
// closure — the identical path used by runs.resume. It reports the outcome the
// same way handleRunsResume does: fresh run record + final result.
func (m *HibernateMethods) handleWake(ctx context.Context, client *gateway.Client, req *protocol.RequestFrame) {
	locale := store.LocaleFromContext(ctx)
	if m.resumer == nil {
		client.SendResponse(protocol.NewErrorResponse(req.ID, protocol.ErrUnavailable, i18n.T(locale, i18n.MsgRunsWakeUnavailable)))
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
	// Viewer-role clients may only wake their own runs (parity with runs.resume).
	if !canSeeAll(client.Role(), m.cfg.Gateway.OwnerIDs, client.UserID()) {
		if m.runs == nil {
			client.SendResponse(protocol.NewErrorResponse(req.ID, protocol.ErrUnavailable, i18n.T(locale, i18n.MsgRunsWakeUnavailable)))
			return
		}
		run, err := m.runs.GetRun(ctx, params.RunID)
		if err != nil {
			client.SendResponse(protocol.NewErrorResponse(req.ID, protocol.ErrNotFound, i18n.T(locale, i18n.MsgNotFound, "run", params.RunID)))
			return
		}
		if run.UserID != client.UserID() {
			client.SendResponse(protocol.NewErrorResponse(req.ID, protocol.ErrNotFound, i18n.T(locale, i18n.MsgNotFound, "run", params.RunID)))
			return
		}
	}
	result, err := m.resumer(ctx, params.RunID)
	if err != nil {
		slog.Warn("runs.wake_failed", "run_id", params.RunID, "error", err)
		if errors.Is(err, agent.ErrRunResumeNotFound) {
			client.SendResponse(protocol.NewErrorResponse(req.ID, protocol.ErrNotFound, i18n.T(locale, i18n.MsgNotFound, "run", params.RunID)))
			return
		}
		if errors.Is(err, agent.ErrRunResumeUnavailable) {
			client.SendResponse(protocol.NewErrorResponse(req.ID, protocol.ErrUnavailable, i18n.T(locale, i18n.MsgRunsWakeUnavailable)))
			return
		}
		client.SendResponse(protocol.NewErrorResponse(req.ID, protocol.ErrInternal, i18n.T(locale, i18n.MsgInternalError, "wake run")))
		return
	}
	var run *store.AgentRun
	if m.runs != nil {
		if run, err = m.runs.GetRun(ctx, params.RunID); err != nil {
			slog.Warn("runs.wake_refetch_failed", "run_id", params.RunID, "error", err)
			run = nil
		}
	}
	client.SendResponse(protocol.NewOKResponse(req.ID, map[string]any{
		"runId":  params.RunID,
		"run":    run,
		"result": result,
	}))
}