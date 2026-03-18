// Package scheduler provides lane-based concurrency control and
// per-session message queuing for the GoClaw gateway.
//
// Lanes are named worker pools with configurable concurrency limits.
// Each lane processes requests independently, and the scheduler routes
// incoming requests to the appropriate lane based on configuration.
//
// Session serialization ensures only one agent run executes at a time
// per session key, preventing race conditions on session state.
package scheduler

import (
	"context"
	"log/slog"
	"os"
	"strconv"
	"sync"
	"sync/atomic"
)

// Lane name constants.
const (
	LaneMain     = "main"
	LaneSubagent = "subagent"
	LaneTeam     = "team"
	LaneCron     = "cron"
)

// LaneConfig configures a single lane.
type LaneConfig struct {
	Name        string `json:"name"`
	Concurrency int    `json:"concurrency"`
}

// Lane is a named worker pool with bounded concurrency.
// Requests submitted to a lane execute concurrently up to the
// configured limit; excess requests wait in a buffered channel.
type Lane struct {
	name        string
	concurrency int
	sem         chan struct{} // semaphore tokens
	pending     atomic.Int64  // pending requests count
	active      atomic.Int64  // active (running) requests count
	ctx         context.Context
	cancel      context.CancelFunc
	wg          sync.WaitGroup
}

// NewLane creates a lane with the given concurrency limit.
func NewLane(name string, concurrency int) *Lane {
	if concurrency <= 0 {
		concurrency = 2
	}

	ctx, cancel := context.WithCancel(context.Background())

	l := &Lane{
		name:        name,
		concurrency: concurrency,
		sem:         make(chan struct{}, concurrency),
		ctx:         ctx,
		cancel:      cancel,
	}

	// Pre-fill semaphore tokens
	for i := 0; i < concurrency; i++ {
		l.sem <- struct{}{}
	}

	return l
}

// Submit runs fn in the lane, blocking until a worker slot is available
// or ctx is cancelled. Returns immediately if the lane is shut down.
func (l *Lane) Submit(ctx context.Context, fn func()) error {
	l.pending.Add(1)
	defer l.pending.Add(-1)

	// Wait for a semaphore token or cancellation
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-l.ctx.Done():
		return context.Canceled
	case token, ok := <-l.sem:
		if !ok {
			return context.Canceled
		}

		l.active.Add(1)
		l.wg.Add(1)

		go func() {
			defer func() {
				l.active.Add(-1)
				l.wg.Done()
				l.sem <- token // return token
			}()
			fn()
		}()

		return nil
	}
}

// Stop drains the lane and waits for active work to complete.
func (l *Lane) Stop() {
	l.cancel()
	l.wg.Wait()
}

// Stats returns lane utilization metrics.
func (l *Lane) Stats() LaneStats {
	return LaneStats{
		Name:        l.name,
		Concurrency: l.concurrency,
		Active:      int(l.active.Load()),
		Pending:     int(l.pending.Load()),
	}
}

// LaneStats is a snapshot of lane utilization.
type LaneStats struct {
	Name        string `json:"name"`
	Concurrency int    `json:"concurrency"`
	Active      int    `json:"active"`
	Pending     int    `json:"pending"`
}

// LaneManager manages named lanes.
type LaneManager struct {
	lanes map[string]*Lane
	mu    sync.RWMutex
}

// NewLaneManager creates a lane manager with preconfigured lanes.
func NewLaneManager(configs []LaneConfig) *LaneManager {
	lm := &LaneManager{
		lanes: make(map[string]*Lane),
	}

	for _, cfg := range configs {
		lm.lanes[cfg.Name] = NewLane(cfg.Name, cfg.Concurrency)
		slog.Info("lane created", "name", cfg.Name, "concurrency", cfg.Concurrency)
	}

	return lm
}

// DefaultLanes returns the standard lane configuration.
// Concurrency defaults can be overridden via env vars:
//
//	GOCLAW_LANE_MAIN=30
//	GOCLAW_LANE_SUBAGENT=50
//	GOCLAW_LANE_TEAM=100
//	GOCLAW_LANE_CRON=30
func DefaultLanes() []LaneConfig {
	return []LaneConfig{
		{Name: LaneMain, Concurrency: laneEnv("GOCLAW_LANE_MAIN", 30)},
		{Name: LaneSubagent, Concurrency: laneEnv("GOCLAW_LANE_SUBAGENT", 50)},
		{Name: LaneTeam, Concurrency: laneEnvFallback("GOCLAW_LANE_TEAM", "GOCLAW_LANE_DELEGATE", 100)},
		{Name: LaneCron, Concurrency: laneEnv("GOCLAW_LANE_CRON", 30)},
	}
}

// laneEnv reads an int from an env var, falling back to defaultVal.
func laneEnv(key string, defaultVal int) int {
	if v := os.Getenv(key); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			return n
		}
	}
	return defaultVal
}

// laneEnvFallback reads an int from primary env var, then fallback env var, then defaultVal.
func laneEnvFallback(primary, fallback string, defaultVal int) int {
	if v := os.Getenv(primary); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			return n
		}
	}
	return laneEnv(fallback, defaultVal)
}

// Get returns a lane by name. Returns the "main" lane as fallback.
func (lm *LaneManager) Get(name string) *Lane {
	lm.mu.RLock()
	defer lm.mu.RUnlock()

	if lane, ok := lm.lanes[name]; ok {
		return lane
	}

	// Fallback to main lane
	if lane, ok := lm.lanes[LaneMain]; ok {
		return lane
	}

	return nil
}

// GetOrCreate returns an existing lane or creates one with default concurrency.
func (lm *LaneManager) GetOrCreate(name string, concurrency int) *Lane {
	lm.mu.Lock()
	defer lm.mu.Unlock()

	if lane, ok := lm.lanes[name]; ok {
		return lane
	}

	lane := NewLane(name, concurrency)
	lm.lanes[name] = lane
	slog.Info("lane created on demand", "name", name, "concurrency", concurrency)
	return lane
}

// StopAll stops all lanes and waits for active work.
func (lm *LaneManager) StopAll() {
	lm.mu.RLock()
	defer lm.mu.RUnlock()

	for name, lane := range lm.lanes {
		slog.Info("stopping lane", "name", name)
		lane.Stop()
	}
}

// AllStats returns utilization for all lanes.
func (lm *LaneManager) AllStats() []LaneStats {
	lm.mu.RLock()
	defer lm.mu.RUnlock()

	stats := make([]LaneStats, 0, len(lm.lanes))
	for _, lane := range lm.lanes {
		stats = append(stats, lane.Stats())
	}
	return stats
}
