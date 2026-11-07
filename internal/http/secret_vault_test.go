package http

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/nextlevelbuilder/goclaw/internal/store"
)

const vaultTestSecret = "hunter2-very-secret-9931"

// memVault is an in-memory SecretVaultStore with the same validation as the
// real stores. It keeps values so tests can prove they never come back out.
type memVault struct {
	store.SecretVaultStore
	entries map[uuid.UUID]*store.SecretVaultEntry
	values  map[uuid.UUID]store.SecretVaultPayload
	grants  map[uuid.UUID][]uuid.UUID
	audit   []store.SecretVaultAuditEntry
}

func newMemVault() *memVault {
	return &memVault{entries: map[uuid.UUID]*store.SecretVaultEntry{}, values: map[uuid.UUID]store.SecretVaultPayload{}, grants: map[uuid.UUID][]uuid.UUID{}}
}

func (m *memVault) Create(_ context.Context, e *store.SecretVaultEntry, p store.SecretVaultPayload) error {
	if err := store.ValidateSecretVaultEntry(e); err != nil {
		return err
	}
	if err := store.ValidateSecretVaultPayload(e.Kind, p); err != nil {
		return err
	}
	for _, x := range m.entries {
		if x.Name == e.Name {
			return store.ErrSecretVaultNameTaken
		}
	}
	e.ID = uuid.New()
	e.CreatedAt, e.UpdatedAt, e.Revision = time.Now(), time.Now(), 1
	cp := *e
	m.entries[e.ID], m.values[e.ID] = &cp, p
	return nil
}

func (m *memVault) Get(_ context.Context, id uuid.UUID) (*store.SecretVaultEntry, error) {
	if e, ok := m.entries[id]; ok {
		cp := *e
		return &cp, nil
	}
	return nil, store.ErrSecretVaultNotFound
}

