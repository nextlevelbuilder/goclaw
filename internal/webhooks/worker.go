// Package webhooks provides the background callback delivery worker for async webhook calls.
// The worker polls webhook_calls rows in status=queued (or stale running), invokes the
// agent if needed, signs and POSTs the result to callback_url, and persists the outcome.
//
// Architecture: single loop per worker instance → claim one row per poll cycle → launch
// goroutine for delivery (capped by CallbackLimiter). Poll interval 2s.
package webhooks

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"

	"github.com/nextlevelbuilder/goclaw/internal/agent"
	"github.com/nextlevelbuilder/goclaw/internal/crypto"
	"github.com/nextlevelbuilder/goclaw/internal/security"
	"github.com/nextlevelbuilder/goclaw/internal/store"
)

const (
	// workerPollInterval is how often the main loop scans for queued rows.
	workerPollInterval = 2 * time.Second

	// staleRunningWindow is how long a running row must be inactive before being reclaimed.
	staleRunningWindow = 90 * time.Second

	// reclaimTickInterval is how often the reclaim sweep runs after startup.
	reclaimTickInterval = 60 * time.Second

	// pruneTickInterval is how often old terminal rows are deleted.
	pruneTickInterval = 1 * time.Hour

	// pruneRetentionDays is how old terminal rows must be before deletion.
	pruneRetentionDays = 30 * 24 * time.Hour

	// callbackTimeout is the per-request outbound HTTP timeout.
	callbackTimeout = 15 * time.Second

	// callbackMaxResponseBytes is the max response body read from callback endpoints.
	callbackMaxResponseBytes = 64 * 1024 // 64 KB

	// callbackResponseStorageLimit is the max bytes stored in webhook_calls.response.
	callbackResponseStorageLimit = 32 * 1024 // 32 KB

	// asyncAgentTimeout is the max time to invoke the LLM agent for async_llm mode.
	asyncAgentTimeout = 30 * time.Second

	// retryAfterCap caps the Retry-After header value to 6 hours.
	retryAfterCap = 6 * time.Hour
)

// asyncPayload is the stored request payload written by phase 06 handleAsync.
// Must match webhookLLMReq in internal/http/webhooks_llm.go.
type asyncPayload struct {
	Input       json.RawMessage `json:"input"`
	SessionKey  string          `json:"session_key,omitempty"`
	UserID      string          `json:"user_id,omitempty"`
	Model       string          `json:"model,omitempty"`
	Mode        string          `json:"mode,omitempty"`
	CallbackURL string          `json:"callback_url,omitempty"`
	Metadata    json.RawMessage `json:"metadata,omitempty"`
}

// callbackPayload is the JSON body POSTed to the receiver's callback_url.
type callbackPayload struct {
	CallID     string          `json:"call_id"`
	DeliveryID string          `json:"delivery_id"`
	AgentID    string          `json:"agent_id,omitempty"`
	Status     string          `json:"status"` // "done" | "failed"
	Output     string          `json:"output,omitempty"`
	Usage      *callbackUsage  `json:"usage,omitempty"`
	Metadata   json.RawMessage `json:"metadata,omitempty"`
	Error      string          `json:"error,omitempty"`
}

// callbackUsage mirrors providers.Usage for the callback payload.
type callbackUsage struct {
	PromptTokens     int `json:"prompt_tokens"`
	CompletionTokens int `json:"completion_tokens"`
	TotalTokens      int `json:"total_tokens"`
}

// WorkerConfig holds tunable parameters for WebhookWorker.
type WorkerConfig struct {
	// WorkerConcurrency is the number of parallel claim-and-deliver goroutines.
	// Set to 1 for SQLite (Lite edition) to avoid lock contention.
	WorkerConcurrency int

	// PerTenantConcurrency is the per-tenant cap passed to CallbackLimiter.
	// 0 = default (4).
	PerTenantConcurrency int
}

// WebhookWorker is the background callback delivery service. It is started once per
// process and runs until ctx is cancelled (SIGTERM). It owns:
//   - Poll loop (claim queued rows, dispatch goroutines)
//   - Stale-running reclaim (startup + 60s ticker)
//   - Retention prune (hourly ticker)
//   - CallbackLimiter (per-tenant concurrency cap)
type WebhookWorker struct {
	calls    store.WebhookCallStore
	webhooks store.WebhookStore
	tenants  store.TenantStore
	router   *agent.Router
	limiter  *CallbackLimiter
	cfg      WorkerConfig
	// encKey is the AES-256-GCM key used to decrypt webhook.encrypted_secret at HMAC sign time.
	// Sourced from GOCLAW_ENCRYPTION_KEY env var. Empty string disables outbound HMAC signing.
	encKey string

	// inFlight tracks active delivery goroutines for graceful drain.
	inFlight sync.WaitGroup
}

