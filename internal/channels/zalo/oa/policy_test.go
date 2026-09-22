package oa

import (
	"context"
	"testing"

	"github.com/nextlevelbuilder/goclaw/internal/bus"
	"github.com/nextlevelbuilder/goclaw/internal/config"
	"github.com/nextlevelbuilder/goclaw/internal/store"
)

type stubPairingStore struct {
	paired map[string]bool
	codes  int
}

func (s *stubPairingStore) IsPaired(_ context.Context, senderID, _ string) (bool, error) {
	return s.paired[senderID], nil
}
func (s *stubPairingStore) RequestPairing(context.Context, string, string, string, string, map[string]string) (string, error) {
	s.codes++
	return "oa-code", nil
}
func (s *stubPairingStore) ApprovePairing(context.Context, string, string) (*store.PairedDeviceData, error) {
	return nil, nil
}
func (s *stubPairingStore) DenyPairing(context.Context, string) error { return nil }
func (s *stubPairingStore) RevokePairing(context.Context, string, string) error {
	return nil
}
func (s *stubPairingStore) ListPending(context.Context) []store.PairingRequestData { return nil }
func (s *stubPairingStore) ListPaired(context.Context) []store.PairedDeviceData    { return nil }
func (s *stubPairingStore) MigrateGroupChatID(context.Context, string, string, string) error {
	return nil
}

func TestNew_DefaultsDMPolicyPairing(t *testing.T) {
	ch, err := New("oa", config.ZaloOAConfig{}, &ChannelCreds{AppID: "a", SecretKey: "s"}, &noopCIStore{}, bus.New(), nil)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if ch.cfg.DMPolicy != "pairing" {
		t.Errorf("DMPolicy = %q, want pairing", ch.cfg.DMPolicy)
	}
}

func TestAuthorizeInbound_OpenAllowsUnknownSender(t *testing.T) {
	ch, err := New("oa", config.ZaloOAConfig{DMPolicy: "open"}, &ChannelCreds{AppID: "a", SecretKey: "s"}, &noopCIStore{}, bus.New(), nil)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if !ch.authorizeInbound(context.Background(), "u1", "u1") {
		t.Fatal("open policy must allow unknown sender")
	}
}

func TestAuthorizeInbound_PairingDeniesUnknownAndRequestsCode(t *testing.T) {
	ps := &stubPairingStore{paired: map[string]bool{}}
	ch, err := New("oa", config.ZaloOAConfig{}, &ChannelCreds{AppID: "a", SecretKey: "s"}, &noopCIStore{}, bus.New(), ps)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if ch.authorizeInbound(context.Background(), "u1", "u1") {
		t.Fatal("unpaired sender must not be authorized")
	}
	if ps.codes != 1 {
		t.Fatalf("pairing codes issued = %d, want 1", ps.codes)
	}
}

func TestAuthorizeInbound_PairingAllowsPairedSender(t *testing.T) {
	ps := &stubPairingStore{paired: map[string]bool{"u1": true}}
	ch, err := New("oa", config.ZaloOAConfig{}, &ChannelCreds{AppID: "a", SecretKey: "s"}, &noopCIStore{}, bus.New(), ps)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if !ch.authorizeInbound(context.Background(), "u1", "u1") {
		t.Fatal("paired sender must be allowed")
	}
	if ps.codes != 0 {
		t.Fatalf("paired sender issued %d pairing codes", ps.codes)
	}
}

func TestQuoteInboundOnDM_DefaultOff(t *testing.T) {
	ch, err := New("oa", config.ZaloOAConfig{}, &ChannelCreds{AppID: "a", SecretKey: "s"}, &noopCIStore{}, bus.New(), nil)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if ch.QuoteInboundOnDM() {
		t.Fatal("quote_user_message default must be off")
	}
	on := true
	ch.cfg.QuoteUserMessage = &on
	if !ch.QuoteInboundOnDM() {
		t.Fatal("explicit true must opt in")
	}
}
