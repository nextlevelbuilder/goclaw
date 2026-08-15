package tools

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/nextlevelbuilder/goclaw/internal/channels"
)

type fakeTelegramManager struct {
	channel string
	req     channels.TelegramManagerRequest
}

func (f *fakeTelegramManager) manage(ctx context.Context, channel string, req channels.TelegramManagerRequest) (channels.TelegramManagerResult, error) {
	f.channel = channel
	f.req = req
	return channels.TelegramManagerResult{
		Action: req.Action,
		Result: map[string]any{
			"chat_id":           req.ChatID,
			"message_thread_id": req.MessageThreadID,
			"name":              req.Name,
		},
	}, nil
}

func TestTelegramManagerToolUsesCurrentChannelAndChat(t *testing.T) {
	t.Parallel()
	fake := &fakeTelegramManager{}
	tool := NewTelegramManagerTool()
	tool.SetTelegramManager(fake.manage)

	ctx := WithTelegramManagerPermissions(
		WithToolChannelType(
			WithToolChatID(WithToolChannel(context.Background(), "telegram-main"), "-100123"),
			"telegram",
		),
		[]string{"topic"},
	)
	res := tool.Execute(ctx, map[string]any{
		"action":            "topic.create",
		"name":              "Daily Ops",
		"message_thread_id": 42,
	})

	if res == nil || res.IsError {
		t.Fatalf("Execute() error = %#v", res)
	}
	if fake.channel != "telegram-main" {
		t.Fatalf("channel = %q, want current channel", fake.channel)
	}
	if fake.req.ChatID != "-100123" {
		t.Fatalf("chat_id = %q, want current chat", fake.req.ChatID)
	}
	if fake.req.Action != "topic.create" || fake.req.Name != "Daily Ops" || fake.req.MessageThreadID != 42 {
		t.Fatalf("request = %#v", fake.req)
	}

	var payload map[string]any
	if err := json.Unmarshal([]byte(res.ForLLM), &payload); err != nil {
		t.Fatalf("result is not JSON: %v", err)
	}
	if payload["action"] != "topic.create" {
		t.Fatalf("result action = %#v", payload["action"])
	}
}

func TestTelegramManagerToolRejectsUnknownAction(t *testing.T) {
	t.Parallel()
	tool := NewTelegramManagerTool()
	tool.SetTelegramManager(func(context.Context, string, channels.TelegramManagerRequest) (channels.TelegramManagerResult, error) {
		t.Fatal("manager should not be called for unknown actions")
		return channels.TelegramManagerResult{}, nil
	})

	res := tool.Execute(context.Background(), map[string]any{"action": "raw.call"})
	if res == nil || !res.IsError {
		t.Fatalf("Execute() should reject unknown action, got %#v", res)
	}
}

func TestTelegramManagerToolRequiresTelegramChannelPermission(t *testing.T) {
	t.Parallel()
	tool := NewTelegramManagerTool()
	tool.SetTelegramManager(func(context.Context, string, channels.TelegramManagerRequest) (channels.TelegramManagerResult, error) {
		t.Fatal("manager should not be called without Telegram manager permission")
		return channels.TelegramManagerResult{}, nil
	})

	ctx := WithToolChatID(WithToolChannel(context.Background(), "telegram-main"), "-100123")
	res := tool.Execute(ctx, map[string]any{"action": "topic.create", "name": "Daily Ops"})
	if res == nil || !res.IsError {
		t.Fatalf("Execute() should reject without Telegram channel context, got %#v", res)
	}

	ctx = WithToolChannelType(ctx, "telegram")
	res = tool.Execute(ctx, map[string]any{"action": "topic.create", "name": "Daily Ops"})
	if res == nil || !res.IsError {
		t.Fatalf("Execute() should reject without Telegram manager permissions, got %#v", res)
	}
}

func TestTelegramManagerToolRejectsDisallowedPermissionGroup(t *testing.T) {
	t.Parallel()
	tool := NewTelegramManagerTool()
	tool.SetTelegramManager(func(context.Context, string, channels.TelegramManagerRequest) (channels.TelegramManagerResult, error) {
		t.Fatal("manager should not be called for a disallowed permission group")
		return channels.TelegramManagerResult{}, nil
	})

	ctx := WithTelegramManagerPermissions(
		WithToolChannelType(
			WithToolChatID(WithToolChannel(context.Background(), "telegram-main"), "-100123"),
			"telegram",
		),
		[]string{"topic"},
	)
	res := tool.Execute(ctx, map[string]any{"action": "message.delete", "message_id": 12})
	if res == nil || !res.IsError {
		t.Fatalf("Execute() should reject message action when only topic is allowed, got %#v", res)
	}
}

func TestTelegramManagerToolRequiresManager(t *testing.T) {
	t.Parallel()
	tool := NewTelegramManagerTool()
	ctx := WithTelegramManagerPermissions(
		WithToolChannelType(
			WithToolChatID(WithToolChannel(context.Background(), "telegram-main"), "-100123"),
			"telegram",
		),
		[]string{"chat"},
	)

	res := tool.Execute(ctx, map[string]any{"action": "chat.get"})
	if res == nil || !res.IsError {
		t.Fatalf("Execute() should fail without manager, got %#v", res)
	}
}
