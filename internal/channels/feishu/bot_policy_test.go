package feishu

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/nextlevelbuilder/goclaw/internal/bus"
	"github.com/nextlevelbuilder/goclaw/internal/config"
	"github.com/nextlevelbuilder/goclaw/internal/store"
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

type feishuPolicyPairingStore struct {
	requests     int
	lastSenderID string
	lastChannel  string
	lastChatID   string
	paired       map[string]bool
}

func (s *feishuPolicyPairingStore) RequestPairing(_ context.Context, senderID, channel, chatID, _ string, _ map[string]string) (string, error) {
	s.requests++
	s.lastSenderID = senderID
	s.lastChannel = channel
	s.lastChatID = chatID
	return "PAIR1234", nil
}

func (s *feishuPolicyPairingStore) IsPaired(_ context.Context, senderID, channel string) (bool, error) {
	return s.paired[senderID+"|"+channel], nil
}

func (s *feishuPolicyPairingStore) ApprovePairing(context.Context, string, string) (*store.PairedDeviceData, error) {
	return nil, errors.New("not implemented")
}
func (s *feishuPolicyPairingStore) DenyPairing(context.Context, string) error { return nil }
func (s *feishuPolicyPairingStore) RevokePairing(context.Context, string, string) error {
	return nil
}
func (s *feishuPolicyPairingStore) ListPending(context.Context) []store.PairingRequestData {
	return nil
}
func (s *feishuPolicyPairingStore) ListPaired(context.Context) []store.PairedDeviceData {
	return nil
}
func (s *feishuPolicyPairingStore) MigrateGroupChatID(context.Context, string, string, string) error {
	return nil
}

func TestHandleMessageEvent_GroupPairingRequiresTargetBotMention(t *testing.T) {
	requireMention := true
	ps := &feishuPolicyPairingStore{paired: map[string]bool{}}
	msgBus := bus.New()
	defer msgBus.Close()

	ch, err := New(config.FeishuConfig{
		AppID:          "app",
		AppSecret:      "secret",
		GroupPolicy:    "pairing",
		RequireMention: &requireMention,
	}, msgBus, ps, nil, nil)
	if err != nil {
		t.Fatalf("New feishu channel: %v", err)
	}
	ch.SetName("feishu-test")
	ch.botOpenID = "ou_target_bot"
	srv := newSimpleMockServer(t, `{"code":0,"msg":"ok","data":{"message_id":"om_sent"}}`)
	ch.client = NewLarkClient("app", "secret", srv.URL)

	event := feishuGroupTextEvent(t, "om_non_target", "ou_alice", "oc_group", "@_user_1 help", []EventMention{
		feishuMention("@_user_1", "ou_other_bot", "OtherBot"),
	})
	ch.handleMessageEvent(context.Background(), event)

	if ps.requests != 0 {
		t.Fatalf("pairing requests = %d, want 0 for non-target mention", ps.requests)
	}
	assertNoFeishuInbound(t, msgBus)
}

func TestHandleMessageEvent_GroupSkipsExplicitOtherMentionEvenWhenRequireMentionDisabled(t *testing.T) {
	requireMention := false
	ps := &feishuPolicyPairingStore{paired: map[string]bool{}}
	msgBus := bus.New()
	defer msgBus.Close()

	ch, err := New(config.FeishuConfig{
		AppID:          "app",
		AppSecret:      "secret",
		GroupPolicy:    "pairing",
		RequireMention: &requireMention,
	}, msgBus, ps, nil, nil)
	if err != nil {
		t.Fatalf("New feishu channel: %v", err)
	}
	ch.SetName("lark-cppai-pm")
	ch.botOpenID = "ou_0b6fac6e84cf8c773a0c2768147799e2"
	srv := newSimpleMockServer(t, `{"code":0,"msg":"ok","data":{"message_id":"om_sent"}}`)
	ch.client = NewLarkClient("app", "secret", srv.URL)

	event := feishuGroupTextEvent(t, "om_other_bot_target", "ou_alice", "oc_group", "@_user_1 help", []EventMention{
		feishuMention("@_user_1", "ou_3ef6f00429b40780fb7ffc4a972fd835", "itsddvnm"),
	})
	ch.handleMessageEvent(context.Background(), event)

	if ps.requests != 0 {
		t.Fatalf("pairing requests = %d, want 0 when explicit mention targets another bot", ps.requests)
	}
	assertNoFeishuInbound(t, msgBus)
}

