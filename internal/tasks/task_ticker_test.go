package tasks

import (
	"context"
	"encoding/json"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/nextlevelbuilder/goclaw/internal/bus"
	"github.com/nextlevelbuilder/goclaw/internal/store"
	"github.com/nextlevelbuilder/goclaw/internal/tools"
)

// ─── minimal stub stores ───────────────────────────────────────────────────

// getTeamCall records ctx metadata for each GetTeam call (tenant propagation assertions).
type getTeamCall struct {
	TeamID   uuid.UUID
	TenantID uuid.UUID
}

// stubTeamStore satisfies store.TeamStore by embedding the interface and only
// overriding the methods actually exercised by TaskTicker.
type stubTeamStore struct {
	store.TeamStore

	mu sync.Mutex

	forceRecoverCalls atomic.Int32
	recoverCalls      atomic.Int32
	staleCalls        atomic.Int32
	inReviewCalls     atomic.Int32
	orphanCalls       atomic.Int32
	followupListCalls atomic.Int32
	incrementCalls    atomic.Int32

	forceRecoverErr error
	recoverErr      error
	staleErr        error

	recovered     []store.RecoveredTaskInfo
	stale         []store.RecoveredTaskInfo
	inReview      []store.RecoveredTaskInfo
	orphans       []store.RecoveredTaskInfo
	followupTasks []store.TeamTaskData

	// function-based dispatch — nil means default behavior (return error "not found")
	getTeamFunc func(ctx context.Context, id uuid.UUID) (*store.TeamData, error)
	getTaskFunc func(ctx context.Context, id uuid.UUID) (*store.TeamTaskData, error)

	// captured calls for tenant propagation assertions (protected by mu)
	getTeamCalls []getTeamCall
}

func (s *stubTeamStore) ForceRecoverAllTasks(_ context.Context) ([]store.RecoveredTaskInfo, error) {
	s.forceRecoverCalls.Add(1)
	return s.recovered, s.forceRecoverErr
}

func (s *stubTeamStore) RecoverAllStaleTasks(_ context.Context) ([]store.RecoveredTaskInfo, error) {
	s.recoverCalls.Add(1)
	return s.recovered, s.recoverErr
}

func (s *stubTeamStore) MarkAllStaleTasks(_ context.Context, _ time.Time) ([]store.RecoveredTaskInfo, error) {
	s.staleCalls.Add(1)
	return s.stale, s.staleErr
}

func (s *stubTeamStore) MarkInReviewStaleTasks(_ context.Context, _ time.Time) ([]store.RecoveredTaskInfo, error) {
	s.inReviewCalls.Add(1)
	return s.inReview, nil
}

func (s *stubTeamStore) FixOrphanedBlockedTasks(_ context.Context) ([]store.RecoveredTaskInfo, error) {
	s.orphanCalls.Add(1)
	return s.orphans, nil
}

func (s *stubTeamStore) ListAllFollowupDueTasks(_ context.Context) ([]store.TeamTaskData, error) {
	s.followupListCalls.Add(1)
	return s.followupTasks, nil
}

func (s *stubTeamStore) IncrementFollowupCount(_ context.Context, _ uuid.UUID, _ *time.Time) error {
	s.incrementCalls.Add(1)
	return nil
}

func (s *stubTeamStore) GetTeam(ctx context.Context, id uuid.UUID) (*store.TeamData, error) {
	s.mu.Lock()
	s.getTeamCalls = append(s.getTeamCalls, getTeamCall{
		TeamID:   id,
		TenantID: store.TenantIDFromContext(ctx),
	})
	s.mu.Unlock()
	if s.getTeamFunc != nil {
		return s.getTeamFunc(ctx, id)
	}
	return nil, errors.New("not found")
}

func (s *stubTeamStore) GetTask(ctx context.Context, id uuid.UUID) (*store.TeamTaskData, error) {
	if s.getTaskFunc != nil {
		return s.getTaskFunc(ctx, id)
	}
	return nil, errors.New("not found")
}

