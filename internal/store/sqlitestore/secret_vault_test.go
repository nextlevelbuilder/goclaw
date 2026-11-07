//go:build sqlite || sqliteonly

package sqlitestore

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
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
	if _, err := s.UpdateMeta(ctx, e.ID, store.SecretVaultMetaUpdate{RequireApproval: &appr, Hints: &hints, AllowedDomains: &doms}, 0); err != nil {
		t.Fatal(err)
	}
	g, err := s.Get(ctx, e.ID)
	if err != nil || !g.RequireApproval || g.Hints != hints || g.AllowedDomains[0] != "*.example.com" {
		t.Fatalf("get after update: %v", err)
	}
	if _, err := s.ReplaceValue(ctx, e.ID, store.SecretVaultPayload{Secret: "tok-456"}, 0); err != nil {
		t.Fatal(err)
	}
	if _, p, _ := s.ResolveForUse(ctx, agent, "gh-token"); p.Secret != "tok-456" {
		t.Fatal("value not replaced")
	}
	if _, err := s.ReplaceValue(ctx, e.ID, store.SecretVaultPayload{Username: "u", Secret: "x"}, 0); !errors.Is(err, store.ErrSecretVaultInvalid) {
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

// AddGrant adds one agent without rewriting the set, so a grant removed by
// someone else in between stays removed.
func TestSecretVault_AddGrantKeepsOtherChanges(t *testing.T) {
	db := openVaultTestDB(t)
	ta, tb := seedTenant(t, db, "vault-add-a"), seedTenant(t, db, "vault-add-b")
	agentA, agentA2 := seedAgent(t, db, ta, "add-a1"), seedAgent(t, db, ta, "add-a2")
	agentB := seedAgent(t, db, tb, "add-b")
	s := NewSQLiteSecretVaultStore(db, vaultTestKey)
	ctx := vaultCtx(ta)

	e := newVaultEntry("add-grant", store.SecretKindAPIKey)
	if err := s.Create(ctx, e, store.SecretVaultPayload{Secret: "v-add-grant"}); err != nil {
		t.Fatal(err)
	}
	if err := s.SetGrants(ctx, e.ID, []uuid.UUID{agentA}); err != nil {
		t.Fatal(err)
	}
	if err := s.SetGrants(ctx, e.ID, nil); err != nil {
		t.Fatal(err)
	}
	if err := s.AddGrant(ctx, e.ID, agentA2); err != nil {
		t.Fatalf("add: %v", err)
	}
	if err := s.AddGrant(ctx, e.ID, agentA2); err != nil {
		t.Fatalf("add twice must be a no-op: %v", err)
	}
	if got, err := s.ListGrants(ctx, e.ID); err != nil || len(got) != 1 || got[0] != agentA2 {
		t.Fatalf("grants %v err=%v", got, err)
	}
	if err := s.AddGrant(ctx, e.ID, agentB); !errors.Is(err, store.ErrSecretVaultAgentInvalid) {
		t.Fatalf("foreign-tenant agent: %v", err)
	}
	if err := s.AddGrant(ctx, uuid.New(), agentA2); !errors.Is(err, store.ErrSecretVaultNotFound) {
		t.Fatalf("unknown entry: %v", err)
	}
	if err := s.AddGrant(vaultCtx(tb), e.ID, agentA2); !errors.Is(err, store.ErrSecretVaultNotFound) {
		t.Fatalf("other tenant: %v", err)
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

func newAgentVaultEntry(name string, agent uuid.UUID) *store.SecretVaultEntry {
	e := newVaultEntry(name, store.SecretKindAPIKey)
	e.CreatedByAgent = &agent
	return e
}

func TestSecretVault_AgentCreatedIsPendingUntilApproved(t *testing.T) {
	db := openVaultTestDB(t)
	tid := seedTenant(t, db, "vault-pending")
	agent := seedAgent(t, db, tid, "agent-pending")
	s := NewSQLiteSecretVaultStore(db, vaultTestKey)
	ctx := vaultCtx(tid)

	user := newVaultEntry("user-made", store.SecretKindAPIKey)
	user.Status = store.SecretVaultStatusPending // ignored: user entries are always active
	if err := s.Create(ctx, user, store.SecretVaultPayload{Secret: "user-value"}); err != nil {
		t.Fatal(err)
	}
	if user.Status != store.SecretVaultStatusActive || user.CreatedVia != store.SecretVaultCreatedViaUser || user.CreatedByAgent != nil {
		t.Fatalf("user entry origin: %+v", user)
	}

	e := newAgentVaultEntry("agent-made", agent)
	e.Status = store.SecretVaultStatusActive // ignored: agent entries are always pending
	if err := s.Create(ctx, e, store.SecretVaultPayload{Secret: "agent-value"}); err != nil {
		t.Fatal(err)
	}
	got, err := s.Get(ctx, e.ID)
	if err != nil || got.Status != store.SecretVaultStatusPending || got.CreatedVia != store.SecretVaultCreatedViaAgent ||
		got.CreatedByAgent == nil || *got.CreatedByAgent != agent {
		t.Fatalf("agent entry after create: %+v %v", got, err)
	}
	if err := s.SetGrants(ctx, e.ID, []uuid.UUID{agent}); err != nil {
		t.Fatal(err)
	}
	if _, _, err := s.ResolveForUse(ctx, agent, "agent-made"); !errors.Is(err, store.ErrSecretVaultNotGranted) {
		t.Fatalf("pending entry must resolve as not granted, got %v", err)
	}
	if meta, _ := s.ListGrantedMeta(ctx, agent); len(meta) != 0 {
		t.Fatalf("pending entry in granted meta: %+v", meta)
	}
	if sec, _ := s.ListGrantedSecrets(ctx, agent); len(sec) != 0 {
		t.Fatal("pending entry in granted secrets")
	}
	audit, err := s.ListAudit(ctx, e.ID, 10)
	if err != nil || len(audit) != 1 || audit[0].Action != store.SecretAuditCreated ||
		audit[0].Outcome != store.SecretAuditOutcomeAgentPending || audit[0].AgentID == nil || *audit[0].AgentID != agent {
		t.Fatalf("created audit: %+v %v", audit, err)
	}

	if err := s.Approve(ctx, e.ID, nil); !errors.Is(err, store.ErrSecretVaultInvalid) {
		t.Fatalf("approve without domains: %v", err)
	}
	if err := s.Approve(ctx, uuid.New(), []string{"a.example.com"}); !errors.Is(err, store.ErrSecretVaultNotFound) {
		t.Fatalf("approve missing: %v", err)
	}
	if err := s.Approve(ctx, user.ID, []string{"a.example.com"}); !errors.Is(err, store.ErrSecretVaultNotPending) {
		t.Fatalf("approve active: %v", err)
	}
	approver := store.WithUserID(ctx, "admin-1")
	if err := s.Approve(approver, e.ID, []string{"Final.Example.com"}); err != nil {
		t.Fatal(err)
	}
	if err := s.Approve(approver, e.ID, []string{"final.example.com"}); !errors.Is(err, store.ErrSecretVaultNotPending) {
		t.Fatalf("second approve: %v", err)
	}
	resolved, p, err := s.ResolveForUse(ctx, agent, "agent-made")
	// The confirmed domains are bound into the ciphertext: approval re-seals
	// the value and moves the entry to the next revision.
	if err != nil || p.Secret != "agent-value" || resolved.Status != store.SecretVaultStatusActive ||
		len(resolved.AllowedDomains) != 1 || resolved.AllowedDomains[0] != "final.example.com" || resolved.Revision != 2 {
		t.Fatalf("resolve after approve: %+v %v", resolved, err)
	}
	if meta, _ := s.ListGrantedMeta(ctx, agent); len(meta) != 1 {
		t.Fatal("approved entry missing from granted meta")
	}
	if sec, _ := s.ListGrantedSecrets(ctx, agent); len(sec) != 1 || sec[0].Payload.Secret != "agent-value" {
		t.Fatal("approved entry missing from granted secrets")
	}
	audit, _ = s.ListAudit(ctx, e.ID, 10)
	var approved bool
	for _, a := range audit {
		if a.Action == store.SecretAuditApproved && a.UserID == "admin-1" {
			approved = true
		}
	}
	if !approved {
		t.Fatalf("approved audit row missing: %+v", audit)
	}
}

func TestSecretVault_AgentCreateNeverOverwrites(t *testing.T) {
	db := openVaultTestDB(t)
	tid := seedTenant(t, db, "vault-collide")
	agent := seedAgent(t, db, tid, "agent-collide")
	s := NewSQLiteSecretVaultStore(db, vaultTestKey)
	ctx := vaultCtx(tid)

	orig := newVaultEntry("taken", store.SecretKindAPIKey)
	if err := s.Create(ctx, orig, store.SecretVaultPayload{Secret: "original"}); err != nil {
		t.Fatal(err)
	}
	if err := s.Create(ctx, newAgentVaultEntry("taken", agent), store.SecretVaultPayload{Secret: "attacker"}); !errors.Is(err, store.ErrSecretVaultNameTaken) {
		t.Fatalf("agent collision: %v", err)
	}
	if err := s.SetGrants(ctx, orig.ID, []uuid.UUID{agent}); err != nil {
		t.Fatal(err)
	}
	if _, p, err := s.ResolveForUse(ctx, agent, "taken"); err != nil || p.Secret != "original" {
		t.Fatalf("value replaced by agent create: %v", err)
	}
	if n, _ := s.CountPendingByAgent(ctx, agent); n != 0 {
		t.Fatalf("failed create counted as pending: %d", n)
	}
	var created int
	_ = db.QueryRow(`SELECT COUNT(*) FROM secret_vault_audit WHERE action = ?`, store.SecretAuditCreated).Scan(&created)
	if created != 0 {
		t.Fatal("failed agent create must not leave a created audit row")
	}
}

func TestSecretVault_PendingCapAndTenantIsolation(t *testing.T) {
	db := openVaultTestDB(t)
	ta, tb := seedTenant(t, db, "vault-cap-a"), seedTenant(t, db, "vault-cap-b")
	agentA, agentA2, agentB := seedAgent(t, db, ta, "agent-cap-a"), seedAgent(t, db, ta, "agent-cap-a2"), seedAgent(t, db, tb, "agent-cap-b")
	s := NewSQLiteSecretVaultStore(db, vaultTestKey)
	ctxA, ctxB := vaultCtx(ta), vaultCtx(tb)

	var first uuid.UUID
	for i := range store.SecretVaultMaxPendingPerAgent {
		e := newAgentVaultEntry(fmt.Sprintf("pending-%02d", i), agentA)
		if err := s.Create(ctxA, e, store.SecretVaultPayload{Secret: "v"}); err != nil {
			t.Fatalf("create %d: %v", i, err)
		}
		if i == 0 {
			first = e.ID
		}
	}
	if n, err := s.CountPendingByAgent(ctxA, agentA); err != nil || n != store.SecretVaultMaxPendingPerAgent {
		t.Fatalf("count: %d %v", n, err)
	}
	if err := s.Create(ctxA, newAgentVaultEntry("one-too-many", agentA), store.SecretVaultPayload{Secret: "v"}); !errors.Is(err, store.ErrSecretVaultPendingLimit) {
		t.Fatalf("cap: %v", err)
	}
	if err := s.Create(ctxA, newAgentVaultEntry("other-agent", agentA2), store.SecretVaultPayload{Secret: "v"}); err != nil {
		t.Fatalf("cap must be per agent: %v", err)
	}
	if err := s.Approve(ctxA, first, []string{"api.example.com"}); err != nil {
		t.Fatal(err)
	}
	if n, _ := s.CountPendingByAgent(ctxA, agentA); n != store.SecretVaultMaxPendingPerAgent-1 {
		t.Fatalf("count after approve: %d", n)
	}
	if err := s.Create(ctxA, newAgentVaultEntry("one-too-many", agentA), store.SecretVaultPayload{Secret: "v"}); err != nil {
		t.Fatalf("create after approve frees a slot: %v", err)
	}

	if n, err := s.CountPendingByAgent(ctxB, agentA); err != nil || n != 0 {
		t.Fatalf("cross-tenant count: %d %v", n, err)
	}
	if err := s.Approve(ctxB, first, []string{"b.example.com"}); !errors.Is(err, store.ErrSecretVaultNotFound) {
		t.Fatalf("cross-tenant approve: %v", err)
	}
	if err := s.Create(ctxA, newAgentVaultEntry("foreign-agent", agentB), store.SecretVaultPayload{Secret: "v"}); !errors.Is(err, store.ErrSecretVaultAgentInvalid) {
		t.Fatalf("foreign agent create: %v", err)
	}
	if _, err := s.CountPendingByAgent(context.Background(), agentA); !errors.Is(err, store.ErrSecretVaultNoTenant) {
		t.Fatalf("no tenant count: %v", err)
	}
	if err := s.Approve(context.Background(), first, []string{"a.example.com"}); !errors.Is(err, store.ErrSecretVaultNoTenant) {
		t.Fatalf("no tenant approve: %v", err)
	}
}

func TestSQLiteSchema_FreshDBHasSecretVaultOriginColumns(t *testing.T) {
	db := openVaultTestDB(t)
	for _, col := range []string{"status", "created_via", "created_by_agent"} {
		ok, err := sqliteColumnExists(db, "secret_vault_entries", col)
		if err != nil || !ok {
			t.Fatalf("column %s missing: %v", col, err)
		}
	}
	tid := seedTenant(t, db, "vault-check")
	_, err := db.Exec(`INSERT INTO secret_vault_entries (id, tenant_id, name, kind, encrypted_payload, allowed_domains, status)
		VALUES (?, ?, 'bad', 'api_key', 'x', '["a.com"]', 'bogus')`, uuid.New(), tid)
	if err == nil {
		t.Fatal("status CHECK constraint missing")
	}
}

func TestSQLiteSchemaUpgrade_64_to_66_SecretVaultRevisionAndOrigin(t *testing.T) {
	db := openTestDB(t)
	if err := EnsureSchema(db); err != nil {
		t.Fatal(err)
	}
	// Rebuild the vault tables in their v64 shape (no revision, no origin
	// columns) and keep one row; the upgrade applies both patches in order.
	mustExec(t, db, `DROP TABLE secret_vault_audit`)
	mustExec(t, db, `DROP TABLE secret_vault_grants`)
	mustExec(t, db, `DROP TABLE secret_vault_entries`)
	mustExec(t, db, sqliteSecretVaultTables)
	tid := seedTenant(t, db, "vault-upgrade")
	id := uuid.New()
	if _, err := db.Exec(`INSERT INTO secret_vault_entries (id, tenant_id, name, kind, encrypted_payload, allowed_domains)
		VALUES (?, ?, 'legacy', 'api_key', 'x', '["a.com"]')`, id, tid); err != nil {
		t.Fatal(err)
	}
	mustExec(t, db, `UPDATE schema_version SET version = 64`)
	if err := EnsureSchema(db); err != nil {
		t.Fatalf("v64 to v66: %v", err)
	}
	var v int
	if err := db.QueryRow(`SELECT version FROM schema_version LIMIT 1`).Scan(&v); err != nil || v != SchemaVersion {
		t.Fatalf("version %d err %v", v, err)
	}
	var status, via string
	var byAgent sql.NullString
	var revision int64
	if err := db.QueryRow(`SELECT status, created_via, created_by_agent, revision FROM secret_vault_entries WHERE id = ?`, id).
		Scan(&status, &via, &byAgent, &revision); err != nil {
		t.Fatal(err)
	}
	if status != store.SecretVaultStatusActive || via != store.SecretVaultCreatedViaUser || byAgent.Valid || revision != 1 {
		t.Fatalf("existing row after upgrade: %s %s %v rev=%d", status, via, byAgent, revision)
	}

	// Re-running the patch on an already-upgraded table is a no-op.
	mustExec(t, db, `UPDATE schema_version SET version = 64`)
	if err := EnsureSchema(db); err != nil {
		t.Fatalf("re-run v64 to v66: %v", err)
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
