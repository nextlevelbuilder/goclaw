package http

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"

	"github.com/google/uuid"

	"github.com/nextlevelbuilder/goclaw/internal/agent"
	"github.com/nextlevelbuilder/goclaw/internal/i18n"
	"github.com/nextlevelbuilder/goclaw/internal/permissions"
	"github.com/nextlevelbuilder/goclaw/internal/sessions"
	"github.com/nextlevelbuilder/goclaw/internal/store"
	"github.com/nextlevelbuilder/goclaw/internal/tools"
)

// WakeHandler handles POST /v1/agents/{id}/wake — external trigger API.
// Allows orchestrators (Paperclip, n8n, etc.) to trigger agent runs via HTTP.
type WakeHandler struct {
	agents   *agent.Router
	token    string
	postTurn tools.PostTurnProcessor
}

// SetPostTurnProcessor sets the post-turn processor for team task dispatch.
func (h *WakeHandler) SetPostTurnProcessor(pt tools.PostTurnProcessor) {
	h.postTurn = pt
}

// NewWakeHandler creates a handler for the wake endpoint.
func NewWakeHandler(agents *agent.Router, token string) *WakeHandler {
	return &WakeHandler{agents: agents, token: token}
}

// RegisterRoutes registers wake routes on the given mux.
func (h *WakeHandler) RegisterRoutes(mux *http.ServeMux) {
	mux.HandleFunc("POST /v1/agents/{id}/wake", h.handleWake)
}

type wakeRequest struct {
	Message    string         `json:"message"`
	SessionKey string         `json:"session_key,omitempty"`
	UserID     string         `json:"user_id,omitempty"`
	Metadata   map[string]any `json:"metadata,omitempty"`
}

type wakeResponse struct {
	Content string   `json:"content"`
	RunID   string   `json:"run_id"`
	Usage   *wakeUsage `json:"usage,omitempty"`
}

type wakeUsage struct {
	PromptTokens     int `json:"prompt_tokens"`
	CompletionTokens int `json:"completion_tokens"`
	TotalTokens      int `json:"total_tokens"`
}

func (h *WakeHandler) handleWake(w http.ResponseWriter, r *http.Request) {
	locale := extractLocale(r)

	// Auth + RBAC check (gateway token or API key, operator required for POST)
	auth := resolveAuth(r, h.token)
	if !auth.Authenticated {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": i18n.T(locale, i18n.MsgUnauthorized)})
		return
	}
	if !permissions.HasMinRole(auth.Role, permissions.RoleOperator) {
		writeJSON(w, http.StatusForbidden, map[string]string{"error": i18n.T(locale, i18n.MsgPermissionDenied, r.URL.Path)})
		return
	}

	agentID := r.PathValue("id")
	if agentID == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": i18n.T(locale, i18n.MsgInvalidID, "agent")})
		return
	}

	// Limit request body size
	const maxBodySize = 1 << 20 // 1MB
	r.Body = http.MaxBytesReader(w, r.Body, maxBodySize)

	var req wakeRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": i18n.T(locale, i18n.MsgInvalidRequest, err.Error())})
		return
	}

	if req.Message == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "message is required"})
		return
	}

	loop, err := h.agents.Get(agentID)
	if err != nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": i18n.T(locale, i18n.MsgNotFound, "agent", agentID)})
		return
	}

	// Build session key
	sessionKey := req.SessionKey
	if sessionKey == "" {
		sessionKey = sessions.SessionKey(agentID, "wake-"+uuid.NewString()[:8])
	}

	userID := req.UserID
	if userID == "" {
		userID = extractUserID(r)
	}

	ctx := store.WithLocale(r.Context(), locale)
	if userID != "" {
		ctx = store.WithUserID(ctx, userID)
	}

	runID := uuid.NewString()
	slog.Info("wake request", "agent", agentID, "user", userID, "session", sessionKey)

	ctx, drainTeamDispatch := tools.InjectTeamDispatch(ctx, h.postTurn)
	defer drainTeamDispatch()

	result, err := loop.Run(ctx, agent.RunRequest{
		SessionKey: sessionKey,
		Message:    req.Message,
		Channel:    "wake",
		ChatID:     "api",
		RunID:      runID,
		UserID:     userID,
		Stream:     false,
	})
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": fmt.Sprintf("agent run failed: %v", err)})
		return
	}

	resp := wakeResponse{
		Content: result.Content,
		RunID:   runID,
	}
	if result.Usage != nil {
		resp.Usage = &wakeUsage{
			PromptTokens:     result.Usage.PromptTokens,
			CompletionTokens: result.Usage.CompletionTokens,
			TotalTokens:      result.Usage.TotalTokens,
		}
	}

	writeJSON(w, http.StatusOK, resp)
}
