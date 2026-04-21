package bitrix24

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/nextlevelbuilder/goclaw/internal/bus"
	"github.com/nextlevelbuilder/goclaw/internal/store"
)

// newHandleTestChannel builds a Channel ready to accept events without
// running Start(). Pre-populates botID so mention matching works.
func newHandleTestChannel(t *testing.T, botID int, requireMention bool) (*Channel, *bus.MessageBus) {
	t.Helper()
	fs := newFakeStore()
	tid := store.GenNewID()
	resetWebhookRouterForTest()

	mb := bus.New()
	fn := FactoryWithPortalStore(fs, "")
	cfg := json.RawMessage(`{"portal":"p","bot_code":"c","bot_name":"n","dm_policy":"open","group_policy":"open"}`)
	ch, err := fn("b1", nil, cfg, mb, nil)
	if err != nil {
		t.Fatalf("factory: %v", err)
	}
	bc := ch.(*Channel)
	bc.SetTenantID(tid)
	bc.SetRequireMention(requireMention)

	// Bypass Start — inject minimal state so handleMessage/DispatchEvent have
	// what they need (bot_id for mention regex, client for welcome message).
	bc.startMu.Lock()
	bc.botID = botID
	bc.client = NewClient("portal.bitrix24.com", nil)
	bc.startMu.Unlock()
	return bc, mb
}

func drainOne(mb *bus.MessageBus, timeout time.Duration) (bus.InboundMessage, bool) {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	return mb.ConsumeInbound(ctx)
}

func TestDispatchEvent_NilIsNoop(t *testing.T) {
	ch, _ := newHandleTestChannel(t, 1, false)
	defer resetWebhookRouterForTest()
	// Must not panic on nil event.
	ch.DispatchEvent(context.Background(), nil)
}

func TestDispatchEvent_UnknownTypeIgnored(t *testing.T) {
	ch, mb := newHandleTestChannel(t, 1, false)
	defer resetWebhookRouterForTest()

	ch.DispatchEvent(context.Background(), &Event{
		Type:   "ONIMBOTSOMETHINGNEW",
		Params: EventParams{FromUserID: "99", DialogID: "99", Message: "hi"},
	})
	if _, ok := drainOne(mb, 50*time.Millisecond); ok {
		t.Error("unknown event type should not publish")
	}
}

func TestHandleMessage_DMHappyPath_PublishesInbound(t *testing.T) {
	ch, mb := newHandleTestChannel(t, 101, false)
	defer resetWebhookRouterForTest()

	ch.DispatchEvent(context.Background(), &Event{
		Type: EventMessageAdd,
		Params: EventParams{
			FromUserID:  "42",
			DialogID:    "42",
			MessageID:   "m-1",
			MessageType: "private",
			Message:     "Xin chào",
		},
	})
	msg, ok := drainOne(mb, 500*time.Millisecond)
	if !ok {
		t.Fatal("expected an inbound message")
	}
	if msg.Content != "Xin chào" {
		t.Errorf("content = %q; want Xin chào", msg.Content)
	}
	if msg.PeerKind != "direct" {
		t.Errorf("PeerKind = %q; want direct", msg.PeerKind)
	}
	if msg.Metadata["bitrix_dialog_id"] != "42" {
		t.Errorf("missing/wrong bitrix_dialog_id: %v", msg.Metadata)
	}
	if msg.Metadata["bitrix_bot_id"] != "101" {
		t.Errorf("missing/wrong bitrix_bot_id: %v", msg.Metadata)
	}
	if msg.Metadata["bitrix_message_id"] != "m-1" {
		t.Errorf("missing/wrong bitrix_message_id: %v", msg.Metadata)
	}
}

func TestHandleMessage_SystemMessageSkipped(t *testing.T) {
	ch, mb := newHandleTestChannel(t, 101, false)
	defer resetWebhookRouterForTest()

	ch.DispatchEvent(context.Background(), &Event{
		Type: EventMessageAdd,
		Params: EventParams{
			FromUserID:    "42",
			DialogID:      "42",
			MessageType:   "private",
			Message:       "User X joined the chat",
			SystemMessage: true,
		},
	})
	if _, ok := drainOne(mb, 50*time.Millisecond); ok {
		t.Error("system messages must not trigger agent replies")
	}
}

