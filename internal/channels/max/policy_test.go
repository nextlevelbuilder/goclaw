package max

import (
	"context"
	"testing"

	"github.com/nextlevelbuilder/goclaw/internal/bus"
	"github.com/nextlevelbuilder/goclaw/internal/store"
)

// =====================================================================
// Helpers — fake PairingStore + factory-style channel constructor
// =====================================================================

// fakePairingStore implements store.PairingStore in-memory.
type fakePairingStore struct {
	paired       map[string]bool
	codes        map[string]string
	requestCalls int
}

func (f *fakePairingStore) IsPaired(_ context.Context, senderID, _ string) (bool, error) {
	return f.paired[senderID], nil
}
func (f *fakePairingStore) RequestPairing(_ context.Context, senderID, _, _, _ string, _ map[string]string) (string, error) {
	f.requestCalls++
	if f.codes == nil {
		f.codes = map[string]string{}
	}
	code := "TESTCODE"
	f.codes[senderID] = code
	return code, nil
}
func (f *fakePairingStore) ApprovePairing(_ context.Context, _, _ string) (*store.PairedDeviceData, error) {
	return &store.PairedDeviceData{}, nil
}
func (f *fakePairingStore) DenyPairing(_ context.Context, _ string) error              { return nil }
func (f *fakePairingStore) RevokePairing(_ context.Context, _, _ string) error         { return nil }
func (f *fakePairingStore) ListPending(_ context.Context) []store.PairingRequestData   { return nil }
func (f *fakePairingStore) ListPaired(_ context.Context) []store.PairedDeviceData      { return nil }
func (f *fakePairingStore) MigrateGroupChatID(_ context.Context, _, _, _ string) error { return nil }

// channelWithPolicy builds a Channel pointed at a mock backend with the
// policy/allowlist/pairing knobs configured.
func channelWithPolicy(t *testing.T, dmPolicy, groupPolicy string, allow []string, paired map[string]bool) (*Channel, *mockMaxBackend, *fakePairingStore) {
	t.Helper()
	m := newMockBackend(t)

	creds := instanceCreds{BotToken: "test-token", BotID: 256747471, Username: "test_bot"}
	cfg := instanceConfig{
		Mode:           "polling",
		PollingTimeout: 30,
		DMPolicy:       dmPolicy,
		GroupPolicy:    groupPolicy,
		AllowFrom:      allow,
		HistoryLimit:   50,
	}
	ps := &fakePairingStore{paired: paired}

	c, err := New("test-max", creds, cfg, bus.New(), ps, nil, nil)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	c.client = NewClient("test-token", WithBaseURL(m.server.URL), WithMaxRetries(1))
	return c, m, ps
}

func TestPolicy_DM_OpenAllows(t *testing.T) {
	c, m, _ := channelWithPolicy(t, "open", "disabled", nil, nil)
	if !c.checkDMPolicy(context.Background(), "user-1", 100) {
		t.Fatal("open dm_policy should allow any sender")
	}
	if got := len(m.captured()); got != 0 {
		t.Fatalf("open policy must not send pairing reply, got %d sends", got)
	}
}

func TestPolicy_DM_DisabledDenies(t *testing.T) {
	c, m, _ := channelWithPolicy(t, "disabled", "disabled", nil, nil)
	if c.checkDMPolicy(context.Background(), "user-1", 100) {
		t.Fatal("disabled dm_policy must reject")
	}
	if got := len(m.captured()); got != 0 {
		t.Fatalf("disabled policy must not trigger pairing reply, got %d sends", got)
	}
}

func TestPolicy_DM_AllowlistAllowsKnown(t *testing.T) {
	c, _, _ := channelWithPolicy(t, "allowlist", "disabled", []string{"user-1"}, nil)
	if !c.checkDMPolicy(context.Background(), "user-1", 100) {
		t.Fatal("allowlist should allow listed sender")
	}
}

