package store

import (
	"context"
	"fmt"
	"testing"

	"github.com/google/uuid"
)

// TestCheckFileWriterPermission_NonGroupContext tests that non-group contexts bypass permission checks.
func TestCheckFileWriterPermission_NonGroupContext(t *testing.T) {
	ctx := context.Background()
	ctx = WithUserID(ctx, "user:123")        // non-group prefix
	ctx = WithSenderID(ctx, "456")
	ctx = WithAgentID(ctx, uuid.New())

	stub := &stubPermStore{}
	err := CheckFileWriterPermission(ctx, stub)
	if err != nil {
		t.Fatalf("expected nil for non-group context, got: %v", err)
	}
	if stub.listFileWritersCalled {
		t.Fatal("expected ListFileWriters not to be called for non-group context")
	}
}

// TestCheckFileWriterPermission_GroupContext_WithWriterGrant tests that writers are allowed.
func TestCheckFileWriterPermission_GroupContext_WithWriterGrant(t *testing.T) {
	agentID := uuid.New()
	groupID := "group:telegram:-100456"
	ctx := context.Background()
	ctx = WithUserID(ctx, groupID)
	ctx = WithSenderID(ctx, "789")
	ctx = WithAgentID(ctx, agentID)

	stub := &stubPermStore{
		writers: []ConfigPermission{
			{
				UserID:     "789",
				Permission: "allow",
			},
		},
	}

	err := CheckFileWriterPermission(ctx, stub)
	if err != nil {
		t.Fatalf("expected nil for writer in allowlist, got: %v", err)
	}
	if !stub.listFileWritersCalled {
		t.Fatal("expected ListFileWriters to be called")
	}
}

// TestCheckFileWriterPermission_GroupContext_WithoutWriterGrant tests that non-writers are denied.
func TestCheckFileWriterPermission_GroupContext_WithoutWriterGrant(t *testing.T) {
	agentID := uuid.New()
	groupID := "group:telegram:-100456"
	ctx := context.Background()
	ctx = WithUserID(ctx, groupID)
	ctx = WithSenderID(ctx, "789")
	ctx = WithAgentID(ctx, agentID)

	stub := &stubPermStore{
		writers: []ConfigPermission{}, // empty allowlist
	}

	err := CheckFileWriterPermission(ctx, stub)
	if err == nil {
		t.Fatal("expected error for non-writer, got nil")
	}
	if err.Error() != "permission denied: only file writers can modify files in this group. Use /addwriter to get write access" {
		t.Fatalf("unexpected error message: %v", err)
	}
}

// TestCheckFileWriterPermission_GuildContext_WithWriterGrant tests Discord guild contexts.
func TestCheckFileWriterPermission_GuildContext_WithWriterGrant(t *testing.T) {
	agentID := uuid.New()
	guildID := "guild:123456:user:789"
	ctx := context.Background()
	ctx = WithUserID(ctx, guildID)
	ctx = WithSenderID(ctx, "789")
	ctx = WithAgentID(ctx, agentID)

	stub := &stubPermStore{
		writers: []ConfigPermission{
			{
				UserID:     "789",
				Permission: "allow",
			},
		},
	}

	err := CheckFileWriterPermission(ctx, stub)
	if err != nil {
		t.Fatalf("expected nil for guild writer, got: %v", err)
	}
}

// TestCheckFileWriterPermission_NilPermStore tests that nil permStore is handled gracefully.
func TestCheckFileWriterPermission_NilPermStore(t *testing.T) {
	ctx := context.Background()
	ctx = WithUserID(ctx, "group:telegram:-100456")
	ctx = WithSenderID(ctx, "789")
	ctx = WithAgentID(ctx, uuid.New())

	err := CheckFileWriterPermission(ctx, nil)
	if err != nil {
		t.Fatalf("expected nil for nil permStore, got: %v", err)
	}
}

