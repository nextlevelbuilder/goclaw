package channels

import (
	"context"
	"database/sql"
	"log/slog"
	"sync"
	"time"

	"github.com/nextlevelbuilder/goclaw/internal/config"
)

// QuotaResult is returned by QuotaChecker.Check.
type QuotaResult struct {
	Allowed bool   // whether the request is within quota
	Window  string // which window was exceeded: "hour", "day", "week"
	Used    int    // current usage in the exceeded window
	Limit   int    // configured limit for the exceeded window
}

// quotaCounts holds cached request counts for a user.
type quotaCounts struct {
	hour, day, week int
	fetchedAt       time.Time
}

// QuotaChecker enforces per-user/group request quotas by counting top-level
// traces in the database. Results are cached in-memory for cacheTTL.
// Nil-safe (nil means quota not configured).
type QuotaChecker struct {
	db       *sql.DB
	config   config.QuotaConfig
	cache    map[string]*quotaCounts
	cacheTTL time.Duration
	mu       sync.RWMutex
	stopCh   chan struct{}
}

// NewQuotaChecker creates a quota checker backed by the traces table.
// Starts a background goroutine to evict stale cache entries.
func NewQuotaChecker(db *sql.DB, cfg config.QuotaConfig) *QuotaChecker {
	qc := &QuotaChecker{
		db:       db,
		config:   cfg,
		cache:    make(map[string]*quotaCounts),
		cacheTTL: 60 * time.Second,
		stopCh:   make(chan struct{}),
	}
	go qc.cleanupLoop()
	return qc
}

// Stop shuts down the background cleanup goroutine.
func (qc *QuotaChecker) Stop() {
	close(qc.stopCh)
}

// UpdateConfig replaces the quota configuration (e.g., after config reload).
func (qc *QuotaChecker) UpdateConfig(cfg config.QuotaConfig) {
	qc.mu.Lock()
	defer qc.mu.Unlock()
	qc.config = cfg
}

// Check verifies if a user is within their quota limits.
// Returns QuotaResult with Allowed=true if all windows are within limits.
func (qc *QuotaChecker) Check(ctx context.Context, userID, channel, provider string) QuotaResult {
	window := qc.resolveWindow(userID, channel, provider)
	if window.IsZero() {
		return QuotaResult{Allowed: true}
	}

	counts := qc.getCounts(ctx, userID)

	// Check each window — report the first exceeded
	if window.Hour > 0 && counts.hour >= window.Hour {
		return QuotaResult{Allowed: false, Window: "hour", Used: counts.hour, Limit: window.Hour}
	}
	if window.Day > 0 && counts.day >= window.Day {
		return QuotaResult{Allowed: false, Window: "day", Used: counts.day, Limit: window.Day}
	}
	if window.Week > 0 && counts.week >= window.Week {
		return QuotaResult{Allowed: false, Window: "week", Used: counts.week, Limit: window.Week}
	}

	return QuotaResult{Allowed: true}
}

// Increment optimistically bumps cached counts after a request is accepted.
// This prevents multiple rapid requests from bypassing the quota within the
// cache TTL window.
func (qc *QuotaChecker) Increment(userID string) {
	qc.mu.Lock()
	defer qc.mu.Unlock()
	if c, ok := qc.cache[userID]; ok {
		c.hour++
		c.day++
		c.week++
	}
}

// resolveWindow returns the effective quota window for a user, applying
// config merge priority: Groups > Channels > Providers > Default.
func (qc *QuotaChecker) resolveWindow(userID, channel, provider string) config.QuotaWindow {
	qc.mu.RLock()
	cfg := qc.config
	qc.mu.RUnlock()

	// Most specific: per-user/group override
	if w, ok := cfg.Groups[userID]; ok && !w.IsZero() {
		return w
	}

	// Channel-level override
	if channel != "" {
		if w, ok := cfg.Channels[channel]; ok && !w.IsZero() {
			return w
		}
	}

	// Provider-level override
	if provider != "" {
		if w, ok := cfg.Providers[provider]; ok && !w.IsZero() {
			return w
		}
	}

	return cfg.Default
}

// getCounts returns cached or fresh counts for a user.
func (qc *QuotaChecker) getCounts(ctx context.Context, userID string) quotaCounts {
	qc.mu.RLock()
	if c, ok := qc.cache[userID]; ok && time.Since(c.fetchedAt) < qc.cacheTTL {
		counts := *c
		qc.mu.RUnlock()
		return counts
	}
	qc.mu.RUnlock()

	// Cache miss — query DB
	counts := qc.queryDB(ctx, userID)

	qc.mu.Lock()
	qc.cache[userID] = &counts
	qc.mu.Unlock()

	return counts
}

// queryDB counts top-level traces for a user across time windows in a single query.
// Uses idx_traces_quota partial index: (user_id, created_at DESC) WHERE parent_trace_id IS NULL.
func (qc *QuotaChecker) queryDB(ctx context.Context, userID string) quotaCounts {
	now := time.Now().UTC()
	hourAgo := now.Add(-1 * time.Hour)
	dayAgo := now.Add(-24 * time.Hour)
	weekAgo := now.Add(-7 * 24 * time.Hour)

	var counts quotaCounts
	counts.fetchedAt = now

	err := qc.db.QueryRowContext(ctx, `
		SELECT
			COUNT(*) FILTER (WHERE created_at >= $2) AS hour_count,
			COUNT(*) FILTER (WHERE created_at >= $3) AS day_count,
			COUNT(*) FILTER (WHERE created_at >= $4) AS week_count
		FROM traces
		WHERE user_id = $1 AND parent_trace_id IS NULL AND created_at >= $4`,
		userID, hourAgo, dayAgo, weekAgo,
	).Scan(&counts.hour, &counts.day, &counts.week)
	if err != nil {
		slog.Warn("quota: failed to query trace counts", "user_id", userID, "error", err)
	}

	return counts
}