func TestHandleMessage_EmptyFromUserIDSkipped(t *testing.T) {
	ch, mb := newHandleTestChannel(t, 101, false)
	defer resetWebhookRouterForTest()

	ch.DispatchEvent(context.Background(), &Event{
		Type: EventMessageAdd,
		Params: EventParams{
			FromUserID:  "",
			DialogID:    "42",
			MessageType: "private",
			Message:     "hi",
		},
	})
	if _, ok := drainOne(mb, 50*time.Millisecond); ok {
		t.Error("messages without FromUserID must be ignored")
	}
}

func TestHandleMessage_EmptyContentNoMediaSkipped(t *testing.T) {
	ch, mb := newHandleTestChannel(t, 101, false)
	defer resetWebhookRouterForTest()

	ch.DispatchEvent(context.Background(), &Event{
		Type: EventMessageAdd,
		Params: EventParams{
			FromUserID:  "42",
			DialogID:    "42",
			MessageType: "private",
			Message:     "   ",
		},
	})
	if _, ok := drainOne(mb, 50*time.Millisecond); ok {
		t.Error("empty content with no media must be dropped")
	}
}

func TestHandleMessage_GroupRequireMention_DropsWithoutMention(t *testing.T) {
	ch, mb := newHandleTestChannel(t, 101, true)
	defer resetWebhookRouterForTest()

	ch.DispatchEvent(context.Background(), &Event{
		Type: EventMessageAdd,
		Params: EventParams{
			FromUserID:  "42",
			DialogID:    "chat10",
			MessageType: "chat",
			Message:     "hey everyone just chatting",
		},
	})
	if _, ok := drainOne(mb, 50*time.Millisecond); ok {
		t.Error("group message without @mention must be dropped when RequireMention=true")
	}
}

func TestHandleMessage_GroupWithMention_Published(t *testing.T) {
	ch, mb := newHandleTestChannel(t, 101, true)
	defer resetWebhookRouterForTest()

	// Mention this bot (bot_id 101) → must strip the tag and publish body.
	ch.DispatchEvent(context.Background(), &Event{
		Type: EventMessageAdd,
		Params: EventParams{
			FromUserID:  "42",
			DialogID:    "chat10",
			MessageType: "chat",
			Message:     "[USER=101]Bot[/USER] what time is it?",
		},
	})
	msg, ok := drainOne(mb, 500*time.Millisecond)
	if !ok {
		t.Fatal("mentioned group message must publish")
	}
	if strings.Contains(msg.Content, "[USER=101]") {
		t.Errorf("mention not stripped: %q", msg.Content)
	}
	if !strings.Contains(msg.Content, "what time is it?") {
		t.Errorf("body stripped out: %q", msg.Content)
	}
	if msg.PeerKind != "group" {
		t.Errorf("PeerKind = %q; want group", msg.PeerKind)
	}
}

func TestIsMentioned_MatchesBOTVariant(t *testing.T) {
	ch, _ := newHandleTestChannel(t, 101, false)
	defer resetWebhookRouterForTest()

	if !ch.isMentioned("[BOT=101]Bot[/BOT] hello") {
		t.Error("[BOT=<id>] variant should also match")
	}
	if !ch.isMentioned("[USER=101]Bot[/USER] hi") {
		t.Error("[USER=<id>] variant should match")
	}
	if ch.isMentioned("[USER=999]Other[/USER] hi") {
		t.Error("mention of a different bot_id must NOT match")
	}
	if ch.isMentioned("plain text no mention") {
		t.Error("plain text must not register a mention")
	}
}

func TestStripMention_OnlyOurs(t *testing.T) {
	ch, _ := newHandleTestChannel(t, 101, false)
	defer resetWebhookRouterForTest()

	input := "[USER=999]Alice[/USER] hey [USER=101]Bot[/USER] can you help?"
	got := ch.stripMention(input)

	if strings.Contains(got, "[USER=101]") {
		t.Errorf("our mention not stripped: %q", got)
	}
	if !strings.Contains(got, "[USER=999]Alice[/USER]") {
		t.Errorf("other users' mentions must be preserved: %q", got)
	}
}

