package http

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"

	"github.com/google/uuid"

	"github.com/nextlevelbuilder/goclaw/internal/bootstrap"
	"github.com/nextlevelbuilder/goclaw/internal/bus"
	"github.com/nextlevelbuilder/goclaw/internal/store"
	"github.com/nextlevelbuilder/goclaw/pkg/protocol"
)

// AgentsHandler handles agent CRUD and sharing endpoints.
type AgentsHandler struct {
	agents   store.AgentStore
	token    string
	msgBus   *bus.MessageBus  // for cache invalidation events (nil = no events)
	summoner *AgentSummoner   // LLM-based agent setup (nil = disabled)
	isOwner  func(string) bool // checks if user ID is a system owner (nil = no owners configured)
}

// NewAgentsHandler creates a handler for agent management endpoints.
// isOwner is a function that checks if a user ID is in GOCLAW_OWNER_IDS (nil = disabled).
func NewAgentsHandler(agents store.AgentStore, token string, msgBus *bus.MessageBus, summoner *AgentSummoner, isOwner func(string) bool) *AgentsHandler {
	return &AgentsHandler{agents: agents, token: token, msgBus: msgBus, summoner: summoner, isOwner: isOwner}
}

// isOwnerUser checks if the given user ID is a system owner.
func (h *AgentsHandler) isOwnerUser(userID string) bool {
	return userID != "" && h.isOwner != nil && h.isOwner(userID)
}

// emitCacheInvalidate broadcasts a cache invalidation event if msgBus is set.
func (h *AgentsHandler) emitCacheInvalidate(kind, key string) {
	if h.msgBus == nil {
		return
	}
	h.msgBus.Broadcast(bus.Event{
		Name:    protocol.EventCacheInvalidate,
		Payload: bus.CacheInvalidatePayload{Kind: kind, Key: key},
	})
}

// RegisterRoutes registers all agent management routes on the given mux.
func (h *AgentsHandler) RegisterRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /v1/agents", h.authMiddleware(h.handleList))
	mux.HandleFunc("POST /v1/agents", h.authMiddleware(h.handleCreate))
	mux.HandleFunc("GET /v1/agents/{id}", h.authMiddleware(h.handleGet))
	mux.HandleFunc("PUT /v1/agents/{id}", h.authMiddleware(h.handleUpdate))
	mux.HandleFunc("DELETE /v1/agents/{id}", h.authMiddleware(h.handleDelete))
	mux.HandleFunc("GET /v1/agents/{id}/shares", h.authMiddleware(h.handleListShares))
	mux.HandleFunc("POST /v1/agents/{id}/shares", h.authMiddleware(h.handleShare))
	mux.HandleFunc("DELETE /v1/agents/{id}/shares/{userID}", h.authMiddleware(h.handleRevokeShare))
	mux.HandleFunc("POST /v1/agents/{id}/regenerate", h.authMiddleware(h.handleRegenerate))
	mux.HandleFunc("POST /v1/agents/{id}/resummon", h.authMiddleware(h.handleResummon))
}

func (h *AgentsHandler) authMiddleware(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if h.token != "" {
			if extractBearerToken(r) != h.token {
				writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "unauthorized"})
				return
			}
		}
		// Inject user_id into context
		userID := extractUserID(r)
		if userID != "" {
			ctx := store.WithUserID(r.Context(), userID)
			r = r.WithContext(ctx)
		}
		next(w, r)
	}
}

func (h *AgentsHandler) handleList(w http.ResponseWriter, r *http.Request) {
	userID := store.UserIDFromContext(r.Context())
	if userID == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "X-GoClaw-User-Id header required"})
		return
	}

	var agents []store.AgentData
	var err error
	if h.isOwnerUser(userID) {
		agents, err = h.agents.List(r.Context(), "") // owners see all agents
	} else {
		agents, err = h.agents.ListAccessible(r.Context(), userID)
	}
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{"agents": agents})
}

func (h *AgentsHandler) handleCreate(w http.ResponseWriter, r *http.Request) {
	userID := store.UserIDFromContext(r.Context())
	if userID == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "X-GoClaw-User-Id header required"})
		return
	}

	var req store.AgentData
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid JSON: " + err.Error()})
		return
	}

	if !isValidSlug(req.AgentKey) {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "agent_key must be a valid slug (lowercase letters, numbers, hyphens only)"})
		return
	}

	req.OwnerID = userID
	if req.AgentType == "" {
		req.AgentType = store.AgentTypeOpen
	}
	if req.ContextWindow <= 0 {
		req.ContextWindow = 200000
	}
	if req.MaxToolIterations <= 0 {
		req.MaxToolIterations = 20
	}
	if req.Workspace == "" {
		if req.IsDefault {
			req.Workspace = "~/.goclaw/workspace"
		} else {
			req.Workspace = fmt.Sprintf("~/.goclaw/%s-workspace", req.AgentKey)
		}
	}
	req.RestrictToWorkspace = true

	// Default: enable compaction and memory for new agents
	if len(req.CompactionConfig) == 0 {
		req.CompactionConfig = json.RawMessage(`{}`)
	}
	if len(req.MemoryConfig) == 0 {
		req.MemoryConfig = json.RawMessage(`{"enabled":true}`)
	}

	// Check if predefined agent has a description for LLM summoning
	description := extractDescription(req.OtherConfig)
	if req.AgentType == store.AgentTypePredefined && description != "" && h.summoner != nil {
		req.Status = store.AgentStatusSummoning
	} else if req.Status == "" {
		req.Status = store.AgentStatusActive
	}

	if err := h.agents.Create(r.Context(), &req); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}

	// Seed context files into agent_context_files (skipped for open agents).
	// For summoning agents, templates serve as fallback if LLM fails.
	if _, err := bootstrap.SeedToStore(r.Context(), h.agents, req.ID, req.AgentType); err != nil {
		slog.Warn("failed to seed context files for new agent", "agent", req.AgentKey, "error", err)
	}

	// Start LLM summoning in background if applicable
	if req.Status == store.AgentStatusSummoning {
		go h.summoner.SummonAgent(req.ID, req.Provider, req.Model, description)
	}

	writeJSON(w, http.StatusCreated, req)
}

