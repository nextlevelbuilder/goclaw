package channels

import (
	"strings"
	"testing"
)

func TestFormatAgentError_ContextOverflow(t *testing.T) {
	t.Parallel()
	testCases := []string{
		"context length exceeded",
		"Prompt exceeds max length",
		"request_too_large: payload too big",
		"Input is too long for this model",
		"token limit exceeded",
		"请求输入过长",
	}

	for _, tc := range testCases {
		result := FormatAgentError(tc)
		if !strings.Contains(result, "conversation has grown too long") {
			t.Errorf("expected context overflow message for %q, got %q", tc, result)
		}
	}
}

func TestFormatAgentError_RateLimit(t *testing.T) {
	t.Parallel()
	result := FormatAgentError("rate limit exceeded")
	if !strings.Contains(result, "Too many requests") {
		t.Errorf("unexpected rate limit message: %s", result)
	}
}

func TestFormatAgentError_Auth(t *testing.T) {
	t.Parallel()
	result := FormatAgentError("unauthorized access")
	if !strings.Contains(result, "Authentication error") {
		t.Errorf("unexpected auth message: %s", result)
	}
}

func TestFormatAgentError_Timeout(t *testing.T) {
	t.Parallel()
	result := FormatAgentError("request timeout")
	if !strings.Contains(result, "timed out") {
		t.Errorf("unexpected timeout message: %s", result)
	}
}

func TestFormatAgentError_Generic(t *testing.T) {
	t.Parallel()
	result := FormatAgentError("some unknown error")
	if !strings.Contains(result, "Something went wrong") {
		t.Errorf("unexpected generic message: %s", result)
	}
}

func TestFormatAgentError_Empty(t *testing.T) {
	t.Parallel()
	result := FormatAgentError("")
	if result != "" {
		t.Errorf("expected empty string for empty error, got %q", result)
	}
}
