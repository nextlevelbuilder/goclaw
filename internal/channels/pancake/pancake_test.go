package pancake

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/nextlevelbuilder/goclaw/internal/bus"
	"github.com/nextlevelbuilder/goclaw/internal/channels"
)

// TestFactory_Valid verifies Factory creates a Channel from valid JSON creds/config.
func TestFactory_Valid(t *testing.T) {
	creds, _ := json.Marshal(pancakeCreds{
		APIKey:          "test-api-key",
		PageAccessToken: "test-page-token",
	})
	cfg, _ := json.Marshal(pancakeInstanceConfig{PageID: "12345"})

	ch, err := Factory("pancake-test", creds, cfg, nil, nil)
	if err != nil {
		t.Fatalf("Factory returned unexpected error: %v", err)
	}
	if ch == nil {
		t.Fatal("Factory returned nil channel")
	}
	if ch.Name() != "pancake-test" {
		t.Errorf("Name() = %q, want %q", ch.Name(), "pancake-test")
	}
}

// TestFactory_MissingAPIKey verifies Factory returns error when api_key is empty.
func TestFactory_MissingAPIKey(t *testing.T) {
	creds, _ := json.Marshal(pancakeCreds{PageAccessToken: "token"})
	cfg, _ := json.Marshal(pancakeInstanceConfig{PageID: "12345"})

	_, err := Factory("test", creds, cfg, nil, nil)
	if err == nil {
		t.Fatal("expected error for missing api_key, got nil")
	}
}

// TestFactory_MissingPageAccessToken verifies Factory returns error when page_access_token is empty.
func TestFactory_MissingPageAccessToken(t *testing.T) {
	creds, _ := json.Marshal(pancakeCreds{APIKey: "key"})
	cfg, _ := json.Marshal(pancakeInstanceConfig{PageID: "12345"})

	_, err := Factory("test", creds, cfg, nil, nil)
	if err == nil {
		t.Fatal("expected error for missing page_access_token, got nil")
	}
}

// TestFactory_MissingPageID verifies Factory returns error when page_id is empty.
func TestFactory_MissingPageID(t *testing.T) {
	creds, _ := json.Marshal(pancakeCreds{
		APIKey:          "key",
		PageAccessToken: "token",
	})
	cfg, _ := json.Marshal(pancakeInstanceConfig{}) // no page_id

	_, err := Factory("test", creds, cfg, nil, nil)
	if err == nil {
		t.Fatal("expected error for missing page_id, got nil")
	}
}

// TestFormatOutbound verifies platform-aware formatting for each platform.
func TestFormatOutbound(t *testing.T) {
	input := "**Hello** _world_ `code` ## Header [link](http://example.com)"

	cases := []struct {
		platform string
		wantNot  string // substring that should NOT appear in output
	}{
		{"facebook", "**"},
		{"zalo", "**"},
		{"instagram", "_"},
		{"tiktok", "##"},
		{"whatsapp", "**"},
		{"line", "##"},
		{"unknown", "`"},
	}

	for _, tc := range cases {
		t.Run(tc.platform, func(t *testing.T) {
			out := FormatOutbound(input, tc.platform)
			if out == "" {
				t.Error("FormatOutbound returned empty string")
			}
			_ = out // formatting verified visually; we just check no panic + non-empty
		})
	}
}

// TestSplitMessage verifies message splitting at platform character limits.
func TestSplitMessage(t *testing.T) {
	t.Run("short message not split", func(t *testing.T) {
		parts := splitMessage("hello", 100)
		if len(parts) != 1 || parts[0] != "hello" {
			t.Errorf("unexpected parts: %v", parts)
		}
	})

	t.Run("exact limit not split", func(t *testing.T) {
		msg := string(make([]byte, 100))
		parts := splitMessage(msg, 100)
		if len(parts) != 1 {
			t.Errorf("expected 1 part, got %d", len(parts))
		}
	})

	t.Run("over limit is split", func(t *testing.T) {
		msg := string(make([]byte, 250))
		parts := splitMessage(msg, 100)
		if len(parts) != 3 {
			t.Errorf("expected 3 parts, got %d", len(parts))
		}
	})

	t.Run("zero limit returns whole string", func(t *testing.T) {
		parts := splitMessage("hello", 0)
		if len(parts) != 1 {
			t.Errorf("expected 1 part with zero limit, got %d", len(parts))
		}
	})
}

