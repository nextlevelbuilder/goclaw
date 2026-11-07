package zalobot

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/nextlevelbuilder/goclaw/internal/bus"
	"github.com/nextlevelbuilder/goclaw/internal/config"
)

func TestChannel_Send_TextChunking(t *testing.T) {
	t.Parallel()

	var sentMessages int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/bottest-token/sendMessage" {
			atomic.AddInt32(&sentMessages, 1)
			var req sendMessageRequest
			_ = json.NewDecoder(r.Body).Decode(&req)
			if len(req.Text) > 2000 {
				t.Errorf("chunk exceeds max 2000 characters: len=%d", len(req.Text))
			}
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(APIResponse[SentMessageResult]{
				Ok: true,
				Result: SentMessageResult{
					MessageID: "msg_chunk",
				},
			})
			return
		}
		http.NotFound(w, r)
	}))
	defer server.Close()

	msgBus := bus.New()
	cfg := config.ZaloBotConfig{
		Enabled: true,
		Token:   "test-token",
	}

	ch, err := New(cfg, msgBus, nil)
	if err != nil {
		t.Fatalf("New failed: %v", err)
	}
	ch.client = NewClient("test-token", WithBaseURL(server.URL))

	longContent := strings.Repeat("A", 3500)
	outbound := bus.OutboundMessage{
		ChatID:  "chat_test_123",
		Content: longContent,
	}

	if err := ch.Send(context.Background(), outbound); err != nil {
		t.Fatalf("Send failed: %v", err)
	}

	if count := atomic.LoadInt32(&sentMessages); count < 2 {
		t.Errorf("expected at least 2 chunks sent, got %d", count)
	}
}

func TestChannel_Send_MediaPhoto(t *testing.T) {
	t.Parallel()

	var photoSent bool
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/bottest-token/sendPhoto" {
			photoSent = true
			var req sendPhotoRequest
			_ = json.NewDecoder(r.Body).Decode(&req)
			if req.Photo != "https://example.com/test.png" {
				t.Errorf("unexpected photo URL: %s", req.Photo)
			}
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(APIResponse[SentMessageResult]{
				Ok: true,
				Result: SentMessageResult{
					MessageID: "photo_sent",
				},
			})
			return
		}
		http.NotFound(w, r)
	}))
	defer server.Close()

	msgBus := bus.New()
	cfg := config.ZaloBotConfig{
		Enabled: true,
		Token:   "test-token",
	}

	ch, err := New(cfg, msgBus, nil)
	if err != nil {
		t.Fatalf("New failed: %v", err)
	}
	ch.client = NewClient("test-token", WithBaseURL(server.URL))

	outbound := bus.OutboundMessage{
		ChatID:  "chat_test_123",
		Content: "here is photo",
		Media: []bus.MediaAttachment{
			{
				URL:         "https://example.com/test.png",
				ContentType: "image/png",
			},
		},
	}

	if err := ch.Send(context.Background(), outbound); err != nil {
		t.Fatalf("Send failed: %v", err)
	}

	if !photoSent {
		t.Error("expected sendPhoto to be called")
	}
}

func TestChannel_StartStopLifecycle(t *testing.T) {
	t.Parallel()

	var pollCount int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/bottest-token/getMe" {
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(APIResponse[BotUser]{
				Ok: true,
				Result: BotUser{
					ID:       "bot_99",
					Name:     "LifecycleBot",
					Username: "lifecycle_bot",
				},
			})
			return
		}
		if r.URL.Path == "/bottest-token/getUpdates" {
			atomic.AddInt32(&pollCount, 1)
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(APIResponse[[]Update]{
				Ok:     true,
				Result: []Update{},
			})
			return
		}
		http.NotFound(w, r)
	}))
	defer server.Close()

	msgBus := bus.New()
	cfg := config.ZaloBotConfig{
		Enabled:        true,
		Token:          "test-token",
		PollTimeoutSec: 1,
	}

	ch, err := New(cfg, msgBus, nil)
	if err != nil {
		t.Fatalf("New failed: %v", err)
	}
	ch.client = NewClient("test-token", WithBaseURL(server.URL))

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	if err := ch.Start(ctx); err != nil {
		t.Fatalf("Start failed: %v", err)
	}

	// Give the loop time to run at least one poll
	time.Sleep(100 * time.Millisecond)

	if err := ch.Stop(context.Background()); err != nil {
		t.Fatalf("Stop failed: %v", err)
	}

	if count := atomic.LoadInt32(&pollCount); count < 1 {
		t.Errorf("expected at least 1 poll, got %d", count)
	}
}
