package bitrix24

import (
	"context"
	"encoding/json"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/nextlevelbuilder/goclaw/internal/bus"
	"github.com/nextlevelbuilder/goclaw/internal/cache"
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

// Regression: Bitrix24 strips ALL `[USER=...]` mentions from MESSAGE on group
// chats — including mentions of OTHER users — so relying on MESSAGE alone loses
// the addressed-user context. Handler must read MESSAGE_ORIGINAL when present,
// strip THIS bot's mention, and surface remaining user mentions to the agent
// in a readable form. Field-tested on a payload that originally arrived as just
// "Đây này em", losing two upstream `[USER=...]` mentions to teammates.
func TestHandleMessage_GroupPreservesOtherUserMentions(t *testing.T) {
	ch, mb := newHandleTestChannel(t, 101, true)
	defer resetWebhookRouterForTest()

	ch.DispatchEvent(context.Background(), &Event{
		Type: EventMessageAdd,
		Params: EventParams{
			FromUserID:  "614",
			DialogID:    "chat4932",
			MessageType: "chat",
			// Bitrix24 strips ALL mentions from MESSAGE — even of other users.
			// MESSAGE_ORIGINAL is the raw BBCode source.
			Message:         "Đây này em",
			MessageOriginal: "[USER=982]Ngân Nguyệt - Hàn Lập[/USER] [USER=62]Đặng Văn Tình[/USER] [USER=101]Bot[/USER] Đây này em",
			MentionedList:   map[string]string{"101": "101"}, // pass mention check
		},
	})
	msg, ok := drainOne(mb, 500*time.Millisecond)
	if !ok {
		t.Fatal("mentioned group message must publish")
	}
	if strings.Contains(msg.Content, "[USER=101]") {
		t.Errorf("THIS bot's mention must be stripped, got: %q", msg.Content)
	}
	if strings.Contains(msg.Content, "[USER=") || strings.Contains(msg.Content, "[/USER]") {
		t.Errorf("remaining BBCode tags must be converted, got: %q", msg.Content)
	}
	if !strings.Contains(msg.Content, "@Ngân Nguyệt - Hàn Lập (ID:982)") {
		t.Errorf("other user mention must be preserved/readable, got: %q", msg.Content)
	}
	if !strings.Contains(msg.Content, "@Đặng Văn Tình (ID:62)") {
		t.Errorf("other user mention must be preserved/readable, got: %q", msg.Content)
	}
	if !strings.Contains(msg.Content, "Đây này em") {
		t.Errorf("body lost, got: %q", msg.Content)
	}
}

// Legacy portals may omit MESSAGE_ORIGINAL — handler must fall back to
// MESSAGE (the historical behavior) without panicking.
func TestHandleMessage_GroupFallsBackToMessageWhenOriginalAbsent(t *testing.T) {
	ch, mb := newHandleTestChannel(t, 101, true)
	defer resetWebhookRouterForTest()

	ch.DispatchEvent(context.Background(), &Event{
		Type: EventMessageAdd,
		Params: EventParams{
			FromUserID:    "42",
			DialogID:      "chat10",
			MessageType:   "chat",
			Message:       "[USER=101]Bot[/USER] hello",
			MentionedList: map[string]string{"101": "101"},
		},
	})
	msg, ok := drainOne(mb, 500*time.Millisecond)
	if !ok {
		t.Fatal("expected publish")
	}
	if !strings.Contains(msg.Content, "hello") {
		t.Errorf("body lost on MESSAGE-only fallback: %q", msg.Content)
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

// --- ContactCollector wiring tests (Phase B) -----------------------------

// fakeContactStore captures UpsertContact calls so handle_test can verify
// the channel invokes ContactCollector.EnsureContact the same way Telegram
// does. Only implements the methods ContactCollector actually exercises.
type fakeContactStore struct {
	mu      sync.Mutex
	upserts []fakeUpsertCall
}

type fakeUpsertCall struct {
	channelType     string
	channelInstance string
	senderID        string
	userID          string
	peerKind        string
	contactType     string
	threadID        string
}

func (f *fakeContactStore) UpsertContact(_ context.Context, channelType, channelInstance, senderID, userID, _, _, peerKind, contactType, threadID, _ string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.upserts = append(f.upserts, fakeUpsertCall{
		channelType:     channelType,
		channelInstance: channelInstance,
		senderID:        senderID,
		userID:          userID,
		peerKind:        peerKind,
		contactType:     contactType,
		threadID:        threadID,
	})
	return nil
}

func (f *fakeContactStore) ResolveTenantUserID(_ context.Context, _, _ string) (string, error) {
	return "", nil
}
func (f *fakeContactStore) ListContacts(_ context.Context, _ store.ContactListOpts) ([]store.ChannelContact, error) {
	return nil, nil
}
func (f *fakeContactStore) CountContacts(_ context.Context, _ store.ContactListOpts) (int, error) {
	return 0, nil
}
func (f *fakeContactStore) GetContactsBySenderIDs(_ context.Context, _ []string) (map[string]store.ChannelContact, error) {
	return nil, nil
}
func (f *fakeContactStore) GetContactByID(_ context.Context, _ uuid.UUID) (*store.ChannelContact, error) {
	return nil, nil
}
func (f *fakeContactStore) GetSenderIDsByContactIDs(_ context.Context, _ []uuid.UUID) ([]string, error) {
	return nil, nil
}
func (f *fakeContactStore) MergeContacts(_ context.Context, _ []uuid.UUID, _ uuid.UUID) error {
	return nil
}
func (f *fakeContactStore) UnmergeContacts(_ context.Context, _ []uuid.UUID) error { return nil }
func (f *fakeContactStore) GetContactsByMergedID(_ context.Context, _ uuid.UUID) ([]store.ChannelContact, error) {
	return nil, nil
}

func (f *fakeContactStore) snapshot() []fakeUpsertCall {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([]fakeUpsertCall, len(f.upserts))
	copy(out, f.upserts)
	return out
}

func newChannelWithContactCollector(t *testing.T, botID int, requireMention bool) (*Channel, *bus.MessageBus, *fakeContactStore) {
	t.Helper()
	ch, mb := newHandleTestChannel(t, botID, requireMention)
	fakeStore := &fakeContactStore{}
	ch.SetContactCollector(store.NewContactCollector(fakeStore, cache.NewInMemoryCache[bool]()))
	return ch, mb, fakeStore
}

func TestHandleMessage_DM_CollectsSenderContact(t *testing.T) {
	ch, _, cs := newChannelWithContactCollector(t, 101, false)
	defer resetWebhookRouterForTest()

	ch.DispatchEvent(context.Background(), &Event{
		Type: EventMessageAdd,
		Params: EventParams{
			FromUserID:  "42",
			DialogID:    "42",
			MessageID:   "m-1",
			MessageType: "P", // DM short code
			Message:     "hi",
		},
	})

	calls := cs.snapshot()
	if len(calls) != 1 {
		t.Fatalf("expected 1 contact upsert for DM, got %d: %+v", len(calls), calls)
	}
	c := calls[0]
	if c.channelType != ch.Type() || c.channelInstance != ch.Name() {
		t.Errorf("wrong channel routing: %+v", c)
	}
	if c.senderID != "42" || c.userID != "42" {
		t.Errorf("sender/userID mismatch; want both '42', got sender=%q userID=%q", c.senderID, c.userID)
	}
	if c.peerKind != "direct" || c.contactType != "user" {
		t.Errorf("peerKind/contactType mismatch; want direct/user, got %q/%q", c.peerKind, c.contactType)
	}
	if c.threadID != "" {
		t.Errorf("DM must not set threadID, got %q", c.threadID)
	}
}

func TestHandleMessage_Group_CollectsBothSenderAndGroupContact(t *testing.T) {
	ch, _, cs := newChannelWithContactCollector(t, 101, false)
	defer resetWebhookRouterForTest()

	ch.DispatchEvent(context.Background(), &Event{
		Type: EventMessageAdd,
		Params: EventParams{
			FromUserID:  "42",
			DialogID:    "chat10",
			MessageID:   "m-2",
			MessageType: "C", // group short code
			Message:     "team ping",
		},
	})

	calls := cs.snapshot()
	if len(calls) != 2 {
		t.Fatalf("expected 2 contact upserts (sender + group), got %d: %+v", len(calls), calls)
	}

	// Call 0 = sender as user contact
	if calls[0].senderID != "42" || calls[0].peerKind != "group" || calls[0].contactType != "user" {
		t.Errorf("call[0] wrong; expected sender=42 peer=group ctype=user, got %+v", calls[0])
	}
	// Call 1 = group as group contact
	if calls[1].senderID != "chat10" || calls[1].peerKind != "group" || calls[1].contactType != "group" {
		t.Errorf("call[1] wrong; expected sender=chat10 peer=group ctype=group, got %+v", calls[1])
	}
}

// TestHandleMessage_ChatEntityForwardedAsMetadata proves the bus.InboundMessage
// carries the entity binding so MCP tools can resolve "this deal" / "this task"
// without the agent guessing from CHAT_TITLE strings. Plain user-created chats
// (no entity binding) must NOT add stale or empty metadata keys — downstream
// readers do `_, ok := metadata["bitrix_chat_entity_id"]` checks.
func TestHandleMessage_ChatEntityForwardedAsMetadata(t *testing.T) {
	cases := []struct {
		name         string
		entityType   string
		entityID     string
		messageType  string
		wantTypeMeta string // "" means key must be absent
		wantIDMeta   string
	}{
		{
			name: "crm_deal_chat",
			entityType: "CRM", entityID: "DEAL|2064", messageType: "C",
			wantTypeMeta: "CRM", wantIDMeta: "DEAL|2064",
		},
		{
			name: "tasks_chat_X_type",
			entityType: "TASKS_TASK", entityID: "2704", messageType: "X",
			wantTypeMeta: "TASKS_TASK", wantIDMeta: "2704",
		},
		{
			name: "plain_group_omits_keys",
			entityType: "", entityID: "", messageType: "C",
			wantTypeMeta: "", wantIDMeta: "",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			ch, mb := newHandleTestChannel(t, 101, false)
			defer resetWebhookRouterForTest()

			ch.DispatchEvent(context.Background(), &Event{
				Type: EventMessageAdd,
				Params: EventParams{
					FromUserID:     "42",
					DialogID:       "chat999",
					MessageID:      "m-entity",
					MessageType:    tc.messageType,
					Message:        "anything",
					MessageOriginal: "[USER=101]Bot[/USER] anything", // pass mention check for groups
					MentionedList:  map[string]string{"101": "101"},
					ChatEntityType: tc.entityType,
					ChatEntityID:   tc.entityID,
				},
			})
			msg, ok := drainOne(mb, 500*time.Millisecond)
			if !ok {
				t.Fatal("expected inbound message")
			}
			gotType, hasType := msg.Metadata["bitrix_chat_entity_type"]
			gotID, hasID := msg.Metadata["bitrix_chat_entity_id"]
			if tc.wantTypeMeta == "" {
				if hasType {
					t.Errorf("bitrix_chat_entity_type unexpectedly set: %q", gotType)
				}
				if hasID {
					t.Errorf("bitrix_chat_entity_id unexpectedly set: %q", gotID)
				}
				return
			}
			if gotType != tc.wantTypeMeta {
				t.Errorf("bitrix_chat_entity_type = %q; want %q", gotType, tc.wantTypeMeta)
			}
			if gotID != tc.wantIDMeta {
				t.Errorf("bitrix_chat_entity_id = %q; want %q", gotID, tc.wantIDMeta)
			}
		})
	}
}

func TestHandleMessage_Blocked_DoesNotCollectContact(t *testing.T) {
	ch, _, cs := newChannelWithContactCollector(t, 101, false)
	defer resetWebhookRouterForTest()

	// System message is filtered BEFORE contact collection — must not record.
	ch.DispatchEvent(context.Background(), &Event{
		Type: EventMessageAdd,
		Params: EventParams{
			FromUserID:    "42",
			DialogID:      "42",
			MessageType:   "P",
			Message:       "user X joined",
			SystemMessage: true,
		},
	})
	// Empty content also filtered — must not record.
	ch.DispatchEvent(context.Background(), &Event{
		Type: EventMessageAdd,
		Params: EventParams{
			FromUserID:  "42",
			DialogID:    "42",
			MessageType: "P",
			Message:     "   ",
		},
	})

	if n := len(cs.snapshot()); n != 0 {
		t.Errorf("blocked messages must not record contacts, got %d upserts", n)
	}
}

// --- Open Channel (MESSAGE_TYPE="L") gating ---------------------------------

func TestIsOpenChannelParams(t *testing.T) {
	tests := []struct {
		name   string
		params *EventParams
		want   bool
	}{
		{name: "nil", params: nil, want: false},
		{name: "message type L", params: &EventParams{MessageType: "L"}, want: true},
		{name: "lines entity", params: &EventParams{ChatEntityType: "LINES"}, want: true},
		{name: "case insensitive", params: &EventParams{ChatEntityType: "lines"}, want: true},
		{name: "regular group", params: &EventParams{MessageType: "C"}, want: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := isOpenChannelParams(tt.params); got != tt.want {
				t.Errorf("isOpenChannelParams() = %v; want %v", got, tt.want)
			}
		})
	}
}

// TestHandleMessage_OpenChannel_ConnectorWithoutMentionDropped covers the
// customer side of the Open Channel gate on a GROUP-style connector
// (Zalo personal) without an explicit mention: customers chatting amongst
// themselves in the same Open Channel chat must NOT trigger the agent.
// The CHAT_ENTITY_ID prefix is what tells us "this connector is
// group-style and therefore needs the gate" — connectors that don't
// match the whitelist are handled by sibling tests (no gate, every
// message forwards).
func TestHandleMessage_OpenChannel_ConnectorWithoutMentionDropped(t *testing.T) {
	ch, mb := newHandleTestChannel(t, 1058, false)
	defer resetWebhookRouterForTest()

	ch.DispatchEvent(context.Background(), &Event{
		Type: EventMessageAdd,
		Params: EventParams{
			FromUserID:      "960",
			DialogID:        "chat4878",
			MessageID:       "m-ol-customer",
			MessageType:     "L",
			ChatEntityType:  "LINES",
			ChatEntityID:    "synity_zalo_personal|20|grp|960",
			Message:         "Em hỏi giá sản phẩm A",
			FromIsConnector: true,
		},
	})
	if _, ok := drainOne(mb, 100*time.Millisecond); ok {
		t.Error("Open Channel connector (whitelisted group-style) messages without mention must be dropped")
	}
}

// TestHandleMessage_OpenChannel_OneToOneConnectorWithoutMentionForwarded
// covers the inverse case: a 1-to-1 connector (e.g. Facebook Messenger,
// Zalo OA) where each customer has their own Open Channel chat. Every
// customer message is addressed to the bot by construction; gating
// would silently drop legitimate customer turns. CHAT_ENTITY_ID prefix
// "facebook" is not in the require-mention whitelist, so the message
// MUST forward without a mention.
func TestHandleMessage_OpenChannel_OneToOneConnectorWithoutMentionForwarded(t *testing.T) {
	ch, mb := newHandleTestChannel(t, 1058, false)
	defer resetWebhookRouterForTest()

	ch.DispatchEvent(context.Background(), &Event{
		Type: EventMessageAdd,
		Params: EventParams{
			FromUserID:      "1074",
			DialogID:        "chat5208",
			MessageID:       "m-fb-customer",
			MessageType:     "L",
			ChatEntityType:  "LINES",
			ChatEntityID:    "facebook|34|36305652112415023|1074",
			Message:         "hello, do you have product X in stock?",
			FromIsConnector: true,
		},
	})
	if _, ok := drainOne(mb, 200*time.Millisecond); !ok {
		t.Error("1-to-1 connector (facebook) customer messages must forward without requiring mention")
	}
}

// TestHandleMessage_OpenChannel_ConnectorWithMentionForwarded covers the
// "customer calls the bot" case: a Zalo/FB customer (IS_CONNECTOR=Y) whose
// upstream populated MENTIONED_LIST with the bot id must trigger a reply,
// the same as an internal staff member who @-mentions the bot. Connector
// status alone is no longer a drop signal — mention is the only gate.
func TestHandleMessage_OpenChannel_ConnectorWithMentionForwarded(t *testing.T) {
	const botID = 1058
	ch, mb := newHandleTestChannel(t, botID, false)
	defer resetWebhookRouterForTest()

	ch.DispatchEvent(context.Background(), &Event{
		Type: EventMessageAdd,
		Params: EventParams{
			FromUserID:      "960",
			DialogID:        "chat4878",
			MessageID:       "m-ol-customer-mention",
			MessageType:     "L",
			ChatEntityType:  "LINES",
			Message:         "alo bot ơi cho hỏi giá",
			MessageOriginal: "[USER=1058]Tiểu Hà[/USER] alo bot ơi cho hỏi giá",
			MentionedList:   map[string]string{"1058": "1058"},
			FromIsConnector: true,
		},
	})
	msg, ok := drainOne(mb, 500*time.Millisecond)
	if !ok {
		t.Fatal("Open Channel connector WITH @mention must forward to agent")
	}
	if msg.PeerKind != "group" {
		t.Errorf("PeerKind = %q; want group (OL forced into group routing)", msg.PeerKind)
	}
	if !strings.Contains(msg.Content, "alo bot ơi cho hỏi giá") {
		t.Errorf("content stripped wrong: %q", msg.Content)
	}
	if strings.Contains(msg.Content, "[USER=1058]") {
		t.Errorf("bot mention BBCode not stripped: %q", msg.Content)
	}
}

// TestHandleMessage_OpenChannel_InternalStaffWithoutMentionDropped covers the
// silent-staff case: an internal Bitrix24 user joins the Open Channel session
// to supervise but does NOT @-mention the bot — bot stays out.
func TestHandleMessage_OpenChannel_InternalStaffWithoutMentionDropped(t *testing.T) {
	ch, mb := newHandleTestChannel(t, 1058, false)
	defer resetWebhookRouterForTest()

	ch.DispatchEvent(context.Background(), &Event{
		Type: EventMessageAdd,
		Params: EventParams{
			FromUserID:      "610",
			DialogID:        "chat4878",
			MessageID:       "m-ol-staff-quiet",
			MessageType:     "L",
			ChatEntityType:  "LINES",
			Message:         "haha được rồi",
			FromIsConnector: false,
		},
	})
	if _, ok := drainOne(mb, 100*time.Millisecond); ok {
		t.Error("Open Channel internal staff without @mention must be dropped")
	}
}

// TestHandleMessage_OpenChannel_InternalStaffMentionForwarded covers the
// "call the bot" case: internal staff @-mentions the bot — message goes to the
// agent like a normal group mention.
func TestHandleMessage_OpenChannel_InternalStaffMentionForwarded(t *testing.T) {
	const botID = 1058
	ch, mb := newHandleTestChannel(t, botID, false)
	defer resetWebhookRouterForTest()

	ch.DispatchEvent(context.Background(), &Event{
		Type: EventMessageAdd,
		Params: EventParams{
			FromUserID:      "610",
			DialogID:        "chat4878",
			MessageID:       "m-ol-staff-mention",
			MessageType:     "L",
			ChatEntityType:  "LINES",
			Message:         "tổng hợp khách này giúp",                                 // stripped form (Bitrix strips group mentions)
			MessageOriginal: "[USER=1058]Tiểu Hà[/USER] tổng hợp khách này giúp",
			MentionedList:   map[string]string{"1058": "1058"},
			FromIsConnector: false,
		},
	})
	msg, ok := drainOne(mb, 500*time.Millisecond)
	if !ok {
		t.Fatal("Open Channel internal staff WITH @mention must forward to agent")
	}
	if msg.PeerKind != "group" {
		t.Errorf("PeerKind = %q; want group (OL forced into group routing)", msg.PeerKind)
	}
	if !strings.Contains(msg.Content, "tổng hợp khách này giúp") {
		t.Errorf("content stripped wrong: %q", msg.Content)
	}
	// The bot's own [USER=1058]…[/USER] mention should be stripped from the
	// forwarded content.
	if strings.Contains(msg.Content, "[USER=1058]") {
		t.Errorf("bot mention BBCode not stripped: %q", msg.Content)
	}
}

// TestHandleMessage_OL_ThreeToken_ConnectorCreatesParticipantIdentity verifies a
// connector message (IS_CONNECTOR=Y) carrying the 3-token "[Name] #uid #msgId"
// tag mints a per-participant synthetic identity: participant_user_id contains the
// uid + chat id, the echo prefix is "#msgId" only, and the tag is stripped from the
// body the agent sees.
func TestHandleMessage_OL_ThreeToken_ConnectorCreatesParticipantIdentity(t *testing.T) {
	ch, mb := newHandleTestChannel(t, 1058, false)
	defer resetWebhookRouterForTest()

	ch.DispatchEvent(context.Background(), &Event{
		Type: EventMessageAdd,
		Params: EventParams{
			FromUserID:      "960",
			DialogID:        "chat4878",
			MessageID:       "297178", // Bitrix MESSAGE_ID (distinct from the Zalo msgId)
			MessageType:     "L",
			ChatEntityType:  "LINES",
			Message:         "[Trung Hee] #111222 #777888 alo bot",
			MessageOriginal: "[USER=1058]bot[/USER] [Trung Hee] #111222 #777888 alo bot",
			MentionedList:   map[string]string{"1058": "1058"},
			FromIsConnector: true,
		},
	})
	msg, ok := drainOne(mb, 500*time.Millisecond)
	if !ok {
		t.Fatal("connector 3-token message must forward")
	}
	pid := msg.Metadata[MetaKeyParticipantUserID]
	if !strings.Contains(pid, "111222") {
		t.Errorf("participant_user_id = %q; must contain uid 111222", pid)
	}
	if !strings.Contains(pid, "chat4878") {
		t.Errorf("participant_user_id = %q; must contain chat id chat4878", pid)
	}
	// SenderID is the synthetic per-participant id, not the proxy "960".
	if msg.SenderID != pid {
		t.Errorf("SenderID = %q; want synthetic participant id %q", msg.SenderID, pid)
	}
	if msg.SenderID == "960" {
		t.Error("SenderID must not stay the connector proxy id 960")
	}
	// Echo prefix = "#msgId" only (no name, no uid).
	if pfx := msg.Metadata[MetaKeySenderPrefix]; pfx != "#777888" {
		t.Errorf("MetaKeySenderPrefix = %q; want #777888 (msgId only)", pfx)
	}
	// Bitrix MESSAGE_ID for the v2 fields.replyId stays the Bitrix id, NOT the
	// 13-digit Zalo msgId — the echo prefix is the only place the connector msgId
	// is surfaced.
	if mid := msg.Metadata[MetaKeyMessageID]; mid != "297178" {
		t.Errorf("MetaKeyMessageID = %q; want 297178 (Bitrix MESSAGE_ID preserved)", mid)
	}
	if strings.Contains(msg.Content, "[Trung Hee]") || strings.Contains(msg.Content, "#111222") {
		t.Errorf("content must not contain the sender tag: %q", msg.Content)
	}
	if !strings.Contains(msg.Content, "alo bot") {
		t.Errorf("content lost the body: %q", msg.Content)
	}
}

// TestHandleMessage_OL_ForgedTag_OperatorNotGettingParticipantIdentity is the
// security gate: an operator message (IS_CONNECTOR=N) whose text mimics the
// 3-token connector tag must NOT mint a participant identity. Live logs showed an
// operator typing "[Trung Hee] ..." — without the gate they'd be misattributed as
// that customer.
func TestHandleMessage_OL_ForgedTag_OperatorNotGettingParticipantIdentity(t *testing.T) {
	ch, mb := newHandleTestChannel(t, 1058, false)
	defer resetWebhookRouterForTest()

	ch.DispatchEvent(context.Background(), &Event{
		Type: EventMessageAdd,
		Params: EventParams{
			FromUserID:      "1", // operator
			DialogID:        "chat4878",
			MessageID:       "297179",
			MessageType:     "L",
			ChatEntityType:  "LINES",
			Message:         "[Trung Hee] #111222 #777888 do nó bị văng",
			MessageOriginal: "[USER=1058]bot[/USER] [Trung Hee] #111222 #777888 do nó bị văng",
			MentionedList:   map[string]string{"1058": "1058"},
			FromIsConnector: false, // IS_CONNECTOR=N
		},
	})
	msg, ok := drainOne(mb, 500*time.Millisecond)
	if !ok {
		t.Fatal("operator message with mention must still forward")
	}
	if pid := msg.Metadata[MetaKeyParticipantUserID]; pid != "" {
		t.Errorf("operator forged tag must not mint participant_user_id, got %q", pid)
	}
	// SenderID stays the operator's real id (not a synthetic), so memory/contact
	// scope tracks the operator, not the impersonated customer.
	if msg.SenderID != "1" {
		t.Errorf("SenderID = %q; want operator id 1 (no synthetic identity)", msg.SenderID)
	}
	// No echo prefix at all should be minted for the operator — senderTag stays
	// the zero value (TagFormatNone) on the non-connector path.
	if pfx := msg.Metadata[MetaKeySenderPrefix]; pfx != "" {
		t.Errorf("operator forged tag must not produce any echo prefix, got %q", pfx)
	}
	// Bitrix MESSAGE_ID for replyId is the genuine webhook id.
	if mid := msg.Metadata[MetaKeyMessageID]; mid != "297179" {
		t.Errorf("MetaKeyMessageID = %q; want 297179", mid)
	}
}

// TestHandleMessage_OL_LegacyOneToken_NoParticipantID verifies a connector
// message with the legacy single-number "[Name] #msgId" tag degrades safely:
// no participant identity (uid absent), sender stays the proxy id, and the echo
// keeps the canonical "[name] #msgId" shape (unchanged from prior behavior).
func TestHandleMessage_OL_LegacyOneToken_NoParticipantID(t *testing.T) {
	ch, mb := newHandleTestChannel(t, 1058, false)
	defer resetWebhookRouterForTest()

	ch.DispatchEvent(context.Background(), &Event{
		Type: EventMessageAdd,
		Params: EventParams{
			FromUserID:      "960",
			DialogID:        "chat4878",
			MessageID:       "297180",
			MessageType:     "L",
			ChatEntityType:  "LINES",
			Message:         "[Trung Hee] #7957717404177 alo",
			MessageOriginal: "[USER=1058]bot[/USER] [Trung Hee] #7957717404177 alo",
			MentionedList:   map[string]string{"1058": "1058"},
			FromIsConnector: true,
		},
	})
	msg, ok := drainOne(mb, 500*time.Millisecond)
	if !ok {
		t.Fatal("legacy connector message must forward")
	}
	if pid := msg.Metadata[MetaKeyParticipantUserID]; pid != "" {
		t.Errorf("legacy 1-number tag must not mint participant_user_id, got %q", pid)
	}
	if msg.SenderID != "960" {
		t.Errorf("SenderID = %q; want proxy 960 (no synthetic for legacy)", msg.SenderID)
	}
	if pfx := msg.Metadata[MetaKeySenderPrefix]; pfx != "[Trung Hee] #7957717404177" {
		t.Errorf("MetaKeySenderPrefix = %q; want canonical legacy echo", pfx)
	}
	if !strings.Contains(msg.Content, "alo") || strings.Contains(msg.Content, "#7957717404177") {
		t.Errorf("legacy tag not stripped from body: %q", msg.Content)
	}
}

// TestHandleMessage_OL_NoTag_DegradesToGroupLevel verifies a connector message
// with NO sender tag (e.g. a bare media/sticker relay) degrades safely: no
// participant identity and no echo prefix, so the consumer falls back to the
// group-level userID.
func TestHandleMessage_OL_NoTag_DegradesToGroupLevel(t *testing.T) {
	ch, mb := newHandleTestChannel(t, 1058, false)
	defer resetWebhookRouterForTest()

	ch.DispatchEvent(context.Background(), &Event{
		Type: EventMessageAdd,
		Params: EventParams{
			FromUserID:      "960",
			DialogID:        "chat4878",
			MessageID:       "297181",
			MessageType:     "L",
			ChatEntityType:  "LINES",
			Message:         "chào shop cho hỏi giá",
			MessageOriginal: "[USER=1058]bot[/USER] chào shop cho hỏi giá",
			MentionedList:   map[string]string{"1058": "1058"},
			FromIsConnector: true,
		},
	})
	msg, ok := drainOne(mb, 500*time.Millisecond)
	if !ok {
		t.Fatal("untagged connector message must forward")
	}
	if pid := msg.Metadata[MetaKeyParticipantUserID]; pid != "" {
		t.Errorf("untagged message must not mint participant_user_id, got %q", pid)
	}
	if msg.SenderID != "960" {
		t.Errorf("SenderID = %q; want proxy 960", msg.SenderID)
	}
	if pfx := msg.Metadata[MetaKeySenderPrefix]; pfx != "" {
		t.Errorf("untagged message must not set an echo prefix, got %q", pfx)
	}
}
