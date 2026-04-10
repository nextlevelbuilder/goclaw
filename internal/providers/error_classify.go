package providers

import (
	"errors"
	"net"
	"strings"
)

// FailoverReason categorizes provider errors for failover decisions.
type FailoverReason string

const (
	FailoverAuth          FailoverReason = "auth"
	FailoverAuthPermanent FailoverReason = "auth_permanent"
	FailoverFormat        FailoverReason = "format"
	FailoverRateLimit     FailoverReason = "rate_limit"
	FailoverOverloaded    FailoverReason = "overloaded"
	FailoverBilling       FailoverReason = "billing"
	FailoverTimeout       FailoverReason = "timeout"
	FailoverModelNotFound FailoverReason = "model_not_found"
	FailoverUnknown       FailoverReason = "unknown"
)

// FailoverClassification is the result of classifying a provider error.
type FailoverClassification struct {
	Kind   string         // "reason" or "context_overflow"
	Reason FailoverReason // only when Kind == "reason"
}

// Convenience constructors
func classifyReason(r FailoverReason) FailoverClassification {
	return FailoverClassification{Kind: "reason", Reason: r}
}

func classifyContextOverflow() FailoverClassification {
	return FailoverClassification{Kind: "context_overflow"}
}

// ErrorClassifier classifies provider errors for failover routing.
type ErrorClassifier interface {
	Classify(err error, statusCode int, body string) FailoverClassification
}

// DefaultClassifier handles common HTTP status + body pattern matching.
type DefaultClassifier struct {
	providerPatterns map[string][]ErrorPattern
}

// ErrorPattern maps a body substring pattern to a FailoverReason.
type ErrorPattern struct {
	Contains string
	Reason   FailoverReason
}

// NewDefaultClassifier returns a classifier with built-in patterns
// for OpenAI and Anthropic providers pre-registered.
func NewDefaultClassifier() *DefaultClassifier {
	c := &DefaultClassifier{
		providerPatterns: make(map[string][]ErrorPattern),
	}
	RegisterOpenAIPatterns(c)
	RegisterAnthropicPatterns(c)
	return c
}

// RegisterPatterns adds provider-specific error body patterns.
// Must be called during init only (not thread-safe for concurrent writes).
func (c *DefaultClassifier) RegisterPatterns(provider string, patterns []ErrorPattern) {
	c.providerPatterns[provider] = append(c.providerPatterns[provider], patterns...)
}

// Classify determines the failover reason for an error.
func (c *DefaultClassifier) Classify(err error, statusCode int, body string) FailoverClassification {
	lower := strings.ToLower(body)

	// Context overflow — not a failover reason, triggers compaction
	if isContextOverflow(lower) {
		return classifyContextOverflow()
	}

	// HTTP status-based classification
	switch {
	case statusCode == 429:
		return classifyReason(FailoverRateLimit)
	case statusCode == 402:
		return classifyReason(FailoverBilling)
	case statusCode == 401 || statusCode == 403:
		if containsAny(lower, "revoked", "deleted", "deactivated", "disabled", "expired") {
			return classifyReason(FailoverAuthPermanent)
		}
		return classifyReason(FailoverAuth)
	case statusCode == 404:
		if containsAny(lower, "model", "not found", "does not exist") {
			return classifyReason(FailoverModelNotFound)
		}
	case statusCode == 529:
		// Anthropic-specific overloaded status
		return classifyReason(FailoverOverloaded)
	case statusCode >= 500:
		if containsAny(lower, "overload", "capacity", "too many") {
			return classifyReason(FailoverOverloaded)
		}
	}

	// Body pattern matching for specific error types
	if containsAny(lower, "credit balance", "insufficient_quota", "billing") && statusCode != 0 {
		return classifyReason(FailoverBilling)
	}
	if containsAny(lower, "tool_call", "function_call", "invalid_request") && statusCode == 400 {
		return classifyReason(FailoverFormat)
	}

	// Provider-specific patterns
	for _, patterns := range c.providerPatterns {
		for _, p := range patterns {
			if strings.Contains(lower, strings.ToLower(p.Contains)) {
				return classifyReason(p.Reason)
			}
		}
	}

	// Network errors → timeout
	if isNetworkError(err) {
		return classifyReason(FailoverTimeout)
	}

	return classifyReason(FailoverUnknown)
}

// ClassifyHTTPError is a convenience that extracts status + body from an HTTPError.
func ClassifyHTTPError(classifier ErrorClassifier, err error) FailoverClassification {
	if err == nil {
		return classifyReason(FailoverUnknown)
	}
	var httpErr *HTTPError
	if errors.As(err, &httpErr) {
		return classifier.Classify(err, httpErr.Status, httpErr.Body)
	}
	// Non-HTTP error — check for network errors
	if isNetworkError(err) {
		return classifyReason(FailoverTimeout)
	}
	return classifyReason(FailoverUnknown)
}

// isContextOverflow detects context window exceeded errors across providers.
func isContextOverflow(lower string) bool {
	return containsAny(lower,
		"context length exceeded",
		"context window",
		"maximum context length",
		"token limit",
		"too many tokens",
		"prompt is too long",
		// Chinese patterns (Qwen/DashScope)
		"超出最大长度限制",
		"上下文长度",
	)
}

// isNetworkError checks if an error is a network-level failure.
func isNetworkError(err error) bool {
	if err == nil {
		return false
	}
	var netErr net.Error
	if errors.As(err, &netErr) {
		return true
	}
	s := err.Error()
	return containsAny(s, "connection reset", "broken pipe", "EOF", "timeout", "ECONNREFUSED")
}

// containsAny returns true if s contains any of the substrings.
func containsAny(s string, subs ...string) bool {
	for _, sub := range subs {
		if strings.Contains(s, sub) {
			return true
		}
	}
	return false
}

// RegisterOpenAIPatterns adds OpenAI-ecosystem specific error patterns.
func RegisterOpenAIPatterns(c *DefaultClassifier) {
	c.RegisterPatterns("openai", []ErrorPattern{
		{Contains: "model_is_deactivated", Reason: FailoverModelNotFound},
		{Contains: "model not found", Reason: FailoverModelNotFound},
		{Contains: "does not exist", Reason: FailoverModelNotFound},
	})
}

// RegisterAnthropicPatterns adds Anthropic-specific error patterns.
func RegisterAnthropicPatterns(c *DefaultClassifier) {
	c.RegisterPatterns("anthropic", []ErrorPattern{
		{Contains: "overloaded", Reason: FailoverOverloaded},
		{Contains: "credit balance", Reason: FailoverBilling},
	})
}
