package providers

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"
)

// ---------------------------------------------------------------------------
// RunWithFailover through REAL HTTP: a fake LLM server scripts per-candidate
// chaos (429 primary, 5xx series) and asserts the failover engine rotates to
// the backup. The streamed-chunk case drives the real HTTP SSE read path to
// prove a mid-stream failure that already emitted output is settled by
// FailoverStreamed and never triggers a fallback.
// ---------------------------------------------------------------------------

// failoverHTTPCandidates is the shared candidate list: two OpenAI profiles of
// the same model — key1 first (the "primary"), key2 the backup.
var failoverHTTPCandidates = []ModelCandidate{
	{Provider: "openai", Model: "gpt-4o", ProfileID: "key1"},
	{Provider: "openai", Model: "gpt-4o", ProfileID: "key2"},
}

// TestFailover_HTTP_429Primary_BackupSucceeds scripts the primary (key1) to
// answer 429 for every request and the backup (key2) to return 200. The runFn
// performs a real HTTP POST and converts non-200 into *HTTPError. RunWithFailover
// must classify the 429 as a transient profile rotation and reach the backup.
func TestFailover_HTTP_429Primary_BackupSucceeds(t *testing.T) {
	server := newFakeLLMServerEmpty(t)
	server.script(
		server.httpErrorStep(http.StatusTooManyRequests, 0, "rate limited"), // key1
		server.openAICompleteStep("backup result"),                          // key2
	)

	cfg := FailoverConfig{
		Candidates: failoverHTTPCandidates,
		Classifier: NewDefaultClassifier(),
	}

	client := &http.Client{Timeout: 5 * time.Second}
	ctx := context.Background()
	result, attempts, err := RunWithFailover(ctx, cfg, failoverHTTPRunFn(server.URL(), client))

	if err != nil {
		t.Fatalf("failover returned error on 429 primary -> 200 backup: %v", err)
	}
	if !strings.Contains(result, "backup result") {
		t.Errorf("result = %q, want the backup candidate's output", result)
	}
	if got := server.requestCount(); got != 2 {
		t.Errorf("requestCount = %d, want 2 (primary 429, backup 200)", got)
	}
	if len(attempts) < 1 {
		t.Errorf("attempts recorded = %d, want >= 1 (the 429 failure)", len(attempts))
	}
	if len(attempts) > 0 && attempts[0].Classification.Reason != FailoverRateLimit {
		t.Errorf("first attempt reason = %v, want rate_limit", attempts[0].Classification.Reason)
	}
}

// TestFailover_HTTP_5xxSeries_Rotates scripts a persistent 503 on the primary
// and a 200 on the backup; the 503 is classified server_error (profile-rotatable)
// and the run lands on the backup.
func TestFailover_HTTP_5xxSeries_Rotates(t *testing.T) {
	server := newFakeLLMServerEmpty(t)
	server.script(
		server.httpErrorStep(http.StatusServiceUnavailable, 0, "upstream down"), // key1
		server.openAICompleteStep("backup result"),                              // key2
	)

	cfg := FailoverConfig{
		Candidates: failoverHTTPCandidates,
		Classifier: NewDefaultClassifier(),
	}

	client := &http.Client{Timeout: 5 * time.Second}
	result, attempts, err := RunWithFailover(context.Background(), cfg, failoverHTTPRunFn(server.URL(), client))

	if err != nil {
		t.Fatalf("failover returned error on 503 primary -> 200 backup: %v", err)
	}
	if !strings.Contains(result, "backup result") {
		t.Errorf("result = %q, want the backup candidate's output", result)
	}
	if got := server.requestCount(); got != 2 {
		t.Errorf("requestCount = %d, want 2 (primary 503, backup 200)", got)
	}
	if len(attempts) < 1 {
		t.Errorf("attempts recorded = %d, want >= 1 (the 503 failure)", len(attempts))
	}
	if len(attempts) > 0 && attempts[0].Classification.Reason != FailoverServerError {
		t.Errorf("first attempt reason = %v, want server_error", attempts[0].Classification.Reason)
	}
}

