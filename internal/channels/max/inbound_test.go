package max

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/nextlevelbuilder/goclaw/internal/bus"
)

// =====================================================================
// Helpers — build a Channel that publishes to a captured bus + drain helper
// =====================================================================

// newTestChannel constructs a Channel suitable for translator tests.
// Returns the channel and a drain function that returns published inbound messages.
func newTestChannel(t *testing.T, botID int64, botUsername string, requireMention bool) (*Channel, func() []bus.InboundMessage) {
	t.Helper()
	msgBus := bus.New()

	creds := instanceCreds{
		BotToken: "test",
		BotID:    botID,
		Username: botUsername,
	}
	cfg := instanceConfig{
		Mode:           "polling",
		PollingTimeout: 30,
		DMPolicy:       "open",
		GroupPolicy:    "open",
		HistoryLimit:   50,
	}

	c, err := New("test-max", creds, cfg, msgBus, nil, nil, nil)
	if err != nil {
		t.Fatalf("New channel: %v", err)
	}
	c.SetRequireMention(requireMention)
	// Sprint 10: handleMessage tests use synchronous drain() — disable
	// the aggregator so each Push falls through to dispatchMessage
	// directly. The aggregator has its own unit tests separately.
	c.aggregator = nil

	// Drain function: consume all currently-buffered inbound messages.
	drain := func() []bus.InboundMessage {
		var collected []bus.InboundMessage
		ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
		defer cancel()
		for {
			msg, ok := msgBus.ConsumeInbound(ctx)
			if !ok {
				return collected
			}
			collected = append(collected, msg)
		}
	}
	return c, drain
}

// loadUpdate parses a single Update from a fixture's first updates[] entry.
func loadUpdate(t *testing.T, fixtureName string) Update {
	t.Helper()
	data := loadFixture(t, fixtureName)
	var resp UpdatesResponse
	if err := json.Unmarshal(data, &resp); err != nil {
		t.Fatalf("unmarshal fixture %s: %v", fixtureName, err)
	}
	if len(resp.Updates) == 0 {
		t.Fatalf("fixture %s has no updates", fixtureName)
	}
	return resp.Updates[0]
}

// =====================================================================
// Translator: DM text
// =====================================================================

func TestInbound_DMText(t *testing.T) {
	c, drain := newTestChannel(t, 256747471, "id772879874571_bot", false)
	u := loadUpdate(t, "update_dm_text.json")

	c.handleMessage(context.Background(), *u.Message, false)

	msgs := drain()
	if len(msgs) != 1 {
		t.Fatalf("expected 1 inbound message, got %d", len(msgs))
	}
	m := msgs[0]
	if m.SenderID != "74757262" {
		t.Errorf("SenderID = %q, want 74757262", m.SenderID)
	}
	// DM chatID = recipient.chat_id (dialog thread ID)
	if m.ChatID != "188289857" {
		t.Errorf("ChatID = %q, want 188289857 (dialog thread ID)", m.ChatID)
	}
	if m.Content != "тест 123" {
		t.Errorf("Content = %q, want %q", m.Content, "тест 123")
	}
	if m.PeerKind != "direct" {
		t.Errorf("PeerKind = %q, want direct", m.PeerKind)
	}
	if m.Channel != "test-max" {
		t.Errorf("Channel = %q", m.Channel)
	}
	if m.Metadata["message_id"] != "mid.000000000b391341019df70b03786130" {
		t.Errorf("Metadata.message_id = %q", m.Metadata["message_id"])
	}
	if m.Metadata["timestamp"] == "" {
		t.Error("Metadata.timestamp empty")
	}
}

// =====================================================================
// Translator: DM with image attachment
// =====================================================================

