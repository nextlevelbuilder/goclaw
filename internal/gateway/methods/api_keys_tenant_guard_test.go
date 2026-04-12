package methods

import (
	"context"
	"encoding/json"
	"errors"
	"slices"
	"sync"
	"testing"

	"github.com/google/uuid"

	"github.com/nextlevelbuilder/goclaw/internal/gateway"
	"github.com/nextlevelbuilder/goclaw/internal/permissions"
	"github.com/nextlevelbuilder/goclaw/internal/store"
	"github.com/nextlevelbuilder/goclaw/pkg/protocol"
)

// Phase 0b review regression coverage: WS api_keys.revoke.
//
// The HTTP fix in internal/http/api_keys.go has its own test file, but the
// original Phase 0b WS fix (internal/gateway/methods/api_keys.go) had zero
// test coverage — and a Major bug slipped past: store.IsOwnerRole(ctx)
// always returns false in WS handlers because the router does not inject
// role into ctx. The bug was caught by code review, not tests. These tests
// lock the fix in place and cover the four critical paths.

// ---- minimal stub store ----

type stubAPIKeyStore struct {
	mu           sync.Mutex
	byID         map[uuid.UUID]*store.APIKeyData
	revokedCalls []uuid.UUID
}

func newStubAPIKeyStore() *stubAPIKeyStore {
	return &stubAPIKeyStore{byID: make(map[uuid.UUID]*store.APIKeyData)}
}

func (s *stubAPIKeyStore) Create(_ context.Context, k *store.APIKeyData) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.byID[k.ID] = k
	return nil
}

func (s *stubAPIKeyStore) Get(_ context.Context, id uuid.UUID) (*store.APIKeyData, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if k, ok := s.byID[id]; ok {
		return k, nil
	}
	return nil, errors.New("not found")
}

func (s *stubAPIKeyStore) GetByHash(_ context.Context, _ string) (*store.APIKeyData, error) {
	return nil, nil
}

func (s *stubAPIKeyStore) List(_ context.Context, _ string) ([]store.APIKeyData, error) {
	return nil, nil
}

func (s *stubAPIKeyStore) Revoke(_ context.Context, id uuid.UUID, _ string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.revokedCalls = append(s.revokedCalls, id)
	if k, ok := s.byID[id]; ok {
		k.Revoked = true
	}
	return nil
}

func (s *stubAPIKeyStore) TouchLastUsed(_ context.Context, _ uuid.UUID) error { return nil }

func (s *stubAPIKeyStore) wasRevoked(id uuid.UUID) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return slices.Contains(s.revokedCalls, id)
}

// wsCallCtx mirrors what router.handleRequest injects into ctx for method
// handlers: locale, tenantID (from client.TenantID()), and role (added
// by Phase 0b hardening). Tests bypass the router, so we reproduce the
// minimum set the handler reads.
func wsCallCtx(client *gateway.Client) context.Context {
	ctx := context.Background()
	if tid := client.TenantID(); tid != uuid.Nil {
		ctx = store.WithTenantID(ctx, tid)
	}
	if role := client.Role(); role != "" {
		ctx = store.WithRole(ctx, string(role))
	}
	return ctx
}

// ---- harness ----

func buildRevokeRequest(t *testing.T, keyID uuid.UUID) *protocol.RequestFrame {
	t.Helper()
	raw, err := json.Marshal(map[string]string{"id": keyID.String()})
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	return &protocol.RequestFrame{
		Type:   protocol.FrameTypeRequest,
		ID:     "revoke-req-1",
		Method: protocol.MethodAPIKeysRevoke,
		Params: raw,
	}
}

// ---- tests ----

// TestWSRevoke_TenantAdmin_CrossTenantKey_Denied is the core HIGH finding:
// a tenant admin attempting to revoke a key owned by another tenant must
// be rejected, and Revoke must not be called on the store.
func TestWSRevoke_TenantAdmin_CrossTenantKey_Denied(t *testing.T) {
	callerTID := uuid.MustParse("11111111-1111-1111-1111-111111111111")
	keyOwnerTID := uuid.MustParse("22222222-2222-2222-2222-222222222222")
	keyID := uuid.New()

	stub := newStubAPIKeyStore()
	_ = stub.Create(context.Background(), &store.APIKeyData{
		ID:       keyID,
		Name:     "other-tenant-key",
		TenantID: keyOwnerTID,
	})

	m := &APIKeysMethods{apiKeys: stub}
	client := gateway.NewTestClient(permissions.RoleAdmin, callerTID, "user-1")

	m.handleRevoke(wsCallCtx(client), client, buildRevokeRequest(t, keyID))

	if stub.wasRevoked(keyID) {
		t.Fatalf("cross-tenant key should NOT have been revoked")
	}
}

