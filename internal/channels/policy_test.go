package channels

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/nextlevelbuilder/goclaw/internal/store"
)

// mockPairingStore is a test implementation of store.PairingStore.
type mockPairingStore struct {
	pairedDevices map[string]map[string]bool // senderID -> channel -> paired
	failIsPaired  bool                        // force IsPaired to return error
}

func newMockPairingStore() *mockPairingStore {
	return &mockPairingStore{
		pairedDevices: make(map[string]map[string]bool),
	}
}

func (m *mockPairingStore) IsPaired(ctx context.Context, senderID, channel string) (bool, error) {
	if m.failIsPaired {
		return false, errors.New("pairing service error")
	}
	if m.pairedDevices[senderID] == nil {
		return false, nil
	}
	return m.pairedDevices[senderID][channel], nil
}

func (m *mockPairingStore) RequestPairing(ctx context.Context, senderID, channel, chatID, accountID string, metadata map[string]string) (string, error) {
	return "code123", nil
}

func (m *mockPairingStore) ApprovePairing(ctx context.Context, code, approvedBy string) (*store.PairedDeviceData, error) {
	return nil, nil
}

func (m *mockPairingStore) DenyPairing(ctx context.Context, code string) error {
	return nil
}

func (m *mockPairingStore) RevokePairing(ctx context.Context, senderID, channel string) error {
	return nil
}

func (m *mockPairingStore) ListPending(ctx context.Context) []store.PairingRequestData {
	return nil
}

func (m *mockPairingStore) ListPaired(ctx context.Context) []store.PairedDeviceData {
	return nil
}

func (m *mockPairingStore) MigrateGroupChatID(ctx context.Context, channel, oldChatID, newChatID string) error {
	return nil
}

// setPaired sets up a mock paired relationship.
func (m *mockPairingStore) setPaired(senderID, channel string) {
	if m.pairedDevices[senderID] == nil {
		m.pairedDevices[senderID] = make(map[string]bool)
	}
	m.pairedDevices[senderID][channel] = true
}

// TestCheckDMPolicy_PolicyDisabled rejects all messages.
func TestCheckDMPolicy_PolicyDisabled(t *testing.T) {
	bc := NewBaseChannel("test", nil, nil)
	ctx := context.Background()

	result := bc.CheckDMPolicy(ctx, "user123", "disabled")

	if result != PolicyDeny {
		t.Errorf("CheckDMPolicy(disabled) = %v; want PolicyDeny", result)
	}
}

// TestCheckDMPolicy_PolicyOpen accepts all messages.
func TestCheckDMPolicy_PolicyOpen(t *testing.T) {
	bc := NewBaseChannel("test", nil, nil)
	ctx := context.Background()

	result := bc.CheckDMPolicy(ctx, "unknown_user", "open")

	if result != PolicyAllow {
		t.Errorf("CheckDMPolicy(open) = %v; want PolicyAllow", result)
	}
}

// TestCheckDMPolicy_PolicyAllowlist checks allowlist membership.
func TestCheckDMPolicy_PolicyAllowlist(t *testing.T) {
	tests := []struct {
		name       string
		senderID   string
		allowList  []string
		wantResult PolicyResult
	}{
		{
			name:       "Sender in allowlist is allowed",
			senderID:   "user123",
			allowList:  []string{"user123", "user456"},
			wantResult: PolicyAllow,
		},
		{
			name:       "Sender not in allowlist is denied",
			senderID:   "user999",
			allowList:  []string{"user123", "user456"},
			wantResult: PolicyDeny,
		},
		{
			name:       "Empty allowlist allows all (no restrictions)",
			senderID:   "anyone",
			allowList:  []string{},
			wantResult: PolicyAllow, // empty allowlist = no allowlist restrictions
		},
		{
			name:       "Username with @ prefix is matched",
			senderID:   "user123",
			allowList:  []string{"@user123"},
			wantResult: PolicyAllow,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			bc := NewBaseChannel("test", nil, tt.allowList)
			ctx := context.Background()

			result := bc.CheckDMPolicy(ctx, tt.senderID, "allowlist")

			if result != tt.wantResult {
				t.Errorf("CheckDMPolicy(allowlist) with allowList=%v = %v; want %v", tt.allowList, result, tt.wantResult)
			}
		})
	}
}

