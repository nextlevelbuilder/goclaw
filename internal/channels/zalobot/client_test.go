package zalobot

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestClient_GetMe(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/bottest-token/getMe" {
			t.Errorf("unexpected path: %s", r.URL.Path)
			http.NotFound(w, r)
			return
		}
		if r.Method != http.MethodPost {
			t.Errorf("unexpected method: %s", r.Method)
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(APIResponse[BotUser]{
			Ok: true,
			Result: BotUser{
				ID:          "123456",
				AccountName: "TestBot",
				Username:    "test_bot",
			},
		})
	}))
	defer server.Close()

	client := NewClient("test-token", WithBaseURL(server.URL))
	bot, err := client.GetMe(context.Background())
	if err != nil {
		t.Fatalf("GetMe failed: %v", err)
	}

	if bot.ID != "123456" || bot.GetName() != "TestBot" || bot.Username != "test_bot" {
		t.Errorf("unexpected bot info: %+v", bot)
	}
}

func TestClient_GetUpdates(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/bottest-token/getUpdates" {
			t.Errorf("unexpected path: %s", r.URL.Path)
			http.NotFound(w, r)
			return
		}
		if r.Method != http.MethodPost {
			t.Errorf("unexpected method: %s", r.Method)
		}
		var req getUpdatesRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		if req.Timeout != 30 {
			t.Errorf("unexpected timeout: %+v", req)
		}

		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"ok": true,
			"result": map[string]any{
				"event_name": "message.text.received",
				"message": map[string]any{
					"message_id": "msg_1",
					"text":       "hello",
					"from": map[string]any{
						"id":           "user_1",
						"display_name": "User One",
					},
					"chat": map[string]any{
						"id":        "chat_1",
						"chat_type": "PRIVATE",
					},
				},
			},
		})
	}))
	defer server.Close()

	client := NewClient("test-token", WithBaseURL(server.URL))
	updates, err := client.GetUpdates(context.Background(), 30)
	if err != nil {
		t.Fatalf("GetUpdates failed: %v", err)
	}

	if len(updates) != 1 || updates[0].Message.Text != "hello" {
		t.Errorf("unexpected updates: %+v", updates)
	}
}
func TestClient_GetUpdates_Timeout(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/bottest-token/getUpdates" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"ok":          false,
			"description": "Request timeout",
		})
	}))
	defer server.Close()

	client := NewClient("test-token", WithBaseURL(server.URL))
	updates, err := client.GetUpdates(context.Background(), 30)
	if err != nil {
		t.Fatalf("expected nil error on timeout, got: %v", err)
	}
	if len(updates) != 0 {
		t.Errorf("expected 0 updates on timeout, got %d", len(updates))
	}
}


func TestClient_SendMessage(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/bottest-token/sendMessage" {
			t.Errorf("unexpected path: %s", r.URL.Path)
			http.NotFound(w, r)
			return
		}
		if r.Method != http.MethodPost {
			t.Errorf("unexpected method: %s", r.Method)
		}
		var req sendMessageRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		if req.ChatID != "chat_1" || req.Text != "hello world" {
			t.Errorf("unexpected send request: %+v", req)
		}

		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(APIResponse[SentMessageResult]{
			Ok: true,
			Result: SentMessageResult{
				MessageID: "msg_sent_1",
			},
		})
	}))
	defer server.Close()

	client := NewClient("test-token", WithBaseURL(server.URL))
	err := client.SendMessage(context.Background(), "chat_1", "hello world")
	if err != nil {
		t.Fatalf("SendMessage failed: %v", err)
	}
}

func TestClient_SendPhoto(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/bottest-token/sendPhoto" {
			t.Errorf("unexpected path: %s", r.URL.Path)
			http.NotFound(w, r)
			return
		}
		if r.Method != http.MethodPost {
			t.Errorf("unexpected method: %s", r.Method)
		}
		var req sendPhotoRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		if req.ChatID != "chat_1" || req.Photo != "https://example.com/img.jpg" || req.Caption != "nice photo" {
			t.Errorf("unexpected sendPhoto request: %+v", req)
		}

		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(APIResponse[SentMessageResult]{
			Ok: true,
			Result: SentMessageResult{
				MessageID: "msg_sent_photo_1",
			},
		})
	}))
	defer server.Close()

	client := NewClient("test-token", WithBaseURL(server.URL))
	err := client.SendPhoto(context.Background(), "chat_1", "https://example.com/img.jpg", "nice photo")
	if err != nil {
		t.Fatalf("SendPhoto failed: %v", err)
	}
}
