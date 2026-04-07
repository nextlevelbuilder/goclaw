//go:build integration

package integration

import (
	"testing"

	"github.com/google/uuid"
	"github.com/nextlevelbuilder/goclaw/internal/store"
	"github.com/nextlevelbuilder/goclaw/internal/store/pg"
)

func newKGStore(t *testing.T) *pg.PGKnowledgeGraphStore {
	t.Helper()
	db := testDB(t)
	pg.InitSqlx(db)
	s := pg.NewPGKnowledgeGraphStore(db)
	s.SetEmbeddingProvider(newMockEmbedProvider())
	return s
}

func makeEntity(agentID, userID, name, entityType string) *store.Entity {
	return &store.Entity{
		AgentID:    agentID,
		UserID:     userID,
		ExternalID: "ext-" + name,
		Name:       name,
		EntityType: entityType,
		Confidence: 0.9,
	}
}

func TestStoreKG_UpsertAndGetEntity(t *testing.T) {
	db := testDB(t)
	tenantID, agentID := seedTenantAgent(t, db)
	ctx := tenantCtx(tenantID)
	s := newKGStore(t)

	aid := agentID.String()
	uid := "kguser-" + agentID.String()[:8]

	e := makeEntity(aid, uid, "Alice", "person")
	e.Description = "a test person"

	if err := s.UpsertEntity(ctx, e); err != nil {
		t.Fatalf("UpsertEntity: %v", err)
	}

	// Fetch by name lookup via ListEntities (we need the DB-assigned ID).
	entities, err := s.ListEntities(ctx, aid, uid, store.EntityListOptions{Limit: 10})
	if err != nil {
		t.Fatalf("ListEntities: %v", err)
	}
	if len(entities) == 0 {
		t.Fatal("expected at least 1 entity after upsert")
	}
	entityID := entities[0].ID

	got, err := s.GetEntity(ctx, aid, uid, entityID)
	if err != nil {
		t.Fatalf("GetEntity: %v", err)
	}
	if got.Name != "Alice" {
		t.Errorf("expected Name=Alice, got %q", got.Name)
	}
	if got.EntityType != "person" {
		t.Errorf("expected EntityType=person, got %q", got.EntityType)
	}

	// Update via re-upsert (same external_id).
	e.Name = "Alice Updated"
	if err := s.UpsertEntity(ctx, e); err != nil {
		t.Fatalf("UpsertEntity update: %v", err)
	}
	got2, err := s.GetEntity(ctx, aid, uid, entityID)
	if err != nil {
		t.Fatalf("GetEntity after update: %v", err)
	}
	if got2.Name != "Alice Updated" {
		t.Errorf("expected Name='Alice Updated', got %q", got2.Name)
	}
}

func TestStoreKG_DeleteEntity(t *testing.T) {
	db := testDB(t)
	tenantID, agentID := seedTenantAgent(t, db)
	ctx := tenantCtx(tenantID)
	s := newKGStore(t)

	aid := agentID.String()
	uid := "kgdel-" + agentID.String()[:8]

	e := makeEntity(aid, uid, "ToDelete", "concept")
	if err := s.UpsertEntity(ctx, e); err != nil {
		t.Fatalf("UpsertEntity: %v", err)
	}

	entities, err := s.ListEntities(ctx, aid, uid, store.EntityListOptions{Limit: 10})
	if err != nil || len(entities) == 0 {
		t.Fatalf("ListEntities: %v / count=%d", err, len(entities))
	}
	entityID := entities[0].ID

	if err := s.DeleteEntity(ctx, aid, uid, entityID); err != nil {
		t.Fatalf("DeleteEntity: %v", err)
	}

	got, err := s.GetEntity(ctx, aid, uid, entityID)
	if err == nil {
		t.Errorf("expected error after delete, got entity: %+v", got)
	}
}

func TestStoreKG_ListEntities(t *testing.T) {
	db := testDB(t)
	tenantID, agentID := seedTenantAgent(t, db)
	ctx := tenantCtx(tenantID)
	s := newKGStore(t)

	aid := agentID.String()
	uid := "kglist-" + agentID.String()[:8]

	names := []string{"E1", "E2", "E3", "E4", "E5"}
	for _, name := range names {
		if err := s.UpsertEntity(ctx, makeEntity(aid, uid, name, "thing")); err != nil {
			t.Fatalf("UpsertEntity %s: %v", name, err)
		}
	}

	// Fetch with limit=3.
	page1, err := s.ListEntities(ctx, aid, uid, store.EntityListOptions{Limit: 3})
	if err != nil {
		t.Fatalf("ListEntities page1: %v", err)
	}
	if len(page1) != 3 {
		t.Errorf("expected 3, got %d", len(page1))
	}

	// Fetch all.
	all, err := s.ListEntities(ctx, aid, uid, store.EntityListOptions{Limit: 10})
	if err != nil {
		t.Fatalf("ListEntities all: %v", err)
	}
	if len(all) < 5 {
		t.Errorf("expected at least 5 entities, got %d", len(all))
	}
}