// NewWebhookWorker creates a worker. limiter may be nil (one will be created).
func NewWebhookWorker(
	calls store.WebhookCallStore,
	webhooks store.WebhookStore,
	tenants store.TenantStore,
	router *agent.Router,
	limiter *CallbackLimiter,
	cfg WorkerConfig,
) *WebhookWorker {
	if cfg.WorkerConcurrency <= 0 {
		cfg.WorkerConcurrency = 4
	}
	if limiter == nil {
		limiter = NewCallbackLimiter(cfg.PerTenantConcurrency)
	}
	return &WebhookWorker{
		calls:    calls,
		webhooks: webhooks,
		tenants:  tenants,
		router:   router,
		limiter:  limiter,
		cfg:      cfg,
	}
}

// SetEncKey configures the AES-256-GCM decryption key for outbound HMAC signing.
// Must be called before Run() if webhooks use HMAC auth.
func (w *WebhookWorker) SetEncKey(encKey string) {
	w.encKey = encKey
}

// Run starts the worker loop. It blocks until ctx is cancelled, then drains in-flight
// deliveries before returning. Caller should set a drain deadline on ctx.
func (w *WebhookWorker) Run(ctx context.Context) {
	slog.Info("webhook.worker.start",
		"concurrency", w.cfg.WorkerConcurrency,
		"per_tenant_cap", w.cfg.PerTenantConcurrency,
	)

	// Startup: reclaim stale running rows from a previous crash.
	w.reclaimStale(ctx)

	// Background tickers.
	reclaimTick := time.NewTicker(reclaimTickInterval)
	pruneTick := time.NewTicker(pruneTickInterval)
	defer reclaimTick.Stop()
	defer pruneTick.Stop()

	pollTick := time.NewTicker(workerPollInterval)
	defer pollTick.Stop()

	// Semaphore limiting simultaneous goroutines from the poll loop.
	// WorkerConcurrency = 1 on SQLite/Lite; > 1 on PG standard.
	slotCh := make(chan struct{}, w.cfg.WorkerConcurrency)

	for {
		select {
		case <-ctx.Done():
			slog.Info("webhook.worker.draining")
			w.inFlight.Wait()
			w.limiter.Stop()
			slog.Info("webhook.worker.stopped")
			return

		case <-reclaimTick.C:
			w.reclaimStale(ctx)

		case <-pruneTick.C:
			w.pruneOld(ctx)

		case <-pollTick.C:
			// Try to acquire a dispatch slot without blocking.
			select {
			case slotCh <- struct{}{}:
			default:
				// All slots busy — skip this tick; next tick will retry.
				continue
			}

			// slotRelease is passed into the goroutine — the goroutine MUST call it on exit.
			// K4: without this closure the slot is never returned, causing the worker to
			// wedge after WorkerConcurrency deliveries (1 on SQLite/Lite).
			slotRelease := func() { <-slotCh }

			// Scan each active tenant for a claimable row.
			claimed := w.pollOneTenant(ctx, slotRelease)
			if !claimed {
				// No work found — release the slot we just acquired.
				slotRelease()
			}
			// If claimed=true, the goroutine launched by pollOneTenant owns slotRelease.
		}
	}
}