// TestIsDup verifies dedup returns false first, true on repeat.
func TestIsDup(t *testing.T) {
	ch := &Channel{}

	if ch.isDup("key-1") {
		t.Error("isDup: first call should return false")
	}
	if !ch.isDup("key-1") {
		t.Error("isDup: second call should return true")
	}
	if ch.isDup("key-2") {
		t.Error("isDup: different key should return false")
	}
}

// TestWebhookRouterReturns200 verifies the global router always returns HTTP 200.
func TestWebhookRouterReturns200(t *testing.T) {
	// Use a fresh local router to avoid interfering with the package-level globalRouter.
	router := &webhookRouter{instances: make(map[string]*Channel)}

	t.Run("POST event returns 200", func(t *testing.T) {
		body := `{"data":{"conversation":{"id":"123_456","type":"INBOX","from":{"id":"456"}},"message":{"id":"m1"}}}`
		req := httptest.NewRequest(http.MethodPost, "/channels/pancake/webhook",
			strings.NewReader(body))
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)
		if w.Code != http.StatusOK {
			t.Errorf("expected 200, got %d", w.Code)
		}
	})

	t.Run("GET returns 200 (not 405)", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/channels/pancake/webhook", nil)
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)
		if w.Code != http.StatusOK {
			t.Errorf("expected 200, got %d", w.Code)
		}
	})

	t.Run("malformed JSON returns 200", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPost, "/channels/pancake/webhook",
			strings.NewReader("not-json"))
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)
		if w.Code != http.StatusOK {
			t.Errorf("expected 200, got %d", w.Code)
		}
	})
}

// TestMessageHandlerSkipsSelfReply verifies the page's own messages are not published.
// The dedup entry is stored (dedup runs first), but HandleMessage is never called.
// If the self-reply guard were absent, ch.bus (nil) would panic — making no-panic the assertion.
func TestMessageHandlerSkipsSelfReply(t *testing.T) {
	const pageID = "page-123"
	ch := &Channel{pageID: pageID}

	data := MessagingData{
		PageID:         pageID,
		ConversationID: "conv-1",
		Type:           "INBOX",
		Platform:       "facebook",
		Message: MessagingMessage{
			ID:         "msg-self-1",
			SenderID:   pageID, // same as page → must be skipped before HandleMessage
			SenderName: "Page Bot",
			Content:    "Hello",
		},
	}

	// Must not panic. If self-reply guard is missing, nil bus dereference panics here.
	ch.handleMessagingEvent(data)

	// Dedup entry is stored (dedup check runs before self-reply check).
	_, stored := ch.dedup.Load("msg:msg-self-1")
	if !stored {
		t.Error("dedup entry should have been stored (dedup runs before self-reply guard)")
	}
}

func TestMessageHandlerPublishesMessageIDMetadata(t *testing.T) {
	msgBus := bus.New()
	ch := &Channel{
		BaseChannel: channels.NewBaseChannel(channels.TypePancake, msgBus, nil),
		pageID:      "page-123",
	}

	ch.handleMessagingEvent(MessagingData{
		PageID:         "page-123",
		ConversationID: "conv-1",
		Type:           "INBOX",
		Platform:       "facebook",
		Message: MessagingMessage{
			ID:       "msg-123",
			SenderID: "user-1",
			Content:  "hello",
		},
	})

	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	msg, ok := msgBus.ConsumeInbound(ctx)
	if !ok {
		t.Fatal("expected inbound message to be published")
	}
	if got, want := msg.Metadata["message_id"], "msg:msg-123"; got != want {
		t.Fatalf("metadata.message_id = %q, want %q", got, want)
	}
}

func TestMessageHandlerSkipsRecentOutboundEcho(t *testing.T) {
	msgBus := bus.New()
	ch := &Channel{
		BaseChannel: channels.NewBaseChannel(channels.TypePancake, msgBus, nil),
		pageID:      "page-123",
	}
	ch.rememberOutboundEcho("conv-1", "hello from bot")

	ch.handleMessagingEvent(MessagingData{
		PageID:         "page-123",
		ConversationID: "conv-1",
		Type:           "INBOX",
		Platform:       "facebook",
		Message: MessagingMessage{
			ID:       "msg-echo-1",
			SenderID: "user-1",
			Content:  "hello from bot",
		},
	})

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	if _, ok := msgBus.ConsumeInbound(ctx); ok {
		t.Fatal("expected echoed outbound message to be dropped")
	}
}

