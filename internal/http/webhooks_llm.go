package http

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/nextlevelbuilder/goclaw/internal/agent"
	"github.com/nextlevelbuilder/goclaw/internal/i18n"
	"github.com/nextlevelbuilder/goclaw/internal/scheduler"
	"github.com/nextlevelbuilder/goclaw/internal/security"
	"github.com/nextlevelbuilder/goclaw/internal/store"
	"github.com/nextlevelbuilder/goclaw/pkg/protocol"
)

const (
	// webhookLLMTimeout is the hard deadline for synchronous LLM invocations.
	webhookLLMTimeout = 30 * time.Second

	// webhookLLMResponseTruncate is the maximum bytes stored in the audit row response column.
	webhookLLMResponseTruncate = 32 * 1024

	// webhookLaneName is the scheduler lane name for webhook LLM calls.
	webhookLaneName = "webhook"

	// webhookLaneDefaultConcurrency is the fallback concurrency when no lane is provided.
	webhookLaneDefaultConcurrency = 4
)

// webhookLLMReq is the JSON request body for POST /v1/webhooks/llm.
// Input accepts either a plain string or a message array [{role,content}...].
type webhookLLMReq struct {
	// Input is the user prompt. Either a plain string or message array.
	// Required.
	Input json.RawMessage `json:"input"`

	// SessionKey is an optional stable conversation anchor for multi-turn conversations.
	// If omitted, a per-call ephemeral key is generated.
	SessionKey string `json:"session_key,omitempty"`

	// UserID is an optional free-form external user identifier for multi-tenant scoping.
	UserID string `json:"user_id,omitempty"`

	// Model is an optional per-request model override.
	Model string `json:"model,omitempty"`

	// Mode controls dispatch: "sync" (default) or "async".
	Mode string `json:"mode,omitempty"`

	// CallbackURL is required when mode=async. Validated against SSRF policy.
	CallbackURL string `json:"callback_url,omitempty"`

	// Metadata is optional caller-provided context echoed to callback (max 8 KB — enforced by middleware).
	Metadata json.RawMessage `json:"metadata,omitempty"`
}

// webhookInputMessage is a single turn in a structured input array.
type webhookInputMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

// webhookLLMSyncResp is the 200 response for synchronous LLM calls.
type webhookLLMSyncResp struct {
	CallID       string           `json:"call_id"`
	AgentID      string           `json:"agent_id"`
	Output       string           `json:"output"`
	Usage        *webhookLLMUsage `json:"usage,omitempty"`
	FinishReason string           `json:"finish_reason"`
}

// webhookLLMUsage mirrors providers.Usage for the response envelope.
type webhookLLMUsage struct {
	PromptTokens     int `json:"prompt_tokens"`
	CompletionTokens int `json:"completion_tokens"`
	TotalTokens      int `json:"total_tokens"`
}

// webhookLLMAsyncResp is the 202 response for asynchronous LLM calls.
type webhookLLMAsyncResp struct {
	CallID string `json:"call_id"`
	Status string `json:"status"` // always "queued"
}

// WebhookLLMHandler handles POST /v1/webhooks/llm.
// Available in all editions — auth enforced by WebhookAuthMiddleware with kind="llm".
// Sync mode: invokes agent directly with a 30s timeout.
// Async mode: enqueues a webhook_calls row for phase 07 worker.
type WebhookLLMHandler struct {
	agentRouter *agent.Router
	callStore   store.WebhookCallStore
	webhooks    store.WebhookStore
	limiter     *webhookLimiter
	lane        *scheduler.Lane
	encKey      string // AES-256-GCM key for decrypting encrypted_secret at HMAC verify time
	// syncTimeout overrides webhookLLMTimeout (30s) — set in tests only.
	syncTimeout time.Duration
}

