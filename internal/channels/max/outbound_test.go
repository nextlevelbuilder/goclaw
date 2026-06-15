package max

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/nextlevelbuilder/goclaw/internal/bus"
)

// =====================================================================
// Helpers — build a Channel wired to a mock Max API server
// =====================================================================

// mockMaxBackend is a configurable httptest mock for the Max API send flow.
// Captures all received requests for assertion.
type mockMaxBackend struct {
	server *httptest.Server

	mu        sync.Mutex
	requests  []capturedSend
	responder func(req capturedSend) (status int, bodyJSON string)
}

type capturedSend struct {
	Method string
	Path   string
	Query  map[string]string
	Body   SendMessageRequest
}

func newMockBackend(t *testing.T) *mockMaxBackend {
	t.Helper()
	m := &mockMaxBackend{}
	m.server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		m.mu.Lock()
		defer m.mu.Unlock()

		cap := capturedSend{
			Method: r.Method,
			Path:   r.URL.Path,
			Query:  flattenQuery(r.URL.Query()),
		}
		if r.Body != nil {
			body, _ := io.ReadAll(r.Body)
			_ = json.Unmarshal(body, &cap.Body)
		}
		m.requests = append(m.requests, cap)

		status, body := 200, defaultMockBody(cap)
		if m.responder != nil {
			status, body = m.responder(cap)
		}

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		_, _ = io.WriteString(w, body)
	}))
	t.Cleanup(m.server.Close)
	return m
}

// defaultMockBody returns a generic successful SendMessageResponse.
func defaultMockBody(cap capturedSend) string {
	chatID := cap.Query["chat_id"]
	if chatID == "" {
		chatID = cap.Query["user_id"]
	}
	mid := "mid.test." + strconv.Itoa(len(cap.Body.Text))
	return `{
		"message":{"timestamp":1,"message":{"mid":"` + mid + `","seq":1,"text":"` + escapeJSON(cap.Body.Text) + `"}},
		"chat_id":` + chatID + `,
		"recipient_id":` + chatID + `,
		"message_id":"` + mid + `"
	}`
}

func escapeJSON(s string) string {
	b, _ := json.Marshal(s)
	return string(b[1 : len(b)-1]) // strip wrapping quotes
}

func flattenQuery(v map[string][]string) map[string]string {
	out := make(map[string]string, len(v))
	for k, vs := range v {
		if len(vs) > 0 {
			out[k] = vs[0]
		}
	}
	return out
}

// captured returns a copy of all captured requests under lock.
// Safe to call from tests while the server is still receiving requests.
func (m *mockMaxBackend) captured() []capturedSend {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]capturedSend, len(m.requests))
	copy(out, m.requests)
	return out
}

// channelWithMock returns a Channel pointed at the mock backend.
func channelWithMock(t *testing.T, m *mockMaxBackend) *Channel {
	t.Helper()
	creds := instanceCreds{
		BotToken: "test-token",
		BotID:    256747471,
		Username: "test_bot",
	}
	cfg := instanceConfig{
		Mode:           "polling",
		PollingTimeout: 30,
		DMPolicy:       "open",
		GroupPolicy:    "open",
		HistoryLimit:   50,
	}
	c, err := New("test-max", creds, cfg, bus.New(), nil, nil, nil)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	// Replace the client with one pointing at the mock backend.
	c.client = NewClient("test-token",
		WithBaseURL(m.server.URL),
		WithMaxRetries(1),
	)
	return c
}

// =====================================================================
// Send: simple DM
// =====================================================================

func TestSend_DMSimpleText(t *testing.T) {
	m := newMockBackend(t)
	c := channelWithMock(t, m)

	err := c.Send(context.Background(), bus.OutboundMessage{
		Channel: "test-max",
		ChatID:  "188289857",
		Content: "Привет, как дела?",
	})
	if err != nil {
		t.Fatalf("Send: %v", err)
	}

	if got := len(m.captured()); got != 1 {
		t.Fatalf("got %d requests, want 1", got)
	}
	req := m.captured()[0]
	if req.Method != http.MethodPost {
		t.Errorf("method = %s", req.Method)
	}
	if req.Path != "/messages" {
		t.Errorf("path = %s", req.Path)
	}
	if req.Query["chat_id"] != "188289857" {
		t.Errorf("chat_id = %q", req.Query["chat_id"])
	}
	if req.Body.Text != "Привет, как дела?" {
		t.Errorf("body text = %q", req.Body.Text)
	}
	if req.Body.Format != "markdown" {
		t.Errorf("format = %q, want markdown", req.Body.Format)
	}
}