func TestPolicy_DM_AllowlistDeniesUnknown(t *testing.T) {
	c, m, _ := channelWithPolicy(t, "allowlist", "disabled", []string{"user-1"}, nil)
	if c.checkDMPolicy(context.Background(), "user-99", 100) {
		t.Fatal("allowlist should deny non-listed sender")
	}
	if got := len(m.captured()); got != 0 {
		t.Fatalf("allowlist deny must not send pairing reply, got %d sends", got)
	}
}

func TestPolicy_DM_PairingSendsCodeForUnpaired(t *testing.T) {
	c, m, ps := channelWithPolicy(t, "pairing", "disabled", nil, nil)
	if c.checkDMPolicy(context.Background(), "user-1", 100) {
		t.Fatal("pairing policy must drop unpaired sender")
	}
	if ps.requestCalls != 1 {
		t.Fatalf("expected 1 RequestPairing call, got %d", ps.requestCalls)
	}
	if got := len(m.captured()); got != 1 {
		t.Fatalf("expected 1 SendMessage call (pairing reply), got %d", got)
	}
	if m.captured()[0].Body.Text == "" {
		t.Fatal("pairing reply text is empty")
	}
}

func TestPolicy_DM_PairingAllowsPairedSender(t *testing.T) {
	c, m, _ := channelWithPolicy(t, "pairing", "disabled", nil,
		map[string]bool{"user-1": true})
	if !c.checkDMPolicy(context.Background(), "user-1", 100) {
		t.Fatal("paired sender must be allowed under pairing policy")
	}
	if got := len(m.captured()); got != 0 {
		t.Fatalf("paired sender must not get pairing reply, got %d sends", got)
	}
}

func TestPolicy_DM_PairingAllowsAllowlistedEvenWithoutPairing(t *testing.T) {
	c, m, ps := channelWithPolicy(t, "pairing", "disabled",
		[]string{"trusted-user"}, nil)
	if !c.checkDMPolicy(context.Background(), "trusted-user", 100) {
		t.Fatal("allowlisted sender must bypass pairing requirement")
	}
	if ps.requestCalls != 0 {
		t.Fatalf("allowlisted sender must not trigger RequestPairing, got %d calls", ps.requestCalls)
	}
	if got := len(m.captured()); got != 0 {
		t.Fatalf("allowlisted sender must not get pairing reply, got %d sends", got)
	}
}

func TestPolicy_DM_PairingDebouncesRepeatedReplies(t *testing.T) {
	c, m, ps := channelWithPolicy(t, "pairing", "disabled", nil, nil)
	if c.checkDMPolicy(context.Background(), "user-1", 100) {
		t.Fatal("first call must drop unpaired sender")
	}
	if c.checkDMPolicy(context.Background(), "user-1", 100) {
		t.Fatal("second call must also drop unpaired sender")
	}
	if got := len(m.captured()); got != 1 {
		t.Fatalf("expected 1 send total (debounced), got %d", got)
	}
	if ps.requestCalls != 1 {
		t.Fatalf("expected 1 RequestPairing call total (debounced), got %d", ps.requestCalls)
	}
}

func TestPolicy_Group_DisabledDenies(t *testing.T) {
	c, m, _ := channelWithPolicy(t, "open", "disabled", nil, nil)
	if c.checkGroupPolicy(context.Background(), "user-1", 200) {
		t.Fatal("group_policy=disabled must reject")
	}
	if got := len(m.captured()); got != 0 {
		t.Fatalf("disabled group policy must not send anything, got %d sends", got)
	}
}

func TestPolicy_Group_OpenAllows(t *testing.T) {
	c, _, _ := channelWithPolicy(t, "open", "open", nil, nil)
	if !c.checkGroupPolicy(context.Background(), "user-1", 200) {
		t.Fatal("group_policy=open should allow")
	}
}

func TestPolicy_Group_AllowlistDeniesUnknown(t *testing.T) {
	c, m, _ := channelWithPolicy(t, "open", "allowlist", []string{"user-1"}, nil)
	if c.checkGroupPolicy(context.Background(), "user-99", 200) {
		t.Fatal("allowlist should deny non-listed group sender")
	}
	if got := len(m.captured()); got != 0 {
		t.Fatalf("group allowlist deny must not send anything, got %d sends", got)
	}
}
