//go:build sqlite || sqliteonly

package sqlitestore

import (
	"context"
	"database/sql"
	"errors"
	"testing"

	"github.com/google/uuid"

	"github.com/nextlevelbuilder/goclaw/internal/crypto"
	"github.com/nextlevelbuilder/goclaw/internal/store"
)

func storedPayload(t *testing.T, db *sql.DB, id uuid.UUID) string {
	t.Helper()
	var enc string
	if err := db.QueryRow(`SELECT encrypted_payload FROM secret_vault_entries WHERE id = ?`, id).Scan(&enc); err != nil {
		t.Fatal(err)
	}
	return enc
}

func setStoredPayload(t *testing.T, db *sql.DB, id uuid.UUID, enc string) {
	t.Helper()
	if _, err := db.Exec(`UPDATE secret_vault_entries SET encrypted_payload = ? WHERE id = ?`, enc, id); err != nil {
		t.Fatal(err)
	}
}

func resolveSecret(ctx context.Context, s *SQLiteSecretVaultStore, agent uuid.UUID, name string) (string, error) {
	_, p, err := s.ResolveForUse(ctx, agent, name)
	return p.Secret, err
}

func TestSecretVault_NewRowsUseBoundEnvelopeAndStartAtRevisionOne(t *testing.T) {
	db := openVaultTestDB(t)
	tid := seedTenant(t, db, "vault-aad")
	s := NewSQLiteSecretVaultStore(db, vaultTestKey)
	ctx := vaultCtx(tid)

	e := newVaultEntry("tok", store.SecretKindAPIKey)
	if err := s.Create(ctx, e, store.SecretVaultPayload{Secret: "s1"}); err != nil {
		t.Fatal(err)
	}
	if e.Revision != 1 {
		t.Fatalf("revision = %d, want 1", e.Revision)
	}
	if !crypto.IsEncryptedAAD(storedPayload(t, db, e.ID)) {
		t.Fatal("new row must use the v2 envelope")
	}
}

// A ciphertext copied onto another entry (other tenant, other name) must not
// decrypt there: the entry identity is part of the authenticated data.
func TestSecretVault_CiphertextCannotBeTransplanted(t *testing.T) {
	db := openVaultTestDB(t)
	tidA := seedTenant(t, db, "vault-a2")
	tidB := seedTenant(t, db, "vault-b2")
	agentB := seedAgent(t, db, tidB, "agent-b2")
	s := NewSQLiteSecretVaultStore(db, vaultTestKey)
	ctxA, ctxB := vaultCtx(tidA), vaultCtx(tidB)

	a := newVaultEntry("shared", store.SecretKindAPIKey)
	if err := s.Create(ctxA, a, store.SecretVaultPayload{Secret: "tenant-a-secret"}); err != nil {
		t.Fatal(err)
	}
	b := newVaultEntry("shared", store.SecretKindAPIKey)
	if err := s.Create(ctxB, b, store.SecretVaultPayload{Secret: "tenant-b-secret"}); err != nil {
		t.Fatal(err)
	}
	if err := s.SetGrants(ctxB, b.ID, []uuid.UUID{agentB}); err != nil {
		t.Fatal(err)
	}

	setStoredPayload(t, db, b.ID, storedPayload(t, db, a.ID))
	if got, err := resolveSecret(ctxB, s, agentB, "shared"); err == nil {
		t.Fatalf("transplanted ciphertext decrypted to %q", got)
	}
}

// Widening the allowed domains straight in the database invalidates the
// ciphertext: the binding includes the domain list.
func TestSecretVault_WidenedDomainsInvalidateCiphertext(t *testing.T) {
	db := openVaultTestDB(t)
	tid := seedTenant(t, db, "vault-dom")
	agent := seedAgent(t, db, tid, "agent-dom")
	s := NewSQLiteSecretVaultStore(db, vaultTestKey)
	ctx := vaultCtx(tid)

	e := newVaultEntry("tok", store.SecretKindAPIKey)
	if err := s.Create(ctx, e, store.SecretVaultPayload{Secret: "s1"}); err != nil {
		t.Fatal(err)
	}
	if err := s.SetGrants(ctx, e.ID, []uuid.UUID{agent}); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`UPDATE secret_vault_entries SET allowed_domains = ? WHERE id = ?`,
		`["api.example.com","evil.example.net"]`, e.ID); err != nil {
		t.Fatal(err)
	}
	if got, err := resolveSecret(ctx, s, agent, "tok"); err == nil {
		t.Fatalf("tampered domains still decrypted to %q", got)
	}
}

