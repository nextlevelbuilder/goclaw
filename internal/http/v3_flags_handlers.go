package http

import (
	"encoding/json"
	"log/slog"
	"net/http"

	"github.com/google/uuid"

	"github.com/nextlevelbuilder/goclaw/internal/store"
)

// V3FlagsHandler serves per-agent v3 feature flag get/toggle endpoints.
type V3FlagsHandler struct {
	agents store.AgentStore
}

func NewV3FlagsHandler(agents store.AgentStore) *V3FlagsHandler {
	return &V3FlagsHandler{agents: agents}
}

func (h *V3FlagsHandler) RegisterRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /v1/agents/{agentID}/v3-flags", h.auth(h.handleGetFlags))
	mux.HandleFunc("PATCH /v1/agents/{agentID}/v3-flags", h.auth(h.handleToggleFlags))
}

func (h *V3FlagsHandler) auth(next http.HandlerFunc) http.HandlerFunc {
	return requireAuth("", next)
}

// handleGetFlags returns the current v3 feature flags for an agent.
func (h *V3FlagsHandler) handleGetFlags(w http.ResponseWriter, r *http.Request) {
	agentID, err := uuid.Parse(r.PathValue("agentID"))
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid agent ID"})
		return
	}

	ag, err := h.agents.GetByID(r.Context(), agentID)
	if err != nil {
		slog.Warn("v3flags.get_agent failed", "error", err)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	if ag == nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "agent not found"})
		return
	}

	flags := ag.ParseV3Flags()
	writeJSON(w, http.StatusOK, flags)
}

// handleToggleFlags updates specific v3 flags. Accepts partial updates.
func (h *V3FlagsHandler) handleToggleFlags(w http.ResponseWriter, r *http.Request) {
	locale := extractLocale(r)
	agentID, err := uuid.Parse(r.PathValue("agentID"))
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid agent ID"})
		return
	}

	// Parse request body as map of flag key → bool.
	var body map[string]bool
	if !bindJSON(w, r, locale, &body) {
		return
	}

	// Validate all keys are recognized v3 flags.
	for key := range body {
		if !store.IsV3FlagKey(key) {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "unknown v3 flag: " + key})
			return
		}
	}

	ctx := r.Context()

	// Read current agent to get other_config.
	ag, err := h.agents.GetByID(ctx, agentID)
	if err != nil {
		slog.Warn("v3flags.get_agent failed", "error", err)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	if ag == nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "agent not found"})
		return
	}

	// Merge v3 flag changes into other_config.
	var config map[string]any
	if len(ag.OtherConfig) > 2 {
		if err := json.Unmarshal(ag.OtherConfig, &config); err != nil {
			config = make(map[string]any)
		}
	} else {
		config = make(map[string]any)
	}
	for key, val := range body {
		config[key] = val
	}

	updated, err := json.Marshal(config)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to serialize config"})
		return
	}

	// Persist via agent store Update with other_config field.
	if err := h.agents.Update(ctx, agentID, map[string]any{"other_config": updated}); err != nil {
		slog.Warn("v3flags.update failed", "error", err)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}

	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}