// TestCheckFileWriterPermission_EmptySenderID tests that empty senderID bypasses check (system context).
func TestCheckFileWriterPermission_EmptySenderID(t *testing.T) {
	ctx := context.Background()
	ctx = WithUserID(ctx, "group:telegram:-100456")
	ctx = WithSenderID(ctx, "")
	ctx = WithAgentID(ctx, uuid.New())

	stub := &stubPermStore{}
	err := CheckFileWriterPermission(ctx, stub)
	if err != nil {
		t.Fatalf("expected nil for empty senderID (system context), got: %v", err)
	}
	if stub.listFileWritersCalled {
		t.Fatal("expected ListFileWriters not to be called for empty senderID")
	}
}

// TestCheckFileWriterPermission_ErrorFromListFileWriters tests fail-open on DB errors.
func TestCheckFileWriterPermission_ErrorFromListFileWriters(t *testing.T) {
	agentID := uuid.New()
	groupID := "group:telegram:-100456"
	ctx := context.Background()
	ctx = WithUserID(ctx, groupID)
	ctx = WithSenderID(ctx, "789")
	ctx = WithAgentID(ctx, agentID)

	stub := &stubPermStore{
		listFileWritersErr: fmt.Errorf("database error"),
	}

	err := CheckFileWriterPermission(ctx, stub)
	if err != nil {
		t.Fatalf("expected nil on ListFileWriters error (fail-open), got: %v", err)
	}
}

// TestCheckFileWriterPermission_SenderIDWithPipe tests extraction of numeric ID from pipe-delimited senderID.
func TestCheckFileWriterPermission_SenderIDWithPipe(t *testing.T) {
	agentID := uuid.New()
	groupID := "group:telegram:-100456"
	ctx := context.Background()
	ctx = WithUserID(ctx, groupID)
	ctx = WithSenderID(ctx, "789|display_name")  // pipe-delimited format
	ctx = WithAgentID(ctx, agentID)

	stub := &stubPermStore{
		writers: []ConfigPermission{
			{
				UserID:     "789", // should match the extracted numeric part
				Permission: "allow",
			},
		},
	}

	err := CheckFileWriterPermission(ctx, stub)
	if err != nil {
		t.Fatalf("expected nil when numeric ID matches, got: %v", err)
	}
}

// TestCheckFileWriterPermission_WriterWithDenyPermission tests that deny permission is not allowed.
func TestCheckFileWriterPermission_WriterWithDenyPermission(t *testing.T) {
	agentID := uuid.New()
	groupID := "group:telegram:-100456"
	ctx := context.Background()
	ctx = WithUserID(ctx, groupID)
	ctx = WithSenderID(ctx, "789")
	ctx = WithAgentID(ctx, agentID)

	stub := &stubPermStore{
		writers: []ConfigPermission{
			{
				UserID:     "789",
				Permission: "deny", // not "allow"
			},
		},
	}

	err := CheckFileWriterPermission(ctx, stub)
	if err == nil {
		t.Fatal("expected error for non-allow permission, got nil")
	}
}

// stubPermStore is a test stub implementing ConfigPermissionStore.
type stubPermStore struct {
	listFileWritersCalled bool
	listFileWritersErr    error
	writers               []ConfigPermission
}

var _ ConfigPermissionStore = (*stubPermStore)(nil)

func (s *stubPermStore) CheckPermission(ctx context.Context, agentID uuid.UUID, scope, configType, userID string) (bool, error) {
	return false, fmt.Errorf("stub: CheckPermission should not be called in the fixed implementation")
}

func (s *stubPermStore) Grant(ctx context.Context, perm *ConfigPermission) error {
	return fmt.Errorf("stub: Grant not implemented")
}

func (s *stubPermStore) Revoke(ctx context.Context, agentID uuid.UUID, scope, configType, userID string) error {
	return fmt.Errorf("stub: Revoke not implemented")
}

func (s *stubPermStore) List(ctx context.Context, agentID uuid.UUID, configType, scope string) ([]ConfigPermission, error) {
	return nil, fmt.Errorf("stub: List not implemented")
}

func (s *stubPermStore) ListFileWriters(ctx context.Context, agentID uuid.UUID, scope string) ([]ConfigPermission, error) {
	s.listFileWritersCalled = true
	if s.listFileWritersErr != nil {
		return nil, s.listFileWritersErr
	}
	return s.writers, nil
}
