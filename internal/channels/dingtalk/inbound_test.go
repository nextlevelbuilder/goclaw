package dingtalk

import (
	"context"
	"testing"
	"time"

	"github.com/open-dingtalk/dingtalk-stream-sdk-go/chatbot"

	"github.com/nextlevelbuilder/goclaw/internal/bus"
)

// openPolicy disables the pairing gates so inbound tests exercise parsing and
// routing rather than policy. Policy has its own tests.
func openPolicy() Config {
	return Config{
		ClientID: "k", ClientSecret: "s",
		DMPolicy: "open", GroupPolicy: "open",
	}
}

// startedChannel returns a running channel wired to a fake transport, plus the
// bus it publishes to.
func startedChannel(t *testing.T, cfg Config) (*Channel, *fakeTransport, *bus.MessageBus) {
	t.Helper()
	msgBus := bus.New()
	ch, err := New(cfg, msgBus, nil, nil, nil)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	ft := &fakeTransport{}
	ch.newTransport = func(h chatbot.IChatBotMessageHandler) streamTransport {
		ft.handler = h
		return ft
	}
	if err := ch.Start(context.Background()); err != nil {
		t.Fatalf("Start: %v", err)
	}
	t.Cleanup(func() { _ = ch.Stop(context.Background()) })
	return ch, ft, msgBus
}

// waitInbound waits for one published message.
func waitInbound(t *testing.T, b *bus.MessageBus) bus.InboundMessage {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	msg, ok := b.ConsumeInbound(ctx)
	if !ok {
		t.Fatal("no inbound message published within timeout")
	}
	return msg
}

// expectNoInbound asserts nothing is published within a short window.
func expectNoInbound(t *testing.T, b *bus.MessageBus) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 250*time.Millisecond)
	defer cancel()
	if msg, ok := b.ConsumeInbound(ctx); ok {
		t.Fatalf("unexpected inbound message published: %+v", msg)
	}
}

func textMsg(id, text string) *chatbot.BotCallbackDataModel {
	return &chatbot.BotCallbackDataModel{
		MsgId:            id,
		Msgtype:          "text",
		ConversationType: conversationTypeDirect,
		SenderStaffId:    "staff-1",
		SenderNick:       "Alice",
		ConversationId:   "cid-dm",
		SessionWebhook:   "https://hook.example/1",
		Text:             chatbot.BotCallbackDataTextModel{Content: text},
	}
}

func TestInbound_DirectMessagePublishes(t *testing.T) {
	_, ft, b := startedChannel(t, openPolicy())

	if _, err := ft.deliver(context.Background(), textMsg("m1", "hello")); err != nil {
		t.Fatalf("handler: %v", err)
	}

	msg := waitInbound(t, b)
	if msg.Content != "hello" {
		t.Errorf("Content = %q", msg.Content)
	}
	if msg.PeerKind != "direct" {
		t.Errorf("PeerKind = %q, want direct", msg.PeerKind)
	}
	// A DM routes on the sender, not the conversation: proactive replies address
	// a user id.
	if msg.ChatID != "staff-1" {
		t.Errorf("ChatID = %q, want staff-1", msg.ChatID)
	}
	if msg.SenderID != "staff-1" || msg.UserID != "staff-1" {
		t.Errorf("sender/user = %q/%q", msg.SenderID, msg.UserID)
	}
	if msg.Metadata["session_webhook"] != "https://hook.example/1" {
		t.Errorf("session_webhook missing from metadata: %v", msg.Metadata)
	}
	if msg.Metadata["chat_type"] != "direct" {
		t.Errorf("chat_type = %q", msg.Metadata["chat_type"])
	}
}

// senderStaffId is the corp-scoped id every other DingTalk API wants; senderId
// is only a fallback.
func TestInbound_FallsBackToSenderID(t *testing.T) {
	_, ft, b := startedChannel(t, openPolicy())

	data := textMsg("m1", "hi")
	data.SenderStaffId = ""
	data.SenderId = "conv-scoped-id"
	if _, err := ft.deliver(context.Background(), data); err != nil {
		t.Fatal(err)
	}

	if got := waitInbound(t, b).SenderID; got != "conv-scoped-id" {
		t.Errorf("SenderID = %q", got)
	}
}

