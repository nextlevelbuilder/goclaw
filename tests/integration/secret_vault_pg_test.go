//go:build integration

package integration

import (
	"errors"
	"fmt"
	"testing"

	"github.com/google/uuid"

	"github.com/nextlevelbuilder/goclaw/internal/store"
	"github.com/nextlevelbuilder/goclaw/internal/store/pg"
)

func TestSecretVaultPG_EncryptedGrantedAndTenantScoped(t *testing.T) {
	t.Parallel()

	db := testDB(t)
	tenantA, agentA := seedTenantAgent(t, db)
	tenantB, agentB := seedTenantAgent(t, db)
	s := pg.NewPGSecretVaultStore(db, testEncryptionKey)
	if s == nil {
		t.Fatal("vault store must exist when a key is set")
	}
	ctxA, ctxB := tenantCtx(tenantA), tenantCtx(tenantB)

	e := &store.SecretVaultEntry{Name: "gh-token", Kind: store.SecretKindAPIKey, AllowedDomains: []string{"api.github.com"}, CreatedBy: "user1"}
	if err := s.Create(ctxA, e, store.SecretVaultPayload{Secret: "tok-plain-123"}); err != nil {
		t.Fatalf("create: %v", err)
	}
	if err := s.Create(ctxA, &store.SecretVaultEntry{Name: "gh-token", Kind: store.SecretKindAPIKey, AllowedDomains: []string{"a.com"}, CreatedBy: "user1"}, store.SecretVaultPayload{Secret: "x"}); !errors.Is(err, store.ErrSecretVaultNameTaken) {
		t.Fatalf("duplicate name: %v", err)
	}

	var raw string
	if err := db.QueryRow(`SELECT encrypted_payload FROM secret_vault_entries WHERE id = $1`, e.ID).Scan(&raw); err != nil {
		t.Fatal(err)
	}
	if raw == "" || raw == "tok-plain-123" {
		t.Fatal("payload not encrypted at rest")
	}

	if _, _, err := s.ResolveForUse(ctxA, agentA, "gh-token"); !errors.Is(err, store.ErrSecretVaultNotGranted) {
		t.Fatalf("ungranted: %v", err)
	}
	if err := s.SetGrants(ctxA, e.ID, []uuid.UUID{agentB}); !errors.Is(err, store.ErrSecretVaultAgentInvalid) {
		t.Fatalf("foreign-tenant agent grant: %v", err)
	}
	if err := s.SetGrants(ctxA, e.ID, []uuid.UUID{agentA}); err != nil {
		t.Fatal(err)
	}
	_, p, err := s.ResolveForUse(ctxA, agentA, "gh-token")
	if err != nil || p.Secret != "tok-plain-123" {
		t.Fatalf("resolve: %v", err)
	}

	granted, err := s.ListGrantedSecrets(ctxA, agentA)
	if err != nil || len(granted) != 1 || granted[0].Entry.Name != "gh-token" || granted[0].Payload.Secret != "tok-plain-123" {
		t.Fatalf("list granted secrets: %v", err)
	}
	if other, _ := s.ListGrantedSecrets(ctxB, agentA); len(other) != 0 {
		t.Fatal("cross-tenant ListGrantedSecrets must be empty")
	}

	if _, err := s.Get(ctxB, e.ID); !errors.Is(err, store.ErrSecretVaultNotFound) {
		t.Fatalf("cross-tenant get: %v", err)
	}
	if _, _, err := s.ResolveForUse(ctxB, agentA, "gh-token"); !errors.Is(err, store.ErrSecretVaultNotFound) && !errors.Is(err, store.ErrSecretVaultNotGranted) {
		t.Fatalf("cross-tenant resolve: %v", err)
	}
	if err := s.Delete(ctxB, e.ID); !errors.Is(err, store.ErrSecretVaultNotFound) {
		t.Fatalf("cross-tenant delete: %v", err)
	}

	if err := s.AppendAudit(ctxA, &store.SecretVaultAuditEntry{EntryID: e.ID, EntryName: e.Name, AgentID: &agentA, Action: store.SecretAuditUsed, TargetHost: "api.github.com", Outcome: "ok"}); err != nil {
		t.Fatal(err)
	}
	if err := s.Delete(ctxA, e.ID); err != nil {
		t.Fatal(err)
	}
	if a, err := s.ListAudit(ctxA, e.ID, 10); err != nil || len(a) != 1 {
		t.Fatalf("audit should survive delete: %v", err)
	}
}

