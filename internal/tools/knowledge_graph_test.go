package tools

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/nextlevelbuilder/goclaw/internal/store"
)

// ── mock KG store ──────────────────────────────────────────────────

type mockKGStore struct {
	entities  map[string]store.Entity            // keyed by entity ID
	relations []store.Relation                   // all relations
	traversal map[string][]store.TraversalResult // keyed by start entity ID
}

func newMockKGStore() *mockKGStore {
	return &mockKGStore{
		entities:  make(map[string]store.Entity),
		traversal: make(map[string][]store.TraversalResult),
	}
}

func (m *mockKGStore) UpsertEntity(_ context.Context, e *store.Entity) error {
	m.entities[e.ID] = *e
	return nil
}

func (m *mockKGStore) GetEntity(_ context.Context, _, _, entityID string) (*store.Entity, error) {
	if e, ok := m.entities[entityID]; ok {
		return &e, nil
	}
	return nil, fmt.Errorf("entity not found")
}

func (m *mockKGStore) DeleteEntity(context.Context, string, string, string) error { return nil }

func (m *mockKGStore) ListEntities(_ context.Context, _, _ string, opts store.EntityListOptions) ([]store.Entity, error) {
	var out []store.Entity
	for _, e := range m.entities {
		out = append(out, e)
		if opts.Limit > 0 && len(out) >= opts.Limit {
			break
		}
	}
	return out, nil
}

func (m *mockKGStore) SearchEntities(_ context.Context, _, _, query string, limit int) ([]store.Entity, error) {
	var out []store.Entity
	q := strings.ToLower(query)
	for _, e := range m.entities {
		if strings.Contains(strings.ToLower(e.Name), q) || strings.Contains(strings.ToLower(e.Description), q) {
			out = append(out, e)
			if limit > 0 && len(out) >= limit {
				break
			}
		}
	}
	return out, nil
}

func (m *mockKGStore) UpsertRelation(_ context.Context, r *store.Relation) error {
	m.relations = append(m.relations, *r)
	return nil
}

func (m *mockKGStore) DeleteRelation(context.Context, string, string, string) error { return nil }

func (m *mockKGStore) ListRelations(_ context.Context, _, _, entityID string) ([]store.Relation, error) {
	var out []store.Relation
	for _, r := range m.relations {
		if r.SourceEntityID == entityID || r.TargetEntityID == entityID {
			out = append(out, r)
		}
	}
	return out, nil
}

func (m *mockKGStore) ListAllRelations(context.Context, string, string, int) ([]store.Relation, error) {
	return m.relations, nil
}

func (m *mockKGStore) Traverse(_ context.Context, _, _, startEntityID string, _ int) ([]store.TraversalResult, error) {
	return m.traversal[startEntityID], nil
}

func (m *mockKGStore) IngestExtraction(context.Context, string, string, []store.Entity, []store.Relation) ([]string, error) {
	return nil, nil
}

func (m *mockKGStore) PruneByConfidence(context.Context, string, string, float64) (int, error) {
	return 0, nil
}

func (m *mockKGStore) DedupAfterExtraction(context.Context, string, string, []string) (int, int, error) {
	return 0, 0, nil
}

func (m *mockKGStore) ScanDuplicates(context.Context, string, string, float64, int) (int, error) {
	return 0, nil
}

func (m *mockKGStore) ListDedupCandidates(context.Context, string, string, int) ([]store.DedupCandidate, error) {
	return nil, nil
}

func (m *mockKGStore) MergeEntities(context.Context, string, string, string, string) error {
	return nil
}

func (m *mockKGStore) DismissCandidate(context.Context, string, string) error {
	return nil
}

func (m *mockKGStore) Stats(context.Context, string, string) (*store.GraphStats, error) {
	return &store.GraphStats{}, nil
}

func (m *mockKGStore) SetEmbeddingProvider(store.EmbeddingProvider) {}
func (m *mockKGStore) Close() error                                 { return nil }

// ── test helpers ───────────────────────────────────────────────────

var (
	testAgentID = uuid.New()
	testUserID  = "test-user"
)

func kgContext() context.Context {
	ctx := context.Background()
	ctx = store.WithAgentID(ctx, testAgentID)
	ctx = store.WithUserID(ctx, testUserID)
	return ctx
}

