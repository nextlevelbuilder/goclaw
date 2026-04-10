package consolidation

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/nextlevelbuilder/goclaw/internal/eventbus"
	"github.com/nextlevelbuilder/goclaw/internal/knowledgegraph"
	"github.com/nextlevelbuilder/goclaw/internal/providers"
	"github.com/nextlevelbuilder/goclaw/internal/store"
)

// recallCall is a single RecordRecall invocation captured by the mock store
// so tests can assert the (id, score) stream forwarded by memory_search.
type recallCall struct {
	ID    string
	Score float64
}

// mockEpisodicStore implements store.EpisodicStore for testing.
type mockEpisodicStore struct {
	created     []*store.EpisodicSummary
	existsByID  map[string]bool
	unpromoted  []store.EpisodicSummary
	promoted    map[string]bool
	countResult int
	countCalls  int // set by CountUnpromoted — used by disabled-skip tests
	recallCalls []recallCall
	pruneErr    error
	pruneCount  int
	mu          sync.Mutex
}

func (m *mockEpisodicStore) Create(_ context.Context, ep *store.EpisodicSummary) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.created = append(m.created, ep)
	return nil
}

func (m *mockEpisodicStore) Get(context.Context, string) (*store.EpisodicSummary, error) {
	return nil, nil
}

func (m *mockEpisodicStore) Delete(context.Context, string) error { return nil }

func (m *mockEpisodicStore) List(context.Context, string, string, int, int) ([]store.EpisodicSummary, error) {
	return nil, nil
}

func (m *mockEpisodicStore) Search(context.Context, string, string, string, store.EpisodicSearchOptions) ([]store.EpisodicSearchResult, error) {
	return nil, nil
}

func (m *mockEpisodicStore) ExistsBySourceID(_ context.Context, _, _, sourceID string) (bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.existsByID[sourceID], nil
}

func (m *mockEpisodicStore) PruneExpired(_ context.Context) (int, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.pruneErr != nil {
		return 0, m.pruneErr
	}
	result := m.pruneCount
	m.pruneCount = 0
	return result, nil
}

func (m *mockEpisodicStore) CountUnpromoted(_ context.Context, _, _ string) (int, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.countCalls++
	return m.countResult, nil
}

func (m *mockEpisodicStore) ListUnpromoted(_ context.Context, _, _ string, _ int) ([]store.EpisodicSummary, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.unpromoted, nil
}

// ListUnpromotedScored returns the same fixture as ListUnpromoted; tests that
// need to verify sort order override the method directly on a dedicated mock.
func (m *mockEpisodicStore) ListUnpromotedScored(_ context.Context, _, _ string, _ int) ([]store.EpisodicSummary, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.unpromoted, nil
}

// RecordRecall appends the call into the recallCalls slice so tests can
// assert which episodic IDs received which score.
func (m *mockEpisodicStore) RecordRecall(_ context.Context, id string, score float64) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.recallCalls = append(m.recallCalls, recallCall{ID: id, Score: score})
	return nil
}

func (m *mockEpisodicStore) MarkPromoted(_ context.Context, ids []string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, id := range ids {
		m.promoted[id] = true
	}
	return nil
}

func (m *mockEpisodicStore) SetEmbeddingProvider(store.EmbeddingProvider) {}

func (m *mockEpisodicStore) Close() error { return nil }

// mockKGStore implements store.KnowledgeGraphStore for testing.
type mockKGStore struct {
	ingestedEntities  []store.Entity
	ingestedRelations []store.Relation
	dedupMerged       int
	dedupFlagged      int
	dedupErr          error
	mu                sync.Mutex
}

func (m *mockKGStore) IngestExtraction(ctx context.Context, agentID, userID string, entities []store.Entity, relations []store.Relation) ([]string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.ingestedEntities = append(m.ingestedEntities, entities...)
	m.ingestedRelations = append(m.ingestedRelations, relations...)

	// Return mock IDs for ingested entities
	ids := make([]string, len(entities))
	for i := range entities {
		ids[i] = fmt.Sprintf("entity-%d", i)
	}
	return ids, nil
}

func (m *mockKGStore) DedupAfterExtraction(_ context.Context, _, _ string, _ []string) (int, int, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.dedupErr != nil {
		return 0, 0, m.dedupErr
	}
	return m.dedupMerged, m.dedupFlagged, nil
}

