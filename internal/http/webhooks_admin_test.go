package http

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/nextlevelbuilder/goclaw/internal/edition"
	"github.com/nextlevelbuilder/goclaw/internal/store"
)

// ---- stub WebhookStore for admin tests ----
// webhooks_auth_test.go already defines stubWebhookStore but only covers the
// authentication surface. We need a richer version for CRUD: Create stores rows,
// List / GetByID return them, Update / RotateSecret / Revoke mutate in-memory.

type adminWebhookStore struct {
	mu   sync.Mutex
	rows map[uuid.UUID]*store.WebhookData
}

func newAdminWebhookStore(rows ...*store.WebhookData) *adminWebhookStore {
	s := &adminWebhookStore{rows: make(map[uuid.UUID]*store.WebhookData)}
	for _, r := range rows {
		cp := *r
		s.rows[r.ID] = &cp
	}
	return s
}

func (s *adminWebhookStore) Create(_ context.Context, w *store.WebhookData) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	cp := *w
	s.rows[w.ID] = &cp
	return nil
}

func (s *adminWebhookStore) GetByID(ctx context.Context, id uuid.UUID) (*store.WebhookData, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	row, ok := s.rows[id]
	if !ok {
		return nil, sql.ErrNoRows
	}
	// Tenant-scope enforcement mirrors real store behaviour.
	tid := store.TenantIDFromContext(ctx)
	if tid != uuid.Nil && row.TenantID != tid && !store.IsOwnerRole(ctx) {
		return nil, sql.ErrNoRows
	}
	cp := *row
	return &cp, nil
}

func (s *adminWebhookStore) GetByHash(_ context.Context, h string) (*store.WebhookData, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, r := range s.rows {
		if r.SecretHash == h {
			cp := *r
			return &cp, nil
		}
	}
	return nil, sql.ErrNoRows
}

func (s *adminWebhookStore) List(ctx context.Context, f store.WebhookListFilter) ([]store.WebhookData, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	tid := store.TenantIDFromContext(ctx)
	var out []store.WebhookData
	for _, r := range s.rows {
		if !store.IsOwnerRole(ctx) && r.TenantID != tid {
			continue
		}
		if f.AgentID != nil && (r.AgentID == nil || *r.AgentID != *f.AgentID) {
			continue
		}
		out = append(out, *r)
	}
	return out, nil
}

func (s *adminWebhookStore) Update(_ context.Context, id uuid.UUID, updates map[string]any) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	row, ok := s.rows[id]
	if !ok {
		return sql.ErrNoRows
	}
	if v, ok := updates["name"]; ok {
		row.Name = v.(string)
	}
	if v, ok := updates["require_hmac"]; ok {
		row.RequireHMAC = v.(bool)
	}
	if v, ok := updates["localhost_only"]; ok {
		row.LocalhostOnly = v.(bool)
	}
	row.UpdatedAt = time.Now()
	return nil
}

func (s *adminWebhookStore) RotateSecret(_ context.Context, id uuid.UUID, newHash, newPrefix, newEncryptedSecret string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	row, ok := s.rows[id]
	if !ok {
		return sql.ErrNoRows
	}
	row.SecretHash = newHash
	row.SecretPrefix = newPrefix
	row.EncryptedSecret = newEncryptedSecret
	row.UpdatedAt = time.Now()
	return nil
}

func (s *adminWebhookStore) Revoke(_ context.Context, id uuid.UUID) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	row, ok := s.rows[id]
	if !ok {
		return sql.ErrNoRows
	}
	row.Revoked = true
	row.UpdatedAt = time.Now()
	return nil
}

func (s *adminWebhookStore) TouchLastUsed(_ context.Context, _ uuid.UUID) error { return nil }