func (m *memVault) List(context.Context) ([]store.SecretVaultEntry, error) {
	var out []store.SecretVaultEntry
	for _, e := range m.entries {
		out = append(out, *e)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out, nil
}

func (m *memVault) UpdateMeta(_ context.Context, id uuid.UUID, u store.SecretVaultMetaUpdate, expectedRev int64) (int64, error) {
	e, ok := m.entries[id]
	if !ok {
		return 0, store.ErrSecretVaultNotFound
	}
	if expectedRev != 0 && e.Revision != expectedRev {
		return e.Revision, store.ErrSecretVaultConflict
	}
	if u.AllowedDomains != nil {
		d, err := store.NormalizeSecretDomains(*u.AllowedDomains)
		if err != nil {
			return 0, err
		}
		e.AllowedDomains = d
	}
	if u.RequireApproval != nil {
		e.RequireApproval = *u.RequireApproval
	}
	if u.Hints != nil {
		e.Hints = *u.Hints
	}
	e.Revision++
	return e.Revision, nil
}

func (m *memVault) ReplaceValue(_ context.Context, id uuid.UUID, p store.SecretVaultPayload, expectedRev int64) (int64, error) {
	e, ok := m.entries[id]
	if !ok {
		return 0, store.ErrSecretVaultNotFound
	}
	if expectedRev != 0 && e.Revision != expectedRev {
		return e.Revision, store.ErrSecretVaultConflict
	}
	if err := store.ValidateSecretVaultPayload(e.Kind, p); err != nil {
		return 0, err
	}
	m.values[id] = p
	e.Revision++
	return e.Revision, nil
}

func (m *memVault) Delete(_ context.Context, id uuid.UUID) error {
	if _, ok := m.entries[id]; !ok {
		return store.ErrSecretVaultNotFound
	}
	delete(m.entries, id)
	delete(m.values, id)
	return nil
}

func (m *memVault) SetGrants(_ context.Context, id uuid.UUID, ids []uuid.UUID) error {
	m.grants[id] = ids
	return nil
}

func (m *memVault) ListGrants(_ context.Context, id uuid.UUID) ([]uuid.UUID, error) {
	return m.grants[id], nil
}

func (m *memVault) AppendAudit(_ context.Context, a *store.SecretVaultAuditEntry) error {
	m.audit = append(m.audit, *a)
	return nil
}

func (m *memVault) ListAudit(_ context.Context, id uuid.UUID, _ int) ([]store.SecretVaultAuditEntry, error) {
	var out []store.SecretVaultAuditEntry
	for _, a := range m.audit {
		if a.EntryID == id {
			out = append(out, a)
		}
	}
	return out, nil
}

func vaultMux(t *testing.T, ts store.TenantStore) (*http.ServeMux, *memVault) {
	t.Helper()
	setAdminToken(t, "test-admin-token")
	mv := newMemVault()
	mux := http.NewServeMux()
	NewSecretVaultHandler(mv, ts).RegisterRoutes(mux)
	return mux, mv
}

func vaultCall(mux *http.ServeMux, method, path string, body any) *httptest.ResponseRecorder {
	buf := &bytes.Buffer{}
	if body != nil {
		_ = json.NewEncoder(buf).Encode(body)
	}
	req := httptest.NewRequest(method, path, buf)
	req.Header.Set("Authorization", "Bearer test-admin-token")
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	return rec
}

func TestSecretVault_NeverReturnsTheValue(t *testing.T) {
	mux, mv := vaultMux(t, oauthAdminTS{})
	var all strings.Builder
	keep := func(rec *httptest.ResponseRecorder) *httptest.ResponseRecorder {
		all.WriteString(rec.Body.String())
		return rec
	}

	rec := keep(vaultCall(mux, "POST", "/v1/secret-vault", map[string]any{
		"name": "gh-login", "kind": "login", "allowed_domains": []string{"github.com"},
		"username": "bruce", "secret": vaultTestSecret,
	}))
	if rec.Code != http.StatusCreated {
		t.Fatalf("create: %d %s", rec.Code, rec.Body)
	}
	var created map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &created)
	id := created["id"].(string)
	if created["require_approval"] != true {
		t.Errorf("login must default to require_approval=true: %v", created)
	}
	// The response shape is part of the contract: metadata keys only.
	want := []string{"allowed_domains", "created_at", "created_by", "hints", "id", "kind", "name", "require_approval", "revision", "updated_at"}
	var got []string
	for k := range created {
		got = append(got, k)
	}
	sort.Strings(got)
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Errorf("entry shape = %v, want %v", got, want)
	}

	keep(vaultCall(mux, "GET", "/v1/secret-vault", nil))
	keep(vaultCall(mux, "PUT", "/v1/secret-vault/"+id, map[string]any{"hints": "work account"}))
	keep(vaultCall(mux, "PUT", "/v1/secret-vault/"+id+"/value", map[string]any{"username": "bruce", "secret": vaultTestSecret + "-v2"}))
	agent := uuid.New()
	keep(vaultCall(mux, "PUT", "/v1/secret-vault/"+id+"/grants", map[string]any{"agent_ids": []string{agent.String()}}))
	keep(vaultCall(mux, "GET", "/v1/secret-vault/"+id+"/grants", nil))
	keep(vaultCall(mux, "GET", "/v1/secret-vault/"+id+"/audit", nil))
	keep(vaultCall(mux, "POST", "/v1/secret-vault", map[string]any{"name": "Bad Name", "kind": "login", "secret": vaultTestSecret}))
	keep(vaultCall(mux, "DELETE", "/v1/secret-vault/"+id, nil))

	if strings.Contains(all.String(), vaultTestSecret) {
		t.Fatalf("a response leaks the secret value:\n%s", all.String())
	}
	var actions []string
	for _, a := range mv.audit {
		actions = append(actions, a.Action)
		if a.UserID == "" && a.Action == "" {
			t.Error("empty audit row")
		}
	}
	if strings.Join(actions, ",") != "created,updated,updated,updated,deleted" {
		t.Errorf("audit actions = %v", actions)
	}
}

func TestSecretVault_NonTenantAdminIsDenied(t *testing.T) {
	mux, mv := vaultMux(t, oauthDenyTS{})
	for _, c := range []struct{ method, path string }{
		{"GET", "/v1/secret-vault"},
		{"POST", "/v1/secret-vault"},
		{"DELETE", "/v1/secret-vault/" + uuid.NewString()},
		{"PUT", "/v1/secret-vault/" + uuid.NewString() + "/grants"},
		{"GET", "/v1/secret-vault/" + uuid.NewString() + "/audit"},
	} {
		rec := vaultCall(mux, c.method, c.path, map[string]any{"name": "x"})
		if rec.Code != http.StatusForbidden {
			t.Errorf("%s %s = %d, want 403", c.method, c.path, rec.Code)
		}
	}
	if len(mv.entries) != 0 {
		t.Error("a denied caller created an entry")
	}
}

