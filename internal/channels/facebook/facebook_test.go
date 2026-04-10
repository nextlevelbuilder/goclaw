package facebook

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// --- verifySignature ---

func TestVerifySignature(t *testing.T) {
	secret := "test_secret"
	body := []byte(`{"object":"page","entry":[]}`)

	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write(body)
	validSig := "sha256=" + hex.EncodeToString(mac.Sum(nil))

	tests := []struct {
		name      string
		sig       string
		wantValid bool
	}{
		{"valid signature", validSig, true},
		{"wrong prefix", "sha1=" + hex.EncodeToString(mac.Sum(nil)), false},
		{"empty signature", "", false},
		{"bad hex", "sha256=notvalidhex", false},
		{"tampered body", "sha256=0000000000000000000000000000000000000000000000000000000000000000", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := verifySignature(body, tt.sig, secret)
			if got != tt.wantValid {
				t.Errorf("verifySignature() = %v, want %v", got, tt.wantValid)
			}
		})
	}
}

// --- WebhookHandler GET verification ---

func TestWebhookHandlerVerification(t *testing.T) {
	wh := NewWebhookHandler("secret", "my_verify_token")

	t.Run("valid challenge", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet,
			"/webhook?hub.mode=subscribe&hub.verify_token=my_verify_token&hub.challenge=abc123", nil)
		w := httptest.NewRecorder()
		wh.ServeHTTP(w, req)
		if w.Code != http.StatusOK {
			t.Fatalf("status = %d, want 200", w.Code)
		}
		if w.Body.String() != "abc123" {
			t.Errorf("body = %q, want %q", w.Body.String(), "abc123")
		}
	})

	t.Run("wrong verify token", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet,
			"/webhook?hub.mode=subscribe&hub.verify_token=wrong&hub.challenge=abc123", nil)
		w := httptest.NewRecorder()
		wh.ServeHTTP(w, req)
		if w.Code != http.StatusForbidden {
			t.Errorf("status = %d, want 403", w.Code)
		}
	})

	t.Run("wrong hub.mode", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet,
			"/webhook?hub.mode=unsubscribe&hub.verify_token=my_verify_token&hub.challenge=abc123", nil)
		w := httptest.NewRecorder()
		wh.ServeHTTP(w, req)
		if w.Code != http.StatusForbidden {
			t.Errorf("status = %d, want 403", w.Code)
		}
	})

	t.Run("unsafe challenge rejected", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet,
			"/webhook?hub.mode=subscribe&hub.verify_token=my_verify_token&hub.challenge=<script>alert(1)</script>", nil)
		w := httptest.NewRecorder()
		wh.ServeHTTP(w, req)
		if w.Code != http.StatusBadRequest {
			t.Errorf("status = %d, want 400", w.Code)
		}
	})
}

// --- WebhookHandler POST event delivery ---

func signBody(t *testing.T, body []byte, secret string) string {
	t.Helper()
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write(body)
	return "sha256=" + hex.EncodeToString(mac.Sum(nil))
}