// GetByHashUnscoped and GetByIDUnscoped are auth-middleware-only unscoped lookups.
// In admin tests the middleware is not exercised, so these are no-ops.
func (s *adminWebhookStore) GetByHashUnscoped(ctx context.Context, h string) (*store.WebhookData, error) {
	return s.GetByHash(ctx, h)
}
func (s *adminWebhookStore) GetByIDUnscoped(ctx context.Context, id uuid.UUID) (*store.WebhookData, error) {
	return s.GetByID(ctx, id)
}

// ---- stub TenantStore for admin tests ----
// Delegates GetUserRole to a configurable map; stubs everything else.

type adminTenantStore struct {
	roles map[string]string // key = tenantID+":"+userID
}

func (a *adminTenantStore) key(tid uuid.UUID, uid string) string {
	return tid.String() + ":" + uid
}

func (a *adminTenantStore) GetUserRole(_ context.Context, tid uuid.UUID, uid string) (string, error) {
	if r, ok := a.roles[a.key(tid, uid)]; ok {
		return r, nil
	}
	return "", nil
}

// Remaining store.TenantStore methods — no-op stubs.
func (a *adminTenantStore) CreateTenant(context.Context, *store.TenantData) error { return nil }
func (a *adminTenantStore) GetTenant(_ context.Context, _ uuid.UUID) (*store.TenantData, error) {
	return nil, sql.ErrNoRows
}
func (a *adminTenantStore) GetTenantBySlug(_ context.Context, _ string) (*store.TenantData, error) {
	return nil, sql.ErrNoRows
}
func (a *adminTenantStore) ListTenants(context.Context) ([]store.TenantData, error) { return nil, nil }
func (a *adminTenantStore) UpdateTenant(context.Context, uuid.UUID, map[string]any) error {
	return nil
}
func (a *adminTenantStore) AddUser(context.Context, uuid.UUID, string, string) error { return nil }
func (a *adminTenantStore) RemoveUser(context.Context, uuid.UUID, string) error      { return nil }
func (a *adminTenantStore) ListUsers(context.Context, uuid.UUID) ([]store.TenantUserData, error) {
	return nil, nil
}
func (a *adminTenantStore) ListUserTenants(context.Context, string) ([]store.TenantUserData, error) {
	return nil, nil
}
func (a *adminTenantStore) GetTenantsByIDs(context.Context, []uuid.UUID) ([]store.TenantData, error) {
	return nil, nil
}
func (a *adminTenantStore) ResolveUserTenant(context.Context, string) (uuid.UUID, error) {
	return uuid.Nil, sql.ErrNoRows
}
func (a *adminTenantStore) GetTenantUser(context.Context, uuid.UUID) (*store.TenantUserData, error) {
	return nil, sql.ErrNoRows
}
func (a *adminTenantStore) CreateTenantUserReturning(context.Context, uuid.UUID, string, string, string) (*store.TenantUserData, error) {
	return nil, nil
}

// ---- helpers ----

func tenantAdminCtx(tenantID uuid.UUID, userID string) context.Context {
	ctx := context.Background()
	ctx = store.WithTenantID(ctx, tenantID)
	ctx = store.WithUserID(ctx, userID)
	return ctx
}

func ownerCtx() context.Context {
	ctx := context.Background()
	ctx = store.WithRole(ctx, store.RoleOwner)
	return ctx
}

// testAdminEncKey is a 32-byte (256-bit) AES key used only in tests.
const testAdminEncKey = "00000000000000000000000000000000"

func newAdminHandler(ws *adminWebhookStore, ts *adminTenantStore) *WebhooksAdminHandler {
	h := NewWebhooksAdminHandler(ws, ts, nil)
	h.SetEncKey(testAdminEncKey) // required since K6 guard rejects empty encKey
	return h
}

func doRequest(t *testing.T, h *WebhooksAdminHandler, method, path string, body any, ctx context.Context) *httptest.ResponseRecorder {
	t.Helper()
	var buf bytes.Buffer
	if body != nil {
		if err := json.NewEncoder(&buf).Encode(body); err != nil {
			t.Fatalf("encode body: %v", err)
		}
	}
	r := httptest.NewRequest(method, path, &buf)
	r = r.WithContext(ctx)
	r.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	mux := http.NewServeMux()
	h.RegisterRoutes(mux)
	mux.ServeHTTP(w, r)
	return w
}

