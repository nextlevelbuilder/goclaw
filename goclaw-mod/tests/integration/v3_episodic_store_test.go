//go:build integration

package integration

import (
	"fmt"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/nextlevelbuilder/goclaw/internal/store"
	"github.com/nextlevelbuilder/goclaw/internal/store/pg"
)

func newEpisodicStore(t *testing.T) *pg.PGEpisodicStore {
	t.Helper()
	db := testDB(t)
	pg.InitSqlx(db)
	return pg.NewPGEpisodicStore(db)
}

func episodicTimePtr(t time.Time) *time.Time { return &t }

func TestStoreEpisodic_CreateAndGet(t *testing.T) {
	db := testDB(t)
	tenantID, agentID := seedTenantAgent(t, db)
	ctx := tenantCtx(tenantID)
	s := newEpisodicStore(t)

	ep := &store.EpisodicSummary{
		TenantID:   tenantID,
		AgentID:    agentID,
		UserID:     "ep-user-" + tenantID.String()[:8],
		SessionKey: "sess-001",
		Summary:    "User discussed project deadlines and team coordination",
		KeyTopics:  []string{"deadlines", "coordination"},
		L0Abstract: "Project deadline discussion",
		SourceType: "session",
		SourceID:   "src-" + uuid.New().String()[:8],
		TurnCount:  10,
		TokenCount: 500,
	}
	if err := s.Create(ctx, ep); err != nil {
		t.Fatalf("Create: %v", err)
	}
	if ep.ID == uuid.Nil {
		t.Fatal("expected non-nil ID after Create")
	}

	got, err := s.Get(ctx, ep.ID.String())
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got.Summary != ep.Summary {
		t.Errorf("Summary = %q, want %q", got.Summary, ep.Summary)
	}
	if got.L0Abstract != "Project deadline discussion" {
		t.Errorf("L0Abstract = %q, want %q", got.L0Abstract, "Project deadline discussion")
	}
	if got.TurnCount != 10 {
		t.Errorf("TurnCount = %d, want 10", got.TurnCount)
	}
	if got.TokenCount != 500 {
		t.Errorf("TokenCount = %d, want 500", got.TokenCount)
	}
	if len(got.KeyTopics) != 2 {
		t.Errorf("KeyTopics len = %d, want 2", len(got.KeyTopics))
	}

	// ExistsBySourceID
	exists, err := s.ExistsBySourceID(ctx, agentID.String(), ep.UserID, ep.SourceID)
	if err != nil {
		t.Fatalf("ExistsBySourceID: %v", err)
	}
	if !exists {
		t.Error("ExistsBySourceID returned false")
	}
}

func TestStoreEpisodic_List(t *testing.T) {
	db := testDB(t)
	tenantID, agentID := seedTenantAgent(t, db)
	ctx := tenantCtx(tenantID)
	s := newEpisodicStore(t)
	userID := "list-user-" + tenantID.String()[:8]

	// Create 3 summaries
	for i := 0; i < 3; i++ {
		ep := &store.EpisodicSummary{
			TenantID:   tenantID,
			AgentID:    agentID,
			UserID:     userID,
			SessionKey: fmt.Sprintf("sess-%03d", i),
			Summary:    fmt.Sprintf("Summary %d", i),
			L0Abstract: fmt.Sprintf("Abstract %d", i),
			SourceType: "session",
			SourceID:   fmt.Sprintf("list-src-%d-%s", i, tenantID.String()[:8]),
			TurnCount:  5,
			TokenCount: 200,
		}
		if err := s.Create(ctx, ep); err != nil {
			t.Fatalf("Create %d: %v", i, err)
		}
		// Small delay for ordering by created_at
		time.Sleep(5 * time.Millisecond)
	}

	// List with limit
	results, err := s.List(ctx, agentID.String(), userID, 2, 0)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(results) != 2 {
		t.Errorf("List len = %d, want 2", len(results))
	}

	// Verify DESC order (newest first)
	if len(results) == 2 && results[0].CreatedAt.Before(results[1].CreatedAt) {
		t.Error("expected DESC order (newest first)")
	}

	// List all
	all, err := s.List(ctx, agentID.String(), userID, 10, 0)
	if err != nil {
		t.Fatalf("List all: %v", err)
	}
	if len(all) != 3 {
		t.Errorf("List all len = %d, want 3", len(all))
	}

	// ListUnpromoted (all should be unpromoted)
	unpromoted, err := s.ListUnpromoted(ctx, agentID.String(), userID, 10)
	if err != nil {
		t.Fatalf("ListUnpromoted: %v", err)
	}
	if len(unpromoted) != 3 {
		t.Errorf("ListUnpromoted len = %d, want 3", len(unpromoted))
	}

	// MarkPromoted, then CountUnpromoted
	if len(unpromoted) > 0 {
		if err := s.MarkPromoted(ctx, []string{unpromoted[0].ID.String()}); err != nil {
			t.Fatalf("MarkPromoted: %v", err)
		}
		count, err := s.CountUnpromoted(ctx, agentID.String(), userID)
		if err != nil {
			t.Fatalf("CountUnpromoted: %v", err)
		}
		if count != 2 {
			t.Errorf("CountUnpromoted = %d, want 2", count)
		}
	}
}

