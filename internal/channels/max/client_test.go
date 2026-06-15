package max

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// =====================================================================
// Test helpers
// =====================================================================

// loadFixture reads a JSON test fixture and returns its bytes.
func loadFixture(t *testing.T, name string) []byte {
	t.Helper()
	path := filepath.Join("testdata", name)
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read fixture %s: %v", path, err)
	}
	return data
}

// newTestClient builds a Client pointing at the given test server.
// Sets a small retry count and disables exponential backoff via short delays.
func newTestClient(t *testing.T, srv *httptest.Server) *Client {
	t.Helper()
	return NewClient(
		"test-token",
		WithBaseURL(srv.URL),
		WithMaxRetries(1),
		WithHTTPClient(&http.Client{Timeout: 5 * time.Second}),
	)
}

// =====================================================================
// GET /me
// =====================================================================

func TestClient_GetMe_Success(t *testing.T) {
	body := loadFixture(t, "me_response.json")

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Verify auth header — Max requires raw token (no Bearer).
		if got := r.Header.Get("Authorization"); got != "test-token" {
			t.Errorf("Authorization header = %q, want %q", got, "test-token")
		}
		if r.Method != http.MethodGet {
			t.Errorf("method = %s, want GET", r.Method)
		}
		if r.URL.Path != "/me" {
			t.Errorf("path = %s, want /me", r.URL.Path)
		}
		// API version must be present in query.
		if r.URL.Query().Get("v") == "" {
			t.Error("v query param missing")
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(body)
	}))
	defer srv.Close()

	client := newTestClient(t, srv)
	me, err := client.GetMe(context.Background())
	if err != nil {
		t.Fatalf("GetMe: %v", err)
	}

	if me.UserID != 256747471 {
		t.Errorf("UserID = %d, want 256747471", me.UserID)
	}
	if me.Username != "id772879874571_bot" {
		t.Errorf("Username = %q", me.Username)
	}
	if !me.IsBot {
		t.Error("IsBot = false, want true")
	}
	if me.FirstName != "3-С" {
		t.Errorf("FirstName = %q, want %q", me.FirstName, "3-С")
	}
}

func TestClient_GetMe_Unauthorized(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = io.WriteString(w, `{"code":"unauthorized","message":"invalid token"}`)
	}))
	defer srv.Close()

	client := newTestClient(t, srv)
	_, err := client.GetMe(context.Background())
	if err == nil {
		t.Fatal("expected error on 401")
	}

	var apiErr *APIError
	if !errors.As(err, &apiErr) {
		t.Fatalf("expected *APIError, got %T: %v", err, err)
	}
	if apiErr.Code != "unauthorized" {
		t.Errorf("Code = %q, want unauthorized", apiErr.Code)
	}
}

func TestClient_GetMe_NetworkError(t *testing.T) {
	// Point client at a closed server to force connection refused.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	srv.Close()

	client := newTestClient(t, srv)
	_, err := client.GetMe(context.Background())
	if err == nil {
		t.Fatal("expected network error")
	}
	if !strings.Contains(err.Error(), "http") {
		t.Errorf("expected http-prefixed error, got: %v", err)
	}
}

// =====================================================================
// GET /updates
// =====================================================================

func TestClient_GetUpdates_NoMarker(t *testing.T) {
	body := loadFixture(t, "update_dm_text.json")

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		if q.Get("marker") != "" {
			t.Errorf("marker should be absent on first call, got %q", q.Get("marker"))
		}
		if q.Get("limit") != "100" {
			t.Errorf("limit = %q, want 100", q.Get("limit"))
		}
		if q.Get("timeout") != "30" {
			t.Errorf("timeout = %q, want 30", q.Get("timeout"))
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(body)
	}))
	defer srv.Close()

	client := newTestClient(t, srv)
	resp, err := client.GetUpdates(context.Background(), GetUpdatesParams{
		Limit:   100,
		Timeout: 30,
	})
	if err != nil {
		t.Fatalf("GetUpdates: %v", err)
	}

	if len(resp.Updates) != 1 {
		t.Fatalf("got %d updates, want 1", len(resp.Updates))
	}
	if resp.Marker == nil {
		t.Fatal("marker is nil")
	}
	if *resp.Marker != 35807261 {
		t.Errorf("marker = %d, want 35807261", *resp.Marker)
	}

	u := resp.Updates[0]
	if u.UpdateType != UpdateTypeMessageCreated {
		t.Errorf("UpdateType = %q", u.UpdateType)
	}
	if u.Message == nil {
		t.Fatal("Message nil")
	}
	if u.Message.Body == nil {
		t.Fatal("Message.Body nil — JSON tag mismatch?")
	}
	if u.Message.Body.Text != "тест 123" {
		t.Errorf("Text = %q, want %q", u.Message.Body.Text, "тест 123")
	}
}