func TestStoreKG_RelationCRUD(t *testing.T) {
	db := testDB(t)
	tenantID, agentID := seedTenantAgent(t, db)
	ctx := tenantCtx(tenantID)
	s := newKGStore(t)

	aid := agentID.String()
	uid := "kgrel-" + agentID.String()[:8]

	eA := makeEntity(aid, uid, "NodeA", "person")
	eB := makeEntity(aid, uid, "NodeB", "org")
	if err := s.UpsertEntity(ctx, eA); err != nil {
		t.Fatalf("UpsertEntity A: %v", err)
	}
	if err := s.UpsertEntity(ctx, eB); err != nil {
		t.Fatalf("UpsertEntity B: %v", err)
	}

	entities, err := s.ListEntities(ctx, aid, uid, store.EntityListOptions{Limit: 10})
	if err != nil || len(entities) < 2 {
		t.Fatalf("ListEntities: %v / count=%d", err, len(entities))
	}
	// Map by name for stable lookup.
	byName := make(map[string]string)
	for _, e := range entities {
		byName[e.Name] = e.ID
	}
	idA, idB := byName["NodeA"], byName["NodeB"]

	rel := &store.Relation{
		AgentID:        aid,
		UserID:         uid,
		SourceEntityID: idA,
		RelationType:   "works_at",
		TargetEntityID: idB,
		Confidence:     0.95,
	}
	if err := s.UpsertRelation(ctx, rel); err != nil {
		t.Fatalf("UpsertRelation: %v", err)
	}

	// ListRelations for entity A — should include the new relation.
	rels, err := s.ListRelations(ctx, aid, uid, idA)
	if err != nil {
		t.Fatalf("ListRelations: %v", err)
	}
	if len(rels) == 0 {
		t.Fatal("expected at least 1 relation")
	}
	relID := rels[0].ID

	// Delete relation and verify it's gone.
	if err := s.DeleteRelation(ctx, aid, uid, relID); err != nil {
		t.Fatalf("DeleteRelation: %v", err)
	}
	rels2, err := s.ListRelations(ctx, aid, uid, idA)
	if err != nil {
		t.Fatalf("ListRelations after delete: %v", err)
	}
	if len(rels2) != 0 {
		t.Errorf("expected 0 relations after delete, got %d", len(rels2))
	}
}

func TestStoreKG_Traverse(t *testing.T) {
	db := testDB(t)
	tenantID, agentID := seedTenantAgent(t, db)
	ctx := tenantCtx(tenantID)
	s := newKGStore(t)

	aid := agentID.String()
	uid := "kgtrv-" + agentID.String()[:8]

	// Build chain A → B → C.
	for _, name := range []string{"ChainA", "ChainB", "ChainC"} {
		if err := s.UpsertEntity(ctx, makeEntity(aid, uid, name, "node")); err != nil {
			t.Fatalf("UpsertEntity %s: %v", name, err)
		}
	}

	entities, err := s.ListEntities(ctx, aid, uid, store.EntityListOptions{Limit: 10})
	if err != nil || len(entities) < 3 {
		t.Fatalf("ListEntities: %v / count=%d", err, len(entities))
	}
	byName := make(map[string]string)
	for _, e := range entities {
		byName[e.Name] = e.ID
	}
	idA, idB, idC := byName["ChainA"], byName["ChainB"], byName["ChainC"]

	if err := s.UpsertRelation(ctx, &store.Relation{
		AgentID: aid, UserID: uid,
		SourceEntityID: idA, RelationType: "links", TargetEntityID: idB, Confidence: 1.0,
	}); err != nil {
		t.Fatalf("UpsertRelation A→B: %v", err)
	}
	if err := s.UpsertRelation(ctx, &store.Relation{
		AgentID: aid, UserID: uid,
		SourceEntityID: idB, RelationType: "links", TargetEntityID: idC, Confidence: 1.0,
	}); err != nil {
		t.Fatalf("UpsertRelation B→C: %v", err)
	}

	// The CTE seeds root at depth=1; recursive step fires when p.depth < maxDepth.
	// So maxDepth=2 reaches 1-hop neighbors (B), maxDepth=3 reaches 2-hop (C).

	// Traverse from A with maxDepth=2 → should reach B only.
	res1, err := s.Traverse(ctx, aid, uid, idA, 2)
	if err != nil {
		t.Fatalf("Traverse maxDepth=2: %v", err)
	}
	found := make(map[string]bool)
	for _, r := range res1 {
		found[r.Entity.ID] = true
	}
	if !found[idB] {
		t.Errorf("Traverse maxDepth=2: expected B in results, got %v", res1)
	}
	if found[idC] {
		t.Errorf("Traverse maxDepth=2: C should not be reachable")
	}

	// Traverse from A with maxDepth=3 → should reach both B and C.
	res2, err := s.Traverse(ctx, aid, uid, idA, 3)
	if err != nil {
		t.Fatalf("Traverse maxDepth=3: %v", err)
	}
	found2 := make(map[string]bool)
	for _, r := range res2 {
		found2[r.Entity.ID] = true
	}
	if !found2[idB] {
		t.Errorf("Traverse maxDepth=3: expected B in results")
	}
	if !found2[idC] {
		t.Errorf("Traverse maxDepth=3: expected C in results, got %v", res2)
	}
}