// stubAgentStore satisfies store.AgentStore — unused by most ticker logic.
// GetByID is overridable via getByIDFunc for tests exercising the lead-agent lookup path.
type stubAgentStore struct {
	store.AgentStore
	getByIDFunc func(ctx context.Context, id uuid.UUID) (*store.AgentData, error)
}

func (s *stubAgentStore) GetByID(ctx context.Context, id uuid.UUID) (*store.AgentData, error) {
	if s.getByIDFunc != nil {
		return s.getByIDFunc(ctx, id)
	}
	return nil, errors.New("not found")
}

// ─── NewTaskTicker ─────────────────────────────────────────────────────────

func TestNewTaskTicker_DefaultInterval(t *testing.T) {
	ts := &stubTeamStore{}
	as := &stubAgentStore{}
	tt := NewTaskTicker(ts, as, nil, 0)
	if tt.interval != defaultRecoveryInterval {
		t.Errorf("interval = %v, want %v", tt.interval, defaultRecoveryInterval)
	}
}

func TestNewTaskTicker_CustomInterval(t *testing.T) {
	ts := &stubTeamStore{}
	as := &stubAgentStore{}
	tt := NewTaskTicker(ts, as, nil, 7)
	if tt.interval != 7*time.Second {
		t.Errorf("interval = %v, want 7s", tt.interval)
	}
}

func TestNewTaskTicker_NegativeIntervalUsesDefault(t *testing.T) {
	ts := &stubTeamStore{}
	as := &stubAgentStore{}
	tt := NewTaskTicker(ts, as, nil, -1)
	if tt.interval != defaultRecoveryInterval {
		t.Errorf("interval = %v, want default", tt.interval)
	}
}

// ─── Start / Stop ──────────────────────────────────────────────────────────

func TestTaskTicker_StartStop(t *testing.T) {
	ts := &stubTeamStore{}
	as := &stubAgentStore{}
	// Use a large interval so periodic ticks don't fire.
	tt := NewTaskTicker(ts, as, nil, 3600)
	tt.Start()
	tt.Stop() // must not block

	// ForceRecover must have been called exactly once (startup recovery).
	if n := ts.forceRecoverCalls.Load(); n != 1 {
		t.Errorf("ForceRecoverAllTasks called %d times, want 1", n)
	}
}

func TestTaskTicker_StopIsIdempotentAndClean(t *testing.T) {
	ts := &stubTeamStore{}
	as := &stubAgentStore{}
	tt := NewTaskTicker(ts, as, nil, 3600)
	tt.Start()

	done := make(chan struct{})
	go func() {
		tt.Stop()
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("Stop() did not return within 2s")
	}
}

// ─── Periodic tick ─────────────────────────────────────────────────────────

func TestTaskTicker_PeriodicTick_CallsRecoverOnTick(t *testing.T) {
	ts := &stubTeamStore{}
	as := &stubAgentStore{}
	// Very short interval so we get at least one tick.
	tt := NewTaskTicker(ts, as, nil, 0)
	// Override interval to something tiny.
	tt.interval = 10 * time.Millisecond

	tt.Start()

	// Wait for at least one periodic tick (recoverCalls increments on tick).
	deadline := time.After(500 * time.Millisecond)
	for {
		if ts.recoverCalls.Load() >= 1 {
			break
		}
		select {
		case <-deadline:
			t.Fatalf("RecoverAllStaleTasks never called after 500ms (periodic tick missed)")
		default:
			time.Sleep(5 * time.Millisecond)
		}
	}

	tt.Stop()
}

// ─── recoverAll steps (unit-level, no goroutine) ──────────────────────────

