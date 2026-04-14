package http

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/nextlevelbuilder/goclaw/internal/store"
)

// Phase 0b hotfix coverage:
//   - requireMasterScope unit tests (predicate surface)
//   - builtin_tools handleUpdate: CRITICAL-1 regression guard
//   - packages handleInstall / handleUninstall: CRITICAL-2 regression guard
//   - api_keys handleRevoke: HIGH finding regression guard
//
// All tests use focused harnesses that avoid DB / real installers.

// ---- helpers ----

func newMasterScopeReq(method, path string, tid uuid.UUID, role string) *http.Request {
	r := httptest.NewRequest(method, path, strings.NewReader(""))
	ctx := r.Context()
	if tid != uuid.Nil {
		ctx = store.WithTenantID(ctx, tid)
	}
	if role != "" {
		ctx = store.WithRole(ctx, role)
	}
	return r.WithContext(ctx)
}

// ---- requireMasterScope predicate tests ----

func TestRequireMasterScope_SystemOwner_Allows(t *testing.T) {
	// Owner role on arbitrary tenant must pass.
	r := newMasterScopeReq(http.MethodPut, "/test", uuid.New(), store.RoleOwner)
	w := httptest.NewRecorder()
	if !requireMasterScope(w, r) {
		t.Fatalf("system owner must be allowed, got %d: %s", w.Code, w.Body.String())
	}
}

func TestRequireMasterScope_MasterTenant_Allows(t *testing.T) {
	r := newMasterScopeReq(http.MethodPut, "/test", store.MasterTenantID, "admin")
	w := httptest.NewRecorder()
	if !requireMasterScope(w, r) {
		t.Fatalf("master tenant ctx must be allowed, got %d: %s", w.Code, w.Body.String())
	}
}

func TestRequireMasterScope_NilTenant_Allows(t *testing.T) {
	// Legacy / system callers without any tenant ctx.
	r := newMasterScopeReq(http.MethodPut, "/test", uuid.Nil, "")
	w := httptest.NewRecorder()
	if !requireMasterScope(w, r) {
		t.Fatalf("nil tenant ctx must be allowed (master-scope fallback), got %d", w.Code)
	}
}

func TestRequireMasterScope_NonMasterAdmin_Rejects(t *testing.T) {
	// The bug-fix path: non-master tenant admin must be rejected.
	tid := uuid.MustParse("aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa")
	r := newMasterScopeReq(http.MethodPut, "/test", tid, "admin")
	w := httptest.NewRecorder()
	if requireMasterScope(w, r) {
		t.Fatalf("non-master admin ctx must NOT be allowed")
	}
	if w.Code != http.StatusForbidden {
		t.Fatalf("expected 403, got %d", w.Code)
	}
	if !strings.Contains(w.Body.String(), "master") {
		t.Errorf("expected error message to mention master scope, got: %s", w.Body.String())
	}
}

// ---- CRITICAL-1: builtin_tools handleUpdate regression ----

// TestBuiltinToolsUpdate_RejectsNonMasterAdmin verifies that Phase 0b guard
// blocks the exact attack vector described in the audit: a tenant admin
// (RoleAdmin in a non-master tenant) attempting PUT /v1/tools/builtin/{name}
// receives 403 without any store mutation.
func TestBuiltinToolsUpdate_RejectsNonMasterAdmin(t *testing.T) {
	h := &BuiltinToolsHandler{} // store nil — must never be reached
	mux := http.NewServeMux()
	mux.HandleFunc("PUT /v1/tools/builtin/{name}", h.handleUpdate)

	tid := uuid.MustParse("bbbbbbbb-bbbb-bbbb-bbbb-bbbbbbbbbbbb")
	req := httptest.NewRequest(http.MethodPut, "/v1/tools/builtin/web_search",
		strings.NewReader(`{"enabled":true}`))
	req.Header.Set("Content-Type", "application/json")
	ctx := store.WithTenantID(req.Context(), tid)
	ctx = store.WithRole(ctx, "admin")
	req = req.WithContext(ctx)

	rec := httptest.NewRecorder()
	// If the guard fails open, h.store.Update is called on a nil store and
	// the test panics — explicit recover catches that regression distinctly.
	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("guard did not short-circuit — handler panicked on nil store: %v", r)
		}
	}()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusForbidden {
		t.Fatalf("expected 403 for non-master admin, got %d: %s", rec.Code, rec.Body.String())
	}
}

// ---- CRITICAL-2: packages handleInstall / handleUninstall regression ----