func TestStoreKG_Stats(t *testing.T) {
	db := testDB(t)
	tenantID, agentID := seedTenantAgent(t, db)
	ctx := tenantCtx(tenantID)
	s := newKGStore(t)

	aid := agentID.String()
	uid := "kgstat-" + agentID.String()[:8]

	// Upsert 3 entities.
	for _, name := range []string{"S1", "S2", "S3"} {
		if err := s.UpsertEntity(ctx, makeEntity(aid, uid, name, "item")); err != nil {
			t.Fatalf("UpsertEntity %s: %v", name, err)
		}
	}

	entities, err := s.ListEntities(ctx, aid, uid, store.EntityListOptions{Limit: 10})
	if err != nil || len(entities) < 2 {
		t.Fatalf("ListEntities: %v / count=%d", err, len(entities))
	}
	id0, id1 := entities[0].ID, entities[1].ID

	// Upsert 2 relations.
	for _, pair := range [][2]string{{id0, id1}, {id1, id0}} {
		if err := s.UpsertRelation(ctx, &store.Relation{
			AgentID: aid, UserID: uid,
			SourceEntityID: pair[0], RelationType: "ref", TargetEntityID: pair[1], Confidence: 1.0,
		}); err != nil {
			t.Fatalf("UpsertRelation: %v", err)
		}
	}

	stats, err := s.Stats(ctx, aid, uid)
	if err != nil {
		t.Fatalf("Stats: %v", err)
	}
	if stats.EntityCount < 3 {
		t.Errorf("expected EntityCount >= 3, got %d", stats.EntityCount)
	}
	if stats.RelationCount < 2 {
		t.Errorf("expected RelationCount >= 2, got %d", stats.RelationCount)
	}
}

func TestStoreKG_UserScopeIsolation(t *testing.T) {
	db := testDB(t)
	tenantID, agentID := seedTenantAgent(t, db)
	ctx := tenantCtx(tenantID)
	s := newKGStore(t)

	aid := agentID.String()
	user1 := "kgu1-" + agentID.String()[:8]
	user2 := "kgu2-" + agentID.String()[:8]

	if err := s.UpsertEntity(ctx, makeEntity(aid, user1, "User1Entity", "thing")); err != nil {
		t.Fatalf("UpsertEntity user1: %v", err)
	}
	if err := s.UpsertEntity(ctx, makeEntity(aid, user2, "User2Entity", "thing")); err != nil {
		t.Fatalf("UpsertEntity user2: %v", err)
	}

	// ListEntities scoped to user1 should not include user2's entity.
	u1Entities, err := s.ListEntities(ctx, aid, user1, store.EntityListOptions{Limit: 50})
	if err != nil {
		t.Fatalf("ListEntities user1: %v", err)
	}
	for _, e := range u1Entities {
		if e.Name == "User2Entity" {
			t.Error("user1 ListEntities should not return User2Entity")
		}
	}

	// Verify user1's entity is present.
	found := false
	for _, e := range u1Entities {
		if e.Name == "User1Entity" {
			found = true
		}
	}
	if !found {
		t.Error("user1 ListEntities should include User1Entity")
	}

	// GetEntity: user2's ID should not be accessible with user1 credentials.
	u2Entities, err := s.ListEntities(ctx, aid, user2, store.EntityListOptions{Limit: 10})
	if err != nil || len(u2Entities) == 0 {
		t.Fatalf("ListEntities user2: %v / count=%d", err, len(u2Entities))
	}
	u2ID := u2Entities[0].ID
	got, err := s.GetEntity(ctx, aid, user1, u2ID)
	if err == nil {
		t.Errorf("user1 should not access user2's entity, got: %+v", got)
	}

	_ = uuid.Nil // imported for compile
}

