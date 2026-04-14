// Package bgalert reports non-retryable LLM errors from background workers
// to the web UI via system_configs + WS events.
package bgalert

import (
	"context"
	"encoding/json"
	"log/slog"
	"regexp"
	"time"

	"github.com/nextlevelbuilder/goclaw/internal/bus"
	"github.com/nextlevelbuilder/goclaw/internal/providers"
	"github.com/nextlevelbuilder/goclaw/internal/store"
	"github.com/nextlevelbuilder/goclaw/pkg/protocol"
)

// AlertKeyProviderError is the system_configs key for background provider errors.
const AlertKeyProviderError = "alert.background.provider_error"

// AlertDeps bundles dependencies for error reporting.
type AlertDeps struct {
	SystemConfigs store.SystemConfigStore
	MsgBus        bus.EventPublisher
}

// ProviderErrorPayload is stored in system_configs and sent via WS event.
type ProviderErrorPayload struct {
	Reason    string `json:"reason"`
	Worker    string `json:"worker"`
	Message   string `json:"message"`
	Timestamp string `json:"timestamp"`
}

// alertableReasons are non-retryable errors that should surface to the user.
var alertableReasons = map[providers.FailoverReason]bool{
	providers.FailoverAuth:          true,
	providers.FailoverAuthPermanent: true,
	providers.FailoverBilling:       true,
	providers.FailoverModelNotFound: true,
}

// IsAlertableError returns true if the error is non-retryable and should
// be surfaced to the user (auth, billing, model_not_found).
func IsAlertableError(err error) (providers.FailoverReason, bool) {
	if err == nil {
		return "", false
	}
	cls := providers.ClassifyHTTPError(providers.NewDefaultClassifier(), err)
	if cls.Kind != "reason" {
		return "", false
	}
	return cls.Reason, alertableReasons[cls.Reason]
}

// ReportProviderError stores an alert in system_configs and broadcasts a
// WS event when the error is non-retryable (auth/billing/model_not_found).
// Safe to call with nil deps or for retryable errors — returns silently.
func ReportProviderError(ctx context.Context, deps AlertDeps, workerName string, err error) {
	if deps.SystemConfigs == nil || err == nil {
		return
	}
	reason, alertable := IsAlertableError(err)
	if !alertable {
		return
	}

	payload := ProviderErrorPayload{
		Reason:    string(reason),
		Worker:    workerName,
		Message:   sanitizeErrorMessage(err.Error()),
		Timestamp: time.Now().UTC().Format(time.RFC3339),
	}
	data, jsonErr := json.Marshal(payload)
	if jsonErr != nil {
		slog.Warn("bgalert: marshal failed", "err", jsonErr)
		return
	}

	if setErr := deps.SystemConfigs.Set(ctx, AlertKeyProviderError, string(data)); setErr != nil {
		slog.Warn("bgalert: store alert failed", "err", setErr)
		return
	}

	slog.Warn("bgalert: provider error stored",
		"worker", workerName, "reason", reason, "err", err)

	if deps.MsgBus != nil {
		tenantID := store.TenantIDFromContext(ctx)
		deps.MsgBus.Broadcast(bus.Event{
			Name:     protocol.EventBackgroundError,
			TenantID: tenantID,
			Payload:  payload,
		})
	}
}

// ClearProviderError removes the alert key from system_configs.
// Ignores "not found" errors. Safe to call when no alert exists.
func ClearProviderError(ctx context.Context, configs store.SystemConfigStore) {
	if configs == nil {
		return
	}
	_ = configs.Delete(ctx, AlertKeyProviderError)
}

// apiKeyPattern matches common API key formats in error messages.
var apiKeyPattern = regexp.MustCompile(`(?i)(sk-|Bearer\s+|api[_-]?key[=:]\s*)[a-zA-Z0-9_-]{4,}`)

// sanitizeErrorMessage strips potentially sensitive content from error messages.
// Keeps the message useful for diagnostics while removing API keys and long bodies.
func sanitizeErrorMessage(msg string) string {
	msg = apiKeyPattern.ReplaceAllString(msg, "${1}****")
	// Rune-safe truncation to avoid corrupting multi-byte UTF-8 characters.
	const maxLen = 200
	if runes := []rune(msg); len(runes) > maxLen {
		msg = string(runes[:maxLen]) + "..."
	}
	return msg
}
