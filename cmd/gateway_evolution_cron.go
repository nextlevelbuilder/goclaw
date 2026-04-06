package cmd

import (
	"context"
	"log/slog"
	"time"

	"github.com/nextlevelbuilder/goclaw/internal/agent"
	"github.com/nextlevelbuilder/goclaw/internal/store"
)

// runEvolutionCron runs the v3 evolution suggestion engine (daily) and
// evaluation/rollback check (weekly) as background goroutines.
// Designed to be called with `go runEvolutionCron(...)`.
func runEvolutionCron(stores *store.Stores, engine *agent.SuggestionEngine) {
	dailyTicker := time.NewTicker(24 * time.Hour)
	defer dailyTicker.Stop()

	weeklyTicker := time.NewTicker(7 * 24 * time.Hour)
	defer weeklyTicker.Stop()

	// Run first analysis 1 minute after startup (warm-up).
	time.Sleep(1 * time.Minute)
	runSuggestionAnalysis(stores, engine)

	for {
		select {
		case <-dailyTicker.C:
			runSuggestionAnalysis(stores, engine)
		case <-weeklyTicker.C:
			runEvolutionEvaluation(stores)
		}
	}
}

// runSuggestionAnalysis lists agents with evolution metrics enabled and runs analysis.
func runSuggestionAnalysis(stores *store.Stores, engine *agent.SuggestionEngine) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	// List all agents (empty ownerID = all, tenant-scoped via context).
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
		agentCtx := store.WithTenantID(ctx, ag.TenantID)
		if err := agent.EvaluateApplied(agentCtx, ag.ID, guardrails, stores.EvolutionMetrics, stores.EvolutionSuggestions, stores.Agents); err != nil {
			slog.Debug("evolution.cron.eval_failed", "agent", ag.ID, "error", err)
		}
	}
}