// Implement remaining store.KnowledgeGraphStore methods
func (m *mockKGStore) UpsertEntity(context.Context, *store.Entity) error { return nil }
func (m *mockKGStore) GetEntity(context.Context, string, string, string) (*store.Entity, error) { return nil, nil }
func (m *mockKGStore) DeleteEntity(context.Context, string, string, string) error { return nil }
func (m *mockKGStore) ListEntities(context.Context, string, string, store.EntityListOptions) ([]store.Entity, error) { return nil, nil }
func (m *mockKGStore) SearchEntities(context.Context, string, string, string, int) ([]store.Entity, error) { return nil, nil }
func (m *mockKGStore) UpsertRelation(context.Context, *store.Relation) error { return nil }
func (m *mockKGStore) DeleteRelation(context.Context, string, string, string) error { return nil }
func (m *mockKGStore) ListRelations(context.Context, string, string, string) ([]store.Relation, error) { return nil, nil }
func (m *mockKGStore) ListAllRelations(context.Context, string, string, int) ([]store.Relation, error) { return nil, nil }
func (m *mockKGStore) Traverse(context.Context, string, string, string, int) ([]store.TraversalResult, error) { return nil, nil }
func (m *mockKGStore) PruneByConfidence(context.Context, string, string, float64) (int, error) { return 0, nil }
func (m *mockKGStore) ScanDuplicates(context.Context, string, string, float64, int) (int, error) { return 0, nil }
func (m *mockKGStore) ListDedupCandidates(context.Context, string, string, int) ([]store.DedupCandidate, error) { return nil, nil }
func (m *mockKGStore) MergeEntities(context.Context, string, string, string, string) error { return nil }
func (m *mockKGStore) DismissCandidate(context.Context, string, string) error { return nil }
func (m *mockKGStore) Stats(context.Context, string, string) (*store.GraphStats, error) { return nil, nil }
func (m *mockKGStore) ListEntitiesTemporal(context.Context, string, string, store.EntityListOptions, store.TemporalQueryOptions) ([]store.Entity, error) { return nil, nil }
func (m *mockKGStore) SupersedeEntity(context.Context, *store.Entity, *store.Entity) error { return nil }
func (m *mockKGStore) SetEmbeddingProvider(store.EmbeddingProvider) {}
func (m *mockKGStore) Close() error { return nil }

// mockMemoryStore implements store.MemoryStore for testing.
type mockMemoryStore struct {
	docs      map[string]string
	indexed   map[string]bool
	mu        sync.Mutex
}

func newMockMemoryStore() *mockMemoryStore {
	return &mockMemoryStore{
		docs:    make(map[string]string),
		indexed: make(map[string]bool),
	}
}

func (m *mockMemoryStore) PutDocument(_ context.Context, _, _, path, content string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.docs[path] = content
	return nil
}

func (m *mockMemoryStore) IndexDocument(_ context.Context, _, _, path string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.indexed[path] = true
	return nil
}

// Implement remaining store.MemoryStore methods
func (m *mockMemoryStore) GetDocument(context.Context, string, string, string) (string, error) { return "", nil }
func (m *mockMemoryStore) DeleteDocument(context.Context, string, string, string) error { return nil }
func (m *mockMemoryStore) ListDocuments(context.Context, string, string) ([]store.DocumentInfo, error) { return nil, nil }
func (m *mockMemoryStore) ListAllDocumentsGlobal(context.Context) ([]store.DocumentInfo, error) { return nil, nil }
func (m *mockMemoryStore) ListAllDocuments(context.Context, string) ([]store.DocumentInfo, error) { return nil, nil }
func (m *mockMemoryStore) GetDocumentDetail(context.Context, string, string, string) (*store.DocumentDetail, error) { return nil, nil }
func (m *mockMemoryStore) ListChunks(context.Context, string, string, string) ([]store.ChunkInfo, error) { return nil, nil }
func (m *mockMemoryStore) Search(context.Context, string, string, string, store.MemorySearchOptions) ([]store.MemorySearchResult, error) { return nil, nil }
func (m *mockMemoryStore) IndexAll(context.Context, string, string) error { return nil }
func (m *mockMemoryStore) SetEmbeddingProvider(store.EmbeddingProvider) {}
func (m *mockMemoryStore) Close() error { return nil }

// mockSessionStore implements store.SessionCoreStore for testing.
type mockSessionStore struct {
	history []providers.Message
	summary string
}

func (m *mockSessionStore) GetOrCreate(context.Context, string) *store.SessionData { return nil }
func (m *mockSessionStore) Get(context.Context, string) *store.SessionData { return nil }
func (m *mockSessionStore) AddMessage(context.Context, string, providers.Message) {}