func TestPackagesInstall_RejectsNonMasterAdmin(t *testing.T) {
	h := NewPackagesHandler()
	mux := http.NewServeMux()
	mux.HandleFunc("POST /v1/packages/install", h.handleInstall)

	tid := uuid.MustParse("cccccccc-cccc-cccc-cccc-cccccccccccc")
	req := httptest.NewRequest(http.MethodPost, "/v1/packages/install",
		strings.NewReader(`{"package":"pip:malicious-pkg"}`))
	req.Header.Set("Content-Type", "application/json")
	ctx := store.WithTenantID(req.Context(), tid)
	ctx = store.WithRole(ctx, "admin")
	req = req.WithContext(ctx)

	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusForbidden {
		t.Fatalf("expected 403 for non-master admin, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestPackagesUninstall_RejectsNonMasterAdmin(t *testing.T) {
	h := NewPackagesHandler()
	mux := http.NewServeMux()
	mux.HandleFunc("POST /v1/packages/uninstall", h.handleUninstall)

	tid := uuid.MustParse("dddddddd-dddd-dddd-dddd-dddddddddddd")
	req := httptest.NewRequest(http.MethodPost, "/v1/packages/uninstall",
		strings.NewReader(`{"package":"pip:pandas"}`))
	req.Header.Set("Content-Type", "application/json")
	ctx := store.WithTenantID(req.Context(), tid)
	ctx = store.WithRole(ctx, "admin")
	req = req.WithContext(ctx)

	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusForbidden {
		t.Fatalf("expected 403 for non-master admin, got %d: %s", rec.Code, rec.Body.String())
	}
}

// ---- HIGH: api_keys handleRevoke ownership regression ----

// TestAPIKeyRevoke_RejectsCrossTenant exercises the post-fix ownership check
// in the HTTP handler. A tenant admin (non-owner) attempting to revoke a
// key owned by a different tenant must receive 403, and the store-layer
// Revoke must never be called — mockAPIKeyStore flags when it is.
func TestAPIKeyRevoke_RejectsCrossTenant(t *testing.T) {
	ms := newMockAPIKeyStore()
	callerTID := uuid.MustParse("eeeeeeee-eeee-eeee-eeee-eeeeeeeeeeee")
	keyOwnerTID := uuid.MustParse("ffffffff-ffff-ffff-ffff-ffffffffffff")
	keyID := uuid.New()
	ms.byID[keyID] = &store.APIKeyData{
		ID:       keyID,
		Name:     "other-tenant-key",
		TenantID: keyOwnerTID,
	}

	h := &APIKeysHandler{apiKeys: ms}
	mux := http.NewServeMux()
	mux.HandleFunc("POST /v1/api-keys/{id}/revoke", h.handleRevoke)

	req := httptest.NewRequest(http.MethodPost, "/v1/api-keys/"+keyID.String()+"/revoke", nil)
	ctx := store.WithTenantID(req.Context(), callerTID)
	ctx = store.WithRole(ctx, "admin")
	req = req.WithContext(ctx)

	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusForbidden {
		t.Fatalf("expected 403 for cross-tenant revoke, got %d: %s", rec.Code, rec.Body.String())
	}
}

// TestAPIKeyRevoke_RejectsSystemKeyForTenantAdmin verifies the NULL-tenant
// (system-level) key cannot be revoked by a tenant admin — the core HIGH
// vulnerability from the audit (store SQL `OR tenant_id IS NULL` arm).
func TestAPIKeyRevoke_RejectsSystemKeyForTenantAdmin(t *testing.T) {
	ms := newMockAPIKeyStore()
	callerTID := uuid.MustParse("11111111-1111-1111-1111-111111111111")
	keyID := uuid.New()
	// Key with TenantID == uuid.Nil represents a NULL-tenant system key.
	ms.byID[keyID] = &store.APIKeyData{
		ID:       keyID,
		Name:     "system-ci-key",
		TenantID: uuid.Nil,
	}

	h := &APIKeysHandler{apiKeys: ms}
	mux := http.NewServeMux()
	mux.HandleFunc("POST /v1/api-keys/{id}/revoke", h.handleRevoke)

	req := httptest.NewRequest(http.MethodPost, "/v1/api-keys/"+keyID.String()+"/revoke", nil)
	ctx := store.WithTenantID(req.Context(), callerTID)
	ctx = store.WithRole(ctx, "admin")
	req = req.WithContext(ctx)

	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusForbidden {
		t.Fatalf("expected 403 for tenant admin revoking system key, got %d: %s", rec.Code, rec.Body.String())
	}
}

// TestAPIKeyRevoke_AllowsOwnTenantKey happy path — tenant admin revoking a
// key owned by their own tenant must succeed (no regression).
func TestAPIKeyRevoke_AllowsOwnTenantKey(t *testing.T) {
	ms := newMockAPIKeyStore()
	tid := uuid.MustParse("22222222-2222-2222-2222-222222222222")
	keyID := uuid.New()
	ms.byID[keyID] = &store.APIKeyData{
		ID:       keyID,
		Name:     "own-tenant-key",
		TenantID: tid,
	}

	h := &APIKeysHandler{apiKeys: ms}
	mux := http.NewServeMux()
	mux.HandleFunc("POST /v1/api-keys/{id}/revoke", h.handleRevoke)

	req := httptest.NewRequest(http.MethodPost, "/v1/api-keys/"+keyID.String()+"/revoke", nil)
	ctx := store.WithTenantID(req.Context(), tid)
	ctx = store.WithRole(ctx, "admin")
	req = req.WithContext(ctx)

	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 for same-tenant revoke, got %d: %s", rec.Code, rec.Body.String())
	}
}

// TestAPIKeyRevoke_AllowsSystemOwnerOnSystemKey happy path — system owner
// can revoke NULL-tenant system keys (CI/CD rotation).
func TestAPIKeyRevoke_AllowsSystemOwnerOnSystemKey(t *testing.T) {
	ms := newMockAPIKeyStore()
	keyID := uuid.New()
	ms.byID[keyID] = &store.APIKeyData{
		ID:       keyID,
		Name:     "system-ci-key",
		TenantID: uuid.Nil,
	}

	h := &APIKeysHandler{apiKeys: ms}
	mux := http.NewServeMux()
	mux.HandleFunc("POST /v1/api-keys/{id}/revoke", h.handleRevoke)

	req := httptest.NewRequest(http.MethodPost, "/v1/api-keys/"+keyID.String()+"/revoke", nil)
	ctx := store.WithRole(req.Context(), store.RoleOwner)
	req = req.WithContext(ctx)

	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 for system owner revoking system key, got %d: %s", rec.Code, rec.Body.String())
	}
}