// QuotaUsage represents usage vs limit for a single time window.
type QuotaUsage struct {
	Used  int `json:"used"`
	Limit int `json:"limit"` // 0 = unlimited
}

// QuotaUsageEntry represents a single user/group's quota consumption.
type QuotaUsageEntry struct {
	UserID string     `json:"userId"`
	Hour   QuotaUsage `json:"hour"`
	Day    QuotaUsage `json:"day"`
	Week   QuotaUsage `json:"week"`
}

// QuotaUsageResult is the response for the quota.usage RPC method.
type QuotaUsageResult struct {
	Enabled           bool              `json:"enabled"`
	RequestsToday     int               `json:"requestsToday"`
	InputTokensToday  int64             `json:"inputTokensToday"`
	OutputTokensToday int64             `json:"outputTokensToday"`
	CostToday         float64           `json:"costToday"`
	UniqueUsersToday  int               `json:"uniqueUsersToday"`
	Entries           []QuotaUsageEntry `json:"entries"`
}

// Usage returns quota consumption for all users with recent activity.
// Used by the dashboard to display quota usage with progress bars.
func (qc *QuotaChecker) Usage(ctx context.Context) QuotaUsageResult {
	qc.mu.RLock()
	cfg := qc.config
	qc.mu.RUnlock()

	result := QuotaUsageResult{
		Enabled: cfg.Enabled,
		Entries: []QuotaUsageEntry{},
	}
	if !cfg.Enabled {
		// Still return today's summary even when quota is disabled
		QueryTodaySummary(ctx, qc.db, &result)
		return result
	}

	now := time.Now().UTC()
	hourAgo := now.Add(-1 * time.Hour)
	dayAgo := now.Add(-24 * time.Hour)
	weekAgo := now.Add(-7 * 24 * time.Hour)

	// Query per-user counts for the past week
	rows, err := qc.db.QueryContext(ctx, `
		SELECT
			user_id,
			COUNT(*) FILTER (WHERE created_at >= $1) AS hour_count,
			COUNT(*) FILTER (WHERE created_at >= $2) AS day_count,
			COUNT(*) AS week_count
		FROM traces
		WHERE parent_trace_id IS NULL AND created_at >= $3
		GROUP BY user_id
		ORDER BY week_count DESC
		LIMIT 50`,
		hourAgo, dayAgo, weekAgo,
	)
	if err != nil {
		slog.Warn("quota.usage: failed to query user counts", "error", err)
		QueryTodaySummary(ctx, qc.db, &result)
		return result
	}
	defer rows.Close()

	for rows.Next() {
		var userID string
		var hour, day, week int
		if err := rows.Scan(&userID, &hour, &day, &week); err != nil {
			continue
		}

		window := qc.resolveWindow(userID, "", "")
		result.Entries = append(result.Entries, QuotaUsageEntry{
			UserID: userID,
			Hour:   QuotaUsage{Used: hour, Limit: window.Hour},
			Day:    QuotaUsage{Used: day, Limit: window.Day},
			Week:   QuotaUsage{Used: week, Limit: window.Week},
		})
	}

	QueryTodaySummary(ctx, qc.db, &result)
	return result
}

// QueryTodaySummary fills today's aggregate stats into the result.
// Exported so QuotaMethods can call it directly when QuotaChecker is nil.
func QueryTodaySummary(ctx context.Context, db *sql.DB, result *QuotaUsageResult) {
	if db == nil {
		return
	}
	now := time.Now().UTC()
	startOfDay := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.UTC)

	err := db.QueryRowContext(ctx, `
		SELECT
			COUNT(*),
			COALESCE(SUM(total_input_tokens), 0),
			COALESCE(SUM(total_output_tokens), 0),
			COALESCE(SUM(total_cost), 0),
			COUNT(DISTINCT user_id)
		FROM traces
		WHERE parent_trace_id IS NULL AND created_at >= $1`,
		startOfDay,
	).Scan(&result.RequestsToday, &result.InputTokensToday, &result.OutputTokensToday, &result.CostToday, &result.UniqueUsersToday)
	if err != nil {
		slog.Warn("quota.usage: failed to query today summary", "error", err)
	}
}

// cleanupLoop periodically evicts stale cache entries.
func (qc *QuotaChecker) cleanupLoop() {
	ticker := time.NewTicker(2 * time.Minute)
	defer ticker.Stop()

	for {
		select {
		case <-qc.stopCh:
			return
		case <-ticker.C:
			qc.mu.Lock()
			staleThreshold := time.Now().Add(-5 * time.Minute)
			for k, v := range qc.cache {
				if v.fetchedAt.Before(staleThreshold) {
					delete(qc.cache, k)
				}
			}
			qc.mu.Unlock()
		}
	}
}
