package http

import (
	"log/slog"
	"net/http"

	"github.com/google/uuid"

	"github.com/nextlevelbuilder/goclaw/internal/agent"
	"github.com/nextlevelbuilder/goclaw/internal/store"
)

// OrchestrationHandler serves read-only orchestration mode info.
type OrchestrationHandler struct {
	agents store.AgentStore
	teams  store.TeamStore
	links  store.AgentLinkStore
}

func NewOrchestrationHandler(agents store.AgentStore, teams store.TeamStore, links store.AgentLinkStore) *OrchestrationHandler {
	return &OrchestrationHandler{agents: agents, teams: teams, links: links}
}

func (h *OrchestrationHandler) RegisterRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /v1/agents/{agentID}/orchestration", h.auth(h.handleGetMode))
}

func (h *OrchestrationHandler) auth(next http.HandlerFunc) http.HandlerFunc {
	return requireAuth("", next)
}

// handleGetMode returns the computed orchestration mode and delegate targets for an agent.
func (h *OrchestrationHandler) handleGetMode(w http.ResponseWriter, r *http.Request) {
	agentID, err := uuid.Parse(r.PathValue("agentID"))
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid agent ID"})
		return
	}

	ctx := r.Context()
	mode := agent.ResolveOrchestrationMode(ctx, agentID, h.teams, h.links)

	resp := map[string]any{
		"mode":             string(mode),
		"delegate_targets": []any{},
		"team":             nil,
	}

	// Populate delegate targets if in delegate or team mode.
	if h.links != nil {
		targets, err := h.links.DelegateTargets(ctx, agentID)
		if err != nil {
			slog.Warn("orchestration.delegate_targets failed", "error", err)
		} else if len(targets) > 0 {
			entries := make([]map[string]string, 0, len(targets))
			for _, t := range targets {
				entries = append(entries, map[string]string{
					"agent_key":    t.TargetAgentKey,
					"display_name": t.TargetDisplayName,
				})
			}
			resp["delegate_targets"] = entries
		}
	}

	// Populate team info if in team mode.
	if mode == agent.ModeTeam && h.teams != nil {
		if team, err := h.teams.GetTeamForAgent(ctx, agentID); err == nil && team != nil {
			resp["team"] = map[string]any{
				"id":   team.ID,
				"name": team.Name,
			}
		}
	}

	writeJSON(w, http.StatusOK, resp)
}