func TestTaskTicker_RecoverAll_ForceRecoverCalled(t *testing.T) {
	ts := &stubTeamStore{}
	as := &stubAgentStore{}
	tt := NewTaskTicker(ts, as, nil, 3600)

	tt.recoverAll(true)

	if ts.forceRecoverCalls.Load() != 1 {
		t.Errorf("ForceRecoverAllTasks calls = %d, want 1", ts.forceRecoverCalls.Load())
	}
	if ts.recoverCalls.Load() != 0 {
		t.Errorf("RecoverAllStaleTasks unexpectedly called")
	}
}

func TestTaskTicker_RecoverAll_StaleCalled(t *testing.T) {
	ts := &stubTeamStore{}
	as := &stubAgentStore{}
	tt := NewTaskTicker(ts, as, nil, 3600)

	tt.recoverAll(false)

	if ts.staleCalls.Load() != 1 {
		t.Errorf("MarkAllStaleTasks calls = %d, want 1", ts.staleCalls.Load())
	}
	if ts.inReviewCalls.Load() != 1 {
		t.Errorf("MarkInReviewStaleTasks calls = %d, want 1", ts.inReviewCalls.Load())
	}
	if ts.orphanCalls.Load() != 1 {
		t.Errorf("FixOrphanedBlockedTasks calls = %d, want 1", ts.orphanCalls.Load())
	}
}

func TestTaskTicker_RecoverAll_ErrorsAreToleratedGracefully(t *testing.T) {
	ts := &stubTeamStore{
		forceRecoverErr: errors.New("db offline"),
		recoverErr:      errors.New("db offline"),
		staleErr:        errors.New("db offline"),
	}
	as := &stubAgentStore{}
	tt := NewTaskTicker(ts, as, nil, 3600)

	// Must not panic
	tt.recoverAll(true)
	tt.recoverAll(false)
}

// ─── followupInterval ──────────────────────────────────────────────────────

func TestFollowupInterval_DefaultWhenNoSettings(t *testing.T) {
	d := followupInterval(store.TeamData{})
	if d != defaultFollowupInterval {
		t.Errorf("got %v, want %v", d, defaultFollowupInterval)
	}
}

func TestFollowupInterval_CustomFromSettings(t *testing.T) {
	raw, _ := json.Marshal(map[string]any{"followup_interval_minutes": float64(10)})
	d := followupInterval(store.TeamData{Settings: raw})
	if d != 10*time.Minute {
		t.Errorf("got %v, want 10m", d)
	}
}

func TestFollowupInterval_ZeroValueUsesDefault(t *testing.T) {
	raw, _ := json.Marshal(map[string]any{"followup_interval_minutes": float64(0)})
	d := followupInterval(store.TeamData{Settings: raw})
	if d != defaultFollowupInterval {
		t.Errorf("got %v, want default", d)
	}
}

func TestFollowupInterval_InvalidJSONUsesDefault(t *testing.T) {
	d := followupInterval(store.TeamData{Settings: []byte("not-json")})
	if d != defaultFollowupInterval {
		t.Errorf("got %v, want default", d)
	}
}

// ─── pruneCooldowns ───────────────────────────────────────────────────────

func TestPruneCooldowns_RemovesExpiredEntries(t *testing.T) {
	ts := &stubTeamStore{}
	as := &stubAgentStore{}
	tt := NewTaskTicker(ts, as, nil, 3600)

	id1 := uuid.New()
	id2 := uuid.New()

	// id1 is old enough to be pruned (> 2*followupCooldown)
	tt.lastFollowupSent[id1] = time.Now().Add(-(2*followupCooldown + time.Second))
	// id2 is recent — should be kept
	tt.lastFollowupSent[id2] = time.Now()

	tt.pruneCooldowns()

	if _, ok := tt.lastFollowupSent[id1]; ok {
		t.Error("expected id1 to be pruned")
	}
	if _, ok := tt.lastFollowupSent[id2]; !ok {
		t.Error("expected id2 to be kept")
	}
}

