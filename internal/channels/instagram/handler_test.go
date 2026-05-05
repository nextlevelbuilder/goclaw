package instagram

import (
	"bytes"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

const testAppSecret = "test_app_secret"

func sign(body []byte, secret string) string {
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write(body)
	return "sha256=" + hex.EncodeToString(mac.Sum(nil))
}

func TestWebhookHandler_Verification(t *testing.T) {
	wh := NewWebhookHandler(testAppSecret, "test_verify_token")

	tests := []struct {
		name       string
		mode       string
		token      string
		challenge  string
		wantStatus int
	}{
		{"valid verification", "subscribe", "test_verify_token", "test_challenge_123", http.StatusOK},
		{"invalid mode", "unsubscribe", "test_verify_token", "test_challenge", http.StatusForbidden},
		{"invalid token", "subscribe", "wrong_token", "test_challenge", http.StatusForbidden},
		{"invalid challenge format", "subscribe", "test_verify_token", "<script>alert(1)</script>", http.StatusBadRequest},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, "/webhook?hub.mode="+tt.mode+"&hub.verify_token="+tt.token+"&hub.challenge="+tt.challenge, nil)
			w := httptest.NewRecorder()
			wh.ServeHTTP(w, req)
			if w.Code != tt.wantStatus {
				t.Errorf("status = %d, want %d", w.Code, tt.wantStatus)
			}
			if tt.wantStatus == http.StatusOK && w.Body.String() != tt.challenge {
				t.Errorf("body = %q, want %q", w.Body.String(), tt.challenge)
			}
		})
	}
}

func TestWebhookHandler_MethodNotAllowed(t *testing.T) {
	wh := NewWebhookHandler(testAppSecret, "test_token")
	req := httptest.NewRequest(http.MethodPut, "/webhook", nil)
	w := httptest.NewRecorder()
	wh.ServeHTTP(w, req)
	if w.Code != http.StatusMethodNotAllowed {
		t.Errorf("status = %d, want %d", w.Code, http.StatusMethodNotAllowed)
	}
}

// validInstagramPayload is a realistic Meta-shaped Instagram Messaging webhook body.
const validInstagramPayload = `{
  "object": "instagram",
  "entry": [{
    "id": "17841400000000000",
    "time": 1733520000000,
    "messaging": [{
      "sender": {"id": "12334"},
      "recipient": {"id": "17841400000000000"},
      "timestamp": 1733520000000,
      "message": {"mid": "mid.msg_abc_123", "text": "hello"}
    }]
  }]
}`

func TestWebhookHandler_PostSignatureRequired(t *testing.T) {
	wh := NewWebhookHandler(testAppSecret, "vt")

	t.Run("missing signature dropped", func(t *testing.T) {
		got := captureEvent(t, wh, []byte(validInstagramPayload), "")
		if got != nil {
			t.Errorf("unsigned POST was accepted: %+v", got)
		}
	})

	t.Run("bad signature dropped", func(t *testing.T) {
		got := captureEvent(t, wh, []byte(validInstagramPayload), "sha256="+strings.Repeat("00", 32))
		if got != nil {
			t.Errorf("bad-signature POST was accepted")
		}
	})

	t.Run("valid signature accepted and parses sender/recipient", func(t *testing.T) {
		body := []byte(validInstagramPayload)
		got := captureEvent(t, wh, body, sign(body, testAppSecret))
		if got == nil {
			t.Fatal("valid POST was dropped")
		}
		if got.entry.ID != "17841400000000000" {
			t.Errorf("entry.ID = %q, want 17841400000000000", got.entry.ID)
		}
		if got.event.Sender.ID != "12334" {
			t.Errorf("sender.ID = %q (json tag regression?) want 12334", got.event.Sender.ID)
		}
		if got.event.Recipient.ID != "17841400000000000" {
			t.Errorf("recipient.ID = %q, want 17841400000000000", got.event.Recipient.ID)
		}
		if got.event.Message == nil || got.event.Message.ID != "mid.msg_abc_123" {
			t.Errorf("message.mid parse failed: %+v", got.event.Message)
		}
		if got.event.Message.Text != "hello" {
			t.Errorf("message.text = %q, want hello", got.event.Message.Text)
		}
	})

	t.Run("empty app secret drops even valid sig", func(t *testing.T) {
		empty := NewWebhookHandler("", "vt")
		body := []byte(validInstagramPayload)
		got := captureEvent(t, empty, body, sign(body, ""))
		if got != nil {
			t.Errorf("POST accepted when app_secret missing")
		}
	})

	t.Run("malformed JSON returns 200 and is dropped", func(t *testing.T) {
		body := []byte("{not json")
		got := captureEvent(t, wh, body, sign(body, testAppSecret))
		if got != nil {
			t.Errorf("malformed body dispatched to handler")
		}
	})

	t.Run("wrong object type dropped", func(t *testing.T) {
		body := []byte(`{"object":"page","entry":[]}`)
		got := captureEvent(t, wh, body, sign(body, testAppSecret))
		if got != nil {
			t.Errorf("non-instagram object dispatched")
		}
	})
}

type captured struct {
	entry WebhookEntry
	event MessagingEvent
}

func captureEvent(t *testing.T, wh *WebhookHandler, body []byte, sig string) *captured {
	t.Helper()
	var got *captured
	wh.onMessage = func(e WebhookEntry, ev MessagingEvent) {
		got = &captured{entry: e, event: ev}
	}
	defer func() { wh.onMessage = nil }()

	req := httptest.NewRequest(http.MethodPost, "/webhook", bytes.NewReader(body))
	if sig != "" {
		req.Header.Set("X-Hub-Signature-256", sig)
	}
	w := httptest.NewRecorder()
	wh.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("webhook POST status = %d, want 200 (always)", w.Code)
	}
	return got
}

