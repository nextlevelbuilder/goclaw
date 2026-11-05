//go:build sqlite || sqliteonly

package sqlitestore

import (
	"context"
	"database/sql"
	"errors"
	"testing"

	"github.com/google/uuid"

	"github.com/nextlevelbuilder/goclaw/internal/store"
)

const vaultTestKey = "0123456789abcdef0123456789abcdef"

func openVaultTestDB(t *testing.T) *sql.DB {
	t.Helper()
	db := openTestDB(t)
	if err := EnsureSchema(db); err != nil {
		t.Fatalf("EnsureSchema: %v", err)
	}
	initSqlx(db)
	return db
}

func vaultCtx(t uuid.UUID) context.Context {
	return store.WithTenantID(context.Background(), t)
}

func newVaultEntry(name, kind string) *store.SecretVaultEntry {
	return &store.SecretVaultEntry{Name: name, Kind: kind, AllowedDomains: []string{"api.example.com"}, CreatedBy: "user1"}
}

func TestSecretVault_CRUDAndResolve(t *testing.T) {
	db := openVaultTestDB(t)
	tid := seedTenant(t, db, "vault-a")
	agent := seedAgent(t, db, tid, "agent-a")
	other := seedAgent(t, db, tid, "agent-b")
	s := NewSQLiteSecretVaultStore(db, vaultTestKey)
	ctx := vaultCtx(tid)

	e := newVaultEntry("gh-token", store.SecretKindAPIKey)
	if err := s.Create(ctx, e, store.SecretVaultPayload{Secret: "tok-123"}); err != nil {
		t.Fatal(err)
	}
	if err := s.Create(ctx, newVaultEntry("gh-token", store.SecretKindAPIKey), store.SecretVaultPayload{Secret: "x"}); !errors.Is(err, store.ErrSecretVaultNameTaken) {
		t.Fatalf("duplicate name: %v", err)
	}

	var raw string
	if err := db.QueryRow(`SELECT encrypted_payload FROM secret_vault_entries WHERE id = ?`, e.ID).Scan(&raw); err != nil {
		t.Fatal(err)
	}
	if raw == "" || raw == "tok-123" {
		t.Fatal("payload not encrypted at rest")
	}

	if _, _, err := s.ResolveForUse(ctx, agent, "gh-token"); !errors.Is(err, store.ErrSecretVaultNotGranted) {
		t.Fatalf("want NotGranted, got %v", err)
	}
	if err := s.SetGrants(ctx, e.ID, []uuid.UUID{agent, agent}); err != nil {
		t.Fatal(err)
	}
	got, p, err := s.ResolveForUse(ctx, agent, "gh-token")
	if err != nil || p.Secret != "tok-123" || got.Name != "gh-token" {
		t.Fatalf("resolve failed: %v", err)
	}
	if _, _, err := s.ResolveForUse(ctx, other, "gh-token"); !errors.Is(err, store.ErrSecretVaultNotGranted) {
		t.Fatalf("other agent: %v", err)
	}
	if _, _, err := s.ResolveForUse(ctx, agent, "missing"); !errors.Is(err, store.ErrSecretVaultNotFound) {
		t.Fatalf("missing: %v", err)
	}
	if meta, err := s.ListGrantedMeta(ctx, agent); err != nil || len(meta) != 1 {
		t.Fatalf("granted meta: %v %d", err, len(meta))
	}
	if grants, _ := s.ListGrants(ctx, e.ID); len(grants) != 1 {
		t.Fatalf("grants: %d", len(grants))
	}

	appr, hints, doms := true, "use for GitHub", []string{"*.example.com"}
	if err := s.UpdateMeta(ctx, e.ID, store.SecretVaultMetaUpdate{RequireApproval: &appr, Hints: &hints, AllowedDomains: &doms}); err != nil {
		t.Fatal(err)
	}
	g, err := s.Get(ctx, e.ID)
	if err != nil || !g.RequireApproval || g.Hints != hints || g.AllowedDomains[0] != "*.example.com" {
		t.Fatalf("get after update: %v", err)
	}
	if err := s.ReplaceValue(ctx, e.ID, store.SecretVaultPayload{Secret: "tok-456"}); err != nil {
		t.Fatal(err)
	}
	if _, p, _ := s.ResolveForUse(ctx, agent, "gh-token"); p.Secret != "tok-456" {
		t.Fatal("value not replaced")
	}
	if err := s.ReplaceValue(ctx, e.ID, store.SecretVaultPayload{Username: "u", Secret: "x"}); !errors.Is(err, store.ErrSecretVaultInvalid) {
		t.Fatalf("api_key with username: %v", err)
	}

	if err := s.AppendAudit(ctx, &store.SecretVaultAuditEntry{EntryID: e.ID, EntryName: e.Name, AgentID: &agent, Action: store.SecretAuditUsed, TargetHost: "api.example.com", Outcome: "ok"}); err != nil {
		t.Fatal(err)
	}
	if a, err := s.ListAudit(ctx, e.ID, 10); err != nil || len(a) != 1 || a[0].Action != store.SecretAuditUsed {
		t.Fatalf("audit: %v", err)
	}

	if err := s.Delete(ctx, e.ID); err != nil {
		t.Fatal(err)
	}
	if err := s.Delete(ctx, e.ID); !errors.Is(err, store.ErrSecretVaultNotFound) {
		t.Fatalf("second delete: %v", err)
	}
	var n int
	_ = db.QueryRow(`SELECT COUNT(*) FROM secret_vault_grants`).Scan(&n)
	if n != 0 {
		t.Fatal("grants not cascaded")
	}
	if a, _ := s.ListAudit(ctx, e.ID, 10); len(a) != 1 {
		t.Fatal("audit should survive delete")
	}
}