func TestPruneCooldowns_EmptyMapIsNoop(t *testing.T) {
	ts := &stubTeamStore{}
	as := &stubAgentStore{}
	tt := NewTaskTicker(ts, as, nil, 3600)
	tt.pruneCooldowns() // must not panic
}

// ─── processTeamFollowups ─────────────────────────────────────────────────

func TestProcessTeamFollowups_CooldownPreventsRepeat(t *testing.T) {
	ts := &stubTeamStore{}
	as := &stubAgentStore{}
	mb := bus.New()
	tt := NewTaskTicker(ts, as, mb, 3600)

	taskID := uuid.New()
	// Mark as sent recently.
	tt.lastFollowupSent[taskID] = time.Now()

	tasks := []store.TeamTaskData{
		{
			BaseModel:       store.BaseModel{ID: taskID},
			FollowupChannel: "telegram",
			FollowupChatID:  "chat-1",
			FollowupMessage: "any reminder",
		},
	}

	tt.processTeamFollowups(context.Background(), tasks, defaultFollowupInterval)

	// Bus should not receive an outbound message because of cooldown.
	select {
	case <-time.After(20 * time.Millisecond):
		// Good: nothing sent.
	}
	if ts.incrementCalls.Load() != 0 {
		t.Error("IncrementFollowupCount should not be called when on cooldown")
	}
}

func TestProcessTeamFollowups_SkipsEmptyChannel(t *testing.T) {
	ts := &stubTeamStore{}
	as := &stubAgentStore{}
	mb := bus.New()
	tt := NewTaskTicker(ts, as, mb, 3600)

	taskID := uuid.New()
	tasks := []store.TeamTaskData{
		{
			BaseModel:       store.BaseModel{ID: taskID},
			FollowupChannel: "", // empty — should skip
			FollowupChatID:  "chat-1",
			FollowupMessage: "any reminder",
		},
	}

	tt.processTeamFollowups(context.Background(), tasks, defaultFollowupInterval)

	if ts.incrementCalls.Load() != 0 {
		t.Error("IncrementFollowupCount should not be called when channel is empty")
	}
}

func TestProcessTeamFollowups_SendsReminderWhenEligible(t *testing.T) {
	ts := &stubTeamStore{}
	as := &stubAgentStore{}
	mb := bus.New()
	tt := NewTaskTicker(ts, as, mb, 3600)

	taskID := uuid.New()
	tasks := []store.TeamTaskData{
		{
			BaseModel:       store.BaseModel{ID: taskID},
			FollowupChannel: "telegram",
			FollowupChatID:  "chat-99",
			FollowupMessage: "please respond",
			FollowupCount:   0,
			FollowupMax:     3,
		},
	}

	tt.processTeamFollowups(context.Background(), tasks, defaultFollowupInterval)

	// Should have incremented followup count.
	if ts.incrementCalls.Load() != 1 {
		t.Errorf("IncrementFollowupCount calls = %d, want 1", ts.incrementCalls.Load())
	}

	// lastFollowupSent should be updated.
	tt.mu.Lock()
	_, exists := tt.lastFollowupSent[taskID]
	tt.mu.Unlock()
	if !exists {
		t.Error("lastFollowupSent not updated for task")
	}
}

func TestProcessTeamFollowups_MaxReached_NoNextAt(t *testing.T) {
	ts := &stubTeamStore{}
	as := &stubAgentStore{}
	mb := bus.New()
	tt := NewTaskTicker(ts, as, mb, 3600)

	taskID := uuid.New()
	tasks := []store.TeamTaskData{
		{
			BaseModel:       store.BaseModel{ID: taskID},
			FollowupChannel: "telegram",
			FollowupChatID:  "chat-99",
			FollowupMessage: "last reminder",
			FollowupCount:   2,
			FollowupMax:     3, // newCount = 3 = max → nextAt should be nil
		},
	}

	tt.processTeamFollowups(context.Background(), tasks, defaultFollowupInterval)

	// Increment should still be called (it receives nil nextAt when max reached).
	if ts.incrementCalls.Load() != 1 {
		t.Errorf("IncrementFollowupCount calls = %d, want 1", ts.incrementCalls.Load())
	}
}

