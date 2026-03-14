package http

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"time"

	"github.com/google/uuid"

	"github.com/nextlevelbuilder/goclaw/internal/agent"
	"github.com/nextlevelbuilder/goclaw/internal/sessions"
	"github.com/nextlevelbuilder/goclaw/internal/store"
)

// ResponsesHandler handles POST /v1/responses (OpenResponses protocol).
type ResponsesHandler struct {
	agents   *agent.Router
	sessions store.SessionStore
	token    string
}

// NewResponsesHandler creates a handler for the responses endpoint.
func NewResponsesHandler(agents *agent.Router, sess store.SessionStore, token string) *ResponsesHandler {
	return &ResponsesHandler{
		agents:   agents,
		sessions: sess,
		token:    token,
	}
}

type responsesRequest struct {
	Model     string        `json:"model"`
	Messages  []chatMessage `json:"messages"`
	Stream    bool          `json:"stream"`
	MaxTokens int           `json:"max_tokens,omitempty"`
}

func (h *ResponsesHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// Auth check (timing-safe comparison)
	if !tokenMatch(extractBearerToken(r), h.token) {
		http.Error(w, `{"error":"unauthorized"}`, http.StatusUnauthorized)
		return
	}

	// Limit request body size to prevent DoS
	const maxRequestBodySize = 1 << 20 // 1MB
	r.Body = http.MaxBytesReader(w, r.Body, maxRequestBodySize)

	var req responsesRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, fmt.Sprintf(`{"error":"invalid JSON: %s"}`, err), http.StatusBadRequest)
		return
	}

	if len(req.Messages) == 0 {
		http.Error(w, `{"error":"messages is required"}`, http.StatusBadRequest)
		return
	}

	agentID := extractAgentID(r, req.Model)
	userID := extractUserID(r)

	loop, err := h.agents.Get(agentID)
	if err != nil {
		http.Error(w, fmt.Sprintf(`{"error":"agent not found: %s"}`, agentID), http.StatusNotFound)
		return
	}

	var lastMessage string
	for i := len(req.Messages) - 1; i >= 0; i-- {
		if req.Messages[i].Role == "user" {
			lastMessage = req.Messages[i].Content
			break
		}
	}
	if lastMessage == "" {
		http.Error(w, `{"error":"no user message found"}`, http.StatusBadRequest)
		return
	}

	// Inject user_id into context for downstream stores/tools
	ctx := r.Context()
	if userID != "" {
		ctx = store.WithUserID(ctx, userID)
	}

	runID := uuid.NewString()
	responseID := "resp-" + runID[:8]
	sessionSuffix := "responses-" + runID[:8]
	if userID != "" {
		sessionSuffix = "responses-" + userID + "-" + runID[:8]
	}
	sessionKey := sessions.SessionKey(agentID, sessionSuffix)

	slog.Info("responses request", "agent", agentID, "stream", req.Stream, "user", userID)

	if req.Stream {
		h.handleStream(w, r.WithContext(ctx), loop, runID, responseID, sessionKey, lastMessage, userID)
	} else {
		h.handleNonStream(w, r.WithContext(ctx), loop, runID, responseID, sessionKey, lastMessage, userID)
	}
}

func (h *ResponsesHandler) handleNonStream(w http.ResponseWriter, r *http.Request, loop agent.Agent, runID, responseID, sessionKey, message, userID string) {
	result, err := loop.Run(r.Context(), agent.RunRequest{
		SessionKey: sessionKey,
		Message:    message,
		Channel:    "http",
		ChatID:     "api",
		RunID:      runID,
		UserID:     userID,
		Stream:     false,
	})

	if err != nil {
		http.Error(w, fmt.Sprintf(`{"error":"agent error: %s"}`, err), http.StatusInternalServerError)
		return
	}

	resp := map[string]any{
		"id":     responseID,
		"status": "completed",
		"output": []map[string]any{{
			"type":    "message",
			"role":    "assistant",
			"content": []map[string]string{{"type": "text", "text": result.Content}},
		}},
	}

	if result.Usage != nil {
		resp["usage"] = map[string]int{
			"prompt_tokens":     result.Usage.PromptTokens,
			"completion_tokens": result.Usage.CompletionTokens,
			"total_tokens":      result.Usage.TotalTokens,
		}
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(resp)
}

func (h *ResponsesHandler) handleStream(w http.ResponseWriter, r *http.Request, loop agent.Agent, runID, responseID, sessionKey, message, userID string) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming not supported", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.WriteHeader(http.StatusOK)

	// response.started
	writeResponseEvent(w, flusher, map[string]any{
		"type": "response.started",
		"response": map[string]any{
			"id":         responseID,
			"status":     "in_progress",
			"created_at": time.Now().Unix(),
		},
	})

	result, err := loop.Run(r.Context(), agent.RunRequest{
		SessionKey: sessionKey,
		Message:    message,
		Channel:    "http",
		ChatID:     "api",
		RunID:      runID,
		UserID:     userID,
		Stream:     true,
	})

	if err != nil {
		// response.done with error
		writeResponseEvent(w, flusher, map[string]any{
			"type": "response.done",
			"response": map[string]any{
				"id":     responseID,
				"status": "failed",
				"error":  err.Error(),
			},
		})
		return
	}

	// response.delta
	writeResponseEvent(w, flusher, map[string]any{
		"type": "response.delta",
		"delta": map[string]any{
			"type":    "content",
			"content": result.Content,
		},
	})

	// response.done
	doneResp := map[string]any{
		"id":     responseID,
		"status": "completed",
	}
	if result.Usage != nil {
		doneResp["usage"] = map[string]int{
			"prompt_tokens":     result.Usage.PromptTokens,
			"completion_tokens": result.Usage.CompletionTokens,
			"total_tokens":      result.Usage.TotalTokens,
		}
	}

	writeResponseEvent(w, flusher, map[string]any{
		"type":     "response.done",
		"response": doneResp,
	})
}

func writeResponseEvent(w http.ResponseWriter, flusher http.Flusher, data any) {
	jsonData, _ := json.Marshal(data)
	fmt.Fprintf(w, "data: %s\n\n", jsonData)
	flusher.Flush()
}
