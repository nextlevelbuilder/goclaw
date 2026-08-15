package methods

import (
	"context"
	"testing"

	"github.com/google/uuid"

	"github.com/nextlevelbuilder/goclaw/internal/gateway"
	"github.com/nextlevelbuilder/goclaw/internal/store"
	"github.com/nextlevelbuilder/goclaw/pkg/protocol"
)

// ---- Tests: store.IsMasterScope helper ----

func TestIsMasterScopeContext_OwnerRole_Allowed(t *testing.T) {
	ctx := store.WithRole(context.Background(), store.RoleOwner)
	// Tenant set to a non-master tenant — owner role must still pass
	ctx = store.WithTenantID(ctx, uuid.MustParse("11111111-1111-1111-1111-111111111111"))
	if !store.IsMasterScope(ctx) {
		t.Fatalf("owner role with non-master tenant should be allowed")
	}
}

func TestIsMasterScopeContext_NilTenant_Allowed(t *testing.T) {
	// Legacy / system callers without tenant scope → treated as master
	ctx := context.Background()
	if !store.IsMasterScope(ctx) {
		t.Fatalf("nil tenant ctx should be allowed (master-scope fallback)")
	}
}

func TestIsMasterScopeContext_MasterTenant_Allowed(t *testing.T) {
	ctx := store.WithTenantID(context.Background(), store.MasterTenantID)
	if !store.IsMasterScope(ctx) {
		t.Fatalf("master tenant ctx should be allowed")
	}
}

func TestIsMasterScopeContext_NonMasterTenantNoOwner_Denied(t *testing.T) {
	tid := uuid.MustParse("22222222-2222-2222-2222-222222222222")
	ctx := store.WithTenantID(context.Background(), tid)
	if store.IsMasterScope(ctx) {
		t.Fatalf("non-master tenant ctx without owner role must be denied")
	}
}

func TestIsMasterScopeContext_NonMasterTenantWithOwnerRole_Allowed(t *testing.T) {
	// A system owner visiting a tenant dashboard — bypass-all allows through
	tid := uuid.MustParse("33333333-3333-3333-3333-333333333333")
	ctx := store.WithTenantID(context.Background(), tid)
	ctx = store.WithRole(ctx, store.RoleOwner)
	if !store.IsMasterScope(ctx) {
		t.Fatalf("owner role must bypass tenant scope check")
	}
}

// ---- Tests: requireMasterScope middleware ----

func configGuardRequest(method string) *protocol.RequestFrame {
	return &protocol.RequestFrame{
		Type:   protocol.FrameTypeRequest,
		ID:     "cfg-guard-req-1",
		Method: method,
	}
}

// nextCalledHandler returns a handler that flips *called to true when invoked.
func nextCalledHandler(called *bool) gateway.MethodHandler {
	return func(_ context.Context, _ *gateway.Client, _ *protocol.RequestFrame) {
		*called = true
	}
}

func TestRequireMasterScope_MasterTenant_CallsNext(t *testing.T) {
	m := &ConfigMethods{}
	var called bool
	h := m.requireMasterScope(nextCalledHandler(&called))

	ctx := store.WithTenantID(context.Background(), store.MasterTenantID)
	h(ctx, nullClient(), configGuardRequest(protocol.MethodConfigPatch))

	if !called {
		t.Fatalf("expected next handler to be called for master tenant ctx")
	}
}

func TestRequireMasterScope_NilTenant_CallsNext(t *testing.T) {
	m := &ConfigMethods{}
	var called bool
	h := m.requireMasterScope(nextCalledHandler(&called))

	h(context.Background(), nullClient(), configGuardRequest(protocol.MethodConfigGet))

	if !called {
		t.Fatalf("expected next handler to be called for nil tenant ctx (legacy compat)")
	}
}

func TestRequireMasterScope_OwnerRole_CallsNext(t *testing.T) {
	m := &ConfigMethods{}
	var called bool
	h := m.requireMasterScope(nextCalledHandler(&called))

	// System owner visiting a non-master tenant ctx — must pass
	ctx := store.WithTenantID(context.Background(), uuid.MustParse("44444444-4444-4444-4444-444444444444"))
	ctx = store.WithRole(ctx, store.RoleOwner)
	h(ctx, nullClient(), configGuardRequest(protocol.MethodConfigApply))

	if !called {
		t.Fatalf("expected next handler to be called for owner role bypass")
	}
}

func TestRequireMasterScope_NonMasterTenantNoOwner_BlocksNext(t *testing.T) {
	m := &ConfigMethods{}
	var called bool
	h := m.requireMasterScope(nextCalledHandler(&called))

	// Non-master tenant admin (non-owner role) — must be blocked
	ctx := store.WithTenantID(context.Background(), uuid.MustParse("55555555-5555-5555-5555-555555555555"))
	ctx = store.WithRole(ctx, "admin")
	h(ctx, nullClient(), configGuardRequest(protocol.MethodConfigPatch))

	if called {
		t.Fatalf("non-master tenant non-owner ctx must NOT reach next handler")
	}
}

// ---- Test: middleware chain (requireMasterScope → requireOwner → handler) ----

// TestRequireMasterScope_ChainedWithRequireOwner asserts the master-scope guard
// fires BEFORE requireOwner (which uses client.IsOwner()). Non-master tenant ctx
// must be rejected even though nullClient has no owner role set — this protects
// the downstream handler from ever touching m.cfg for a non-master caller.
func TestRequireMasterScope_ChainedWithRequireOwner(t *testing.T) {
	m := &ConfigMethods{}
	var innerCalled bool
	inner := nextCalledHandler(&innerCalled)
	chain := m.requireMasterScope(m.requireOwner(inner))

	// Non-master tenant ctx → master-scope guard rejects first
	ctx := store.WithTenantID(context.Background(), uuid.MustParse("66666666-6666-6666-6666-666666666666"))
	ctx = store.WithRole(ctx, "admin")

	// Must not panic (handler with nil m.cfg is never reached)
	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("chained middleware panicked (guard did not fire early): %v", r)
		}
	}()
	chain(ctx, nullClient(), configGuardRequest(protocol.MethodConfigPatch))

	if innerCalled {
		t.Fatalf("inner handler must not be reached when master-scope guard rejects")
	}
}
