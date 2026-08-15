//go:build integration

package invariants

import (
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"testing"

	"github.com/google/uuid"

	"github.com/nextlevelbuilder/goclaw/internal/store"
	"github.com/nextlevelbuilder/goclaw/internal/store/pg"
)

// webhookListFilter returns a zero-value filter (list all webhooks for the tenant in context).
func webhookListFilter() store.WebhookListFilter {
	return store.WebhookListFilter{}
}

// seedWebhook creates a webhook for a tenant.
func seedWebhook(t *testing.T, db *sql.DB, tenantID uuid.UUID, kind string) uuid.UUID {
	t.Helper()

	webhookID := uuid.New()
	rawSecret := "wh_secret_" + webhookID.String()[:8]
	h := sha256.Sum256([]byte(rawSecret))
	hashHex := hex.EncodeToString(h[:])

	_, err := db.Exec(`
		INSERT INTO webhooks (id, tenant_id, name, kind, secret_prefix, secret_hash)
		VALUES ($1, $2, $3, $4, $5, $6)
	`, webhookID, tenantID, "test-webhook-"+webhookID.String()[:8], kind, "wh_test", hashHex)
	if err != nil {
		t.Fatalf("seed webhook: %v", err)
	}

	return webhookID
}

// P0: TestWebhookTenantIsolationListGet ensures no tenant can list/get another tenant's webhook.
func TestWebhookTenantIsolationListGet(t *testing.T) {
	db := testDB(t)

	// Seed 2 independent tenants with their webhooks.
	tenantA, _ := seedTenantAgent(t, db)
	tenantB, _ := seedTenantAgent(t, db)

	webhookAID := seedWebhook(t, db, tenantA, "llm")
	webhookBID := seedWebhook(t, db, tenantB, "message")

	store := pg.NewPGWebhookStore(db)

	ctxA := tenantCtx(tenantA)
	ctxB := tenantCtx(tenantB)

	// Tenant A lists webhooks — should only see their own.
	listA, err := store.List(ctxA, webhookListFilter())
	if err != nil {
		t.Fatalf("Tenant A list failed: %v", err)
	}

	for _, w := range listA {
		if w.TenantID != tenantA {
			t.Errorf("P0 VIOLATION: Tenant A listed webhook with tenant_id=%v (not %v)", w.TenantID, tenantA)
		}
		if w.ID == webhookBID {
			t.Errorf("P0 VIOLATION: Tenant A listed Tenant B's webhook")
		}
	}

	// Tenant B lists webhooks — should only see their own.
	listB, err := store.List(ctxB, webhookListFilter())
	if err != nil {
		t.Fatalf("Tenant B list failed: %v", err)
	}

	for _, w := range listB {
		if w.TenantID != tenantB {
			t.Errorf("P0 VIOLATION: Tenant B listed webhook with tenant_id=%v (not %v)", w.TenantID, tenantB)
		}
		if w.ID == webhookAID {
			t.Errorf("P0 VIOLATION: Tenant B listed Tenant A's webhook")
		}
	}

	// Tenant B tries to GET Tenant A's webhook.
	_, err = store.GetByID(ctxB, webhookAID)
	if err != sql.ErrNoRows {
		t.Errorf("P0 VIOLATION: Tenant B was able to GetByID Tenant A's webhook (expected ErrNoRows, got %v)", err)
	}
}