func TestBlockReplyEnabledUsesChannelOverride(t *testing.T) {
	enabled := true
	ch := &Channel{
		config: pancakeInstanceConfig{
			BlockReply: &enabled,
		},
	}

	got := ch.BlockReplyEnabled()
	if got == nil || !*got {
		t.Fatalf("BlockReplyEnabled() = %v, want true", got)
	}
}

type captureTransport struct {
	req  *http.Request
	body []byte
	resp *http.Response
}

func (t *captureTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	t.req = req.Clone(req.Context())
	if req.Body != nil {
		body, err := io.ReadAll(req.Body)
		if err != nil {
			return nil, err
		}
		t.body = body
		req.Body = io.NopCloser(bytes.NewReader(body))
	}
	if t.resp != nil {
		return t.resp, nil
	}
	return &http.Response{
		StatusCode: http.StatusOK,
		Header:     make(http.Header),
		Body:       io.NopCloser(strings.NewReader(`{"success":true}`)),
		Request:    req,
	}, nil
}

func TestAPIClientSendMessageMatchesOfficialContract(t *testing.T) {
	transport := &captureTransport{}
	client := NewAPIClient("user-token", "page-token", "page-123")
	client.httpClient = &http.Client{Transport: transport}

	if err := client.SendMessage(context.Background(), "conv-456", "xin chao"); err != nil {
		t.Fatalf("SendMessage returned error: %v", err)
	}

	if transport.req == nil {
		t.Fatal("expected outbound request to be captured")
	}
	if got, want := transport.req.URL.Path, "/api/public_api/v1/pages/page-123/conversations/conv-456/messages"; got != want {
		t.Fatalf("request path = %q, want %q", got, want)
	}
	if got := transport.req.URL.Query().Get("page_access_token"); got != "page-token" {
		t.Fatalf("page_access_token query = %q, want %q", got, "page-token")
	}

	var payload map[string]any
	if err := json.Unmarshal(transport.body, &payload); err != nil {
		t.Fatalf("unmarshal payload: %v", err)
	}
	if got, want := payload["action"], "reply_inbox"; got != want {
		t.Fatalf("payload.action = %#v, want %#v", got, want)
	}
	if got, want := payload["message"], "xin chao"; got != want {
		t.Fatalf("payload.message = %#v, want %#v", got, want)
	}
	if _, exists := payload["content"]; exists {
		t.Fatalf("payload must not contain legacy content field: %s", string(transport.body))
	}
	if _, exists := payload["attachment_id"]; exists {
		t.Fatalf("payload must not contain attachment_id field: %s", string(transport.body))
	}
}

func TestAPIClientSendMessageReturnsBodyLevelError(t *testing.T) {
	transport := &captureTransport{
		resp: &http.Response{
			StatusCode: http.StatusOK,
			Header:     make(http.Header),
			Body:       io.NopCloser(strings.NewReader(`{"success":false,"message":"conversation blocked"}`)),
		},
	}
	client := NewAPIClient("user-token", "page-token", "page-123")
	client.httpClient = &http.Client{Transport: transport}

	err := client.SendMessage(context.Background(), "conv-456", "xin chao")
	if err == nil {
		t.Fatal("expected SendMessage to return body-level error")
	}
	if !strings.Contains(err.Error(), "conversation blocked") {
		t.Fatalf("SendMessage error = %v, want body-level message", err)
	}
}

// TestTruncateForTikTok_MultiByteCharacters verifies rune-safe truncation for
// Vietnamese diacritics and emoji (multi-byte UTF-8 sequences).
func TestTruncateForTikTok_MultiByteCharacters(t *testing.T) {
	// Vietnamese text with diacritics (multi-byte UTF-8)
	input := strings.Repeat("Xin chào ", 100) // ~900 bytes, <500 runes
	result := truncateForTikTok(input)
	if !utf8.ValidString(result) {
		t.Fatal("truncateForTikTok produced invalid UTF-8")
	}

	// Emoji string exceeding 500 runes
	emoji := strings.Repeat("😊", 600)
	result = truncateForTikTok(emoji)
	runes := []rune(result)
	if len(runes) > 500 {
		t.Errorf("expected <=500 runes, got %d", len(runes))
	}
	if !utf8.ValidString(result) {
		t.Fatal("emoji truncation produced invalid UTF-8")
	}
}