// NewWebhookLLMHandler constructs a WebhookLLMHandler.
// lane controls concurrency for sync LLM calls (nil → uses internal default lane).
func NewWebhookLLMHandler(
	agentRouter *agent.Router,
	callStore store.WebhookCallStore,
	webhooks store.WebhookStore,
	limiter *webhookLimiter,
	lane *scheduler.Lane,
) *WebhookLLMHandler {
	if lane == nil {
		lane = scheduler.NewLane(webhookLaneName, webhookLaneDefaultConcurrency)
	}
	return &WebhookLLMHandler{
		agentRouter: agentRouter,
		callStore:   callStore,
		webhooks:    webhooks,
		limiter:     limiter,
		lane:        lane,
	}
}

// SetEncKey sets the AES-256-GCM encryption key for decrypting webhook secrets at HMAC verify time.
func (h *WebhookLLMHandler) SetEncKey(encKey string) {
	h.encKey = encKey
}

// RegisterRoutes mounts POST /v1/webhooks/llm behind the auth middleware.
// Mounted in both Standard and Lite editions (localhost_only enforced at middleware level).
func (h *WebhookLLMHandler) RegisterRoutes(mux *http.ServeMux) {
	authMW := WebhookAuthMiddleware(
		h.webhooks,
		h.callStore,
		h.limiter,
		h.encKey,
		"llm",
		WebhookMaxBodyLLM,
	)
	mux.Handle("POST /v1/webhooks/llm", authMW(http.HandlerFunc(h.handle)))
}

// handle is the HTTP handler for POST /v1/webhooks/llm.
func (h *WebhookLLMHandler) handle(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	locale := store.LocaleFromContext(ctx)

	// Webhook row always present — injected by WebhookAuthMiddleware.
	webhook := WebhookDataFromContext(ctx)
	if webhook == nil {
		writeError(w, http.StatusInternalServerError, protocol.ErrInternal,
			i18n.T(locale, i18n.MsgInternalError, "webhook context missing"))
		return
	}

	// P0: webhook must have a bound agent.
	if webhook.AgentID == nil {
		writeError(w, http.StatusBadRequest, protocol.ErrInvalidRequest,
			i18n.T(locale, i18n.MsgWebhookAgentNotFound))
		return
	}
	agentID := webhook.AgentID.String()

	// Decode and validate request body.
	var req webhookLLMReq
	if !bindJSON(w, r, locale, &req) {
		return
	}

	// Validate input field is present.
	if len(req.Input) == 0 || string(req.Input) == "null" {
		writeError(w, http.StatusBadRequest, protocol.ErrInvalidRequest,
			i18n.T(locale, i18n.MsgRequired, "input"))
		return
	}

	// Determine mode: default sync, or async when callback_url provided.
	mode := "sync"
	if req.Mode == "async" || req.CallbackURL != "" {
		mode = "async"
	}
	if req.Mode != "" && req.Mode != "sync" && req.Mode != "async" {
		writeError(w, http.StatusBadRequest, protocol.ErrInvalidRequest,
			i18n.T(locale, i18n.MsgInvalidRequest, "mode must be 'sync' or 'async'"))
		return
	}
	if mode == "async" && req.CallbackURL == "" {
		writeError(w, http.StatusBadRequest, protocol.ErrInvalidRequest,
			i18n.T(locale, i18n.MsgRequired, "callback_url"))
		return
	}

	// Parse and build user message + optional extra system prompt from input.
	userMessage, extraSystemPrompt, err := buildInput(req.Input)
	if err != nil {
		writeError(w, http.StatusBadRequest, protocol.ErrInvalidRequest,
			i18n.T(locale, i18n.MsgInvalidRequest, err.Error()))
		return
	}
	if userMessage == "" {
		writeError(w, http.StatusBadRequest, protocol.ErrInvalidRequest,
			i18n.T(locale, i18n.MsgRequired, "input"))
		return
	}

	// Resolve agent via router — uses webhook.AgentID (UUID string).
	// router.Get caches by tenantID:agentKey. UUID form incurs a fresh resolver
	// call each time (documented in router.go:90), but correctness is guaranteed.
	ag, agErr := h.agentRouter.Get(ctx, agentID)
	if agErr != nil {
		writeError(w, http.StatusNotFound, protocol.ErrNotFound,
			i18n.T(locale, i18n.MsgWebhookAgentNotFound))
		return
	}

	// P0 cross-tenant isolation: agent must belong to webhook's tenant.
	if ag.UUID() != *webhook.AgentID {
		slog.Warn("security.webhook.tenant_mismatch",
			"webhook_id", webhook.ID,
			"webhook_tenant", webhook.TenantID,
			"agent_id", agentID,
		)
		writeError(w, http.StatusForbidden, protocol.ErrUnauthorized,
			i18n.T(locale, i18n.MsgWebhookTenantMismatch))
		return
	}

	callID := store.GenNewID()
	deliveryID := store.GenNewID()
	now := time.Now()

	// Capture raw body bytes for body_hash computation.
	// req was decoded from the HTTP body; re-marshal to get canonical bytes.
	// The audit payload uses the canonical JSON shape {"body_hash":"...","meta":{...}}
	// so PG jsonb insert never triggers error 22P02.
	reqBytes, _ := json.Marshal(req)
	requestPayload, _ := buildAuditPayload(reqBytes, req)

	// Dispatch based on mode.
	switch mode {
	case "async":
		h.handleAsync(w, r, ctx, locale, webhook, ag, agentID, req, callID, deliveryID, now, requestPayload, userMessage, extraSystemPrompt)
	default: // "sync"
		h.handleSync(w, r, ctx, locale, webhook, ag, agentID, req, callID, deliveryID, now, requestPayload, userMessage, extraSystemPrompt)
	}
}

