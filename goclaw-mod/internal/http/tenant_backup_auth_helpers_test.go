package http

import (
	"context"
	"database/sql"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/nextlevelbuilder/goclaw/internal/store"
)

type fakeTenantStore struct {
	tenant *store.TenantData
	err    error
}

func (f fakeTenantStore) CreateTenant(context.Context, *store.TenantData) error { return nil }
func (f fakeTenantStore) GetTenant(context.Context, uuid.UUID) (*store.TenantData, error) {
	return f.tenant, f.err
}
func (f fakeTenantStore) GetTenantBySlug(context.Context, string) (*store.TenantData, error) {
	return f.tenant, f.err
}
func (f fakeTenantStore) ListTenants(context.Context) ([]store.TenantData, error)       { return nil, nil }
func (f fakeTenantStore) UpdateTenant(context.Context, uuid.UUID, map[string]any) error { return nil }
func (f fakeTenantStore) AddUser(context.Context, uuid.UUID, string, string) error      { return nil }
func (f fakeTenantStore) RemoveUser(context.Context, uuid.UUID, string) error           { return nil }
func (f fakeTenantStore) GetUserRole(context.Context, uuid.UUID, string) (string, error) {
	return "", nil
}
func (f fakeTenantStore) ListUsers(context.Context, uuid.UUID) ([]store.TenantUserData, error) {
	return nil, nil
}
func (f fakeTenantStore) ListUserTenants(context.Context, string) ([]store.TenantUserData, error) {
	return nil, nil
}
func (f fakeTenantStore) GetTenantsByIDs(context.Context, []uuid.UUID) ([]store.TenantData, error) {
	return nil, nil
}
func (f fakeTenantStore) ResolveUserTenant(context.Context, string) (uuid.UUID, error) {
	return uuid.Nil, nil
}
func (f fakeTenantStore) GetTenantUser(context.Context, uuid.UUID) (*store.TenantUserData, error) {
	return nil, nil
}
func (f fakeTenantStore) CreateTenantUserReturning(context.Context, uuid.UUID, string, string, string) (*store.TenantUserData, error) {
	return nil, nil
}

func TestResolveTenantLooksUpSlugForTenantID(t *testing.T) {
	tenantID := uuid.New()
	handler := &TenantBackupHandler{
		tenants: fakeTenantStore{tenant: &store.TenantData{ID: tenantID, Slug: "acme"}},
	}

	req := httptest.NewRequest(http.MethodGet, "/v1/tenant/backup?tenant_id="+tenantID.String(), nil)
	rec := httptest.NewRecorder()

	id, slug, ok := handler.resolveTenant(rec, req)
	if !ok {
		t.Fatal("resolveTenant() returned ok=false")
	}
	if id != tenantID {
		t.Fatalf("resolveTenant() id = %s, want %s", id, tenantID)
	}
	if slug != "acme" {
		t.Fatalf("resolveTenant() slug = %q, want %q", slug, "acme")
	}
}

func TestResolveTenantReturnsNotFoundForMissingTenant(t *testing.T) {
	tenantID := uuid.New()
	handler := &TenantBackupHandler{tenants: fakeTenantStore{err: sql.ErrNoRows}}

	req := httptest.NewRequest(http.MethodGet, "/v1/tenant/backup?tenant_id="+tenantID.String(), nil)
	rec := httptest.NewRecorder()

	_, _, ok := handler.resolveTenant(rec, req)
	if ok {
		t.Fatal("resolveTenant() returned ok=true, want false")
	}
	if rec.Code != http.StatusNotFound {
		t.Fatalf("resolveTenant() status = %d, want %d", rec.Code, http.StatusNotFound)
	}
}

func TestResolveTenantReturnsInternalForLookupError(t *testing.T) {
	tenantID := uuid.New()
	handler := &TenantBackupHandler{tenants: fakeTenantStore{err: errors.New("db down")}}

	req := httptest.NewRequest(http.MethodGet, "/v1/tenant/backup?tenant_id="+tenantID.String(), nil)
	rec := httptest.NewRecorder()

	_, _, ok := handler.resolveTenant(rec, req)
	if ok {
		t.Fatal("resolveTenant() returned ok=true, want false")
	}
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("resolveTenant() status = %d, want %d", rec.Code, http.StatusInternalServerError)
	}
}

func TestResolveRestoreTargetNewModeUsesSlug(t *testing.T) {
	handler := &TenantBackupHandler{tenants: fakeTenantStore{}}
	req := httptest.NewRequest(http.MethodPost, "/v1/tenant/restore?mode=new&tenant_slug=fresh-slug", nil)
	rec := httptest.NewRecorder()

	id, slug, ok := handler.resolveRestoreTarget(rec, req, "new")
	if !ok {
		t.Fatal("resolveRestoreTarget() returned ok=false")
	}
	if id != uuid.Nil {
		t.Fatalf("resolveRestoreTarget() id = %s, want uuid.Nil", id)
	}
	if slug != "fresh-slug" {
		t.Fatalf("resolveRestoreTarget() slug = %q, want %q", slug, "fresh-slug")
	}
}

func TestResolveRestoreTargetNewModeRejectsTenantID(t *testing.T) {
	handler := &TenantBackupHandler{tenants: fakeTenantStore{}}
	req := httptest.NewRequest(http.MethodPost,
		"/v1/tenant/restore?mode=new&tenant_id="+uuid.New().String(), nil)
	rec := httptest.NewRecorder()

	_, _, ok := handler.resolveRestoreTarget(rec, req, "new")
	if ok {
		t.Fatal("resolveRestoreTarget() returned ok=true, want false")
	}
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusBadRequest)
	}
	body := rec.Body.String()
	if !strings.Contains(body, "tenant_slug") {
		t.Fatalf("error body should mention tenant_slug; got %q", body)
	}
}

func TestResolveRestoreTargetNewModeRequiresSlug(t *testing.T) {
	handler := &TenantBackupHandler{tenants: fakeTenantStore{}}
	req := httptest.NewRequest(http.MethodPost, "/v1/tenant/restore?mode=new", nil)
	rec := httptest.NewRecorder()

	_, _, ok := handler.resolveRestoreTarget(rec, req, "new")
	if ok {
		t.Fatal("resolveRestoreTarget() returned ok=true, want false")
	}
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusBadRequest)
	}
}

// TestResolveRestoreTargetNewModeRejectsTenantIDWithSlug verifies that
// mode=new rejects tenant_id even when tenant_slug is also provided — matches
// CLI contract, prevents ambiguous "which one wins" semantics.
func TestResolveRestoreTargetNewModeRejectsTenantIDWithSlug(t *testing.T) {
	handler := &TenantBackupHandler{tenants: fakeTenantStore{}}
	req := httptest.NewRequest(http.MethodPost,
		"/v1/tenant/restore?mode=new&tenant_slug=fresh&tenant_id="+uuid.New().String(), nil)
	rec := httptest.NewRecorder()

	_, _, ok := handler.resolveRestoreTarget(rec, req, "new")
	if ok {
		t.Fatal("resolveRestoreTarget() returned ok=true, want false")
	}
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusBadRequest)
	}
	if !strings.Contains(rec.Body.String(), "tenant_slug") {
		t.Fatalf("error body should reference the expected param; got %q", rec.Body.String())
	}
}
