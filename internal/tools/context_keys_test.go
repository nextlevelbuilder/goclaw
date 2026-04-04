package tools

import (
	"context"
	"testing"
)

func TestToolContextKeys_Channel(t *testing.T) {
	ctx := context.Background()
	if v := ToolChannelFromCtx(ctx); v != "" {
		t.Errorf("expected empty, got %q", v)
	}

	ctx = WithToolChannel(ctx, "telegram")
	if v := ToolChannelFromCtx(ctx); v != "telegram" {
		t.Errorf("expected telegram, got %q", v)
	}
}

func TestToolContextKeys_ChatID(t *testing.T) {
	ctx := context.Background()
	if v := ToolChatIDFromCtx(ctx); v != "" {
		t.Errorf("expected empty, got %q", v)
	}

	ctx = WithToolChatID(ctx, "chat-123")
	if v := ToolChatIDFromCtx(ctx); v != "chat-123" {
		t.Errorf("expected chat-123, got %q", v)
	}
}

func TestToolContextKeys_PeerKind(t *testing.T) {
	ctx := context.Background()
	ctx = WithToolPeerKind(ctx, "group")
	if v := ToolPeerKindFromCtx(ctx); v != "group" {
		t.Errorf("expected group, got %q", v)
	}
}

func TestToolContextKeys_SandboxKey(t *testing.T) {
	ctx := context.Background()
	ctx = WithToolSandboxKey(ctx, "agent:main:telegram:direct:123")
	if v := ToolSandboxKeyFromCtx(ctx); v != "agent:main:telegram:direct:123" {
		t.Errorf("expected sandbox key, got %q", v)
	}
}

func TestToolContextKeys_AsyncCB(t *testing.T) {
	ctx := context.Background()
	if v := ToolAsyncCBFromCtx(ctx); v != nil {
		t.Error("expected nil callback")
	}

	called := false
	cb := AsyncCallback(func(ctx context.Context, result *Result) {
		called = true
	})

	ctx = WithToolAsyncCB(ctx, cb)
	got := ToolAsyncCBFromCtx(ctx)
	if got == nil {
		t.Fatal("expected non-nil callback")
	}
	got(ctx, nil)
	if !called {
		t.Error("callback was not invoked")
	}
}

func TestToolContextKeys_LeaderAgentID(t *testing.T) {
	ctx := context.Background()
	if v := LeaderAgentIDFromCtx(ctx); v != "" {
		t.Errorf("expected empty, got %q", v)
	}

	ctx = WithLeaderAgentID(ctx, "leader-uuid-123")
	if v := LeaderAgentIDFromCtx(ctx); v != "leader-uuid-123" {
		t.Errorf("expected leader-uuid-123, got %q", v)
	}
}

func TestToolContextKeys_MultipleValues(t *testing.T) {
	ctx := context.Background()
	ctx = WithToolChannel(ctx, "slack")
	ctx = WithToolChatID(ctx, "C123")
	ctx = WithToolPeerKind(ctx, "direct")
	ctx = WithToolSandboxKey(ctx, "sandbox-1")

	if v := ToolChannelFromCtx(ctx); v != "slack" {
		t.Errorf("channel: expected slack, got %q", v)
	}
	if v := ToolChatIDFromCtx(ctx); v != "C123" {
		t.Errorf("chatID: expected C123, got %q", v)
	}
	if v := ToolPeerKindFromCtx(ctx); v != "direct" {
		t.Errorf("peerKind: expected direct, got %q", v)
	}
	if v := ToolSandboxKeyFromCtx(ctx); v != "sandbox-1" {
		t.Errorf("sandboxKey: expected sandbox-1, got %q", v)
	}
}