func TestSecretVault_RevisionCompareAndSet(t *testing.T) {
	db := openVaultTestDB(t)
	tid := seedTenant(t, db, "vault-cas")
	agent := seedAgent(t, db, tid, "agent-cas")
	s := NewSQLiteSecretVaultStore(db, vaultTestKey)
	ctx := vaultCtx(tid)

	e := newVaultEntry("tok", store.SecretKindAPIKey)
	if err := s.Create(ctx, e, store.SecretVaultPayload{Secret: "s1"}); err != nil {
		t.Fatal(err)
	}
	if err := s.SetGrants(ctx, e.ID, []uuid.UUID{agent}); err != nil {
		t.Fatal(err)
	}

	rev, err := s.ReplaceValue(ctx, e.ID, store.SecretVaultPayload{Secret: "s2"}, 1)
	if err != nil || rev != 2 {
		t.Fatalf("replace at rev 1 = %d, %v; want 2, nil", rev, err)
	}
	// A second writer still holding revision 1 loses, and learns the current one.
	rev, err = s.ReplaceValue(ctx, e.ID, store.SecretVaultPayload{Secret: "stale"}, 1)
	if !errors.Is(err, store.ErrSecretVaultConflict) || rev != 2 {
		t.Fatalf("stale replace = %d, %v; want conflict at 2", rev, err)
	}
	hints := "x"
	if rev, err = s.UpdateMeta(ctx, e.ID, store.SecretVaultMetaUpdate{Hints: &hints}, 1); !errors.Is(err, store.ErrSecretVaultConflict) || rev != 2 {
		t.Fatalf("stale meta = %d, %v; want conflict at 2", rev, err)
	}
	if got, err := resolveSecret(ctx, s, agent, "tok"); err != nil || got != "s2" {
		t.Fatalf("value after conflicts = %q, %v; want s2", got, err)
	}

	// Without a revision the write is unconditional.
	if rev, err = s.ReplaceValue(ctx, e.ID, store.SecretVaultPayload{Secret: "s3"}, 0); err != nil || rev != 3 {
		t.Fatalf("unconditional replace = %d, %v; want 3", rev, err)
	}
}

// Metadata changes re-seal the payload under the new binding, so the secret
// stays readable after the domains, hints or approval flag change.
func TestSecretVault_UpdateMetaResealsPayload(t *testing.T) {
	db := openVaultTestDB(t)
	tid := seedTenant(t, db, "vault-meta")
	agent := seedAgent(t, db, tid, "agent-meta")
	s := NewSQLiteSecretVaultStore(db, vaultTestKey)
	ctx := vaultCtx(tid)

	e := newVaultEntry("tok", store.SecretKindAPIKey)
	if err := s.Create(ctx, e, store.SecretVaultPayload{Secret: "s1"}); err != nil {
		t.Fatal(err)
	}
	if err := s.SetGrants(ctx, e.ID, []uuid.UUID{agent}); err != nil {
		t.Fatal(err)
	}
	before := storedPayload(t, db, e.ID)

	doms, appr := []string{"api.example.com", "*.example.org"}, true
	rev, err := s.UpdateMeta(ctx, e.ID, store.SecretVaultMetaUpdate{AllowedDomains: &doms, RequireApproval: &appr}, 1)
	if err != nil || rev != 2 {
		t.Fatalf("update meta = %d, %v; want 2", rev, err)
	}
	if storedPayload(t, db, e.ID) == before {
		t.Fatal("payload was not re-sealed")
	}
	if got, err := resolveSecret(ctx, s, agent, "tok"); err != nil || got != "s1" {
		t.Fatalf("after meta update = %q, %v; want s1", got, err)
	}
}

