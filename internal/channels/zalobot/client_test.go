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
		if r.URL.Path != "/bot/getMe" {
			t.Errorf("unexpected path: %s", r.URL.Path)
			http.NotFound(w, r)
			return
		}
		if token := r.Header.Get("bot-token"); token != "test-token" {
			t.Errorf("unexpected bot-token header: %s", token)
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(APIResponse[BotUser]{
			Ok: true,
			Result: BotUser{
				ID:       "123456",
				Name:     "TestBot",
				Username: "test_bot",
			},
		})
	}))
	defer server.Close()

	client := NewClient("test-token", WithBaseURL(server.URL))
	bot, err := client.GetMe(context.Background())
	if err != nil {
		t.Fatalf("GetMe failed: %v", err)
	}

	if bot.ID != "123456" || bot.Name != "TestBot" || bot.Username != "test_bot" {
		t.Errorf("unexpected bot info: %+v", bot)
	}
}

func TestClient_GetUpdates(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/bot/getUpdates" {
			t.Errorf("unexpected path: %s", r.URL.Path)
			http.NotFound(w, r)
			return
		}
		var req getUpdatesRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		if req.Offset != 100 || req.Timeout != 30 {
			t.Errorf("unexpected request: %+v", req)
		}

		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(APIResponse[[]Update]{
			Ok: true,
			Result: []Update{
				{
					UpdateID: 101,
					Message: &Message{
						MessageID: "msg_1",
						Text:      "hello",
						From: User{
							ID:   "user_1",
							Name: "User One",
						},
						Chat: Chat{
							ID:       "chat_1",
							ChatType: "DIRECT",
						},
					},
				},
			},
		})
	}))
	defer server.Close()

	client := NewClient("test-token", WithBaseURL(server.URL))
	updates, err := client.GetUpdates(context.Background(), 100, 50, 30)
	if err != nil {
		t.Fatalf("GetUpdates failed: %v", err)
	}

	if len(updates) != 1 || updates[0].UpdateID != 101 || updates[0].Message.Text != "hello" {
		t.Errorf("unexpected updates: %+v", updates)
	}
}

func TestClient_SendMessage(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/bot/sendMessage" {
			t.Errorf("unexpected path: %s", r.URL.Path)
			http.NotFound(w, r)
			return
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
		if r.URL.Path != "/bot/sendPhoto" {
			t.Errorf("unexpected path: %s", r.URL.Path)
			http.NotFound(w, r)
			return
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
