//go:build sqlite || sqliteonly

package sqlitestore

import (
	"context"
	"testing"

	"github.com/google/uuid"

	"github.com/nextlevelbuilder/goclaw/internal/store"
)

func TestResolveGroupTitlesUsesChannelContacts(t *testing.T) {
	db := openTestDB(t)
	if err := EnsureSchema(db); err != nil {
		t.Fatalf("EnsureSchema: %v", err)
	}
	tenantID := uuid.New()
	if _, err := db.Exec(`INSERT INTO tenants (id, name, slug, status) VALUES (?, 'T', 't-pending-title', 'active')`, tenantID.String()); err != nil {
		t.Fatalf("insert tenant: %v", err)
	}

	ctx := store.WithTenantID(context.Background(), tenantID)
	contacts := NewSQLiteContactStore(db)
	if err := contacts.UpsertContact(ctx, "discord", "discord-main", "thread-1", "", "launch-thread", "", "group", "group", "", ""); err != nil {
		t.Fatalf("upsert thread contact: %v", err)
	}
	if err := contacts.UpsertContact(ctx, "discord", "discord-main", "parent-1", "", "product-planning", "", "group", "group", "", ""); err != nil {
		t.Fatalf("upsert parent contact: %v", err)
	}

	pending := NewSQLitePendingMessageStore(db)
	titles, err := pending.ResolveGroupTitles(ctx, []store.PendingMessageGroup{
		{ChannelName: "discord-main", HistoryKey: "thread-1"},
		{ChannelName: "discord-main", HistoryKey: "parent-1"},
	})
	if err != nil {
		t.Fatalf("ResolveGroupTitles: %v", err)
	}
	if titles["discord-main:thread-1"] != "launch-thread" {
		t.Fatalf("thread title = %q, want launch-thread", titles["discord-main:thread-1"])
	}
	if titles["discord-main:parent-1"] != "product-planning" {
		t.Fatalf("parent title = %q, want product-planning", titles["discord-main:parent-1"])
	}
}