// A row written before the envelope change stays readable, and the next write
// upgrades it to the bound envelope.
func TestSecretVault_LegacyRowReadableThenUpgradedOnWrite(t *testing.T) {
	db := openVaultTestDB(t)
	tid := seedTenant(t, db, "vault-legacy")
	agent := seedAgent(t, db, tid, "agent-legacy")
	s := NewSQLiteSecretVaultStore(db, vaultTestKey)
	ctx := vaultCtx(tid)

	e := newVaultEntry("tok", store.SecretKindAPIKey)
	if err := s.Create(ctx, e, store.SecretVaultPayload{Secret: "placeholder"}); err != nil {
		t.Fatal(err)
	}
	if err := s.SetGrants(ctx, e.ID, []uuid.UUID{agent}); err != nil {
		t.Fatal(err)
	}
	legacy, err := crypto.Encrypt(`{"s":"legacy-value"}`, vaultTestKey)
	if err != nil {
		t.Fatal(err)
	}
	setStoredPayload(t, db, e.ID, legacy)

	if got, err := resolveSecret(ctx, s, agent, "tok"); err != nil || got != "legacy-value" {
		t.Fatalf("legacy read = %q, %v", got, err)
	}
	hints := "now bound"
	if _, err := s.UpdateMeta(ctx, e.ID, store.SecretVaultMetaUpdate{Hints: &hints}, 0); err != nil {
		t.Fatal(err)
	}
	if !crypto.IsEncryptedAAD(storedPayload(t, db, e.ID)) {
		t.Fatal("write must upgrade the row to the bound envelope")
	}
	if got, err := resolveSecret(ctx, s, agent, "tok"); err != nil || got != "legacy-value" {
		t.Fatalf("read after upgrade = %q, %v", got, err)
	}
}

func TestSecretVault_SchemaMigrationAddsRevisionColumn(t *testing.T) {
	db := openVaultTestDB(t)
	has, err := sqliteColumnExists(db, "secret_vault_entries", "revision")
	if err != nil || !has {
		t.Fatalf("revision column present = %v, %v", has, err)
	}
	patch, err := sqliteSecretVaultRevisionMigrationPatch(db)
	if err != nil || patch != "SELECT 1;" {
		t.Fatalf("patch on migrated db = %q, %v; want no-op", patch, err)
	}
}

func TestSecretVault_RewrapUpgradesLegacyRowsOnceWithoutBumpingRevision(t *testing.T) {
	db := openVaultTestDB(t)
	tid := seedTenant(t, db, "vault-rewrap")
	agent := seedAgent(t, db, tid, "agent-rewrap")
	s := NewSQLiteSecretVaultStore(db, vaultTestKey)
	ctx := vaultCtx(tid)

	legacyEntry, boundEntry, brokenEntry := newVaultEntry("legacy", store.SecretKindAPIKey), newVaultEntry("bound", store.SecretKindAPIKey), newVaultEntry("broken", store.SecretKindAPIKey)
	for _, e := range []*store.SecretVaultEntry{legacyEntry, boundEntry, brokenEntry} {
		if err := s.Create(ctx, e, store.SecretVaultPayload{Secret: "v-" + e.Name}); err != nil {
			t.Fatal(err)
		}
	}
	if err := s.SetGrants(ctx, legacyEntry.ID, []uuid.UUID{agent}); err != nil {
		t.Fatal(err)
	}
	legacy, err := crypto.Encrypt(`{"s":"v-legacy"}`, vaultTestKey)
	if err != nil {
		t.Fatal(err)
	}
	setStoredPayload(t, db, legacyEntry.ID, legacy)
	other, err := crypto.Encrypt(`{"s":"x"}`, "fedcba9876543210fedcba9876543210")
	if err != nil {
		t.Fatal(err)
	}
	setStoredPayload(t, db, brokenEntry.ID, other)

	rep, err := s.RewrapSecrets(ctx, true)
	if err != nil || rep.Total != 3 || rep.Rewrapped != 1 || rep.Current != 1 || len(rep.Unreadable) != 1 || rep.Unreadable[0] != brokenEntry.ID {
		t.Fatalf("dry run = %+v, %v", rep, err)
	}
	if storedPayload(t, db, legacyEntry.ID) != legacy {
		t.Fatal("dry run modified a row")
	}

	rep, err = s.RewrapSecrets(ctx, false)
	if err != nil || rep.Rewrapped != 1 {
		t.Fatalf("rewrap = %+v, %v", rep, err)
	}
	if !crypto.IsEncryptedAAD(storedPayload(t, db, legacyEntry.ID)) {
		t.Fatal("legacy row was not upgraded")
	}
	if storedPayload(t, db, brokenEntry.ID) != other {
		t.Fatal("an unreadable row must be left untouched")
	}
	if got, err := s.Get(ctx, legacyEntry.ID); err != nil || got.Revision != 1 {
		t.Fatalf("revision after rewrap = %v, %v; want unchanged 1", got, err)
	}
	if got, err := resolveSecret(ctx, s, agent, "legacy"); err != nil || got != "v-legacy" {
		t.Fatalf("value after rewrap = %q, %v", got, err)
	}

	// Idempotent: nothing left to upgrade.
	if rep, err = s.RewrapSecrets(ctx, false); err != nil || rep.Rewrapped != 0 || rep.Current != 2 {
		t.Fatalf("second rewrap = %+v, %v", rep, err)
	}
}