// TestCheckDMPolicy_PolicyPairing checks pairing status.
func TestCheckDMPolicy_PolicyPairing(t *testing.T) {
	tests := []struct {
		name              string
		senderID          string
		allowList         []string
		paired            bool
		failPairingCheck  bool
		wantResult        PolicyResult
	}{
		{
			name:             "Paired sender is allowed",
			senderID:         "user123",
			allowList:        []string{},
			paired:           true,
			failPairingCheck: false,
			wantResult:       PolicyAllow,
		},
		{
			name:             "Unpaired sender needs pairing",
			senderID:         "user999",
			allowList:        []string{},
			paired:           false,
			failPairingCheck: false,
			wantResult:       PolicyNeedsPairing,
		},
		{
			name:             "Sender in allowlist bypasses pairing",
			senderID:         "user456",
			allowList:        []string{"user456"},
			paired:           false,
			failPairingCheck: false,
			wantResult:       PolicyAllow,
		},
		{
			name:             "Pairing service error allows message (fail-open)",
			senderID:         "user999",
			allowList:        []string{},
			paired:           false,
			failPairingCheck: true,
			wantResult:       PolicyAllow,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			bc := NewBaseChannel("test", nil, tt.allowList)
			ps := newMockPairingStore()
			ps.failIsPaired = tt.failPairingCheck

			if tt.paired {
				ps.setPaired(tt.senderID, "test")
			}

			bc.SetPairingService(ps)
			ctx := context.Background()

			result := bc.CheckDMPolicy(ctx, tt.senderID, "pairing")

			if result != tt.wantResult {
				t.Errorf("CheckDMPolicy(pairing) = %v; want %v", result, tt.wantResult)
			}
		})
	}
}

// TestCheckDMPolicy_DefaultToPairing verifies empty policy defaults to "pairing".
func TestCheckDMPolicy_DefaultToPairing(t *testing.T) {
	bc := NewBaseChannel("test", nil, nil)
	ps := newMockPairingStore()
	bc.SetPairingService(ps)
	ctx := context.Background()

	result := bc.CheckDMPolicy(ctx, "unpaired_user", "")

	if result != PolicyNeedsPairing {
		t.Errorf("CheckDMPolicy with empty policy defaults to pairing, got %v; want PolicyNeedsPairing", result)
	}
}

// TestCheckGroupPolicy_PolicyDisabled rejects group messages.
func TestCheckGroupPolicy_PolicyDisabled(t *testing.T) {
	bc := NewBaseChannel("test", nil, nil)
	ctx := context.Background()

	result := bc.CheckGroupPolicy(ctx, "user123", "chat_group_1", "disabled")

	if result != PolicyDeny {
		t.Errorf("CheckGroupPolicy(disabled) = %v; want PolicyDeny", result)
	}
}

// TestCheckGroupPolicy_PolicyOpen accepts all group messages.
func TestCheckGroupPolicy_PolicyOpen(t *testing.T) {
	bc := NewBaseChannel("test", nil, nil)
	ctx := context.Background()

	result := bc.CheckGroupPolicy(ctx, "anyone", "chat_group_1", "open")

	if result != PolicyAllow {
		t.Errorf("CheckGroupPolicy(open) = %v; want PolicyAllow", result)
	}
}

// TestCheckGroupPolicy_PolicyAllowlist checks sender's allowlist membership.
func TestCheckGroupPolicy_PolicyAllowlist(t *testing.T) {
	tests := []struct {
		name       string
		senderID   string
		allowList  []string
		wantResult PolicyResult
	}{
		{
			name:       "Sender in allowlist is allowed",
			senderID:   "user123",
			allowList:  []string{"user123", "user456"},
			wantResult: PolicyAllow,
		},
		{
			name:       "Sender not in allowlist is denied",
			senderID:   "user999",
			allowList:  []string{"user123", "user456"},
			wantResult: PolicyDeny,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			bc := NewBaseChannel("test", nil, tt.allowList)
			ctx := context.Background()

			result := bc.CheckGroupPolicy(ctx, tt.senderID, "chat_group_1", "allowlist")

			if result != tt.wantResult {
				t.Errorf("CheckGroupPolicy(allowlist) = %v; want %v", result, tt.wantResult)
			}
		})
	}
}

