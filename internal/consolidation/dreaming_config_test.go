package consolidation

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/nextlevelbuilder/goclaw/internal/config"
	"github.com/nextlevelbuilder/goclaw/internal/eventbus"
	"github.com/nextlevelbuilder/goclaw/internal/providers"
	"github.com/nextlevelbuilder/goclaw/internal/store"
)

func boolPtr(b bool) *bool { return &b }

func TestMergeDreamingConfigNilOverrideReturnsBase(t *testing.T) {
	base := defaultDreamingConfig()
	got := mergeDreamingConfig(base, nil)
	if got != base {
		t.Fatalf("nil override mutated base: got %+v, want %+v", got, base)
	}
}

func TestMergeDreamingConfigPartialOverride(t *testing.T) {
	base := defaultDreamingConfig()
	override := &config.DreamingConfig{
		Threshold:  3,
		DebounceMs: 120_000, // 2 min
	}
	got := mergeDreamingConfig(base, override)

	if got.Threshold != 3 {
		t.Errorf("Threshold = %d, want 3", got.Threshold)
	}
	if got.Debounce != 2*time.Minute {
		t.Errorf("Debounce = %v, want 2m", got.Debounce)
	}
	// Enabled not set in override → stays at default.
	if !got.Enabled {
		t.Errorf("Enabled = false, want default true")
	}
}

func TestMergeDreamingConfigDisable(t *testing.T) {
	base := defaultDreamingConfig()
	override := &config.DreamingConfig{Enabled: boolPtr(false)}
	got := mergeDreamingConfig(base, override)
	if got.Enabled {
		t.Errorf("Enabled = true, want false (override)")
	}
}

// TestMergeDreamingConfigVerboseLogNilPreservesDefault guards against the
// footgun where an override without VerboseLog silently reverts the base
// value. Pointer semantics require nil-check, not unconditional assignment.
func TestMergeDreamingConfigVerboseLogNilPreservesDefault(t *testing.T) {
	base := defaultDreamingConfig()
	base.VerboseLog = true // simulate an operator default of true
	override := &config.DreamingConfig{Threshold: 2}
	got := mergeDreamingConfig(base, override)
	if !got.VerboseLog {
		t.Errorf("VerboseLog = false, want true (nil override should preserve base)")
	}
}

func TestMergeDreamingConfigVerboseLogExplicitFalse(t *testing.T) {
	base := defaultDreamingConfig()
	base.VerboseLog = true
	override := &config.DreamingConfig{VerboseLog: boolPtr(false)}
	got := mergeDreamingConfig(base, override)
	if got.VerboseLog {
		t.Errorf("VerboseLog = true, want false (explicit override must apply)")
	}
}

func TestMergeDreamingConfigZeroFieldsIgnored(t *testing.T) {
	// Zero values on non-pointer fields must not clobber defaults — this is
	// critical for agents with an empty memory_config.dreaming JSONB object.
	base := defaultDreamingConfig()
	override := &config.DreamingConfig{} // all zero
	got := mergeDreamingConfig(base, override)
	if got.Threshold != base.Threshold {
		t.Errorf("Threshold = %d, want base %d", got.Threshold, base.Threshold)
	}
	if got.Debounce != base.Debounce {
		t.Errorf("Debounce = %v, want base %v", got.Debounce, base.Debounce)
	}
}

// TestDreamingWorkerHandleHonoursCustomThreshold verifies that a per-agent
// resolver lowering the threshold fires even when the worker's global default
// would have skipped the event.
func TestDreamingWorkerHandleHonoursCustomThreshold(t *testing.T) {
	summaries := []store.EpisodicSummary{
		{ID: uuid.New(), Summary: "s1"},
		{ID: uuid.New(), Summary: "s2"},
	}
	mockEpisodic := &mockEpisodicStore{
		countResult: 2,
		unpromoted:  summaries,
		promoted:    make(map[string]bool),
	}
	mockMemory := newMockMemoryStore()
	mockProvider := &mockProvider{
		chatResp: &providers.ChatResponse{Content: "ok"},
	}

	worker := &dreamingWorker{
		episodicStore: mockEpisodic,
		memoryStore:   mockMemory,
		registry:      testRegistry(mockProvider),
		threshold:     5, // global default says skip at count=2
		debounce:      1 * time.Second,
		resolveConfig: func(_ context.Context, _ string) *config.DreamingConfig {
			return &config.DreamingConfig{Threshold: 2} // per-agent override lowers it
		},
	}

	err := worker.Handle(context.Background(), eventbus.DomainEvent{
		Type:     eventbus.EventEpisodicCreated,
		TenantID: uuid.New().String(),
		AgentID:  "agent-123",
		UserID:   "user-123",
		Payload:  &eventbus.EpisodicCreatedPayload{},
	})
	if err != nil {
		t.Fatalf("Handle failed: %v", err)
	}
	if len(mockEpisodic.promoted) != 2 {
		t.Errorf("Expected 2 promotions with override threshold, got %d", len(mockEpisodic.promoted))
	}
}

// TestDreamingWorkerHandleDisabledSkips verifies that Enabled=false prevents
// any store activity, regardless of threshold/debounce.
func TestDreamingWorkerHandleDisabledSkips(t *testing.T) {
	mockEpisodic := &mockEpisodicStore{
		countResult: 100, // would normally trigger
		promoted:    make(map[string]bool),
	}

	worker := &dreamingWorker{
		episodicStore: mockEpisodic,
		threshold:     5,
		debounce:      1 * time.Second,
		resolveConfig: func(_ context.Context, _ string) *config.DreamingConfig {
			return &config.DreamingConfig{Enabled: boolPtr(false)}
		},
	}

	err := worker.Handle(context.Background(), eventbus.DomainEvent{
		Type:     eventbus.EventEpisodicCreated,
		TenantID: uuid.New().String(),
		AgentID:  "agent-disabled",
		UserID:   "user-123",
		Payload:  &eventbus.EpisodicCreatedPayload{},
	})
	if err != nil {
		t.Fatalf("Handle failed: %v", err)
	}
	if len(mockEpisodic.promoted) > 0 {
		t.Errorf("Expected 0 promotions when disabled, got %d", len(mockEpisodic.promoted))
	}
	// CountUnpromoted must also be skipped — it's a downstream read that
	// dreaming workers should not perform when an agent opts out.
	if mockEpisodic.countCalls > 0 {
		t.Errorf("Expected 0 CountUnpromoted calls when disabled, got %d", mockEpisodic.countCalls)
	}
}

// TestDreamingWorkerHandleNilResolverUsesDefaults verifies backward
// compatibility — no resolver means the worker behaves exactly as before.
func TestDreamingWorkerHandleNilResolverUsesDefaults(t *testing.T) {
	mockEpisodic := &mockEpisodicStore{countResult: 2} // below default 5
	worker := &dreamingWorker{
		episodicStore: mockEpisodic,
		threshold:     5,
		debounce:      1 * time.Second,
		// resolveConfig: nil
	}
	err := worker.Handle(context.Background(), eventbus.DomainEvent{
		Type:     eventbus.EventEpisodicCreated,
		TenantID: uuid.New().String(),
		AgentID:  "agent-123",
		UserID:   "user-123",
		Payload:  &eventbus.EpisodicCreatedPayload{},
	})
	if err != nil {
		t.Fatalf("Handle failed: %v", err)
	}
	// Below threshold — no promotions expected (default behaviour preserved).
	if len(mockEpisodic.promoted) > 0 {
		t.Errorf("Expected no promotions below default threshold, got %d", len(mockEpisodic.promoted))
	}
}