// =====================================================================
// Send: long content gets chunked
// =====================================================================

func TestSend_ChunksLongText(t *testing.T) {
	m := newMockBackend(t)
	c := channelWithMock(t, m)

	// Build text larger than maxMessageBytes.
	long := strings.Repeat("paragraph ", 1500) // 15000 bytes
	err := c.Send(context.Background(), bus.OutboundMessage{
		Channel: "test-max",
		ChatID:  "999",
		Content: long,
	})
	if err != nil {
		t.Fatalf("Send: %v", err)
	}

	if got := len(m.captured()); got < 4 {
		t.Fatalf("got %d requests, want >= 4 chunks for 15000-byte input", got)
	}

	// Each chunk must respect the byte limit.
	for i, req := range m.captured() {
		if len(req.Body.Text) > maxMessageBytes {
			t.Errorf("chunk %d: %d bytes > max %d", i, len(req.Body.Text), maxMessageBytes)
		}
		if req.Query["chat_id"] != "999" {
			t.Errorf("chunk %d: chat_id = %q", i, req.Query["chat_id"])
		}
	}
}

// =====================================================================
// Send: empty content with no media is no-op
// =====================================================================

func TestSend_EmptyContent_NoOp(t *testing.T) {
	m := newMockBackend(t)
	c := channelWithMock(t, m)

	err := c.Send(context.Background(), bus.OutboundMessage{
		Channel: "test-max",
		ChatID:  "188289857",
		Content: "",
	})
	if err != nil {
		t.Fatalf("Send: %v", err)
	}
	if got := len(m.captured()); got != 0 {
		t.Errorf("expected 0 requests for empty content, got %d", got)
	}
}

// =====================================================================
// Send: whitespace-only content is treated as empty
// =====================================================================

func TestSend_WhitespaceContent_NoOp(t *testing.T) {
	m := newMockBackend(t)
	c := channelWithMock(t, m)

	err := c.Send(context.Background(), bus.OutboundMessage{
		Channel: "test-max",
		ChatID:  "188289857",
		Content: "   \n\n\t  ",
	})
	if err != nil {
		t.Fatalf("Send: %v", err)
	}
	if got := len(m.captured()); got != 0 {
		t.Errorf("expected 0 requests, got %d", got)
	}
}

// =====================================================================
// Send: media-only message currently a no-op (Day 4 work)
// =====================================================================

func TestSend_MediaOnly_Day3NoOp(t *testing.T) {
	m := newMockBackend(t)
	c := channelWithMock(t, m)

	err := c.Send(context.Background(), bus.OutboundMessage{
		Channel: "test-max",
		ChatID:  "188289857",
		Content: "",
		Media: []bus.MediaAttachment{
			{URL: "/tmp/test.png", ContentType: "image/png"},
		},
	})
	if err != nil {
		t.Fatalf("Send: %v", err)
	}
	if got := len(m.captured()); got != 0 {
		t.Errorf("Day 3 should not send media — got %d requests", got)
	}
}

// =====================================================================
// Send: invalid ChatID returns error without HTTP call
// =====================================================================

func TestSend_InvalidChatID(t *testing.T) {
	m := newMockBackend(t)
	c := channelWithMock(t, m)

	tests := []struct {
		name   string
		chatID string
	}{
		{"empty", ""},
		{"non-numeric", "not-a-number"},
		{"zero", "0"},
		{"hex", "0xff"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := c.Send(context.Background(), bus.OutboundMessage{
				ChatID:  tt.chatID,
				Content: "x",
			})
			if err == nil {
				t.Fatalf("expected error for ChatID=%q", tt.chatID)
			}
			if !strings.Contains(err.Error(), "ChatID") {
				t.Errorf("error should mention ChatID: %v", err)
			}
		})
	}
	if got := len(m.captured()); got != 0 {
		t.Errorf("expected 0 HTTP calls for invalid ChatIDs, got %d", got)
	}
}