// setupBaseGraph creates the shared test graph:
//
//	A(Viettx) →[owns]→ B(GoClaw) →[implements]→ C(Dầu thô) →[related_to]→ D(Kuwait)
//	A(Viettx) →[manages]→ C(Dầu thô)
//	E(Chiến sự Trung Đông) — isolated, no relations
func setupBaseGraph() (*mockKGStore, map[string]string) {
	ms := newMockKGStore()
	ids := map[string]string{
		"A": uuid.NewString(),
		"B": uuid.NewString(),
		"C": uuid.NewString(),
		"D": uuid.NewString(),
		"E": uuid.NewString(),
	}

	entities := []store.Entity{
		{ID: ids["A"], AgentID: testAgentID.String(), UserID: testUserID, Name: "Viettx", EntityType: "person"},
		{ID: ids["B"], AgentID: testAgentID.String(), UserID: testUserID, Name: "GoClaw", EntityType: "project"},
		{ID: ids["C"], AgentID: testAgentID.String(), UserID: testUserID, Name: "Dầu thô", EntityType: "concept"},
		{ID: ids["D"], AgentID: testAgentID.String(), UserID: testUserID, Name: "Kuwait", EntityType: "location"},
		{ID: ids["E"], AgentID: testAgentID.String(), UserID: testUserID, Name: "Chiến sự Trung Đông", EntityType: "event"},
	}
	for i := range entities {
		ms.entities[entities[i].ID] = entities[i]
	}

	ms.relations = []store.Relation{
		{ID: uuid.NewString(), AgentID: testAgentID.String(), UserID: testUserID, SourceEntityID: ids["A"], RelationType: "owns", TargetEntityID: ids["B"]},
		{ID: uuid.NewString(), AgentID: testAgentID.String(), UserID: testUserID, SourceEntityID: ids["A"], RelationType: "manages", TargetEntityID: ids["C"]},
		{ID: uuid.NewString(), AgentID: testAgentID.String(), UserID: testUserID, SourceEntityID: ids["C"], RelationType: "related_to", TargetEntityID: ids["D"]},
		{ID: uuid.NewString(), AgentID: testAgentID.String(), UserID: testUserID, SourceEntityID: ids["B"], RelationType: "implements", TargetEntityID: ids["C"]},
	}

	// Pre-compute traversal results (mock outgoing-only behavior)
	// A → B, C (outgoing from A)
	ms.traversal[ids["A"]] = []store.TraversalResult{
		{Entity: entities[1], Depth: 1, Via: "owns"},    // B=GoClaw
		{Entity: entities[2], Depth: 1, Via: "manages"}, // C=Dầu thô
	}
	// B → C (outgoing from B)
	ms.traversal[ids["B"]] = []store.TraversalResult{
		{Entity: entities[2], Depth: 1, Via: "implements"}, // C=Dầu thô
	}
	// C → D (outgoing from C)
	ms.traversal[ids["C"]] = []store.TraversalResult{
		{Entity: entities[3], Depth: 1, Via: "related_to"}, // D=Kuwait
	}
	// D has no outgoing → empty traversal
	// E is isolated → empty traversal

	return ms, ids
}

// ── tests ──────────────────────────────────────────────────────────

func TestKGTraversal_Tier1_OutgoingEdges(t *testing.T) {
	ms, ids := setupBaseGraph()
	tool := NewKnowledgeGraphSearchTool()
	tool.SetKGStore(ms)

	ctx := kgContext()
	result := tool.executeTraversal(ctx, testAgentID.String(), testUserID, ids["A"], 2, "Viettx")
	text := result.ForLLM

	if !strings.Contains(text, "GoClaw") {
		t.Error("expected result to contain 'GoClaw'")
	}
	if !strings.Contains(text, "Dầu thô") {
		t.Error("expected result to contain 'Dầu thô'")
	}
	if strings.Contains(text, "Direct connections") {
		t.Error("tier 1 should NOT show 'Direct connections'")
	}
}

func TestKGTraversal_Tier2_OnlyIncomingEdges(t *testing.T) {
	ms, ids := setupBaseGraph()
	tool := NewKnowledgeGraphSearchTool()
	tool.SetKGStore(ms)

	ctx := kgContext()
	// D=Kuwait has 0 outgoing, 1 incoming (C→D)
	result := tool.executeTraversal(ctx, testAgentID.String(), testUserID, ids["D"], 2, "Kuwait")
	text := result.ForLLM

	if !strings.Contains(text, "Direct connections") {
		t.Error("expected tier 2 'Direct connections' section")
	}
	if !strings.Contains(text, "Dầu thô") {
		t.Error("expected to see 'Dầu thô' in direct connections")
	}
	if !strings.Contains(text, "—[related_to]→") {
		t.Error("expected relation format '—[related_to]→'")
	}
}

