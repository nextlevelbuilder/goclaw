package telegram

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/mymmrac/telego"

	"github.com/nextlevelbuilder/goclaw/internal/channels"
	"github.com/nextlevelbuilder/goclaw/internal/store"
)

// tenantRecordingPairingStore records the tenant each pairing call runs under.
type tenantRecordingPairingStore struct {
	store.PairingStore
	isPairedTenant uuid.UUID
	requestTenant  uuid.UUID
}

func (s *tenantRecordingPairingStore) IsPaired(ctx context.Context, _, _ string) (bool, error) {
	s.isPairedTenant = store.TenantIDFromContext(ctx)
	return false, nil
}

func (s *tenantRecordingPairingStore) RequestPairing(ctx context.Context, _, _, _, _ string, _ map[string]string) (string, error) {
	s.requestTenant = store.TenantIDFromContext(ctx)
	return "ABCD1234", nil
}

func TestHandleMessagePairingUsesChannelTenant(t *testing.T) {
	bot, err := telego.NewBot(
		"123456:abcdefghijklmnopqrstuvwxyzABCDE1234",
		telego.WithAPICaller(&recordingTelegramCaller{}),
		telego.WithDiscardLogger(),
	)
	if err != nil {
		t.Fatalf("NewBot: %v", err)
	}

	tenantID := uuid.New()
	pairing := &tenantRecordingPairingStore{}
	ch := &Channel{
		BaseChannel: channels.NewBaseChannel("telegram", nil, nil),
		bot:         bot,
	}
	ch.SetTenantID(tenantID)
	ch.SetPairingService(pairing)

	ch.handleMessage(context.Background(), telego.Update{Message: &telego.Message{
		Chat: telego.Chat{ID: 42, Type: "private"},
		From: &telego.User{ID: 7, Username: "sender"},
		Text: "hello",
	}})

	if pairing.isPairedTenant != tenantID {
		t.Fatalf("IsPaired tenant = %s, want %s", pairing.isPairedTenant, tenantID)
	}
	if pairing.requestTenant != tenantID {
		t.Fatalf("RequestPairing tenant = %s, want %s", pairing.requestTenant, tenantID)
	}
}