// TestCheckGroupPolicy_PolicyPairing checks group pairing status.
func TestCheckGroupPolicy_PolicyPairing(t *testing.T) {
	tests := []struct {
		name             string
		senderID         string
		chatID           string
		allowList        []string
		groupApproved    bool
		paired           bool
		failPairingCheck bool
		wantResult       PolicyResult
	}{
		{
			name:          "Sender in allowlist bypasses pairing check",
			senderID:      "user123",
			chatID:        "chat_1",
			allowList:     []string{"user123"},
			groupApproved: false,
			paired:        false,
			wantResult:    PolicyAllow,
		},
		{
			name:          "Group already approved allows message",
			senderID:      "user999",
			chatID:        "chat_1",
			allowList:     []string{},
			groupApproved: true,
			paired:        false,
			wantResult:    PolicyAllow,
		},
		{
			name:          "Unpaired group needs pairing",
			senderID:      "user999",
			chatID:        "chat_new",
			allowList:     []string{},
			groupApproved: false,
			paired:        false,
			wantResult:    PolicyNeedsPairing,
		},
		{
			name:             "Pairing service error allows (fail-open)",
			senderID:         "user999",
			chatID:           "chat_2",
			allowList:        []string{},
			groupApproved:    false,
			paired:           false,
			failPairingCheck: true,
			wantResult:       PolicyAllow,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			bc := NewBaseChannel("test", nil, tt.allowList)
			ps := newMockPairingStore()
			ps.failIsPaired = tt.failPairingCheck

			// For pairing policy, groups are keyed as "group:<chatID>"
			if tt.paired {
				ps.setPaired("group:"+tt.chatID, "test")
			}

			bc.SetPairingService(ps)

			if tt.groupApproved {
				bc.MarkGroupApproved(tt.chatID)
			}

			ctx := context.Background()
			result := bc.CheckGroupPolicy(ctx, tt.senderID, tt.chatID, "pairing")

			if result != tt.wantResult {
				t.Errorf("CheckGroupPolicy(pairing) = %v; want %v", result, tt.wantResult)
			}
		})
	}
}

// TestCheckGroupPolicy_DefaultToOpen verifies empty policy defaults to "open".
func TestCheckGroupPolicy_DefaultToOpen(t *testing.T) {
	bc := NewBaseChannel("test", nil, nil)
	ctx := context.Background()

	result := bc.CheckGroupPolicy(ctx, "anyone", "chat_1", "")

	if result != PolicyAllow {
		t.Errorf("CheckGroupPolicy with empty policy defaults to open, got %v; want PolicyAllow", result)
	}
}

// TestCanSendPairingNotif_RespectDebounce tests debounce behavior.
func TestCanSendPairingNotif_RespectDebounce(t *testing.T) {
	bc := NewBaseChannel("test", nil, nil)
	senderID := "user123"
	debounce := 100 * time.Millisecond

	// First call should always return true (no prior send)
	if !bc.CanSendPairingNotif(senderID, debounce) {
		t.Error("First CanSendPairingNotif should return true")
	}

	// Record the send
	bc.MarkPairingNotifSent(senderID)

	// Immediate second call should return false (within debounce window)
	if bc.CanSendPairingNotif(senderID, debounce) {
		t.Error("Second CanSendPairingNotif within debounce window should return false")
	}

	// After debounce expires, should return true again
	time.Sleep(debounce + 10*time.Millisecond)
	if !bc.CanSendPairingNotif(senderID, debounce) {
		t.Error("CanSendPairingNotif after debounce should return true")
	}
}