// pollOneTenant iterates active tenants and claims+dispatches the first available row.
// slotRelease must be called by the launched goroutine (K4 fix: prevents slot drain).
// Returns true if a delivery goroutine was launched (slot consumed), false otherwise.
func (w *WebhookWorker) pollOneTenant(ctx context.Context, slotRelease func()) bool {
	tenantList, err := w.tenants.ListTenants(ctx)
	if err != nil {
		slog.Error("webhook.worker.list_tenants_failed", "error", err)
		return false
	}

	now := time.Now()
	for _, tenant := range tenantList {
		if tenant.Status != store.TenantStatusActive {
			continue
		}

		tctx := store.WithTenantID(ctx, tenant.ID)
		call, claimErr := w.calls.ClaimNext(tctx, tenant.ID, now)
		if errors.Is(claimErr, sql.ErrNoRows) || call == nil {
			continue // no work for this tenant
		}
		if claimErr != nil {
			slog.Error("webhook.worker.claim_failed",
				"tenant_id", tenant.ID,
				"error", claimErr,
			)
			continue
		}

		// Extract lease token set by ClaimNext (K5: CAS guard for UpdateStatusCAS).
		lease := ""
		if call.LeaseToken != nil {
			lease = *call.LeaseToken
		}

		// Try per-tenant concurrency cap (non-blocking).
		tenantIDStr := tenant.ID.String()
		if !w.limiter.TryAcquire(tenantIDStr) {
			// Tenant is at cap. Reset row to queued so the next poll can retry.
			w.resetToQueued(ctx, call, tenant.ID, "tenant_concurrency_cap")
			return false
		}

		// Dispatch delivery goroutine.
		// K4: slotRelease is called in defer so the semaphore slot is always returned.
		callCopy := *call
		w.inFlight.Add(1)
		go func() {
			defer slotRelease() // K4: release semaphore slot on goroutine exit
			defer w.inFlight.Done()
			defer w.limiter.Release(tenantIDStr)
			w.execute(ctx, &callCopy, tenant.ID, lease)
		}()
		return true
	}
	return false
}

