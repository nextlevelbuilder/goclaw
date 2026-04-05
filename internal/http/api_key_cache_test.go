package http

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/nextlevelbuilder/goclaw/internal/permissions"
	"github.com/nextlevelbuilder/goclaw/internal/store"
)

// mockAPIKeyStore is a minimal mock for testing the cache layer.
type mockAPIKeyStore struct {
	mu        sync.Mutex
	keys      map[string]*store.APIKeyData // hash → key
	calls     int                          // GetByHash call count
	touchedID uuid.UUID                    // last TouchLastUsed ID
}

func newMockAPIKeyStore() *mockAPIKeyStore {
	return &mockAPIKeyStore{keys: make(map[string]*store.APIKeyData)}
}

func (m *mockAPIKeyStore) GetByHash(_ context.Context, hash string) (*store.APIKeyData, error) {
	m.mu.Lock()
	m.calls++
	m.mu.Unlock()
	if k, ok := m.keys[hash]; ok {
		return k, nil
	}
	return nil, nil
}

func (m *mockAPIKeyStore) TouchLastUsed(_ context.Context, id uuid.UUID) error {
	m.mu.Lock()
	m.touchedID = id
	m.mu.Unlock()
	return nil
}

func (m *mockAPIKeyStore) Create(_ context.Context, _ *store.APIKeyData) error { return nil }
func (m *mockAPIKeyStore) List(_ context.Context, _ string) ([]store.APIKeyData, error) {
	return nil, nil
}
func (m *mockAPIKeyStore) Revoke(_ context.Context, _ uuid.UUID, _ string) error { return nil }
func (m *mockAPIKeyStore) Delete(_ context.Context, _ uuid.UUID, _ string) error { return nil }

func (m *mockAPIKeyStore) getCalls() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.calls
}

func TestCacheMissFetchesFromStore(t *testing.T) {
	ms := newMockAPIKeyStore()
	keyID := uuid.New()
	ms.keys["hash123"] = &store.APIKeyData{
		ID:     keyID,
		Scopes: []string{"operator.read"},
	}

	c := newAPIKeyCache(ms, 5*time.Minute)
	key, role := c.getOrFetch(context.Background(), "hash123")

	if key == nil {
		t.Fatal("expected key, got nil")
	}
	if key.ID != keyID {
		t.Errorf("key ID = %v, want %v", key.ID, keyID)
	}
	if role != permissions.RoleViewer {
		t.Errorf("role = %v, want %v", role, permissions.RoleViewer)
	}
	if ms.getCalls() != 1 {
		t.Errorf("store calls = %d, want 1", ms.getCalls())
	}
}

func TestCacheHitReturnsWithoutStoreCall(t *testing.T) {
	ms := newMockAPIKeyStore()
	keyID := uuid.New()
	ms.keys["hash456"] = &store.APIKeyData{
		ID:     keyID,
		Scopes: []string{"operator.admin"},
	}

	c := newAPIKeyCache(ms, 5*time.Minute)

	// First call: cache miss → store fetch
	c.getOrFetch(context.Background(), "hash456")
	// Second call: cache hit → no store fetch
	key, role := c.getOrFetch(context.Background(), "hash456")

	if key == nil {
		t.Fatal("expected key on cache hit, got nil")
	}
	if role != permissions.RoleAdmin {
		t.Errorf("role = %v, want %v", role, permissions.RoleAdmin)
	}
	if ms.getCalls() != 1 {
		t.Errorf("store calls = %d, want 1 (cache hit should not call store)", ms.getCalls())
	}
}

func TestCacheTTLExpiry(t *testing.T) {
	ms := newMockAPIKeyStore()
	ms.keys["hash789"] = &store.APIKeyData{
		ID:     uuid.New(),
		Scopes: []string{"operator.write"},
	}

	c := newAPIKeyCache(ms, 10*time.Millisecond) // very short TTL

	// First fetch
	c.getOrFetch(context.Background(), "hash789")
	if ms.getCalls() != 1 {
		t.Fatalf("initial calls = %d, want 1", ms.getCalls())
	}

	// Wait for TTL to expire
	time.Sleep(20 * time.Millisecond)

	// Should re-fetch from store
	key, _ := c.getOrFetch(context.Background(), "hash789")
	if key == nil {
		t.Fatal("expected key after TTL expiry")
	}
	if ms.getCalls() != 2 {
		t.Errorf("store calls after TTL = %d, want 2", ms.getCalls())
	}
}

func TestCacheInvalidateAll(t *testing.T) {
	ms := newMockAPIKeyStore()
	ms.keys["hashA"] = &store.APIKeyData{
		ID:     uuid.New(),
		Scopes: []string{"operator.read"},
	}

	c := newAPIKeyCache(ms, 5*time.Minute)

	// Populate cache
	c.getOrFetch(context.Background(), "hashA")
	if ms.getCalls() != 1 {
		t.Fatalf("initial calls = %d, want 1", ms.getCalls())
	}

	// Invalidate
	c.invalidateAll()

	// Should fetch again from store
	c.getOrFetch(context.Background(), "hashA")
	if ms.getCalls() != 2 {
		t.Errorf("store calls after invalidate = %d, want 2", ms.getCalls())
	}
}

func TestCacheNegativeCache(t *testing.T) {
	ms := newMockAPIKeyStore()
	// No keys in store

	c := newAPIKeyCache(ms, 5*time.Minute)

	// First lookup: cache miss → store returns nil → negative cache
	key1, _ := c.getOrFetch(context.Background(), "unknown")
	if key1 != nil {
		t.Fatal("expected nil key for unknown hash")
	}
	if ms.getCalls() != 1 {
		t.Fatalf("initial calls = %d, want 1", ms.getCalls())
	}

	// Second lookup: negative cache hit → no store call
	key2, _ := c.getOrFetch(context.Background(), "unknown")
	if key2 != nil {
		t.Fatal("expected nil key on negative cache hit")
	}
	if ms.getCalls() != 1 {
		t.Errorf("store calls after negative cache = %d, want 1", ms.getCalls())
	}
}

func TestCacheConcurrentAccess(t *testing.T) {
	ms := newMockAPIKeyStore()
	ms.keys["concurrent"] = &store.APIKeyData{
		ID:     uuid.New(),
		Scopes: []string{"operator.admin"},
	}

	c := newAPIKeyCache(ms, 5*time.Minute)

	var wg sync.WaitGroup
	for range 50 {
		wg.Go(func() {
			key, role := c.getOrFetch(context.Background(), "concurrent")
			if key == nil {
				t.Error("expected key, got nil")
			}
			if role != permissions.RoleAdmin {
				t.Errorf("role = %v, want admin", role)
			}
		})
	}
	wg.Wait()

	// Store should have been called at least once, but not 50 times
	// (cache may have been populated after the first call)
	calls := ms.getCalls()
	if calls == 0 || calls > 50 {
		t.Errorf("store calls = %d, want 1..50", calls)
	}
}
