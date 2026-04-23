package cmd

import (
	"fmt"
	"log/slog"
	"strings"

	"github.com/nextlevelbuilder/goclaw/internal/channels"
)

// Matching TS pi-embedded-helpers/errors.ts error classification.
// Never expose raw JSON/API payloads to the user.
func formatAgentError(err error) string {
	raw := err.Error()
	lower := strings.ToLower(raw)

	// 1. Timeout — must be checked BEFORE context overflow because
	// "context deadline exceeded" contains both "context" and "exceeded",
	// which would false-positive match the context overflow heuristic.
	if containsAny(lower, "timeout", "timed out", "deadline exceeded") {
		return "⚠️ Request timed out. Please try again."
	}

	// 2. Context overflow
	if isContextOverflowError(lower) {
		return "⚠️ Context overflow — message too large for this model. Try /new to start a fresh session."
	}

	// 3. Role ordering / message format errors (tool_use_id mismatch, roles must alternate, etc.)
	if isMessageFormatError(lower) {
		return "⚠️ Session history conflict — please try again. If this persists, use /new to start a fresh session."
	}

	// 4. Rate limit
	if containsAny(lower, "rate limit", "rate_limit", "too many requests", "429", "quota exceeded", "resource_exhausted", "usage limit") {
		return "⚠️ API rate limit reached. Please try again later."
	}

	// 5. Overloaded
	if strings.Contains(lower, "overloaded") {
		return "⚠️ The AI service is temporarily overloaded. Please try again in a moment."
	}

	// 6. Billing
	if containsAny(lower, "billing", "insufficient credits", "credit balance", "payment required", "402") {
		return "⚠️ API billing error — your API key may have run out of credits. Check your provider's billing dashboard."
	}

	// 7. Auth errors
	if containsAny(lower, "invalid api key", "invalid_api_key", "unauthorized", "forbidden", "authentication", "401", "403", "access denied") {
		return "⚠️ Authentication error. Please check your API key configuration."
	}

	// 8. Model config
	if strings.Contains(lower, "not a valid model") {
		return "⚠️ Model configuration error. Please check your config and restart."
	}

	// 9. Generic — log the full error but show only a safe message to user
	slog.Warn("unclassified agent error", "error", raw)
	return "⚠️ Sorry, something went wrong processing your message. Please try again."
}

// isContextOverflowError checks for context window/size overflow patterns.
func isContextOverflowError(lower string) bool {
	return containsAny(lower,
		"request_too_large",
		"context length exceeded",
		"maximum context length",
		"prompt is too long",
		"exceeds model context window",
		"request exceeds the maximum size",
	) || (strings.Contains(lower, "context") &&
		containsAny(lower, "overflow", "too large", "too long", "limit", "exceeded"))
}

// isExternalChannel reports whether a channel type serves end users on a
// public-facing platform (Facebook, Telegram, etc.). Internal error details
// must not be forwarded to these channels — the caller publishes an empty
// outbound instead so placeholders get cleaned up without leaking technical
// error text to end users. Internal types ("ws", "") return false.
func isExternalChannel(channelType string) bool {
	switch channelType {
	case channels.TypeFacebook,
		channels.TypeTelegram,
		channels.TypeDiscord,
		channels.TypeFeishu,
		channels.TypeWhatsApp,
		channels.TypeZaloOA,
		channels.TypeZaloPersonal,
		channels.TypePancake,
		channels.TypeSlack:
		return true
	}
	return false
}

// isMessageFormatError checks for tool_use/tool_result mismatch, role ordering,
// and other message format errors that indicate corrupted session history.
func isMessageFormatError(lower string) bool {
	return containsAny(lower,
		"tool_use_id",
		"tool_use.id",
		"unexpected tool",
		"roles must alternate",
		"incorrect role information",
		"invalid request format",
		"tool_result block",
		"tool_use block",
	)
}

// containsAny returns true if s contains any of the given substrings.
func containsAny(s string, substrs ...string) bool {
	for _, sub := range substrs {
		if strings.Contains(s, sub) {
			return true
		}
	}
	return false
}

// formatQuotaExceeded formats a user-friendly quota exceeded message.
func formatQuotaExceeded(result channels.QuotaResult) string {
	labels := map[string]string{"hour": "Hourly", "day": "Daily", "week": "Weekly"}
	return fmt.Sprintf("⚠️ %s request limit reached (%d/%d). Please try again later.",
		labels[result.Window], result.Used, result.Limit)
}
