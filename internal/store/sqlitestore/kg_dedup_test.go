//go:build sqlite || sqliteonly

package sqlitestore

import (
	"testing"

	"github.com/google/uuid"
	"github.com/nextlevelbuilder/goclaw/internal/store"
)

func TestSQLiteKGScanDuplicatesCountsOnlyInsertedCandidates(t *testing.T) {
	db := newHookTestDB(t)
	tenantID, agentID := seedHookTenantAgent(t, db)
	ctx := store.WithSharedKG(sqliteTenantCtx(tenantID))
	kg := NewSQLiteKnowledgeGraphStore(db)

	entities := []*store.Entity{
		{
			AgentID:     agentID.String(),
			UserID:      "u1",
			ExternalID:  "alice-a",
			Name:        "Alice Nguyen",
			EntityType:  "person",
			Description: "Project manager for the migration",
			Confidence:  0.9,
		},
		{
			AgentID:     agentID.String(),
			UserID:      "u2",
			ExternalID:  "alice-b",
			Name:        "Alice Nguyen",
			EntityType:  "person",
			Description: "Project manager for the migration",
			Confidence:  0.9,
		},
	}
	for _, entity := range entities {
		if err := kg.UpsertEntity(ctx, entity); err != nil {
			t.Fatalf("UpsertEntity: %v", err)
		}
	}

	aID, bID := entities[0].ID, entities[1].ID
	if aID > bID {
		aID, bID = bID, aID
	}
	if _, err := db.ExecContext(ctx, `
		INSERT INTO kg_dedup_candidates
			(id, tenant_id, agent_id, user_id, entity_a_id, entity_b_id, similarity, status)
		VALUES (?, ?, ?, '', ?, ?, 1.0, 'dismissed')`,
		uuid.Must(uuid.NewV7()).String(), tenantID.String(), agentID.String(), aID, bID,
	); err != nil {
		t.Fatalf("seed dismissed candidate: %v", err)
	}

	found, err := kg.ScanDuplicates(ctx, agentID.String(), "", 0.90, 100)
	if err != nil {
		t.Fatalf("ScanDuplicates: %v", err)
	}
	if found != 0 {
		t.Fatalf("ScanDuplicates found = %d, want 0 for conflict with dismissed candidate", found)
	}

	if _, err := db.ExecContext(ctx, `DELETE FROM kg_dedup_candidates WHERE entity_a_id = ? AND entity_b_id = ?`, aID, bID); err != nil {
		t.Fatalf("delete seeded candidate: %v", err)
	}
	found, err = kg.ScanDuplicates(ctx, agentID.String(), "", 0.90, 100)
	if err != nil {
		t.Fatalf("ScanDuplicates after delete: %v", err)
	}
	if found != 1 {
		t.Fatalf("ScanDuplicates found = %d, want 1 newly inserted candidate", found)
	}
}
