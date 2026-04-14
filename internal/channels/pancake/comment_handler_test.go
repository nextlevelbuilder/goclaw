package pancake

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/nextlevelbuilder/goclaw/internal/bus"
)

// newTestChannel builds a minimal Channel for handler tests.
// apiSrv may be nil when API calls are not expected.
func newTestChannel(t *testing.T, pageID string, cfg pancakeInstanceConfig) (*Channel, *bus.MessageBus) {
	t.Helper()
	msgBus := bus.New()
	cfg.PageID = pageID
	creds := pancakeCreds{
		APIKey:          "test-key",
		PageAccessToken: "test-token",
	}
	ch, err := New(cfg, creds, msgBus, nil)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	ch.pageName = "Test Page"
	ch.platform = "facebook"
	ch.stopCtx = context.Background()
	return ch, msgBus
}

func newTestChannelWithSrv(t *testing.T, pageID string, cfg pancakeInstanceConfig, srv *httptest.Server) (*Channel, *bus.MessageBus) {
	t.Helper()
	ch, msgBus := newTestChannel(t, pageID, cfg)
	if srv != nil {
		ch.apiClient.pageV2BaseURL = srv.URL
		ch.apiClient.httpClient = srv.Client()
		ch.postFetcher = NewPostFetcher(ch.apiClient, "")
		ch.postFetcher.stopCtx = context.Background()
	}
	return ch, msgBus
}

func commentEvent(pageID, convID, senderID, msgID, content string) MessagingData {
	return MessagingData{
		PageID:         pageID,
		ConversationID: convID,
		Type:           "COMMENT",
		Platform:       "facebook",
		Message: MessagingMessage{
			ID:         msgID,
			SenderID:   senderID,
			SenderName: "Test User",
			Content:    content,
		},
	}
}

// consumeInbound drains one message from the bus with a short timeout.
func consumeInbound(t *testing.T, msgBus *bus.MessageBus, timeout time.Duration) (bus.InboundMessage, bool) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	return msgBus.ConsumeInbound(ctx)
}

// --- Feature Gate ---

func TestHandleCommentEvent_FeatureGated(t *testing.T) {
	cfg := pancakeInstanceConfig{}
	cfg.Features.CommentReply = false
	ch, msgBus := newTestChannel(t, "page-1", cfg)

	ch.handleCommentEvent(commentEvent("page-1", "conv-1", "user-1", "msg-1", "hello"))

	_, ok := consumeInbound(t, msgBus, 50*time.Millisecond)
	if ok {
		t.Error("expected no message published when CommentReply is disabled")
	}
}

func TestHandleCommentEvent_FeatureEnabled(t *testing.T) {
	cfg := pancakeInstanceConfig{}
	cfg.Features.CommentReply = true
	ch, msgBus := newTestChannel(t, "page-1", cfg)

	ch.handleCommentEvent(commentEvent("page-1", "conv-1", "user-1", "msg-1", "hello"))

	msg, ok := consumeInbound(t, msgBus, 100*time.Millisecond)
	if !ok {
		t.Fatal("expected message published when CommentReply is enabled")
	}
	if msg.Metadata["pancake_mode"] != "comment" {
		t.Errorf("pancake_mode = %q, want %q", msg.Metadata["pancake_mode"], "comment")
	}
}

// --- Self-Reply Prevention ---

func TestHandleCommentEvent_SkipsSelfReply(t *testing.T) {
	cfg := pancakeInstanceConfig{}
	cfg.Features.CommentReply = true
	ch, msgBus := newTestChannel(t, "page-1", cfg)

	// senderID == pageID: must be skipped without panic
	ch.handleCommentEvent(commentEvent("page-1", "conv-1", "page-1", "msg-self", "own reply"))

	_, ok := consumeInbound(t, msgBus, 50*time.Millisecond)
	if ok {
		t.Error("expected self-reply to be dropped")
	}
}