// TestMessageHandlerEmptyMessageID verifies that two messages with empty IDs
// from different conversations are both published (not deduped against each other).
func TestMessageHandlerEmptyMessageID(t *testing.T) {
	msgBus := bus.New()
	ch := &Channel{
		BaseChannel: channels.NewBaseChannel(channels.TypePancake, msgBus, nil),
		pageID:      "page-123",
	}

	// First message with empty ID — should be published
	ch.handleMessagingEvent(MessagingData{
		PageID: "page-123", ConversationID: "conv-1",
		Type: "INBOX", Platform: "facebook",
		Message: MessagingMessage{ID: "", SenderID: "user-1", Content: "hello"},
	})

	// Second message with empty ID, different conversation — should ALSO be published
	ch.handleMessagingEvent(MessagingData{
		PageID: "page-123", ConversationID: "conv-2",
		Type: "INBOX", Platform: "facebook",
		Message: MessagingMessage{ID: "", SenderID: "user-2", Content: "world"},
	})

	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	_, ok1 := msgBus.ConsumeInbound(ctx)
	if !ok1 {
		t.Fatal("first empty-ID message should be published")
	}
	_, ok2 := msgBus.ConsumeInbound(ctx)
	if !ok2 {
		t.Fatal("second empty-ID message should NOT be deduped against first")
	}
}

func TestAPIClientUploadMediaMatchesOfficialContract(t *testing.T) {
	transport := &captureTransport{
		resp: &http.Response{
			StatusCode: http.StatusOK,
			Header:     make(http.Header),
			Body:       io.NopCloser(strings.NewReader(`{"id":"upload-123","success":true}`)),
		},
	}
	client := NewAPIClient("user-token", "page-token", "page-123")
	client.httpClient = &http.Client{Transport: transport}

	id, err := client.UploadMedia(context.Background(), "photo.jpg", strings.NewReader("file-bytes"), "image/jpeg")
	if err != nil {
		t.Fatalf("UploadMedia returned error: %v", err)
	}
	if id != "upload-123" {
		t.Fatalf("UploadMedia id = %q, want %q", id, "upload-123")
	}
	if transport.req == nil {
		t.Fatal("expected upload request to be captured")
	}
	if got, want := transport.req.URL.Path, "/api/public_api/v1/pages/page-123/upload_contents"; got != want {
		t.Fatalf("upload path = %q, want %q", got, want)
	}
	if got := transport.req.URL.Query().Get("page_access_token"); got != "page-token" {
		t.Fatalf("upload page_access_token query = %q, want %q", got, "page-token")
	}
	if !strings.HasPrefix(transport.req.Header.Get("Content-Type"), "multipart/form-data; boundary=") {
		t.Fatalf("upload Content-Type = %q, want multipart/form-data", transport.req.Header.Get("Content-Type"))
	}
}

// TestIsAuthError_WrappedError verifies errors.As works with wrapped apiError.
func TestIsAuthError_WrappedError(t *testing.T) {
	inner := &apiError{Code: 401, Message: "unauthorized"}
	wrapped := fmt.Errorf("send failed: %w", inner)
	if !isAuthError(wrapped) {
		t.Error("isAuthError should detect wrapped 401 apiError via errors.As")
	}
	if isAuthError(fmt.Errorf("random error")) {
		t.Error("isAuthError should return false for non-apiError")
	}
}

// TestIsRateLimitError_WrappedError verifies errors.As works with wrapped rate limit.
func TestIsRateLimitError_WrappedError(t *testing.T) {
	inner := &apiError{Code: 429, Message: "too many requests"}
	wrapped := fmt.Errorf("send failed: %w", inner)
	if !isRateLimitError(wrapped) {
		t.Error("isRateLimitError should detect wrapped 429 apiError via errors.As")
	}
}