// execute is the per-row delivery pipeline. It runs in a goroutine and is
// protected by a defer recover() to prevent worker crashes from one bad row.
// lease is the token returned by ClaimNext; used for optimistic-concurrency (K5).
func (w *WebhookWorker) execute(ctx context.Context, call *store.WebhookCallData, tenantID uuid.UUID, lease string) {
	// Use WithoutCancel so DB status writes survive worker ctx cancellation at
	// graceful shutdown. Prevents unnecessary re-delivery via reclaimStale when
	// the send completes but the terminal status update races with shutdown.
	// Initialized BEFORE the panic defer so the recovery path uses a ctx with
	// tenant ID (raw ctx lacks it, which would make requireTenantID fail).
	tctx := store.WithTenantID(context.WithoutCancel(ctx), tenantID)

	defer func() {
		if r := recover(); r != nil {
			slog.Error("security.webhook.worker_panic",
				"call_id", call.ID,
				"delivery_id", call.DeliveryID,
				"panic", r,
			)
			w.updateRetry(tctx, call, tenantID, lease, fmt.Sprintf("panic: %v", r))
		}
	}()

	// Decode stored request payload.
	var req asyncPayload
	if err := json.Unmarshal(call.RequestPayload, &req); err != nil {
		slog.Error("webhook.worker.payload_decode_failed",
			"call_id", call.ID,
			"error", err,
		)
		w.updateFailed(tctx, call, tenantID, lease, "payload decode error: "+err.Error())
		return
	}

	// Step 1: If no response yet, invoke agent to get output.
	var output string
	var usageVal *callbackUsage
	var agentErrMsg string

	if len(call.Response) == 0 && call.AgentID != nil {
		out, usage, invokeErr := w.invokeAgent(tctx, call, req)
		if invokeErr != nil {
			agentErrMsg = invokeErr.Error()
			slog.Warn("webhook.worker.agent_invoke_failed",
				"call_id", call.ID,
				"delivery_id", call.DeliveryID,
				"error", invokeErr,
			)
		} else {
			output = out
			usageVal = usage
		}
	} else if len(call.Response) > 0 {
		// Prior attempt stored a partial response; extract output for re-delivery.
		var prevResp callbackPayload
		if err := json.Unmarshal(call.Response, &prevResp); err == nil {
			output = prevResp.Output
			usageVal = prevResp.Usage
		}
	}

	// Resolve callback_url.
	if call.CallbackURL == nil || *call.CallbackURL == "" {
		slog.Error("webhook.worker.no_callback_url", "call_id", call.ID)
		w.updateFailed(tctx, call, tenantID, lease, "no callback_url")
		return
	}
	callbackURL := *call.CallbackURL

	// Step 2: SSRF re-validation at send time (prevents DNS rebinding).
	_, pinnedIP, ssrfErr := security.Validate(callbackURL)
	if ssrfErr != nil {
		slog.Warn("security.webhook.callback_ssrf_blocked",
			"call_id", call.ID,
			"host", hostOnly(callbackURL),
			"error", ssrfErr,
		)
		w.updateFailed(tctx, call, tenantID, lease, "ssrf: "+ssrfErr.Error())
		return
	}

	// Step 3: Build callback payload.
	statusStr := "done"
	if agentErrMsg != "" {
		statusStr = "failed"
	}
	agentIDStr := ""
	if call.AgentID != nil {
		agentIDStr = call.AgentID.String()
	}

	payload := callbackPayload{
		CallID:     call.ID.String(),
		DeliveryID: call.DeliveryID.String(),
		AgentID:    agentIDStr,
		Status:     statusStr,
		Output:     output,
		Usage:      usageVal,
		Metadata:   req.Metadata,
		Error:      agentErrMsg,
	}
	bodyBytes, err := json.Marshal(payload)
	if err != nil {
		slog.Error("webhook.worker.marshal_failed", "call_id", call.ID, "error", err)
		w.updateFailed(tctx, call, tenantID, lease, "marshal: "+err.Error())
		return
	}

	// Step 4: Load webhook row for HMAC signing.
	wh, whErr := w.webhooks.GetByID(tctx, call.WebhookID)
	if whErr != nil {
		slog.Error("webhook.worker.load_webhook_failed",
			"call_id", call.ID,
			"webhook_id", call.WebhookID,
			"error", whErr,
		)
		w.updateRetry(tctx, call, tenantID, lease, "webhook lookup: "+whErr.Error())
		return
	}

	// Step 5: Decrypt raw secret for HMAC signing (K6).
	// encrypted_secret holds AES-256-GCM ciphertext; decrypt to get the raw signing key.
	// Falls back to no HMAC header if encKey is empty (dev/test environments).
	now := time.Now()
	var sigHeader string
	if wh.EncryptedSecret != "" && w.encKey != "" {
		rawSecret, decErr := crypto.Decrypt(wh.EncryptedSecret, w.encKey)
		if decErr != nil {
			slog.Error("webhook.worker.decrypt_secret_failed",
				"call_id", call.ID,
				"webhook_id", call.WebhookID,
				"error", decErr,
			)
			w.updateFailed(tctx, call, tenantID, lease, "decrypt secret: "+decErr.Error())
			return
		}
		sigHeader = Sign([]byte(rawSecret), now.Unix(), bodyBytes)
	} else if wh.EncryptedSecret == "" {
		slog.Warn("webhook.worker.no_encrypted_secret",
			"call_id", call.ID,
			"webhook_id", call.WebhookID,
		)
	}

	// Step 6: Build and send outbound POST.
	sendCtx := security.WithPinnedIP(context.WithoutCancel(ctx), pinnedIP)
	httpReq, reqErr := http.NewRequestWithContext(sendCtx, http.MethodPost, callbackURL, bytes.NewReader(bodyBytes))
	if reqErr != nil {
		w.updateRetry(tctx, call, tenantID, lease, "build request: "+reqErr.Error())
		return
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("User-Agent", "goclaw-webhook/1")
	httpReq.Header.Set("X-Webhook-Delivery-Id", call.DeliveryID.String())
	if sigHeader != "" {
		httpReq.Header.Set("X-Webhook-Signature", sigHeader)
	}

	client := security.NewSafeClient(callbackTimeout)
	resp, doErr := client.Do(httpReq)

	// Increment attempts AFTER send completes (success or failure) — crash-restart safety.
	newAttempts := call.Attempts + 1

	if doErr != nil {
		slog.Warn("webhook.worker.send_failed",
			"call_id", call.ID,
			"delivery_id", call.DeliveryID,
			"attempt", newAttempts,
			"error", doErr,
		)
		w.handleSendError(tctx, call, tenantID, newAttempts, lease, doErr.Error(), nil)
		return
	}
	defer resp.Body.Close()
	// Drain response body (up to 64 KB) to allow connection reuse.
	respBody, _ := io.ReadAll(io.LimitReader(resp.Body, callbackMaxResponseBytes))

	slog.Info("webhook.worker.delivered",
		"call_id", call.ID,
		"delivery_id", call.DeliveryID,
		"attempt", newAttempts,
		"status_code", resp.StatusCode,
	)

	// Step 7: Classify response and update status.
	w.classifyAndUpdate(tctx, call, tenantID, resp, respBody, bodyBytes, newAttempts, lease, now)
}

// classifyAndUpdate maps the HTTP response status to a terminal or retry state.
// lease is used as the CAS guard (K5) for UpdateStatusCAS to prevent double-delivery.
func (w *WebhookWorker) classifyAndUpdate(
	ctx context.Context,
	call *store.WebhookCallData,
	tenantID uuid.UUID,
	resp *http.Response,
	respBody []byte,
	sentBody []byte,
	newAttempts int,
	lease string,
	sentAt time.Time,
) {
	code := resp.StatusCode
	switch {
	case code >= 200 && code < 300:
		// Success.
		// Store the sent payload as the canonical response.
		storedResp := sentBody
		if len(storedResp) > callbackResponseStorageLimit {
			storedResp = storedResp[:callbackResponseStorageLimit]
		}
		completedAt := sentAt
		updates := map[string]any{
			"status":       "done",
			"attempts":     newAttempts,
			"response":     storedResp,
			"completed_at": completedAt,
			"last_error":   nil,
			"lease_token":  nil, // clear lease on terminal status
		}
		if err := w.calls.UpdateStatusCAS(ctx, call.ID, lease, updates); err != nil {
			if errors.Is(err, store.ErrLeaseExpired) {
				slog.Warn("webhook.worker.lease_expired_on_done", "call_id", call.ID)
				return // another process already updated this row — safe to skip
			}
			slog.Error("webhook.worker.update_done_failed",
				"call_id", call.ID,
				"error", err,
			)
		}

	case code == http.StatusTooManyRequests:
		// Respect Retry-After header if provided.
		delay := DelayFor(newAttempts)
		if ra := resp.Header.Get("Retry-After"); ra != "" {
			if secs, err := strconv.ParseInt(strings.TrimSpace(ra), 10, 64); err == nil && secs > 0 {
				raDelay := min(time.Duration(secs)*time.Second, retryAfterCap)
				delay = raDelay
			}
		}
		errMsg := fmt.Sprintf("http %d", code)
		nextAt := time.Now().Add(delay)
		updates := map[string]any{
			"status":          "queued",
			"attempts":        newAttempts,
			"next_attempt_at": nextAt,
			"last_error":      errMsg,
			"lease_token":     nil, // clear lease so next claimer can acquire
		}
		if err := w.calls.UpdateStatusCAS(ctx, call.ID, lease, updates); err != nil {
			if errors.Is(err, store.ErrLeaseExpired) {
				slog.Warn("webhook.worker.lease_expired_on_retry", "call_id", call.ID)
				return
			}
			slog.Error("webhook.worker.update_retry_failed",
				"call_id", call.ID,
				"error", err,
			)
		}

	case code >= 400 && code < 500:
		// Permanent client-side error (except 429 handled above).
		errMsg := fmt.Sprintf("http %d (permanent)", code)
		completedAt := sentAt
		updates := map[string]any{
			"status":       "failed",
			"attempts":     newAttempts,
			"last_error":   errMsg,
			"completed_at": completedAt,
			"lease_token":  nil,
		}
		if err := w.calls.UpdateStatusCAS(ctx, call.ID, lease, updates); err != nil {
			if errors.Is(err, store.ErrLeaseExpired) {
				slog.Warn("webhook.worker.lease_expired_on_fail", "call_id", call.ID)
				return
			}
			slog.Error("webhook.worker.update_failed_failed",
				"call_id", call.ID,
				"error", err,
			)
		}

	default:
		// 5xx or unexpected — retry with exponential backoff; move to dead at cap.
		errMsg := fmt.Sprintf("http %d", code)
		w.handleSendError(ctx, call, tenantID, newAttempts, lease, errMsg, nil)
	}
}

// handleSendError routes a network or 5xx error to retry or dead based on attempt count.
// lease is the CAS guard; ignored (falls through to UpdateStatus) only when lease is empty.
func (w *WebhookWorker) handleSendError(
	ctx context.Context,
	call *store.WebhookCallData,
	_ uuid.UUID,
	newAttempts int,
	lease string,
	errMsg string,
	_ error,
) {
	if newAttempts >= MaxAttempts {
		completedAt := time.Now()
		updates := map[string]any{
			"status":       "dead",
			"attempts":     newAttempts,
			"last_error":   errMsg,
			"completed_at": completedAt,
			"lease_token":  nil,
		}
		if err := w.calls.UpdateStatusCAS(ctx, call.ID, lease, updates); err != nil {
			if errors.Is(err, store.ErrLeaseExpired) {
				slog.Warn("webhook.worker.lease_expired_on_dead", "call_id", call.ID)
				return
			}
			slog.Error("webhook.worker.update_dead_failed",
				"call_id", call.ID,
				"error", err,
			)
		}
		return
	}

	delay := DelayFor(newAttempts)
	nextAt := time.Now().Add(delay)
	updates := map[string]any{
		"status":          "queued",
		"attempts":        newAttempts,
		"next_attempt_at": nextAt,
		"last_error":      errMsg,
		"lease_token":     nil,
	}
	if err := w.calls.UpdateStatusCAS(ctx, call.ID, lease, updates); err != nil {
		if errors.Is(err, store.ErrLeaseExpired) {
			slog.Warn("webhook.worker.lease_expired_on_retry", "call_id", call.ID)
			return
		}
		slog.Error("webhook.worker.update_retry_failed",
			"call_id", call.ID,
			"error", err,
		)
	}
}

// updateFailed marks the call as permanently failed (no retry).
// lease is the CAS guard for UpdateStatusCAS (K5).
func (w *WebhookWorker) updateFailed(ctx context.Context, call *store.WebhookCallData, _ uuid.UUID, lease, reason string) {
	newAttempts := call.Attempts + 1
	completedAt := time.Now()
	updates := map[string]any{
		"status":       "failed",
		"attempts":     newAttempts,
		"last_error":   reason,
		"completed_at": completedAt,
		"lease_token":  nil,
	}
	if err := w.calls.UpdateStatusCAS(ctx, call.ID, lease, updates); err != nil {
		if errors.Is(err, store.ErrLeaseExpired) {
			slog.Warn("webhook.worker.lease_expired_on_fail", "call_id", call.ID)
			return
		}
		slog.Error("webhook.worker.update_failed_error",
			"call_id", call.ID,
			"error", err,
		)
	}
}

// updateRetry resets the call to queued with backoff for transient failures.
// lease is the CAS guard for UpdateStatusCAS (K5).
func (w *WebhookWorker) updateRetry(ctx context.Context, call *store.WebhookCallData, _ uuid.UUID, lease, reason string) {
	newAttempts := call.Attempts + 1
	if newAttempts >= MaxAttempts {
		w.updateFailed(ctx, call, uuid.Nil, lease, reason)
		return
	}
	delay := DelayFor(newAttempts)
	nextAt := time.Now().Add(delay)
	updates := map[string]any{
		"status":          "queued",
		"attempts":        newAttempts,
		"next_attempt_at": nextAt,
		"last_error":      reason,
		"lease_token":     nil,
	}
	if err := w.calls.UpdateStatusCAS(ctx, call.ID, lease, updates); err != nil {
		if errors.Is(err, store.ErrLeaseExpired) {
			slog.Warn("webhook.worker.lease_expired_on_retry", "call_id", call.ID)
			return
		}
		slog.Error("webhook.worker.update_retry_error",
			"call_id", call.ID,
			"error", err,
		)
	}
}

// resetToQueued returns a row claimed by ClaimNext back to queued without incrementing
// attempts. Used when the per-tenant limiter rejects the claim before any delivery work.
// Uses UpdateStatusCAS with the lease from ClaimNext (K5) to prevent races.
func (w *WebhookWorker) resetToQueued(ctx context.Context, call *store.WebhookCallData, tenantID uuid.UUID, reason string) {
	lease := ""
	if call.LeaseToken != nil {
		lease = *call.LeaseToken
	}
	tctx := store.WithTenantID(ctx, tenantID)
	updates := map[string]any{
		"status":      "queued",
		"started_at":  nil,
		"lease_token": nil, // clear lease so next claimer can acquire
		// attempts left unchanged — this was not a real send attempt
	}
	if err := w.calls.UpdateStatusCAS(tctx, call.ID, lease, updates); err != nil {
		if errors.Is(err, store.ErrLeaseExpired) {
			slog.Warn("webhook.worker.lease_expired_on_reset", "call_id", call.ID)
			return
		}
		slog.Error("webhook.worker.reset_queued_failed",
			"call_id", call.ID,
			"reason", reason,
			"error", err,
		)
	}
}

// invokeAgent runs the agent for an async call and returns (output, usage, error).
func (w *WebhookWorker) invokeAgent(
	ctx context.Context,
	call *store.WebhookCallData,
	req asyncPayload,
) (string, *callbackUsage, error) {
	if call.AgentID == nil {
		return "", nil, fmt.Errorf("call has no agent_id")
	}

	agentIDStr := call.AgentID.String()
	ag, err := w.router.Get(ctx, agentIDStr)
	if err != nil {
		return "", nil, fmt.Errorf("agent lookup %s: %w", agentIDStr, err)
	}

	// Parse input.
	userMessage, extraSystem, err := parseAsyncInput(req.Input)
	if err != nil {
		return "", nil, fmt.Errorf("parse input: %w", err)
	}
	if userMessage == "" {
		return "", nil, fmt.Errorf("empty user message in stored payload")
	}

	runID := uuid.NewString()
	sessionKey := req.SessionKey
	if sessionKey == "" {
		sessionKey = fmt.Sprintf("webhook:%s:%s:%s",
			agentIDStr, call.WebhookID.String(), runID[:8])
	}

	rr := agent.RunRequest{
		SessionKey:        sessionKey,
		Message:           userMessage,
		Channel:           "webhook",
		ChatID:            call.WebhookID.String(),
		RunID:             runID,
		UserID:            req.UserID,
		Stream:            false,
		ModelOverride:     req.Model,
		ExtraSystemPrompt: extraSystem,
		TraceName:         "webhook.async",
		TraceTags:         []string{"webhook", "async"},
	}

	runCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), asyncAgentTimeout)
	defer cancel()

	result, runErr := ag.Run(runCtx, rr)
	if runErr != nil {
		return "", nil, runErr
	}

	var usage *callbackUsage
	if result.Usage != nil {
		usage = &callbackUsage{
			PromptTokens:     result.Usage.PromptTokens,
			CompletionTokens: result.Usage.CompletionTokens,
			TotalTokens:      result.Usage.TotalTokens,
		}
	}
	return result.Content, usage, nil
}