// P0: TestWebhookTenantIsolationRotateRevoke ensures no tenant can rotate/revoke another's webhook.
func TestWebhookTenantIsolationRotateRevoke(t *testing.T) {
	db := testDB(t)

	tenantA, _ := seedTenantAgent(t, db)
	tenantB, _ := seedTenantAgent(t, db)

	webhookAID := seedWebhook(t, db, tenantA, "llm")

	whs := pg.NewPGWebhookStore(db)

	ctxA := tenantCtx(tenantA)
	ctxB := tenantCtx(tenantB)

	// Get the original webhook.
	origWH, err := whs.GetByID(ctxA, webhookAID)
	if err != nil {
		t.Fatalf("Tenant A get their webhook: %v", err)
	}
	origHash := origWH.SecretHash

	// Tenant B tries to rotate Tenant A's webhook secret.
	newHash := "newsecret_hash_" + uuid.New().String()[:8]
	newPrefix := "wh_newprefix"
	newEncrypted := "encrypted_secret_b64_payload"
	err = whs.RotateSecret(ctxB, webhookAID, newHash, newPrefix, newEncrypted)
	if err == nil {
		// This is a P0 violation — the rotate should have failed (ErrNoRows or equivalent).
		t.Errorf("P0 VIOLATION: Tenant B was able to rotate Tenant A's webhook secret")

		// Verify it actually changed (worse violation).
		updated, _ := whs.GetByID(ctxA, webhookAID)
		if updated.SecretHash != origHash {
			t.Errorf("P0 VIOLATION: Secret hash actually changed when Tenant B called RotateSecret")
		}
	}

	// Tenant B tries to revoke Tenant A's webhook.
	err = whs.Revoke(ctxB, webhookAID)
	if err == nil {
		// Check if it actually revoked.
		updated, _ := whs.GetByID(ctxA, webhookAID)
		if updated.Revoked {
			t.Errorf("P0 VIOLATION: Tenant B was able to revoke Tenant A's webhook")
		}
	}
}

// P0: TestWebhookTenantIsolationUpdate ensures no tenant can update another's webhook.
func TestWebhookTenantIsolationUpdate(t *testing.T) {
	db := testDB(t)

	tenantA, _ := seedTenantAgent(t, db)
	tenantB, _ := seedTenantAgent(t, db)

	webhookAID := seedWebhook(t, db, tenantA, "llm")

	whs := pg.NewPGWebhookStore(db)

	ctxA := tenantCtx(tenantA)
	ctxB := tenantCtx(tenantB)

	// Get original rate limit.
	origWH, err := whs.GetByID(ctxA, webhookAID)
	if err != nil {
		t.Fatalf("get original webhook: %v", err)
	}
	origRPM := origWH.RateLimitPerMin

	// Tenant B tries to update Tenant A's rate limit.
	err = whs.Update(ctxB, webhookAID, map[string]any{
		"rate_limit_per_min": 999,
	})
	if err == nil {
		// Check if it actually updated.
		updated, _ := whs.GetByID(ctxA, webhookAID)
		if updated.RateLimitPerMin != origRPM {
			t.Errorf("P0 VIOLATION: Tenant B was able to update Tenant A's rate_limit_per_min from %d to %d",
				origRPM, updated.RateLimitPerMin)
		}
	}
}

// P0: TestWebhookTenantIsolationGetByHash ensures GetByHash never returns cross-tenant webhook.
func TestWebhookTenantIsolationGetByHash(t *testing.T) {
	db := testDB(t)

	tenantA, _ := seedTenantAgent(t, db)
	tenantB, _ := seedTenantAgent(t, db)

	// Create webhooks with known secrets.
	webhookAID := uuid.New()
	secretA := "wh_secret_a_" + webhookAID.String()[:8]
	hA := sha256.Sum256([]byte(secretA))
	hashA := hex.EncodeToString(hA[:])

	_, err := db.Exec(`
		INSERT INTO webhooks (id, tenant_id, name, kind, secret_prefix, secret_hash)
		VALUES ($1, $2, $3, 'llm', 'wh_test', $4)
	`, webhookAID, tenantA, "test-webhook-"+webhookAID.String()[:8], hashA)
	if err != nil {
		t.Fatalf("seed webhook A: %v", err)
	}

	whs := pg.NewPGWebhookStore(db)

	ctxA := tenantCtx(tenantA)
	ctxB := tenantCtx(tenantB)

	// Tenant A gets webhook by hash — should succeed.
	whA, err := whs.GetByHash(ctxA, hashA)
	if err != nil {
		t.Fatalf("Tenant A GetByHash failed: %v", err)
	}
	if whA.TenantID != tenantA {
		t.Errorf("Tenant A retrieved webhook with wrong tenant_id: %v", whA.TenantID)
	}

	// Tenant B gets same hash — should fail (tenant_id check in query).
	whB, err := whs.GetByHash(ctxB, hashA)
	if err != sql.ErrNoRows {
		t.Errorf("P0 VIOLATION: Tenant B GetByHash succeeded (expected ErrNoRows, got %v, webhook=%v)", err, whB)
	}
}
