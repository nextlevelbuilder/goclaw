package channels

import (
	"strings"

	"github.com/nextlevelbuilder/goclaw/internal/providers"
)

// FormatAgentError converts internal error to user-friendly message.
// Issue 958: Send user-friendly error on RunFailed instead of silent "...".
func FormatAgentError(errStr string) string {
	if errStr == "" {
		return ""
	}

	lower := strings.ToLower(errStr)

	// Context overflow (highest priority — specific actionable message)
	if providers.IsContextOverflowMessage(lower) {
		return "⚠️ The conversation has grown too long. Please start a new chat or ask me to summarize."
	}

	// Rate limit
	if strings.Contains(lower, "rate limit") || strings.Contains(lower, "too many requests") || strings.Contains(lower, "429") {
		return "⏳ Too many requests. Please wait a moment and try again."
	}

	// Auth errors
	if strings.Contains(lower, "unauthorized") || strings.Contains(lower, "invalid api key") || strings.Contains(lower, "401") || strings.Contains(lower, "403") {
		return "🔑 Authentication error. Please check your API configuration."
	}

	// Timeout
	if strings.Contains(lower, "timeout") || strings.Contains(lower, "deadline exceeded") {
		return "⏱️ Request timed out. Please try again."
	}

	// Overloaded
	if strings.Contains(lower, "overload") {
		return "🔄 Service is busy. Please try again in a moment."
	}

	// Generic fallback (don't expose internal error details)
	return "❌ Something went wrong. Please try again."
}
