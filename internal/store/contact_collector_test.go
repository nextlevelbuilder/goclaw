package store

import (
	"context"
	"sync"
	"testing"

	"github.com/google/uuid"
	"github.com/nextlevelbuilder/goclaw/internal/cache"
)

// mockContactStore records every UpsertContact call for assertion.
// Only implements the methods used by ContactCollector (store.ContactStore).
type mockContactStore struct {
	mu      sync.Mutex
	upserts []mockUpsertCall
}

type mockUpsertCall struct {
	tenantID        uuid.UUID
	channelType     string
	channelInstance string
	senderID        string
	userID          string
	displayName     string
	username        string
	peerKind        string
	contactType     string
	threadID        string
	threadType      string
}

func (m *mockContactStore) UpsertContact(ctx context.Context, channelType, channelInstance, senderID, userID, displayName, username, peerKind, contactType, threadID, threadType string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.upserts = append(m.upserts, mockUpsertCall{
		tenantID:        TenantIDFromContext(ctx),
		channelType:     channelType,
		channelInstance: channelInstance,
		senderID:        senderID,
		userID:          userID,
		displayName:     displayName,
		username:        username,
		peerKind:        peerKind,
		contactType:     contactType,
		threadID:        threadID,
		threadType:      threadType,
	})
	return nil
}

func (m *mockContactStore) ResolveTenantUserID(_ context.Context, _, _ string) (string, error) {
	return "", nil
}

// Stub methods to satisfy ContactStore interface (not used in these tests).
func (m *mockContactStore) ListContacts(_ context.Context, _ ContactListOpts) ([]ChannelContact, error) {
	return nil, nil
}
func (m *mockContactStore) CountContacts(_ context.Context, _ ContactListOpts) (int, error) {
	return 0, nil
}
func (m *mockContactStore) GetContactsBySenderIDs(_ context.Context, _ []string) (map[string]ChannelContact, error) {
	return nil, nil
}
func (m *mockContactStore) GetContactByID(_ context.Context, _ uuid.UUID) (*ChannelContact, error) {
	return nil, nil
}
func (m *mockContactStore) GetSenderIDsByContactIDs(_ context.Context, _ []uuid.UUID) ([]string, error) {
	return nil, nil
}
func (m *mockContactStore) MergeContacts(_ context.Context, _ []uuid.UUID, _ uuid.UUID) error {
	return nil
}
func (m *mockContactStore) UnmergeContacts(_ context.Context, _ []uuid.UUID) error { return nil }
func (m *mockContactStore) GetContactsByMergedID(_ context.Context, _ uuid.UUID) ([]ChannelContact, error) {
	return nil, nil
}

func (m *mockContactStore) upsertCount() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return len(m.upserts)
}

// TestContactCollector_SameTenantDedup verifies that repeated calls for the
// same (tenant, channel, sender, thread) only hit the store once.
func TestContactCollector_SameTenantDedup(t *testing.T) {
	mock := &mockContactStore{}
	c := NewContactCollector(mock, cache.NewInMemoryCache[bool]())

	tenant := uuid.New()
	ctx := WithTenantID(context.Background(), tenant)

	for i := 0; i < 5; i++ {
		c.EnsureContact(ctx, "telegram", "tg-main", "user-123", "uid-1", "Alice", "alice", "user", "user", "", "")
	}

	if got := mock.upsertCount(); got != 1 {
		t.Errorf("same-tenant dedup broken: got %d upserts, want 1", got)
	}
}

// TestContactCollector_CrossTenantIsolation verifies the core bug fix: same
// (channel, sender, thread) in DIFFERENT tenants must produce separate upserts.
// Before the fix, the cache key was missing tenantID so the second tenant's
// upsert was silently skipped, causing a cross-tenant contact leak.
func TestContactCollector_CrossTenantIsolation(t *testing.T) {
	mock := &mockContactStore{}
	c := NewContactCollector(mock, cache.NewInMemoryCache[bool]())

	tenantA := uuid.New()
	tenantB := uuid.New()
	ctxA := WithTenantID(context.Background(), tenantA)
	ctxB := WithTenantID(context.Background(), tenantB)

	// Same sender ID "user-123" in both tenants
	c.EnsureContact(ctxA, "telegram", "tg-main", "user-123", "uid-1", "Alice", "alice", "user", "user", "", "")
	c.EnsureContact(ctxB, "telegram", "tg-main", "user-123", "uid-1", "Alice", "alice", "user", "user", "", "")

	if got := mock.upsertCount(); got != 2 {
		t.Errorf("cross-tenant isolation broken: got %d upserts, want 2", got)
	}

	// Verify each tenant got its own upsert
	mock.mu.Lock()
	defer mock.mu.Unlock()
	seen := map[uuid.UUID]bool{}
	for _, u := range mock.upserts {
		seen[u.tenantID] = true
	}
	if !seen[tenantA] || !seen[tenantB] {
		t.Errorf("expected upserts for both tenants, got %+v", seen)
	}
}

// TestContactCollector_ZeroTenantID verifies Desktop edition (no tenant) still
// works with a zero/nil UUID — single-tenant semantics preserved.
func TestContactCollector_ZeroTenantID(t *testing.T) {
	mock := &mockContactStore{}
	c := NewContactCollector(mock, cache.NewInMemoryCache[bool]())

	// No tenant in context — Desktop / single-tenant mode
	ctx := context.Background()

	c.EnsureContact(ctx, "telegram", "tg", "user-1", "uid", "", "", "user", "user", "", "")
	c.EnsureContact(ctx, "telegram", "tg", "user-1", "uid", "", "", "user", "user", "", "") // dup

	if got := mock.upsertCount(); got != 1 {
		t.Errorf("zero-tenant dedup broken: got %d upserts, want 1", got)
	}
}

// TestContactCollector_DifferentThreads verifies same sender in different
// threads (same tenant) produces separate upserts.
func TestContactCollector_DifferentThreads(t *testing.T) {
	mock := &mockContactStore{}
	c := NewContactCollector(mock, cache.NewInMemoryCache[bool]())

	tenant := uuid.New()
	ctx := WithTenantID(context.Background(), tenant)

	c.EnsureContact(ctx, "slack", "ws-1", "user-1", "uid", "", "", "user", "user", "thread-A", "channel")
	c.EnsureContact(ctx, "slack", "ws-1", "user-1", "uid", "", "", "user", "user", "thread-B", "channel")

	if got := mock.upsertCount(); got != 2 {
		t.Errorf("different-thread isolation broken: got %d upserts, want 2", got)
	}
}
