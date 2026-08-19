package methods

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"

	"github.com/google/uuid"

	"github.com/nextlevelbuilder/goclaw/internal/agent"
	"github.com/nextlevelbuilder/goclaw/internal/gateway"
	"github.com/nextlevelbuilder/goclaw/internal/i18n"
	"github.com/nextlevelbuilder/goclaw/internal/store"
	"github.com/nextlevelbuilder/goclaw/pkg/protocol"
)

// MissionMethods handles the mission RPC surface (mission.create/get/list/
// pause/resume/delete). Synchronous reads/writes; resume delegates to the
// wired mission-resume closure (built by cmd.makeMissionResumer) which drives
// the owning agent's run lifecycle. Nil-safe: every handler reports an
// unavailable error until the store / resume entrypoint is attached.
type MissionMethods struct {
	missions store.MissionStore

	// resume drives the owning agent loop for a mission, reusing the durable
	// run resume path. Wired by cmd (nil = mission.resume unavailable).
	resume func(ctx context.Context, missionID string) error
}

// NewMissionMethods creates the mission RPC handler set.
func NewMissionMethods(missions store.MissionStore) *MissionMethods {
	return &MissionMethods{missions: missions}
}

// SetResumer wires the mission resume entrypoint. Nil-safe: mission.resume
// reports unavailable until the closure is attached.
func (m *MissionMethods) SetResumer(resume func(ctx context.Context, missionID string) error) {
	m.resume = resume
}

func (m *MissionMethods) Register(router *gateway.MethodRouter) {
	router.Register(protocol.MethodMissionCreate, m.handleCreate)
	router.Register(protocol.MethodMissionGet, m.handleGet)
	router.Register(protocol.MethodMissionList, m.handleList)
	router.Register(protocol.MethodMissionPause, m.handlePause)
	router.Register(protocol.MethodMissionResume, m.handleResume)
	router.Register(protocol.MethodMissionDelete, m.handleDelete)
}

func (m *MissionMethods) storeUnavailable(locale string, req *protocol.RequestFrame, client *gateway.Client) {
	client.SendResponse(protocol.NewErrorResponse(req.ID, protocol.ErrUnavailable, i18n.T(locale, i18n.MsgMissionUnavailable)))
}

// parseMissionID validates the required missionId param. Returns the parsed
// mission ID, or false after sending an error response.
func (m *MissionMethods) parseMissionID(ctx context.Context, client *gateway.Client, req *protocol.RequestFrame) (uuid.UUID, bool) {
	locale := store.LocaleFromContext(ctx)
	var params struct {
		MissionID string `json:"missionId"`
	}
	if req.Params != nil {
		if err := json.Unmarshal(req.Params, &params); err != nil {
			client.SendResponse(protocol.NewErrorResponse(req.ID, protocol.ErrInvalidRequest, i18n.T(locale, i18n.MsgInvalidJSON)))
			return uuid.Nil, false
		}
	}
	if params.MissionID == "" {
		client.SendResponse(protocol.NewErrorResponse(req.ID, protocol.ErrInvalidRequest, i18n.T(locale, i18n.MsgRequired, "missionId")))
		return uuid.Nil, false
	}
	missionID, err := uuid.Parse(params.MissionID)
	if err != nil {
		client.SendResponse(protocol.NewErrorResponse(req.ID, protocol.ErrInvalidRequest, i18n.T(locale, i18n.MsgInvalidID, "mission")))
		return uuid.Nil, false
	}
	return missionID, true
}

