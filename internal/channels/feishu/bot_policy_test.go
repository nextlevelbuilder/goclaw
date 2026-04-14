package feishu

import (
	"context"
	"testing"
)

// --- isInGroupAllowList ---

func TestIsInGroupAllowList_Match(t *testing.T) {
	ch := &Channel{groupAllowList: []string{"ou_user_1", "ou_user_2"}}
	if !ch.isInGroupAllowList("ou_user_1") {
		t.Error("ou_user_1 should be in allowlist")
	}
}

func TestIsInGroupAllowList_AtPrefix(t *testing.T) {
	// Allow entries may have "@" prefix stripped
	ch := &Channel{groupAllowList: []string{"@ou_user_3"}}
	if !ch.isInGroupAllowList("ou_user_3") {
		t.Error("ou_user_3 (with @ prefix stripped) should match")
	}
}

func TestIsInGroupAllowList_NoMatch(t *testing.T) {
	ch := &Channel{groupAllowList: []string{"ou_user_1"}}
	if ch.isInGroupAllowList("ou_stranger") {
		t.Error("ou_stranger should not be in allowlist")
	}
}

func TestIsInGroupAllowList_Empty(t *testing.T) {
	ch := &Channel{}
	if ch.isInGroupAllowList("ou_anyone") {
		t.Error("empty allowlist should never match")
	}
}

// --- checkGroupPolicy ---

func TestCheckGroupPolicy_Disabled(t *testing.T) {
	ch := &Channel{}
	ch.cfg.GroupPolicy = "disabled"
	if ch.checkGroupPolicy(context.Background(), "ou_user", "oc_chat") {
		t.Error("disabled policy should always return false")
	}
}

func TestCheckGroupPolicy_Open(t *testing.T) {
	ch := &Channel{}
	ch.cfg.GroupPolicy = "open"
	if !ch.checkGroupPolicy(context.Background(), "ou_user", "oc_chat") {
		t.Error("open policy should always return true")
	}
}

func TestCheckGroupPolicy_DefaultIsOpen(t *testing.T) {
	// Empty policy defaults to "open"
	ch := &Channel{}
	ch.cfg.GroupPolicy = ""
	if !ch.checkGroupPolicy(context.Background(), "ou_anyone", "oc_chat") {
		t.Error("empty policy should default to open and return true")
	}
}

func TestCheckGroupPolicy_Pairing_GroupAllowListBypasses(t *testing.T) {
	// Under "pairing" policy, groupAllowList is checked FIRST before BaseChannel,
	// so a matching entry returns true without requiring a BaseChannel.
	ch := &Channel{groupAllowList: []string{"ou_vip"}}
	ch.cfg.GroupPolicy = "pairing"
	if !ch.checkGroupPolicy(context.Background(), "ou_vip", "oc_chat") {
		t.Error("groupAllowList match should bypass pairing and return true")
	}
}

// --- webhookPath ---

func TestWebhookPath_Default(t *testing.T) {
	ch := &Channel{}
	got := ch.webhookPath()
	if got != defaultWebhookPath {
		t.Errorf("got %q, want %q", got, defaultWebhookPath)
	}
}

func TestWebhookPath_Custom(t *testing.T) {
	ch := &Channel{}
	ch.cfg.WebhookPath = "/custom/path"
	got := ch.webhookPath()
	if got != "/custom/path" {
		t.Errorf("got %q, want %q", got, "/custom/path")
	}
}

// --- WebhookHandler ---

func TestWebhookHandler_NonWebhookMode_ReturnsNil(t *testing.T) {
	ch := &Channel{}
	ch.cfg.ConnectionMode = "websocket"
	path, handler := ch.WebhookHandler()
	if path != "" || handler != nil {
		t.Error("WebhookHandler should return ('', nil) for non-webhook mode")
	}
}

func TestWebhookHandler_WebhookWithPort_ReturnsNil(t *testing.T) {
	ch := &Channel{}
	ch.cfg.ConnectionMode = "webhook"
	ch.cfg.WebhookPort = 3001
	path, handler := ch.WebhookHandler()
	if path != "" || handler != nil {
		t.Error("WebhookHandler should return ('', nil) when webhook_port > 0")
	}
}

// --- OnReactionEvent ---

func TestOnReactionEvent_Off(t *testing.T) {
	ch := &Channel{}
	ch.cfg.ReactionLevel = "off"
	// Should be a no-op — no panic, no error
	err := ch.OnReactionEvent(context.Background(), "oc_chat", "", "thinking")
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestOnReactionEvent_EmptyMessageID(t *testing.T) {
	ch := &Channel{}
	ch.cfg.ReactionLevel = "full"
	// Empty messageID → early return
	err := ch.OnReactionEvent(context.Background(), "oc_chat", "", "thinking")
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestOnReactionEvent_Minimal_NonTerminalIgnored(t *testing.T) {
	ch := &Channel{}
	ch.cfg.ReactionLevel = "minimal"
	// "thinking" is not terminal → should be ignored (no-op)
	err := ch.OnReactionEvent(context.Background(), "oc_chat", "om_msg1", "thinking")
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
}

// --- ClearReaction ---

func TestClearReaction_NoExistingReaction(t *testing.T) {
	ch := &Channel{}
	// No reaction stored → should be no-op
	err := ch.ClearReaction(context.Background(), "oc_chat_no_reaction", "")
	if err != nil {
		t.Errorf("ClearReaction with no reaction should succeed: %v", err)
	}
}

// --- sendChunkedText ---

func TestSendChunkedText_ShortText_SingleChunk(t *testing.T) {
	srv := newSimpleMockServer(t, `{"code":0,"msg":"ok","data":{"message_id":"om_chunk_1"}}`)

	ch := &Channel{client: NewLarkClient("app", "secret", srv.URL)}
	err := ch.sendChunkedText(context.Background(), "oc_chat", "chat_id", "short message", 4000, "")
	if err != nil {
		t.Fatalf("sendChunkedText: %v", err)
	}
}