// reclaimStale resets stale running rows back to queued.
func (w *WebhookWorker) reclaimStale(ctx context.Context) {
	threshold := time.Now().Add(-staleRunningWindow)
	n, err := w.calls.ReclaimStale(ctx, threshold)
	if err != nil {
		slog.Error("webhook.worker.reclaim_failed", "error", err)
		return
	}
	if n > 0 {
		slog.Info("webhook.worker.reclaimed_stale", "count", n)
	}
}

// pruneOld deletes terminal rows older than 30 days.
func (w *WebhookWorker) pruneOld(ctx context.Context) {
	cutoff := time.Now().Add(-pruneRetentionDays)
	// Cross-tenant sweep: pass uuid.Nil to DeleteOlderThan.
	n, err := w.calls.DeleteOlderThan(ctx, uuid.Nil, cutoff)
	if err != nil {
		slog.Error("webhook.worker.prune_failed", "error", err)
		return
	}
	if n > 0 {
		slog.Info("webhook.worker.pruned_old", "deleted", n)
	}
}

// parseAsyncInput replicates buildInput from webhooks_llm.go for the stored payload.
// Accepts a plain string or [{role,content}] array.
func parseAsyncInput(raw json.RawMessage) (userMessage, extraSystem string, err error) {
	if len(raw) == 0 || string(raw) == "null" {
		return "", "", fmt.Errorf("empty input")
	}
	var s string
	if json.Unmarshal(raw, &s) == nil {
		return s, "", nil
	}
	type msg struct {
		Role    string `json:"role"`
		Content string `json:"content"`
	}
	var msgs []msg
	if err := json.Unmarshal(raw, &msgs); err != nil {
		return "", "", fmt.Errorf("input parse: %w", err)
	}
	var userParts, sysParts []string
	for _, m := range msgs {
		switch strings.ToLower(m.Role) {
		case "system":
			if m.Content != "" {
				sysParts = append(sysParts, m.Content)
			}
		default:
			if m.Content != "" {
				userParts = append(userParts, m.Content)
			}
		}
	}
	return strings.Join(userParts, "\n"), strings.Join(sysParts, "\n"), nil
}

// hostOnly extracts the hostname from a URL for safe (no-path) logging.
func hostOnly(rawURL string) string {
	// Quick extraction without importing net/url for performance.
	// Handles http(s)://host/path format.
	for _, pfx := range []string{"https://", "http://"} {
		if strings.HasPrefix(rawURL, pfx) {
			rest := rawURL[len(pfx):]
			if before, _, ok := strings.Cut(rest, "/"); ok {
				return before
			}
			return rest
		}
	}
	return "[unknown]"
}
