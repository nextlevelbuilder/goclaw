package scheduler

import (
	"context"
	"log/slog"
	"strings"
	"sync"
	"sync/atomic"

	"github.com/nextlevelbuilder/goclaw/internal/agent"
)

// --- Scheduler ---

// ScheduleOpts provides per-request overrides for the scheduler.
type ScheduleOpts struct {
	MaxConcurrent int // per-session override (0 = use config default)
}

// Scheduler is the top-level coordinator that manages lanes and session queues.
type Scheduler struct {
	lanes           *LaneManager
	sessions        map[string]*SessionQueue
	config          QueueConfig
	runFn           RunFunc
	mu              sync.RWMutex
	draining        atomic.Bool       // set during graceful shutdown to reject new requests
	tokenEstimateFn TokenEstimateFunc // optional: for adaptive throttle
}

// NewScheduler creates a scheduler with the given lane and queue config.
func NewScheduler(laneConfigs []LaneConfig, queueCfg QueueConfig, runFn RunFunc) *Scheduler {
	if laneConfigs == nil {
		laneConfigs = DefaultLanes()
	}

	return &Scheduler{
		lanes:    NewLaneManager(laneConfigs),
		sessions: make(map[string]*SessionQueue),
		config:   queueCfg,
		runFn:    runFn,
	}
}

// SetTokenEstimateFunc sets the callback used by adaptive throttle.
// Must be called before any Schedule calls.
func (s *Scheduler) SetTokenEstimateFunc(fn TokenEstimateFunc) {
	s.tokenEstimateFn = fn
}

// MarkDraining signals that the gateway is shutting down.
// New Schedule/ScheduleWithOpts calls will return ErrGatewayDraining immediately.
// Active runs continue to completion.
func (s *Scheduler) MarkDraining() {
	s.draining.Store(true)
	slog.Info("scheduler: marked as draining, new requests will be rejected")
}

// Schedule submits a run request to the appropriate session queue and lane.
// Returns a channel that receives the result when the run completes.
func (s *Scheduler) Schedule(ctx context.Context, lane string, req agent.RunRequest) <-chan RunOutcome {
	if s.draining.Load() {
		ch := make(chan RunOutcome, 1)
		ch <- RunOutcome{Err: ErrGatewayDraining}
		close(ch)
		return ch
	}
	sq := s.getOrCreateSession(req.SessionKey, lane)
	return sq.Enqueue(ctx, req)
}

// ScheduleWithOpts submits a run request with per-session overrides.
func (s *Scheduler) ScheduleWithOpts(ctx context.Context, lane string, req agent.RunRequest, opts ScheduleOpts) <-chan RunOutcome {
	if s.draining.Load() {
		ch := make(chan RunOutcome, 1)
		ch <- RunOutcome{Err: ErrGatewayDraining}
		close(ch)
		return ch
	}
	sq := s.getOrCreateSession(req.SessionKey, lane)
	if opts.MaxConcurrent > 0 {
		sq.SetMaxConcurrent(opts.MaxConcurrent)
	}
	return sq.Enqueue(ctx, req)
}

// getOrCreateSession returns or creates a session queue for the given key.
func (s *Scheduler) getOrCreateSession(sessionKey, lane string) *SessionQueue {
	s.mu.RLock()
	sq, ok := s.sessions[sessionKey]
	s.mu.RUnlock()

	if ok {
		return sq
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	// Double-check after acquiring write lock
	if sq, ok := s.sessions[sessionKey]; ok {
		return sq
	}

	sq = NewSessionQueue(sessionKey, lane, s.config, s.lanes, s.runFn)
	if s.tokenEstimateFn != nil {
		sq.tokenEstimateFn = s.tokenEstimateFn
	}
	s.sessions[sessionKey] = sq

	slog.Debug("session queue created", "session", sessionKey, "lane", lane)
	return sq
}

// CancelSession cancels all active runs and drains pending queue for a session.
// Returns true if any active run was cancelled.
func (s *Scheduler) CancelSession(sessionKey string) bool {
	s.mu.RLock()
	sq, ok := s.sessions[sessionKey]
	s.mu.RUnlock()
	if !ok {
		return false
	}
	return sq.CancelAll()
}

// CancelOneSession cancels the oldest active run for a session.
// Does NOT drain the pending queue. Used by /stop command.
// Returns true if an active run was cancelled.
func (s *Scheduler) CancelOneSession(sessionKey string) bool {
	s.mu.RLock()
	sq, ok := s.sessions[sessionKey]
	s.mu.RUnlock()
	if !ok {
		return false
	}
	return sq.CancelOne()
}

// Stop shuts down all lanes and clears session queues.
// Automatically marks the scheduler as draining before stopping.
func (s *Scheduler) Stop() {
	s.MarkDraining()
	s.lanes.StopAll()
}

// HasActiveSessionsForAgent checks if any session queue for the given agent has active runs.
// Used by heartbeat ticker to skip agents currently processing other requests.
func (s *Scheduler) HasActiveSessionsForAgent(agentID string) bool {
	prefix := "agent:" + agentID + ":"
	s.mu.RLock()
	defer s.mu.RUnlock()
	for key, sq := range s.sessions {
		if strings.HasPrefix(key, prefix) && sq.IsActive() {
			return true
		}
	}
	return false
}

// LaneStats returns utilization metrics for all lanes.
func (s *Scheduler) LaneStats() []LaneStats {
	return s.lanes.AllStats()
}

// Lanes returns the underlying lane manager (for direct access if needed).
func (s *Scheduler) Lanes() *LaneManager {
	return s.lanes
}
