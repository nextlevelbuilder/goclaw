package providers

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// fakeAnthropicTokenSource is a controllable test double.
type fakeAnthropicTokenSource struct {
	token             string
	accountID         string
	forceRefreshResp  string
	forceRefreshErr   error
	tokenCalls        int
	forceRefreshCalls int
}

func (f *fakeAnthropicTokenSource) Token() (string, error) {
	f.tokenCalls++
	return f.token, nil
}

func (f *fakeAnthropicTokenSource) ForceRefresh(ctx context.Context) (string, error) {
	f.forceRefreshCalls++
	if f.forceRefreshErr != nil {
		return "", f.forceRefreshErr
	}
	return f.forceRefreshResp, nil
}

func (f *fakeAnthropicTokenSource) AccountID() string {
	return f.accountID
}

// minimalAnthropicResponse is a valid non-streaming Anthropic API response body.
const minimalAnthropicResponse = `{"id":"msg_1","type":"message","role":"assistant","content":[{"type":"text","text":"hi"}],"stop_reason":"end_turn","usage":{"input_tokens":1,"output_tokens":1}}`

// TestAnthropicProvider_APIKeyAuth verifies existing API key auth path is unchanged.
func TestAnthropicProvider_APIKeyAuth(t *testing.T) {
	var gotAuth, gotAPIKey, gotBeta string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		gotAPIKey = r.Header.Get("x-api-key")
		gotBeta = r.Header.Get("anthropic-beta")
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(minimalAnthropicResponse)) //nolint:errcheck
	}))
	defer srv.Close()

	p := NewAnthropicProvider("test-api-key", WithAnthropicBaseURL(srv.URL))
	p.retryConfig.Attempts = 1
	_, err := p.Chat(context.Background(), ChatRequest{
		Model:    "claude-sonnet-4-6",
		Messages: []Message{{Role: "user", Content: "hello"}},
	})
	if err != nil {
		t.Fatalf("Chat: %v", err)
	}
	if gotAuth != "" {
		t.Errorf("unexpected Authorization header: %q", gotAuth)
	}
	if gotAPIKey != "test-api-key" {
		t.Errorf("x-api-key = %q, want test-api-key", gotAPIKey)
	}
	if gotBeta != "" {
		t.Errorf("unexpected beta header for non-thinking request: %q", gotBeta)
	}
}

// TestAnthropicProvider_DefaultName verifies Name() returns "anthropic" by default.
func TestAnthropicProvider_DefaultName(t *testing.T) {
	p := NewAnthropicProvider("key")
	if p.Name() != "anthropic" {
		t.Errorf("Name() = %q, want \"anthropic\"", p.Name())
	}
}

// TestAnthropicProvider_CustomName verifies WithAnthropicName overrides the registry name.
func TestAnthropicProvider_CustomName(t *testing.T) {
	p := NewAnthropicProvider("key", WithAnthropicName("anthropic-oauth"))
	if p.Name() != "anthropic-oauth" {
		t.Errorf("Name() = %q, want \"anthropic-oauth\"", p.Name())
	}
}

// TestAnthropicProvider_ThinkingBetaHeaderAppend verifies the beta header is appended (not
// overwritten) when thinking is enabled — fixes the latent Header.Set overwrite bug.
func TestAnthropicProvider_ThinkingBetaHeaderAppend(t *testing.T) {
	var gotBeta string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotBeta = r.Header.Get("anthropic-beta")
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(minimalAnthropicResponse)) //nolint:errcheck
	}))
	defer srv.Close()

	p := NewAnthropicProvider("test-api-key", WithAnthropicBaseURL(srv.URL))
	p.retryConfig.Attempts = 1
	_, err := p.Chat(context.Background(), ChatRequest{
		Model:    "claude-sonnet-4-6",
		Messages: []Message{{Role: "user", Content: "hello"}},
		Options:  map[string]any{OptThinkingLevel: "low"},
	})
	if err != nil {
		t.Fatalf("Chat: %v", err)
	}
	if gotBeta != "interleaved-thinking-2025-05-14" {
		t.Errorf("beta header = %q, want \"interleaved-thinking-2025-05-14\"", gotBeta)
	}
}