// --- master-scope happy paths on guarded handlers ---

// recordingBuiltinToolStore is a minimal BuiltinToolStore stub that records
// calls to Update so the happy-path test can assert "the guard allowed the
// request through to the store" without relying on panic-recover semantics.
// Only Update is exercised; the rest return zero values to satisfy the
// interface contract.
type recordingBuiltinToolStore struct {
	updateName string
	updateBody map[string]any
}

func (s *recordingBuiltinToolStore) Update(_ context.Context, name string, updates map[string]any) error {
	s.updateName = name
	s.updateBody = updates
	return nil
}
func (s *recordingBuiltinToolStore) List(_ context.Context) ([]store.BuiltinToolDef, error) {
	return nil, nil
}
func (s *recordingBuiltinToolStore) Get(_ context.Context, _ string) (*store.BuiltinToolDef, error) {
	return nil, nil
}
func (s *recordingBuiltinToolStore) Seed(_ context.Context, _ []store.BuiltinToolDef) error {
	return nil
}
func (s *recordingBuiltinToolStore) ListEnabled(_ context.Context) ([]store.BuiltinToolDef, error) {
	return nil, nil
}
func (s *recordingBuiltinToolStore) GetSettings(_ context.Context, _ string) (json.RawMessage, error) {
	return nil, nil
}

// TestBuiltinToolsUpdate_AllowsMasterAdmin ensures master-scope callers are
// not blocked by Phase 0b. Uses a recording stub store so we can assert the
// guard passed and the handler reached Update — no fragile panic-recover.
func TestBuiltinToolsUpdate_AllowsMasterAdmin(t *testing.T) {
	rec := &recordingBuiltinToolStore{}
	h := &BuiltinToolsHandler{store: rec}
	mux := http.NewServeMux()
	mux.HandleFunc("PUT /v1/tools/builtin/{name}", h.handleUpdate)

	req := httptest.NewRequest(http.MethodPut, "/v1/tools/builtin/web_search",
		strings.NewReader(`{"enabled":true}`))
	req.Header.Set("Content-Type", "application/json")
	ctx := store.WithTenantID(req.Context(), store.MasterTenantID)
	ctx = store.WithRole(ctx, "admin")
	req = req.WithContext(ctx)

	rr := httptest.NewRecorder()
	mux.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200 for master admin, got %d: %s", rr.Code, rr.Body.String())
	}
	if rec.updateName != "web_search" {
		t.Errorf("expected Update called with name=web_search, got %q", rec.updateName)
	}
	if enabled, ok := rec.updateBody["enabled"].(bool); !ok || !enabled {
		t.Errorf("expected Update body {enabled:true}, got %+v", rec.updateBody)
	}
}

// sanity: make sure json is importable (unused import guard during refactors)
var _ = json.Marshal