// ─── notifyLeaders (nil msgBus is a no-op) ────────────────────────────────

func TestNotifyLeaders_NilMsgBusIsNoop(t *testing.T) {
	ts := &stubTeamStore{}
	as := &stubAgentStore{}
	tt := NewTaskTicker(ts, as, nil, 3600)

	// Must not panic even with tasks.
	tt.notifyLeaders(context.Background(), []store.RecoveredTaskInfo{
		{ID: uuid.New(), TeamID: uuid.New(), TenantID: uuid.New()},
	}, "recovered", "hint")
}

// ─── Race detector: concurrent Start/Stop ────────────────────────────────

func TestTaskTicker_ConcurrentStartStop_Race(t *testing.T) {
	ts := &stubTeamStore{}
	as := &stubAgentStore{}
	tt := NewTaskTicker(ts, as, nil, 3600)
	tt.Start()

	var wg sync.WaitGroup
	for i := 0; i < 5; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			// Access cooldown map concurrently via pruneCooldowns (uses lock).
			tt.pruneCooldowns()
		}()
	}
	wg.Wait()
	tt.Stop()
}

// ─── processFollowups with no tasks ──────────────────────────────────────

func TestProcessFollowups_NoTasksIsNoop(t *testing.T) {
	ts := &stubTeamStore{followupTasks: nil}
	as := &stubAgentStore{}
	tt := NewTaskTicker(ts, as, nil, 3600)

	tt.processFollowups(context.Background())

	if ts.incrementCalls.Load() != 0 {
		t.Errorf("IncrementFollowupCount calls = %d, want 0", ts.incrementCalls.Load())
	}
}

func TestFollowupOutboundMessage_UsesLocalKey(t *testing.T) {
	task := &store.TeamTaskData{
		FollowupChannel: "telegram",
		FollowupChatID:  "-100123456",
		Metadata: map[string]any{
			tools.TaskMetaLocalKey: "-100123456:topic:47",
		},
	}

	got := followupOutboundMessage(task, "Reminder (1): ping")
	if got.Metadata == nil {
		t.Fatal("expected metadata to be populated")
	}
	if got.Metadata["local_key"] != "-100123456:topic:47" {
		t.Fatalf("local_key = %q, want %q", got.Metadata["local_key"], "-100123456:topic:47")
	}
}

func TestFollowupOutboundMessage_OmitsLocalKeyWhenMissing(t *testing.T) {
	task := &store.TeamTaskData{
		FollowupChannel: "telegram",
		FollowupChatID:  "-100123456",
	}

	got := followupOutboundMessage(task, "Reminder (1): ping")
	if got.Metadata != nil {
		t.Fatalf("expected metadata to be nil, got %#v", got.Metadata)
	}
}

// ─── notifyLeaders: nil-team and tenant propagation ──────────────────────────

// TestNotifyLeaders_NilTeam_NoPanic verifies that when GetTeam returns (nil, nil)
// the scope is skipped gracefully — no panic, no publishes.
func TestNotifyLeaders_NilTeam_NoPanic(t *testing.T) {
	tenantID := uuid.New()
	teamID := uuid.New()

	ts := &stubTeamStore{
		getTeamFunc: func(_ context.Context, _ uuid.UUID) (*store.TeamData, error) {
			return nil, nil // simulate PGTeamStore returning nil without error
		},
	}
	as := &stubAgentStore{}
	mb := bus.New()
	tt := NewTaskTicker(ts, as, mb, 3600)

	// Must not panic.
	tt.notifyLeaders(context.Background(), []store.RecoveredTaskInfo{
		{ID: uuid.New(), TeamID: teamID, TenantID: tenantID},
	}, "recovered", "hint")

	ts.mu.Lock()
	callCount := len(ts.getTeamCalls)
	capturedTenant := uuid.Nil
	if callCount > 0 {
		capturedTenant = ts.getTeamCalls[0].TenantID
	}
	ts.mu.Unlock()

	if callCount != 1 {
		t.Errorf("GetTeam call count = %d, want 1", callCount)
	}
	if capturedTenant != tenantID {
		t.Errorf("GetTeam ctx TenantID = %v, want %v", capturedTenant, tenantID)
	}
}