func TestHandleCommentEvent_SkipsAssignedStaff(t *testing.T) {
	cfg := pancakeInstanceConfig{}
	cfg.Features.CommentReply = true
	ch, msgBus := newTestChannel(t, "page-1", cfg)

	data := commentEvent("page-1", "conv-1", "staff-1", "msg-staff", "staff comment")
	data.AssigneeIDs = []string{"staff-1"}
	ch.handleCommentEvent(data)

	_, ok := consumeInbound(t, msgBus, 50*time.Millisecond)
	if ok {
		t.Error("expected assigned staff message to be dropped")
	}
}

// --- Dedup ---

func TestHandleCommentEvent_DedupDropsSecondCall(t *testing.T) {
	cfg := pancakeInstanceConfig{}
	cfg.Features.CommentReply = true
	ch, msgBus := newTestChannel(t, "page-1", cfg)

	evt := commentEvent("page-1", "conv-1", "user-1", "msg-dup", "hello")
	ch.handleCommentEvent(evt)
	ch.handleCommentEvent(evt)

	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	_, ok1 := msgBus.ConsumeInbound(ctx)
	if !ok1 {
		t.Fatal("expected first message to be published")
	}
	_, ok2 := consumeInbound(t, msgBus, 30*time.Millisecond)
	if ok2 {
		t.Error("expected second duplicate to be dropped")
	}
}

func TestHandleCommentEvent_EmptyMsgIDNotDeduped(t *testing.T) {
	cfg := pancakeInstanceConfig{}
	cfg.Features.CommentReply = true
	ch, msgBus := newTestChannel(t, "page-1", cfg)

	ch.handleCommentEvent(commentEvent("page-1", "conv-1", "user-1", "", "msg A"))
	ch.handleCommentEvent(commentEvent("page-1", "conv-1", "user-1", "", "msg B"))

	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	_, ok1 := msgBus.ConsumeInbound(ctx)
	if !ok1 {
		t.Fatal("first empty-ID message should be published")
	}
	_, ok2 := msgBus.ConsumeInbound(ctx)
	if !ok2 {
		t.Fatal("second empty-ID message should NOT be deduped against first")
	}
}

// --- Comment Filter ---

func TestFilterComment_AllMode(t *testing.T) {
	cfg := pancakeInstanceConfig{}
	cfg.CommentReplyOptions.Filter = "all"
	ch, _ := newTestChannel(t, "page-1", cfg)

	if !ch.filterComment("random text") {
		t.Error("filter=all should pass all comments")
	}
}

func TestFilterComment_KeywordMatch(t *testing.T) {
	cfg := pancakeInstanceConfig{}
	cfg.CommentReplyOptions.Filter = "keyword"
	cfg.CommentReplyOptions.Keywords = []string{"price", "buy"}
	ch, _ := newTestChannel(t, "page-1", cfg)

	if !ch.filterComment("what is the price?") {
		t.Error("expected 'price' keyword to match")
	}
	if !ch.filterComment("I want to buy this") {
		t.Error("expected 'buy' keyword to match")
	}
	if ch.filterComment("nice photo") {
		t.Error("expected 'nice photo' to be filtered out")
	}
}

func TestFilterComment_KeywordCaseInsensitive(t *testing.T) {
	cfg := pancakeInstanceConfig{}
	cfg.CommentReplyOptions.Filter = "keyword"
	cfg.CommentReplyOptions.Keywords = []string{"Price"}
	ch, _ := newTestChannel(t, "page-1", cfg)

	if !ch.filterComment("what is the PRICE?") {
		t.Error("keyword match should be case-insensitive")
	}
}

func TestFilterComment_EmptyFilter(t *testing.T) {
	cfg := pancakeInstanceConfig{}
	cfg.CommentReplyOptions.Filter = ""
	ch, _ := newTestChannel(t, "page-1", cfg)

	if !ch.filterComment("anything") {
		t.Error("empty filter should default to all (pass everything)")
	}
}

// --- Content Enrichment ---

