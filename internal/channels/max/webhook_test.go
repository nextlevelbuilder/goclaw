package max

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/nextlevelbuilder/goclaw/internal/bus"
)

// =====================================================================
// Helpers
// =====================================================================

// newWebhookChannel builds a Channel suitable for webhook handler tests.
func newWebhookChannel(t *testing.T, mode, webhookURL string) *Channel {
	t.Helper()
	creds := instanceCreds{BotToken: "tok", BotID: 256747471, Username: "test_bot"}
	cfg := instanceConfig{
		Mode:           mode,
		WebhookURL:     webhookURL,
		PollingTimeout: 30,
		DMPolicy:       "open",
		GroupPolicy:    "open",
		HistoryLimit:   50,
	}
	c, err := New("max-webhook-test", creds, cfg, bus.New(), nil, nil, nil)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	return c
}

// =====================================================================
// WebhookHandler — return values
// =====================================================================

func TestWebhookHandler_NotWebhookMode(t *testing.T) {
	c := newWebhookChannel(t, "polling", "https://example.com/hook")
	path, h := c.WebhookHandler()
	if path != "" || h != nil {
		t.Errorf("polling mode should return ('',nil); got (%q, %v)", path, h)
	}
}

func TestWebhookHandler_WebhookButNoURL(t *testing.T) {
	c := newWebhookChannel(t, "webhook", "")
	path, h := c.WebhookHandler()
	if path != "" || h != nil {
		t.Errorf("empty WebhookURL should return ('',nil); got (%q, %v)", path, h)
	}
}

func TestWebhookHandler_HTTPSchemeRejected(t *testing.T) {
	c := newWebhookChannel(t, "webhook", "http://example.com/hook")
	path, h := c.WebhookHandler()
	if path != "" || h != nil {
		t.Errorf("non-https URL should be rejected; got (%q, %v)", path, h)
	}
}

func TestWebhookHandler_DefaultPath(t *testing.T) {
	c := newWebhookChannel(t, "webhook", "https://example.com/")
	path, h := c.WebhookHandler()
	if path != defaultWebhookPath {
		t.Errorf("path = %q, want %q", path, defaultWebhookPath)
	}
	if h == nil {
		t.Error("handler is nil")
	}
}

func TestWebhookHandler_CustomPath(t *testing.T) {
	c := newWebhookChannel(t, "webhook", "https://example.com/custom/secret-uuid")
	path, h := c.WebhookHandler()
	if path != "/custom/secret-uuid" {
		t.Errorf("path = %q, want %q", path, "/custom/secret-uuid")
	}
	if h == nil {
		t.Error("handler is nil")
	}
}

func TestWebhookHandler_StripTrailingSlash(t *testing.T) {
	c := newWebhookChannel(t, "webhook", "https://example.com/hook/")
	path, _ := c.WebhookHandler()
	if path != "/hook" {
		t.Errorf("path = %q, want %q", path, "/hook")
	}
}

// =====================================================================
// serveWebhook — HTTP behavior
// =====================================================================

func TestServeWebhook_RejectsNonPOST(t *testing.T) {
	c := newWebhookChannel(t, "webhook", "https://example.com/hook")
	_, h := c.WebhookHandler()

	for _, method := range []string{"GET", "PUT", "DELETE", "PATCH"} {
		t.Run(method, func(t *testing.T) {
			req := httptest.NewRequest(method, "/hook", nil)
			w := httptest.NewRecorder()
			h.ServeHTTP(w, req)
			if w.Code != http.StatusMethodNotAllowed {
				t.Errorf("status = %d, want 405", w.Code)
			}
			if w.Header().Get("Allow") != http.MethodPost {
				t.Errorf("Allow header = %q, want POST", w.Header().Get("Allow"))
			}
		})
	}
}

func TestServeWebhook_BodyTooLarge(t *testing.T) {
	c := newWebhookChannel(t, "webhook", "https://example.com/hook")
	_, h := c.WebhookHandler()

	// Body just over the limit.
	body := bytes.Repeat([]byte("x"), maxWebhookBodyBytes+1024)
	req := httptest.NewRequest(http.MethodPost, "/hook", bytes.NewReader(body))
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	if w.Code != http.StatusRequestEntityTooLarge {
		t.Errorf("status = %d, want 413", w.Code)
	}
}

func TestServeWebhook_InvalidJSON(t *testing.T) {
	c := newWebhookChannel(t, "webhook", "https://example.com/hook")
	_, h := c.WebhookHandler()

	req := httptest.NewRequest(http.MethodPost, "/hook",
		strings.NewReader("not valid json{"))
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	if w.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400", w.Code)
	}
}

