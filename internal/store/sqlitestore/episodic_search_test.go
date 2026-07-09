//go:build sqlite || sqliteonly

package sqlitestore

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/nextlevelbuilder/goclaw/internal/store"
)

func TestSQLiteEpisodicSearch_TimeFiltersAndExpiry(t *testing.T) {
	db := openTestDB(t)
	if err := EnsureSchema(db); err != nil {
		t.Fatalf("EnsureSchema: %v", err)
	}
	s := NewSQLiteEpisodicStore(db)
	tenantID := uuid.New()
	agentID := uuid.New()
	userID := "time-user"
	ctx := store.WithTenantID(context.Background(), tenantID)
	base := time.Date(2026, 7, 9, 12, 0, 0, 0, time.UTC)
	if _, err := db.ExecContext(ctx, `INSERT INTO tenants (id, name, slug, status) VALUES (?, ?, ?, ?)`, tenantID.String(), "Test Tenant", "test-tenant", "active"); err != nil {
		t.Fatalf("seed tenant: %v", err)
	}
	if _, err := db.ExecContext(ctx, `INSERT INTO agents (id, agent_key, display_name, owner_id, model, tenant_id) VALUES (?, ?, ?, ?, ?, ?)`, agentID.String(), "test-agent", "Test Agent", "owner", "test-model", tenantID.String()); err != nil {
		t.Fatalf("seed agent: %v", err)
	}

	items := []struct {
		sourceID  string
		summary   string
		createdAt time.Time
		expiresAt *time.Time
	}{
		{sourceID: "old", summary: "Project Alpha old planning note", createdAt: base.Add(-48 * time.Hour)},
		{sourceID: "recent", summary: "Project Alpha recent planning note", createdAt: base.Add(-2 * time.Hour)},
		{sourceID: "expired", summary: "Project Alpha expired planning note", createdAt: base.Add(-time.Hour), expiresAt: sqliteEpisodicTimePtr(base.Add(-30 * time.Minute))},
	}
	for _, item := range items {
		ep := &store.EpisodicSummary{
			TenantID:   tenantID,
			AgentID:    agentID,
			UserID:     userID,
			SessionKey: "time-sess",
			Summary:    item.summary,
			KeyTopics:  []string{"project-alpha", "planning"},
			SourceType: "session",
			SourceID:   "time-filter-" + item.sourceID,
			ExpiresAt:  item.expiresAt,
		}
		if err := s.Create(ctx, ep); err != nil {
			t.Fatalf("Create %s: %v", item.sourceID, err)
		}
		if _, err := db.ExecContext(ctx, `UPDATE episodic_summaries SET created_at = ? WHERE id = ?`, item.createdAt.UTC().Format(time.RFC3339Nano), ep.ID.String()); err != nil {
			t.Fatalf("set created_at %s: %v", item.sourceID, err)
		}
	}

	createdAfter := base.Add(-24 * time.Hour)
	results, err := s.Search(ctx, "*", agentID.String(), userID, store.EpisodicSearchOptions{
		MaxResults:   10,
		CreatedAfter: &createdAfter,
	})
	if err != nil {
		t.Fatalf("Search recent: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("recent non-expired results = %d, want 1", len(results))
	}
	if results[0].L0Abstract != "Project Alpha recent planning note" {
		t.Fatalf("recent result = %q, want non-expired recent note", results[0].L0Abstract)
	}

	results, err = s.Search(ctx, "planning", agentID.String(), userID, store.EpisodicSearchOptions{
		MaxResults:     10,
		CreatedAfter:   &createdAfter,
		IncludeExpired: true,
	})
	if err != nil {
		t.Fatalf("Search include expired: %v", err)
	}
	if len(results) != 2 {
		t.Fatalf("include expired results = %d, want 2", len(results))
	}
}

func sqliteEpisodicTimePtr(t time.Time) *time.Time { return &t }