func TestInbound_DMWithImage(t *testing.T) {
	c, drain := newTestChannel(t, 256747471, "id772879874571_bot", false)
	u := loadUpdate(t, "update_dm_with_image.json")

	c.handleMessage(context.Background(), *u.Message, false)

	msgs := drain()
	if len(msgs) != 1 {
		t.Fatalf("expected 1 inbound message (text empty + media), got %d", len(msgs))
	}
	m := msgs[0]
	// Image attachment, empty text — should still pass through.
	if m.Content != "" {
		t.Errorf("Content = %q, expected empty string for image-only message", m.Content)
	}
	// Day 2: media download is a stub — paths empty.
	if len(m.Media) != 0 {
		t.Errorf("Media files = %d, expected 0 (Day 4 implements download)", len(m.Media))
	}
	if m.PeerKind != "direct" {
		t.Errorf("PeerKind = %q", m.PeerKind)
	}
}

// =====================================================================
// Translator: Group with mention
// =====================================================================

func TestInbound_GroupWithMention(t *testing.T) {
	c, drain := newTestChannel(t, 256747471, "id772879874571_bot", true)
	u := loadUpdate(t, "update_group_with_mention.json")

	c.handleMessage(context.Background(), *u.Message, false)

	msgs := drain()
	if len(msgs) != 1 {
		t.Fatalf("expected 1 inbound message (mention triggers gate), got %d", len(msgs))
	}
	m := msgs[0]
	if m.PeerKind != "group" {
		t.Errorf("PeerKind = %q, want group", m.PeerKind)
	}
	if m.ChatID != "999888777" {
		t.Errorf("ChatID = %q, want 999888777", m.ChatID)
	}
	// Mention should be stripped from content.
	if m.Content == "@id772879874571_bot привет, как дела?" {
		t.Errorf("mention not stripped: Content = %q", m.Content)
	}
	if m.Content != "привет, как дела?" {
		t.Errorf("Content = %q, want 'привет, как дела?' (mention stripped)", m.Content)
	}
}

// =====================================================================
// Translator: Group without mention — should be filtered
// =====================================================================

func TestInbound_GroupNoMention_Filtered(t *testing.T) {
	c, drain := newTestChannel(t, 256747471, "id772879874571_bot", true)
	u := loadUpdate(t, "update_group_no_mention.json")

	c.handleMessage(context.Background(), *u.Message, false)

	msgs := drain()
	if len(msgs) != 0 {
		t.Fatalf("expected 0 messages (mention gate), got %d: %+v", len(msgs), msgs)
	}
}

// =====================================================================
// Translator: Group without mention but RequireMention=false → passes
// =====================================================================

func TestInbound_GroupNoMention_NoGate_Passes(t *testing.T) {
	c, drain := newTestChannel(t, 256747471, "id772879874571_bot", false)
	u := loadUpdate(t, "update_group_no_mention.json")

	c.handleMessage(context.Background(), *u.Message, false)

	msgs := drain()
	if len(msgs) != 1 {
		t.Fatalf("expected 1 message when require_mention=false, got %d", len(msgs))
	}
}

// =====================================================================
// Translator: Self-loop guard
// =====================================================================

func TestInbound_SelfLoop_Skipped(t *testing.T) {
	c, drain := newTestChannel(t, 256747471, "id772879874571_bot", false)
	u := loadUpdate(t, "update_dm_text.json")

	// Mutate sender to be the bot itself.
	u.Message.Sender.UserID = 256747471

	c.handleMessage(context.Background(), *u.Message, false)

	msgs := drain()
	if len(msgs) != 0 {
		t.Fatalf("expected 0 messages (self-loop), got %d", len(msgs))
	}
}

// =====================================================================
// Translator: Empty message (no text, no attachments) is dropped
// =====================================================================

func TestInbound_EmptyMessage_Dropped(t *testing.T) {
	c, drain := newTestChannel(t, 256747471, "id772879874571_bot", false)

	msg := Message{
		Sender:    &User{UserID: 74757262},
		Recipient: &Recipient{ChatID: 188289857, ChatType: "dialog"},
		Body:      &MessageBody{Text: ""},
	}
	c.handleMessage(context.Background(), msg, false)

	msgs := drain()
	if len(msgs) != 0 {
		t.Fatalf("expected 0 messages (empty), got %d", len(msgs))
	}
}

