package bitrix24

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/nextlevelbuilder/goclaw/internal/store"
	"github.com/nextlevelbuilder/goclaw/internal/systemmessages"
)

// fakeBitrixPairingStore implements store.PairingStore for pairing-reply tests.
type fakeBitrixPairingStore struct {
	requested    bool
	requestedFor string
}

func (s *fakeBitrixPairingStore) RequestPairing(_ context.Context, senderID, _, _, _ string, _ map[string]string) (string, error) {
	s.requested = true
	s.requestedFor = senderID
	return "code123", nil
}
func (s *fakeBitrixPairingStore) ApprovePairing(context.Context, string, string) (*store.PairedDeviceData, error) {
	return nil, nil
}
func (s *fakeBitrixPairingStore) DenyPairing(context.Context, string) error           { return nil }
func (s *fakeBitrixPairingStore) RevokePairing(context.Context, string, string) error { return nil }
func (s *fakeBitrixPairingStore) IsPaired(context.Context, string, string) (bool, error) {
	return false, nil
}
func (s *fakeBitrixPairingStore) ListPending(context.Context) []store.PairingRequestData { return nil }
func (s *fakeBitrixPairingStore) ListPaired(context.Context) []store.PairedDeviceData    { return nil }
func (s *fakeBitrixPairingStore) MigrateGroupChatID(context.Context, string, string, string) error {
	return nil
}

// TestSendPairingReply_SendsCodeViaV2 verifies an unpaired sender gets a pairing
// code sent back through the v2 chat API (imbot.v2.Chat.Message.send), not the
// old v1 imbot.message.add, and not silence.
func TestSendPairingReply_SendsCodeViaV2(t *testing.T) {
	var calls int32
	var gotPath, gotMessage string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&calls, 1)
		gotPath = r.URL.Path
		_ = r.ParseForm()
		gotMessage = r.Form.Get("fields[message]")
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"result": map[string]any{"id": 999}})
	}))
	defer srv.Close()

	bc := newChannelWithBoundPortal(t, srv)
	bc.SetSystemMessages(systemmessages.NewResolver(nil))
	ps := &fakeBitrixPairingStore{}
	bc.SetPairingService(ps)

	bc.sendPairingReply(context.Background(), "42", "chat100", "direct")

	if atomic.LoadInt32(&calls) == 0 {
		t.Fatal("expected a REST send for the pairing reply, got none (bot stayed silent)")
	}
	if !strings.HasSuffix(gotPath, "/rest/imbot.v2.Chat.Message.send.json") {
		t.Errorf("pairing reply used wrong endpoint %q; want v2 imbot.v2.Chat.Message.send", gotPath)
	}
	if !ps.requested {
		t.Error("expected RequestPairing to be called to generate a code")
	}
	if !strings.Contains(gotMessage, "code123") {
		t.Errorf("pairing message should contain the code; got %q", gotMessage)
	}
}

// TestSendPairingReply_NoService_NoOp: without a pairing service the call is a
// safe no-op (no send).
func TestSendPairingReply_NoService_NoOp(t *testing.T) {
	var calls int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&calls, 1)
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"result": map[string]any{"id": 1}})
	}))
	defer srv.Close()

	bc := newChannelWithBoundPortal(t, srv) // no SetPairingService → PairingService() nil
	bc.sendPairingReply(context.Background(), "42", "chat100", "direct")

	if got := atomic.LoadInt32(&calls); got != 0 {
		t.Errorf("expected no send without a pairing service, got %d", got)
	}
}

// TestSendPairingReply_Debounced: a second call within the debounce window does
// not fire a second send.
func TestSendPairingReply_Debounced(t *testing.T) {
	var calls int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&calls, 1)
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"result": map[string]any{"id": 1}})
	}))
	defer srv.Close()

	bc := newChannelWithBoundPortal(t, srv)
	bc.SetSystemMessages(systemmessages.NewResolver(nil))
	bc.SetPairingService(&fakeBitrixPairingStore{})

	bc.sendPairingReply(context.Background(), "42", "chat100", "direct")
	bc.sendPairingReply(context.Background(), "42", "chat100", "direct") // debounced

	if got := atomic.LoadInt32(&calls); got != 1 {
		t.Errorf("expected 1 send (second debounced), got %d", got)
	}
}