func TestSecretVaultPG_AgentCreatedPendingUntilApproved(t *testing.T) {
	t.Parallel()

	db := testDB(t)
	tenantA, agentA := seedTenantAgent(t, db)
	tenantB, agentB := seedTenantAgent(t, db)
	s := pg.NewPGSecretVaultStore(db, testEncryptionKey)
	ctxA, ctxB := tenantCtx(tenantA), tenantCtx(tenantB)
	agentEntry := func(name string, agent uuid.UUID) *store.SecretVaultEntry {
		return &store.SecretVaultEntry{Name: name, Kind: store.SecretKindAPIKey, AllowedDomains: []string{"proposed.example.com"}, CreatedBy: "run-user", CreatedByAgent: &agent}
	}

	user := &store.SecretVaultEntry{Name: "user-made", Kind: store.SecretKindAPIKey, AllowedDomains: []string{"a.com"}, CreatedBy: "user1"}
	if err := s.Create(ctxA, user, store.SecretVaultPayload{Secret: "user-value"}); err != nil {
		t.Fatal(err)
	}
	if got, _ := s.Get(ctxA, user.ID); got.Status != store.SecretVaultStatusActive || got.CreatedVia != store.SecretVaultCreatedViaUser || got.CreatedByAgent != nil {
		t.Fatalf("user entry origin: %+v", got)
	}

	e := agentEntry("captured-jwt", agentA)
	if err := s.Create(ctxA, e, store.SecretVaultPayload{Secret: "jwt-value"}); err != nil {
		t.Fatal(err)
	}
	got, err := s.Get(ctxA, e.ID)
	if err != nil || got.Status != store.SecretVaultStatusPending || got.CreatedVia != store.SecretVaultCreatedViaAgent || *got.CreatedByAgent != agentA {
		t.Fatalf("agent entry: %+v %v", got, err)
	}
	if err := s.SetGrants(ctxA, e.ID, []uuid.UUID{agentA}); err != nil {
		t.Fatal(err)
	}
	if _, _, err := s.ResolveForUse(ctxA, agentA, "captured-jwt"); !errors.Is(err, store.ErrSecretVaultNotGranted) {
		t.Fatalf("pending must resolve as not granted: %v", err)
	}
	if m, _ := s.ListGrantedMeta(ctxA, agentA); len(m) != 0 {
		t.Fatal("pending entry listed in granted meta")
	}
	if g, _ := s.ListGrantedSecrets(ctxA, agentA); len(g) != 0 {
		t.Fatal("pending entry listed in granted secrets")
	}
	audit, err := s.ListAudit(ctxA, e.ID, 10)
	if err != nil || len(audit) != 1 || audit[0].Outcome != store.SecretAuditOutcomeAgentPending || audit[0].AgentID == nil || *audit[0].AgentID != agentA {
		t.Fatalf("created audit: %+v %v", audit, err)
	}
	if err := s.Create(ctxA, agentEntry("user-made", agentA), store.SecretVaultPayload{Secret: "overwrite"}); !errors.Is(err, store.ErrSecretVaultNameTaken) {
		t.Fatalf("agent name collision: %v", err)
	}
	if err := s.Create(ctxA, agentEntry("foreign", agentB), store.SecretVaultPayload{Secret: "v"}); !errors.Is(err, store.ErrSecretVaultAgentInvalid) {
		t.Fatalf("foreign agent: %v", err)
	}

	if n, err := s.CountPendingByAgent(ctxA, agentA); err != nil || n != 1 {
		t.Fatalf("count: %d %v", n, err)
	}
	if n, _ := s.CountPendingByAgent(ctxB, agentA); n != 0 {
		t.Fatalf("cross-tenant count: %d", n)
	}
	if err := s.Approve(ctxB, e.ID, []string{"b.com"}); !errors.Is(err, store.ErrSecretVaultNotFound) {
		t.Fatalf("cross-tenant approve: %v", err)
	}
	if err := s.Approve(ctxA, e.ID, nil); !errors.Is(err, store.ErrSecretVaultInvalid) {
		t.Fatalf("approve without domains: %v", err)
	}
	if err := s.Approve(store.WithUserID(ctxA, "admin-1"), e.ID, []string{"final.example.com"}); err != nil {
		t.Fatal(err)
	}
	if err := s.Approve(ctxA, e.ID, []string{"final.example.com"}); !errors.Is(err, store.ErrSecretVaultNotPending) {
		t.Fatalf("second approve: %v", err)
	}
	resolved, p, err := s.ResolveForUse(ctxA, agentA, "captured-jwt")
	if err != nil || p.Secret != "jwt-value" || resolved.AllowedDomains[0] != "final.example.com" {
		t.Fatalf("resolve after approve: %v", err)
	}
	if n, _ := s.CountPendingByAgent(ctxA, agentA); n != 0 {
		t.Fatalf("count after approve: %d", n)
	}
	audit, _ = s.ListAudit(ctxA, e.ID, 10)
	if len(audit) != 2 || audit[0].Action != store.SecretAuditApproved || audit[0].UserID != "admin-1" {
		t.Fatalf("approved audit: %+v", audit)
	}

	for i := range store.SecretVaultMaxPendingPerAgent {
		if err := s.Create(ctxA, agentEntry(fmt.Sprintf("cap-%02d", i), agentA), store.SecretVaultPayload{Secret: "v"}); err != nil {
			t.Fatalf("create %d: %v", i, err)
		}
	}
	if err := s.Create(ctxA, agentEntry("cap-over", agentA), store.SecretVaultPayload{Secret: "v"}); !errors.Is(err, store.ErrSecretVaultPendingLimit) {
		t.Fatalf("pending cap: %v", err)
	}
}