// handleSync invokes the agent within a 30s timeout and returns the response directly.
func (h *WebhookLLMHandler) handleSync(
	w http.ResponseWriter,
	r *http.Request,
	ctx context.Context,
	locale string,
	webhook *store.WebhookData,
	ag agent.Agent,
	agentID string,
	req webhookLLMReq,
	callID, deliveryID uuid.UUID,
	now time.Time,
	requestPayload []byte,
	userMessage, extraSystemPrompt string,
) {
	runID := uuid.NewString()
	sessionKey := resolveWebhookSessionKey(req.SessionKey, agentID, webhook.ID, runID)

	rr := agent.RunRequest{
		SessionKey:        sessionKey,
		Message:           userMessage,
		Channel:           "webhook",
		ChatID:            webhook.ID.String(),
		RunID:             runID,
		UserID:            req.UserID,
		Stream:            false,
		ModelOverride:     req.Model,
		ExtraSystemPrompt: extraSystemPrompt,
		HistoryLimit:      0,
		TraceName:         "webhook.llm",
		TraceTags:         []string{"webhook"},
	}

	slog.Info("webhook.llm.invoked",
		"call_id", callID,
		"mode", "sync",
		"agent_id", agentID,
		"webhook_id", webhook.ID,
		"user_id", req.UserID,
	)

	// type to propagate result from lane goroutine back to the handler.
	type runOutcome struct {
		result *agent.RunResult
		err    error
	}
	outCh := make(chan runOutcome, 1)

	// Determine the effective timeout (30s in production; overridable in tests).
	timeout := webhookLLMTimeout
	if h.syncTimeout > 0 {
		timeout = h.syncTimeout
	}

	// Acquire a webhook-lane slot; if full, return 503.
	laneCtx, laneCancel := context.WithTimeout(ctx, timeout)
	defer laneCancel()

	submitErr := h.lane.Submit(laneCtx, func() {
		// Each sync run gets its own hard timeout, isolated from request context
		// so the HTTP response write path does not race with run cancellation.
		runCtx, runCancel := context.WithTimeout(context.WithoutCancel(ctx), timeout)
		defer runCancel()

		result, err := ag.Run(runCtx, rr)
		outCh <- runOutcome{result: result, err: err}
	})

	if submitErr != nil {
		// Lane at capacity or ctx cancelled before slot acquired.
		slog.Warn("webhook.lane_saturated",
			"webhook_id", webhook.ID,
			"agent_id", agentID,
			"error", submitErr,
		)
		writeError(w, http.StatusServiceUnavailable, protocol.ErrInternal,
			i18n.T(locale, i18n.MsgWebhookLaneSaturated))
		return
	}

	// Wait for run to complete or the overall laneCtx deadline to fire.
	// The goroutine's runCtx (30s) should fire first, but we also select on
	// laneCtx so the handler isn't leaked if the goroutine stalls.
	var out runOutcome
	select {
	case out = <-outCh:
		// normal completion
	case <-laneCtx.Done():
		out = runOutcome{err: context.DeadlineExceeded}
	}

	if out.err != nil {
		completedAt := time.Now()
		if errors.Is(out.err, context.DeadlineExceeded) {
			// Write audit row as failed/timeout.
			errMsg := "context deadline exceeded"
			h.writeCallRecord(ctx, &store.WebhookCallData{
				ID:             callID,
				TenantID:       webhook.TenantID,
				WebhookID:      webhook.ID,
				AgentID:        webhook.AgentID,
				DeliveryID:     deliveryID,
				Mode:           "sync",
				Status:         "failed",
				Attempts:       1,
				RequestPayload: requestPayload,
				LastError:      &errMsg,
				CreatedAt:      now,
				CompletedAt:    &completedAt,
				StartedAt:      &now,
			})
			writeError(w, http.StatusGatewayTimeout, protocol.ErrInternal,
				i18n.T(locale, i18n.MsgWebhookLLMTimeout))
			return
		}

		// Other error.
		errMsg := out.err.Error()
		h.writeCallRecord(ctx, &store.WebhookCallData{
			ID:             callID,
			TenantID:       webhook.TenantID,
			WebhookID:      webhook.ID,
			AgentID:        webhook.AgentID,
			DeliveryID:     deliveryID,
			Mode:           "sync",
			Status:         "failed",
			Attempts:       1,
			RequestPayload: requestPayload,
			LastError:      &errMsg,
			CreatedAt:      now,
			CompletedAt:    &completedAt,
			StartedAt:      &now,
		})
		writeError(w, http.StatusInternalServerError, protocol.ErrInternal,
			i18n.T(locale, i18n.MsgInternalError, out.err.Error()))
		return
	}

	// Build response.
	resp := webhookLLMSyncResp{
		CallID:       callID.String(),
		AgentID:      agentID,
		Output:       out.result.Content,
		FinishReason: "stop",
	}
	if out.result.Usage != nil {
		resp.Usage = &webhookLLMUsage{
			PromptTokens:     out.result.Usage.PromptTokens,
			CompletionTokens: out.result.Usage.CompletionTokens,
			TotalTokens:      out.result.Usage.TotalTokens,
		}
	}

	// Persist audit row (truncate response to 32 KB).
	respBytes, _ := json.Marshal(resp)
	if len(respBytes) > webhookLLMResponseTruncate {
		respBytes = respBytes[:webhookLLMResponseTruncate]
	}

	completedAt := time.Now()
	h.writeCallRecord(ctx, &store.WebhookCallData{
		ID:             callID,
		TenantID:       webhook.TenantID,
		WebhookID:      webhook.ID,
		AgentID:        webhook.AgentID,
		DeliveryID:     deliveryID,
		Mode:           "sync",
		Status:         "done",
		Attempts:       1,
		RequestPayload: requestPayload,
		Response:       respBytes,
		CreatedAt:      now,
		CompletedAt:    &completedAt,
		StartedAt:      &now,
	})

	slog.Info("webhook.llm.sync",
		"call_id", callID,
		"agent_id", agentID,
		"webhook_id", webhook.ID,
		"output_len", len(out.result.Content),
	)

	writeJSON(w, http.StatusOK, resp)
}

