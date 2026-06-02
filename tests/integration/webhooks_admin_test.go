//go:build integration

package integration

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"testing"

	"github.com/google/uuid"
	"github.com/nextlevelbuilder/goclaw/internal/store"
	"github.com/nextlevelbuilder/goclaw/internal/store/pg"
)

// seedWebhook creates a webhook in the database and returns its ID + raw secret.
func seedWebhook(t *testing.T, db *sql.DB, tenantID uuid.UUID, kind string) (webhookID uuid.UUID, rawSecret string) {
	t.Helper()

	webhookID = uuid.New()
	rawSecret = "wh_testsecret_" + webhookID.String()[:8]

	// Hash the secret as the store does.
	h := sha256.Sum256([]byte(rawSecret))
	hashHex := hex.EncodeToString(h[:])

	_, err := db.Exec(`
		INSERT INTO webhooks (id, tenant_id, kind, secret_prefix, secret_hash, status)
		VALUES ($1, $2, $3, $4, $5, 'active')
	`, webhookID, tenantID, kind, "wh_test", hashHex)
	if err != nil {
		t.Fatalf("seed webhook: %v", err)
	}

	t.Cleanup(func() {
		db.Exec("DELETE FROM webhook_calls WHERE webhook_id = $1", webhookID)
		db.Exec("DELETE FROM webhooks WHERE id = $1", webhookID)
	})

	return webhookID, rawSecret
}

// TestWebhookAdminCRUD tests basic admin CRUD: create, list, get, update, rotate, revoke.
func TestWebhookAdminCRUD(t *testing.T) {
	db := testDB(t)
	tenantID, _ := seedTenantAgent(t, db)

	// Initialize store.
	s := pg.NewPGWebhookStore(db)
	ctx := context.Background()
	ctx = store.WithTenantID(ctx, tenantID)

	// Create webhook.
	wh := &store.WebhookData{
		ID:              uuid.New(),
		TenantID:        tenantID,
		Kind:            "llm",
		SecretPrefix:    "wh_test",
		RateLimitPerMin: 60,
	}
	rawSecret := "wh_testsecret_initial"
	h := sha256.Sum256([]byte(rawSecret))
	wh.SecretHash = hex.EncodeToString(h[:])

	err := s.Create(ctx, wh)
	if err != nil {
		t.Fatalf("Create failed: %v", err)
	}

	// Get webhook.
	retrieved, err := s.GetByID(ctx, wh.ID)
	if err != nil {
		t.Fatalf("GetByID failed: %v", err)
	}
	if retrieved.ID != wh.ID {
		t.Errorf("retrieved webhook ID mismatch: got %v, want %v", retrieved.ID, wh.ID)
	}

	// List webhooks.
	list, err := s.List(ctx, store.WebhookListFilter{})
	if err != nil {
		t.Fatalf("List failed: %v", err)
	}
	if len(list) < 1 {
		t.Errorf("List returned no webhooks")
	}

	// Update webhook.
	err = s.Update(ctx, wh.ID, map[string]any{
		"rate_limit_per_min": 120,
	})
	if err != nil {
		t.Fatalf("Update failed: %v", err)
	}

	// Verify update.
	updated, err := s.GetByID(ctx, wh.ID)
	if err != nil {
		t.Fatalf("GetByID after update failed: %v", err)
	}
	if updated.RateLimitPerMin != 120 {
		t.Errorf("rate limit not updated: got %d, want 120", updated.RateLimitPerMin)
	}

	// Rotate secret.
	newRawSecret := "wh_newsecret_rotated"
	newH := sha256.Sum256([]byte(newRawSecret))
	newHashHex := hex.EncodeToString(newH[:])
	err = s.RotateSecret(ctx, wh.ID, newHashHex, "wh_newrot", "encrypted_placeholder")
	if err != nil {
		t.Fatalf("RotateSecret failed: %v", err)
	}

	// Verify old secret hash is now secret_hash_prev.
	rotated, err := s.GetByID(ctx, wh.ID)
	if err != nil {
		t.Fatalf("GetByID after rotate failed: %v", err)
	}
	if rotated.SecretHash != newHashHex {
		t.Errorf("secret_hash not updated: got %s, want %s", rotated.SecretHash, newHashHex)
	}

	// Revoke webhook.
	err = s.Revoke(ctx, wh.ID)
	if err != nil {
		t.Fatalf("Revoke failed: %v", err)
	}

	// Verify revoked.
	revoked, err := s.GetByID(ctx, wh.ID)
	if err != nil {
		t.Fatalf("GetByID after revoke failed: %v", err)
	}
	if !revoked.Revoked {
		t.Errorf("webhook not revoked: %+v", revoked)
	}
}

// TestWebhookAdminTenantIsolation tests that webhooks from tenant A cannot be accessed by tenant B.
func TestWebhookAdminTenantIsolation(t *testing.T) {
	db := testDB(t)
	tenantA, _ := seedTenantAgent(t, db)
	tenantB, _ := seedTenantAgent(t, db)

	sA := pg.NewPGWebhookStore(db)
	sB := pg.NewPGWebhookStore(db)

	ctxA := context.Background()
	ctxA = store.WithTenantID(ctxA, tenantA)

	ctxB := context.Background()
	ctxB = store.WithTenantID(ctxB, tenantB)

	// Tenant A creates a webhook.
	whA := &store.WebhookData{
		ID:       uuid.New(),
		TenantID: tenantA,
		Kind:     "llm",
	}
	h := sha256.Sum256([]byte("secret_a"))
	whA.SecretHash = hex.EncodeToString(h[:])

	err := sA.Create(ctxA, whA)
	if err != nil {
		t.Fatalf("Tenant A create failed: %v", err)
	}

	// Tenant B tries to access tenant A's webhook directly from DB.
	// GetByID should filter by tenant_id in the WHERE clause.
	ctxBToGetA := store.WithTenantID(context.Background(), tenantB)
	_, err = sB.GetByID(ctxBToGetA, whA.ID)
	if err != sql.ErrNoRows {
		t.Errorf("Tenant B should not access Tenant A's webhook; got err=%v", err)
	}

	// Tenant B lists webhooks — should only see their own.
	listB, err := sB.List(ctxB, store.WebhookListFilter{})
	if err != nil {
		t.Fatalf("Tenant B list failed: %v", err)
	}
	for _, w := range listB {
		if w.TenantID != tenantB {
			t.Errorf("Tenant B listed webhook with wrong tenant_id: %v", w.TenantID)
		}
	}
}
