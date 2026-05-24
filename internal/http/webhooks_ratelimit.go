package http

import (
	"sync"
	"sync/atomic"
	"time"

	"golang.org/x/time/rate"
)

// webhookLimiter is a two-tier token-bucket rate limiter for webhook endpoints.
//
// Tier 1 — per-webhook: keyed by webhook UUID. Rate sourced from
// WebhookData.RateLimitPerMin (0 = unlimited).
//
// Tier 2 — per-tenant: keyed by tenant UUID. Rate sourced from
// WebhookTenantRatePerMin config (default 600).
//
// Both tiers must allow for a request to proceed. Per-webhook is checked first
// so a misconfigured individual webhook can't starve the tenant bucket.
//
// Ownership: single *webhookLimiter per gateway process, held by middleware
// closure. Never attach to request context — stale buckets would never evict.
type webhookLimiter struct {
	tenantRPM int // global per-tenant rate (req/min); 0 = unlimited

	buckets     sync.Map // string key → *webhookLimiterEntry
	callCounter atomic.Int64
}

type webhookLimiterEntry struct {
	limiter  *rate.Limiter
	lastSeen atomic.Int64 // unix nanoseconds
}

const (
	// webhookLimiterSweepEvery — sweep stale entries every N accepted calls.
	webhookLimiterSweepEvery = 512
	// webhookLimiterStaleAfter — evict buckets idle for this long.
	webhookLimiterStaleAfter = 30 * time.Minute
)

// newWebhookLimiter creates a limiter with the given tenant-level RPM cap.
// rpm <= 0 disables the tenant tier (unlimited).
func newWebhookLimiter(tenantRPM int) *webhookLimiter {
	return &webhookLimiter{tenantRPM: tenantRPM}
}

// NewWebhookLimiter creates a process-lifetime limiter with the default tenant RPM cap.
// Use this when wiring the message/LLM handlers outside the http package.
func NewWebhookLimiter() *webhookLimiter {
	return newWebhookLimiter(defaultWebhookTenantRPM)
}

// AllowWebhook checks the per-webhook bucket. webhookID must be the UUID string;
// rpm is WebhookData.RateLimitPerMin (0 = unlimited).
func (wl *webhookLimiter) AllowWebhook(webhookID string, rpm int) bool {
	return wl.allow("webhook:"+webhookID, rpm)
}

// AllowTenant checks the per-tenant bucket using the configured tenant RPM.
func (wl *webhookLimiter) AllowTenant(tenantID string) bool {
	return wl.allow("tenant:"+tenantID, wl.tenantRPM)
}

// allow is the shared implementation for both keyspaces.
// rpm == 0 → unlimited (always returns true, no bucket created).
func (wl *webhookLimiter) allow(key string, rpm int) bool {
	if rpm <= 0 {
		return true
	}
	limit := rate.Limit(float64(rpm) / 60.0)
	burst := rpm // burst = full rpm per spec (Success Criteria §3)

	nowNs := time.Now().UnixNano()

	// Fast path: Load avoids allocating a new entry on hits (the common case).
	var entry *webhookLimiterEntry
	if v, ok := wl.buckets.Load(key); ok {
		entry = v.(*webhookLimiterEntry)
	} else {
		fresh := &webhookLimiterEntry{limiter: rate.NewLimiter(limit, burst)}
		fresh.lastSeen.Store(nowNs)
		v, _ := wl.buckets.LoadOrStore(key, fresh)
		entry = v.(*webhookLimiterEntry)
	}
	if !entry.limiter.Allow() {
		return false
	}
	entry.lastSeen.Store(nowNs)

	if wl.callCounter.Add(1)%webhookLimiterSweepEvery == 0 {
		wl.sweepStale()
	}
	return true
}

// sweepStale evicts entries that have been idle longer than webhookLimiterStaleAfter.
// Safe for concurrent calls — sync.Map.Range + atomic lastSeen are data-race free.
func (wl *webhookLimiter) sweepStale() {
	cutoffNs := time.Now().Add(-webhookLimiterStaleAfter).UnixNano()
	wl.buckets.Range(func(k, v any) bool {
		if v.(*webhookLimiterEntry).lastSeen.Load() < cutoffNs {
			wl.buckets.Delete(k)
		}
		return true
	})
}

// defaultWebhookTenantRPM is the fallback tenant rate when config omits the field.
const defaultWebhookTenantRPM = 600