// handleAsync enqueues a webhook_calls row and returns 202 immediately.
func (h *WebhookLLMHandler) handleAsync(
	w http.ResponseWriter,
	_ *http.Request,
	ctx context.Context,
	locale string,
	webhook *store.WebhookData,
	_ agent.Agent,
	agentID string,
	req webhookLLMReq,
	callID, deliveryID uuid.UUID,
	now time.Time,
	requestPayload []byte,
	_, _ string, // userMessage, extraSystemPrompt — stored in requestPayload, not used here
) {
	// SSRF validation on callback_url — defense against DNS rebinding.
	if _, _, err := security.Validate(req.CallbackURL); err != nil {
		slog.Warn("security.webhook.callback_url_blocked",
			"webhook_id", webhook.ID,
			"url_hint", redactedHost(req.CallbackURL),
			"error", err,
		)
		writeError(w, http.StatusBadRequest, protocol.ErrInvalidRequest,
			i18n.T(locale, i18n.MsgWebhookCallbackURLInvalid))
		return
	}

	cbURL := req.CallbackURL
	nextAttempt := now

	call := &store.WebhookCallData{
		ID:             callID,
		TenantID:       webhook.TenantID,
		WebhookID:      webhook.ID,
		AgentID:        webhook.AgentID,
		DeliveryID:     deliveryID,
		Mode:           "async",
		Status:         "queued",
		CallbackURL:    &cbURL,
		NextAttemptAt:  &nextAttempt,
		RequestPayload: requestPayload,
		Attempts:       0,
		CreatedAt:      now,
	}

	if err := h.callStore.Create(ctx, call); err != nil {
		slog.Error("webhook.llm.async_enqueue_failed",
			"error", err,
			"call_id", callID,
			"webhook_id", webhook.ID,
		)
		writeError(w, http.StatusInternalServerError, protocol.ErrInternal,
			i18n.T(locale, i18n.MsgInternalError, "failed to enqueue"))
		return
	}

	slog.Info("webhook.llm.async_enqueued",
		"call_id", callID,
		"delivery_id", deliveryID,
		"agent_id", agentID,
		"webhook_id", webhook.ID,
	)

	writeJSON(w, http.StatusAccepted, webhookLLMAsyncResp{
		CallID: callID.String(),
		Status: "queued",
	})
}