func TestStoreEpisodic_FTSSearch(t *testing.T) {
	db := testDB(t)
	tenantID, agentID := seedTenantAgent(t, db)
	ctx := tenantCtx(tenantID)
	s := newEpisodicStore(t)
	userID := "fts-user-" + tenantID.String()[:8]

	summaries := []struct {
		summary string
		l0      string
		srcID   string
	}{
		{"The deployment pipeline uses Docker containers for isolation", "Docker deployment", "fts-1-" + tenantID.String()[:8]},
		{"Database migration strategy with PostgreSQL and pgvector", "DB migration", "fts-2-" + tenantID.String()[:8]},
		{"Frontend React components with TypeScript type safety", "React frontend", "fts-3-" + tenantID.String()[:8]},
	}
	for _, item := range summaries {
		ep := &store.EpisodicSummary{
			TenantID:   tenantID,
			AgentID:    agentID,
			UserID:     userID,
			SessionKey: "fts-sess",
			Summary:    item.summary,
			L0Abstract: item.l0,
			SourceType: "session",
			SourceID:   item.srcID,
			TurnCount:  5,
			TokenCount: 200,
		}
		if err := s.Create(ctx, ep); err != nil {
			t.Fatalf("Create: %v", err)
		}
	}

	// Search for "Docker" — should match first summary
	results, err := s.Search(ctx, "Docker containers deployment", agentID.String(), userID, store.EpisodicSearchOptions{
		MaxResults: 10,
	})
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if len(results) == 0 {
		t.Fatal("Search returned 0 results, expected at least 1")
	}
	if results[0].L0Abstract != "Docker deployment" {
		t.Errorf("top result L0 = %q, want %q", results[0].L0Abstract, "Docker deployment")
	}

	// Search for "PostgreSQL" — should match DB migration summary
	results2, err := s.Search(ctx, "PostgreSQL migration", agentID.String(), userID, store.EpisodicSearchOptions{
		MaxResults: 10,
	})
	if err != nil {
		t.Fatalf("Search PostgreSQL: %v", err)
	}
	if len(results2) == 0 {
		t.Fatal("PostgreSQL search returned 0 results")
	}
}

func TestStoreEpisodic_SearchSharedBlankL0FallsBackToSummary(t *testing.T) {
	db := testDB(t)
	tenantID, agentID := seedTenantAgent(t, db)
	ctx := tenantCtx(tenantID)
	s := newEpisodicStore(t)

	summary := "Project Alpha workshop notes mention visual hierarchy as a focus area"
	ep := &store.EpisodicSummary{
		TenantID:   tenantID,
		AgentID:    agentID,
		UserID:     "",
		SessionKey: "channel:discord",
		Summary:    summary,
		KeyTopics:  []string{"project-alpha", "workshop-notes", "visual-hierarchy"},
		SourceType: "channel",
		SourceID:   "channel-blank-l0-" + tenantID.String()[:8],
	}
	if err := s.Create(ctx, ep); err != nil {
		t.Fatalf("Create: %v", err)
	}

	if _, err := db.ExecContext(ctx, `UPDATE episodic_summaries SET l0_abstract = '' WHERE id = $1`, ep.ID); err != nil {
		t.Fatalf("force blank l0: %v", err)
	}

	results, err := s.Search(ctx, "Project Alpha visual hierarchy", agentID.String(), "guild:discord:user:sample", store.EpisodicSearchOptions{
		MaxResults: 10,
	})
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if len(results) == 0 {
		t.Fatal("Search returned 0 results, expected shared channel memory")
	}
	if results[0].L0Abstract == "" {
		t.Fatal("Search returned blank L0Abstract, want summary fallback")
	}
	if results[0].L0Abstract != summary {
		t.Fatalf("Search L0Abstract = %q, want summary fallback", results[0].L0Abstract)
	}
}

