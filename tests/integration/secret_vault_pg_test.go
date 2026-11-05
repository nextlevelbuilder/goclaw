//go:build integration

package integration

import (
	"errors"
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