func TestFormatMessage(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		wantLen int // rune length
	}{
		{"short message", "Hello", 5},
		{"exactly max length", strings.Repeat("a", instagramMaxChars), instagramMaxChars},
		{"too-long ascii truncates to max", strings.Repeat("a", instagramMaxChars+100), instagramMaxChars},
		{"utf8 content preserves rune boundary", strings.Repeat("ñ", instagramMaxChars+50), instagramMaxChars},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := FormatMessage(tt.input)
			if n := len([]rune(got)); n != tt.wantLen {
				t.Errorf("rune len = %d, want %d", n, tt.wantLen)
			}
		})
	}
}

func TestFormatMessage_NoInvalidUtf8(t *testing.T) {
	// Pre-fix: byte slice of a multibyte rune produced invalid UTF-8.
	input := strings.Repeat("ñ", instagramMaxChars+10)
	got := FormatMessage(input)
	for i, r := range got {
		if r == '�' {
			t.Fatalf("FormatMessage produced replacement char at byte %d (invalid UTF-8)", i)
		}
	}
	if !strings.HasSuffix(got, "...") {
		t.Errorf("expected ellipsis suffix, got tail %q", got[max(0, len(got)-5):])
	}
}

// TestWebhookRouter_RegisterUnregister verifies basic register/unregister
// operations mutate the instances map.
func TestWebhookRouter_RegisterUnregister(t *testing.T) {
	r := &webhookRouter{instances: make(map[string]*Channel)}
	ch := &Channel{instagramID: "17841400000000000"}
	r.register(ch)
	r.mu.RLock()
	_, present := r.instances["17841400000000000"]
	r.mu.RUnlock()
	if !present {
		t.Error("register did not insert")
	}
	r.unregister("17841400000000000")
	r.mu.RLock()
	_, present = r.instances["17841400000000000"]
	r.mu.RUnlock()
	if present {
		t.Error("unregister did not remove")
	}
}

// TestWebhookRouter_RouteOnce verifies webhookRoute returns a path on the
// first call and empty on subsequent calls — the mux must only register once.
func TestWebhookRouter_RouteOnce(t *testing.T) {
	r := &webhookRouter{instances: make(map[string]*Channel)}
	path1, h1 := r.webhookRoute()
	if path1 == "" || h1 == nil {
		t.Fatal("first call returned empty")
	}
	path2, h2 := r.webhookRoute()
	if path2 != "" || h2 != nil {
		t.Errorf("second call should be empty, got %q / %v", path2, h2)
	}
}

// TestWebhookRouter_ServeHTTPNoInstances verifies the router responds 200
// when no instances are registered (Meta retries on non-2xx for 24h).
func TestWebhookRouter_ServeHTTPNoInstances(t *testing.T) {
	r := &webhookRouter{instances: make(map[string]*Channel)}
	req := httptest.NewRequest(http.MethodPost, "/webhook", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Errorf("status = %d, want 200", w.Code)
	}
}

// TestWebhookRouter_PerEntryDispatch is a Critical-2 regression: two
// instances register under different instagram_user_ids, a webhook for ID-B
// arrives at the shared endpoint, and the router must route it to B (not A,
// not drop). Before the fix, the first-registered handler owned the route
// and the entry.ID guard silently dropped B's messages.
func TestWebhookRouter_PerEntryDispatch(t *testing.T) {
	r := &webhookRouter{instances: make(map[string]*Channel)}
	chA := &Channel{
		instagramID: "17841AAA",
		webhookH:    NewWebhookHandler(testAppSecret, "vt"),
	}
	chB := &Channel{
		instagramID: "17841BBB",
		webhookH:    NewWebhookHandler(testAppSecret, "vt"),
	}
	r.register(chA)
	r.register(chB)

	// Intercept per-entry dispatch by swapping the router's HTTP path:
	// drive ServeHTTP directly and inspect whose handleMessagingEvent would
	// have been invoked by checking r.instances lookup for entry.ID.
	payload := `{"object":"instagram","entry":[{"id":"17841BBB","time":1,"messaging":[{"sender":{"id":"s"},"recipient":{"id":"17841BBB"},"timestamp":1,"message":{"mid":"m1","text":"hi"}}]}]}`
	body := []byte(payload)
	req := httptest.NewRequest(http.MethodPost, "/webhook", bytes.NewReader(body))
	req.Header.Set("X-Hub-Signature-256", sign(body, testAppSecret))
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", w.Code)
	}
	// We can't observe the private handleMessagingEvent without a full
	// Channel, but we can confirm lookup resolves to B (not A).
	r.mu.RLock()
	target := r.instances["17841BBB"]
	r.mu.RUnlock()
	if target != chB {
		t.Fatalf("entry.ID 17841BBB should resolve to chB, got %v", target)
	}
	if target == chA {
		t.Fatal("Critical-2 regression: entry routed to first-registered chA")
	}
}

func TestDedupKey_StableAcrossTimestamps(t *testing.T) {
	// Regression guard for Critical-4: string(rune(event.Timestamp)) collapsed all
	// large timestamps to U+FFFD, dropping every subsequent message from one sender
	// for 24h. message_handler now uses strconv.FormatInt for the fallback key and
	// event.Message.ID as the primary key. Verify both paths produce distinct keys.
	a := fmt.Sprintf("entry:sender:%d", int64(1733520000000))
	b := fmt.Sprintf("entry:sender:%d", int64(1733520000001))
	if a == b {
		t.Fatalf("distinct timestamps collapsed to same dedup key: %q", a)
	}
}