// TestAppendBetaHeader verifies the helper appends correctly and doesn't overwrite.
func TestAppendBetaHeader(t *testing.T) {
	t.Run("empty", func(t *testing.T) {
		req, _ := http.NewRequest("GET", "/", nil)
		appendBetaHeader(req, "foo")
		if got := req.Header.Get("anthropic-beta"); got != "foo" {
			t.Errorf("got %q, want \"foo\"", got)
		}
	})

	t.Run("append", func(t *testing.T) {
		req, _ := http.NewRequest("GET", "/", nil)
		appendBetaHeader(req, "foo")
		appendBetaHeader(req, "bar")
		if got := req.Header.Get("anthropic-beta"); got != "foo,bar" {
			t.Errorf("got %q, want \"foo,bar\"", got)
		}
	})
}

// TestAnthropicProvider_401Retry verifies that a 401 triggers ForceRefresh + single retry.
// Uses a nil tokenSource provider to confirm API-key path is NOT affected.
func TestAnthropicProvider_APIKey_401_NoRetry(t *testing.T) {
	calls := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		w.WriteHeader(http.StatusUnauthorized)
		w.Write([]byte(`{"error":"unauthorized"}`)) //nolint:errcheck
	}))
	defer srv.Close()

	p := NewAnthropicProvider("test-api-key", WithAnthropicBaseURL(srv.URL))
	p.retryConfig.Attempts = 1 // disable retry loop so we get a clean count
	_, err := p.Chat(context.Background(), ChatRequest{
		Model:    "claude-sonnet-4-6",
		Messages: []Message{{Role: "user", Content: "hello"}},
	})
	if err == nil {
		t.Fatal("expected error on 401")
	}
	// API key path: no OAuth retry, so exactly 1 request
	if calls != 1 {
		t.Errorf("expected 1 request for API key 401, got %d", calls)
	}
}

// TestAnthropicProvider_OAuthHeaders verifies OAuth path sends Bearer + beta header and no x-api-key.
func TestAnthropicProvider_OAuthHeaders(t *testing.T) {
	var gotAuth, gotAPIKey, gotBeta string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		gotAPIKey = r.Header.Get("x-api-key")
		gotBeta = r.Header.Get("anthropic-beta")
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(minimalAnthropicResponse)) //nolint:errcheck
	}))
	defer srv.Close()

	ts := &fakeAnthropicTokenSource{token: "oauth-access-token"}
	p := NewAnthropicProvider("", WithAnthropicBaseURL(srv.URL), WithAnthropicOAuth(ts))
	p.retryConfig.Attempts = 1
	_, err := p.Chat(context.Background(), ChatRequest{
		Model:    "claude-sonnet-4-6",
		Messages: []Message{{Role: "user", Content: "hello"}},
	})
	if err != nil {
		t.Fatalf("Chat: %v", err)
	}
	if gotAuth != "Bearer oauth-access-token" {
		t.Errorf("Authorization = %q, want Bearer oauth-access-token", gotAuth)
	}
	if gotAPIKey != "" {
		t.Errorf("x-api-key should be empty for OAuth, got %q", gotAPIKey)
	}
	if gotBeta != "oauth-2025-04-20" {
		t.Errorf("anthropic-beta = %q, want oauth-2025-04-20", gotBeta)
	}
}

