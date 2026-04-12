package feishu

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/nextlevelbuilder/goclaw/internal/config"
)

// --- wsEventAdapter.HandleEvent ---

func TestWSEventAdapter_HandleEvent_InvalidJSON(t *testing.T) {
	ch := &Channel{}
	adapter := &wsEventAdapter{ch: ch}
	err := adapter.HandleEvent(context.Background(), []byte("not-json"))
	if err == nil {
		t.Fatal("expected error for invalid JSON payload")
	}
}

func TestWSEventAdapter_HandleEvent_NonMessageEvent_NoError(t *testing.T) {
	ch := &Channel{}
	adapter := &wsEventAdapter{ch: ch}
	payload := []byte(`{"schema":"2.0","header":{"event_type":"im.chat.member.bot.added_v1"}}`)
	err := adapter.HandleEvent(context.Background(), payload)
	if err != nil {
		t.Fatalf("non-message event should not error: %v", err)
	}
}

// --- probeBotInfo ---

func TestProbeBotInfo_Success(t *testing.T) {
	srv := newSimpleMockServer(t, `{"code":0,"msg":"ok","data":{"bot":{"open_id":"ou_bot_test"}}}`)
	ch := &Channel{client: NewLarkClient("app", "secret", srv.URL)}

	if err := ch.probeBotInfo(context.Background()); err != nil {
		t.Fatalf("probeBotInfo error: %v", err)
	}
	if ch.botOpenID != "ou_bot_test" {
		t.Errorf("botOpenID: got %q, want ou_bot_test", ch.botOpenID)
	}
}

func TestProbeBotInfo_EmptyOpenID(t *testing.T) {
	srv := newSimpleMockServer(t, `{"code":0,"msg":"ok","data":{"bot":{"open_id":""}}}`)
	ch := &Channel{client: NewLarkClient("app", "secret", srv.URL)}

	if err := ch.probeBotInfo(context.Background()); err == nil {
		t.Fatal("expected error for empty open_id")
	}
}

func TestProbeBotInfo_APIError(t *testing.T) {
	srv := newSimpleMockServer(t, `{"code":10001,"msg":"forbidden","data":{}}`)
	ch := &Channel{client: NewLarkClient("app", "secret", srv.URL)}

	if err := ch.probeBotInfo(context.Background()); err == nil {
		t.Fatal("expected error on API failure")
	}
}

// --- SetPendingCompaction / SetPendingHistoryTenantID ---
// Both use GroupHistory() which requires a real BaseChannel from New().

func TestSetPendingCompaction_WithChannel(t *testing.T) {
	ch := newLifecycleTestChannel(t)
	defer func() {
		if r := recover(); r != nil {
			t.Errorf("SetPendingCompaction panicked: %v", r)
		}
	}()
	ch.SetPendingCompaction(nil)
}

func TestSetPendingHistoryTenantID_WithChannel(t *testing.T) {
	ch := newLifecycleTestChannel(t)
	defer func() {
		if r := recover(); r != nil {
			t.Errorf("SetPendingHistoryTenantID panicked: %v", r)
		}
	}()
	ch.SetPendingHistoryTenantID(uuid.UUID{})
}

// --- ListGroupMembers ---

func TestListGroupMembers_Success(t *testing.T) {
	respJSON := `{"code":0,"msg":"ok","data":{"items":[{"member_id":"ou_A","name":"Alice"},{"member_id":"ou_B","name":"Bob"}],"has_more":false}}`
	srv := newSimpleMockServer(t, respJSON)
	ch := newLifecycleTestChannel(t)
	ch.client = NewLarkClient("app", "secret", srv.URL)

	members, err := ch.ListGroupMembers(context.Background(), "oc_chat_1")
	if err != nil {
		t.Fatalf("ListGroupMembers error: %v", err)
	}
	if len(members) != 2 {
		t.Errorf("expected 2 members, got %d", len(members))
	}
	if members[0].MemberID != "ou_A" {
		t.Errorf("first member: got %q", members[0].MemberID)
	}
}

func TestListGroupMembers_APIError(t *testing.T) {
	srv := newSimpleMockServer(t, `{"code":10001,"msg":"not found","data":{}}`)
	ch := newLifecycleTestChannel(t)
	ch.client = NewLarkClient("app", "secret", srv.URL)

	if _, err := ch.ListGroupMembers(context.Background(), "oc_missing"); err == nil {
		t.Fatal("expected error for API failure")
	}
}

// --- resolveSenderName / fetchSenderName ---

