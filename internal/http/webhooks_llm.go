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
	media "github.com/nextlevelbuilder/goclaw/internal/channels/media"
	"github.com/nextlevelbuilder/goclaw/internal/i18n"
	"github.com/nextlevelbuilder/goclaw/internal/providers"
	"github.com/nextlevelbuilder/goclaw/internal/scheduler"
	"github.com/nextlevelbuilder/goclaw/internal/security"
	"github.com/nextlevelbuilder/goclaw/internal/store"
	"github.com/nextlevelbuilder/goclaw/internal/webhooks"
	"github.com/nextlevelbuilder/goclaw/pkg/protocol"
)

const (
	// webhookLLMTimeout is the default hard deadline for webhook LLM invocations
	// (sync + admin test). Overridable per-handler via syncTimeout from config.
	webhookLLMTimeout = 600 * time.Second

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
	// Required unless Media is non-empty.
	Input json.RawMessage `json:"input"`

	// Media is an optional list of remote attachments the gateway fetches
	// server-side and hands to the agent as local files. Max 10 items, 25 MB
	// each. Sync mode only - see the async guard in handle().
	Media []webhooks.InboundMediaItem `json:"media,omitempty"`

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
	CallID       string                `json:"call_id"`
	AgentID      string                `json:"agent_id"`
	Output       string                `json:"output"`
	Usage        *webhookLLMUsage      `json:"usage,omitempty"`
	FinishReason string                `json:"finish_reason"`
	Calls        []providers.CallUsage `json:"calls,omitempty"`
	TotalCostUSD float64               `json:"total_cost_usd,omitempty"`
}

// webhookLLMUsage mirrors providers.Usage for the response envelope.
type webhookLLMUsage struct {
	PromptTokens                      int  `json:"prompt_tokens"`
	CompletionTokens                  int  `json:"completion_tokens"`
	TotalTokens                       int  `json:"total_tokens"`
	CacheReadTokens                   int  `json:"cache_read_input_tokens,omitempty"`
	CacheCreationTokens               int  `json:"cache_creation_input_tokens,omitempty"`
	PromptTokensIncludeCachedSegments bool `json:"prompt_tokens_include_cached_segments,omitempty"`
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
	// syncTimeout overrides the webhookLLMTimeout default (600s); set from
	// gateway.webhook_sync_timeout_sec config (and in tests). 0 → use default.
	syncTimeout time.Duration
	// stream controls whether sync + test webhook agent runs stream provider
	// responses so the upstream can populate/serve its prompt cache. The response
	// returned to the caller is unchanged (assembled JSON). Set from
	// gateway.webhook_stream config via webhooks.ResolveStream (default true).
	stream bool
}