// TestWSRevoke_TenantAdmin_SystemKey_Denied is the second HIGH path: the
// NULL-tenant (system) key must not be revocable by a tenant admin.
func TestWSRevoke_TenantAdmin_SystemKey_Denied(t *testing.T) {
	callerTID := uuid.MustParse("33333333-3333-3333-3333-333333333333")
	keyID := uuid.New()

	stub := newStubAPIKeyStore()
	_ = stub.Create(context.Background(), &store.APIKeyData{
		ID:       keyID,
		Name:     "system-ci-key",
		TenantID: uuid.Nil, // NULL-tenant system key
	})

	m := &APIKeysMethods{apiKeys: stub}
	client := gateway.NewTestClient(permissions.RoleAdmin, callerTID, "user-1")

	m.handleRevoke(wsCallCtx(client), client, buildRevokeRequest(t, keyID))

	if stub.wasRevoked(keyID) {
		t.Fatalf("system (NULL-tenant) key should NOT have been revoked by tenant admin")
	}
}

// TestWSRevoke_SystemOwner_SystemKey_Allowed is the regression caught by
// code review: the original Phase 0b fix used store.IsOwnerRole(ctx),
// which returns false in WS because the router does not inject role into
// ctx. This test exercises the now-correct client.IsOwner() path.
func TestWSRevoke_SystemOwner_SystemKey_Allowed(t *testing.T) {
	keyID := uuid.New()

	stub := newStubAPIKeyStore()
	_ = stub.Create(context.Background(), &store.APIKeyData{
		ID:       keyID,
		Name:     "system-ci-key",
		TenantID: uuid.Nil,
	})

	m := &APIKeysMethods{apiKeys: stub}
	// Owner client — narrowed-to-master tenant scope is the default for
	// owner logins per router.applyTenantScope.
	client := gateway.NewTestClient(permissions.RoleOwner, store.MasterTenantID, "owner-1")

	m.handleRevoke(wsCallCtx(client), client, buildRevokeRequest(t, keyID))

	if !stub.wasRevoked(keyID) {
		t.Fatalf("system owner must be allowed to revoke NULL-tenant system key")
	}
}

// TestWSRevoke_TenantAdmin_OwnTenantKey_Allowed is the happy path: admin
// revoking a key inside their own tenant — no regression for legitimate
// scope-matching revocations.
func TestWSRevoke_TenantAdmin_OwnTenantKey_Allowed(t *testing.T) {
	tid := uuid.MustParse("44444444-4444-4444-4444-444444444444")
	keyID := uuid.New()

	stub := newStubAPIKeyStore()
	_ = stub.Create(context.Background(), &store.APIKeyData{
		ID:       keyID,
		Name:     "own-tenant-key",
		TenantID: tid,
	})

	m := &APIKeysMethods{apiKeys: stub}
	client := gateway.NewTestClient(permissions.RoleAdmin, tid, "user-1")

	m.handleRevoke(wsCallCtx(client), client, buildRevokeRequest(t, keyID))

	if !stub.wasRevoked(keyID) {
		t.Fatalf("own-tenant key must be revocable by same-tenant admin")
	}
}

// TestWSRevoke_SystemOwner_CrossTenantKey_Allowed documents the intentional
// behaviour: a system owner narrowed to the master scope can still revoke
// keys belonging to other tenants (operator-level housekeeping). If this
// is ever tightened, update both the test and the handler comment.
func TestWSRevoke_SystemOwner_CrossTenantKey_Allowed(t *testing.T) {
	otherTID := uuid.MustParse("55555555-5555-5555-5555-555555555555")
	keyID := uuid.New()

	stub := newStubAPIKeyStore()
	_ = stub.Create(context.Background(), &store.APIKeyData{
		ID:       keyID,
		Name:     "tenant-specific-key",
		TenantID: otherTID,
	})

	m := &APIKeysMethods{apiKeys: stub}
	client := gateway.NewTestClient(permissions.RoleOwner, store.MasterTenantID, "owner-1")

	m.handleRevoke(wsCallCtx(client), client, buildRevokeRequest(t, keyID))

	if !stub.wasRevoked(keyID) {
		t.Fatalf("system owner must be allowed to revoke cross-tenant keys")
	}
}