// =====================================================================
// Translator: Missing required fields → silent skip
// =====================================================================

func TestInbound_MalformedMessage_Skipped(t *testing.T) {
	c, drain := newTestChannel(t, 256747471, "id772879874571_bot", false)

	tests := []struct {
		name string
		msg  Message
	}{
		{name: "no sender", msg: Message{Recipient: &Recipient{}, Body: &MessageBody{}}},
		{name: "no recipient", msg: Message{Sender: &User{}, Body: &MessageBody{}}},
		{name: "no body", msg: Message{Sender: &User{}, Recipient: &Recipient{}}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c.handleMessage(context.Background(), tt.msg, false)
			if msgs := drain(); len(msgs) != 0 {
				t.Errorf("expected 0 messages, got %d", len(msgs))
			}
		})
	}
}

// =====================================================================
// detectMention — markup-based and textual
// =====================================================================

func TestDetectMention_MarkupBased(t *testing.T) {
	c, _ := newTestChannel(t, 256747471, "id772879874571_bot", true)

	msg := Message{
		Body: &MessageBody{
			Text: "@id772879874571_bot привет",
			Markup: []Markup{
				{Type: "user_mention", From: 0, Length: 21, UserID: 256747471},
			},
		},
	}
	if !c.detectMention(msg) {
		t.Error("expected mention via markup, got false")
	}
}

func TestDetectMention_TextualFallback(t *testing.T) {
	c, _ := newTestChannel(t, 256747471, "id772879874571_bot", true)

	msg := Message{
		Body: &MessageBody{Text: "hey @id772879874571_bot are you here?"},
	}
	if !c.detectMention(msg) {
		t.Error("expected mention via @username text, got false")
	}
}

func TestDetectMention_CaseInsensitive(t *testing.T) {
	c, _ := newTestChannel(t, 256747471, "TestBot", true)

	msg := Message{Body: &MessageBody{Text: "hey @testbot"}}
	if !c.detectMention(msg) {
		t.Error("expected case-insensitive match")
	}
}

func TestDetectMention_NotPresent(t *testing.T) {
	c, _ := newTestChannel(t, 256747471, "id772879874571_bot", true)

	msg := Message{Body: &MessageBody{Text: "no mention here"}}
	if c.detectMention(msg) {
		t.Error("expected no mention, got true")
	}
}

func TestDetectMention_DifferentBotID(t *testing.T) {
	c, _ := newTestChannel(t, 256747471, "id772879874571_bot", true)

	msg := Message{
		Body: &MessageBody{
			Text: "@otherbot hello",
			Markup: []Markup{
				{Type: "user_mention", From: 0, Length: 9, UserID: 99999999},
			},
		},
	}
	if c.detectMention(msg) {
		t.Error("expected no match for different bot ID")
	}
}

// =====================================================================
// stripBotMention
// =====================================================================

func TestStripBotMention(t *testing.T) {
	tests := []struct {
		name     string
		text     string
		username string
		want     string
	}{
		{"prefix mention", "@bot hello world", "bot", "hello world"},
		{"middle mention", "say @bot now", "bot", "say   now"},
		{"no mention", "no mention here", "bot", "no mention here"},
		{"empty username noop", "@bot text", "", "@bot text"},
		{"case insensitive", "@BoT hi", "bot", "hi"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := stripBotMention(tt.text, tt.username, 0)
			if got != tt.want {
				t.Errorf("stripBotMention(%q, %q) = %q, want %q",
					tt.text, tt.username, got, tt.want)
			}
		})
	}
}

// =====================================================================
// IsDialog — discriminator logic
// =====================================================================

func TestRecipient_IsDialog(t *testing.T) {
	tests := []struct {
		name string
		r    Recipient
		want bool
	}{
		{"explicit dialog", Recipient{ChatType: "dialog", UserID: 1, ChatID: 2}, true},
		{"explicit chat", Recipient{ChatType: "chat", ChatID: 3}, false},
		{"empty type with both ids", Recipient{UserID: 1, ChatID: 2}, false},
		{"empty type chat only", Recipient{ChatID: 3}, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.r.IsDialog(); got != tt.want {
				t.Errorf("IsDialog() = %v, want %v (recipient: %+v)", got, tt.want, tt.r)
			}
		})
	}
}

