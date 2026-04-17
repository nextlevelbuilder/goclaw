package http

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"slices"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/nextlevelbuilder/goclaw/internal/bus"
	"github.com/nextlevelbuilder/goclaw/internal/skills"
	"github.com/nextlevelbuilder/goclaw/internal/store"
)

// ---- test doubles ----

// mockEventPublisher records broadcast calls for assertion.
type mockEventPublisher struct {
	mu     sync.Mutex
	events []bus.Event
}

func (m *mockEventPublisher) Subscribe(_ string, _ bus.EventHandler) {}
func (m *mockEventPublisher) Unsubscribe(_ string)                   {}
func (m *mockEventPublisher) Broadcast(e bus.Event) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.events = append(m.events, e)
}
func (m *mockEventPublisher) capturedEvents() []bus.Event {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]bus.Event, len(m.events))
	copy(out, m.events)
	return out
}

// nopExecutor is a no-op UpdateExecutor that always succeeds.
type nopExecutor struct{ source string }

func (e *nopExecutor) Source() string { return e.source }
func (e *nopExecutor) Update(_ context.Context, name, _ string, _ map[string]any) error {
	return nil
}

// partialExecutor fails for the named package, succeeds for all others.
type partialExecutor struct {
	source   string
	failName string
}

func (e *partialExecutor) Source() string { return e.source }
func (e *partialExecutor) Update(_ context.Context, name, _ string, _ map[string]any) error {
	if name == e.failName {
		return errors.New("injected failure for " + name)
	}
	return nil
}

// ---- context builders matching existing test patterns ----

// ownerCtx builds a master-scope request context (uuid.Nil = no tenant restriction).
// Each call should pass a unique userID to avoid hitting the package-level rate limiter
// shared across tests (burst=3, rpm=10 on packagesWriteLimiter).
func ownerCtx(base context.Context, userID string) context.Context {
	ctx := store.WithUserID(base, userID)
	ctx = store.WithTenantID(ctx, uuid.Nil)
	ctx = store.WithRole(ctx, store.RoleOwner)
	return ctx
}

// tenantAdminCtx builds a non-master tenant-admin context (rejected by requireMasterScope).
func tenantAdminCtx(base context.Context, userID string) context.Context {
	tid := uuid.MustParse("aaaabbbb-cccc-dddd-eeee-ffffaaaabbbb")
	ctx := store.WithUserID(base, userID)
	ctx = store.WithTenantID(ctx, tid)
	ctx = store.WithRole(ctx, "admin")
	return ctx
}

// ---- registry builder ----

func buildTestRegistry(updates []skills.UpdateInfo) *skills.UpdateRegistry {
	cache := &skills.UpdateCache{}
	if len(updates) > 0 {
		checkedAt := updates[0].CheckedAt
		if checkedAt.IsZero() {
			checkedAt = time.Now().UTC()
		}
		cache.ReplaceUpdates(updates, checkedAt)
	}
	return skills.NewUpdateRegistry(cache, "", time.Hour)
}

// ---- GET /v1/packages/updates ----

func TestHandleListUpdates_EmptyCache(t *testing.T) {
	pub := &mockEventPublisher{}
	registry := buildTestRegistry(nil)
	h := NewPackagesHandler(registry, pub)

	req := httptest.NewRequest(http.MethodGet, "/v1/packages/updates", nil)
	req = req.WithContext(store.WithRole(store.WithTenantID(store.WithUserID(req.Context(), "u1"), uuid.Nil), "operator"))
	w := httptest.NewRecorder()

	h.handleListUpdates(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("want 200, got %d: %s", w.Code, w.Body.String())
	}
	var body map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	for _, field := range []string{"updates", "stale", "sources", "checkedAt", "ageSeconds", "ttlSeconds"} {
		if _, ok := body[field]; !ok {
			t.Errorf("response missing field %q", field)
		}
	}
}