// writeCallRecord persists an audit call record. Best-effort — failures are logged but not fatal.
func (h *WebhookLLMHandler) writeCallRecord(ctx context.Context, call *store.WebhookCallData) {
	if err := h.callStore.Create(ctx, call); err != nil {
		slog.Warn("webhook.llm.audit_write_failed",
			"error", err,
			"call_id", call.ID,
		)
	}
}

// buildInput parses the raw JSON input into a user message and optional extra system prompt.
//
// Two formats are accepted:
//  1. Plain string: used verbatim as the user message.
//  2. Array of {role, content} objects: non-system roles concatenated as the user message;
//     system entries contribute to ExtraSystemPrompt.
//
// v2 note: full multi-turn array support (passing turns directly to RunRequest) is deferred.
func buildInput(raw json.RawMessage) (userMessage string, extraSystemPrompt string, err error) {
	// Try plain string first.
	var s string
	if json.Unmarshal(raw, &s) == nil {
		return s, "", nil
	}

	// Try message array.
	var msgs []webhookInputMessage
	if err := json.Unmarshal(raw, &msgs); err != nil {
		return "", "", fmt.Errorf("input must be a string or array of {role,content} objects: %w", err)
	}

	var userParts, systemParts []string
	for _, m := range msgs {
		switch strings.ToLower(m.Role) {
		case "system":
			if m.Content != "" {
				systemParts = append(systemParts, m.Content)
			}
		default: // "user", "assistant", anything else treated as user content
			if m.Content != "" {
				userParts = append(userParts, m.Content)
			}
		}
	}

	return strings.Join(userParts, "\n"), strings.Join(systemParts, "\n"), nil
}

// resolveWebhookSessionKey returns a stable or ephemeral session key.
// If the caller provides a sessionKey, it is used verbatim for conversation continuity.
// Otherwise, an ephemeral key is generated per-call.
func resolveWebhookSessionKey(reqSessionKey, agentID string, webhookID uuid.UUID, runID string) string {
	if reqSessionKey != "" {
		return reqSessionKey
	}
	return fmt.Sprintf("webhook:%s:%s:%s", agentID, webhookID.String(), runID[:8])
}