// NewWebhookLLMHandler constructs a WebhookLLMHandler.
// lane controls concurrency for sync LLM calls (nil → uses internal default lane).
func NewWebhookLLMHandler(
	agentRouter *agent.Router,
	callStore store.WebhookCallStore,
	webhooks store.WebhookStore,
	limiter *webhookLimiter,
	lane *scheduler.Lane,
	syncTimeout time.Duration,
	stream bool,
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
		syncTimeout: syncTimeout,
		stream:      stream,
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

	// input is optional when media is present: an image with no caption is a
	// legitimate request. A request carrying neither is still rejected.
	hasInput := len(req.Input) > 0 && string(req.Input) != "null"
	if !hasInput && len(req.Media) == 0 {
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

	// Async + media is refused, not deferred. requestPayload is frozen below,
	// before the mode dispatch, and handleAsync stores that byte slice
	// verbatim - so server-computed local paths have no way to reach the
	// worker. An accepted request would run the agent with no media and no
	// media tag, then answer confidently about an image nobody sent, with
	// status "done" and nothing in last_error. Rejecting here means nothing is
	// downloaded and no row is enqueued for a call that could never succeed.
	if len(req.Media) > 0 && mode == "async" {
		writeError(w, http.StatusBadRequest, protocol.ErrInvalidRequest,
			i18n.T(locale, i18n.MsgInvalidRequest, "media is not supported in async mode"))
		return
	}

	// Parse and build user message + optional extra system prompt from input.
	var userMessage, extraSystemPrompt string
	if hasInput {
		var err error
		userMessage, extraSystemPrompt, err = buildInput(req.Input)
		if err != nil {
			writeError(w, http.StatusBadRequest, protocol.ErrInvalidRequest,
				i18n.T(locale, i18n.MsgInvalidRequest, err.Error()))
			return
		}
	}
	if userMessage == "" && len(req.Media) == 0 {
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

	// Capture raw body bytes for body_hash computation when middleware supplied them.
	// Direct handler tests fall back to canonical JSON bytes from the decoded request.
	// The audit payload uses the canonical JSON shape {"body_hash":"...","meta":{...}}
	// so PG jsonb insert never triggers error 22P02.
	reqBytes := WebhookRawBodyFromContext(ctx)
	if reqBytes == nil {
		reqBytes, _ = json.Marshal(req)
	}
	// meta is surfaced verbatim by GET /v1/webhooks/{id}/calls/{callId} and kept
	// for the 30-day retention window. The whole point of media[] is short-lived
	// signed CDN URLs, and that signature is a bearer credential - store the URL
	// with its query stripped, never raw. This stays cheap only because media is
	// carried by URL; a future base64 option must drop the field here entirely.
	requestPayload, _ := buildAuditPayload(reqBytes, redactedAuditReq(req))
	idempotencyKey := optionalIdempotencyKey(r)

	// ONE deadline covers the download and the agent run, so a sync request
	// never outlives gateway.webhook_sync_timeout_sec no matter how the time is
	// split. Without it the fetch would run on the bare request context, bounded
	// only by the fetcher's 5-minute-per-item client timeout — ten items in
	// sequence is nearly an hour of request-goroutine occupancy that no
	// operator setting could shorten.
	deadline := time.Now().Add(h.effectiveSyncTimeout())

	// Fetch media BEFORE a lane slot is taken. The webhook lane's concurrency
	// budget is small (4 by default) and a slow download must not hold a slot.
	// Disk is bounded by the fetcher's own byte budget rather than by the rate
	// limiter, whose allow() returns true whenever rpm <= 0.
	var fetched webhooks.InboundMediaResult
	if len(req.Media) > 0 {
		fetchCtx, cancelFetch := context.WithDeadline(ctx, deadline)
		fetched = webhooks.FetchInboundMedia(fetchCtx, req.Media)
		cancelFetch()
		if len(fetched.Failures) > 0 {
			// All-or-nothing: FetchInboundMedia already removed its own temps.
			writeMediaFailure(w, locale, webhooks.WorstFailure(fetched.Failures))
			return
		}
	}

	// Dispatch based on mode.
	switch mode {
	case "async":
		h.handleAsync(w, r, ctx, locale, webhook, ag, agentID, req, callID, deliveryID, now, requestPayload, idempotencyKey, userMessage, extraSystemPrompt)
	default: // "sync"
		h.handleSync(w, r, ctx, locale, webhook, ag, agentID, req, callID, deliveryID, now, requestPayload, idempotencyKey, userMessage, extraSystemPrompt, fetched, deadline)
	}
}

// effectiveSyncTimeout is the configured budget for one sync call
// (gateway.webhook_sync_timeout_sec), falling back to the package default.
func (h *WebhookLLMHandler) effectiveSyncTimeout() time.Duration {
	if h.syncTimeout > 0 {
		return h.syncTimeout
	}
	return webhookLLMTimeout
}

// writeMediaFailure maps a media failure to a status code deterministically.
//
// The code comes from WorstFailure, which applies a fixed severity order across
// every failed item, so the status never depends on which array slot happened
// to fail. The i18n message is the ONLY thing the caller sees: no hostname, no
// IP, no port, no error string. The detail is already in slog.
func writeMediaFailure(w http.ResponseWriter, locale string, f webhooks.InboundMediaFailure) {
	switch f.Code {
	case webhooks.FailSSRF:
		writeError(w, http.StatusBadRequest, protocol.ErrInvalidRequest,
			i18n.T(locale, i18n.MsgWebhookMediaSSRFBlocked))
	case webhooks.FailTooLarge:
		writeError(w, http.StatusRequestEntityTooLarge, protocol.ErrInvalidRequest,
			i18n.T(locale, i18n.MsgWebhookMediaTooLarge))
	case webhooks.FailMIMEDenied:
		writeError(w, http.StatusUnsupportedMediaType, protocol.ErrInvalidRequest,
			i18n.T(locale, i18n.MsgWebhookMediaMIMEDenied))
	case webhooks.FailBudgetExhausted:
		// Transient and server-side: the caller should retry, not rewrite the
		// request. Ranked lowest by WorstFailure so a genuinely bad item is
		// never reported as retryable.
		w.Header().Set("Retry-After", "5")
		writeError(w, http.StatusServiceUnavailable, protocol.ErrInternal,
			i18n.T(locale, i18n.MsgWebhookMediaBudgetExhausted))
	default:
		writeError(w, http.StatusBadRequest, protocol.ErrInvalidRequest,
			i18n.T(locale, i18n.MsgWebhookMediaDownloadFailed))
	}
}

// redactedAuditReq returns a copy of req whose media URLs have query string and
// userinfo stripped, for persistence into the audit row.
func redactedAuditReq(req webhookLLMReq) webhookLLMReq {
	if len(req.Media) == 0 {
		return req
	}
	safe := make([]webhooks.InboundMediaItem, len(req.Media))
	for i, item := range req.Media {
		safe[i] = webhooks.InboundMediaItem{
			URL:      security.RedactURL(item.URL),
			Filename: item.Filename,
		}
	}
	req.Media = safe
	return req
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
	idempotencyKey *string,
	userMessage, extraSystemPrompt string,
	fetched webhooks.InboundMediaResult,
	deadline time.Time,
) {
	runID := uuid.NewString()
	sessionKey := resolveWebhookSessionKey(req.SessionKey, agentID, webhook.ID, runID)
	callRecord := &store.WebhookCallData{
		ID:             callID,
		TenantID:       webhook.TenantID,
		WebhookID:      webhook.ID,
		AgentID:        webhook.AgentID,
		DeliveryID:     deliveryID,
		IdempotencyKey: idempotencyKey,
		Mode:           "sync",
		Status:         "running",
		Attempts:       0,
		RequestPayload: requestPayload,
		CreatedAt:      now,
		StartedAt:      &now,
	}
	callReserved, handled := reserveIdempotentCall(w, r, h.callStore, callRecord)
	if handled {
		// The run never starts, so nothing downstream will own these files.
		webhooks.CleanupInboundMedia(fetched.Files)
		return
	}

	// Media tags are load-bearing, not cosmetic. enrichImageIDs rewrites an
	// EXISTING <media:image> tag via replaceFirstMediaTag; it never inserts one.
	// When the agent has a dedicated read_image provider (file-ref vision mode)
	// the images are not attached to the main LLM at all, so the tag is the only
	// thing telling the model an image exists. No tag means a silent no-op: no
	// error, no log, wrong answer - and only on that configuration, so it works
	// on a dev box and fails at a customer.
	//
	// No skip annotations: all-or-nothing means the run only starts once every
	// item succeeded, so fetched.Failures is empty here by construction.
	message := userMessage
	if len(fetched.Infos) > 0 {
		if tags := media.BuildMediaTags(fetched.Infos); tags != "" {
			if message != "" {
				message = tags + "\n\n" + message
			} else {
				message = tags
			}
		}
	}

	rr := agent.RunRequest{
		SessionKey:        sessionKey,
		Message:           message,
		Media:             fetched.Files,
		Channel:           "webhook",
		ChatID:            webhook.ID.String(),
		RunID:             runID,
		UserID:            req.UserID,
		Stream:            h.stream,
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

	// deadline was set in handle() before the media fetch, so whatever the
	// download consumed is already subtracted from the run's share.

	// Acquire a webhook-lane slot; if full, return 503.
	laneCtx, laneCancel := context.WithDeadline(ctx, deadline)
	defer laneCancel()

	submitErr := h.lane.Submit(laneCtx, func() {
		// Cleanup belongs to the RUN, not to the handler. Submit runs fn in a
		// detached goroutine and returns immediately; handleSync then selects on
		// outCh or laneCtx.Done(), and laneCtx derives from the REQUEST context
		// while the run below uses context.WithoutCancel. A deferred cleanup in
		// the handler would therefore delete these files on client disconnect or
		// lane timeout while ag.Run is still executing - persistMedia would fail
		// its copy, log a warning, drop the ref, and the agent would spend
		// minutes answering about a <media:image> tag with no image behind it.
		defer webhooks.CleanupInboundMedia(fetched.Files)

		// Each sync run gets its own hard deadline, isolated from the request
		// context so the HTTP response write path does not race with run
		// cancellation. Same instant as laneCtx, so download plus run share one
		// budget rather than each getting a full one.
		runCtx, runCancel := context.WithDeadline(context.WithoutCancel(ctx), deadline)
		defer runCancel()

		result, err := ag.Run(runCtx, rr)
		outCh <- runOutcome{result: result, err: err}
	})

	if submitErr != nil {
		// The closure never ran, so its deferred cleanup never will either.
		webhooks.CleanupInboundMedia(fetched.Files)
		completedAt := time.Now()
		errMsg := submitErr.Error()
		callRecord.Status = "failed"
		callRecord.Attempts = 1
		callRecord.CompletedAt = &completedAt
		callRecord.LastError = &errMsg
		persistWebhookCall(ctx, h.callStore, callRecord, callReserved, "webhook.llm.audit_write_failed")
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
			callRecord.Status = "failed"
			callRecord.Attempts = 1
			callRecord.LastError = &errMsg
			callRecord.CompletedAt = &completedAt
			persistWebhookCall(ctx, h.callStore, callRecord, callReserved, "webhook.llm.audit_write_failed")
			writeError(w, http.StatusGatewayTimeout, protocol.ErrInternal,
				i18n.T(locale, i18n.MsgWebhookLLMTimeout))
			return
		}

		// Other error.
		errMsg := out.err.Error()
		callRecord.Status = "failed"
		callRecord.Attempts = 1
		callRecord.LastError = &errMsg
		callRecord.CompletedAt = &completedAt
		persistWebhookCall(ctx, h.callStore, callRecord, callReserved, "webhook.llm.audit_write_failed")
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
	if len(out.result.Calls) > 0 {
		resp.Calls = out.result.Calls
		resp.TotalCostUSD = providers.SumCallCost(out.result.Calls)
		sum := providers.SumCallUsage(out.result.Calls)
		resp.Usage = &webhookLLMUsage{
			PromptTokens:                      sum.PromptTokens,
			CompletionTokens:                  sum.CompletionTokens,
			TotalTokens:                       sum.TotalTokens,
			CacheReadTokens:                   sum.CacheReadTokens,
			CacheCreationTokens:               sum.CacheCreationTokens,
			PromptTokensIncludeCachedSegments: sum.PromptTokensIncludeCachedSegments,
		}
	} else if out.result.Usage != nil {
		resp.Usage = &webhookLLMUsage{
			PromptTokens:                      out.result.Usage.PromptTokens,
			CompletionTokens:                  out.result.Usage.CompletionTokens,
			TotalTokens:                       out.result.Usage.TotalTokens,
			CacheReadTokens:                   out.result.Usage.CacheReadTokens,
			CacheCreationTokens:               out.result.Usage.CacheCreationTokens,
			PromptTokensIncludeCachedSegments: out.result.Usage.PromptTokensIncludeCachedSegments,
		}
	}

	// Persist audit row (truncate response to 32 KB).
	respBytes, _ := json.Marshal(resp)
	if len(respBytes) > webhookLLMResponseTruncate {
		respBytes = respBytes[:webhookLLMResponseTruncate]
	}

	completedAt := time.Now()
	callRecord.Status = "done"
	callRecord.Attempts = 1
	callRecord.Response = respBytes
	callRecord.CompletedAt = &completedAt
	persistWebhookCall(ctx, h.callStore, callRecord, callReserved, "webhook.llm.audit_write_failed")

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
	r *http.Request,
	ctx context.Context,
	locale string,
	webhook *store.WebhookData,
	_ agent.Agent,
	agentID string,
	req webhookLLMReq,
	callID, deliveryID uuid.UUID,
	now time.Time,
	requestPayload []byte,
	idempotencyKey *string,
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
		IdempotencyKey: idempotencyKey,
		Mode:           "async",
		Status:         "queued",
		CallbackURL:    &cbURL,
		NextAttemptAt:  &nextAttempt,
		RequestPayload: requestPayload,
		Attempts:       0,
		CreatedAt:      now,
	}

	if err := h.callStore.Create(ctx, call); err != nil {
		if idempotencyKey != nil && errors.Is(err, store.ErrIdempotencyConflict) {
			if replayStoredIdempotencyFromPayload(w, r, h.callStore, webhook.ID, *idempotencyKey, requestPayload) {
				return
			}
		}
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

// RunTest performs a synchronous test invocation of an llm-kind webhook on behalf of an
// admin (POST /v1/webhooks/{id}/test). The caller (WebhooksAdminHandler) has already
// authorized the request and verified webhook ownership — no webhook secret is involved.
// It resolves the bound agent, runs it within the standard sync timeout, writes an audit
// row (mode=sync), and returns the response. Errors are returned (not written to HTTP).
func (h *WebhookLLMHandler) RunTest(ctx context.Context, wh *store.WebhookData, input, model string) (*webhookLLMSyncResp, error) {
	if wh == nil || wh.AgentID == nil {
		return nil, errors.New("webhook has no bound agent")
	}
	agentID := wh.AgentID.String()

	ag, agErr := h.agentRouter.Get(ctx, agentID)
	if agErr != nil {
		return nil, fmt.Errorf("agent not found: %w", agErr)
	}
	// P0 cross-tenant isolation: agent must belong to webhook's tenant.
	if ag.UUID() != *wh.AgentID {
		slog.Warn("security.webhook.tenant_mismatch", "webhook_id", wh.ID, "webhook_tenant", wh.TenantID, "agent_id", agentID)
		return nil, errors.New("agent tenant mismatch")
	}

	callID := store.GenNewID()
	deliveryID := store.GenNewID()
	now := time.Now()
	runID := uuid.NewString()
	sessionKey := resolveWebhookSessionKey("", agentID, wh.ID, runID)

	requestPayload, _ := buildAuditPayload(nil, map[string]any{"test": true, "input_len": len(input)})
	callRecord := &store.WebhookCallData{
		ID:             callID,
		TenantID:       wh.TenantID,
		WebhookID:      wh.ID,
		AgentID:        wh.AgentID,
		DeliveryID:     deliveryID,
		Mode:           "sync",
		Status:         "running",
		RequestPayload: requestPayload,
		CreatedAt:      now,
		StartedAt:      &now,
	}

	rr := agent.RunRequest{
		SessionKey:    sessionKey,
		Message:       input,
		Channel:       "webhook",
		ChatID:        wh.ID.String(),
		RunID:         runID,
		Stream:        h.stream,
		ModelOverride: model,
		TraceName:     "webhook.llm.test",
		TraceTags:     []string{"webhook", "test"},
	}

	timeout := webhookLLMTimeout
	if h.syncTimeout > 0 {
		timeout = h.syncTimeout
	}

	type runOutcome struct {
		result *agent.RunResult
		err    error
	}
	outCh := make(chan runOutcome, 1)

	laneCtx, laneCancel := context.WithTimeout(ctx, timeout)
	defer laneCancel()

	submitErr := h.lane.Submit(laneCtx, func() {
		runCtx, runCancel := context.WithTimeout(context.WithoutCancel(ctx), timeout)
		defer runCancel()
		result, err := ag.Run(runCtx, rr)
		outCh <- runOutcome{result: result, err: err}
	})
	if submitErr != nil {
		completedAt := time.Now()
		errMsg := submitErr.Error()
		callRecord.Status = "failed"
		callRecord.Attempts = 1
		callRecord.CompletedAt = &completedAt
		callRecord.LastError = &errMsg
		persistWebhookCall(ctx, h.callStore, callRecord, false, "webhook.llm.test_audit_write_failed")
		return nil, fmt.Errorf("webhook lane saturated: %w", submitErr)
	}

	var out runOutcome
	select {
	case out = <-outCh:
	case <-laneCtx.Done():
		out = runOutcome{err: context.DeadlineExceeded}
	}

	if out.err != nil {
		completedAt := time.Now()
		errMsg := out.err.Error()
		callRecord.Status = "failed"
		callRecord.Attempts = 1
		callRecord.CompletedAt = &completedAt
		callRecord.LastError = &errMsg
		persistWebhookCall(ctx, h.callStore, callRecord, false, "webhook.llm.test_audit_write_failed")
		return nil, out.err
	}

	resp := &webhookLLMSyncResp{
		CallID:       callID.String(),
		AgentID:      agentID,
		Output:       out.result.Content,
		FinishReason: "stop",
	}
	if len(out.result.Calls) > 0 {
		resp.Calls = out.result.Calls
		resp.TotalCostUSD = providers.SumCallCost(out.result.Calls)
		sum := providers.SumCallUsage(out.result.Calls)
		resp.Usage = &webhookLLMUsage{
			PromptTokens:                      sum.PromptTokens,
			CompletionTokens:                  sum.CompletionTokens,
			TotalTokens:                       sum.TotalTokens,
			CacheReadTokens:                   sum.CacheReadTokens,
			CacheCreationTokens:               sum.CacheCreationTokens,
			PromptTokensIncludeCachedSegments: sum.PromptTokensIncludeCachedSegments,
		}
	} else if out.result.Usage != nil {
		resp.Usage = &webhookLLMUsage{
			PromptTokens:                      out.result.Usage.PromptTokens,
			CompletionTokens:                  out.result.Usage.CompletionTokens,
			TotalTokens:                       out.result.Usage.TotalTokens,
			CacheReadTokens:                   out.result.Usage.CacheReadTokens,
			CacheCreationTokens:               out.result.Usage.CacheCreationTokens,
			PromptTokensIncludeCachedSegments: out.result.Usage.PromptTokensIncludeCachedSegments,
		}
	}

	respBytes, _ := json.Marshal(resp)
	if len(respBytes) > webhookLLMResponseTruncate {
		respBytes = respBytes[:webhookLLMResponseTruncate]
	}
	completedAt := time.Now()
	callRecord.Status = "done"
	callRecord.Attempts = 1
	callRecord.Response = respBytes
	callRecord.CompletedAt = &completedAt
	persistWebhookCall(ctx, h.callStore, callRecord, false, "webhook.llm.test_audit_write_failed")

	slog.Info("webhook.llm.test", "call_id", callID, "agent_id", agentID, "webhook_id", wh.ID, "output_len", len(out.result.Content))
	return resp, nil
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