func TestSecretVault_UnauthenticatedAndBadInput(t *testing.T) {
	mux, _ := vaultMux(t, oauthAdminTS{})
	req := httptest.NewRequest("GET", "/v1/secret-vault", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Errorf("no token = %d", rec.Code)
	}
	if rec := vaultCall(mux, "DELETE", "/v1/secret-vault/not-a-uuid", nil); rec.Code != http.StatusBadRequest {
		t.Errorf("bad id = %d", rec.Code)
	}
	if rec := vaultCall(mux, "DELETE", "/v1/secret-vault/"+uuid.NewString(), nil); rec.Code != http.StatusNotFound {
		t.Errorf("unknown id = %d", rec.Code)
	}
	body := map[string]any{"name": "k", "kind": "api_key", "allowed_domains": []string{"a.com"}, "secret": vaultTestSecret}
	if rec := vaultCall(mux, "POST", "/v1/secret-vault", body); rec.Code != http.StatusCreated {
		t.Fatalf("create = %d %s", rec.Code, rec.Body)
	}
	if rec := vaultCall(mux, "POST", "/v1/secret-vault", body); rec.Code != http.StatusConflict {
		t.Errorf("duplicate name = %d", rec.Code)
	} else if strings.Contains(rec.Body.String(), vaultTestSecret) {
		t.Error("conflict response echoes the value")
	}
}

func vaultCallIfMatch(mux *http.ServeMux, method, path, ifMatch string, body any) *httptest.ResponseRecorder {
	buf := &bytes.Buffer{}
	if body != nil {
		_ = json.NewEncoder(buf).Encode(body)
	}
	req := httptest.NewRequest(method, path, buf)
	req.Header.Set("Authorization", "Bearer test-admin-token")
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("If-Match", ifMatch)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	return rec
}

func TestSecretVault_IfMatchRevisionGuardsWrites(t *testing.T) {
	mux, mv := vaultMux(t, oauthAdminTS{})
	rec := vaultCall(mux, "POST", "/v1/secret-vault", map[string]any{
		"name": "k", "kind": "api_key", "allowed_domains": []string{"a.com"}, "secret": vaultTestSecret,
	})
	var created map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &created)
	id, path := created["id"].(string), "/v1/secret-vault/"+created["id"].(string)
	if created["revision"] != float64(1) {
		t.Fatalf("new entry revision = %v, want 1", created["revision"])
	}

	// Each accepted write moves the revision; the header may be quoted or weak.
	for i, header := range []string{`1`, `"2"`, `W/"3"`} {
		rec = vaultCallIfMatch(mux, "PUT", path, header, map[string]any{"hints": "h"})
		if rec.Code != http.StatusOK {
			t.Fatalf("write %d with If-Match %s = %d %s", i, header, rec.Code, rec.Body)
		}
	}
	rec = vaultCallIfMatch(mux, "PUT", path+"/value", "4", map[string]any{"secret": vaultTestSecret + "-v2"})
	if rec.Code != http.StatusOK {
		t.Fatalf("value write = %d %s", rec.Code, rec.Body)
	}

	// A stale revision is refused with 409, the current revision, and an audit row.
	for _, p := range []string{path, path + "/value"} {
		before := len(mv.audit)
		rec = vaultCallIfMatch(mux, "PUT", p, "1", map[string]any{"hints": "stale", "secret": "stale-value"})
		if rec.Code != http.StatusConflict {
			t.Fatalf("stale write to %s = %d, want 409", p, rec.Code)
		}
		var body map[string]any
		_ = json.Unmarshal(rec.Body.Bytes(), &body)
		if body["code"] != "revision_conflict" || body["current_revision"] != float64(5) {
			t.Fatalf("conflict body = %v", body)
		}
		if len(mv.audit) != before+1 || mv.audit[len(mv.audit)-1].Action != store.SecretAuditConflict {
			t.Fatalf("conflict was not audited: %v", mv.audit)
		}
	}
	if mv.entries[uuid.MustParse(id)].Hints == "stale" {
		t.Fatal("a refused write was applied")
	}

	// A malformed header is a client error, not a silent unconditional write.
	for _, bad := range []string{"abc", "0", "-3"} {
		if rec = vaultCallIfMatch(mux, "PUT", path, bad, map[string]any{"hints": "x"}); rec.Code != http.StatusBadRequest {
			t.Errorf("If-Match %q = %d, want 400", bad, rec.Code)
		}
	}

	// No header keeps the previous behaviour so existing clients are not broken.
	if rec = vaultCall(mux, "PUT", path, map[string]any{"hints": "legacy client"}); rec.Code != http.StatusOK {
		t.Errorf("write without If-Match = %d %s", rec.Code, rec.Body)
	}
}