func TestKGTraversal_Tier3_IsolatedWithQuery(t *testing.T) {
	ms, ids := setupBaseGraph()
	tool := NewKnowledgeGraphSearchTool()
	tool.SetKGStore(ms)

	ctx := kgContext()
	// E=Chiến sự TĐ: 0 outgoing, 0 incoming, but searchable by name
	result := tool.executeTraversal(ctx, testAgentID.String(), testUserID, ids["E"], 2, "Chiến sự")
	text := result.ForLLM

	if !strings.Contains(text, "Chiến sự Trung Đông") {
		t.Error("expected tier 3 fallback to find 'Chiến sự Trung Đông' via search")
	}
	if !strings.Contains(text, "Found") {
		t.Error("expected search result format with 'Found'")
	}
}

func TestKGTraversal_Tier3_IsolatedNoQuery(t *testing.T) {
	ms, ids := setupBaseGraph()
	tool := NewKnowledgeGraphSearchTool()
	tool.SetKGStore(ms)

	ctx := kgContext()
	// E=isolated, no query fallback
	result := tool.executeTraversal(ctx, testAgentID.String(), testUserID, ids["E"], 2, "")
	text := result.ForLLM

	if !strings.Contains(text, "No connected entities found") {
		t.Errorf("expected 'No connected entities found', got: %s", text)
	}
}

func TestKGTraversal_Tier2_CappedAt10(t *testing.T) {
	ms := newMockKGStore()
	entityX := uuid.NewString()
	ms.entities[entityX] = store.Entity{ID: entityX, AgentID: testAgentID.String(), UserID: testUserID, Name: "HubNode", EntityType: "concept"}

	// Create 15 incoming relations to X
	for i := range 15 {
		srcID := uuid.NewString()
		srcName := fmt.Sprintf("Source_%02d", i)
		ms.entities[srcID] = store.Entity{ID: srcID, AgentID: testAgentID.String(), UserID: testUserID, Name: srcName, EntityType: "concept"}
		ms.relations = append(ms.relations, store.Relation{
			ID: uuid.NewString(), AgentID: testAgentID.String(), UserID: testUserID,
			SourceEntityID: srcID, RelationType: "connects_to", TargetEntityID: entityX,
		})
	}
	// X has no outgoing → empty traversal

	tool := NewKnowledgeGraphSearchTool()
	tool.SetKGStore(ms)

	ctx := kgContext()
	result := tool.executeTraversal(ctx, testAgentID.String(), testUserID, entityX, 2, "")
	text := result.ForLLM

	count := strings.Count(text, "—[connects_to]→")
	if count > 10 {
		t.Errorf("expected max 10 direct connections, got %d", count)
	}
	if count == 0 {
		t.Error("expected at least 1 direct connection")
	}
}

func TestKGTraversal_Tier1_SkipsTier2WhenTraversalHasResults(t *testing.T) {
	ms, ids := setupBaseGraph()
	tool := NewKnowledgeGraphSearchTool()
	tool.SetKGStore(ms)

	ctx := kgContext()
	// B=GoClaw has 1 outgoing (B→C) and 1 incoming (A→B)
	result := tool.executeTraversal(ctx, testAgentID.String(), testUserID, ids["B"], 2, "GoClaw")
	text := result.ForLLM

	if !strings.Contains(text, "Dầu thô") {
		t.Error("expected traversal to contain 'Dầu thô' (outgoing from B)")
	}
	if strings.Contains(text, "Direct connections") {
		t.Error("tier 1 should NOT fall through to tier 2")
	}
}