// TestAnthropicProvider_OAuthAndThinkingBetaHeaders verifies OAuth + thinking produces both beta values.
func TestAnthropicProvider_OAuthAndThinkingBetaHeaders(t *testing.T) {
	var gotBeta string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotBeta = r.Header.Get("anthropic-beta")
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(minimalAnthropicResponse)) //nolint:errcheck
	}))
	defer srv.Close()

	ts := &fakeAnthropicTokenSource{token: "oauth-access-token"}
	p := NewAnthropicProvider("", WithAnthropicBaseURL(srv.URL), WithAnthropicOAuth(ts))
	p.retryConfig.Attempts = 1
	_, err := p.Chat(context.Background(), ChatRequest{
		Model:    "claude-sonnet-4-6",
		Messages: []Message{{Role: "user", Content: "hello"}},
		Options:  map[string]any{OptThinkingLevel: "low"},
	})
	if err != nil {
		t.Fatalf("Chat: %v", err)
	}
	if !strings.Contains(gotBeta, "oauth-2025-04-20") {
		t.Errorf("beta header missing oauth-2025-04-20: %q", gotBeta)
	}
	if !strings.Contains(gotBeta, "interleaved-thinking-2025-05-14") {
		t.Errorf("beta header missing interleaved-thinking: %q", gotBeta)
	}
	if !strings.Contains(gotBeta, ",") {
		t.Errorf("expected comma-separated beta headers, got %q", gotBeta)
	}
}

// TestAnthropicProvider_OAuth401TriggersRefresh verifies 401 triggers ForceRefresh + single retry.
func TestAnthropicProvider_OAuth401TriggersRefresh(t *testing.T) {
	attempt := 0
	var authHeaders []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		authHeaders = append(authHeaders, r.Header.Get("Authorization"))
		attempt++
		if attempt == 1 {
			w.WriteHeader(http.StatusUnauthorized)
			w.Write([]byte(`{"error":"token expired"}`)) //nolint:errcheck
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(minimalAnthropicResponse)) //nolint:errcheck
	}))
	defer srv.Close()

	ts := &fakeAnthropicTokenSource{
		token:            "old-token",
		forceRefreshResp: "new-token",
	}
	p := NewAnthropicProvider("", WithAnthropicBaseURL(srv.URL), WithAnthropicOAuth(ts))
	p.retryConfig.Attempts = 1
	_, err := p.Chat(context.Background(), ChatRequest{
		Model:    "claude-sonnet-4-6",
		Messages: []Message{{Role: "user", Content: "hello"}},
	})
	if err != nil {
		t.Fatalf("Chat: %v", err)
	}
	if ts.forceRefreshCalls != 1 {
		t.Errorf("ForceRefresh calls = %d, want 1", ts.forceRefreshCalls)
	}
	if attempt != 2 {
		t.Errorf("request attempts = %d, want 2 (original + retry)", attempt)
	}
	if len(authHeaders) != 2 {
		t.Fatalf("captured %d auth headers, want 2", len(authHeaders))
	}
	if authHeaders[0] != "Bearer old-token" {
		t.Errorf("first attempt Authorization = %q, want Bearer old-token", authHeaders[0])
	}
	if authHeaders[1] != "Bearer new-token" {
		t.Errorf("retry Authorization = %q, want Bearer new-token", authHeaders[1])
	}
}

// TestAnthropicProvider_OAuth401RetryBounded verifies 401 on retry does NOT loop, returns error.
func TestAnthropicProvider_OAuth401RetryBounded(t *testing.T) {
	attempt := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		attempt++
		w.WriteHeader(http.StatusUnauthorized)
		w.Write([]byte(`{"error":"still expired"}`)) //nolint:errcheck
	}))
	defer srv.Close()

	ts := &fakeAnthropicTokenSource{
		token:            "token1",
		forceRefreshResp: "token2",
	}
	p := NewAnthropicProvider("", WithAnthropicBaseURL(srv.URL), WithAnthropicOAuth(ts))
	p.retryConfig.Attempts = 1
	_, err := p.Chat(context.Background(), ChatRequest{
		Model:    "claude-sonnet-4-6",
		Messages: []Message{{Role: "user", Content: "hello"}},
	})
	if err == nil {
		t.Fatal("expected error after repeated 401, got nil")
	}
	if attempt != 2 {
		t.Errorf("request attempts = %d, want exactly 2 (no infinite loop)", attempt)
	}
	if ts.forceRefreshCalls != 1 {
		t.Errorf("ForceRefresh calls = %d, want exactly 1", ts.forceRefreshCalls)
	}
}