func TestHandleListUpdates_ReturnsUpdates(t *testing.T) {
	updates := []skills.UpdateInfo{
		{Source: "github", Name: "lazygit", CurrentVersion: "v0.40.0", LatestVersion: "v0.41.0"},
	}
	registry := buildTestRegistry(updates)
	h := NewPackagesHandler(registry, nil)

	req := httptest.NewRequest(http.MethodGet, "/v1/packages/updates", nil)
	req = req.WithContext(store.WithRole(store.WithTenantID(store.WithUserID(req.Context(), "u1"), uuid.Nil), "operator"))
	w := httptest.NewRecorder()

	h.handleListUpdates(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("want 200, got %d", w.Code)
	}
	var body map[string]any
	_ = json.Unmarshal(w.Body.Bytes(), &body)
	arr, _ := body["updates"].([]any)
	if len(arr) != 1 {
		t.Errorf("want 1 update, got %d", len(arr))
	}
}

func TestHandleListUpdates_NilRegistry(t *testing.T) {
	h := NewPackagesHandler(nil, nil)
	req := httptest.NewRequest(http.MethodGet, "/v1/packages/updates", nil)
	w := httptest.NewRecorder()
	h.handleListUpdates(w, req)
	if w.Code != http.StatusServiceUnavailable {
		t.Fatalf("want 503, got %d", w.Code)
	}
}

// ---- POST /v1/packages/updates/refresh ----

func TestHandleRefreshUpdates_RejectNonMaster(t *testing.T) {
	h := NewPackagesHandler(buildTestRegistry(nil), nil)

	req := httptest.NewRequest(http.MethodPost, "/v1/packages/updates/refresh", nil)
	req = req.WithContext(tenantAdminCtx(req.Context(), t.Name()))
	w := httptest.NewRecorder()

	h.handleRefreshUpdates(w, req)

	// red-team H5: non-master admin must get 403.
	if w.Code != http.StatusForbidden {
		t.Fatalf("want 403 for non-master admin, got %d: %s", w.Code, w.Body.String())
	}
}

func TestHandleRefreshUpdates_MasterPublishesCheckedEvent(t *testing.T) {
	// No checkers registered → CheckAll returns empty; still publishes event.
	pub := &mockEventPublisher{}
	h := NewPackagesHandler(buildTestRegistry(nil), pub)

	req := httptest.NewRequest(http.MethodPost, "/v1/packages/updates/refresh", nil)
	req = req.WithContext(ownerCtx(req.Context(), t.Name()))
	w := httptest.NewRecorder()

	h.handleRefreshUpdates(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("want 200, got %d: %s", w.Code, w.Body.String())
	}
	evts := pub.capturedEvents()
	if len(evts) == 0 {
		t.Fatal("expected package.update.checked event")
	}
	if evts[0].Name != eventPackageUpdateChecked {
		t.Errorf("event name = %q, want %q", evts[0].Name, eventPackageUpdateChecked)
	}
	// TenantID must be Nil — only Owner clients receive unscoped events.
	if evts[0].TenantID != uuid.Nil {
		t.Errorf("event TenantID must be uuid.Nil, got %v", evts[0].TenantID)
	}
}

// ---- POST /v1/packages/update ----

func TestHandleUpdatePackage_RejectNonMaster(t *testing.T) {
	h := NewPackagesHandler(buildTestRegistry(nil), nil)

	req := httptest.NewRequest(http.MethodPost, "/v1/packages/update",
		bytes.NewBufferString(`{"package":"github:lazygit"}`))
	req = req.WithContext(tenantAdminCtx(req.Context(), t.Name()))
	w := httptest.NewRecorder()

	h.handleUpdatePackage(w, req)

	if w.Code != http.StatusForbidden {
		t.Fatalf("want 403, got %d: %s", w.Code, w.Body.String())
	}
}

func TestHandleUpdatePackage_InvalidBody(t *testing.T) {
	h := NewPackagesHandler(buildTestRegistry(nil), nil)

	req := httptest.NewRequest(http.MethodPost, "/v1/packages/update",
		bytes.NewBufferString(`{invalid`))
	req = req.WithContext(ownerCtx(req.Context(), t.Name()))
	w := httptest.NewRecorder()

	h.handleUpdatePackage(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("want 400 for invalid JSON, got %d: %s", w.Code, w.Body.String())
	}
}

