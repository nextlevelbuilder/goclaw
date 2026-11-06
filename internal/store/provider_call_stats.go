package store

import (
	"context"
	"math"
	"time"

	"github.com/google/uuid"
)

// ProviderCallStatsQuery selects the LLM call spans of one provider in one tenant.
type ProviderCallStatsQuery struct {
	TenantID uuid.UUID
	Provider string // provider name as stored in spans.provider
	From     time.Time
	To       time.Time
	// ErrorLimit caps how many of the newest failed spans are returned.
	ErrorLimit int
}

// ProviderErrorSpan is one failed LLM call. Error is the raw span error text and
// must never be sent to clients; callers classify it first.
type ProviderErrorSpan struct {
	At    time.Time
	Error string
}

// ProviderCallStats aggregates the LLM call spans matched by a ProviderCallStatsQuery.
// Latency percentiles are nearest-rank (PG percentile_disc) over duration_ms of
// completed and failed calls alike, so both dialects return the same values.
type ProviderCallStats struct {
	Calls        int64
	Errors       int64
	LatencyP50MS float64
	LatencyP95MS float64
	// RecentErrors holds the newest failed calls, newest first.
	RecentErrors []ProviderErrorSpan
}

// ProviderCallStatsStore reads per-provider LLM call statistics from spans.
// The tracing stores implement it; handlers discover it by type assertion so the
// TracingStore interface and its many test doubles stay unchanged.
type ProviderCallStatsStore interface {
	GetProviderCallStats(ctx context.Context, q ProviderCallStatsQuery) (*ProviderCallStats, error)
}

// NearestRank is the 1-based nearest-rank position of percentile p (0 < p <= 1)
// among n sorted values, the same row PG percentile_disc picks. n must be > 0.
func NearestRank(p float64, n int64) int64 {
	// The epsilon keeps float noise (0.95*20 = 19.000000000000004) from bumping the rank.
	rank := int64(math.Ceil(p*float64(n) - 1e-9))
	return max(1, min(rank, n))
}