func (m *mockSessionStore) GetHistory(_ context.Context, _ string) []providers.Message {
	return m.history
}

func (m *mockSessionStore) GetSummary(_ context.Context, _ string) string {
	return m.summary
}

func (m *mockSessionStore) SetSummary(context.Context, string, string) {}
func (m *mockSessionStore) GetLabel(context.Context, string) string { return "" }
func (m *mockSessionStore) SetLabel(context.Context, string, string) {}
func (m *mockSessionStore) SetAgentInfo(context.Context, string, uuid.UUID, string) {}
func (m *mockSessionStore) TruncateHistory(context.Context, string, int) {}
func (m *mockSessionStore) SetHistory(context.Context, string, []providers.Message) {}
func (m *mockSessionStore) Reset(context.Context, string) {}
func (m *mockSessionStore) Delete(context.Context, string) error { return nil }
func (m *mockSessionStore) Save(context.Context, string) error { return nil }

// mockDomainEventBus implements eventbus.DomainEventBus for testing.
type mockDomainEventBus struct {
	published []eventbus.DomainEvent
	handlers  map[eventbus.EventType][]eventbus.DomainEventHandler
	mu        sync.Mutex
}

func newMockDomainEventBus() *mockDomainEventBus {
	return &mockDomainEventBus{
		handlers: make(map[eventbus.EventType][]eventbus.DomainEventHandler),
	}
}

func (m *mockDomainEventBus) Publish(event eventbus.DomainEvent) {
	m.mu.Lock()
	m.published = append(m.published, event)
	handlers := m.handlers[event.Type]
	m.mu.Unlock()

	for _, h := range handlers {
		_ = h(context.Background(), event)
	}
}

func (m *mockDomainEventBus) Subscribe(eventType eventbus.EventType, handler eventbus.DomainEventHandler) func() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.handlers[eventType] = append(m.handlers[eventType], handler)
	return func() {
		m.mu.Lock()
		defer m.mu.Unlock()
		handlers := m.handlers[eventType]
		for i, h := range handlers {
			if h != nil { // simple equality check for cleanup
				handlers = append(handlers[:i], handlers[i+1:]...)
				m.handlers[eventType] = handlers
				break
			}
		}
	}
}

func (m *mockDomainEventBus) Start(_ context.Context) {}

func (m *mockDomainEventBus) Drain(_ time.Duration) error { return nil }

// Test episodic worker

func TestEpisodicWorkerHandle_WithSummary(t *testing.T) {
	mockStore := &mockEpisodicStore{existsByID: make(map[string]bool)}
	mockEventBus := newMockDomainEventBus()
	mockProvider := &mockProvider{
		chatResp: &providers.ChatResponse{
			Content: "Summarized content",
		},
	}

	worker := &episodicWorker{
		store:    mockStore,
		provider: mockProvider,
		model:    "test-model",
		eventBus: mockEventBus,
	}

	ctx := context.Background()
	event := eventbus.DomainEvent{
		Type:     eventbus.EventSessionCompleted,
		TenantID: uuid.New().String(),
		AgentID:  uuid.New().String(),
		UserID:   "test-user",
		Payload: &eventbus.SessionCompletedPayload{
			SessionKey:     "session-123",
			CompactionCount: 1,
			Summary:        "Pre-computed summary",
			MessageCount:   10,
			TokensUsed:     1000,
		},
	}

	err := worker.Handle(ctx, event)
	if err != nil {
		t.Fatalf("Handle failed: %v", err)
	}

	if len(mockStore.created) != 1 {
		t.Errorf("Expected 1 created episodic, got %d", len(mockStore.created))
	}
	if mockStore.created[0].Summary != "Pre-computed summary" {
		t.Errorf("Expected summary 'Pre-computed summary', got %q", mockStore.created[0].Summary)
	}
	if len(mockEventBus.published) == 0 {
		t.Error("Expected episodic.created event to be published")
	}
}

func TestEpisodicWorkerHandle_InvalidPayload(t *testing.T) {
	worker := &episodicWorker{}

	ctx := context.Background()
	event := eventbus.DomainEvent{
		Type:    eventbus.EventSessionCompleted,
		Payload: &eventbus.EntityUpsertedPayload{}, // wrong payload type
	}

	err := worker.Handle(ctx, event)
	if err == nil {
		t.Fatal("Expected error for invalid payload type")
	}
}

