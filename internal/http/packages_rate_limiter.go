package http

import (
	"log/slog"
	"net"
	"net/http"
	"sync"
	"sync/atomic"
	"time"

	"github.com/nextlevelbuilder/goclaw/internal/store"
	"golang.org/x/time/rate"
)

// perKeyRateLimiter is a minimal per-key token-bucket limiter used to cap
// external GitHub API usage initiated through /v1/packages/github-releases.
// Key is userID (header X-GoClaw-User-Id) or RemoteAddr when anonymous.
//
// Stale-entry eviction is amortized: every perKeyRateLimiterSweepInterval
// accepted requests trigger an inline scan. This avoids a background
// goroutine (which leaked in tests when the package-level instance was
// swapped out) and sidesteps data races on per-entry state.
type perKeyRateLimiter struct {
	limiters    sync.Map // key → *perKeyEntry
	rps         rate.Limit
	burst       int
	callCounter atomic.Int64
}

type perKeyEntry struct {
	limiter  *rate.Limiter
	lastSeen atomic.Int64 // unix nanoseconds; updated on every Allow=true
}

// perKeyRateLimiterSweepInterval controls how often (per accepted call) the
// stale-entry sweep runs. Power-of-two for cheap modulo.
const perKeyRateLimiterSweepInterval = 1024

// perKeyRateLimiterStaleAfter is the idle window before an entry is evicted.
const perKeyRateLimiterStaleAfter = 10 * time.Minute

// newPerKeyRateLimiter: rpm is requests per minute, burst is max burst size.
// rpm <= 0 disables (always allows).
func newPerKeyRateLimiter(rpm, burst int) *perKeyRateLimiter {
	if burst <= 0 {
		burst = 5
	}
	r := rate.Limit(0)
	if rpm > 0 {
		r = rate.Limit(float64(rpm) / 60.0)
	}
	return &perKeyRateLimiter{rps: r, burst: burst}
}

// Allow reports whether the request is within budget.
func (rl *perKeyRateLimiter) Allow(key string) bool {
	if rl.rps == 0 {
		return true // disabled
	}
	nowNs := time.Now().UnixNano()

	// Prepare a fresh entry up front; LoadOrStore discards it on existing keys.
	fresh := &perKeyEntry{limiter: rate.NewLimiter(rl.rps, rl.burst)}
	fresh.lastSeen.Store(nowNs)

	v, _ := rl.limiters.LoadOrStore(key, fresh)
	entry := v.(*perKeyEntry)
	if !entry.limiter.Allow() {
		return false
	}
	entry.lastSeen.Store(nowNs)

	if rl.callCounter.Add(1)%perKeyRateLimiterSweepInterval == 0 {
		rl.sweepStale()
	}
	return true
}

// sweepStale evicts entries older than perKeyRateLimiterStaleAfter.
// Safe for concurrent invocation — sync.Map.Range + atomic lastSeen guarantee
// data-race freedom.
func (rl *perKeyRateLimiter) sweepStale() {
	cutoffNs := time.Now().Add(-perKeyRateLimiterStaleAfter).UnixNano()
	rl.limiters.Range(func(k, v any) bool {
		if v.(*perKeyEntry).lastSeen.Load() < cutoffNs {
			rl.limiters.Delete(k)
		}
		return true
	})
}

// githubReleasesLimiter caps calls to the picker endpoint to protect the
// shared upstream GitHub API quota. 30 req/min/user with burst 10 leaves
// plenty of headroom for UX while preventing quota exhaustion.
var githubReleasesLimiter = newPerKeyRateLimiter(30, 10)

// packagesWriteLimiter throttles POST /install and /uninstall. Admin-only
// endpoints, but a compromised admin token could otherwise flood the upstream
// GitHub API / pip / npm or spam manifest mutations. 10 req/min/user with
// burst 3 comfortably covers real admin workflows while breaking automated
// abuse.
var packagesWriteLimiter = newPerKeyRateLimiter(10, 3)

// rateLimitKeyFromRequest returns the authenticated user ID if present (from
// the request context populated by enrichContext/requireAuth), else falls
// back to the raw X-GoClaw-User-Id header, else the remote IP.
//
// Preferring context over header means an admin with a leaked token can't
// rotate the header mid-session to dodge the per-user bucket — the context
// is bound to the API-key owner / session principal that survived auth.
func rateLimitKeyFromRequest(r *http.Request) string {
	if uid := store.UserIDFromContext(r.Context()); uid != "" {
		return "uid:" + uid
	}
	if uid := extractUserID(r); uid != "" {
		return "uid:" + uid
	}
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil || host == "" {
		host = r.RemoteAddr
	}
	return "ip:" + host
}

// enforceGitHubReleasesLimit returns true if the request is allowed; false (after
// writing 429) if throttled.
func enforceGitHubReleasesLimit(w http.ResponseWriter, r *http.Request) bool {
	key := rateLimitKeyFromRequest(r)
	if githubReleasesLimiter.Allow(key) {
		return true
	}
	slog.Warn("security.rate_limited", "endpoint", "/v1/packages/github-releases", "key", key)
	w.Header().Set("Retry-After", "60")
	writeJSON(w, http.StatusTooManyRequests, map[string]string{
		"error": "rate limit exceeded; try again in 60 seconds",
	})
	return false
}

// enforcePackagesWriteLimit caps POST /install + /uninstall per user.
// Returns true when the request is within budget; false (after writing 429)
// when throttled.
func enforcePackagesWriteLimit(w http.ResponseWriter, r *http.Request, endpoint string) bool {
	key := rateLimitKeyFromRequest(r)
	if packagesWriteLimiter.Allow(key) {
		return true
	}
	slog.Warn("security.rate_limited", "endpoint", endpoint, "key", key)
	w.Header().Set("Retry-After", "60")
	writeJSON(w, http.StatusTooManyRequests, map[string]string{
		"error": "rate limit exceeded; try again in 60 seconds",
	})
	return false
}