// TestFailover_HTTP_StreamedChunk_DoesNotFallback scripts the primary to emit a
// single SSE data chunk then abruptly close the connection (no [DONE]). The
// runFn reads the stream, emits the chunk through the raw stream reader, and
// wraps the mid-stream failure in FailoverStreamed. RunWithFailover must settle
// the run on that error without ever calling the backup.
func TestFailover_HTTP_StreamedChunk_DoesNotFallback(t *testing.T) {
	server := newFakeLLMServerEmpty(t)
	server.script(responseStep{
		Status:    http.StatusOK,
		Headers:   http.Header{"Connection": []string{"close"}},
		SSEFrames: []sseFrame{server.openAITextDelta("partial answer")},
		// no SSEDone: the connection closes (Connection: close) mid-stream,
		// after a chunk escaped, without a [DONE] terminator
	})

	cfg := FailoverConfig{
		Candidates: failoverHTTPCandidates,
		Classifier: NewDefaultClassifier(),
	}

	client := &http.Client{Timeout: 5 * time.Second}
	ctx := context.Background()
	result, attempts, err := RunWithFailover(ctx, cfg, failoverStreamReadRunFn(server.URL(), client))

	if err == nil {
		t.Fatal("expected the streamed error to settle the run, got nil")
	}
	var streamedErr *FailoverStreamed
	if !errors.As(err, &streamedErr) {
		t.Fatalf("err type = %T, want *FailoverStreamed", err)
	}
	if result != "partial answer" {
		t.Errorf("result = %q, want the partial output that escaped", result)
	}
	if got := server.requestCount(); got != 1 {
		t.Errorf("requestCount = %d, want 1 (no fallback after streamed chunk)", got)
	}
	if len(attempts) != 0 {
		t.Errorf("attempts recorded = %d, want 0 (streamed errors are not classified)", len(attempts))
	}
}

// failoverHTTPRunFn builds a runFn for RunWithFailover that performs a real
// HTTP POST to the fake server and maps failures to *HTTPError.
func failoverHTTPRunFn(baseURL string, client *http.Client) func(context.Context, ModelCandidate) (string, error) {
	return func(_ context.Context, candidate ModelCandidate) (string, error) {
		body := strings.NewReader(`{"model":"` + candidate.Model + `","messages":[{"role":"user","content":"hi"}]}`)
		req, err := http.NewRequest(http.MethodPost, baseURL+"/v1/chat/completions", body)
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
				Status: resp.StatusCode,
				Body:   string(payload),
			}
		}
		return string(payload), nil
	}
}

// failoverStreamReadRunFn builds a runFn that opens the real HTTP stream, reads
// it to completion, and reports a FailoverStreamed when output escaped before
// the stream terminated cleanly (no [DONE]). This mirrors the provider stream
// adapters: chunks are delivered to the caller as they arrive, so a mid-stream
// close must settle the run instead of replaying through another candidate.
func failoverStreamReadRunFn(baseURL string, client *http.Client) func(context.Context, ModelCandidate) (string, error) {
	return func(_ context.Context, candidate ModelCandidate) (string, error) {
		body := strings.NewReader(`{"model":"` + candidate.Model + `","messages":[{"role":"user","content":"hi"}],"stream":true}`)
		req, err := http.NewRequest(http.MethodPost, baseURL+"/v1/chat/completions", body)
		if err != nil {
			return "", err
		}
		req.Header.Set("Content-Type", "application/json")
		resp, err := client.Do(req)
		if err != nil {
			return "", err
		}
		defer resp.Body.Close()

		sse := NewSSEScanner(resp.Body)
		emitted := ""
		sawDone := false
		for sse.Next() {
			data := sse.Data()
			if data == "[DONE]" {
				sawDone = true
				break
			}
			var chunk openAIStreamChunk
			if err := unmarshalStreamChunk([]byte(data), &chunk); err != nil {
				continue
			}
			if len(chunk.Choices) > 0 {
				if d := chunk.Choices[0].Delta.Content; d != "" {
					emitted += d
				}
			}
		}
		if err := sse.Err(); err != nil {
			// A transport-level failure mid-stream: whatever escaped already went out.
			return emitted, &FailoverStreamed{}
		}
		if !sawDone && emitted != "" {
			// Stream ended without a terminator after output escaped.
			return emitted, &FailoverStreamed{}
		}
		if !sawDone {
			// Closed before any output escaped — a plain pre-output failure.
			return "", &HTTPError{Status: http.StatusInternalServerError, Body: "stream closed before data"}
		}
		return emitted, nil
	}
}

// unmarshalStreamChunk decodes an OpenAI-compat SSE data payload.
func unmarshalStreamChunk(b []byte, out *openAIStreamChunk) error {
	return json.Unmarshal(b, out)
}