func TestResolveSenderName_EmptyOpenID(t *testing.T) {
	ch := &Channel{}
	if got := ch.resolveSenderName(context.Background(), ""); got != "" {
		t.Errorf("empty openID should return empty name, got %q", got)
	}
}

func TestResolveSenderName_CacheHit(t *testing.T) {
	ch := &Channel{}
	ch.senderCache.Store("ou_cached", &senderCacheEntry{
		name:      "Cached User",
		expiresAt: time.Now().Add(10 * time.Minute),
	})
	if got := ch.resolveSenderName(context.Background(), "ou_cached"); got != "Cached User" {
		t.Errorf("cache hit: got %q, want Cached User", got)
	}
}

func TestResolveSenderName_ExpiredCacheFetchesAgain(t *testing.T) {
	srv := newSimpleMockServer(t, `{"code":0,"msg":"ok","data":{"user":{"name":"Fresh Name"}}}`)
	ch := &Channel{client: NewLarkClient("app", "secret", srv.URL)}
	// Store an expired entry
	ch.senderCache.Store("ou_expired", &senderCacheEntry{
		name:      "Stale Name",
		expiresAt: time.Now().Add(-1 * time.Minute), // expired
	})
	got := ch.resolveSenderName(context.Background(), "ou_expired")
	if got != "Fresh Name" {
		t.Errorf("after cache expiry: got %q, want Fresh Name", got)
	}
}

func TestFetchSenderName_Success(t *testing.T) {
	srv := newSimpleMockServer(t, `{"code":0,"msg":"ok","data":{"user":{"name":"Bob Jones"}}}`)
	ch := &Channel{client: NewLarkClient("app", "secret", srv.URL)}

	if got := ch.fetchSenderName(context.Background(), "ou_bob"); got != "Bob Jones" {
		t.Errorf("fetchSenderName: got %q, want Bob Jones", got)
	}
}

func TestFetchSenderName_APIError_ReturnsEmpty(t *testing.T) {
	srv := newSimpleMockServer(t, `{"code":50000,"msg":"not found","data":{}}`)
	ch := &Channel{client: NewLarkClient("app", "secret", srv.URL)}

	if got := ch.fetchSenderName(context.Background(), "ou_missing"); got != "" {
		t.Errorf("fetchSenderName on error: got %q, want empty", got)
	}
}

// --- larkws sendPing with nil conn (no-op) ---

func TestLarkWSSendPing_NilConn(t *testing.T) {
	c := NewWSClient("app", "secret", "http://localhost", nil)
	c.mu.Lock()
	c.stopCh = make(chan struct{})
	c.mu.Unlock()
	defer func() {
		if r := recover(); r != nil {
			t.Errorf("sendPing with nil conn panicked: %v", r)
		}
	}()
	c.sendPing() // conn is nil → should be a no-op
}

// --- OnReactionEvent terminal states ---

func TestOnReactionEvent_Done_RemovesReaction(t *testing.T) {
	ch := &Channel{}
	ch.cfg.ReactionLevel = "full"
	// "done" with no stored reaction → removeTypingReaction → no-op, no error
	if err := ch.OnReactionEvent(context.Background(), "oc_chat", "om_msg", "done"); err != nil {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestOnReactionEvent_ErrorStatus(t *testing.T) {
	ch := &Channel{}
	ch.cfg.ReactionLevel = "full"
	if err := ch.OnReactionEvent(context.Background(), "oc_chat", "om_msg", "error"); err != nil {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestOnReactionEvent_Minimal_TerminalAllowed(t *testing.T) {
	ch := &Channel{}
	ch.cfg.ReactionLevel = "minimal"
	if err := ch.OnReactionEvent(context.Background(), "oc_chat", "om_msg", "done"); err != nil {
		t.Errorf("unexpected error: %v", err)
	}
}

// --- test helpers ---

// newLifecycleTestChannel creates a minimal valid Channel using New() so
// BaseChannel is fully initialised. Tests that need BaseChannel methods
// (GroupHistory, ContactCollector) should use this instead of &Channel{}.
// Named distinctly from newTestChannel in commands_writers_test.go.
func newLifecycleTestChannel(t *testing.T) *Channel {
	t.Helper()
	cfg := config.FeishuConfig{
		AppID:     "test-app-id",
		AppSecret: "test-app-secret",
	}
	ch, err := New(cfg, nil, nil, nil)
	if err != nil {
		t.Fatalf("newTestChannel: New() error: %v", err)
	}
	return ch
}