// TestCanSendPairingNotif_IndependentPerSender tests debounce is per-sender.
func TestCanSendPairingNotif_IndependentPerSender(t *testing.T) {
	bc := NewBaseChannel("test", nil, nil)
	debounce := 100 * time.Millisecond

	user1 := "user1"
	user2 := "user2"

	// Mark user1 as sent
	bc.MarkPairingNotifSent(user1)

	// user2 should not be affected
	if !bc.CanSendPairingNotif(user2, debounce) {
		t.Error("Different senders should have independent debounce windows")
	}

	// user1 should still be in debounce
	if bc.CanSendPairingNotif(user1, debounce) {
		t.Error("user1 should still be in debounce window")
	}
}

// TestIsGroupApproved_CachesBehavior tests group approval cache.
func TestIsGroupApproved_CachesBehavior(t *testing.T) {
	bc := NewBaseChannel("test", nil, nil)
	chatID := "chat_group_123"

	// Initially not approved
	if bc.IsGroupApproved(chatID) {
		t.Error("Group should not be approved initially")
	}

	// Mark as approved
	bc.MarkGroupApproved(chatID)

	// Now should be approved
	if !bc.IsGroupApproved(chatID) {
		t.Error("Group should be approved after MarkGroupApproved")
	}

	// Clear approval
	bc.ClearGroupApproval(chatID)

	// Should be not approved again
	if bc.IsGroupApproved(chatID) {
		t.Error("Group should not be approved after ClearGroupApproval")
	}
}

// TestIsGroupApproved_MultipleGroups verifies independent cache per group.
func TestIsGroupApproved_MultipleGroups(t *testing.T) {
	bc := NewBaseChannel("test", nil, nil)

	chat1 := "chat_1"
	chat2 := "chat_2"

	// Approve only chat1
	bc.MarkGroupApproved(chat1)

	if !bc.IsGroupApproved(chat1) {
		t.Error("chat1 should be approved")
	}

	if bc.IsGroupApproved(chat2) {
		t.Error("chat2 should not be approved")
	}

	// Approve chat2 as well
	bc.MarkGroupApproved(chat2)

	if !bc.IsGroupApproved(chat1) {
		t.Error("chat1 should still be approved")
	}

	if !bc.IsGroupApproved(chat2) {
		t.Error("chat2 should now be approved")
	}
}

// TestCheckGroupPolicy_PairingMarksGroupApproved verifies groups are cached after pairing.
func TestCheckGroupPolicy_PairingMarksGroupApproved(t *testing.T) {
	bc := NewBaseChannel("test", nil, nil)
	ps := newMockPairingStore()
	bc.SetPairingService(ps)

	chatID := "chat_group_1"

	// Pair the group
	ps.setPaired("group:"+chatID, "test")

	ctx := context.Background()

	// First check should succeed and cache the approval
	result := bc.CheckGroupPolicy(ctx, "user999", chatID, "pairing")
	if result != PolicyAllow {
		t.Errorf("First CheckGroupPolicy with paired group = %v; want PolicyAllow", result)
	}

	// Verify group is now cached as approved
	if !bc.IsGroupApproved(chatID) {
		t.Error("Group should be cached as approved after successful pairing check")
	}
}

// TestCheckDMPolicy_AllPolicies_TableDriven comprehensive table test.
func TestCheckDMPolicy_AllPolicies_TableDriven(t *testing.T) {
	tests := []struct {
		name             string
		policy           string
		senderID         string
		allowList        []string
		paired           bool
		failPairingCheck bool
		wantResult       PolicyResult
	}{
		{
			name:       "disabled policy always denies",
			policy:     "disabled",
			senderID:   "user1",
			wantResult: PolicyDeny,
		},
		{
			name:       "open policy always allows",
			policy:     "open",
			senderID:   "user1",
			wantResult: PolicyAllow,
		},
		{
			name:       "allowlist policy allows if in list",
			policy:     "allowlist",
			senderID:   "user1",
			allowList:  []string{"user1", "user2"},
			wantResult: PolicyAllow,
		},
		{
			name:       "allowlist policy denies if not in list",
			policy:     "allowlist",
			senderID:   "user3",
			allowList:  []string{"user1", "user2"},
			wantResult: PolicyDeny,
		},
		{
			name:       "pairing policy allows paired users",
			policy:     "pairing",
			senderID:   "user1",
			allowList:  []string{},
			paired:     true,
			wantResult: PolicyAllow,
		},
		{
			name:       "pairing policy needs pairing for unpaired users",
			policy:     "pairing",
			senderID:   "user3",
			allowList:  []string{},
			paired:     false,
			wantResult: PolicyNeedsPairing,
		},
		{
			name:             "pairing policy fail-open on service error",
			policy:           "pairing",
			senderID:         "user3",
			allowList:        []string{},
			paired:           false,
			failPairingCheck: true,
			wantResult:       PolicyAllow,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			bc := NewBaseChannel("test", nil, tt.allowList)

			if tt.policy == "pairing" {
				ps := newMockPairingStore()
				ps.failIsPaired = tt.failPairingCheck

				if tt.paired {
					ps.setPaired(tt.senderID, "test")
				}

				bc.SetPairingService(ps)
			}

			ctx := context.Background()
			result := bc.CheckDMPolicy(ctx, tt.senderID, tt.policy)

			if result != tt.wantResult {
				t.Errorf("CheckDMPolicy(%s) = %v; want %v", tt.policy, result, tt.wantResult)
			}
		})
	}
}