func TestInbound_GroupRequiresMention(t *testing.T) {
	cfg := openPolicy()
	_, ft, b := startedChannel(t, cfg) // require_mention defaults to true

	data := textMsg("m1", "chatter")
	data.ConversationType = conversationTypeGroup
	data.ConversationId = "cid-group"
	data.IsInAtList = false

	if _, err := ft.deliver(context.Background(), data); err != nil {
		t.Fatal(err)
	}
	expectNoInbound(t, b)

	// Mentioned: publishes, and carries the prior message as context.
	data2 := textMsg("m2", "hey bot")
	data2.ConversationType = conversationTypeGroup
	data2.ConversationId = "cid-group"
	data2.IsInAtList = true

	if _, err := ft.deliver(context.Background(), data2); err != nil {
		t.Fatal(err)
	}
	msg := waitInbound(t, b)
	if msg.PeerKind != "group" {
		t.Errorf("PeerKind = %q, want group", msg.PeerKind)
	}
	if msg.ChatID != "cid-group" {
		t.Errorf("ChatID = %q", msg.ChatID)
	}
	if msg.Metadata["mentioned_bot"] != "true" {
		t.Errorf("mentioned_bot = %q", msg.Metadata["mentioned_bot"])
	}
}

// With require_mention off, an un-@'d group message is a normal turn.
func TestInbound_GroupWithoutMentionWhenNotRequired(t *testing.T) {
	no := false
	cfg := openPolicy()
	cfg.RequireMention = &no
	_, ft, b := startedChannel(t, cfg)

	data := textMsg("m1", "chatter")
	data.ConversationType = conversationTypeGroup
	data.IsInAtList = false

	if _, err := ft.deliver(context.Background(), data); err != nil {
		t.Fatal(err)
	}
	waitInbound(t, b) // must not time out
}

func TestInbound_GroupSessionScopeGroupSender(t *testing.T) {
	cfg := openPolicy()
	cfg.GroupSessionScope = GroupSessionScopeGroupSender
	_, ft, b := startedChannel(t, cfg)

	data := textMsg("m1", "hey")
	data.ConversationType = conversationTypeGroup
	data.ConversationId = "cid-group"
	data.IsInAtList = true

	if _, err := ft.deliver(context.Background(), data); err != nil {
		t.Fatal(err)
	}
	if got := waitInbound(t, b).ChatID; got != "cid-group:staff-1" {
		t.Errorf("ChatID = %q, want cid-group:staff-1", got)
	}
}

// DingTalk redelivers. One user message must produce one agent run.
func TestInbound_DedupByMsgID(t *testing.T) {
	_, ft, b := startedChannel(t, openPolicy())
	ctx := context.Background()

	for range 3 {
		if _, err := ft.deliver(ctx, textMsg("same-id", "hello")); err != nil {
			t.Fatal(err)
		}
	}

	waitInbound(t, b)
	expectNoInbound(t, b)
}

// Returning from the handler is the ack. A saturated bus must not stall it:
// bus.PublishInbound is a bare channel send, so the publish has to happen off
// the socket's read loop.
func TestInbound_AckDoesNotWaitForBus(t *testing.T) {
	_, ft, b := startedChannel(t, openPolicy())

	// Fill the bus buffer so any further publish blocks.
	for b.TryPublishInbound(bus.InboundMessage{ChatID: "filler"}) {
	}

	start := time.Now()
	if _, err := ft.deliver(context.Background(), textMsg("m1", "hello")); err != nil {
		t.Fatalf("handler: %v", err)
	}
	if elapsed := time.Since(start); elapsed > 500*time.Millisecond {
		t.Fatalf("handler blocked for %v on a full bus; DingTalk would time out the ack", elapsed)
	}
}

func TestInbound_NilPayloadAcks(t *testing.T) {
	_, ft, _ := startedChannel(t, openPolicy())
	if _, err := ft.deliver(context.Background(), nil); err != nil {
		t.Fatalf("nil payload must ack, not error: %v", err)
	}
}
