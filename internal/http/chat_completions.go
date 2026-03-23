package http

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"time"

	"github.com/google/uuid"

	"github.com/nextlevelbuilder/goclaw/internal/agent"
	"github.com/nextlevelbuilder/goclaw/internal/i18n"
	"github.com/nextlevelbuilder/goclaw/internal/permissions"
	"github.com/nextlevelbuilder/goclaw/internal/sessions"
	"github.com/nextlevelbuilder/goclaw/internal/store"
	"github.com/nextlevelbuilder/goclaw/internal/tools"
)

// ChatCompletionsHandler handles POST /v1/chat/completions (OpenAI-compatible).
type ChatCompletionsHandler struct {
	agents      *agent.Router
	sessions    store.SessionStore
	token       string // expected bearer token (empty = no auth)
	isManaged   bool
	rateLimiter func(string) bool // rate limit check: key → allowed (nil = no limit)
	postTurn    tools.PostTurnProcessor
}

// SetPostTurnProcessor sets the post-turn processor for team task dispatch.
func (h *ChatCompletionsHandler) SetPostTurnProcessor(pt tools.PostTurnProcessor) {
	h.postTurn = pt
}

// NewChatCompletionsHandler creates a handler for the chat completions endpoint.
func NewChatCompletionsHandler(agents *agent.Router, sess store.SessionStore, token string, isManaged bool) *ChatCompletionsHandler {
	return &ChatCompletionsHandler{
		agents:    agents,
		sessions:  sess,
		token:     token,
		isManaged: isManaged,
	}
}

// SetRateLimiter sets the rate limiter function for HTTP requests.
func (h *ChatCompletionsHandler) SetRateLimiter(fn func(string) bool) {
	h.rateLimiter = fn
}

type chatCompletionsRequest struct {
	Model    string        `json:"model"`
	Messages []chatMessage `json:"messages"`
	Stream   bool          `json:"stream"`
	User     string        `json:"user,omitempty"`
}

type chatMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
	Name    string `json:"name,omitempty"`
}

type chatCompletionsResponse struct {
	ID      string       `json:"id"`
	Object  string       `json:"object"`
	Created int64        `json:"created"`
	Model   string       `json:"model"`
	Choices []chatChoice `json:"choices"`
	Usage   *chatUsage   `json:"usage,omitempty"`
}

type chatChoice struct {
	Index        int          `json:"index"`
	Message      *chatMessage `json:"message,omitempty"`
	Delta        *chatMessage `json:"delta,omitempty"`
	FinishReason string       `json:"finish_reason,omitempty"`
}

type chatUsage struct {
	PromptTokens     int `json:"prompt_tokens"`
	CompletionTokens int `json:"completion_tokens"`
	TotalTokens      int `json:"total_tokens"`
}

func (h *ChatCompletionsHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	locale := extractLocale(r)

	if r.Method != http.MethodPost {
		http.Error(w, i18n.T(locale, i18n.MsgMethodNotAllowed), http.StatusMethodNotAllowed)
		return
	}

	// Auth + RBAC check (gateway token or API key, operator required for POST)
	auth := resolveAuth(r, h.token)
	if !auth.Authenticated {
		http.Error(w, fmt.Sprintf(`{"error":{"message":"%s","type":"invalid_request_error"}}`, i18n.T(locale, i18n.MsgInvalidAuth)), http.StatusUnauthorized)
		return
	}
	if !permissions.HasMinRole(auth.Role, permissions.RoleOperator) {
		http.Error(w, fmt.Sprintf(`{"error":{"message":"%s","type":"invalid_request_error"}}`, i18n.T(locale, i18n.MsgPermissionDenied, "/v1/chat/completions")), http.StatusForbidden)
		return
	}

	// Rate limit check (per IP or bearer token)
	if h.rateLimiter != nil {
		key := r.RemoteAddr
		if token := extractBearerToken(r); token != "" {
			key = "token:" + token
		}
		if !h.rateLimiter(key) {
			w.Header().Set("Retry-After", "60")
			http.Error(w, fmt.Sprintf(`{"error":{"message":"%s","type":"rate_limit_error"}}`, i18n.T(locale, i18n.MsgRateLimitExceeded)), http.StatusTooManyRequests)
			return
		}
	}

	// Limit request body size to prevent DoS
	const maxRequestBodySize = 1 << 20 // 1MB
	r.Body = http.MaxBytesReader(w, r.Body, maxRequestBodySize)

	var req chatCompletionsRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, fmt.Sprintf(`{"error":{"message":"%s"}}`, i18n.T(locale, i18n.MsgInvalidRequest, err.Error())), http.StatusBadRequest)
		return
	}

	if len(req.Messages) == 0 {
		http.Error(w, fmt.Sprintf(`{"error":{"message":"%s"}}`, i18n.T(locale, i18n.MsgMsgsRequired)), http.StatusBadRequest)
		return
	}

	agentID := extractAgentID(r, req.Model)
	userID := extractUserID(r)
	if h.isManaged && userID == "" {
		http.Error(w, fmt.Sprintf(`{"error":{"message":"%s"}}`, i18n.T(locale, i18n.MsgUserIDHeader)), http.StatusBadRequest)
		return
	}

	loop, err := h.agents.Get(r.Context(), agentID)
	if err != nil {
		http.Error(w, fmt.Sprintf(`{"error":{"message":"%s"}}`, i18n.T(locale, i18n.MsgNotFound, "agent", agentID)), http.StatusNotFound)
		return
	}

	// Extract the last user message
	var lastMessage string
	for i := len(req.Messages) - 1; i >= 0; i-- {
		if req.Messages[i].Role == "user" {
			lastMessage = req.Messages[i].Content
			break
		}
	}
	if lastMessage == "" {
		http.Error(w, fmt.Sprintf(`{"error":{"message":"%s"}}`, i18n.T(locale, i18n.MsgNoUserMessage)), http.StatusBadRequest)
		return
	}

	// Inject user_id and locale into context for downstream stores/tools
	ctx := store.WithLocale(r.Context(), extractLocale(r))
	if userID != "" {
		ctx = store.WithUserID(ctx, userID)
	}

	runID := uuid.NewString()
	// Include userID in session key for multi-tenant isolation
	sessionSuffix := "http-" + runID[:8]
	if userID != "" {
		sessionSuffix = "http-" + userID + "-" + runID[:8]
	}
	sessionKey := sessions.SessionKey(agentID, sessionSuffix)

	slog.Info("chat completions request", "agent", agentID, "stream", req.Stream, "user", userID)

	if req.Stream {
		h.handleStream(w, r.WithContext(ctx), loop, runID, sessionKey, lastMessage, req.Model, userID)
	} else {
		h.handleNonStream(w, r.WithContext(ctx), loop, runID, sessionKey, lastMessage, req.Model, userID)
	}
}

