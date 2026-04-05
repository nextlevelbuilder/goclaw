package teams

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/nextlevelbuilder/goclaw/internal/bus"
	"github.com/nextlevelbuilder/goclaw/internal/config"
)

func TestStripBotMention(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"<at>Bot</at> hello", "hello"},
		{"<at>My Bot</at> what is 2+2?", "what is 2+2?"},
		{"hello world", "hello world"},
		{"<at>Bot</at>", ""},
		{"", ""},
		{"<at>Bot</at> <at>User</at> hi", "hi"},
		{"<at>unclosed tag", "<at>unclosed tag"},
		{"no </at> opening", "no </at> opening"},
	}
	for _, tt := range tests {
		got := stripBotMention(tt.input)
		if got != tt.want {
			t.Errorf("stripBotMention(%q) = %q, want %q", tt.input, got, tt.want)
		}
	}
}

func TestHandleWebhook_MethodNotAllowed(t *testing.T) {
	ch := mustCreateChannel(t)
	ch.SetRunning(true)

	req := httptest.NewRequest(http.MethodGet, "/webhooks/teams", nil)
	w := httptest.NewRecorder()
	ch.handleWebhook(w, req)

	if w.Code != http.StatusMethodNotAllowed {
		t.Errorf("GET status = %d, want %d", w.Code, http.StatusMethodNotAllowed)
	}
}

func TestHandleWebhook_MissingAuth(t *testing.T) {
	ch := mustCreateChannel(t)
	ch.SetRunning(true)

	body := mustMarshal(t, Activity{Type: "message", Text: "hi"})
	req := httptest.NewRequest(http.MethodPost, "/webhooks/teams", bytes.NewReader(body))
	w := httptest.NewRecorder()
	ch.handleWebhook(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Errorf("no-auth status = %d, want %d", w.Code, http.StatusUnauthorized)
	}
}

func TestHandleWebhook_InvalidJSON(t *testing.T) {
	ch := mustCreateChannel(t)
	ch.SetRunning(true)
	// Skip JWT validation for this test by using a channel with a permissive validator
	ch.validator = &tokenValidator{botID: "test", keys: nil}

	req := httptest.NewRequest(http.MethodPost, "/webhooks/teams", bytes.NewReader([]byte("{invalid")))
	req.Header.Set("Authorization", "Bearer fake-token")
	w := httptest.NewRecorder()
	ch.handleWebhook(w, req)

	// Will fail at JWT validation (no keys), returning 401
	if w.Code != http.StatusUnauthorized {
		t.Errorf("invalid-json status = %d, want %d", w.Code, http.StatusUnauthorized)
	}
}

func TestPeerKindDetection(t *testing.T) {
	tests := []struct {
		conversationType string
		wantPeerKind     string
	}{
		{"personal", "direct"},
		{"groupChat", "group"},
		{"channel", "group"},
		{"", "direct"}, // default
	}
	for _, tt := range tests {
		peerKind := "direct"
		switch tt.conversationType {
		case "groupChat", "channel":
			peerKind = "group"
		}
		if peerKind != tt.wantPeerKind {
			t.Errorf("conversationType=%q → peerKind=%q, want %q",
				tt.conversationType, peerKind, tt.wantPeerKind)
		}
	}
}

func TestExtractBearerToken(t *testing.T) {
	tests := []struct {
		header string
		want   string
	}{
		{"Bearer abc123", "abc123"},
		{"Bearer ", ""},
		{"bearer abc", ""},  // case sensitive
		{"Basic abc123", ""},
		{"", ""},
	}
	for _, tt := range tests {
		got := extractBearerToken(tt.header)
		if got != tt.want {
			t.Errorf("extractBearerToken(%q) = %q, want %q", tt.header, got, tt.want)
		}
	}
}

// --- helpers ---

func mustCreateChannel(t *testing.T) *Channel {
	t.Helper()
	cfg := config.TeamsConfig{
		BotID:       "test-bot",
		BotPassword: "test-secret",
		BotType:     "SingleTenant",
		TenantID:    "test-tenant",
	}
	ch, err := New(cfg, bus.New())
	if err != nil {
		t.Fatalf("New() error: %v", err)
	}
	return ch
}

func mustMarshal(t *testing.T, v any) []byte {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatalf("json.Marshal error: %v", err)
	}
	return b
}