func TestEpisodicWorkerHandle_DuplicateSourceID(t *testing.T) {
	mockStore := &mockEpisodicStore{
		existsByID: map[string]bool{"session-123:1": true},
	}

	worker := &episodicWorker{
		store: mockStore,
	}

	ctx := context.Background()
	event := eventbus.DomainEvent{
		Type:     eventbus.EventSessionCompleted,
		TenantID: uuid.New().String(),
		AgentID:  uuid.New().String(),
		UserID:   "test-user",
		Payload: &eventbus.SessionCompletedPayload{
			SessionKey:      "session-123",
			CompactionCount: 1,
			Summary:         "Summary",
		},
	}

	err := worker.Handle(ctx, event)
	if err != nil {
		t.Fatalf("Handle failed: %v", err)
	}

	// Should not create new episodic for duplicate
	if len(mockStore.created) != 0 {
		t.Errorf("Expected 0 created episodics for duplicate, got %d", len(mockStore.created))
	}
}

// Test semantic worker

func TestSemanticWorkerHandle_WithValidExtraction(t *testing.T) {
	mockKG := &mockKGStore{}
	mockExtractor := &mockExtractor{
		result: &knowledgegraph.ExtractionResult{
			Entities: []store.Entity{
				{Name: "Entity1", EntityType: "Person"},
				{Name: "Entity2", EntityType: "Organization"},
			},
			Relations: []store.Relation{
				{SourceEntityID: "e1", TargetEntityID: "e2", RelationType: "works_at"},
			},
		},
	}
	mockEventBus := newMockDomainEventBus()

	worker := &semanticWorker{
		kgStore:   mockKG,
		extractor: mockExtractor,
		eventBus:  mockEventBus,
	}

	ctx := context.Background()
	event := eventbus.DomainEvent{
		Type:     eventbus.EventEpisodicCreated,
		TenantID: uuid.New().String(),
		AgentID:  uuid.New().String(),
		UserID:   "test-user",
		Payload: &eventbus.EpisodicCreatedPayload{
			EpisodicID: "ep-123",
			Summary:    "Alice works at TechCorp",
		},
	}

	err := worker.Handle(ctx, event)
	if err != nil {
		t.Fatalf("Handle failed: %v", err)
	}

	if len(mockKG.ingestedEntities) != 2 {
		t.Errorf("Expected 2 ingested entities, got %d", len(mockKG.ingestedEntities))
	}

	// Should publish entity.upserted event
	entityUpsertedCount := 0
	for _, pub := range mockEventBus.published {
		if pub.Type == eventbus.EventEntityUpserted {
			entityUpsertedCount++
		}
	}
	if entityUpsertedCount != 1 {
		t.Errorf("Expected 1 entity.upserted event, got %d", entityUpsertedCount)
	}
}

func TestSemanticWorkerHandle_NilExtractor(t *testing.T) {
	worker := &semanticWorker{
		extractor: nil,
	}

	ctx := context.Background()
	event := eventbus.DomainEvent{
		Type:    eventbus.EventEpisodicCreated,
		Payload: &eventbus.EpisodicCreatedPayload{Summary: "test"},
	}

	// Should not error, just return nil
	err := worker.Handle(ctx, event)
	if err != nil {
		t.Fatalf("Handle should not error with nil extractor: %v", err)
	}
}

func TestSemanticWorkerHandle_InvalidPayload(t *testing.T) {
	worker := &semanticWorker{}

	ctx := context.Background()
	event := eventbus.DomainEvent{
		Type:    eventbus.EventEpisodicCreated,
		Payload: &eventbus.EntityUpsertedPayload{}, // wrong payload type
	}

	err := worker.Handle(ctx, event)
	if err == nil {
		t.Fatal("Expected error for invalid payload type")
	}
}

func TestSemanticWorkerHandle_ExtractionError(t *testing.T) {
	mockKG := &mockKGStore{}
	mockExtractor := &mockExtractor{
		err: errors.New("extraction failed"),
	}

	worker := &semanticWorker{
		kgStore:   mockKG,
		extractor: mockExtractor,
	}

	ctx := context.Background()
	event := eventbus.DomainEvent{
		Type:    eventbus.EventEpisodicCreated,
		Payload: &eventbus.EpisodicCreatedPayload{Summary: "test"},
	}

	// Should not propagate error (non-fatal in worker design)
	err := worker.Handle(ctx, event)
	if err != nil {
		t.Fatalf("Handle should not propagate extraction error: %v", err)
	}
}