func TestServeWebhook_ValidUpdate_Returns200(t *testing.T) {
	c := newWebhookChannel(t, "webhook", "https://example.com/hook")
	_, h := c.WebhookHandler()

	body := loadFixture(t, "update_dm_text.json")

	// Wrap fixture into a single Update — our fixture is the full
	// UpdatesResponse shape, but webhook receives one Update. Extract first.
	// We inline the JSON of one update for simplicity.
	oneUpdate := []byte(`{
		"update_type": "message_created",
		"timestamp": 1777966187384,
		"message": {
			"sender": {"user_id": 74757262, "name": "test"},
			"recipient": {"user_id": 256747471, "chat_id": 188289857, "chat_type": "dialog"},
			"timestamp": 1777966187384,
			"message": {"mid": "mid.x", "seq": 1, "text": "hello via webhook"}
		}
	}`)

	req := httptest.NewRequest(http.MethodPost, "/hook", bytes.NewReader(oneUpdate))
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Errorf("status = %d, want 200; body = %q", w.Code, w.Body.String())
	}

	// Suppress unused warning — we don't actually read body in this test.
	_ = body
}

func TestServeWebhook_DispatchErrorStillReturns200(t *testing.T) {
	// An update that's well-formed JSON but missing required fields.
	// handleUpdate should silently skip; webhook still returns 200.
	c := newWebhookChannel(t, "webhook", "https://example.com/hook")
	_, h := c.WebhookHandler()

	body := []byte(`{"update_type": "message_created"}`) // no message field

	req := httptest.NewRequest(http.MethodPost, "/hook", bytes.NewReader(body))
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Errorf("status = %d, want 200 even on dispatch issues", w.Code)
	}
}

// =====================================================================
// webhook dispatch context lifetime — Day 5b regression
// =====================================================================

// TestServeWebhook_DispatchSurvivesStop verifies the Day 5b fix: a webhook
// delivery in flight when Stop is called must complete its dispatch
// goroutine independently. Before the fix, dispatch shared c.pollRunCtx,
// so Stop would cancel mid-dispatch and the message could be lost after
// we'd already 200-OK'd Max.
//
// We can't directly observe "dispatch completed" without an inbound
// listener, but we can verify:
//  1. ServeHTTP returns 200 promptly (does not block on dispatch).
//  2. Stop on the underlying Channel is non-fatal (no panic, no goroutine
//     leak detectable in this short test).
//
// A stronger end-to-end test lives in integration_test.go.
func TestServeWebhook_DispatchSurvivesStop(t *testing.T) {
	c := newWebhookChannel(t, "webhook", "https://example.com/hook")
	_, h := c.WebhookHandler()

	body := []byte(`{
		"update_type": "message_created",
		"timestamp": 1730000000,
		"message": {
			"sender":    {"user_id": 74757262, "first_name": "Test"},
			"recipient": {"chat_type": "dialog", "user_id": 256747471, "chat_id": 188289857},
			"timestamp": 1730000000,
			"message":   {"mid": "mid.test", "seq": 1, "text": "hello"}
		}
	}`)

	req := httptest.NewRequest(http.MethodPost, "/hook", bytes.NewReader(body))
	w := httptest.NewRecorder()

	// Dispatch happens in a goroutine; ServeHTTP returns immediately after 200.
	h.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", w.Code)
	}

	// Now Stop the channel — this used to cancel the dispatch context. With
	// the fix, dispatch has its own context.Background-based timeout and is
	// unaffected.
	if err := c.Stop(context.Background()); err != nil {
		t.Errorf("Stop after webhook dispatch returned error: %v", err)
	}
}

func TestWebhookPathFromURL(t *testing.T) {
	tests := []struct {
		name    string
		url     string
		want    string
		wantErr bool
	}{
		{"https with path", "https://example.com/foo/bar", "/foo/bar", false},
		{"https no path", "https://example.com", defaultWebhookPath, false},
		{"https slash only", "https://example.com/", defaultWebhookPath, false},
		{"https trailing slash", "https://example.com/hook/", "/hook", false},
		{"http rejected", "http://example.com/hook", "", true},
		{"ftp rejected", "ftp://example.com/hook", "", true},
		{"no scheme rejected", "example.com/hook", "", true},
		{"malformed", "://broken", "", true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := webhookPathFromURL(tt.url)
			if (err != nil) != tt.wantErr {
				t.Fatalf("err = %v, wantErr %v", err, tt.wantErr)
			}
			if !tt.wantErr && got != tt.want {
				t.Errorf("got %q, want %q", got, tt.want)
			}
		})
	}
}