func (h *ChatCompletionsHandler) handleNonStream(w http.ResponseWriter, r *http.Request, loop agent.Agent, runID, sessionKey, message, model, userID string) {
	ctx, drainTeamDispatch := tools.InjectTeamDispatch(r.Context(), h.postTurn)
	defer drainTeamDispatch()

	result, err := loop.Run(ctx, agent.RunRequest{
		SessionKey: sessionKey,
		Message:    message,
		Channel:    "http",
		ChatID:     "api",
		RunID:      runID,
		UserID:     userID,
		Stream:     false,
	})

	if err != nil {
		locale := store.LocaleFromContext(r.Context())
		http.Error(w, fmt.Sprintf(`{"error":{"message":"%s"}}`, i18n.T(locale, i18n.MsgInternalError, err.Error())), http.StatusInternalServerError)
		return
	}

	resp := chatCompletionsResponse{
		ID:      "chatcmpl-" + runID[:8],
		Object:  "chat.completion",
		Created: time.Now().Unix(),
		Model:   model,
		Choices: []chatChoice{{
			Index:        0,
			Message:      &chatMessage{Role: "assistant", Content: SignFileURLs(result.Content, FileSigningKey())},
			FinishReason: "stop",
		}},
	}

	if result.Usage != nil {
		resp.Usage = &chatUsage{
			PromptTokens:     result.Usage.PromptTokens,
			CompletionTokens: result.Usage.CompletionTokens,
			TotalTokens:      result.Usage.TotalTokens,
		}
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(resp)
}

func (h *ChatCompletionsHandler) handleStream(w http.ResponseWriter, r *http.Request, loop agent.Agent, runID, sessionKey, message, model, userID string) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		locale := store.LocaleFromContext(r.Context())
		http.Error(w, i18n.T(locale, i18n.MsgStreamingNotSupported), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.WriteHeader(http.StatusOK)

	completionID := "chatcmpl-" + runID[:8]

	// Send initial role chunk
	writeSSEChunk(w, flusher, completionID, model, &chatMessage{Role: "assistant"}, "")

	ctx, drainTeamDispatch := tools.InjectTeamDispatch(r.Context(), h.postTurn)
	defer drainTeamDispatch()

	result, err := loop.Run(ctx, agent.RunRequest{
		SessionKey: sessionKey,
		Message:    message,
		Channel:    "http",
		ChatID:     "api",
		RunID:      runID,
		UserID:     userID,
		Stream:     true,
	})

	if err != nil {
		writeSSEChunk(w, flusher, completionID, model, &chatMessage{Content: "Error: " + err.Error()}, "stop")
	} else {
		// Send content chunk
		writeSSEChunk(w, flusher, completionID, model, &chatMessage{Content: SignFileURLs(result.Content, FileSigningKey())}, "stop")
	}

	// Send [DONE]
	fmt.Fprintf(w, "data: [DONE]\n\n")
	flusher.Flush()
}

func writeSSEChunk(w http.ResponseWriter, flusher http.Flusher, id, model string, delta *chatMessage, finishReason string) {
	chunk := map[string]any{
		"id":      id,
		"object":  "chat.completion.chunk",
		"created": time.Now().Unix(),
		"model":   model,
		"choices": []map[string]any{{
			"index":         0,
			"delta":         delta,
			"finish_reason": nilIfEmpty(finishReason),
		}},
	}

	data, _ := json.Marshal(chunk)
	fmt.Fprintf(w, "data: %s\n\n", data)
	flusher.Flush()
}

func nilIfEmpty(s string) any {
	if s == "" {
		return nil
	}
	return s
}
