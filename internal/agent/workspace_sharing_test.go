package agent

import (
	"testing"

	"github.com/nextlevelbuilder/goclaw/internal/store"
)

func TestShouldShareWorkspace_NilConfig(t *testing.T) {
	l := &Loop{workspaceSharing: nil}
	if l.shouldShareWorkspace("user1", "direct") {
		t.Error("nil config should never share")
	}
}

func TestShouldShareWorkspace_SharedDM(t *testing.T) {
	l := &Loop{workspaceSharing: &store.WorkspaceSharingConfig{SharedDM: true}}

	if !l.shouldShareWorkspace("user1", "direct") {
		t.Error("shared_dm=true should share for direct peer")
	}
	if l.shouldShareWorkspace("user1", "group") {
		t.Error("shared_dm=true should NOT share for group peer")
	}
}

func TestShouldShareWorkspace_SharedGroup(t *testing.T) {
	l := &Loop{workspaceSharing: &store.WorkspaceSharingConfig{SharedGroup: true}}

	if !l.shouldShareWorkspace("group:telegram:-100", "group") {
		t.Error("shared_group=true should share for group peer")
	}
	if l.shouldShareWorkspace("user1", "direct") {
		t.Error("shared_group=true should NOT share for direct peer")
	}
}

func TestShouldShareWorkspace_SharedUsers(t *testing.T) {
	l := &Loop{workspaceSharing: &store.WorkspaceSharingConfig{
		SharedUsers: []string{"telegram:386246614", "group:telegram:-100"},
	}}

	if !l.shouldShareWorkspace("telegram:386246614", "direct") {
		t.Error("user in shared_users should share regardless of peerKind")
	}
	if !l.shouldShareWorkspace("group:telegram:-100", "group") {
		t.Error("group in shared_users should share")
	}
	if l.shouldShareWorkspace("unknown-user", "direct") {
		t.Error("user NOT in shared_users should not share")
	}
}

func TestShouldShareWorkspace_SharedUsersTakesPriority(t *testing.T) {
	// shared_dm=false, shared_group=false, but user is in shared_users
	l := &Loop{workspaceSharing: &store.WorkspaceSharingConfig{
		SharedDM:    false,
		SharedGroup: false,
		SharedUsers: []string{"special-user"},
	}}

	if !l.shouldShareWorkspace("special-user", "direct") {
		t.Error("shared_users should override shared_dm=false")
	}
	if l.shouldShareWorkspace("other-user", "direct") {
		t.Error("non-listed user should not share when shared_dm=false")
	}
}

func TestShouldShareWorkspace_UnknownPeerKind(t *testing.T) {
	l := &Loop{workspaceSharing: &store.WorkspaceSharingConfig{SharedDM: true, SharedGroup: true}}

	if l.shouldShareWorkspace("user1", "unknown") {
		t.Error("unknown peerKind should default to not sharing")
	}
	if l.shouldShareWorkspace("user1", "") {
		t.Error("empty peerKind should default to not sharing")
	}
}

// shouldShareMemory tests — independent of workspace sharing

func TestShouldShareMemory_NilConfig(t *testing.T) {
	l := &Loop{workspaceSharing: nil}
	if l.shouldShareMemory() {
		t.Error("nil config should not share memory")
	}
}

func TestShouldShareMemory_Enabled(t *testing.T) {
	l := &Loop{workspaceSharing: &store.WorkspaceSharingConfig{ShareMemory: true}}
	if !l.shouldShareMemory() {
		t.Error("share_memory=true should share memory")
	}
}

func TestShouldShareMemory_DisabledByDefault(t *testing.T) {
	l := &Loop{workspaceSharing: &store.WorkspaceSharingConfig{SharedDM: true}}
	if l.shouldShareMemory() {
		t.Error("SharedDM without ShareMemory should not share memory")
	}
}

func TestShouldShareMemory_IndependentOfWorkspace(t *testing.T) {
	// share_memory=true but no workspace sharing → memory shared, workspace per-user
	l := &Loop{workspaceSharing: &store.WorkspaceSharingConfig{ShareMemory: true}}
	if !l.shouldShareMemory() {
		t.Error("share_memory should work without workspace sharing")
	}
	if l.shouldShareWorkspace("user1", "direct") {
		t.Error("workspace should NOT be shared when only share_memory is set")
	}

	// workspace shared but share_memory=false → workspace shared, memory per-user
	l2 := &Loop{workspaceSharing: &store.WorkspaceSharingConfig{SharedDM: true, ShareMemory: false}}
	if l2.shouldShareMemory() {
		t.Error("memory should NOT be shared when share_memory=false")
	}
	if !l2.shouldShareWorkspace("user1", "direct") {
		t.Error("workspace should be shared when SharedDM=true")
	}
}

func TestShouldShareWorkspace_BothEnabled(t *testing.T) {
	l := &Loop{workspaceSharing: &store.WorkspaceSharingConfig{
		SharedDM:    true,
		SharedGroup: true,
		SharedUsers: []string{"extra-user"},
	}}

	if !l.shouldShareWorkspace("user1", "direct") {
		t.Error("should share DM")
	}
	if !l.shouldShareWorkspace("group:tg:-100", "group") {
		t.Error("should share group")
	}
	if !l.shouldShareWorkspace("extra-user", "direct") {
		t.Error("should share listed user")
	}
}