func TestClient_GetUpdates_WithMarker(t *testing.T) {
	body := loadFixture(t, "updates_empty.json")

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		if q.Get("marker") != "12345" {
			t.Errorf("marker = %q, want 12345", q.Get("marker"))
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(body)
	}))
	defer srv.Close()

	marker := int64(12345)
	client := newTestClient(t, srv)
	_, err := client.GetUpdates(context.Background(), GetUpdatesParams{Marker: &marker})
	if err != nil {
		t.Fatalf("GetUpdates: %v", err)
	}
}

func TestClient_GetUpdates_WithTypes(t *testing.T) {
	body := loadFixture(t, "updates_empty.json")

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got := r.URL.Query().Get("types")
		want := "message_created,message_callback"
		if got != want {
			t.Errorf("types = %q, want %q", got, want)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(body)
	}))
	defer srv.Close()

	client := newTestClient(t, srv)
	_, err := client.GetUpdates(context.Background(), GetUpdatesParams{
		Types: []string{"message_created", "message_callback"},
	})
	if err != nil {
		t.Fatalf("GetUpdates: %v", err)
	}
}

func TestClient_GetUpdates_RateLimit_Retries(t *testing.T) {
	body := loadFixture(t, "updates_empty.json")
	var calls int32

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n := atomic.AddInt32(&calls, 1)
		if n == 1 {
			w.WriteHeader(http.StatusTooManyRequests)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(body)
	}))
	defer srv.Close()

	client := NewClient("t",
		WithBaseURL(srv.URL),
		WithMaxRetries(2),
		WithHTTPClient(&http.Client{Timeout: 5 * time.Second}),
	)

	// Use a long-enough context — first attempt fails, retry sleeps 1s.
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	_, err := client.GetUpdates(ctx, GetUpdatesParams{})
	if err != nil {
		t.Fatalf("GetUpdates after retry: %v", err)
	}
	if got := atomic.LoadInt32(&calls); got != 2 {
		t.Errorf("server calls = %d, want 2 (initial + retry)", got)
	}
}

func TestClient_GetUpdates_ContextCancel(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Slow handler — will outlast the context.
		select {
		case <-r.Context().Done():
			return
		case <-time.After(2 * time.Second):
		}
	}))
	defer srv.Close()

	client := newTestClient(t, srv)
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	_, err := client.GetUpdates(ctx, GetUpdatesParams{})
	if err == nil {
		t.Fatal("expected context cancellation error")
	}
}

// =====================================================================
// POST /messages
// =====================================================================

func TestClient_SendMessage_DM(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("method = %s, want POST", r.Method)
		}
		if r.URL.Path != "/messages" {
			t.Errorf("path = %s", r.URL.Path)
		}
		q := r.URL.Query()
		if q.Get("user_id") != "12345" {
			t.Errorf("user_id = %q", q.Get("user_id"))
		}
		if q.Get("chat_id") != "" {
			t.Errorf("chat_id should not be set for DM")
		}

		var body SendMessageRequest
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatalf("decode body: %v", err)
		}
		if body.Text != "hello" {
			t.Errorf("text = %q", body.Text)
		}

		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{
			"message":{"timestamp":111,"message":{"mid":"mid.x","seq":1,"text":"hello"}},
			"chat_id":888,
			"recipient_id":888,
			"message_id":"mid.x"
		}`)
	}))
	defer srv.Close()

	client := newTestClient(t, srv)
	resp, err := client.SendMessage(context.Background(), SendMessageParams{
		UserID: 12345,
		Body:   SendMessageRequest{Text: "hello"},
	})
	if err != nil {
		t.Fatalf("SendMessage: %v", err)
	}
	if resp.Message.Body.Text != "hello" {
		t.Errorf("response text = %q", resp.Message.Body.Text)
	}
	if resp.MessageID != "mid.x" {
		t.Errorf("MessageID = %q, want mid.x", resp.MessageID)
	}
	if resp.ChatID != 888 {
		t.Errorf("ChatID = %d, want 888", resp.ChatID)
	}
}

func TestClient_SendMessage_Group(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		if q.Get("chat_id") != "999" {
			t.Errorf("chat_id = %q, want 999", q.Get("chat_id"))
		}
		if q.Get("user_id") != "" {
			t.Errorf("user_id should not be set for group")
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"message":{"timestamp":1,"message":{"mid":"x","seq":1}},"message_id":"x"}`)
	}))
	defer srv.Close()

	client := newTestClient(t, srv)
	resp, err := client.SendMessage(context.Background(), SendMessageParams{
		ChatID: 999,
		Body:   SendMessageRequest{Text: "hi"},
	})
	if err != nil {
		t.Fatalf("SendMessage: %v", err)
	}
	if resp.MessageID != "x" {
		t.Errorf("MessageID = %q", resp.MessageID)
	}
}