// handleCreate creates a mission (mission.create). Params: { name, goals,
// milestones, acceptance, agentId, sessionKey, metadata }.
func (m *MissionMethods) handleCreate(ctx context.Context, client *gateway.Client, req *protocol.RequestFrame) {
	locale := store.LocaleFromContext(ctx)
	if m.missions == nil {
		m.storeUnavailable(locale, req, client)
		return
	}
	var params struct {
		Name       string          `json:"name"`
		Goals      []string        `json:"goals"`
		Milestones []string        `json:"milestones"`
		Acceptance []string        `json:"acceptance"`
		AgentID    string          `json:"agentId"`
		SessionKey string          `json:"sessionKey"`
		Metadata   json.RawMessage `json:"metadata"`
	}
	if req.Params != nil {
		if err := json.Unmarshal(req.Params, &params); err != nil {
			client.SendResponse(protocol.NewErrorResponse(req.ID, protocol.ErrInvalidRequest, i18n.T(locale, i18n.MsgInvalidJSON)))
			return
		}
	}
	if params.Name == "" {
		client.SendResponse(protocol.NewErrorResponse(req.ID, protocol.ErrInvalidRequest, i18n.T(locale, i18n.MsgRequired, "name")))
		return
	}
	mission := &store.Mission{
		Name:       params.Name,
		Goals:      params.Goals,
		Milestones: params.Milestones,
		Acceptance: params.Acceptance,
		SessionKey: params.SessionKey,
		Metadata:   params.Metadata,
	}
	if params.AgentID != "" {
		if agentID, err := uuid.Parse(params.AgentID); err == nil {
			mission.AgentID = &agentID
		} else {
			client.SendResponse(protocol.NewErrorResponse(req.ID, protocol.ErrInvalidRequest, i18n.T(locale, i18n.MsgInvalidID, "agent")))
			return
		}
	}
	if err := m.missions.CreateMission(ctx, mission); err != nil {
		slog.Warn("mission.create_failed", "name", params.Name, "error", err)
		client.SendResponse(protocol.NewErrorResponse(req.ID, protocol.ErrInternal, i18n.T(locale, i18n.MsgFailedToCreate, "mission")))
		return
	}
	client.SendResponse(protocol.NewOKResponse(req.ID, map[string]any{"mission": mission}))
}

// handleGet returns one mission by ID (mission.get). Params: { missionId }.
func (m *MissionMethods) handleGet(ctx context.Context, client *gateway.Client, req *protocol.RequestFrame) {
	locale := store.LocaleFromContext(ctx)
	if m.missions == nil {
		m.storeUnavailable(locale, req, client)
		return
	}
	missionID, ok := m.parseMissionID(ctx, client, req)
	if !ok {
		return
	}
	mission, err := m.missions.GetMission(ctx, missionID)
	if err != nil {
		if errors.Is(err, store.ErrMissionNotFound) {
			client.SendResponse(protocol.NewErrorResponse(req.ID, protocol.ErrNotFound, i18n.T(locale, i18n.MsgMissionNotFound)))
			return
		}
		slog.Warn("mission.get_failed", "mission_id", missionID, "error", err)
		client.SendResponse(protocol.NewErrorResponse(req.ID, protocol.ErrInternal, i18n.T(locale, i18n.MsgInternalError, "get mission")))
		return
	}
	client.SendResponse(protocol.NewOKResponse(req.ID, map[string]any{"mission": mission}))
}

// handleList lists missions filtered by status (mission.list). Params:
// { status, limit, offset }.
func (m *MissionMethods) handleList(ctx context.Context, client *gateway.Client, req *protocol.RequestFrame) {
	locale := store.LocaleFromContext(ctx)
	if m.missions == nil {
		m.storeUnavailable(locale, req, client)
		return
	}
	var params struct {
		Status string `json:"status"`
		Limit  int    `json:"limit"`
		Offset int    `json:"offset"`
	}
	if req.Params != nil {
		if err := json.Unmarshal(req.Params, &params); err != nil {
			client.SendResponse(protocol.NewErrorResponse(req.ID, protocol.ErrInvalidRequest, i18n.T(locale, i18n.MsgInvalidJSON)))
			return
		}
	}
	if params.Status != "" && !store.ValidMissionStatus(params.Status) {
		client.SendResponse(protocol.NewErrorResponse(req.ID, protocol.ErrInvalidRequest, i18n.T(locale, i18n.MsgMissionInvalidStatus, params.Status)))
		return
	}
	if params.Offset < 0 {
		client.SendResponse(protocol.NewErrorResponse(req.ID, protocol.ErrInvalidRequest, i18n.T(locale, i18n.MsgInvalidRequest, "offset must be non-negative")))
		return
	}
	if params.Limit <= 0 || params.Limit > 500 {
		params.Limit = 100
	}
	items, err := m.missions.ListMissions(ctx, store.MissionListOpts{
		Status: params.Status,
		Limit:  params.Limit,
		Offset: params.Offset,
	})
	if err != nil {
		slog.Warn("mission.list_failed", "status", params.Status, "error", err)
		client.SendResponse(protocol.NewErrorResponse(req.ID, protocol.ErrInternal, i18n.T(locale, i18n.MsgInternalError, "list missions")))
		return
	}
	client.SendResponse(protocol.NewOKResponse(req.ID, map[string]any{
		"items":  items,
		"limit":  params.Limit,
		"offset": params.Offset,
	}))
}

