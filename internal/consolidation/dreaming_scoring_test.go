package consolidation

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/nextlevelbuilder/goclaw/internal/eventbus"
	"github.com/nextlevelbuilder/goclaw/internal/providers"
	"github.com/nextlevelbuilder/goclaw/internal/store"
)

// TestDreamingWorkerUsesScoredListing verifies the worker fetches via
// ListUnpromotedScored (not the legacy created_at ASC path) so recall-hot
// entries synthesise first.
func TestDreamingWorkerUsesScoredListing(t *testing.T) {
	// Two unpromoted entries: one cold-but-old, one hot-recent.
	now := time.Now().UTC()
	lastRecall := now.Add(-1 * time.Hour)
	summaries := []store.EpisodicSummary{
		{ID: uuid.New(), Summary: "hot", RecallCount: 5, RecallScore: 0.9, LastRecalledAt: &lastRecall, CreatedAt: now.Add(-2 * 24 * time.Hour)},
		{ID: uuid.New(), Summary: "cold", CreatedAt: now.Add(-5 * 24 * time.Hour)},
	}
	// Custom scored-mock flagging whether the correct listing method fired.
	mockEp := &scoredEpisodicMock{
		mockEpisodicStore: mockEpisodicStore{
			countResult: 5,
			unpromoted:  summaries,
			promoted:    make(map[string]bool),
		},
	}
	mockMem := newMockMemoryStore()
	mockProv := &mockProvider{chatResp: &providers.ChatResponse{Content: "consolidated"}}

	worker := &dreamingWorker{
		episodicStore: mockEp,
		memoryStore:   mockMem,
		provider:      mockProv,
		model:         "test",
		threshold:     5,
		debounce:      1 * time.Second,
	}

	err := worker.Handle(context.Background(), eventbus.DomainEvent{
		Type:     eventbus.EventEpisodicCreated,
		TenantID: uuid.New().String(),
		AgentID:  "agent-scored",
		UserID:   "user-scored",
		Payload:  &eventbus.EpisodicCreatedPayload{},
	})
	if err != nil {
		t.Fatalf("Handle failed: %v", err)
	}
	if !mockEp.scoredCalled {
		t.Errorf("worker did not call ListUnpromotedScored (scoring bypass)")
	}
	if mockEp.legacyCalled {
		t.Errorf("worker called legacy ListUnpromoted — should use scored path")
	}
	if len(mockEp.promoted) != 2 {
		t.Errorf("expected 2 entries promoted, got %d", len(mockEp.promoted))
	}
}

// TestDreamingWorkerFiltersBelowThreshold verifies that even when the agent
// has enough unpromoted entries to pass the count check, the recall-score
// filter can still skip synthesis if every entry is too weak.
func TestDreamingWorkerFiltersBelowThreshold(t *testing.T) {
	now := time.Now().UTC()
	// All entries recalled once (below MinRecallCount=2) with trivial score.
	summaries := make([]store.EpisodicSummary, 5)
	for i := range summaries {
		summaries[i] = store.EpisodicSummary{
			ID:          uuid.New(),
			Summary:     "weak",
			CreatedAt:   now.Add(-30 * 24 * time.Hour),
			RecallCount: 1,
			RecallScore: 0.1,
		}
	}
	mockEp := &scoredEpisodicMock{
		mockEpisodicStore: mockEpisodicStore{
			countResult: 5,
			unpromoted:  summaries,
			promoted:    make(map[string]bool),
		},
	}
	mockMem := newMockMemoryStore()
	mockProv := &mockProvider{chatResp: &providers.ChatResponse{Content: "noop"}}

	worker := &dreamingWorker{
		episodicStore: mockEp,
		memoryStore:   mockMem,
		provider:      mockProv,
		model:         "test",
		threshold:     5,
		debounce:      1 * time.Second,
	}

	err := worker.Handle(context.Background(), eventbus.DomainEvent{
		Type:     eventbus.EventEpisodicCreated,
		TenantID: uuid.New().String(),
		AgentID:  "agent-filter",
		UserID:   "user-filter",
		Payload:  &eventbus.EpisodicCreatedPayload{},
	})
	if err != nil {
		t.Fatalf("Handle failed: %v", err)
	}
	if len(mockEp.promoted) > 0 {
		t.Errorf("expected all entries filtered, got %d promoted", len(mockEp.promoted))
	}
	if len(mockMem.docs) > 0 {
		t.Errorf("expected no synthesis document, got %d", len(mockMem.docs))
	}
}

// TestDreamingWorkerFilterEmptyStampsDebounce guards against the starvation
// loop: when all entries fail the recall threshold filter we still bump
// lastRun, otherwise every subsequent episodic.created event re-runs the
// full scoring pipeline in a tight loop. See code review note P10.1.
func TestDreamingWorkerFilterEmptyStampsDebounce(t *testing.T) {
	now := time.Now().UTC()
	summaries := make([]store.EpisodicSummary, 5)
	for i := range summaries {
		summaries[i] = store.EpisodicSummary{
			ID:          uuid.New(),
			Summary:     "weak",
			CreatedAt:   now.Add(-30 * 24 * time.Hour),
			RecallCount: 1, // below MinRecallCount=2 → filtered
			RecallScore: 0.05,
		}
	}
	mockEp := &scoredEpisodicMock{
		mockEpisodicStore: mockEpisodicStore{
			countResult: 5,
			unpromoted:  summaries,
			promoted:    make(map[string]bool),
		},
	}
	worker := &dreamingWorker{
		episodicStore: mockEp,
		memoryStore:   newMockMemoryStore(),
		provider:      &mockProvider{chatResp: &providers.ChatResponse{Content: "noop"}},
		model:         "test",
		threshold:     5,
		debounce:      10 * time.Minute, // realistic
	}

	ev := eventbus.DomainEvent{
		Type:     eventbus.EventEpisodicCreated,
		TenantID: uuid.New().String(),
		AgentID:  "agent-loop",
		UserID:   "user-loop",
		Payload:  &eventbus.EpisodicCreatedPayload{},
	}

	// First event: filter drops everything → no promotions.
	if err := worker.Handle(context.Background(), ev); err != nil {
		t.Fatalf("first Handle: %v", err)
	}
	firstCountCalls := mockEp.countCalls

	// Second event immediately after: debounce should skip BEFORE CountUnpromoted.
	if err := worker.Handle(context.Background(), ev); err != nil {
		t.Fatalf("second Handle: %v", err)
	}
	if mockEp.countCalls != firstCountCalls {
		t.Errorf("debounce not stamped on filter-empty skip — CountUnpromoted called %d times, want %d",
			mockEp.countCalls, firstCountCalls)
	}
}

// scoredEpisodicMock wraps mockEpisodicStore with tracking flags for the
// two ListUnpromoted* methods so tests can assert which path the worker used.
type scoredEpisodicMock struct {
	mockEpisodicStore
	scoredCalled bool
	legacyCalled bool
}

func (m *scoredEpisodicMock) ListUnpromoted(ctx context.Context, agentID, userID string, limit int) ([]store.EpisodicSummary, error) {
	m.legacyCalled = true
	return m.mockEpisodicStore.ListUnpromoted(ctx, agentID, userID, limit)
}

func (m *scoredEpisodicMock) ListUnpromotedScored(ctx context.Context, agentID, userID string, limit int) ([]store.EpisodicSummary, error) {
	m.scoredCalled = true
	return m.mockEpisodicStore.ListUnpromotedScored(ctx, agentID, userID, limit)
}