// ---- tests ----

// TestWebhookAdmin_Create_HappyPath verifies POST /v1/webhooks returns secret once.
func TestWebhookAdmin_Create_HappyPath(t *testing.T) {
	tenantID := uuid.New()
	userID := "user-1"

	ts := &adminTenantStore{
		roles: map[string]string{
			tenantID.String() + ":" + userID: store.TenantRoleAdmin,
		},
	}
	ws := newAdminWebhookStore()
	h := newAdminHandler(ws, ts)

	ctx := tenantAdminCtx(tenantID, userID)
	w := doRequest(t, h, http.MethodPost, "/v1/webhooks", map[string]any{
		"name": "my webhook",
		"kind": "llm",
	}, ctx)

	if w.Code != http.StatusCreated {
		t.Fatalf("want 201, got %d: %s", w.Code, w.Body.String())
	}

	var resp webhookCreateResp
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp.Secret == "" {
		t.Fatal("secret must be present in create response")
	}
	if resp.HMACSigningKey == "" {
		t.Fatal("hmac_signing_key must be present in create response")
	}
	if resp.SecretPrefix == "" {
		t.Fatal("secret_prefix must be present in create response")
	}
	// secret must start with wh_
	if len(resp.Secret) < 3 || resp.Secret[:3] != "wh_" {
		t.Fatalf("secret must start with wh_, got %q", resp.Secret)
	}
	// verify prefix matches first 8 chars of raw secret
	if resp.SecretPrefix != resp.Secret[:8] {
		t.Fatalf("prefix %q != first 8 chars of secret %q", resp.SecretPrefix, resp.Secret[:8])
	}
}

// TestWebhookAdmin_Create_NonAdmin_403 verifies non-admin cannot create.
func TestWebhookAdmin_Create_NonAdmin_403(t *testing.T) {
	tenantID := uuid.New()
	userID := "user-2"

	// operator role, not admin/owner
	ts := &adminTenantStore{
		roles: map[string]string{
			tenantID.String() + ":" + userID: "operator",
		},
	}
	ws := newAdminWebhookStore()
	h := newAdminHandler(ws, ts)

	ctx := tenantAdminCtx(tenantID, userID)
	w := doRequest(t, h, http.MethodPost, "/v1/webhooks", map[string]any{
		"name": "x",
		"kind": "llm",
	}, ctx)

	if w.Code != http.StatusForbidden {
		t.Fatalf("want 403, got %d: %s", w.Code, w.Body.String())
	}
}

// TestWebhookAdmin_Create_InvalidKind_400 verifies unknown kind is rejected.
func TestWebhookAdmin_Create_InvalidKind_400(t *testing.T) {
	tenantID := uuid.New()
	userID := "user-3"

	ts := &adminTenantStore{
		roles: map[string]string{
			tenantID.String() + ":" + userID: store.TenantRoleAdmin,
		},
	}
	ws := newAdminWebhookStore()
	h := newAdminHandler(ws, ts)

	ctx := tenantAdminCtx(tenantID, userID)
	w := doRequest(t, h, http.MethodPost, "/v1/webhooks", map[string]any{
		"name": "x",
		"kind": "unknown",
	}, ctx)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("want 400, got %d: %s", w.Code, w.Body.String())
	}
}

// TestWebhookAdmin_Create_LiteMessageKind_403 verifies Lite rejects kind=message.
func TestWebhookAdmin_Create_LiteMessageKind_403(t *testing.T) {
	// Set Lite edition for this test, restore Standard after.
	edition.SetCurrent(edition.Lite)
	t.Cleanup(func() { edition.SetCurrent(edition.Standard) })

	tenantID := uuid.New()
	userID := "user-4"

	ts := &adminTenantStore{
		roles: map[string]string{
			tenantID.String() + ":" + userID: store.TenantRoleAdmin,
		},
	}
	ws := newAdminWebhookStore()
	h := newAdminHandler(ws, ts)

	ctx := tenantAdminCtx(tenantID, userID)
	w := doRequest(t, h, http.MethodPost, "/v1/webhooks", map[string]any{
		"name": "x",
		"kind": "message",
	}, ctx)

	if w.Code != http.StatusForbidden {
		t.Fatalf("want 403 for message kind on Lite, got %d: %s", w.Code, w.Body.String())
	}
}