// Test dedup worker

func TestDedupWorkerHandle_ValidPayload(t *testing.T) {
	mockKG := &mockKGStore{
		dedupMerged:  2,
		dedupFlagged: 1,
	}

	worker := &dedupWorker{
		kgStore: mockKG,
	}

	ctx := context.Background()
	event := eventbus.DomainEvent{
		Type:     eventbus.EventEntityUpserted,
		AgentID:  "agent-123",
		UserID:   "user-123",
		Payload: &eventbus.EntityUpsertedPayload{
			EntityIDs: []string{"e1", "e2", "e3"},
		},
	}

	err := worker.Handle(ctx, event)
	if err != nil {
		t.Fatalf("Handle failed: %v", err)
	}
}

func TestDedupWorkerHandle_EmptyEntityList(t *testing.T) {
	mockKG := &mockKGStore{}

	worker := &dedupWorker{
		kgStore: mockKG,
	}

	ctx := context.Background()
	event := eventbus.DomainEvent{
		Type:    eventbus.EventEntityUpserted,
		Payload: &eventbus.EntityUpsertedPayload{EntityIDs: []string{}},
	}

	err := worker.Handle(ctx, event)
	if err != nil {
		t.Fatalf("Handle failed: %v", err)
	}
}

func TestDedupWorkerHandle_InvalidPayload(t *testing.T) {
	worker := &dedupWorker{}

	ctx := context.Background()
	event := eventbus.DomainEvent{
		Type:    eventbus.EventEntityUpserted,
		Payload: &eventbus.EpisodicCreatedPayload{}, // wrong payload type
	}

	err := worker.Handle(ctx, event)
	if err == nil {
		t.Fatal("Expected error for invalid payload type")
	}
}

func TestDedupWorkerHandle_DedupError(t *testing.T) {
	mockKG := &mockKGStore{
		dedupErr: errors.New("dedup failed"),
	}

	worker := &dedupWorker{
		kgStore: mockKG,
	}

	ctx := context.Background()
	event := eventbus.DomainEvent{
		Type:    eventbus.EventEntityUpserted,
		Payload: &eventbus.EntityUpsertedPayload{EntityIDs: []string{"e1"}},
	}

	// Should not propagate error (non-fatal)
	err := worker.Handle(ctx, event)
	if err != nil {
		t.Fatalf("Handle should not propagate dedup error: %v", err)
	}
}

// Test dreaming worker

func TestDreamingWorkerHandle_BelowThreshold(t *testing.T) {
	mockEpisodic := &mockEpisodicStore{countResult: 2} // below default threshold of 5

	worker := &dreamingWorker{
		episodicStore: mockEpisodic,
		threshold:     5,
	}

	ctx := context.Background()
	event := eventbus.DomainEvent{
		Type:     eventbus.EventEpisodicCreated,
		AgentID:  "agent-123",
		UserID:   "user-123",
		Payload: &eventbus.EpisodicCreatedPayload{},
	}

	err := worker.Handle(ctx, event)
	if err != nil {
		t.Fatalf("Handle failed: %v", err)
	}

	// Should not have synthesized (below threshold)
	if len(mockEpisodic.promoted) > 0 {
		t.Errorf("Expected no promotions below threshold, got %d", len(mockEpisodic.promoted))
	}
}

func TestDreamingWorkerHandle_MeetsThreshold(t *testing.T) {
	summaries := []store.EpisodicSummary{
		{ID: uuid.New(), Summary: "Session 1 summary"},
		{ID: uuid.New(), Summary: "Session 2 summary"},
		{ID: uuid.New(), Summary: "Session 3 summary"},
		{ID: uuid.New(), Summary: "Session 4 summary"},
		{ID: uuid.New(), Summary: "Session 5 summary"},
	}

	mockEpisodic := &mockEpisodicStore{
		countResult: 5,
		unpromoted:  summaries,
		promoted:    make(map[string]bool),
	}
	mockMemory := newMockMemoryStore()
	mockProvider := &mockProvider{
		chatResp: &providers.ChatResponse{
			Content: "Consolidated insights",
		},
	}

	worker := &dreamingWorker{
		episodicStore: mockEpisodic,
		memoryStore:   mockMemory,
		provider:      mockProvider,
		model:         "test-model",
		threshold:     5,
		debounce:      1 * time.Second,
	}

	ctx := context.Background()
	event := eventbus.DomainEvent{
		Type:     eventbus.EventEpisodicCreated,
		TenantID: uuid.New().String(),
		AgentID:  "agent-123",
		UserID:   "user-123",
		Payload:  &eventbus.EpisodicCreatedPayload{},
	}

	err := worker.Handle(ctx, event)
	if err != nil {
		t.Fatalf("Handle failed: %v", err)
	}

	// Should have promoted entries
	if len(mockEpisodic.promoted) != 5 {
		t.Errorf("Expected 5 promoted entries, got %d", len(mockEpisodic.promoted))
	}

	// Should have stored consolidated document
	if len(mockMemory.docs) == 0 {
		t.Error("Expected consolidated document to be stored")
	}

	if len(mockMemory.indexed) == 0 {
		t.Error("Expected consolidated document to be indexed")
	}
}

