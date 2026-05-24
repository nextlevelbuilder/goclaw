package bitrix24

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/nextlevelbuilder/goclaw/internal/store"
)

// seedOrphanPortal preps a fake portal store with a portal row that has
// credentials + (optional) state.RegisteredBots. Returns the store + tenant
// for the test to drive DestroyOrphanBot against.
//
// Tests below cover the no-op + error branches of DestroyOrphanBot. The
// happy-path (imbot.unregister actually fires) is exercised indirectly via
// TestDestroy_FullFlow in register_idempotency_test.go which uses the
// Channel.Destroy code path that shares the same unregisterBot helper.
// DestroyOrphanBot internally builds a fresh Portal whose Client would hit
// the real Bitrix domain — wiring a test transport into that internal
// construction would require either exporting client surface or duplicating
// the entire orchestration; we skip both per KISS.
func seedOrphanPortal(t *testing.T, registeredBots map[string]int) (*fakeBitrixStore, uuid.UUID) {
	t.Helper()
	fs := newFakeStore()
	tid := store.GenNewID()
	creds, _ := json.Marshal(store.BitrixPortalCredentials{ClientID: "cid", ClientSecret: "secret"})

	stateJSON, _ := json.Marshal(store.BitrixPortalState{
		RefreshToken:   "RT",
		AccessToken:    "AT",
		ExpiresAt:      time.Now().Add(24 * time.Hour),
		RegisteredBots: registeredBots,
	})
	fs.seed(tid, "p", "portal.bitrix24.com", creds, stateJSON)
	return fs, tid
}

// TestDestroyOrphanBot_NilConfig — no-op safety.
func TestDestroyOrphanBot_NilConfig(t *testing.T) {
	fs := newFakeStore()
	if err := DestroyOrphanBot(context.Background(), fs, "", uuid.New(), nil); err != nil {
		t.Errorf("expected nil for empty config, got %v", err)
	}
}

// TestDestroyOrphanBot_NilStore — explicit error so wiring bugs surface
// loudly instead of silent skip.
func TestDestroyOrphanBot_NilStore(t *testing.T) {
	cfg, _ := json.Marshal(map[string]string{"portal": "p", "bot_code": "b"})
	if err := DestroyOrphanBot(context.Background(), nil, "", uuid.New(), cfg); err == nil {
		t.Fatal("expected error for nil store, got nil")
	}
}

// TestDestroyOrphanBot_MissingPortalName — config without portal/bot_code →
// no-op (channel was never functional, nothing to clean).
func TestDestroyOrphanBot_MissingPortalName(t *testing.T) {
	fs := newFakeStore()
	cfg, _ := json.Marshal(map[string]string{"bot_code": "support"})
	if err := DestroyOrphanBot(context.Background(), fs, "", uuid.New(), cfg); err != nil {
		t.Errorf("expected nil for missing portal field, got %v", err)
	}
}

// TestDestroyOrphanBot_MissingBotCode — same as above for bot_code.
func TestDestroyOrphanBot_MissingBotCode(t *testing.T) {
	fs := newFakeStore()
	cfg, _ := json.Marshal(map[string]string{"portal": "p"})
	if err := DestroyOrphanBot(context.Background(), fs, "", uuid.New(), cfg); err != nil {
		t.Errorf("expected nil for missing bot_code field, got %v", err)
	}
}

// TestDestroyOrphanBot_PortalNotInStore — store returns "not found" →
// helper surfaces the error so caller can log + decide.
func TestDestroyOrphanBot_PortalNotInStore(t *testing.T) {
	fs := newFakeStore() // empty
	cfg, _ := json.Marshal(map[string]string{"portal": "ghost", "bot_code": "support"})
	if err := DestroyOrphanBot(context.Background(), fs, "", uuid.New(), cfg); err == nil {
		t.Fatal("expected error when portal row doesn't exist, got nil")
	}
}

// TestDestroyOrphanBot_NoBotRegistered — portal exists but the bot_code
// was never registered (RegisteredBots map empty or missing key) → no-op.
func TestDestroyOrphanBot_NoBotRegistered(t *testing.T) {
	fs, tid := seedOrphanPortal(t, nil) // no bots registered
	cfg, _ := json.Marshal(map[string]string{"portal": "p", "bot_code": "never_registered"})
	if err := DestroyOrphanBot(context.Background(), fs, "", tid, cfg); err != nil {
		t.Errorf("expected nil when bot was never registered, got %v", err)
	}
}

// TestDestroyOrphanBot_BadJSON — malformed config → decode error surfaces.
func TestDestroyOrphanBot_BadJSON(t *testing.T) {
	fs := newFakeStore()
	if err := DestroyOrphanBot(context.Background(), fs, "", uuid.New(), []byte(`{not json`)); err == nil {
		t.Fatal("expected decode error, got nil")
	}
}