func TestStoreEpisodic_SearchCreatedAtAndExpiryFilters(t *testing.T) {
	db := testDB(t)
	tenantID, agentID := seedTenantAgent(t, db)
	ctx := tenantCtx(tenantID)
	s := newEpisodicStore(t)
	userID := "time-user-" + tenantID.String()[:8]
	base := time.Date(2026, 7, 9, 12, 0, 0, 0, time.UTC)

	items := []struct {
		sourceID  string
		summary   string
		createdAt time.Time
		expiresAt *time.Time
	}{
		{
			sourceID:  "old",
			summary:   "Project Alpha old planning note",
			createdAt: base.Add(-48 * time.Hour),
		},
		{
			sourceID:  "recent",
			summary:   "Project Alpha recent planning note",
			createdAt: base.Add(-2 * time.Hour),
		},
		{
			sourceID:  "expired",
			summary:   "Project Alpha expired planning note",
			createdAt: base.Add(-1 * time.Hour),
			expiresAt: episodicTimePtr(base.Add(-30 * time.Minute)),
		},
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
			SourceID:   "time-filter-" + item.sourceID + "-" + tenantID.String()[:8],
		}
		if item.expiresAt != nil {
			ep.ExpiresAt = item.expiresAt
		}
		if err := s.Create(ctx, ep); err != nil {
			t.Fatalf("Create %s: %v", item.sourceID, err)
		}
		if _, err := db.ExecContext(ctx, `UPDATE episodic_summaries SET created_at = $1 WHERE id = $2`, item.createdAt, ep.ID); err != nil {
			t.Fatalf("set created_at %s: %v", item.sourceID, err)
		}
	}

	createdAfter := base.Add(-24 * time.Hour)
	results, err := s.Search(ctx, "Project Alpha planning", agentID.String(), userID, store.EpisodicSearchOptions{
		MaxResults:     10,
		CreatedAfter:   &createdAfter,
		IncludeExpired: true,
	})
	if err != nil {
		t.Fatalf("Search with CreatedAfter: %v", err)
	}
	if len(results) != 2 {
		t.Fatalf("CreatedAfter results = %d, want 2 recent+expired", len(results))
	}
	for _, r := range results {
		if r.CreatedAt.Before(createdAfter) {
			t.Fatalf("result before CreatedAfter: %s < %s", r.CreatedAt, createdAfter)
		}
	}

	results, err = s.Search(ctx, "*", agentID.String(), userID, store.EpisodicSearchOptions{
		MaxResults:   10,
		CreatedAfter: &createdAfter,
	})
	if err != nil {
		t.Fatalf("Recent list search: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("non-expired recent results = %d, want 1", len(results))
	}
	if results[0].L0Abstract != "Project Alpha recent planning note" {
		t.Fatalf("recent result = %q, want non-expired recent note", results[0].L0Abstract)
	}
	if results[0].ExpiresAt != nil {
		t.Fatalf("non-expired result ExpiresAt = %v, want nil", results[0].ExpiresAt)
	}

	createdBefore := base.Add(-24 * time.Hour)
	results, err = s.Search(ctx, "*", agentID.String(), userID, store.EpisodicSearchOptions{
		MaxResults:     10,
		CreatedBefore:  &createdBefore,
		IncludeExpired: true,
	})
	if err != nil {
		t.Fatalf("CreatedBefore list search: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("CreatedBefore results = %d, want 1 old note", len(results))
	}
	if !results[0].CreatedAt.Before(createdBefore) {
		t.Fatalf("result not before CreatedBefore: %s >= %s", results[0].CreatedAt, createdBefore)
	}
}

func TestStoreEpisodic_TenantIsolation(t *testing.T) {
	db := testDB(t)
	tenantA, agentA := seedTenantAgent(t, db)
	tenantB, _ := seedTenantAgent(t, db)
	ctxA := tenantCtx(tenantA)
	ctxB := tenantCtx(tenantB)
	s := newEpisodicStore(t)
	userID := "iso-user-" + tenantA.String()[:8]

	ep := &store.EpisodicSummary{
		TenantID:   tenantA,
		AgentID:    agentA,
		UserID:     userID,
		SessionKey: "iso-sess",
		Summary:    "Tenant A secret discussion about product strategy",
		L0Abstract: "Product strategy",
		SourceType: "session",
		SourceID:   "iso-src-" + tenantA.String()[:8],
		TurnCount:  5,
		TokenCount: 200,
	}
	if err := s.Create(ctxA, ep); err != nil {
		t.Fatalf("Create: %v", err)
	}

	// Tenant B cannot Get
	_, err := s.Get(ctxB, ep.ID.String())
	if err == nil {
		t.Error("tenant B can Get tenant A's episodic — isolation broken")
	}

	// Tenant B cannot List
	list, err := s.List(ctxB, agentA.String(), userID, 10, 0)
	if err != nil {
		t.Fatalf("List from B: %v", err)
	}
	if len(list) != 0 {
		t.Errorf("tenant B sees %d episodic summaries — isolation broken", len(list))
	}

	// Tenant A can see its own
	listA, err := s.List(ctxA, agentA.String(), userID, 10, 0)
	if err != nil {
		t.Fatalf("List from A: %v", err)
	}
	if len(listA) != 1 {
		t.Errorf("tenant A sees %d, want 1", len(listA))
	}
}
