package slack

import (
	"context"
	"testing"
	"time"

	"github.com/nextlevelbuilder/goclaw/internal/bus"
	"github.com/nextlevelbuilder/goclaw/internal/config"
	"github.com/nextlevelbuilder/goclaw/internal/store"
)

type slackPolicyPairingStore struct {
	paired map[string]map[string]bool
}

func newSlackPolicyPairingStore() *slackPolicyPairingStore {
	return &slackPolicyPairingStore{paired: make(map[string]map[string]bool)}
}

func (s *slackPolicyPairingStore) RequestPairing(context.Context, string, string, string, string, map[string]string) (string, error) {
	return "code123", nil
}

func (s *slackPolicyPairingStore) ApprovePairing(context.Context, string, string) (*store.PairedDeviceData, error) {
	return nil, nil
}

func (s *slackPolicyPairingStore) DenyPairing(context.Context, string) error { return nil }

func (s *slackPolicyPairingStore) RevokePairing(context.Context, string, string) error { return nil }

func (s *slackPolicyPairingStore) IsPaired(_ context.Context, senderID, channel string) (bool, error) {
	if s.paired[senderID] == nil {
		return false, nil
	}
	return s.paired[senderID][channel], nil
}

func (s *slackPolicyPairingStore) ListPending(context.Context) []store.PairingRequestData {
	return nil
}

func (s *slackPolicyPairingStore) ListPaired(context.Context) []store.PairedDeviceData {
	return nil
}

func (s *slackPolicyPairingStore) MigrateGroupChatID(context.Context, string, string, string) error {
	return nil
}

func (s *slackPolicyPairingStore) setPaired(senderID, channel string) {
	if s.paired[senderID] == nil {
		s.paired[senderID] = make(map[string]bool)
	}
	s.paired[senderID][channel] = true
}

func TestSlackPairedDirectMessageBypassesAllowlistAfterPolicyCheck(t *testing.T) {
	msgBus := bus.New()
	pairingStore := newSlackPolicyPairingStore()
	pairingStore.setPaired("U_PAIRED", "slack-test")

	ch, err := New(config.SlackConfig{
		BotToken:  "xoxb-test",
		AppToken:  "xapp-test",
		AllowFrom: []string{"U_ALLOWED"},
		DMPolicy:  "pairing",
	}, msgBus, pairingStore, nil)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	ch.SetName("slack-test")

	if !ch.checkDMPolicy(context.Background(), "U_PAIRED", "D123") {
		t.Fatal("expected paired Slack sender to pass DM policy")
	}
	ch.HandleAuthorizedMessage("U_PAIRED", "D123", "hello", nil, map[string]string{"username": "paired"}, "direct")

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	msg, ok := msgBus.ConsumeInbound(ctx)
	if !ok {
		t.Fatal("expected paired Slack DM to publish inbound")
	}
	if msg.SenderID != "U_PAIRED" || msg.ChatID != "D123" || msg.PeerKind != "direct" {
		t.Fatalf("unexpected inbound message: %+v", msg)
	}
}

func TestSlackHandleMessageWithoutPolicyCheckStillUsesAllowlistSafetyNet(t *testing.T) {
	msgBus := bus.New()
	ch, err := New(config.SlackConfig{
		BotToken:  "xoxb-test",
		AppToken:  "xapp-test",
		AllowFrom: []string{"U_ALLOWED"},
		DMPolicy:  "pairing",
	}, msgBus, newSlackPolicyPairingStore(), nil)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	ch.HandleMessage("U_PAIRED", "D123", "hello", nil, map[string]string{"username": "paired"}, "direct")

	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	if msg, ok := msgBus.ConsumeInbound(ctx); ok {
		t.Fatalf("expected unverified Slack DM to drop, got %+v", msg)
	}
}
