package security

import (
	"sync"
	"time"

	"github.com/google/uuid"
)

// rateLimitKey identifies a unique (tenant, workstation, agent) combination.
type rateLimitKey struct {
	tenantID      uuid.UUID
	workstationID uuid.UUID
	agentID       string // string to handle uuid.Nil cleanly
}

// bucket is a simple sliding-window token bucket.
type bucket struct {
	mu        sync.Mutex
	tokens    int
	maxTokens int
	resetAt   time.Time
	window    time.Duration
}

func newBucket(max int, window time.Duration) *bucket {
	return &bucket{
		tokens:    max,
		maxTokens: max,
		resetAt:   time.Now().Add(window),
		window:    window,
	}
}

// Allow consumes one token. Returns false if the bucket is empty.
func (b *bucket) Allow() bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	now := time.Now()
	if now.After(b.resetAt) {
		b.tokens = b.maxTokens
		b.resetAt = now.Add(b.window)
	}
	if b.tokens <= 0 {
		return false
	}
	b.tokens--
	return true
}

// WorkstationRateLimiter enforces per-(tenant, workstation, agent) exec rate limits.
//
// Limits:
//   - 30 exec/minute per (tenant, workstation, agent) — prevents agent runaway
//   - 300 exec/hour per (tenant, workstation) — workstation-wide ceiling
//
// State is in-process only (no Redis/DB). Rate limit resets on gateway restart —
// acceptable for a soft limit. Document as known limitation.
type WorkstationRateLimiter struct {
	mu         sync.Mutex
	perAgent   map[rateLimitKey]*bucket // per (tenant, ws, agent) — 30/min
	perStation map[rateLimitKey]*bucket // per (tenant, ws) — 300/hour

	agentMax   int
	agentWin   time.Duration
	stationMax int
	stationWin time.Duration
}

// NewWorkstationRateLimiter creates a WorkstationRateLimiter with default limits:
// 30 exec/min per agent+workstation, 300 exec/hour per workstation.
func NewWorkstationRateLimiter() *WorkstationRateLimiter {
	return &WorkstationRateLimiter{
		perAgent:   make(map[rateLimitKey]*bucket),
		perStation: make(map[rateLimitKey]*bucket),
		agentMax:   30,
		agentWin:   time.Minute,
		stationMax: 300,
		stationWin: time.Hour,
	}
}

// Allow checks both rate limit tiers and returns false if either is exceeded.
// agentID is the agent UUID string; empty string collapses all unknown agents to one bucket.
func (r *WorkstationRateLimiter) Allow(tenantID, workstationID uuid.UUID, agentID string) bool {
	agentKey := rateLimitKey{tenantID: tenantID, workstationID: workstationID, agentID: agentID}
	stationKey := rateLimitKey{tenantID: tenantID, workstationID: workstationID}

	r.mu.Lock()
	ab, ok := r.perAgent[agentKey]
	if !ok {
		ab = newBucket(r.agentMax, r.agentWin)
		r.perAgent[agentKey] = ab
	}
	sb, ok := r.perStation[stationKey]
	if !ok {
		sb = newBucket(r.stationMax, r.stationWin)
		r.perStation[stationKey] = sb
	}
	r.mu.Unlock()

	// Check workstation-wide limit first (cheaper reject path).
	if !sb.Allow() {
		return false
	}
	if !ab.Allow() {
		// Refund the station token since agent was rejected.
		sb.mu.Lock()
		if sb.tokens < sb.maxTokens {
			sb.tokens++
		}
		sb.mu.Unlock()
		return false
	}
	return true
}