// TestCheckGroupPolicy_AllPolicies_TableDriven comprehensive table test for group policies.
func TestCheckGroupPolicy_AllPolicies_TableDriven(t *testing.T) {
	tests := []struct {
		name          string
		policy        string
		senderID      string
		chatID        string
		allowList     []string
		groupApproved bool
		paired        bool
		wantResult    PolicyResult
	}{
		{
			name:       "disabled policy always denies",
			policy:     "disabled",
			senderID:   "user1",
			chatID:     "chat1",
			wantResult: PolicyDeny,
		},
		{
			name:       "open policy always allows",
			policy:     "open",
			senderID:   "user1",
			chatID:     "chat1",
			wantResult: PolicyAllow,
		},
		{
			name:       "allowlist policy allows if sender in list",
			policy:     "allowlist",
			senderID:   "user1",
			chatID:     "chat1",
			allowList:  []string{"user1", "user2"},
			wantResult: PolicyAllow,
		},
		{
			name:       "allowlist policy denies if sender not in list",
			policy:     "allowlist",
			senderID:   "user3",
			chatID:     "chat1",
			allowList:  []string{"user1", "user2"},
			wantResult: PolicyDeny,
		},
		{
			name:          "pairing policy allows if sender in allowlist",
			policy:        "pairing",
			senderID:      "user1",
			chatID:        "chat1",
			allowList:     []string{"user1"},
			groupApproved: false,
			paired:        false,
			wantResult:    PolicyAllow,
		},
		{
			name:          "pairing policy allows if group already approved",
			policy:        "pairing",
			senderID:      "user3",
			chatID:        "chat1",
			allowList:     []string{},
			groupApproved: true,
			paired:        false,
			wantResult:    PolicyAllow,
		},
		{
			name:          "pairing policy allows if group paired",
			policy:        "pairing",
			senderID:      "user3",
			chatID:        "chat1",
			allowList:     []string{},
			groupApproved: false,
			paired:        true,
			wantResult:    PolicyAllow,
		},
		{
			name:          "pairing policy needs pairing if group not approved or paired",
			policy:        "pairing",
			senderID:      "user3",
			chatID:        "chat_new",
			allowList:     []string{},
			groupApproved: false,
			paired:        false,
			wantResult:    PolicyNeedsPairing,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			bc := NewBaseChannel("test", nil, tt.allowList)

			if tt.groupApproved {
				bc.MarkGroupApproved(tt.chatID)
			}

			if tt.policy == "pairing" {
				ps := newMockPairingStore()

				if tt.paired {
					ps.setPaired("group:"+tt.chatID, "test")
				}

				bc.SetPairingService(ps)
			}

			ctx := context.Background()
			result := bc.CheckGroupPolicy(ctx, tt.senderID, tt.chatID, tt.policy)

			if result != tt.wantResult {
				t.Errorf("CheckGroupPolicy(%s) = %v; want %v", tt.policy, result, tt.wantResult)
			}
		})
	}
}