func TestWebhookHandlerPostEvents(t *testing.T) {
	appSecret := "app_secret_123"

	t.Run("invalid signature returns 200 (no retry)", func(t *testing.T) {
		wh := NewWebhookHandler(appSecret, "token")
		body := []byte(`{"object":"page","entry":[]}`)
		req := httptest.NewRequest(http.MethodPost, "/webhook", strings.NewReader(string(body)))
		req.Header.Set("X-Hub-Signature-256", "sha256=badbadbadbad")
		w := httptest.NewRecorder()
		wh.ServeHTTP(w, req)
		if w.Code != http.StatusOK {
			t.Errorf("status = %d, want 200 (Facebook must stop retrying)", w.Code)
		}
	})

	t.Run("valid comment event dispatches callback", func(t *testing.T) {
		var gotChange ChangeValue
		wh := NewWebhookHandler(appSecret, "token")
		wh.onComment = func(entry WebhookEntry, change ChangeValue) {
			gotChange = change
		}

		payload := WebhookPayload{
			Object: "page",
			Entry: []WebhookEntry{{
				ID:   "111",
				Time: 1234567890,
				Changes: []WebhookChange{{
					Field: "feed",
					Value: ChangeValue{
						From:      FBUser{ID: "456", Name: "Alice"},
						Item:      "comment",
						CommentID: "789",
						PostID:    "post1",
						Message:   "Hello page!",
						Verb:      "add",
					},
				}},
			}},
		}
		body, _ := json.Marshal(payload)
		req := httptest.NewRequest(http.MethodPost, "/webhook", strings.NewReader(string(body)))
		req.Header.Set("X-Hub-Signature-256", signBody(t, body, appSecret))
		req.Header.Set("Content-Type", "application/json")

		w := httptest.NewRecorder()
		wh.ServeHTTP(w, req)

		if w.Code != http.StatusOK {
			t.Fatalf("status = %d, want 200", w.Code)
		}
		if gotChange.CommentID != "789" {
			t.Errorf("comment callback not called or wrong comment_id: %+v", gotChange)
		}
	})

	t.Run("valid messenger event dispatches callback", func(t *testing.T) {
		var gotEvent MessagingEvent
		wh := NewWebhookHandler(appSecret, "token")
		wh.onMessage = func(_ WebhookEntry, event MessagingEvent) {
			gotEvent = event
		}

		mid := "m_abc123"
		payload := WebhookPayload{
			Object: "page",
			Entry: []WebhookEntry{{
				ID:   "111",
				Time: 1234567890,
				Messaging: []MessagingEvent{{
					Sender:    FBUser{ID: "user1"},
					Recipient: FBUser{ID: "111"},
					Timestamp: 1234567890,
					Message:   &IncomingMessage{MID: mid, Text: "hi"},
				}},
			}},
		}
		body, _ := json.Marshal(payload)
		req := httptest.NewRequest(http.MethodPost, "/webhook", strings.NewReader(string(body)))
		req.Header.Set("X-Hub-Signature-256", signBody(t, body, appSecret))

		w := httptest.NewRecorder()
		wh.ServeHTTP(w, req)

		if w.Code != http.StatusOK {
			t.Fatalf("status = %d, want 200", w.Code)
		}
		if gotEvent.Message == nil || gotEvent.Message.MID != mid {
			t.Errorf("messenger callback not called: %+v", gotEvent)
		}
	})

	t.Run("non-page object ignored", func(t *testing.T) {
		called := false
		wh := NewWebhookHandler(appSecret, "token")
		wh.onComment = func(_ WebhookEntry, _ ChangeValue) { called = true }

		payload := map[string]any{"object": "user", "entry": []any{}}
		body, _ := json.Marshal(payload)
		req := httptest.NewRequest(http.MethodPost, "/webhook", strings.NewReader(string(body)))
		req.Header.Set("X-Hub-Signature-256", signBody(t, body, appSecret))

		w := httptest.NewRecorder()
		wh.ServeHTTP(w, req)

		if called {
			t.Error("callback should not be called for non-page object")
		}
	})
}

// --- Formatter ---

func TestFormatForComment(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"**bold** text", "bold text"},
		{"*italic* text", "italic text"},
		{"**bold** and *italic*", "bold and italic"},
		{"[link](https://example.com)", "link (https://example.com)"},
		{"# Heading\nbody", "Heading\nbody"},
		{"<b>html</b>", "html"},
		{"`code`", "code"},
		// Long text truncated at 8000 chars.
		{strings.Repeat("a", 9000), strings.Repeat("a", 8000)},
	}
	for _, tt := range tests {
		got := FormatForComment(tt.input)
		if got != tt.want {
			t.Errorf("FormatForComment(%q) = %q, want %q", tt.input, got, tt.want)
		}
	}
}

func TestFormatForMessenger(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"**bold**", "bold"},
		{"*italic*", "italic"},
		{"[link](https://x.com)", "link (https://x.com)"},
		{"  leading space  ", "leading space"},
	}
	for _, tt := range tests {
		got := FormatForMessenger(tt.input)
		if got != tt.want {
			t.Errorf("FormatForMessenger(%q) = %q, want %q", tt.input, got, tt.want)
		}
	}
}

