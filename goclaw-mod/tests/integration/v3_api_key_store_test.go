//go:build integration

package integration

import (
	"database/sql"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/nextlevelbuilder/goclaw/internal/store"
	"github.com/nextlevelbuilder/goclaw/internal/store/pg"
)

func TestStoreAPIKey_CreateAndGetByHash(t *testing.T) {
	db := testDB(t)
	tenantID, _ := seedTenantAgent(t, db)
	ctx := tenantCtx(tenantID)
	s := pg.NewPGAPIKeyStore(db)

	_, keyHash := seedAPIKey(t, db, tenantID)

	// GetByHash with correct hash — should find it.
	got, err := s.GetByHash(ctx, keyHash)
	if err != nil {
		t.Fatalf("GetByHash: %v", err)
	}
	if got == nil {
		t.Fatal("expected key, got nil")
	}
	if got.KeyHash != keyHash {
		t.Errorf("KeyHash mismatch: got %q, want %q", got.KeyHash, keyHash)
	}

	// GetByHash with wrong hash — should return not found.
	missing, err := s.GetByHash(ctx, "nonexistent-hash")
	if !errors.Is(err, sql.ErrNoRows) {
		t.Errorf("expected sql.ErrNoRows for wrong hash, got err=%v result=%v", err, missing)
	}
}

func TestStoreAPIKey_ListAndRevoke(t *testing.T) {
	db := testDB(t)
	tenantID, _ := seedTenantAgent(t, db)
	ctx := tenantCtx(tenantID)
	s := pg.NewPGAPIKeyStore(db)

	// Create two keys via the store (not seedAPIKey) so we control tenant context.
	now := time.Now()
	key1 := &store.APIKeyData{
		ID:        uuid.New(),
		TenantID:  tenantID,
		Name:      "key-one",
		Prefix:    "gclw_k1",
		KeyHash:   "hash-one-" + uuid.New().String(),
		Scopes:    []string{"operator.read"},
		CreatedAt: now,
		UpdatedAt: now,
	}
	key2 := &store.APIKeyData{
		ID:        uuid.New(),
		TenantID:  tenantID,
		Name:      "key-two",
		Prefix:    "gclw_k2",
		KeyHash:   "hash-two-" + uuid.New().String(),
		Scopes:    []string{"operator.admin"},
		CreatedAt: now,
		UpdatedAt: now,
	}
	if err := s.Create(ctx, key1); err != nil {
		t.Fatalf("Create key1: %v", err)
	}
	if err := s.Create(ctx, key2); err != nil {
		t.Fatalf("Create key2: %v", err)
	}
	t.Cleanup(func() {
		db.Exec("DELETE FROM api_keys WHERE id = $1 OR id = $2", key1.ID, key2.ID)
	})

	// List — should see at least 2 (may include seed keys from other tests, but tenant-scoped).
	keys, err := s.List(ctx, "")
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	count := 0
	for _, k := range keys {
		if k.ID == key1.ID || k.ID == key2.ID {
			count++
		}
	}
	if count != 2 {
		t.Errorf("expected both created keys in list, found %d out of %d total", count, len(keys))
	}

	// Revoke key1.
	if err := s.Revoke(ctx, key1.ID, ""); err != nil {
		t.Fatalf("Revoke: %v", err)
	}

	// List again — both keys still present but key1 should be revoked.
	keys2, err := s.List(ctx, "")
	if err != nil {
		t.Fatalf("List after revoke: %v", err)
	}
	revokedFound := false
	for _, k := range keys2 {
		if k.ID == key1.ID {
			if !k.Revoked {
				t.Error("expected key1 to be revoked")
			}
			revokedFound = true
		}
	}
	if !revokedFound {
		t.Error("key1 not found in list after revoke")
	}
}

func TestStoreAPIKey_TouchLastUsed(t *testing.T) {
	db := testDB(t)
	tenantID, _ := seedTenantAgent(t, db)
	ctx := tenantCtx(tenantID)
	s := pg.NewPGAPIKeyStore(db)

	keyID, _ := seedAPIKey(t, db, tenantID)

	// Confirm last_used_at is NULL before touch.
	var lastUsed *time.Time
	db.QueryRow("SELECT last_used_at FROM api_keys WHERE id = $1", keyID).Scan(&lastUsed)
	if lastUsed != nil {
		t.Errorf("expected last_used_at to be NULL before touch, got %v", lastUsed)
	}

	// Touch.
	if err := s.TouchLastUsed(ctx, keyID); err != nil {
		t.Fatalf("TouchLastUsed: %v", err)
	}

	// Verify last_used_at updated in DB.
	var lastUsed2 *time.Time
	db.QueryRow("SELECT last_used_at FROM api_keys WHERE id = $1", keyID).Scan(&lastUsed2)
	if lastUsed2 == nil {
		t.Error("expected last_used_at to be set after TouchLastUsed")
	}
}
