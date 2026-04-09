package gateway

import "testing"

func TestIsOwnerID_Configured(t *testing.T) {
	ownerIDs := []string{"alice", "bob"}
	if !isOwnerID("alice", ownerIDs) {
		t.Error("alice should be owner")
	}
	if !isOwnerID("bob", ownerIDs) {
		t.Error("bob should be owner")
	}
	if isOwnerID("charlie", ownerIDs) {
		t.Error("charlie should NOT be owner")
	}
}

func TestIsOwnerID_EmptyList_FallbackToSystem(t *testing.T) {
	if !isOwnerID("system", nil) {
		t.Error("'system' should be owner when ownerIDs is nil")
	}
	if !isOwnerID("system", []string{}) {
		t.Error("'system' should be owner when ownerIDs is empty")
	}
	if isOwnerID("anyone", nil) {
		t.Error("non-system should NOT be owner when ownerIDs is nil")
	}
}

func TestIsOwnerID_EmptyUserID(t *testing.T) {
	if isOwnerID("", []string{"alice"}) {
		t.Error("empty userID should never be owner")
	}
	if isOwnerID("", nil) {
		t.Error("empty userID should never be owner (even with nil list)")
	}
}
