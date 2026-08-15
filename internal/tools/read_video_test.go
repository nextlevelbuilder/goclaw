package tools

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/nextlevelbuilder/goclaw/internal/security"
	"github.com/nextlevelbuilder/goclaw/internal/store"
)

type mockCredentialProvider struct {
	apiKey  string
	apiBase string
}

func (m *mockCredentialProvider) APIKey() string  { return m.apiKey }
func (m *mockCredentialProvider) APIBase() string { return m.apiBase }

func TestReadVideo_BothMediaIdAndUrl_Error(t *testing.T) {
	tool := NewReadVideoTool(nil, nil)

	res := tool.Execute(context.Background(), map[string]any{
		"prompt":   "describe this video",
		"media_id": "video-123",
		"url":      "https://example.com/video.mp4",
	})

	if !res.IsError {
		t.Fatalf("expected error when both media_id and url are provided")
	}

	if !strings.Contains(res.ForLLM, "Both 'media_id' and 'url' parameters cannot be specified") {
		t.Errorf("unexpected error message: %s", res.ForLLM)
	}
}

func TestReadVideo_PrivateURL_Error(t *testing.T) {
	tool := NewReadVideoTool(nil, nil)

	res := tool.Execute(context.Background(), map[string]any{
		"prompt": "describe this video",
		"url":    "http://127.0.0.1/video.mp4",
	})

	if !res.IsError {
		t.Fatalf("expected error for private video URL")
	}
	if !strings.Contains(res.ForLLM, "Invalid video URL") {
		t.Errorf("unexpected error message: %s", res.ForLLM)
	}
}

// TestReadVideo_GeminiURL_FailsClosedUnderAgentBudget locks the fail-closed
// contract for a streamed video URL. The stream is never buffered, so its bytes
// cannot be counted into completeInput; under an agent budget the call refuses
// before any transport rather than undercounting. Because every tool LLM call
// is agent-scoped in production, this refusal — not the downstream
// Content-Length / 2 GB / HTTP-status checks — is the reachable behavior. Those
// transport validations remain in callProvider for defense in depth but are
// unreachable once an agent budget is present.
func TestReadVideo_GeminiURL_FailsClosedUnderAgentBudget(t *testing.T) {
	security.SetAllowLoopbackForTest(true)
	defer security.SetAllowLoopbackForTest(false)

	// A server that would otherwise satisfy the static-streaming constraints
	// (valid Content-Length, 2xx). The call must still fail closed before it is
	// ever contacted, because the payload cannot be counted.
	hits := 0
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits++
		w.Header().Set("Content-Length", "1024")
		w.WriteHeader(http.StatusOK)
		w.Write(make([]byte, 1024))
	}))
	defer ts.Close()

	tool := NewReadVideoTool(nil, nil)
	cp := &mockCredentialProvider{apiKey: "test-key"}

	ctx := store.WithAgentContextWindow(context.Background(), 200_000)
	ctx = store.WithAgentMaxTokens(ctx, 32_000)

	params := map[string]any{
		"prompt":         "describe this video",
		"url":            ts.URL,
		"_provider_type": "gemini",
	}

	_, _, err := tool.callProvider(ctx, cp, "gemini", "gemini-2.5-flash", params)
	if err == nil {
		t.Fatalf("expected fail-closed for an unverifiable streamed video URL under an agent budget")
	}
	if !strings.Contains(err.Error(), "cannot verify streamed native media") {
		t.Errorf("unexpected error, want fail-closed refusal: %v", err)
	}
	if hits != 0 {
		t.Errorf("fail-closed must refuse before any transport, but the URL was contacted %d time(s)", hits)
	}
}
