package cmd

import (
	"context"
	"errors"
	"log/slog"

	"github.com/google/uuid"

	"github.com/nextlevelbuilder/goclaw/internal/agent"
	"github.com/nextlevelbuilder/goclaw/internal/store"
)

// makeMissionResumer builds the closure behind the WS mission.resume handler.
// It resolves the mission, finds its most recent run record (runs are keyed by
// the mission's session key), resolves the owning agent through the router,
// asserts it implements loopResumer, and drives the agent's Loop.ResumeRun —
// the same durable resume path used by runs.resume — so a resumed mission picks
// up from its latest checkpoint instead of starting fresh. A successful resume
// transitions the mission back to active. Returns nil when the store, runs
// store, or router is absent — the handlers then report unavailable.
func makeMissionResumer(agents *agent.Router, missions store.MissionStore, runs store.RunsStore) func(ctx context.Context, missionID string) error {
	if agents == nil || missions == nil || runs == nil {
		return nil
	}
	return func(ctx context.Context, missionID string) error {
		id, err := uuid.Parse(missionID)
		if err != nil {
			return err
		}
		mission, err := missions.GetMission(ctx, id)
		if err != nil {
			if errors.Is(err, store.ErrMissionNotFound) {
				return store.ErrMissionNotFound
			}
			slog.Warn("mission.resume_mission_lookup_failed", "mission_id", missionID, "error", err)
			return err
		}
		// Terminal missions cannot be re-driven.
		switch mission.Status {
		case store.MissionStatusCompleted, store.MissionStatusFailed, store.MissionStatusCancelled:
			return store.ErrMissionNotResumable
		}
		if mission.AgentID == nil || mission.SessionKey == "" {
			return store.ErrMissionNotResumable
		}

		// The mission's working run lives in the session keyed by
		// mission.SessionKey. Find the most recent run record for that session.
		missionRuns, err := runs.ListRuns(ctx, store.RunListOpts{
			SessionKey: mission.SessionKey,
			Limit:      1,
			Offset:     0,
		})
		if err != nil {
			slog.Warn("mission.resume_run_lookup_failed", "mission_id", missionID, "error", err)
			return err
		}
		if len(missionRuns) == 0 {
			return store.ErrMissionNotResumable
		}
		runID := missionRuns[0].RunID

		ag, err := agents.Get(ctx, mission.AgentID.String())
		if err != nil {
			slog.Warn("mission.resume_agent_resolve_failed", "mission_id", missionID, "agent_id", mission.AgentID.String(), "error", err)
			return err
		}
		l, ok := ag.(loopResumer)
		if !ok {
			slog.Warn("mission.resume_not_supported_by_agent", "mission_id", missionID, "agent_id", mission.AgentID.String())
			return agent.ErrRunResumeUnavailable
		}
		if _, err := l.ResumeRun(ctx, runID); err != nil {
			slog.Warn("mission.resume_run_failed", "mission_id", missionID, "run_id", runID, "error", err)
			return err
		}
		// A successfully resumed mission is active again.
		if err := missions.UpdateMissionStatus(ctx, id, store.MissionStatusActive); err != nil {
			slog.Warn("mission.resume_status_update_failed", "mission_id", missionID, "error", err)
			return err
		}
		return nil
	}
}