// TestStoreKG_TemporalFilter verifies that expired entities (valid_until IS NOT NULL)
// are excluded from ListEntities, SearchEntities, ListRelations, and Stats.
func TestStoreKG_TemporalFilter(t *testing.T) {
	db := testDB(t)
	tenantID, agentID := seedTenantAgent(t, db)
	ctx := tenantCtx(tenantID)
	s := newKGStore(t)

	aid := agentID.String()
	uid := "kgtemporal-" + agentID.String()[:8]

	// Create 2 entities: one current, one expired.
	current := makeEntity(aid, uid, "CurrentFact", "concept")
	expired := makeEntity(aid, uid, "ExpiredFact", "concept")

	if err := s.UpsertEntity(ctx, current); err != nil {
		t.Fatalf("UpsertEntity current: %v", err)
	}
	if err := s.UpsertEntity(ctx, expired); err != nil {
		t.Fatalf("UpsertEntity expired: %v", err)
	}

	// Manually expire the second entity via raw SQL.
	_, err := db.Exec(
		`UPDATE kg_entities SET valid_until = NOW() WHERE agent_id = $1 AND user_id = $2 AND name = 'ExpiredFact'`,
		agentID, uid)
	if err != nil {
		t.Fatalf("expire entity: %v", err)
	}

	// ListEntities should return only the current entity.
	entities, err := s.ListEntities(ctx, aid, uid, store.EntityListOptions{Limit: 50})
	if err != nil {
		t.Fatalf("ListEntities: %v", err)
	}
	for _, e := range entities {
		if e.Name == "ExpiredFact" {
			t.Error("ListEntities returned expired entity — temporal filter missing")
		}
	}
	if len(entities) == 0 {
		t.Error("ListEntities returned 0 entities, expected at least CurrentFact")
	}

	// SearchEntities should also exclude expired.
	searchResults, err := s.SearchEntities(ctx, aid, uid, "Fact", 10)
	if err != nil {
		t.Fatalf("SearchEntities: %v", err)
	}
	for _, e := range searchResults {
		if e.Name == "ExpiredFact" {
			t.Error("SearchEntities returned expired entity — temporal filter missing")
		}
	}

	// Stats should count only current entities.
	stats, err := s.Stats(ctx, aid, uid)
	if err != nil {
		t.Fatalf("GetStats: %v", err)
	}
	if stats.EntityCount != 1 {
		t.Errorf("Stats.EntityCount = %d, want 1 (only current)", stats.EntityCount)
	}

	// Create a relation between two entities, then expire it.
	currentEntities, _ := s.ListEntities(ctx, aid, uid, store.EntityListOptions{Limit: 10})
	if len(currentEntities) < 1 {
		t.Skip("no entities to create relation")
	}

	// Create a second current entity for relation test.
	other := makeEntity(aid, uid, "OtherFact", "concept")
	if err := s.UpsertEntity(ctx, other); err != nil {
		t.Fatalf("UpsertEntity other: %v", err)
	}
	otherEntities, _ := s.ListEntities(ctx, aid, uid, store.EntityListOptions{Limit: 10})
	if len(otherEntities) < 2 {
		t.Skip("need 2 entities for relation test")
	}

	rel := &store.Relation{
		AgentID:        aid,
		UserID:         uid,
		SourceEntityID: otherEntities[0].ID,
		TargetEntityID: otherEntities[1].ID,
		RelationType:   "related_to",
		Confidence:     0.8,
	}
	if err := s.UpsertRelation(ctx, rel); err != nil {
		t.Fatalf("UpsertRelation: %v", err)
	}

	// Expire the relation.
	_, err = db.Exec(
		`UPDATE kg_relations SET valid_until = NOW() WHERE agent_id = $1 AND user_id = $2`,
		agentID, uid)
	if err != nil {
		t.Fatalf("expire relation: %v", err)
	}

	// ListRelations should return 0 (all expired).
	rels, err := s.ListRelations(ctx, aid, uid, otherEntities[0].ID)
	if err != nil {
		t.Fatalf("ListRelations: %v", err)
	}
	if len(rels) != 0 {
		t.Errorf("ListRelations returned %d expired relations, want 0", len(rels))
	}
}