func (h *AgentsHandler) handleGet(w http.ResponseWriter, r *http.Request) {
	userID := store.UserIDFromContext(r.Context())
	isOwner := h.isOwnerUser(userID)

	id, err := uuid.Parse(r.PathValue("id"))
	if err != nil {
		// Try by agent_key
		ag, err2 := h.agents.GetByKey(r.Context(), r.PathValue("id"))
		if err2 != nil {
			writeJSON(w, http.StatusNotFound, map[string]string{"error": "agent not found"})
			return
		}
		if userID != "" && !isOwner {
			if ok, _, _ := h.agents.CanAccess(r.Context(), ag.ID, userID); !ok {
				writeJSON(w, http.StatusForbidden, map[string]string{"error": "no access to this agent"})
				return
			}
		}
		writeJSON(w, http.StatusOK, ag)
		return
	}

	ag, err := h.agents.GetByID(r.Context(), id)
	if err != nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "agent not found"})
		return
	}

	if userID != "" && !isOwner {
		if ok, _, _ := h.agents.CanAccess(r.Context(), id, userID); !ok {
			writeJSON(w, http.StatusForbidden, map[string]string{"error": "no access to this agent"})
			return
		}
	}

	writeJSON(w, http.StatusOK, ag)
}

func (h *AgentsHandler) handleUpdate(w http.ResponseWriter, r *http.Request) {
	userID := store.UserIDFromContext(r.Context())
	id, err := uuid.Parse(r.PathValue("id"))
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid agent ID"})
		return
	}

	// Only owner can update
	ag, err := h.agents.GetByID(r.Context(), id)
	if err != nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "agent not found"})
		return
	}
	if userID != "" && ag.OwnerID != userID && !h.isOwnerUser(userID) {
		writeJSON(w, http.StatusForbidden, map[string]string{"error": "only owner can update agent"})
		return
	}

	var updates map[string]any
	if err := json.NewDecoder(r.Body).Decode(&updates); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid JSON: " + err.Error()})
		return
	}

	// Prevent changing owner_id
	delete(updates, "owner_id")
	delete(updates, "id")

	if err := h.agents.Update(r.Context(), id, updates); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}

	// Invalidate caches: agent Loop + bootstrap files
	h.emitCacheInvalidate(bus.CacheKindAgent, ag.AgentKey)
	h.emitCacheInvalidate(bus.CacheKindBootstrap, id.String())

	// Cascade: if status changed, broadcast so channel instances and cron jobs react.
	if newStatus, ok := updates["status"].(string); ok && newStatus != ag.Status {
		if h.msgBus != nil {
			h.msgBus.Broadcast(bus.Event{
				Name: bus.EventAgentStatusChanged,
				Payload: bus.AgentStatusChangedPayload{
					AgentID:   id.String(),
					OldStatus: ag.Status,
					NewStatus: newStatus,
				},
			})
		}
	}

	writeJSON(w, http.StatusOK, map[string]string{"ok": "true"})
}

func (h *AgentsHandler) handleDelete(w http.ResponseWriter, r *http.Request) {
	userID := store.UserIDFromContext(r.Context())
	id, err := uuid.Parse(r.PathValue("id"))
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid agent ID"})
		return
	}

	// Only owner can delete
	ag, err := h.agents.GetByID(r.Context(), id)
	if err != nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "agent not found"})
		return
	}
	if userID != "" && ag.OwnerID != userID && !h.isOwnerUser(userID) {
		writeJSON(w, http.StatusForbidden, map[string]string{"error": "only owner can delete agent"})
		return
	}

	if err := h.agents.Delete(r.Context(), id); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}

	// Invalidate caches: agent Loop + bootstrap files
	h.emitCacheInvalidate(bus.CacheKindAgent, ag.AgentKey)
	h.emitCacheInvalidate(bus.CacheKindBootstrap, id.String())

	writeJSON(w, http.StatusOK, map[string]string{"ok": "true"})
}