func TestHandleUpdatePackage_NonGithubSpec(t *testing.T) {
	// Only "github:" prefix is supported for updates.
	h := NewPackagesHandler(buildTestRegistry(nil), nil)

	req := httptest.NewRequest(http.MethodPost, "/v1/packages/update",
		bytes.NewBufferString(`{"package":"pip:pandas"}`))
	req = req.WithContext(ownerCtx(req.Context(), t.Name()))
	w := httptest.NewRecorder()

	h.handleUpdatePackage(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("want 400 for non-github spec, got %d: %s", w.Code, w.Body.String())
	}
}

func TestHandleUpdatePackage_CacheStaleNoVersion(t *testing.T) {
	// Empty cache + no toVersion → 409.
	h := NewPackagesHandler(buildTestRegistry(nil), nil)

	req := httptest.NewRequest(http.MethodPost, "/v1/packages/update",
		bytes.NewBufferString(`{"package":"github:lazygit"}`))
	req = req.WithContext(ownerCtx(req.Context(), t.Name()))
	w := httptest.NewRecorder()

	h.handleUpdatePackage(w, req)

	if w.Code != http.StatusConflict {
		t.Fatalf("want 409 for empty cache+no version, got %d: %s", w.Code, w.Body.String())
	}
}

func TestHandleUpdatePackage_HappyPath(t *testing.T) {
	updates := []skills.UpdateInfo{{
		Source: "github", Name: "lazygit",
		CurrentVersion: "v0.40.0", LatestVersion: "v0.41.0",
		Meta: map[string]any{},
	}}
	registry := buildTestRegistry(updates)
	registry.RegisterExecutor(&nopExecutor{source: "github"})

	pub := &mockEventPublisher{}
	h := NewPackagesHandler(registry, pub)

	req := httptest.NewRequest(http.MethodPost, "/v1/packages/update",
		bytes.NewBufferString(`{"package":"github:lazygit","toVersion":"v0.41.0"}`))
	req = req.WithContext(ownerCtx(req.Context(), t.Name()))
	w := httptest.NewRecorder()

	h.handleUpdatePackage(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("want 200, got %d: %s", w.Code, w.Body.String())
	}
	var resp map[string]any
	_ = json.Unmarshal(w.Body.Bytes(), &resp)
	if resp["ok"] != true {
		t.Errorf("want ok=true, got %v", resp["ok"])
	}

	names := collectEventNames(pub.capturedEvents())
	if !sliceContains(names, eventPackageUpdateStarted) {
		t.Error("missing package.update.started event")
	}
	if !sliceContains(names, eventPackageUpdateSucceeded) {
		t.Error("missing package.update.succeeded event")
	}
}

// ---- POST /v1/packages/updates/apply-all ----

func TestHandleApplyAllUpdates_RejectNonMaster(t *testing.T) {
	h := NewPackagesHandler(buildTestRegistry(nil), nil)

	req := httptest.NewRequest(http.MethodPost, "/v1/packages/updates/apply-all",
		bytes.NewBufferString(`{}`))
	req = req.WithContext(tenantAdminCtx(req.Context(), t.Name()))
	w := httptest.NewRecorder()

	h.handleApplyAllUpdates(w, req)

	if w.Code != http.StatusForbidden {
		t.Fatalf("want 403, got %d: %s", w.Code, w.Body.String())
	}
}