// TestAnthropicProvider_OAuth403Revoked verifies 403 with revoked body retries; without "revoked" does not.
func TestAnthropicProvider_OAuth403Revoked(t *testing.T) {
	cases := []struct {
		name          string
		body          string
		wantAttempts  int
		wantRefreshes int
		wantErr       bool
	}{
		{"revoked token retries", `{"type":"error","error":{"type":"forbidden","message":"OAuth token has been revoked"}}`, 2, 1, false},
		{"non-revoked 403 no retry", `{"type":"error","error":{"type":"forbidden","message":"insufficient permissions"}}`, 1, 0, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			attempts := 0
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				attempts++
				if attempts == 1 {
					w.WriteHeader(http.StatusForbidden)
					w.Write([]byte(tc.body)) //nolint:errcheck
					return
				}
				w.Header().Set("Content-Type", "application/json")
				w.Write([]byte(minimalAnthropicResponse)) //nolint:errcheck
			}))
			defer srv.Close()

			ts := &fakeAnthropicTokenSource{
				token:            "t1",
				forceRefreshResp: "t2",
			}
			p := NewAnthropicProvider("", WithAnthropicBaseURL(srv.URL), WithAnthropicOAuth(ts))
			p.retryConfig.Attempts = 1
			_, err := p.Chat(context.Background(), ChatRequest{
				Model:    "claude-sonnet-4-6",
				Messages: []Message{{Role: "user", Content: "hello"}},
			})
			if tc.wantErr && err == nil {
				t.Error("expected error, got nil")
			}
			if !tc.wantErr && err != nil {
				t.Errorf("unexpected error: %v", err)
			}
			if attempts != tc.wantAttempts {
				t.Errorf("attempts = %d, want %d", attempts, tc.wantAttempts)
			}
			if ts.forceRefreshCalls != tc.wantRefreshes {
				t.Errorf("ForceRefresh calls = %d, want %d", ts.forceRefreshCalls, tc.wantRefreshes)
			}
		})
	}
}

// TestAnthropicProvider_OAuthMetadataUserID verifies the request body includes metadata.user_id.
func TestAnthropicProvider_OAuthMetadataUserID(t *testing.T) {
	var gotBody map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewDecoder(r.Body).Decode(&gotBody) //nolint:errcheck
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(minimalAnthropicResponse)) //nolint:errcheck
	}))
	defer srv.Close()

	ts := &fakeAnthropicTokenSource{
		token:     "oauth-token",
		accountID: "acc-uuid-12345",
	}
	p := NewAnthropicProvider("", WithAnthropicBaseURL(srv.URL), WithAnthropicOAuth(ts))
	p.retryConfig.Attempts = 1
	_, err := p.Chat(context.Background(), ChatRequest{
		Model:    "claude-sonnet-4-6",
		Messages: []Message{{Role: "user", Content: "hello"}},
	})
	if err != nil {
		t.Fatalf("Chat: %v", err)
	}
	meta, ok := gotBody["metadata"].(map[string]any)
	if !ok {
		t.Fatalf("request body missing metadata field: %+v", gotBody)
	}
	if meta["user_id"] != "acc-uuid-12345" {
		t.Errorf("metadata.user_id = %v, want acc-uuid-12345", meta["user_id"])
	}
}

// TestIsRevokedTokenError verifies the structured + fallback revocation detection.
func TestIsRevokedTokenError(t *testing.T) {
	cases := []struct {
		name string
		body string
		want bool
	}{
		{"anthropic error json with revoked", `{"type":"error","error":{"type":"forbidden","message":"OAuth token has been revoked"}}`, true},
		{"anthropic error json with invalidated", `{"type":"error","error":{"type":"forbidden","message":"Access invalidated"}}`, true},
		{"anthropic error json without revoked", `{"type":"error","error":{"type":"forbidden","message":"insufficient scope"}}`, false},
		{"plain text body with revoked", "OAuth token has been revoked", true},
		{"plain text body without", "forbidden", false},
		{"empty body", "", false},
		{"malformed json with revoked word", `{broken json revoked`, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := isRevokedTokenError([]byte(tc.body)); got != tc.want {
				t.Errorf("isRevokedTokenError(%q) = %v, want %v", tc.body, got, tc.want)
			}
		})
	}
}