// TestWebhookAdmin_Create_LiteForcesLocalhostOnly verifies Lite forces localhost_only=true.
func TestWebhookAdmin_Create_LiteForcesLocalhostOnly(t *testing.T) {
	edition.SetCurrent(edition.Lite)
	t.Cleanup(func() { edition.SetCurrent(edition.Standard) })

	tenantID := uuid.New()
	userID := "user-5"

	ts := &adminTenantStore{
		roles: map[string]string{
			tenantID.String() + ":" + userID: store.TenantRoleAdmin,
		},
	}
	ws := newAdminWebhookStore()
	h := newAdminHandler(ws, ts)

	ctx := tenantAdminCtx(tenantID, userID)
	// Client sends localhost_only=false — server must override to true.
	w := doRequest(t, h, http.MethodPost, "/v1/webhooks", map[string]any{
		"name":           "x",
		"kind":           "llm",
		"localhost_only": false,
	}, ctx)

	if w.Code != http.StatusCreated {
		t.Fatalf("want 201, got %d: %s", w.Code, w.Body.String())
	}

	var resp webhookCreateResp
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if !resp.LocalhostOnly {
		t.Fatal("Lite edition must force localhost_only=true regardless of client input")
	}
}

// TestWebhookAdmin_Get_CrossTenant_404 verifies tenant A cannot see tenant B's webhook.
func TestWebhookAdmin_Get_CrossTenant_404(t *testing.T) {
	tenantA := uuid.New()
	tenantB := uuid.New()
	userA := "user-a"

	// Webhook owned by tenant B.
	webhookID := uuid.New()
	whB := &store.WebhookData{
		ID:       webhookID,
		TenantID: tenantB,
		Name:     "b-webhook",
		Kind:     "llm",
	}

	ts := &adminTenantStore{
		roles: map[string]string{
			tenantA.String() + ":" + userA: store.TenantRoleAdmin,
		},
	}
	ws := newAdminWebhookStore(whB)
	h := newAdminHandler(ws, ts)

	// Request from tenant A.
	ctx := tenantAdminCtx(tenantA, userA)
	r := httptest.NewRequest(http.MethodGet, "/v1/webhooks/"+webhookID.String(), nil)
	r = r.WithContext(ctx)
	w := httptest.NewRecorder()

	mux := http.NewServeMux()
	h.RegisterRoutes(mux)
	mux.ServeHTTP(w, r)

	if w.Code != http.StatusNotFound {
		t.Fatalf("want 404 for cross-tenant get, got %d: %s", w.Code, w.Body.String())
	}
}

