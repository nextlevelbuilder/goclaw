//go:build sqlite || sqliteonly

package sqlitestore

import (
	"context"
	"testing"

	"github.com/google/uuid"

	"github.com/nextlevelbuilder/goclaw/internal/store"
)

func TestListDeliveryTargetsUsesDiscordDisplayTitleMetadata(t *testing.T) {
	db := openTestDB(t)
	if err := EnsureSchema(db); err != nil {
		t.Fatalf("EnsureSchema: %v", err)
	}
	tenantID := uuid.New()
	if _, err := db.Exec(`INSERT INTO tenants (id, name, slug, status) VALUES (?, 'T', 't-heartbeat-title', 'active')`, tenantID.String()); err != nil {
		t.Fatalf("insert tenant: %v", err)
	}
	ctx := store.WithTenantID(context.Background(), tenantID)
	contacts := NewSQLiteContactStore(db)
	if err := contacts.UpsertContactWithMetadata(ctx, "discord", "discord-main", "thread-1", "", "launch-thread", "", "group", "group", "", "", map[string]string{"display_title": "launch-thread / product-planning"}); err != nil {
		t.Fatalf("upsert contact: %v", err)
	}

	targets, err := NewSQLiteHeartbeatStore(db).ListDeliveryTargets(ctx, tenantID)
	if err != nil {
		t.Fatalf("ListDeliveryTargets: %v", err)
	}
	if len(targets) != 1 {
		t.Fatalf("targets = %d, want 1", len(targets))
	}
	if targets[0].ChatID != "thread-1" {
		t.Fatalf("chat id = %q, want stable thread id", targets[0].ChatID)
	}
	if targets[0].Title != "launch-thread / product-planning" {
		t.Fatalf("title = %q, want qualified title", targets[0].Title)
	}
}
