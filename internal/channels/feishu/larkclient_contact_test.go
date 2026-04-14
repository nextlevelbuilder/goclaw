package feishu

import (
	"context"
	"encoding/json"
	"testing"
)

// --- GetMessage ---

func TestGetMessage_Success(t *testing.T) {
	resp := map[string]any{
		"code": 0, "msg": "ok",
		"data": map[string]any{
			"items": []any{
				map[string]any{
					"message_id": "om_abc123",
					"msg_type":   "text",
					"body":       map[string]any{"content": `{"text":"hello"}`},
					"sender":     map[string]any{"id": "ou_sender", "id_type": "open_id", "sender_type": "user"},
				},
			},
		},
	}
	respJSON, _ := json.Marshal(resp)
	srv := newSimpleMockServer(t, string(respJSON))

	c := NewLarkClient("app", "secret", srv.URL)
	msg, err := c.GetMessage(context.Background(), "om_abc123")
	if err != nil {
		t.Fatalf("GetMessage error: %v", err)
	}
	if len(msg.Items) != 1 {
		t.Fatalf("expected 1 item, got %d", len(msg.Items))
	}
	if msg.Items[0].MessageID != "om_abc123" {
		t.Errorf("message_id: got %q", msg.Items[0].MessageID)
	}
}

func TestGetMessage_APIError(t *testing.T) {
	srv := newSimpleMockServer(t, `{"code":230002,"msg":"message not found","data":{}}`)

	c := NewLarkClient("app", "secret", srv.URL)
	_, err := c.GetMessage(context.Background(), "om_missing")
	if err == nil {
		t.Fatal("expected error for non-zero code")
	}
}

// --- GetUser ---

func TestGetUser_Success(t *testing.T) {
	resp := map[string]any{
		"code": 0, "msg": "ok",
		"data": map[string]any{
			"user": map[string]any{
				"name": "Alice Smith",
			},
		},
	}
	respJSON, _ := json.Marshal(resp)
	srv := newSimpleMockServer(t, string(respJSON))

	c := NewLarkClient("app", "secret", srv.URL)
	name, err := c.GetUser(context.Background(), "ou_user_123", "open_id")
	if err != nil {
		t.Fatalf("GetUser error: %v", err)
	}
	if name != "Alice Smith" {
		t.Errorf("name: got %q, want %q", name, "Alice Smith")
	}
}

func TestGetUser_APIError(t *testing.T) {
	srv := newSimpleMockServer(t, `{"code":50000,"msg":"user not found","data":{}}`)

	c := NewLarkClient("app", "secret", srv.URL)
	_, err := c.GetUser(context.Background(), "ou_missing", "open_id")
	if err == nil {
		t.Fatal("expected error for non-zero code")
	}
}