func TestSecretVault_TenantIsolationAndGuards(t *testing.T) {
	db := openVaultTestDB(t)
	ta, tb := seedTenant(t, db, "vault-ta"), seedTenant(t, db, "vault-tb")
	agentA, agentB := seedAgent(t, db, ta, "agent-ta"), seedAgent(t, db, tb, "agent-tb")
	s := NewSQLiteSecretVaultStore(db, vaultTestKey)

	e := newVaultEntry("shared-name", store.SecretKindLogin)
	if err := s.Create(vaultCtx(ta), e, store.SecretVaultPayload{Username: "u", Secret: "p"}); err != nil {
		t.Fatal(err)
	}
	if err := s.Create(vaultCtx(tb), newVaultEntry("shared-name", store.SecretKindLogin), store.SecretVaultPayload{Username: "u2", Secret: "p2"}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Get(vaultCtx(tb), e.ID); !errors.Is(err, store.ErrSecretVaultNotFound) {
		t.Fatalf("cross-tenant get: %v", err)
	}
	if err := s.Delete(vaultCtx(tb), e.ID); !errors.Is(err, store.ErrSecretVaultNotFound) {
		t.Fatalf("cross-tenant delete: %v", err)
	}
	if err := s.SetGrants(vaultCtx(ta), e.ID, []uuid.UUID{agentB}); !errors.Is(err, store.ErrSecretVaultAgentInvalid) {
		t.Fatalf("foreign agent grant: %v", err)
	}
	if err := s.SetGrants(vaultCtx(ta), e.ID, []uuid.UUID{agentA}); err != nil {
		t.Fatal(err)
	}
	if _, _, err := s.ResolveForUse(vaultCtx(tb), agentA, "shared-name"); !errors.Is(err, store.ErrSecretVaultNotGranted) {
		t.Fatalf("cross-tenant resolve: %v", err)
	}
	if l, _ := s.List(vaultCtx(ta)); len(l) != 1 {
		t.Fatalf("list scoped: %d", len(l))
	}
	if _, err := s.List(context.Background()); !errors.Is(err, store.ErrSecretVaultNoTenant) {
		t.Fatalf("no tenant: %v", err)
	}
}

func TestSecretVault_ListGrantedSecrets(t *testing.T) {
	db := openVaultTestDB(t)
	tid, tother := seedTenant(t, db, "vault-lg"), seedTenant(t, db, "vault-lg2")
	agent, agent2 := seedAgent(t, db, tid, "agent-lg"), seedAgent(t, db, tid, "agent-lg2")
	s := NewSQLiteSecretVaultStore(db, vaultTestKey)
	ctx := vaultCtx(tid)

	a, b := newVaultEntry("alpha", store.SecretKindAPIKey), newVaultEntry("bravo", store.SecretKindLogin)
	if err := s.Create(ctx, a, store.SecretVaultPayload{Secret: "alpha-value"}); err != nil {
		t.Fatal(err)
	}
	if err := s.Create(ctx, b, store.SecretVaultPayload{Username: "bob", Secret: "bravo-value"}); err != nil {
		t.Fatal(err)
	}
	if err := s.SetGrants(ctx, a.ID, []uuid.UUID{agent}); err != nil {
		t.Fatal(err)
	}
	if err := s.SetGrants(ctx, b.ID, []uuid.UUID{agent, agent2}); err != nil {
		t.Fatal(err)
	}

	got, err := s.ListGrantedSecrets(ctx, agent)
	if err != nil || len(got) != 2 {
		t.Fatalf("agent: %v %d", err, len(got))
	}
	if got[0].Entry.Name != "alpha" || got[0].Payload.Secret != "alpha-value" || got[1].Payload.Username != "bob" {
		t.Fatal("wrong entries or payloads")
	}
	if g2, _ := s.ListGrantedSecrets(ctx, agent2); len(g2) != 1 || g2[0].Entry.Name != "bravo" {
		t.Fatalf("agent2 must only see its own grant: %+v", g2)
	}
	if other, _ := s.ListGrantedSecrets(vaultCtx(tother), agent); len(other) != 0 {
		t.Fatal("cross-tenant list must be empty")
	}
	if _, err := s.ListGrantedSecrets(context.Background(), agent); !errors.Is(err, store.ErrSecretVaultNoTenant) {
		t.Fatalf("no tenant: %v", err)
	}
}

func TestSecretVault_DisabledWithoutKey(t *testing.T) {
	db := openTestDB(t)
	if NewSQLiteSecretVaultStore(db, "") != nil {
		t.Fatal("store must be nil without key")
	}
	if newSecretVaultStoreOrNil(db, "") != nil {
		t.Fatal("interface must be a true nil without key")
	}
}

func TestSQLiteSchemaUpgrade_63_to_64_SecretVault(t *testing.T) {
	db := openTestDB(t)
	if err := EnsureSchema(db); err != nil {
		t.Fatal(err)
	}
	mustExec(t, db, `DROP TABLE secret_vault_audit`)
	mustExec(t, db, `DROP TABLE secret_vault_grants`)
	mustExec(t, db, `DROP TABLE secret_vault_entries`)
	mustExec(t, db, `UPDATE schema_version SET version = 63`)
	if err := EnsureSchema(db); err != nil {
		t.Fatalf("v63 to v64: %v", err)
	}
	var v int
	if err := db.QueryRow(`SELECT version FROM schema_version LIMIT 1`).Scan(&v); err != nil || v != SchemaVersion {
		t.Fatalf("version %d err %v", v, err)
	}
	for _, tbl := range []string{"secret_vault_entries", "secret_vault_grants", "secret_vault_audit"} {
		if tableSQL(t, db, tbl) == "" {
			t.Fatalf("%s missing", tbl)
		}
	}
}
