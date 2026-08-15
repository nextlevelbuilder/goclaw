package whatsapp

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"go.mau.fi/whatsmeow/proto/waE2E"
	"go.mau.fi/whatsmeow/types"
	"go.mau.fi/whatsmeow/types/events"

	"github.com/nextlevelbuilder/goclaw/internal/bus"
	"github.com/nextlevelbuilder/goclaw/internal/channels"
	"github.com/nextlevelbuilder/goclaw/internal/config"
	"github.com/nextlevelbuilder/goclaw/internal/store"
)

type whatsappInboundPairingStore struct {
	paired map[string]bool
}

func (s whatsappInboundPairingStore) RequestPairing(context.Context, string, string, string, string, map[string]string) (string, error) {
	return "PAIR123", nil
}

func (s whatsappInboundPairingStore) ApprovePairing(context.Context, string, string) (*store.PairedDeviceData, error) {
	return nil, nil
}

func (s whatsappInboundPairingStore) DenyPairing(context.Context, string) error {
	return nil
}

func (s whatsappInboundPairingStore) RevokePairing(context.Context, string, string) error {
	return nil
}

func (s whatsappInboundPairingStore) IsPaired(_ context.Context, senderID, channel string) (bool, error) {
	return s.paired[senderID+"|"+channel], nil
}

func (s whatsappInboundPairingStore) ListPending(context.Context) []store.PairingRequestData {
	return nil
}

func (s whatsappInboundPairingStore) ListPaired(context.Context) []store.PairedDeviceData {
	return nil
}

func (s whatsappInboundPairingStore) MigrateGroupChatID(context.Context, string, string, string) error {
	return nil
}

func TestHandleIncomingMessage_PairedDMPublishesAfterPolicyAllow(t *testing.T) {
	sender := types.NewJID("15550001111", types.DefaultUserServer)
	cfg := config.WhatsAppConfig{
		AllowFrom: config.FlexibleStringSlice{"19990000000@s.whatsapp.net"},
		DMPolicy:  "pairing",
	}
	ch, msgBus, tenantID := newInboundPolicyTestChannel(t, cfg, whatsappInboundPairingStore{
		paired: map[string]bool{sender.String() + "|clark-whatsapp": true},
	})

	ch.handleIncomingMessage(newWhatsAppTextEvent("msg-dm-1", sender, sender, "hello after reauth"))

	msg := consumeWhatsAppInbound(t, msgBus)
	if msg.Channel != "clark-whatsapp" {
		t.Fatalf("Channel = %q, want clark-whatsapp", msg.Channel)
	}
	if msg.AgentID != "clax" {
		t.Fatalf("AgentID = %q, want clax", msg.AgentID)
	}
	if msg.TenantID != tenantID {
		t.Fatalf("TenantID = %s, want %s", msg.TenantID, tenantID)
	}
	if msg.SenderID != sender.String() {
		t.Fatalf("SenderID = %q, want %q", msg.SenderID, sender.String())
	}
	if msg.PeerKind != "direct" {
		t.Fatalf("PeerKind = %q, want direct", msg.PeerKind)
	}
	if msg.Metadata["message_id"] != "msg-dm-1" {
		t.Fatalf("message_id = %q, want msg-dm-1", msg.Metadata["message_id"])
	}
}

func TestHandleIncomingMessage_OpenDMDropsSenderOutsideConfiguredAllowlist(t *testing.T) {
	sender := types.NewJID("15550003333", types.DefaultUserServer)
	cfg := config.WhatsAppConfig{
		AllowFrom: config.FlexibleStringSlice{"15550004444@s.whatsapp.net"},
		DMPolicy:  "open",
	}
	ch, msgBus, _ := newInboundPolicyTestChannel(t, cfg, whatsappInboundPairingStore{})

	ch.handleIncomingMessage(newWhatsAppTextEvent("msg-dm-open-1", sender, sender, "open but not allowlisted"))

	assertNoWhatsAppInbound(t, msgBus)
}

func TestHandleIncomingMessage_OpenGroupPolicyPublishesAfterPolicyAllow(t *testing.T) {
	sender := types.NewJID("15550002222", types.DefaultUserServer)
	group := types.NewJID("120363025555555555", types.GroupServer)
	cfg := config.WhatsAppConfig{
		AllowFrom:   config.FlexibleStringSlice{"19990000000@s.whatsapp.net"},
		GroupPolicy: "open",
	}
	ch, msgBus, _ := newInboundPolicyTestChannel(t, cfg, whatsappInboundPairingStore{})

	ch.handleIncomingMessage(newWhatsAppTextEvent("msg-group-1", sender, group, "group hello"))

	msg := consumeWhatsAppInbound(t, msgBus)
	if msg.ChatID != group.String() {
		t.Fatalf("ChatID = %q, want %q", msg.ChatID, group.String())
	}
	if msg.PeerKind != "group" {
		t.Fatalf("PeerKind = %q, want group", msg.PeerKind)
	}
	if msg.AgentID != "clax" {
		t.Fatalf("AgentID = %q, want clax", msg.AgentID)
	}
}

func newInboundPolicyTestChannel(t *testing.T, cfg config.WhatsAppConfig, ps store.PairingStore) (*Channel, *bus.MessageBus, uuid.UUID) {
	t.Helper()

	msgBus := bus.New()
	base := channels.NewBaseChannel(channels.TypeWhatsApp, msgBus, []string(cfg.AllowFrom))
	base.SetName("clark-whatsapp")
	base.SetType(channels.TypeWhatsApp)
	base.SetAgentID("clax")
	tenantID := uuid.New()
	base.SetTenantID(tenantID)
	base.SetPairingService(ps)
	base.SetGroupHistory(channels.NewPendingHistory())

	return &Channel{
		BaseChannel: base,
		config:      cfg,
	}, msgBus, tenantID
}

func newWhatsAppTextEvent(messageID string, sender, chat types.JID, text string) *events.Message {
	return &events.Message{
		Info: types.MessageInfo{
			MessageSource: types.MessageSource{
				Chat:   chat,
				Sender: sender,
			},
			ID:        types.MessageID(messageID),
			PushName:  "Tester",
			Timestamp: time.Now(),
		},
		Message: &waE2E.Message{Conversation: &text},
	}
}

func consumeWhatsAppInbound(t *testing.T, msgBus *bus.MessageBus) bus.InboundMessage {
	t.Helper()

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()

	msg, ok := msgBus.ConsumeInbound(ctx)
	if !ok {
		t.Fatal("expected inbound message to be published")
	}
	return msg
}

func assertNoWhatsAppInbound(t *testing.T, msgBus *bus.MessageBus) {
	t.Helper()

	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	if msg, ok := msgBus.ConsumeInbound(ctx); ok {
		t.Fatalf("expected no inbound message, got %+v", msg)
	}
}