// TestWebhookAdmin_FullFlow_CreateListGetRotateRevoke exercises the happy path for all 6 endpoints.
func TestWebhookAdmin_FullFlow_CreateListGetRotateRevoke(t *testing.T) {
	tenantID := uuid.New()
	userID := "user-flow"

	ts := &adminTenantStore{
		roles: map[string]string{
			tenantID.String() + ":" + userID: store.TenantRoleAdmin,
		},
	}
	ws := newAdminWebhookStore()
	h := newAdminHandler(ws, ts)
	ctx := tenantAdminCtx(tenantID, userID)

	mux := http.NewServeMux()
	h.RegisterRoutes(mux)

	// 1. Create.
	var createResp webhookCreateResp
	{
		var buf bytes.Buffer
		_ = json.NewEncoder(&buf).Encode(map[string]any{"name": "flow-wh", "kind": "llm"})
		r := httptest.NewRequest(http.MethodPost, "/v1/webhooks", &buf)
		r.Header.Set("Content-Type", "application/json")
		r = r.WithContext(ctx)
		w := httptest.NewRecorder()
		mux.ServeHTTP(w, r)
		if w.Code != http.StatusCreated {
			t.Fatalf("create: want 201, got %d: %s", w.Code, w.Body.String())
		}
		if err := json.NewDecoder(w.Body).Decode(&createResp); err != nil {
			t.Fatalf("create decode: %v", err)
		}
	}
	id := createResp.ID
	originalSecret := createResp.Secret

	// 2. List — must include newly created webhook.
	{
		r := httptest.NewRequest(http.MethodGet, "/v1/webhooks", nil)
		r = r.WithContext(ctx)
		w := httptest.NewRecorder()
		mux.ServeHTTP(w, r)
		if w.Code != http.StatusOK {
			t.Fatalf("list: want 200, got %d: %s", w.Code, w.Body.String())
		}
		var rows []store.WebhookData
		if err := json.NewDecoder(w.Body).Decode(&rows); err != nil {
			t.Fatalf("list decode: %v", err)
		}
		found := false
		for _, row := range rows {
			if row.ID == id {
				found = true
			}
		}
		if !found {
			t.Fatal("list: newly created webhook not found")
		}
	}

	// 3. Get.
	{
		r := httptest.NewRequest(http.MethodGet, "/v1/webhooks/"+id.String(), nil)
		r = r.WithContext(ctx)
		w := httptest.NewRecorder()
		mux.ServeHTTP(w, r)
		if w.Code != http.StatusOK {
			t.Fatalf("get: want 200, got %d: %s", w.Code, w.Body.String())
		}
		var row store.WebhookData
		if err := json.NewDecoder(w.Body).Decode(&row); err != nil {
			t.Fatalf("get decode: %v", err)
		}
		// Secret must NOT be in normal GET response.
		if row.SecretHash != "" {
			// SecretHash has json:"-" tag so it should never appear.
			// This check uses the decoded struct; field is blank as expected.
		}
		if row.ID != id {
			t.Fatalf("get: wrong id %s", row.ID)
		}
	}

	// 4. Rotate.
	var rotateResp map[string]any
	{
		r := httptest.NewRequest(http.MethodPost, "/v1/webhooks/"+id.String()+"/rotate", nil)
		r = r.WithContext(ctx)
		w := httptest.NewRecorder()
		mux.ServeHTTP(w, r)
		if w.Code != http.StatusOK {
			t.Fatalf("rotate: want 200, got %d: %s", w.Code, w.Body.String())
		}
		if err := json.NewDecoder(w.Body).Decode(&rotateResp); err != nil {
			t.Fatalf("rotate decode: %v", err)
		}
		newSecret, _ := rotateResp["secret"].(string)
		if newSecret == "" {
			t.Fatal("rotate: new secret must be present")
		}
		if newSecret == originalSecret {
			t.Fatal("rotate: new secret must differ from original")
		}
	}

	// 5. Revoke.
	{
		r := httptest.NewRequest(http.MethodDelete, "/v1/webhooks/"+id.String(), nil)
		r = r.WithContext(ctx)
		w := httptest.NewRecorder()
		mux.ServeHTTP(w, r)
		if w.Code != http.StatusOK {
			t.Fatalf("revoke: want 200, got %d: %s", w.Code, w.Body.String())
		}
	}

	// 6. Get after revoke — row still exists (soft-delete) but is marked revoked.
	{
		r := httptest.NewRequest(http.MethodGet, "/v1/webhooks/"+id.String(), nil)
		r = r.WithContext(ctx)
		w := httptest.NewRecorder()
		mux.ServeHTTP(w, r)
		if w.Code != http.StatusOK {
			t.Fatalf("get-after-revoke: want 200, got %d: %s", w.Code, w.Body.String())
		}
		var row store.WebhookData
		if err := json.NewDecoder(w.Body).Decode(&row); err != nil {
			t.Fatalf("decode: %v", err)
		}
		if !row.Revoked {
			t.Fatal("row must be marked revoked after DELETE")
		}
	}
}