func TestHandleApplyAllUpdates_EmptyCacheAlways200(t *testing.T) {
	// No cache entries → 200 with non-null empty arrays (red-team M2, M7).
	h := NewPackagesHandler(buildTestRegistry(nil), nil)

	req := httptest.NewRequest(http.MethodPost, "/v1/packages/updates/apply-all",
		bytes.NewBufferString(`{}`))
	req = req.WithContext(ownerCtx(req.Context(), t.Name()))
	w := httptest.NewRecorder()

	h.handleApplyAllUpdates(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("want 200 always (red-team M2), got %d: %s", w.Code, w.Body.String())
	}
	var body map[string]any
	_ = json.Unmarshal(w.Body.Bytes(), &body)

	// Both arrays must be [] not null (red-team M7 — frontend null-check safety).
	succeeded, ok := body["succeeded"].([]any)
	if !ok {
		t.Error("succeeded must be [] not null")
	}
	failed, ok := body["failed"].([]any)
	if !ok {
		t.Error("failed must be [] not null")
	}
	if len(succeeded)+len(failed) != 0 {
		t.Errorf("want 0 items, got succeeded=%d failed=%d", len(succeeded), len(failed))
	}
	if _, hasDur := body["durationMs"]; !hasDur {
		t.Error("response missing durationMs")
	}
}

func TestHandleApplyAllUpdates_MixedSuccessFailure(t *testing.T) {
	updates := []skills.UpdateInfo{
		{Source: "github", Name: "lazygit", CurrentVersion: "v0.40.0", LatestVersion: "v0.41.0", Meta: map[string]any{}},
		{Source: "github", Name: "gh", CurrentVersion: "v2.40.0", LatestVersion: "v2.41.0", Meta: map[string]any{}},
	}
	registry := buildTestRegistry(updates)
	// Succeeds for lazygit, fails for gh.
	registry.RegisterExecutor(&partialExecutor{source: "github", failName: "gh"})

	pub := &mockEventPublisher{}
	h := NewPackagesHandler(registry, pub)

	req := httptest.NewRequest(http.MethodPost, "/v1/packages/updates/apply-all",
		bytes.NewBufferString(`{"packages":["github:lazygit","github:gh"]}`))
	req = req.WithContext(ownerCtx(req.Context(), t.Name()))
	w := httptest.NewRecorder()

	h.handleApplyAllUpdates(w, req)

	// red-team M2: always 200 even with partial failure.
	if w.Code != http.StatusOK {
		t.Fatalf("want 200 always, got %d: %s", w.Code, w.Body.String())
	}
	var resp map[string]any
	_ = json.Unmarshal(w.Body.Bytes(), &resp)
	succeeded, _ := resp["succeeded"].([]any)
	failed, _ := resp["failed"].([]any)
	if len(succeeded) != 1 {
		t.Errorf("want 1 succeeded, got %d", len(succeeded))
	}
	if len(failed) != 1 {
		t.Errorf("want 1 failed, got %d", len(failed))
	}

	// Verify both started+succeeded and started+failed events were emitted.
	names := collectEventNames(pub.capturedEvents())
	if !sliceContains(names, eventPackageUpdateSucceeded) {
		t.Error("missing package.update.succeeded event")
	}
	if !sliceContains(names, eventPackageUpdateFailed) {
		t.Error("missing package.update.failed event")
	}
}

func TestHandleApplyAllUpdates_InvalidSpecInList(t *testing.T) {
	// A non-github spec in the list ends up in failed[], others continue.
	registry := buildTestRegistry(nil)
	registry.RegisterExecutor(&nopExecutor{source: "github"})
	h := NewPackagesHandler(registry, nil)

	// pip:pandas is invalid for updates; github:lazygit has no cache entry → also failed.
	req := httptest.NewRequest(http.MethodPost, "/v1/packages/updates/apply-all",
		bytes.NewBufferString(`{"packages":["pip:pandas","github:lazygit"]}`))
	req = req.WithContext(ownerCtx(req.Context(), t.Name()))
	w := httptest.NewRecorder()

	h.handleApplyAllUpdates(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("want 200, got %d", w.Code)
	}
	var resp map[string]any
	_ = json.Unmarshal(w.Body.Bytes(), &resp)
	failed, _ := resp["failed"].([]any)
	if len(failed) == 0 {
		t.Error("expected at least 1 failed entry for invalid/missing spec")
	}
}

// ---- small utilities ----

func collectEventNames(evts []bus.Event) []string {
	out := make([]string, len(evts))
	for i, e := range evts {
		out[i] = e.Name
	}
	return out
}

func sliceContains(slice []string, s string) bool {
	return slices.Contains(slice, s)
}