// Regression for the `[^\[]*` → `(?s).*?` fix. A mention whose display text
// contains nested BBCode used to leave the opening `[USER=...]` + raw content
// in the stripped string, because the character class stopped at the nested
// `[`. Non-greedy `.*?` with (?s) handles it.
func TestStripMention_NestedBBCodeInDisplayName(t *testing.T) {
	ch, _ := newHandleTestChannel(t, 101, false)
	defer resetWebhookRouterForTest()

	cases := []struct {
		name  string
		input string
	}{
		{"bold display name", "[USER=101][b]Boss[/b][/USER] hello"},
		{"italic + icon", "[USER=101][i]Team[/i] [img]foo[/img][/USER] hi"},
		{"multiline display", "[USER=101]Line1\nLine2[/USER] ping"},
		{"two of our mentions", "[USER=101]A[/USER] and [USER=101]B[/USER] done"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := ch.stripMention(tc.input)
			if strings.Contains(got, "[USER=101]") {
				t.Errorf("opening tag not stripped: %q", got)
			}
			if strings.Contains(got, "[/USER]") {
				t.Errorf("closing tag leaked: %q", got)
			}
		})
	}
}

func TestIsMentioned_NestedBBCodeCounts(t *testing.T) {
	ch, _ := newHandleTestChannel(t, 101, false)
	defer resetWebhookRouterForTest()

	// isMentioned is a pure substring check on `[USER=101]`; nested BBCode in
	// the display text should not affect detection.
	if !ch.isMentioned("[USER=101][b]Boss[/b][/USER] hi") {
		t.Error("nested BBCode inside mention should still count as mentioned")
	}
}

func TestMention_ReturnsNilBeforeBotIDSet(t *testing.T) {
	ch, _ := newHandleTestChannel(t, 0, false)
	defer resetWebhookRouterForTest()

	// botID 0 means we haven't registered yet — mention helpers should degrade
	// gracefully instead of panicking.
	if got := ch.mention(); got != nil {
		t.Errorf("mention() = %+v; want nil when botID=0", got)
	}
	if ch.isMentioned("[USER=101]x[/USER]") {
		t.Error("isMentioned should be false when botID=0")
	}
	if got := ch.stripMention("hello"); got != "hello" {
		t.Errorf("stripMention should no-op when botID=0, got %q", got)
	}
}

func TestDispatchEvent_BotDelete_UnregistersAndMarksStopped(t *testing.T) {
	ch, _ := newHandleTestChannel(t, 555, false)
	defer resetWebhookRouterForTest()

	// Register so we can observe the unregister side-effect.
	ch.router.RegisterBot(555, ch)

	ch.DispatchEvent(context.Background(), &Event{
		Type:   EventBotDelete,
		Params: EventParams{BotID: 555},
	})

	ch.router.mu.RLock()
	_, exists := ch.router.byBotID[555]
	ch.router.mu.RUnlock()
	if exists {
		t.Error("router must no longer have the bot dispatcher after ONIMBOTDELETE")
	}
	if ch.IsRunning() {
		t.Error("channel should be marked not-running after ONIMBOTDELETE")
	}
}

func TestDispatchEvent_MessageEditAndDeleteIgnored(t *testing.T) {
	ch, mb := newHandleTestChannel(t, 101, false)
	defer resetWebhookRouterForTest()

	ch.DispatchEvent(context.Background(), &Event{
		Type:   EventMessageUpdate,
		Params: EventParams{FromUserID: "42", DialogID: "42", Message: "edited text"},
	})
	ch.DispatchEvent(context.Background(), &Event{
		Type:   EventMessageDelete,
		Params: EventParams{FromUserID: "42", DialogID: "42"},
	})
	if _, ok := drainOne(mb, 50*time.Millisecond); ok {
		t.Error("edit/delete events must not produce inbound messages in Phase 03")
	}
}