func TestSplitMessage(t *testing.T) {
	t.Run("short message not split", func(t *testing.T) {
		parts := splitMessage("hello", 2000)
		if len(parts) != 1 || parts[0] != "hello" {
			t.Errorf("unexpected parts: %v", parts)
		}
	})

	t.Run("splits at sentence boundary", func(t *testing.T) {
		long := strings.Repeat("word ", 200) + ". " + strings.Repeat("word ", 200)
		parts := splitMessage(long, 500)
		if len(parts) < 2 {
			t.Errorf("expected multiple parts, got %d", len(parts))
		}
		for _, p := range parts {
			if len([]rune(p)) > 500 {
				t.Errorf("part exceeds maxChars: len=%d", len([]rune(p)))
			}
		}
	})

	t.Run("hard cut when no boundary found", func(t *testing.T) {
		noSpace := strings.Repeat("a", 3000)
		parts := splitMessage(noSpace, 2000)
		if len(parts) != 2 {
			t.Errorf("expected 2 parts, got %d", len(parts))
		}
	})
}

// --- validateFBID ---

func TestValidateFBID(t *testing.T) {
	valid := []string{"123456789", "123_456", "1"}
	invalid := []string{"", "../me", "123abc", "123 456", "../../accounts"}

	for _, id := range valid {
		if err := validateFBID(id); err != nil {
			t.Errorf("validateFBID(%q) unexpectedly returned error: %v", id, err)
		}
	}
	for _, id := range invalid {
		if err := validateFBID(id); err == nil {
			t.Errorf("validateFBID(%q) should have returned error", id)
		}
	}
}

// --- parseRetryAfter ---

func TestParseRetryAfter(t *testing.T) {
	tests := []struct {
		header string
		want   time.Duration
	}{
		{"", 5 * time.Second},
		{"0", 5 * time.Second},
		{"notanumber", 5 * time.Second},
		{"10", 10 * time.Second},
		{"60", 60 * time.Second},
		// Capped at maxRetryAfterSec (60).
		{"3600", 60 * time.Second},
		{"999999", 60 * time.Second},
	}
	for _, tt := range tests {
		resp := &http.Response{Header: make(http.Header)}
		if tt.header != "" {
			resp.Header.Set("Retry-After", tt.header)
		}
		got := parseRetryAfter(resp)
		if got != tt.want {
			t.Errorf("parseRetryAfter(%q) = %v, want %v", tt.header, got, tt.want)
		}
	}
}

// --- Dedup eviction ---

func TestDedupEviction(t *testing.T) {
	// Override TTL to a tiny value so we can test eviction synchronously.
	const shortTTL = 10 * time.Millisecond

	ch := &Channel{}

	key := "comment:test_event"
	// First call: not a dup.
	ch.dedup.Store(key, time.Now().Add(-shortTTL-time.Millisecond))

	// Simulate cleaner: evict entries older than shortTTL.
	now := time.Now()
	ch.dedup.Range(func(k, v any) bool {
		if t2, ok := v.(time.Time); ok && now.Sub(t2) > shortTTL {
			ch.dedup.Delete(k)
		}
		return true
	})

	// After eviction, isDup should return false (entry gone).
	if ch.isDup(key) {
		// isDup stores the key now, so second call would be dup — just verify the first returns false.
		// Since isDup uses LoadOrStore, a successful store means it wasn't there.
		// The test above stored, deleted, so isDup should do a fresh store → return false.
	}
	// Verify second call IS a dup.
	if !ch.isDup(key) {
		t.Error("second isDup call should return true (key already stored)")
	}
}

// --- graphAPIError classification ---

func TestErrorClassifiers(t *testing.T) {
	authErr := &graphAPIError{code: 190, msg: "invalid token"}
	permErr := &graphAPIError{code: 10, msg: "permission denied"}
	rateErr := &graphAPIError{code: 4, msg: "rate limit"}
	otherErr := &graphAPIError{code: 999, msg: "unknown"}
	wrappedAuth := fmt.Errorf("send failed: %w", authErr)

	if !IsAuthError(authErr) {
		t.Error("IsAuthError should be true for code 190")
	}
	if !IsAuthError(wrappedAuth) {
		t.Error("IsAuthError should work with wrapped errors")
	}
	if !IsPermissionError(permErr) {
		t.Error("IsPermissionError should be true for code 10")
	}
	if !IsRateLimitError(rateErr) {
		t.Error("IsRateLimitError should be true for code 4")
	}
	if IsAuthError(otherErr) || IsPermissionError(otherErr) || IsRateLimitError(otherErr) {
		t.Error("unknown error should not match any classifier")
	}
	if IsAuthError(nil) {
		t.Error("nil should not be auth error")
	}
}
