package providers

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"
)

// A response is only signalless when EVERY signal is absent. Each subtest below
// is a shape that legitimately has empty Content and must keep working.
func TestResponseCarriesNoSignalRequiresEverySignalAbsent(t *testing.T) {
	cases := []struct {
		name string
		resp *ChatResponse
		want bool
	}{
		{
			name: "nothing at all",
			resp: &ChatResponse{FinishReason: "stop"},
			want: true,
		},
		{
			name: "text answer",
			resp: &ChatResponse{Content: "done", FinishReason: "stop"},
			want: false,
		},
		{
			name: "thinking only",
			resp: &ChatResponse{Thinking: "reasoning", FinishReason: "stop"},
			want: false,
		},
		{
			name: "tool call with no text",
			resp: &ChatResponse{
				ToolCalls:    []ToolCall{{ID: "1", Name: "write_file"}},
				FinishReason: "tool_calls",
			},
			want: false,
		},
		{
			name: "image only",
			resp: &ChatResponse{
				Images:       []ImageContent{{MimeType: "image/png", Data: "x"}},
				FinishReason: "stop",
			},
			want: false,
		},
		{
			name: "usage reported but nothing emitted",
			resp: &ChatResponse{Usage: &Usage{PromptTokens: 10}, FinishReason: "stop"},
			want: false,
		},
		{
			name: "NO_REPLY is a deliberate silence, not an empty response",
			resp: &ChatResponse{
				Content:      "NO_REPLY",
				Usage:        &Usage{PromptTokens: 10, CompletionTokens: 4},
				FinishReason: "stop",
			},
			want: false,
		},
		{
			name: "anthropic raw content passback",
			resp: &ChatResponse{
				RawAssistantContent: json.RawMessage(`[{"type":"thinking"}]`),
				FinishReason:        "stop",
			},
			want: false,
		},
		{
			name: "anthropic thinking signature",
			resp: &ChatResponse{ThinkingSignature: "sig", FinishReason: "stop"},
			want: false,
		},
		{
			name: "codex phase",
			resp: &ChatResponse{Phase: "final_answer", FinishReason: "stop"},
			want: false,
		},
		{
			name: "nil response is the caller's problem, not ours",
			resp: nil,
			want: false,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := ResponseCarriesNoSignal(tc.resp); got != tc.want {
				t.Errorf("ResponseCarriesNoSignal() = %v, want %v", got, tc.want)
			}
		})
	}
}

// A zero-token usage struct still means the provider accounted for the call, so
// it is a signal. Guards against a future "Usage != nil but all zero" shortcut.
func TestResponseCarriesNoSignalTreatsZeroUsageAsSignal(t *testing.T) {
	resp := &ChatResponse{Usage: &Usage{}, FinishReason: "stop"}
	if ResponseCarriesNoSignal(resp) {
		t.Error("a present Usage struct means the call was accounted for; want signal")
	}
}

// The guard is worthless unless the error it produces reaches the retry path.
func TestEmptyResponseErrorIsRetryable(t *testing.T) {
	err := error(&EmptyResponseError{Provider: "ollama", Model: "qwen3"})
	if !IsRetryableError(err) {
		t.Error("IsRetryableError() = false; an empty response must be retried")
	}
	wrapped := errors.Join(errors.New("outer"), err)
	if !IsRetryableError(wrapped) {
		t.Error("IsRetryableError() must see through wrapping")
	}
}

// FailoverUnknown would make runOrdered stop walking candidates and would fail
// the transient check used for workflow step requeue, so the classification must
// be explicit.
func TestEmptyResponseErrorClassifiesAsServerError(t *testing.T) {
	got := ClassifyHTTPError(NewDefaultClassifier(), &EmptyResponseError{Provider: "9router", Model: "Huy-Minh"})
	if got.Kind != "reason" {
		t.Fatalf("Kind = %q, want %q", got.Kind, "reason")
	}
	if got.Reason == FailoverUnknown {
		t.Fatal("FailoverUnknown suppresses both failover and workflow requeue")
	}
	if got.Reason != FailoverServerError {
		t.Errorf("Reason = %q, want %q", got.Reason, FailoverServerError)
	}
}

// When fallback is configured, the caller receives a FailoverSummaryError rather
// than the underlying cause. Without Unwrap the transient check and
// IsRetryableError both see an opaque string, classify it FailoverUnknown, and
// treat a retryable failure as permanent.
func TestFailoverSummaryErrorExposesUnderlyingCause(t *testing.T) {
	empty := &EmptyResponseError{Provider: "ollama", Model: "qwen3"}
	summary := error(&FailoverSummaryError{Attempts: []FailoverAttempt{
		{Candidate: ModelCandidate{Provider: "a", Model: "m1"}, Err: &HTTPError{Status: 429, Body: "rate limited"}},
		{Candidate: ModelCandidate{Provider: "b", Model: "m2"}, Err: empty},
	}})

	var got *EmptyResponseError
	if !errors.As(summary, &got) {
		t.Fatal("errors.As() could not see the underlying cause through the summary")
	}
	if got.Model != "qwen3" {
		t.Errorf("unwrapped model = %q, want %q", got.Model, "qwen3")
	}
	if !IsRetryableError(summary) {
		t.Error("IsRetryableError() = false for a summary wrapping a retryable cause")
	}
	if cls := ClassifyHTTPError(NewDefaultClassifier(), summary); cls.Reason == FailoverUnknown {
		t.Error("classification = FailoverUnknown; a retryable cause must survive the summary")
	}
}

// An empty attempt list must not panic or invent a cause.
func TestFailoverSummaryErrorWithNoAttemptsUnwrapsToNil(t *testing.T) {
	summary := &FailoverSummaryError{}
	if err := summary.Unwrap(); err != nil {
		t.Errorf("Unwrap() = %v, want nil", err)
	}
	if IsRetryableError(summary) {
		t.Error("IsRetryableError() = true for an empty summary; want false")
	}
}

func TestEmptyResponseErrorMessageNamesProviderAndModel(t *testing.T) {
	msg := (&EmptyResponseError{Provider: "ollama", Model: "qwen3"}).Error()
	for _, want := range []string{"no content", "ollama", "qwen3"} {
		if !strings.Contains(msg, want) {
			t.Errorf("Error() = %q, want it to contain %q", msg, want)
		}
	}
	// Provider/model are unknown on some paths; the message must still read.
	if bare := (&EmptyResponseError{}).Error(); strings.Contains(bare, "(") {
		t.Errorf("Error() with no provider = %q, want no empty parenthetical", bare)
	}
}
