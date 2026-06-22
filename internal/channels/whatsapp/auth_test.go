package whatsapp

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"testing"

	"go.mau.fi/whatsmeow/proto/waAdv"
	wastore "go.mau.fi/whatsmeow/store"
	"go.mau.fi/whatsmeow/store/sqlstore"
	"go.mau.fi/whatsmeow/types"
	_ "modernc.org/sqlite"

	"github.com/nextlevelbuilder/goclaw/internal/bus"
	"github.com/nextlevelbuilder/goclaw/internal/channels"
)

func TestResetClientLockedDoesNotFallbackToFirstDeviceWhenCredentialJIDMissing(t *testing.T) {
	ctx := context.Background()
	db, container := newAuthTestDB(t)
	existingJID := saveWhatsAppTestDevice(t, container, "15550000001")
	missingJID := types.NewJID("15550000002", types.DefaultUserServer)

	factory := FactoryWithDB(db, nil, "sqlite3")
	chRaw, err := factory("wa-target", []byte(fmt.Sprintf(`{"device_jid":%q}`, missingJID.String())), nil, bus.New(), nil)
	if err != nil {
		t.Fatalf("factory error = %v", err)
	}
	ch := chRaw.(*Channel)

	ch.mu.Lock()
	if err := ch.resetClientLocked(ctx); err != nil {
		ch.mu.Unlock()
		t.Fatalf("resetClientLocked() error = %v", err)
	}
	got := ch.client.Store.GetJID()
	ch.mu.Unlock()

	if got == existingJID {
		t.Fatalf("resetClientLocked() reused first device %s instead of creating an unpaired scoped device", got)
	}
	if !got.IsEmpty() {
		t.Fatalf("resetClientLocked() Store.ID = %s, want empty JID when scoped credential device is missing", got)
	}
}

func TestEnsureQRClientLockedRefreshesDeletedStore(t *testing.T) {
	ch := newAuthTestChannel(t)

	ch.mu.Lock()
	if err := ch.ensureQRClientLocked(context.Background()); err != nil {
		ch.mu.Unlock()
		t.Fatalf("ensureQRClientLocked() initial error = %v", err)
	}
	staleClient := ch.client
	ch.mu.Unlock()

	jid := types.NewJID("15551234567", types.DefaultUserServer)
	staleClient.Store.ID = &jid
	if err := staleClient.Store.Delete(context.Background()); err != nil {
		t.Fatalf("delete stale store: %v", err)
	}
	ch.lastQRMu.Lock()
	ch.waAuthenticated = true
	ch.lastQRB64 = "stale-qr"
	ch.lastQRMu.Unlock()

	ch.mu.Lock()
	if err := ch.ensureQRClientLocked(context.Background()); err != nil {
		ch.mu.Unlock()
		t.Fatalf("ensureQRClientLocked() refresh error = %v", err)
	}
	refreshedClient := ch.client
	ch.mu.Unlock()

	if refreshedClient == staleClient {
		t.Fatal("client was not refreshed after store deletion")
	}
	if refreshedClient.Store.Deleted {
		t.Fatal("refreshed client still has a deleted store")
	}
	if refreshedClient.Store.ID != nil {
		t.Fatalf("refreshed client Store.ID = %v, want nil for fresh QR login", refreshedClient.Store.ID)
	}
	if ch.IsAuthenticated() {
		t.Fatal("deleted-store refresh left channel marked authenticated")
	}
	if got := ch.GetLastQRB64(); got != "" {
		t.Fatalf("deleted-store refresh left stale QR cache = %q", got)
	}
}

func TestReauthReplacesDeletedClientStore(t *testing.T) {
	ch := newAuthTestChannel(t)

	ch.mu.Lock()
	if err := ch.ensureQRClientLocked(context.Background()); err != nil {
		ch.mu.Unlock()
		t.Fatalf("ensureQRClientLocked() initial error = %v", err)
	}
	staleClient := ch.client
	jid := types.NewJID("15557654321", types.DefaultUserServer)
	staleClient.Store.ID = &jid
	ch.mu.Unlock()

	if err := ch.Reauth(); err != nil {
		t.Fatalf("Reauth() error = %v", err)
	}

	ch.mu.Lock()
	refreshedClient := ch.client
	ch.mu.Unlock()

	if refreshedClient == staleClient {
		t.Fatal("Reauth() did not replace the client after deleting the device")
	}
	if staleClient.Store.Deleted != true {
		t.Fatal("Reauth() did not mark the previous store deleted")
	}
	if refreshedClient.Store.Deleted {
		t.Fatal("Reauth() replacement store is deleted")
	}
	if refreshedClient.Store.ID != nil {
		t.Fatalf("Reauth() replacement Store.ID = %v, want nil for QR login", refreshedClient.Store.ID)
	}
}

func TestQRStartUserMessageExplainsDeletedDeviceRecovery(t *testing.T) {
	err := fmt.Errorf("whatsapp connect for QR: %w", wastore.ErrDeviceDeleted)

	got := qrStartUserMessage(err)

	if got == err.Error() {
		t.Fatal("deleted-device QR error exposed raw upstream message")
	}
	if got == "" {
		t.Fatal("deleted-device QR error message is empty")
	}
	for _, want := range []string{"unlinked or deleted", "refresh", "start QR login again"} {
		if !strings.Contains(got, want) {
			t.Fatalf("deleted-device QR error message = %q, want to contain %q", got, want)
		}
	}
}

func newAuthTestChannel(t *testing.T) *Channel {
	t.Helper()

	_, container := newAuthTestDB(t)

	return &Channel{
		BaseChannel: channels.NewBaseChannel(channels.TypeWhatsApp, bus.New(), nil),
		container:   container,
	}
}

func newAuthTestDB(t *testing.T) (*sql.DB, *sqlstore.Container) {
	t.Helper()

	ctx := context.Background()
	dsn := "file:" + strings.NewReplacer("/", "_", " ", "_").Replace(t.Name()) + "?mode=memory&cache=shared"
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	db.SetMaxOpenConns(1)
	if _, err := db.ExecContext(ctx, "PRAGMA foreign_keys = ON"); err != nil {
		t.Fatalf("enable foreign keys: %v", err)
	}

	container := sqlstore.NewWithDB(db, "sqlite3", nil)
	if err := container.Upgrade(ctx); err != nil {
		t.Fatalf("upgrade whatsmeow store: %v", err)
	}

	return db, container
}

func saveWhatsAppTestDevice(t *testing.T, container *sqlstore.Container, user string) types.JID {
	t.Helper()

	jid := types.NewJID(user, types.DefaultUserServer)
	device := container.NewDevice()
	device.ID = &jid
	device.Account = &waAdv.ADVSignedDeviceIdentity{
		Details:             []byte{},
		AccountSignatureKey: make([]byte, 32),
		AccountSignature:    make([]byte, 64),
		DeviceSignature:     make([]byte, 64),
	}
	if err := device.Save(context.Background()); err != nil {
		t.Fatalf("save test device %s: %v", jid, err)
	}
	return jid
}