func TestClient_SendMessage_RejectsBothIDs(t *testing.T) {
	client := NewClient("t")
	_, err := client.SendMessage(context.Background(), SendMessageParams{
		UserID: 1,
		ChatID: 2,
	})
	if err == nil {
		t.Fatal("expected error when both UserID and ChatID set")
	}
}

func TestClient_SendMessage_RejectsNoIDs(t *testing.T) {
	client := NewClient("t")
	_, err := client.SendMessage(context.Background(), SendMessageParams{})
	if err == nil {
		t.Fatal("expected error when neither UserID nor ChatID set")
	}
}

// =====================================================================
// PUT /messages
// =====================================================================

func TestClient_EditMessage(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPut {
			t.Errorf("method = %s, want PUT", r.Method)
		}
		if r.URL.Query().Get("message_id") != "mid.123" {
			t.Errorf("message_id = %q", r.URL.Query().Get("message_id"))
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"message":{"timestamp":1,"message":{"mid":"mid.123","seq":1,"text":"updated"}},"message_id":"mid.123"}`)
	}))
	defer srv.Close()

	client := newTestClient(t, srv)
	resp, err := client.EditMessage(context.Background(), EditMessageParams{
		MessageID: "mid.123",
		Body:      EditMessageRequest{Text: "updated"},
	})
	if err != nil {
		t.Fatalf("EditMessage: %v", err)
	}
	if resp.Message.Body.Text != "updated" {
		t.Errorf("text = %q", resp.Message.Body.Text)
	}
}

func TestClient_EditMessage_RejectsEmptyID(t *testing.T) {
	client := NewClient("t")
	_, err := client.EditMessage(context.Background(), EditMessageParams{})
	if err == nil {
		t.Fatal("expected error on empty MessageID")
	}
}

// =====================================================================
// POST /chats/{id}/actions
// =====================================================================

func TestClient_PostAction(t *testing.T) {
	var bodyReceived map[string]string
	var pathReceived string
	var mu sync.Mutex

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		defer mu.Unlock()
		pathReceived = r.URL.Path
		_ = json.NewDecoder(r.Body).Decode(&bodyReceived)
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	client := newTestClient(t, srv)
	if err := client.PostAction(context.Background(), 7777, "typing_on"); err != nil {
		t.Fatalf("PostAction: %v", err)
	}

	mu.Lock()
	defer mu.Unlock()
	if pathReceived != "/chats/7777/actions" {
		t.Errorf("path = %q", pathReceived)
	}
	if bodyReceived["action"] != "typing_on" {
		t.Errorf("body action = %q", bodyReceived["action"])
	}
}

// =====================================================================
// POST /answers (callbacks)
// =====================================================================

func TestClient_AnswerCallback(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("callback_id") != "cb_x" {
			t.Errorf("callback_id = %q", r.URL.Query().Get("callback_id"))
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	client := newTestClient(t, srv)
	if err := client.AnswerCallback(context.Background(), "cb_x", "OK", nil); err != nil {
		t.Fatalf("AnswerCallback: %v", err)
	}
}

// =====================================================================
// Subscriptions
// =====================================================================

func TestClient_SubscribeWebhook(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("method = %s", r.Method)
		}
		if r.URL.Path != "/subscriptions" {
			t.Errorf("path = %q", r.URL.Path)
		}
		var body SubscriptionRequest
		_ = json.NewDecoder(r.Body).Decode(&body)
		if body.URL != "https://example.com/hook" {
			t.Errorf("URL = %q", body.URL)
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	client := newTestClient(t, srv)
	err := client.SubscribeWebhook(context.Background(), "https://example.com/hook",
		[]string{"message_created"})
	if err != nil {
		t.Fatalf("SubscribeWebhook: %v", err)
	}
}

func TestClient_UnsubscribeWebhook(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodDelete {
			t.Errorf("method = %s", r.Method)
		}
		if r.URL.Query().Get("url") != "https://example.com/hook" {
			t.Errorf("url query = %q", r.URL.Query().Get("url"))
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	client := newTestClient(t, srv)
	err := client.UnsubscribeWebhook(context.Background(), "https://example.com/hook")
	if err != nil {
		t.Fatalf("UnsubscribeWebhook: %v", err)
	}
}
