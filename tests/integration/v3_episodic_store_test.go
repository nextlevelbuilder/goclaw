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
