package agent

import (
	"testing"

	"github.com/nextlevelbuilder/goclaw/internal/providers"
)

// The whole point of turning a signalless response into an error is that the
// existing retry machinery picks it up. This is the mechanism that decides
// whether a workflow step is requeued or thrown away, so it is asserted
// end-to-end rather than trusted.
func TestSignallessResponseIsATransientRunFailure(t *testing.T) {
	err := error(&providers.EmptyResponseError{Provider: "ollama", Model: "qwen3"})
	if !IsTransientRunFailure(err) {
		t.Fatal("IsTransientRunFailure() = false; a signalless response must be retried, not settled")
	}
}

// Same guarantee when model fallback is configured and the caller only ever sees
// the summary error.
func TestSignallessResponseIsTransientThroughFailoverSummary(t *testing.T) {
	summary := error(&providers.FailoverSummaryError{Attempts: []providers.FailoverAttempt{
		{Err: &providers.EmptyResponseError{Provider: "ollama", Model: "qwen3"}},
	}})
	if !IsTransientRunFailure(summary) {
		t.Fatal("IsTransientRunFailure() = false through FailoverSummaryError")
	}
}

// A deterministic failure must stay non-retryable — the guard must not turn every
// error into an infinite retry.
func TestContextOverflowStaysNonTransient(t *testing.T) {
	err := &providers.HTTPError{Status: 400, Body: "prompt is too long: 200000 tokens"}
	if IsTransientRunFailure(err) {
		t.Fatal("IsTransientRunFailure() = true for context overflow; retrying cannot help")
	}
}
