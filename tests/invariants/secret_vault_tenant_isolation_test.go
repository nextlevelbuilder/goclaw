//go:build integration

package invariants

import (
	"errors"
	"testing"

	"github.com/google/uuid"

	"github.com/nextlevelbuilder/goclaw/internal/secretvault"
	"github.com/nextlevelbuilder/goclaw/internal/store"
	"github.com/nextlevelbuilder/goclaw/internal/store/pg"
)

const vaultInvariantKey = "0123456789abcdef0123456789abcdef"

// Tenant A must not read, list, change, grant, audit-read or use tenant B's
// secrets through the store or through the runtime that agents resolve with.
// The REST and WebSocket handlers take the tenant from the same context, so
// this is the boundary they rely on.
func TestTenantIsolation_SecretVault(t *testing.T) {
	db := testDB(t)
	tenantA, agentA, tenantB, agentB := seedTwoTenants(t, db)
	defer cleanupTenant(db, tenantA, agentA)
	defer cleanupTenant(db, tenantB, agentB)

	s := pg.NewPGSecretVaultStore(db, vaultInvariantKey)
	if s == nil {
		t.Fatal("vault store must exist when a key is set")
	}
	ctxA, ctxB := tenantCtx(tenantA), tenantCtx(tenantB)

	const valueB = "tenant-b-secret-value"
	entryB := &store.SecretVaultEntry{Name: "shared-name", Kind: store.SecretKindAPIKey, AllowedDomains: []string{"b.example.com"}, CreatedBy: "ub"}
	if err := s.Create(ctxB, entryB, store.SecretVaultPayload{Secret: valueB}); err != nil {
		t.Fatalf("create B: %v", err)
	}
	if err := s.SetGrants(ctxB, entryB.ID, []uuid.UUID{agentB}); err != nil {
		t.Fatalf("grant B: %v", err)
	}

	t.Run("list and get", func(t *testing.T) {
		list, err := s.List(ctxA)
		if err != nil {
			t.Fatal(err)
		}
		for _, e := range list {
			if e.ID == entryB.ID {
				t.Fatal("tenant A lists tenant B's entry")
			}
		}
		if _, err := s.Get(ctxA, entryB.ID); !errors.Is(err, store.ErrSecretVaultNotFound) {
			t.Fatalf("get: %v", err)
		}
		if grants, err := s.ListGrants(ctxA, entryB.ID); err == nil && len(grants) != 0 {
			t.Fatal("tenant A read tenant B's grants")
		}
	})

	t.Run("writes", func(t *testing.T) {
		if err := s.ReplaceValue(ctxA, entryB.ID, store.SecretVaultPayload{Secret: "hijack"}); !errors.Is(err, store.ErrSecretVaultNotFound) {
			t.Fatalf("replace value: %v", err)
		}
		hints := "hijacked"
		if err := s.UpdateMeta(ctxA, entryB.ID, store.SecretVaultMetaUpdate{Hints: &hints}); !errors.Is(err, store.ErrSecretVaultNotFound) {
			t.Fatalf("update meta: %v", err)
		}
		if err := s.SetGrants(ctxA, entryB.ID, []uuid.UUID{agentA}); err == nil {
			t.Fatal("tenant A granted its agent access to tenant B's secret")
		}
		if err := s.Delete(ctxA, entryB.ID); !errors.Is(err, store.ErrSecretVaultNotFound) {
			t.Fatalf("delete: %v", err)
		}
		// Tenant B's entry is untouched.
		got, err := s.Get(ctxB, entryB.ID)
		if err != nil || got.Hints == "hijacked" {
			t.Fatalf("entry B changed: %v", err)
		}
		_, p, err := s.ResolveForUse(ctxB, agentB, "shared-name")
		if err != nil || p.Secret != valueB {
			t.Fatalf("value B changed: %v", err)
		}
	})

	t.Run("audit", func(t *testing.T) {
		if err := s.AppendAudit(ctxB, &store.SecretVaultAuditEntry{EntryID: entryB.ID, EntryName: entryB.Name, AgentID: &agentB, Action: store.SecretAuditUsed, TargetHost: "b.example.com", Outcome: "ok"}); err != nil {
			t.Fatal(err)
		}
		rows, err := s.ListAudit(ctxA, entryB.ID, 10)
		if err == nil && len(rows) != 0 {
			t.Fatal("tenant A reads tenant B's audit trail")
		}
	})

	t.Run("resolver", func(t *testing.T) {
		// Same secret name in tenant A is a different, empty namespace.
		if _, _, err := s.ResolveForUse(ctxA, agentB, "shared-name"); err == nil {
			t.Fatal("tenant A resolved tenant B's secret")
		}
		if got, _ := s.ListGrantedSecrets(ctxA, agentB); len(got) != 0 {
			t.Fatal("tenant A preloads tenant B's secrets")
		}

		rtA := secretvault.NewRuntime(s, agentB, "ua")
		ctx := secretvault.WithRuntime(ctxA, rtA)
		_ = rtA.Preload(ctx)
		if _, err := rtA.Use(ctx, "shared-name", store.SecretKindAPIKey, "b.example.com"); err == nil {
			t.Fatal("runtime in tenant A used tenant B's secret")
		}
		if rtA.Redactor.Redact("x "+valueB) != "x "+valueB {
			t.Fatal("tenant A's redactor learned tenant B's value")
		}

		// Positive control: the owning tenant resolves normally.
		rtB := secretvault.NewRuntime(s, agentB, "ub")
		ctxRB := secretvault.WithRuntime(ctxB, rtB)
		if err := rtB.Preload(ctxRB); err != nil {
			t.Fatal(err)
		}
		sec, err := rtB.Use(ctxRB, "shared-name", store.SecretKindAPIKey, "b.example.com")
		if err != nil || sec == nil {
			t.Fatalf("owner could not use its secret: %v", err)
		}
	})

}
