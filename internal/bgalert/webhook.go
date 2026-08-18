package bgalert

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"sync"
	"time"
)

// WebhookPayload is the JSON body sent to an alert webhook. JSON tags are
// snake_case.
type WebhookPayload struct {
	Severity  string            `json:"severity"`
	Title     string            `json:"title"`
	Message   string            `json:"message"`
	Worker    string            `json:"worker"`
	Reason    string            `json:"reason"`
	Timestamp string            `json:"timestamp"`
	Meta      map[string]string `json:"meta,omitempty"`
}

// webhookClient is shared across sends. The timeout bounds every request so a
// slow receiver can never hold a caller for longer than 5s.
var webhookClient = &http.Client{Timeout: 5 * time.Second}

// webhookMu guards webhookLastSend, the package-level min-interval cooldown.
var (
	webhookMu       sync.Mutex
	webhookLastSend time.Time
)

// webhookSeverity maps alertable provider reasons to a severity label.
// Auth/billing/model-not-found are treated as critical; the rest are warning.
func webhookSeverity(reason string) string {
	switch reason {
	case "auth", "auth_permanent", "billing", "model_not_found":
		return "critical"
	default:
		return "warning"
	}
}

// SendWebhook posts a provider-error alert to deps.WebhookURL (best-effort).
// It never blocks the caller beyond the 5s HTTP timeout: marshal, request,
// and response errors are logged via slog.Warn and swallowed. When
// MinIntervalSeconds > 0, sends are throttled to at most one per interval
// (the timestamp refreshes only on a completed HTTP round-trip, so a failing
// endpoint is not hammered).
func SendWebhook(ctx context.Context, deps AlertDeps, workerName, reason string, err error) {
	if deps.WebhookURL == "" || err == nil {
		return
	}
	if !webhookCooldownOK(deps) {
		return
	}

	payload := WebhookPayload{
		Severity:  webhookSeverity(reason),
		Title:     "GoClaw background provider error",
		Message:   sanitizeErrorMessage(err.Error()),
		Worker:    workerName,
		Reason:    reason,
		Timestamp: time.Now().UTC().Format(time.RFC3339),
	}

	body, marshalErr := json.Marshal(payload)
	if marshalErr != nil {
		slog.Warn("bgalert.webhook_marshal_failed", "worker", workerName, "reason", reason, "err", marshalErr)
		return
	}

	req, reqErr := http.NewRequestWithContext(ctx, http.MethodPost, deps.WebhookURL, bytes.NewReader(body))
	if reqErr != nil {
		slog.Warn("bgalert.webhook_request_failed", "worker", workerName, "reason", reason, "err", reqErr)
		return
	}
	req.Header.Set("Content-Type", "application/json")

	resp, doErr := webhookClient.Do(req)
	if doErr != nil {
		// Transport failure — do not refresh the cooldown so the next alert
		// can retry immediately.
		slog.Warn("bgalert.webhook_failed", "worker", workerName, "reason", reason, "err", doErr)
		return
	}
	defer resp.Body.Close()

	// The round-trip completed: drain the body (bounded by the client timeout)
	// so the connection can be reused, then refresh the cooldown timestamp.
	_, _ = io.Copy(io.Discard, resp.Body)
	webhookCooldownNoteSend()

	if resp.StatusCode >= 400 {
		slog.Warn("bgalert.webhook_non_2xx", "worker", workerName, "reason", reason, "status", resp.StatusCode)
		return
	}
	slog.Info("bgalert.webhook_sent", "worker", workerName, "reason", reason, "status", resp.StatusCode)
}

// webhookCooldownOK reports whether a send may proceed given
// MinIntervalSeconds. It reserves the cooldown slot without a full lock
// hold: only the last-send timestamp is checked, so concurrent alerters
// share one throttle.
func webhookCooldownOK(deps AlertDeps) bool {
	if deps.MinIntervalSeconds <= 0 {
		return true
	}
	interval := time.Duration(deps.MinIntervalSeconds) * time.Second
	webhookMu.Lock()
	defer webhookMu.Unlock()
	if time.Since(webhookLastSend) < interval {
		return false
	}
	return true
}

// webhookCooldownNoteSend refreshes the last-send timestamp after a completed
// HTTP round-trip so the next send must wait a full min-interval.
func webhookCooldownNoteSend() {
	webhookMu.Lock()
	webhookLastSend = time.Now()
	webhookMu.Unlock()
}
