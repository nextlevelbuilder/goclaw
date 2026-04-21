//go:build sqlite || sqliteonly

package sqlitestore

import (
	"context"
	"database/sql"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/nextlevelbuilder/goclaw/internal/store"
)

// openTestDB opens an in-memory SQLite DB with the full schema applied.
func openTestWebhookDB(t *testing.T) *sql.DB {
	t.Helper()
	db, err := OpenDB(":memory:")
	if err != nil {
		t.Fatalf("openDB: %v", err)
	}
	if err := EnsureSchema(db); err != nil {
		t.Fatalf("EnsureSchema: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	return db
}

func testTenantCtx(tenantID uuid.UUID) context.Context {
	return store.WithTenantID(context.Background(), tenantID)
}

// TestWebhookJSONRoundTrip verifies scopes + ip_allowlist survive a write→read cycle
// through the SQLite JSON TEXT encoding.
func TestWebhookJSONRoundTrip(t *testing.T) {
	db := openTestWebhookDB(t)
	ws := NewSQLiteWebhookStore(db)

	tenantID := uuid.New()
	ctx := testTenantCtx(tenantID)

	w := &store.WebhookData{
		ID:              uuid.New(),
		TenantID:        tenantID,
		Name:            "test-webhook",
		Kind:            "llm",
		SecretHash:      "abc123",
		Scopes:          []string{"agent.run", "agent.read"},
		IPAllowlist:     []string{"10.0.0.1", "192.168.1.0/24"},
		RateLimitPerMin: 60,
		CreatedAt:       time.Now().UTC().Truncate(time.Second),
		UpdatedAt:       time.Now().UTC().Truncate(time.Second),
	}

	if err := ws.Create(ctx, w); err != nil {
		t.Fatalf("Create: %v", err)
	}

	got, err := ws.GetByID(ctx, w.ID)
	if err != nil {
		t.Fatalf("GetByID: %v", err)
	}

	if len(got.Scopes) != 2 || got.Scopes[0] != "agent.run" || got.Scopes[1] != "agent.read" {
		t.Errorf("scopes round-trip failed: got %v", got.Scopes)
	}
	if len(got.IPAllowlist) != 2 || got.IPAllowlist[0] != "10.0.0.1" {
		t.Errorf("ip_allowlist round-trip failed: got %v", got.IPAllowlist)
	}
}

// TestWebhookGetByIDWrongTenant verifies tenant isolation: Get with wrong tenant returns ErrNoRows.
func TestWebhookGetByIDWrongTenant(t *testing.T) {
	db := openTestWebhookDB(t)
	ws := NewSQLiteWebhookStore(db)

	ownerTenant := uuid.New()
	otherTenant := uuid.New()

	w := &store.WebhookData{
		ID:              uuid.New(),
		TenantID:        ownerTenant,
		Name:            "secret-webhook",
		Kind:            "llm",
		SecretHash:      "hash-xyz",
		Scopes:          []string{},
		IPAllowlist:     []string{},
		RateLimitPerMin: 30,
		CreatedAt:       time.Now().UTC(),
		UpdatedAt:       time.Now().UTC(),
	}
	if err := ws.Create(testTenantCtx(ownerTenant), w); err != nil {
		t.Fatalf("Create: %v", err)
	}

	// Fetch with wrong tenant — must return ErrNoRows, not the row.
	_, err := ws.GetByID(testTenantCtx(otherTenant), w.ID)
	if err != sql.ErrNoRows {
		t.Errorf("expected sql.ErrNoRows for cross-tenant get, got: %v", err)
	}
}

// TestWebhookCallClaimNextSkipsRunningAndDone verifies ClaimNext only returns queued rows.
func TestWebhookCallClaimNextSkipsRunningAndDone(t *testing.T) {
	db := openTestWebhookDB(t)
	ws := NewSQLiteWebhookStore(db)
	cs := NewSQLiteWebhookCallStore(db)

	tenantID := uuid.New()
	ctx := testTenantCtx(tenantID)

	// Create a parent webhook first (FK constraint).
	wh := &store.WebhookData{
		ID:              uuid.New(),
		TenantID:        tenantID,
		Name:            "wh",
		Kind:            "llm",
		SecretHash:      "h1",
		Scopes:          []string{},
		IPAllowlist:     []string{},
		RateLimitPerMin: 60,
		CreatedAt:       time.Now().UTC(),
		UpdatedAt:       time.Now().UTC(),
	}
	if err := ws.Create(ctx, wh); err != nil {
		t.Fatalf("Create webhook: %v", err)
	}

	now := time.Now().UTC()

	// Insert one running call and one done call — ClaimNext must skip both.
	for _, status := range []string{"running", "done"} {
		c := &store.WebhookCallData{
			ID:         uuid.New(),
			TenantID:   tenantID,
			WebhookID:  wh.ID,
			DeliveryID: uuid.New(),
			Mode:       "async",
			Status:     status,
			Attempts:   1,
			CreatedAt:  now,
		}
		if err := cs.Create(ctx, c); err != nil {
			// "done" row has no idempotency conflict; bypass status check — insert directly.
			_, dbErr := db.ExecContext(ctx,
				`INSERT INTO webhook_calls (id,tenant_id,webhook_id,delivery_id,mode,status,attempts,created_at)
				 VALUES (?,?,?,?,?,?,?,?)`,
				c.ID, c.TenantID, c.WebhookID, c.DeliveryID, c.Mode, status, c.Attempts, c.CreatedAt,
			)
			if dbErr != nil {
				t.Fatalf("insert %s call: %v", status, dbErr)
			}
		}
	}

	// Queue is empty of queued rows — must return ErrNoRows.
	_, err := cs.ClaimNext(ctx, tenantID, now)
	if err != sql.ErrNoRows {
		t.Errorf("expected ErrNoRows when no queued rows, got: %v", err)
	}

	// Insert a queued call due now.
	queued := &store.WebhookCallData{
		ID:         uuid.New(),
		TenantID:   tenantID,
		WebhookID:  wh.ID,
		DeliveryID: uuid.New(),
		Mode:       "async",
		Status:     "queued",
		Attempts:   0,
		CreatedAt:  now,
	}
	if err := cs.Create(ctx, queued); err != nil {
		t.Fatalf("Create queued call: %v", err)
	}

	claimed, err := cs.ClaimNext(ctx, tenantID, now)
	if err != nil {
		t.Fatalf("ClaimNext: %v", err)
	}
	if claimed.ID != queued.ID {
		t.Errorf("claimed wrong call: got %v want %v", claimed.ID, queued.ID)
	}
	if claimed.Status != "running" {
		t.Errorf("expected status=running, got %q", claimed.Status)
	}
	// Attempts must NOT be incremented by ClaimNext.
	if claimed.Attempts != 0 {
		t.Errorf("ClaimNext must not increment attempts: got %d", claimed.Attempts)
	}
	if claimed.StartedAt == nil {
		t.Error("ClaimNext must set started_at")
	}
}

// TestWebhookCallIdempotencyConflict verifies duplicate (webhook_id, idempotency_key)
// returns ErrIdempotencyConflict.
func TestWebhookCallIdempotencyConflict(t *testing.T) {
	db := openTestWebhookDB(t)
	ws := NewSQLiteWebhookStore(db)
	cs := NewSQLiteWebhookCallStore(db)

	tenantID := uuid.New()
	ctx := testTenantCtx(tenantID)

	wh := &store.WebhookData{
		ID: uuid.New(), TenantID: tenantID, Name: "wh2", Kind: "llm",
		SecretHash: "h2", Scopes: []string{}, IPAllowlist: []string{},
		RateLimitPerMin: 60, CreatedAt: time.Now().UTC(), UpdatedAt: time.Now().UTC(),
	}
	if err := ws.Create(ctx, wh); err != nil {
		t.Fatalf("Create webhook: %v", err)
	}

	key := "idem-key-1"
	c1 := &store.WebhookCallData{
		ID: uuid.New(), TenantID: tenantID, WebhookID: wh.ID,
		DeliveryID: uuid.New(), IdempotencyKey: &key,
		Mode: "async", Status: "queued", CreatedAt: time.Now().UTC(),
	}
	if err := cs.Create(ctx, c1); err != nil {
		t.Fatalf("first Create: %v", err)
	}

	c2 := &store.WebhookCallData{
		ID: uuid.New(), TenantID: tenantID, WebhookID: wh.ID,
		DeliveryID: uuid.New(), IdempotencyKey: &key,
		Mode: "async", Status: "queued", CreatedAt: time.Now().UTC(),
	}
	err := cs.Create(ctx, c2)
	if err == nil {
		t.Fatal("expected ErrIdempotencyConflict, got nil")
	}
	if err != store.ErrIdempotencyConflict {
		t.Errorf("expected ErrIdempotencyConflict, got: %v", err)
	}
}
