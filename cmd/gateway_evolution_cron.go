package cmd

import (
	"context"
	"database/sql"
	"log/slog"
	"time"

	"github.com/nextlevelbuilder/goclaw/internal/agent"
	"github.com/nextlevelbuilder/goclaw/internal/store"
)

// evolutionCronLockID is a PG advisory lock ID for preventing duplicate cron runs.
// Only one gateway instance should run evolution analysis at a time.
const evolutionCronLockID int64 = 0x65766F6C // "evol"

// evolutionRunHours defines when analysis runs each day (server local time).
var evolutionRunHours = []int{3, 9, 15, 21}

// runEvolutionCron runs the v3 evolution suggestion engine (every 6 hours at 3:00/9:00/15:00/21:00)
// and evaluation/rollback check (weekly on Sundays at 3:00 AM) as a background goroutine.
// Designed to be called with `go runEvolutionCron(...)`.
func runEvolutionCron(stores *store.Stores, engine *agent.SuggestionEngine) {
	// Wait 1 minute after startup for warm-up, then run first analysis.
	time.Sleep(1 * time.Minute)
	runSuggestionAnalysis(stores, engine)

	for {
		next := nextScheduledRun(evolutionRunHours)

		timer := time.NewTimer(time.Until(next))
		<-timer.C
		timer.Stop()

		runSuggestionAnalysis(stores, engine)

		// Weekly evaluation: Sunday at 3:00 AM only.
		now := time.Now()
		if now.Weekday() == time.Sunday && now.Hour() < 4 {
			runEvolutionEvaluation(stores)
		}
	}
}

// nextScheduledRun returns the earliest future time matching one of the given hours.
// Uses AddDate to handle DST transitions correctly (calendar day, not 24h).
func nextScheduledRun(hours []int) time.Time {
	now := time.Now()
	var best time.Time
	for _, h := range hours {
		t := time.Date(now.Year(), now.Month(), now.Day(), h, 0, 0, 0, now.Location())
		if !now.Before(t) {
			t = t.AddDate(0, 0, 1) // already passed today, try tomorrow
		}
		if best.IsZero() || t.Before(best) {
			best = t
		}
	}
	return best
}

// tryAdvisoryLock acquires a PG session-level advisory lock on a pinned connection.
// Returns the pinned connection (caller must close) and whether the lock was acquired.
// No-op (returns nil, true) when db is nil (SQLite/desktop edition runs single-instance).
func tryAdvisoryLock(ctx context.Context, db *sql.DB) (*sql.Conn, bool) {
	if db == nil {
		return nil, true
	}
	conn, err := db.Conn(ctx)
	if err != nil {
		slog.Debug("evolution.cron.conn_failed", "error", err)
		return nil, false
	}
	var acquired bool
	if err := conn.QueryRowContext(ctx, "SELECT pg_try_advisory_lock($1)", evolutionCronLockID).Scan(&acquired); err != nil {
		conn.Close()
		slog.Debug("evolution.cron.advisory_lock_failed", "error", err)
		return nil, false
	}
	if !acquired {
		conn.Close()
		return nil, false
	}
	return conn, true
}

// releaseAdvisoryLock releases a PG advisory lock on the pinned connection and closes it.
func releaseAdvisoryLock(ctx context.Context, conn *sql.Conn) {
	if conn == nil {
		return
	}
	_, _ = conn.ExecContext(ctx, "SELECT pg_advisory_unlock($1)", evolutionCronLockID)
	conn.Close()
}

// runSuggestionAnalysis lists agents with evolution enabled and runs analysis.
// Note: List(ctx, "") uses bare context (no tenant) to list ALL agents cross-tenant.
// Per-agent calls are scoped via WithTenantID.
func runSuggestionAnalysis(stores *store.Stores, engine *agent.SuggestionEngine) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	// Acquire advisory lock on a pinned connection to prevent duplicate runs across instances.
	conn, acquired := tryAdvisoryLock(ctx, stores.DB)
	if !acquired {
		slog.Debug("evolution.cron.skipped_lock_held")
		return
	}
	defer releaseAdvisoryLock(ctx, conn)

	agents, err := stores.Agents.List(ctx, "")
	if err != nil {
		slog.Warn("evolution.cron.list_agents_failed", "error", err)
		return
	}

	var count int
	for _, ag := range agents {
		if ag.Status != store.AgentStatusActive {
			continue
		}
		flags := ag.ParseV3Flags()
		if !flags.EvolutionMetrics {
			continue
		}
		if !flags.EvolutionSuggest {
			continue // metrics enabled but suggestions disabled — skip analysis
		}
		agentCtx := store.WithTenantID(ctx, ag.TenantID)
		if _, err := engine.Analyze(agentCtx, ag.ID); err != nil {
			slog.Debug("evolution.cron.analyze_failed", "agent", ag.ID, "error", err)
		}
		count++
	}

	if count > 0 {
		slog.Info("evolution.cron.analysis_complete", "agents", count)
	}
}

// runEvolutionEvaluation checks applied suggestions and rolls back quality drops.
func runEvolutionEvaluation(stores *store.Stores) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	agents, err := stores.Agents.List(ctx, "")
	if err != nil {
		slog.Warn("evolution.cron.eval_list_failed", "error", err)
		return
	}

	guardrails := agent.DefaultGuardrails()
	for _, ag := range agents {
		if ag.Status != store.AgentStatusActive {
			continue
		}
		flags := ag.ParseV3Flags()
		if !flags.EvolutionMetrics {
			continue
		}
		if !flags.EvolutionSuggest {
			continue // skip evaluation for agents with suggestions disabled
		}
		agentCtx := store.WithTenantID(ctx, ag.TenantID)
		if err := agent.EvaluateApplied(agentCtx, ag.ID, guardrails, stores.EvolutionMetrics, stores.EvolutionSuggestions, stores.Agents); err != nil {
			slog.Debug("evolution.cron.eval_failed", "agent", ag.ID, "error", err)
		}
	}
}