func TestDreamingWorkerHandle_DebounceSkip(t *testing.T) {
	summaries := []store.EpisodicSummary{
		{ID: uuid.New(), Summary: "Session 1"},
		{ID: uuid.New(), Summary: "Session 2"},
		{ID: uuid.New(), Summary: "Session 3"},
		{ID: uuid.New(), Summary: "Session 4"},
		{ID: uuid.New(), Summary: "Session 5"},
	}

	mockEpisodic := &mockEpisodicStore{
		countResult: 5,
		unpromoted:  summaries,
		promoted:    make(map[string]bool),
	}
	mockMemory := newMockMemoryStore()
	mockProvider := &mockProvider{
		chatResp: &providers.ChatResponse{Content: "Consolidated"},
	}

	worker := &dreamingWorker{
		episodicStore: mockEpisodic,
		memoryStore:   mockMemory,
		provider:      mockProvider,
		model:         "test-model",
		threshold:     5,
		debounce:      10 * time.Second,
	}

	ctx := context.Background()

	// First run should succeed
	event1 := eventbus.DomainEvent{
		Type:     eventbus.EventEpisodicCreated,
		TenantID: uuid.New().String(),
		AgentID:  "agent-123",
		UserID:   "user-123",
		Payload: &eventbus.EpisodicCreatedPayload{
			Summary: "summary1",
		},
	}

	err := worker.Handle(ctx, event1)
	if err != nil {
		t.Fatalf("First Handle failed: %v", err)
	}

	firstPromotedCount := len(mockEpisodic.promoted)
	if firstPromotedCount == 0 {
		t.Error("Expected first run to have promoted entries")
	}

	// Second run immediately after should skip (debounced)
	mockEpisodic.promoted = make(map[string]bool) // reset for second run
	event2 := eventbus.DomainEvent{
		Type:     eventbus.EventEpisodicCreated,
		TenantID: uuid.New().String(),
		AgentID:  "agent-123",
		UserID:   "user-123",
		Payload: &eventbus.EpisodicCreatedPayload{
			Summary: "summary2",
		},
	}

	err = worker.Handle(ctx, event2)
	if err != nil {
		t.Fatalf("Second Handle failed: %v", err)
	}

	// Second run should have been debounced (no new promotions)
	if len(mockEpisodic.promoted) > 0 {
		t.Errorf("Expected debounced skip, but promotions were made: %d", len(mockEpisodic.promoted))
	}
}

// Test Register function

func TestRegister_WiresAllWorkers(t *testing.T) {
	mockEpisodic := &mockEpisodicStore{existsByID: make(map[string]bool)}
	mockKG := &mockKGStore{}
	mockMemory := newMockMemoryStore()
	mockSession := &mockSessionStore{}
	mockEventBus := newMockDomainEventBus()
	mockProvider := &mockProvider{
		chatResp: &providers.ChatResponse{Content: "test"},
	}
	mockExtractor := &mockExtractor{
		result: &knowledgegraph.ExtractionResult{
			Entities:  []store.Entity{},
			Relations: []store.Relation{},
		},
	}

	deps := ConsolidationDeps{
		EpisodicStore: mockEpisodic,
		MemoryStore:   mockMemory,
		KGStore:       mockKG,
		SessionStore:  mockSession,
		EventBus:      mockEventBus,
		Provider:      mockProvider,
		Model:         "test-model",
		Extractor:     mockExtractor,
	}

	cleanup := Register(deps)
	if cleanup == nil {
		t.Fatal("Register should return a cleanup function")
	}

	// Verify event handlers were registered
	if len(mockEventBus.handlers) == 0 {
		t.Error("Expected event handlers to be registered")
	}

	cleanup()
}
