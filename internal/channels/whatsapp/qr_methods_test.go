package whatsapp

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"go.mau.fi/whatsmeow"
	wastore "go.mau.fi/whatsmeow/store"
	"go.mau.fi/whatsmeow/types"

	"github.com/nextlevelbuilder/goclaw/internal/bus"
	"github.com/nextlevelbuilder/goclaw/internal/channels"
	"github.com/nextlevelbuilder/goclaw/internal/store"
)

type recordingWhatsAppInstanceStore struct {
	store.ChannelInstanceStore
	updatedID uuid.UUID
	updates   map[string]any
}

func (s *recordingWhatsAppInstanceStore) Update(_ context.Context, id uuid.UUID, updates map[string]any) error {
	s.updatedID = id
	s.updates = updates
	return nil
}

func TestPersistDeviceJIDStoresAuthenticatedDeviceInCredentials(t *testing.T) {
	instanceID := uuid.New()
	jid := types.NewJID("15550005555", types.DefaultUserServer)
	rec := &recordingWhatsAppInstanceStore{}
	methods := &QRMethods{instanceStore: rec}
	wa := &Channel{
		BaseChannel: channels.NewBaseChannel(channels.TypeWhatsApp, bus.New(), nil),
		client:      &whatsmeow.Client{Store: &wastore.Device{ID: &jid}},
	}

	if err := methods.persistDeviceJID(context.Background(), instanceID, wa); err != nil {
		t.Fatalf("persistDeviceJID() error = %v", err)
	}

	if rec.updatedID != instanceID {
		t.Fatalf("updatedID = %s, want %s", rec.updatedID, instanceID)
	}
	creds, ok := rec.updates["credentials"].(map[string]any)
	if !ok {
		t.Fatalf("credentials update = %#v, want map[string]any", rec.updates["credentials"])
	}
	if got := creds["device_jid"]; got != jid.String() {
		t.Fatalf("device_jid = %v, want %s", got, jid.String())
	}
}
