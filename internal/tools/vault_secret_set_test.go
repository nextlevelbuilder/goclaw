package tools

import (
	"context"
	"strings"
	"sync"
	"testing"

	"github.com/google/uuid"

	"github.com/nextlevelbuilder/goclaw/internal/edition"
	"github.com/nextlevelbuilder/goclaw/internal/secretvault"
	"github.com/nextlevelbuilder/goclaw/internal/store"
)

const setSecretValue = "eyJhbGciOiJub25lIn0.set-secret-value-0451"

// vaultWriteStore is an in-memory store with the agent-write rules of the
// real stores: agent entries are pending and names are unique.
type vaultWriteStore struct {
	store.SecretVaultStore
	mu      sync.Mutex
	entries map[string]*store.SecretVaultEntry
	values  map[uuid.UUID]store.SecretVaultPayload
	grants  map[uuid.UUID][]uuid.UUID
	audit   []store.SecretVaultAuditEntry
}

func newVaultWriteStore() *vaultWriteStore {
	return &vaultWriteStore{
		entries: map[string]*store.SecretVaultEntry{},
		values:  map[uuid.UUID]store.SecretVaultPayload{},
		grants:  map[uuid.UUID][]uuid.UUID{},
	}
}

func (s *vaultWriteStore) Create(_ context.Context, e *store.SecretVaultEntry, p store.SecretVaultPayload) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.entries[e.Name]; ok {
		return store.ErrSecretVaultNameTaken
	}
	e.ID = uuid.New()
	if e.CreatedByAgent != nil {
		e.Status, e.CreatedVia = store.SecretVaultStatusPending, store.SecretVaultCreatedViaAgent
	}
	cp := *e
	s.entries[e.Name] = &cp
	s.values[e.ID] = p
	return nil
}

func (s *vaultWriteStore) List(context.Context) ([]store.SecretVaultEntry, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var out []store.SecretVaultEntry
	for _, e := range s.entries {
		out = append(out, *e)
	}
	return out, nil
}

func (s *vaultWriteStore) ListGrants(_ context.Context, id uuid.UUID) ([]uuid.UUID, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]uuid.UUID(nil), s.grants[id]...), nil
}

func (s *vaultWriteStore) SetGrants(_ context.Context, id uuid.UUID, agents []uuid.UUID) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.grants[id] = agents
	return nil
}

func (s *vaultWriteStore) AppendAudit(_ context.Context, a *store.SecretVaultAuditEntry) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.audit = append(s.audit, *a)
	return nil
}

func (s *vaultWriteStore) ResolveForUse(context.Context, uuid.UUID, string) (*store.SecretVaultEntry, store.SecretVaultPayload, error) {
	return nil, store.SecretVaultPayload{}, store.ErrSecretVaultNotGranted
}

func (s *vaultWriteStore) entry(name string) *store.SecretVaultEntry {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.entries[name]
}

// writeCtx builds a run context whose runtime has no Web UI (approvals fail
// fast with ErrNoUI).
func writeCtx(st store.SecretVaultStore, allow bool) (context.Context, *secretvault.Runtime) {
	rt := secretvault.NewRuntime(st, uuid.New(), "user1")
	rt.AllowWrite = allow
	rt.Forms, rt.SessionKey = secretvault.NewFormManager(), "sess1"
	ctx := store.WithUserID(store.WithTenantID(context.Background(), uuid.New()), "user1")
	return secretvault.WithRuntime(ctx, rt), rt
}

func setArgs(scope string) map[string]any {
	return map[string]any{"name": "app-token", "value": setSecretValue, "domains": []any{"api.example.com"}, "scope": scope}
}

func resultText(r *Result) string {
	s := r.ForLLM + r.ForUser + r.Deliverable
	if r.Err != nil {
		s += r.Err.Error()
	}
	return s
}

// Lite has no surface to approve agent-written secrets: the tools are not
// registered there, and a runtime call is refused anyway.
func TestVaultWriteTools_NotRegisteredOrUsableInLite(t *testing.T) {
	std := NewRegistry()
	RegisterVaultWriteTools(std)
	for _, name := range []string{"vault_secret_set", "vault_grant"} {
		if _, ok := std.Get(name); !ok {
			t.Fatalf("%s must be registered in the standard edition", name)
		}
	}

	prev := edition.Current()
	edition.SetCurrent(edition.Lite)
	t.Cleanup(func() { edition.SetCurrent(prev) })
	lite := NewRegistry()
	RegisterVaultWriteTools(lite)
	for _, name := range []string{"vault_secret_set", "vault_grant"} {
		if _, ok := lite.Get(name); ok {
			t.Fatalf("%s must not be registered in Lite", name)
		}
	}
	st := newVaultWriteStore()
	ctx, _ := writeCtx(st, true)
	res := NewVaultSecretSetTool().Execute(ctx, setArgs("vault"))
	if !res.IsError || !strings.Contains(res.ForLLM, "this edition") || strings.Contains(resultText(res), setSecretValue) {
		t.Fatalf("Lite write: %+v", res)
	}
	if st.entry("app-token") != nil {
		t.Fatal("nothing may be stored in Lite")
	}
}