func TestHandleMessageEvent_GroupPairingRequestsOnlyWhenTargetMentioned(t *testing.T) {
	requireMention := true
	ps := &feishuPolicyPairingStore{paired: map[string]bool{}}
	msgBus := bus.New()
	defer msgBus.Close()

	ch, err := New(config.FeishuConfig{
		AppID:          "app",
		AppSecret:      "secret",
		GroupPolicy:    "pairing",
		RequireMention: &requireMention,
	}, msgBus, ps, nil, nil)
	if err != nil {
		t.Fatalf("New feishu channel: %v", err)
	}
	ch.SetName("feishu-test")
	ch.botOpenID = "ou_target_bot"
	srv := newSimpleMockServer(t, `{"code":0,"msg":"ok","data":{"message_id":"om_sent"}}`)
	ch.client = NewLarkClient("app", "secret", srv.URL)

	event := feishuGroupTextEvent(t, "om_target", "ou_alice", "oc_group", "@_user_1 help", []EventMention{
		feishuMention("@_user_1", "ou_target_bot", "TargetBot"),
	})
	ch.handleMessageEvent(context.Background(), event)

	if ps.requests != 1 {
		t.Fatalf("pairing requests = %d, want 1", ps.requests)
	}
	if ps.lastSenderID != "group:oc_group" {
		t.Fatalf("pairing senderID = %q, want group:oc_group", ps.lastSenderID)
	}
	if ps.lastChannel != "feishu-test" || ps.lastChatID != "oc_group" {
		t.Fatalf("unexpected pairing request channel/chat: %s/%s", ps.lastChannel, ps.lastChatID)
	}
	assertNoFeishuInbound(t, msgBus)
}

func TestHandleMessageEvent_GroupPairingAllowsAlreadyPairedTargetMention(t *testing.T) {
	requireMention := true
	ps := &feishuPolicyPairingStore{
		paired: map[string]bool{"group:oc_group|feishu-test": true},
	}
	msgBus := bus.New()
	defer msgBus.Close()

	ch, err := New(config.FeishuConfig{
		AppID:          "app",
		AppSecret:      "secret",
		GroupPolicy:    "pairing",
		RequireMention: &requireMention,
	}, msgBus, ps, nil, nil)
	if err != nil {
		t.Fatalf("New feishu channel: %v", err)
	}
	ch.SetName("feishu-test")
	ch.botOpenID = "ou_target_bot"
	srv := newSimpleMockServer(t, `{"code":0,"msg":"ok","data":{"name":"Alice"}}`)
	ch.client = NewLarkClient("app", "secret", srv.URL)

	event := feishuGroupTextEvent(t, "om_paired_target", "ou_alice", "oc_group", "@_user_1 help", []EventMention{
		feishuMention("@_user_1", "ou_target_bot", "TargetBot"),
	})
	ch.handleMessageEvent(context.Background(), event)

	if ps.requests != 0 {
		t.Fatalf("pairing requests = %d, want 0 for already paired group", ps.requests)
	}
	msg := assertFeishuInbound(t, msgBus)
	if msg.Channel != "feishu-test" || msg.ChatID != "oc_group" || msg.PeerKind != "group" {
		t.Fatalf("unexpected inbound route: channel=%q chat=%q peer=%q", msg.Channel, msg.ChatID, msg.PeerKind)
	}
}

func feishuGroupTextEvent(t *testing.T, messageID, senderID, chatID, text string, mentions []EventMention) *MessageEvent {
	t.Helper()
	content, err := json.Marshal(map[string]string{"text": text})
	if err != nil {
		t.Fatalf("marshal text content: %v", err)
	}

	ev := &MessageEvent{}
	ev.Event.Message.MessageID = messageID
	ev.Event.Message.ChatID = chatID
	ev.Event.Message.ChatType = "group"
	ev.Event.Message.MessageType = "text"
	ev.Event.Message.Content = string(content)
	ev.Event.Message.Mentions = mentions
	ev.Event.Sender.SenderID.OpenID = senderID
	return ev
}

func feishuMention(key, openID, name string) EventMention {
	return EventMention{
		Key: key,
		ID: struct {
			OpenID  string `json:"open_id"`
			UserID  string `json:"user_id"`
			UnionID string `json:"union_id"`
		}{OpenID: openID},
		Name: name,
	}
}

func assertFeishuInbound(t *testing.T, msgBus *bus.MessageBus) bus.InboundMessage {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	msg, ok := msgBus.ConsumeInbound(ctx)
	if !ok {
		t.Fatal("expected inbound message")
	}
	return msg
}

func assertNoFeishuInbound(t *testing.T, msgBus *bus.MessageBus) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	if msg, ok := msgBus.ConsumeInbound(ctx); ok {
		t.Fatalf("unexpected inbound message: %+v", msg)
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