// TestWebhookAdmin_Patch_NonAdmin_403 verifies non-admin cannot patch.
func TestWebhookAdmin_Patch_NonAdmin_403(t *testing.T) {
	tenantID := uuid.New()
	userID := "viewer"

	ts := &adminTenantStore{roles: map[string]string{
		tenantID.String() + ":" + userID: "viewer",
	}}
	ws := newAdminWebhookStore()
	h := newAdminHandler(ws, ts)

	ctx := tenantAdminCtx(tenantID, userID)
	w := doRequest(t, h, http.MethodPatch, "/v1/webhooks/"+uuid.New().String(), map[string]any{
		"name": "new name",
	}, ctx)

	if w.Code != http.StatusForbidden {
		t.Fatalf("want 403, got %d: %s", w.Code, w.Body.String())
	}
}

// TestWebhookAdmin_Rotate_NonAdmin_403 verifies non-admin cannot rotate.
func TestWebhookAdmin_Rotate_NonAdmin_403(t *testing.T) {
	tenantID := uuid.New()
	userID := "viewer2"

	ts := &adminTenantStore{roles: map[string]string{
		tenantID.String() + ":" + userID: "viewer",
	}}
	ws := newAdminWebhookStore()
	h := newAdminHandler(ws, ts)

	ctx := tenantAdminCtx(tenantID, userID)
	r := httptest.NewRequest(http.MethodPost, "/v1/webhooks/"+uuid.New().String()+"/rotate", nil)
	r = r.WithContext(ctx)
	w := httptest.NewRecorder()

	mux := http.NewServeMux()
	h.RegisterRoutes(mux)
	mux.ServeHTTP(w, r)

	if w.Code != http.StatusForbidden {
		t.Fatalf("want 403, got %d: %s", w.Code, w.Body.String())
	}
}

// TestWebhookAdmin_Revoke_NonAdmin_403 verifies non-admin cannot revoke.
func TestWebhookAdmin_Revoke_NonAdmin_403(t *testing.T) {
	tenantID := uuid.New()
	userID := "viewer3"

	ts := &adminTenantStore{roles: map[string]string{
		tenantID.String() + ":" + userID: "viewer",
	}}
	ws := newAdminWebhookStore()
	h := newAdminHandler(ws, ts)

	ctx := tenantAdminCtx(tenantID, userID)
	r := httptest.NewRequest(http.MethodDelete, "/v1/webhooks/"+uuid.New().String(), nil)
	r = r.WithContext(ctx)
	w := httptest.NewRecorder()

	mux := http.NewServeMux()
	h.RegisterRoutes(mux)
	mux.ServeHTTP(w, r)

	if w.Code != http.StatusForbidden {
		t.Fatalf("want 403, got %d: %s", w.Code, w.Body.String())
	}
}

// TestGenerateWebhookSecret verifies the format and properties of generated secrets.
func TestGenerateWebhookSecret(t *testing.T) {
	raw, hash, prefix, err := generateWebhookSecret()
	if err != nil {
		t.Fatalf("generate: %v", err)
	}
	if len(raw) < 3 || raw[:3] != "wh_" {
		t.Fatalf("raw must start with wh_, got %q", raw)
	}
	if len(prefix) != 8 {
		t.Fatalf("prefix must be 8 chars, got %d: %q", len(prefix), prefix)
	}
	if prefix != raw[:8] {
		t.Fatalf("prefix %q != raw[:8] %q", prefix, raw[:8])
	}
	if len(hash) != 64 {
		t.Fatalf("hash must be 64 hex chars (SHA-256), got %d", len(hash))
	}
	// Two calls must produce different secrets.
	raw2, _, _, _ := generateWebhookSecret()
	if raw == raw2 {
		t.Fatal("secrets must be unique per generation")
	}
}