// =====================================================================
// Send: API returns 5xx — Send propagates error
// =====================================================================

func TestSend_BackendError(t *testing.T) {
	m := newMockBackend(t)
	m.responder = func(req capturedSend) (int, string) {
		return 500, `{"code":"server_error","message":"oops"}`
	}
	c := channelWithMock(t, m)

	err := c.Send(context.Background(), bus.OutboundMessage{
		ChatID:  "999",
		Content: "x",
	})
	if err == nil {
		t.Fatal("expected error")
	}
}

// =====================================================================
// Send: stores message_id of last chunk for streaming use
// =====================================================================

func TestSend_PersistsLastMessageID(t *testing.T) {
	var counter int64
	m := newMockBackend(t)
	m.responder = func(req capturedSend) (int, string) {
		n := atomic.AddInt64(&counter, 1)
		mid := "mid.chunk_" + strconv.FormatInt(n, 10)
		return 200, `{"message":{"timestamp":1,"message":{"mid":"` + mid + `","seq":1}},"message_id":"` + mid + `"}`
	}
	c := channelWithMock(t, m)

	// Long enough to produce 2+ chunks.
	long := strings.Repeat("p ", 3000)
	err := c.Send(context.Background(), bus.OutboundMessage{
		ChatID:  "999",
		Content: long,
	})
	if err != nil {
		t.Fatalf("Send: %v", err)
	}

	if got := len(m.captured()); got < 2 {
		t.Fatalf("expected >= 2 chunks, got %d", got)
	}
	stored := c.lastMessageIDFor("999")
	if stored == "" {
		t.Fatal("expected stored message_id, got empty")
	}
	// Must be the LAST chunk's id, not the first.
	want := "mid.chunk_" + strconv.FormatInt(counter, 10)
	if stored != want {
		t.Errorf("stored = %q, want %q (last chunk)", stored, want)
	}
}

// =====================================================================
// parseChatID
// =====================================================================

func TestParseChatID(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		want    int64
		wantErr bool
	}{
		{"valid positive", "188289857", 188289857, false},
		{"valid negative", "-100123", -100123, false},
		{"empty", "", 0, true},
		{"zero", "0", 0, true},
		{"non-numeric", "abc", 0, true},
		{"hex", "0x10", 0, true},
		{"trailing space", "123 ", 0, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := parseChatID(tt.input)
			if (err != nil) != tt.wantErr {
				t.Fatalf("parseChatID(%q) error = %v, wantErr %v",
					tt.input, err, tt.wantErr)
			}
			if !tt.wantErr && got != tt.want {
				t.Errorf("parseChatID(%q) = %d, want %d", tt.input, got, tt.want)
			}
		})
	}
}

// =====================================================================
// lastMessageIDFor with no prior send returns empty
// =====================================================================

func TestLastMessageID_EmptyForUnknownChat(t *testing.T) {
	m := newMockBackend(t)
	c := channelWithMock(t, m)

	if got := c.lastMessageIDFor("never-sent"); got != "" {
		t.Errorf("expected empty, got %q", got)
	}
}

// =====================================================================
// Send: cancellation propagates
// =====================================================================

func TestSend_ContextCancellation(t *testing.T) {
	m := newMockBackend(t)
	c := channelWithMock(t, m)

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // already cancelled

	err := c.Send(ctx, bus.OutboundMessage{
		ChatID:  "999",
		Content: "x",
	})
	if err == nil {
		t.Fatal("expected error from cancelled context")
	}
	if !errors.Is(err, context.Canceled) {
		// Don't be strict about the wrapping path — just ensure something
		// indicates cancellation.
		if !strings.Contains(err.Error(), "context") &&
			!strings.Contains(err.Error(), "cancel") {
			t.Errorf("error doesn't look like cancellation: %v", err)
		}
	}
}
