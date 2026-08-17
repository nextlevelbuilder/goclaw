package providers

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"
)

// ---------------------------------------------------------------------------
// RetryDo through REAL HTTP: a fake LLM server scripts 429 storms, 5xx series,
// and client-side timeouts, and RetryDo drives the retry loop against it. This
// is the provider-level gap: the classification/backoff unit tests exist in
// retry_test.go, but nothing drove RetryDo end-to-end over a real socket.
// ---------------------------------------------------------------------------

// postToServer returns a closure that POSTs a chat request to the fake server
// and converts non-200 responses into *HTTPError (parsing Retry-After), exactly
// like the provider HTTP layer does. The client is used for the request.
func postToServer(client *http.Client, url string) func() (string, error) {
	body := []byte(`{"model":"gpt-4o","messages":[{"role":"user","content":"hi"}]}`)
	return func() (string, error) {
		req, err := http.NewRequest(http.MethodPost, url, bytes.NewReader(body))
		if err != nil {
			return "", err
		}
		req.Header.Set("Content-Type", "application/json")
		resp, err := client.Do(req)
		if err != nil {
			return "", err
		}
		defer resp.Body.Close()
		payload, err := io.ReadAll(resp.Body)
		if err != nil {
			return "", err
		}
		if resp.StatusCode != http.StatusOK {
			return "", &HTTPError{
				Status:     resp.StatusCode,
				Body:       string(payload),
				RetryAfter: ParseRetryAfter(resp.Header.Get("Retry-After")),
			}
		}
		return string(payload), nil
	}
}

// TestRetryDo_HTTP_429Storm_RespectsRetryAfter scripts a 429 + Retry-After: 1s
// storm that clears on the third attempt and asserts RetryDo honors the server's
// retry hint: exactly 3 attempts, success, and an elapsed time dominated by the
// two 1s Retry-After delays (a backoff that ignored Retry-After would finish in
// tens of milliseconds, far below the bound).
func TestRetryDo_HTTP_429Storm_RespectsRetryAfter(t *testing.T) {
	srv := newFakeLLMServer(t)
	srv.script(
		srv.httpErrorStep(http.StatusTooManyRequests, 1, "rate limited"),
		srv.httpErrorStep(http.StatusTooManyRequests, 1, "rate limited"),
		srv.openAICompleteStep("all clear"),
	)
	server := srv

	cfg := RetryConfig{
		Attempts: 3,
		MinDelay: 10 * time.Millisecond,
		MaxDelay: 50 * time.Millisecond,
		Jitter:   0,
	}
	client := &http.Client{Timeout: 5 * time.Second}
	run := postToServer(client, server.URL()+"/v1/chat/completions")

	start := time.Now()
	result, err := RetryDo(context.Background(), cfg, run)
	elapsed := time.Since(start)

	if err != nil {
		t.Fatalf("RetryDo returned error after Retry-After storm: %v", err)
	}
	if !strings.Contains(result, "all clear") {
		t.Errorf("result = %q, want the final 200 body", result)
	}
	if got := server.requestCount(); got != 3 {
		t.Errorf("requestCount = %d, want 3 (429, 429, 200)", got)
	}
	// Two 1s Retry-After delays dominate the elapsed time. A generous lower
	// bound still proves the server hint was honored rather than the 10-50ms
	// backoff window.
	if elapsed < 900*time.Millisecond {
		t.Errorf("elapsed = %v, want >= ~900ms (Retry-After delays honored)", elapsed)
	}
}

// TestRetryDo_HTTP_5xxSeries_ExhaustsThenFails scripts a persistent 503 series
// with Attempts: 3 and asserts RetryDo burns exactly three attempts, surfaces
// the final *HTTPError, and never exceeds the configured attempt budget.
func TestRetryDo_HTTP_5xxSeries_ExhaustsThenFails(t *testing.T) {
	srv := newFakeLLMServer(t)
	srv.script(
		srv.httpErrorStep(http.StatusServiceUnavailable, 0, "upstream down"),
		srv.httpErrorStep(http.StatusServiceUnavailable, 0, "upstream down"),
		srv.httpErrorStep(http.StatusServiceUnavailable, 0, "upstream down"),
		srv.httpErrorStep(http.StatusServiceUnavailable, 0, "upstream down"), // never reached
	)
	server := srv

	cfg := RetryConfig{
		Attempts: 3,
		MinDelay: 10 * time.Millisecond,
		MaxDelay: 50 * time.Millisecond,
		Jitter:   0,
	}
	client := &http.Client{Timeout: 5 * time.Second}
	run := postToServer(client, server.URL()+"/v1/chat/completions")

	_, err := RetryDo(context.Background(), cfg, run)
	if err == nil {
		t.Fatal("RetryDo returned nil error on a persistent 503 series")
	}
	var httpErr *HTTPError
	if !errors.As(err, &httpErr) {
		t.Fatalf("err type = %T, want *HTTPError", err)
	}
	if httpErr.Status != http.StatusServiceUnavailable {
		t.Errorf("err status = %d, want 503", httpErr.Status)
	}
	if got := server.requestCount(); got != 3 {
		t.Errorf("requestCount = %d, want 3 (attempts capped at Attempts)", got)
	}
}

// TestRetryDo_HTTP_Timeout_NoFalseRetry verifies a client-side timeout (server
// slower than the client's Timeout) is classified retryable but bounded: RetryDo
// retries up to the attempt budget and then errors, never looping forever.
func TestRetryDo_HTTP_Timeout_NoFalseRetry(t *testing.T) {
	server := newFakeLLMServerEmpty(t)
	// The server always answers ~200ms late, well past the 50ms client timeout.
	server.script(responseStep{
		Status:      http.StatusOK,
		DelayBefore: 200 * time.Millisecond,
		Body:        `{"choices":[{"message":{"role":"assistant","content":"slow"}}]}`,
	})

	cfg := RetryConfig{
		Attempts: 3,
		MinDelay: 10 * time.Millisecond,
		MaxDelay: 50 * time.Millisecond,
		Jitter:   0,
	}
	client := &http.Client{Timeout: 50 * time.Millisecond}
	run := postToServer(client, server.URL()+"/v1/chat/completions")

	start := time.Now()
	_, err := RetryDo(context.Background(), cfg, run)
	elapsed := time.Since(start)

	if err == nil {
		t.Fatal("expected timeout error, got nil")
	}
	if !IsRetryableError(err) {
		t.Errorf("timeout error not classified retryable: %v", err)
	}
	if got := server.requestCount(); got != cfg.Attempts {
		t.Errorf("requestCount = %d, want %d (bounded by attempt budget)", got, cfg.Attempts)
	}
	// 3 attempts at ~50ms each plus two backoff delays — generous upper bound
	// proves the loop did not spin out of control.
	if elapsed > 5*time.Second {
		t.Errorf("elapsed = %v, want bounded (no infinite retry loop)", elapsed)
	}
	if elapsed < 100*time.Millisecond {
		t.Errorf("elapsed = %v, want >= ~3 client timeouts (attempts actually ran)", elapsed)
	}
}