func TestBuildEnrichedContent_WithPostContext(t *testing.T) {
	cfg := pancakeInstanceConfig{}
	cfg.Features.CommentReply = true
	cfg.CommentReplyOptions.IncludePostContext = true

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]any{
			"data": []PancakePost{{ID: "post-abc", Message: "Hello world post"}},
		})
	}))
	defer srv.Close()

	ch, _ := newTestChannelWithSrv(t, "page-1", cfg, srv)

	data := commentEvent("page-1", "conv-1", "user-1", "msg-1", "question about this")
	data.PostID = "post-abc"
	content := ch.buildCommentContent(data)

	if !contains(content, "[Bai dang]") {
		t.Errorf("expected [Bai dang] prefix in enriched content, got: %s", content)
	}
	if !contains(content, "Hello world post") {
		t.Errorf("expected post message in enriched content, got: %s", content)
	}
	if !contains(content, "[Comment moi]") {
		t.Errorf("expected [Comment moi] in enriched content, got: %s", content)
	}
	if !contains(content, "question about this") {
		t.Errorf("expected comment text in enriched content, got: %s", content)
	}
}

func TestBuildEnrichedContent_WithoutPostContext(t *testing.T) {
	cfg := pancakeInstanceConfig{}
	cfg.CommentReplyOptions.IncludePostContext = false
	ch, _ := newTestChannel(t, "page-1", cfg)

	data := commentEvent("page-1", "conv-1", "user-1", "msg-1", "just a comment")
	data.PostID = "post-abc"
	content := ch.buildCommentContent(data)

	if contains(content, "[Bai dang]") {
		t.Errorf("should not include post context when IncludePostContext=false, got: %s", content)
	}
	if !contains(content, "just a comment") {
		t.Errorf("expected comment text in output, got: %s", content)
	}
}

func TestBuildEnrichedContent_PostFetchFails(t *testing.T) {
	cfg := pancakeInstanceConfig{}
	cfg.CommentReplyOptions.IncludePostContext = true

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()

	ch, _ := newTestChannelWithSrv(t, "page-1", cfg, srv)

	data := commentEvent("page-1", "conv-1", "user-1", "msg-1", "comment text")
	data.PostID = "post-fail"
	content := ch.buildCommentContent(data)

	// Should fall back to comment text only — no panic, no error propagated.
	if content == "" {
		t.Error("expected non-empty content on post fetch failure")
	}
	if contains(content, "[Bai dang]") {
		t.Errorf("should not include post prefix when fetch fails, got: %s", content)
	}
}

// --- Metadata ---

func TestHandleCommentEvent_MetadataFields(t *testing.T) {
	cfg := pancakeInstanceConfig{}
	cfg.Features.CommentReply = true
	ch, msgBus := newTestChannel(t, "page-1", cfg)
	ch.platform = "facebook"

	data := MessagingData{
		PageID:         "page-1",
		ConversationID: "conv-abc",
		Type:           "COMMENT",
		Platform:       "facebook",
		Message: MessagingMessage{
			ID:         "msg-xyz",
			SenderID:   "user-1",
			SenderName: "John Doe",
			Content:    "test comment",
		},
	}
	ch.handleCommentEvent(data)

	msg, ok := consumeInbound(t, msgBus, 100*time.Millisecond)
	if !ok {
		t.Fatal("expected message published")
	}

	checks := map[string]string{
		"pancake_mode":        "comment",
		"conversation_type":   "COMMENT",
		"reply_to_comment_id": "msg-xyz",
		"sender_id":           "user-1",
		"platform":            "facebook",
		"page_name":           "Test Page",
		"conversation_id":     "conv-abc",
	}
	for key, want := range checks {
		if got := msg.Metadata[key]; got != want {
			t.Errorf("metadata[%q] = %q, want %q", key, got, want)
		}
	}
}

// --- Session Key ---

func TestHandleCommentEvent_ChatIDIsConversationID(t *testing.T) {
	cfg := pancakeInstanceConfig{}
	cfg.Features.CommentReply = true
	ch, msgBus := newTestChannel(t, "page-1", cfg)

	ch.handleCommentEvent(commentEvent("page-1", "conv-distinct", "user-1", "msg-1", "hello"))

	msg, ok := consumeInbound(t, msgBus, 100*time.Millisecond)
	if !ok {
		t.Fatal("expected message published")
	}
	if msg.ChatID != "conv-distinct" {
		t.Errorf("ChatID = %q, want %q", msg.ChatID, "conv-distinct")
	}
}

// contains is a helper to check if a string contains a substring.
func contains(s, substr string) bool {
	return strings.Contains(s, substr)
}