// TestNotifyLeaders_MultiTenantCacheIsolation verifies that tasks from different tenants
// each trigger their own GetTeam call with the correct tenant in ctx.
func TestNotifyLeaders_MultiTenantCacheIsolation(t *testing.T) {
	tenantX := uuid.New()
	tenantY := uuid.New()
	teamT1 := uuid.New()
	teamT2 := uuid.New()
	leadID := uuid.New()

	ts := &stubTeamStore{
		getTeamFunc: func(_ context.Context, id uuid.UUID) (*store.TeamData, error) {
			return &store.TeamData{
				BaseModel:   store.BaseModel{ID: id},
				LeadAgentID: leadID,
			}, nil
		},
	}
	as := &stubAgentStore{
		// short-circuit before publish; return error so notifyLeaders skips publishing
		getByIDFunc: func(_ context.Context, _ uuid.UUID) (*store.AgentData, error) {
			return nil, errors.New("not found")
		},
	}
	mb := bus.New()
	tt := NewTaskTicker(ts, as, mb, 3600)

	tasks := []store.RecoveredTaskInfo{
		{ID: uuid.New(), TeamID: teamT1, TenantID: tenantX},
		{ID: uuid.New(), TeamID: teamT2, TenantID: tenantY},
	}
	tt.notifyLeaders(context.Background(), tasks, "recovered", "hint")

	ts.mu.Lock()
	calls := make([]getTeamCall, len(ts.getTeamCalls))
	copy(calls, ts.getTeamCalls)
	ts.mu.Unlock()

	if len(calls) != 2 {
		t.Fatalf("GetTeam call count = %d, want 2", len(calls))
	}

	// Build map: teamID → tenantID from captured calls.
	captured := map[uuid.UUID]uuid.UUID{}
	for _, c := range calls {
		captured[c.TeamID] = c.TenantID
	}

	if captured[teamT1] != tenantX {
		t.Errorf("team T1: ctx TenantID = %v, want %v", captured[teamT1], tenantX)
	}
	if captured[teamT2] != tenantY {
		t.Errorf("team T2: ctx TenantID = %v, want %v", captured[teamT2], tenantY)
	}
}

// TestNotifyLeaders_CachedOnSameTenant verifies that two scopes with the same team+tenant
// result in only one GetTeam call (cache hit on composite key).
func TestNotifyLeaders_CachedOnSameTenant(t *testing.T) {
	tenantID := uuid.New()
	teamID := uuid.New()
	leadID := uuid.New()

	ts := &stubTeamStore{
		getTeamFunc: func(_ context.Context, id uuid.UUID) (*store.TeamData, error) {
			return &store.TeamData{
				BaseModel:   store.BaseModel{ID: id},
				LeadAgentID: leadID,
			}, nil
		},
	}
	as := &stubAgentStore{
		getByIDFunc: func(_ context.Context, _ uuid.UUID) (*store.AgentData, error) {
			return nil, errors.New("not found")
		},
	}
	mb := bus.New()
	tt := NewTaskTicker(ts, as, mb, 3600)

	// Two tasks same team+tenant but different ChatID → two scopes, same cache key.
	tasks := []store.RecoveredTaskInfo{
		{ID: uuid.New(), TeamID: teamID, TenantID: tenantID, ChatID: "chat-A"},
		{ID: uuid.New(), TeamID: teamID, TenantID: tenantID, ChatID: "chat-B"},
	}
	tt.notifyLeaders(context.Background(), tasks, "recovered", "hint")

	ts.mu.Lock()
	callCount := len(ts.getTeamCalls)
	ts.mu.Unlock()

	if callCount != 1 {
		t.Errorf("GetTeam call count = %d, want 1 (cache hit)", callCount)
	}
}