func TestKGTraversal_Tier2_RelationFormat(t *testing.T) {
	ms := newMockKGStore()
	idF := uuid.NewString()
	idG := uuid.NewString()
	idH := uuid.NewString()

	ms.entities[idF] = store.Entity{ID: idF, AgentID: testAgentID.String(), UserID: testUserID, Name: "NodeF", EntityType: "concept"}
	ms.entities[idG] = store.Entity{ID: idG, AgentID: testAgentID.String(), UserID: testUserID, Name: "NodeG", EntityType: "concept"}
	ms.entities[idH] = store.Entity{ID: idH, AgentID: testAgentID.String(), UserID: testUserID, Name: "NodeH", EntityType: "concept"}

	// F→G (outgoing from F) and H→F (incoming to F)
	ms.relations = []store.Relation{
		{ID: uuid.NewString(), AgentID: testAgentID.String(), UserID: testUserID, SourceEntityID: idF, RelationType: "owns", TargetEntityID: idG},
		{ID: uuid.NewString(), AgentID: testAgentID.String(), UserID: testUserID, SourceEntityID: idH, RelationType: "manages", TargetEntityID: idF},
	}
	// F has no traversal results (mock empty)

	tool := NewKnowledgeGraphSearchTool()
	tool.SetKGStore(ms)

	ctx := kgContext()
	result := tool.executeTraversal(ctx, testAgentID.String(), testUserID, idF, 2, "")
	text := result.ForLLM

	// Outgoing: F —[owns]→ G
	if !strings.Contains(text, "NodeF —[owns]→ NodeG") {
		t.Errorf("expected 'NodeF —[owns]→ NodeG' in output, got: %s", text)
	}
	// Incoming: H —[manages]→ F
	if !strings.Contains(text, "NodeH —[manages]→ NodeF") {
		t.Errorf("expected 'NodeH —[manages]→ NodeF' in output, got: %s", text)
	}
}

func TestKGTraversal_Tier1_CappedAt20(t *testing.T) {
	ms := newMockKGStore()
	startID := uuid.NewString()
	ms.entities[startID] = store.Entity{ID: startID, AgentID: testAgentID.String(), UserID: testUserID, Name: "Start", EntityType: "concept"}

	// Create 25 traversal results
	var results []store.TraversalResult
	for i := range 25 {
		eid := uuid.NewString()
		name := fmt.Sprintf("Node_%02d", i)
		ms.entities[eid] = store.Entity{ID: eid, AgentID: testAgentID.String(), UserID: testUserID, Name: name, EntityType: "concept"}
		results = append(results, store.TraversalResult{Entity: ms.entities[eid], Depth: 1, Via: "links_to"})
	}
	ms.traversal[startID] = results

	tool := NewKnowledgeGraphSearchTool()
	tool.SetKGStore(ms)

	ctx := kgContext()
	result := tool.executeTraversal(ctx, testAgentID.String(), testUserID, startID, 2, "")
	text := result.ForLLM

	count := strings.Count(text, "links_to")
	if count > 20 {
		t.Errorf("expected max 20 traversal results, got %d", count)
	}
	if !strings.Contains(text, "+5 more entities reachable") {
		t.Errorf("expected truncation hint, got: %s", text)
	}
}

func TestKGSearch_RelationsCappedAt5(t *testing.T) {
	ms := newMockKGStore()
	entityID := uuid.NewString()
	ms.entities[entityID] = store.Entity{ID: entityID, AgentID: testAgentID.String(), UserID: testUserID, Name: "HubEntity", EntityType: "concept"}

	// Create 8 outgoing relations from entity
	for i := range 8 {
		tgtID := uuid.NewString()
		tgtName := fmt.Sprintf("Target_%02d", i)
		ms.entities[tgtID] = store.Entity{ID: tgtID, AgentID: testAgentID.String(), UserID: testUserID, Name: tgtName, EntityType: "concept"}
		ms.relations = append(ms.relations, store.Relation{
			ID: uuid.NewString(), AgentID: testAgentID.String(), UserID: testUserID,
			SourceEntityID: entityID, RelationType: "connects", TargetEntityID: tgtID,
		})
	}

	tool := NewKnowledgeGraphSearchTool()
	tool.SetKGStore(ms)

	ctx := kgContext()
	result := tool.executeSearch(ctx, testAgentID.String(), testUserID, "HubEntity", nil)
	text := result.ForLLM

	relCount := strings.Count(text, "—[connects]→")
	if relCount > 5 {
		t.Errorf("expected max 5 relations per entity in search, got %d", relCount)
	}
	if !strings.Contains(text, "+3 more") {
		t.Errorf("expected truncation hint '+3 more', got: %s", text)
	}
	if !strings.Contains(text, "use entity_id=") {
		t.Error("expected hint to use entity_id for full relations")
	}
}