// handlePause suspends a mission (mission.pause). Params: { missionId }.
func (m *MissionMethods) handlePause(ctx context.Context, client *gateway.Client, req *protocol.RequestFrame) {
	locale := store.LocaleFromContext(ctx)
	if m.missions == nil {
		m.storeUnavailable(locale, req, client)
		return
	}
	missionID, ok := m.parseMissionID(ctx, client, req)
	if !ok {
		return
	}
	if err := m.missions.UpdateMissionStatus(ctx, missionID, store.MissionStatusPaused); err != nil {
		slog.Warn("mission.pause_failed", "mission_id", missionID, "error", err)
		client.SendResponse(protocol.NewErrorResponse(req.ID, protocol.ErrInternal, i18n.T(locale, i18n.MsgInternalError, "pause mission")))
		return
	}
	client.SendResponse(protocol.NewOKResponse(req.ID, map[string]any{
		"missionId": missionID,
		"status":    store.MissionStatusPaused,
	}))
}

// handleResume resumes a paused or active mission (mission.resume) by driving
// the owning agent loop through the wired closure. Params: { missionId }.
func (m *MissionMethods) handleResume(ctx context.Context, client *gateway.Client, req *protocol.RequestFrame) {
	locale := store.LocaleFromContext(ctx)
	if m.missions == nil {
		m.storeUnavailable(locale, req, client)
		return
	}
	missionID, ok := m.parseMissionID(ctx, client, req)
	if !ok {
		return
	}
	if m.resume == nil {
		client.SendResponse(protocol.NewErrorResponse(req.ID, protocol.ErrUnavailable, i18n.T(locale, i18n.MsgMissionResumeUnavailable)))
		return
	}
	if err := m.resume(ctx, missionID.String()); err != nil {
		slog.Warn("mission.resume_failed", "mission_id", missionID, "error", err)
		if errors.Is(err, store.ErrMissionNotResumable) {
			client.SendResponse(protocol.NewErrorResponse(req.ID, protocol.ErrInvalidRequest, i18n.T(locale, i18n.MsgMissionInvalidStatus, err.Error())))
			return
		}
		if errors.Is(err, store.ErrMissionNotFound) || errors.Is(err, agent.ErrRunResumeNotFound) {
			client.SendResponse(protocol.NewErrorResponse(req.ID, protocol.ErrNotFound, i18n.T(locale, i18n.MsgMissionNotFound)))
			return
		}
		if errors.Is(err, agent.ErrRunResumeUnavailable) {
			client.SendResponse(protocol.NewErrorResponse(req.ID, protocol.ErrUnavailable, i18n.T(locale, i18n.MsgMissionResumeUnavailable)))
			return
		}
		client.SendResponse(protocol.NewErrorResponse(req.ID, protocol.ErrInternal, i18n.T(locale, i18n.MsgInternalError, "resume mission")))
		return
	}
	client.SendResponse(protocol.NewOKResponse(req.ID, map[string]any{
		"missionId": missionID,
		"status":    store.MissionStatusActive,
	}))
}

// handleDelete removes a mission (mission.delete). Params: { missionId }.
func (m *MissionMethods) handleDelete(ctx context.Context, client *gateway.Client, req *protocol.RequestFrame) {
	locale := store.LocaleFromContext(ctx)
	if m.missions == nil {
		m.storeUnavailable(locale, req, client)
		return
	}
	missionID, ok := m.parseMissionID(ctx, client, req)
	if !ok {
		return
	}
	if err := m.missions.DeleteMission(ctx, missionID); err != nil {
		slog.Warn("mission.delete_failed", "mission_id", missionID, "error", err)
		client.SendResponse(protocol.NewErrorResponse(req.ID, protocol.ErrInternal, i18n.T(locale, i18n.MsgInternalError, "delete mission")))
		return
	}
	client.SendResponse(protocol.NewOKResponse(req.ID, map[string]any{
		"missionId": missionID,
		"deleted":   true,
	}))
}