func TestVaultWriteTools_DisabledAgentIsRefused(t *testing.T) {
	st := newVaultWriteStore()
	ctx, _ := writeCtx(st, false)
	for name, res := range map[string]*Result{
		"set vault":   NewVaultSecretSetTool().Execute(ctx, setArgs("vault")),
		"set session": NewVaultSecretSetTool().Execute(ctx, setArgs("session")),
		"grant":       NewVaultGrantTool().Execute(ctx, map[string]any{"secret": "app-token", "agent_key": "other"}),
	} {
		if !res.IsError || !strings.Contains(res.ForLLM, "not enabled for this agent") {
			t.Errorf("%s: %+v", name, res)
		}
		if strings.Contains(resultText(res), setSecretValue) {
			t.Errorf("%s leaks the value", name)
		}
	}
	if st.entry("app-token") != nil {
		t.Fatal("nothing may be stored")
	}
	if len(st.audit) != 3 {
		t.Fatalf("every refusal must be audited, got %d rows", len(st.audit))
	}
}

func TestVaultSecretSet_VaultScopeCreatesPendingAndReturnsOnlyThePlaceholder(t *testing.T) {
	st := newVaultWriteStore()
	ctx, rt := writeCtx(st, true)

	res := NewVaultSecretSetTool().Execute(ctx, setArgs("vault"))
	if res.IsError || !strings.Contains(res.ForLLM, "{{secret:app-token}}") || !strings.Contains(res.ForLLM, "pending") {
		t.Fatalf("result %+v", res)
	}
	if strings.Contains(resultText(res), setSecretValue) {
		t.Fatal("result leaks the value")
	}
	e := st.entry("app-token")
	if e == nil || e.Status != store.SecretVaultStatusPending || *e.CreatedByAgent != rt.AgentID {
		t.Fatalf("entry %+v", e)
	}
	if strings.Contains(rt.Redactor.Redact(setSecretValue), setSecretValue) {
		t.Fatal("redactor must know the value")
	}

	again := NewVaultSecretSetTool().Execute(ctx, setArgs("vault"))
	if !again.IsError || !strings.Contains(again.ForLLM, "cannot be used") {
		t.Fatalf("existing name must be rejected: %+v", again)
	}
}

func TestVaultSecretSet_SessionScopeIsNotPersisted(t *testing.T) {
	st := newVaultWriteStore()
	ctx, rt := writeCtx(st, true)
	res := NewVaultSecretSetTool().Execute(ctx, setArgs("session"))
	if res.IsError || !strings.Contains(res.ForLLM, "{{secret:app-token}}") {
		t.Fatalf("result %+v", res)
	}
	if st.entry("app-token") != nil {
		t.Fatal("session scope must not reach the store")
	}
	if _, _, ok := rt.Forms.Sessions.Get(store.TenantIDFromContext(ctx), "sess1", "app-token"); !ok {
		t.Fatal("session secret missing")
	}
}

func TestVaultSecretSet_RejectsBadInputWithoutEchoingTheValue(t *testing.T) {
	ctx, _ := writeCtx(newVaultWriteStore(), true)
	cases := map[string]func(map[string]any){
		"bad scope":  func(a map[string]any) { a["scope"] = "forever" },
		"bad name":   func(a map[string]any) { a["name"] = "Bad Name" },
		"no domains": func(a map[string]any) { a["domains"] = []any{} },
		"too large":  func(a map[string]any) { a["value"] = strings.Repeat("x", secretvault.MaxWriteValueBytes+1) },
		"empty":      func(a map[string]any) { a["value"] = "" },
	}
	for name, mutate := range cases {
		args := setArgs("vault")
		mutate(args)
		res := NewVaultSecretSetTool().Execute(ctx, args)
		if !res.IsError {
			t.Errorf("%s accepted", name)
		}
		if strings.Contains(resultText(res), setSecretValue) {
			t.Errorf("%s: error leaks the value", name)
		}
	}
	if res := NewVaultSecretSetTool().Execute(context.Background(), setArgs("vault")); !res.IsError {
		t.Error("no runtime must be an error")
	}
}

func TestVaultGrant_NeedsALiveApprovalFromATenantAdmin(t *testing.T) {
	st := newVaultWriteStore()
	ctx, rt := writeCtx(st, true)
	other := uuid.New()
	rt.Agents = func(context.Context, string) (uuid.UUID, error) { return other, nil }
	if res := NewVaultSecretSetTool().Execute(ctx, setArgs("vault")); res.IsError {
		t.Fatal(res.ForLLM)
	}
	// Without a way to read the user's tenant role nothing counts.
	res := NewVaultGrantTool().Execute(ctx, map[string]any{"secret": "app-token", "agent_key": "helper"})
	if !res.IsError || !strings.Contains(res.ForLLM, "tenant admin") {
		t.Fatalf("a user who is no tenant admin must be refused: %+v", res)
	}
	rt.TenantRoles = func(context.Context, uuid.UUID, string) (string, error) { return store.TenantRoleAdmin, nil }
	res = NewVaultGrantTool().Execute(ctx, map[string]any{"secret": "app-token", "agent_key": "helper"})
	if !res.IsError || !strings.Contains(res.ForLLM, "did not approve") {
		t.Fatalf("no Web UI must refuse the grant: %+v", res)
	}
	if grants, _ := st.ListGrants(ctx, st.entry("app-token").ID); len(grants) != 1 || grants[0] != rt.AgentID {
		t.Fatalf("grants %v", grants)
	}
	if res := NewVaultGrantTool().Execute(ctx, map[string]any{"secret": "app-token"}); !res.IsError {
		t.Error("missing agent_key accepted")
	}
}
