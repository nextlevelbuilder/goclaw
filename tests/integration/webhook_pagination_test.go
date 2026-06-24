//go:build integration

package integration

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"testing"

	"github.com/google/uuid"
	"github.com/nextlevelbuilder/goclaw/internal/store"
	"github.com/nextlevelbuilder/goclaw/internal/store/pg"
)

func TestWebhookListPaginationAndCount(t *testing.T) {
	db := testDB(t)
	tenantID, _ := seedTenantAgent(t, db)

	s := pg.NewPGWebhookStore(db)
	ctx := store.WithTenantID(context.Background(), tenantID)

	t.Cleanup(func() {
		db.Exec("DELETE FROM webhooks WHERE tenant_id = $1", tenantID)
	})

	mk := func(name string, revoked bool) {
		id := uuid.New()
		h := sha256.Sum256([]byte(id.String()))
		wh := &store.WebhookData{
			ID:           id,
			TenantID:     tenantID,
			Name:         name,
			Kind:         "llm",
			SecretPrefix: "wh_test",
			SecretHash:   hex.EncodeToString(h[:]),
			Revoked:      revoked,
		}
		if err := s.Create(ctx, wh); err != nil {
			t.Fatalf("create %s: %v", name, err)
		}
	}

	for i := 0; i < 5; i++ {
		mk("active-"+uuid.NewString()[:8], false)
	}
	for i := 0; i < 2; i++ {
		mk("revoked-"+uuid.NewString()[:8], true)
	}

	// Count default excludes revoked → 5.
	if total, err := s.Count(ctx, store.WebhookListFilter{}); err != nil {
		t.Fatal(err)
	} else if total != 5 {
		t.Fatalf("Count default = %d, want 5", total)
	}

	// Count including revoked → 7.
	if total, err := s.Count(ctx, store.WebhookListFilter{IncludeRevoked: true}); err != nil {
		t.Fatal(err)
	} else if total != 7 {
		t.Fatalf("Count includeRevoked = %d, want 7", total)
	}

	// Page 1 (limit 2, offset 0) → 2 rows.
	if page1, err := s.List(ctx, store.WebhookListFilter{Limit: 2, Offset: 0}); err != nil {
		t.Fatal(err)
	} else if len(page1) != 2 {
		t.Fatalf("page1 len = %d, want 2", len(page1))
	}

	// Page 3 (limit 2, offset 4) → 1 row (5 active total, revoked excluded).
	if page3, err := s.List(ctx, store.WebhookListFilter{Limit: 2, Offset: 4}); err != nil {
		t.Fatal(err)
	} else if len(page3) != 1 {
		t.Fatalf("page3 len = %d, want 1", len(page3))
	}

	// Query filter narrows Count to the 5 active "active-" rows.
	if qc, err := s.Count(ctx, store.WebhookListFilter{Query: "active-"}); err != nil {
		t.Fatal(err)
	} else if qc != 5 {
		t.Fatalf("query Count = %d, want 5", qc)
	}
}