// ─── processFollowups: multi-tenant tenant ctx and nil-team guard ─────────────

// TestProcessFollowups_MultiTenantTenantCtx verifies that tasks from two different
// tenants each trigger GetTeam with the correct tenant in ctx.
func TestProcessFollowups_MultiTenantTenantCtx(t *testing.T) {
	tenantX := uuid.New()
	tenantY := uuid.New()
	teamT1 := uuid.New()
	teamT2 := uuid.New()

	ts := &stubTeamStore{
		followupTasks: []store.TeamTaskData{
			{
				BaseModel:       store.BaseModel{ID: uuid.New()},
				TeamID:          teamT1,
				TenantID:        tenantX,
				FollowupChannel: "telegram",
				FollowupChatID:  "chat-1",
				FollowupMessage: "ping",
			},
			{
				BaseModel:       store.BaseModel{ID: uuid.New()},
				TeamID:          teamT2,
				TenantID:        tenantY,
				FollowupChannel: "telegram",
				FollowupChatID:  "chat-2",
				FollowupMessage: "ping",
			},
		},
		getTeamFunc: func(_ context.Context, id uuid.UUID) (*store.TeamData, error) {
			return &store.TeamData{BaseModel: store.BaseModel{ID: id}}, nil
		},
	}
	as := &stubAgentStore{}
	mb := bus.New()
	tt := NewTaskTicker(ts, as, mb, 3600)

	tt.processFollowups(context.Background())

	ts.mu.Lock()
	calls := make([]getTeamCall, len(ts.getTeamCalls))
	copy(calls, ts.getTeamCalls)
	ts.mu.Unlock()

	if len(calls) != 2 {
		t.Fatalf("GetTeam call count = %d, want 2", len(calls))
	}

	captured := map[uuid.UUID]uuid.UUID{}
	for _, c := range calls {
		captured[c.TeamID] = c.TenantID
	}

	if captured[teamT1] != tenantX {
		t.Errorf("team T1: ctx TenantID = %v, want %v", captured[teamT1], tenantX)
	}
	if captured[teamT2] != tenantY {
		t.Errorf("team T2: ctx TenantID = %v, want %v", captured[teamT2], tenantY)
	}
}

// TestProcessFollowups_NilTeam_NoPanic verifies that when GetTeam returns (nil, nil)
// processFollowups skips without panicking, and no followup is sent.
func TestProcessFollowups_NilTeam_NoPanic(t *testing.T) {
	tenantID := uuid.New()
	teamID := uuid.New()

	ts := &stubTeamStore{
		followupTasks: []store.TeamTaskData{
			{
				BaseModel:       store.BaseModel{ID: uuid.New()},
				TeamID:          teamID,
				TenantID:        tenantID,
				FollowupChannel: "telegram",
				FollowupChatID:  "chat-1",
				FollowupMessage: "ping",
			},
		},
		getTeamFunc: func(_ context.Context, _ uuid.UUID) (*store.TeamData, error) {
			return nil, nil // simulate nil without error
		},
	}
	as := &stubAgentStore{}
	mb := bus.New()
	tt := NewTaskTicker(ts, as, mb, 3600)

	// Must not panic.
	tt.processFollowups(context.Background())

	if ts.incrementCalls.Load() != 0 {
		t.Errorf("IncrementFollowupCount = %d, want 0 (nil team skipped)", ts.incrementCalls.Load())
	}
}