// =====================================================================
// chatIDFromUpdate — extracts chat ID from various update shapes
// =====================================================================

func TestChatIDFromUpdate(t *testing.T) {
	tests := []struct {
		name string
		u    Update
		want int64
	}{
		{
			name: "from message recipient",
			u:    Update{Message: &Message{Recipient: &Recipient{ChatID: 111}}},
			want: 111,
		},
		{
			name: "from chat field",
			u:    Update{Chat: &Chat{ChatID: 222}},
			want: 222,
		},
		{
			name: "from chat_id field",
			u:    Update{ChatID: 333},
			want: 333,
		},
		{
			name: "no chat id",
			u:    Update{},
			want: 0,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := chatIDFromUpdate(tt.u); got != tt.want {
				t.Errorf("chatIDFromUpdate = %d, want %d", got, tt.want)
			}
		})
	}
}

// =====================================================================
// buildMetadata
// =====================================================================

func TestBuildMetadata_Basic(t *testing.T) {
	msg := Message{
		Timestamp: 1777967484119,
		Body:      &MessageBody{MID: "mid.x", Seq: 100},
	}
	md := buildMetadata(msg, false)
	if md["message_id"] != "mid.x" {
		t.Errorf("message_id = %q", md["message_id"])
	}
	if md["timestamp"] != "1777967484119" {
		t.Errorf("timestamp = %q", md["timestamp"])
	}
	if md["edited"] != "" {
		t.Errorf("edited = %q, want empty for non-edited", md["edited"])
	}
}

func TestBuildMetadata_Edited(t *testing.T) {
	msg := Message{Body: &MessageBody{MID: "mid.x"}}
	md := buildMetadata(msg, true)
	if md["edited"] != "true" {
		t.Errorf("edited = %q, want true", md["edited"])
	}
}

func TestBuildMetadata_LinkedReply(t *testing.T) {
	msg := Message{
		Body: &MessageBody{MID: "mid.x"},
		Link: &LinkedMessage{
			Type:    "reply",
			Sender:  &User{UserID: 999},
			Message: &MessageBody{MID: "mid.parent", Text: "parent text"},
		},
	}
	md := buildMetadata(msg, false)
	if md["link_type"] != "reply" {
		t.Errorf("link_type = %q", md["link_type"])
	}
	if md["link_sender_id"] != "999" {
		t.Errorf("link_sender_id = %q", md["link_sender_id"])
	}
	if md["link_message_id"] != "mid.parent" {
		t.Errorf("link_message_id = %q", md["link_message_id"])
	}
	if md["link_message_text"] != "parent text" {
		t.Errorf("link_message_text = %q", md["link_message_text"])
	}
}

// =====================================================================
// Marker management
// =====================================================================

func TestMarkerSetGet(t *testing.T) {
	c, _ := newTestChannel(t, 0, "", false)

	if got := c.getMarker(); got != nil {
		t.Errorf("initial marker = %v, want nil", got)
	}

	val := int64(12345)
	c.setMarker(&val)

	got := c.getMarker()
	if got == nil {
		t.Fatal("marker is nil after setMarker")
	}
	if *got != 12345 {
		t.Errorf("marker = %d, want 12345", *got)
	}

	// setMarker with nil clears.
	c.setMarker(nil)
	if got := c.getMarker(); got != nil {
		t.Errorf("marker = %v after clear, want nil", got)
	}
}

func TestMarker_GetReturnsCopy(t *testing.T) {
	c, _ := newTestChannel(t, 0, "", false)

	val := int64(999)
	c.setMarker(&val)

	got1 := c.getMarker()
	*got1 = 7777 // mutate the returned pointer

	got2 := c.getMarker()
	if *got2 != 999 {
		t.Errorf("internal marker corrupted by external mutation: got %d", *got2)
	}
}
