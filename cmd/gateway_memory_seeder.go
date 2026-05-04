package cmd

import (
	"context"
	"log/slog"
	"time"

	"github.com/nextlevelbuilder/goclaw/internal/config"
	"github.com/nextlevelbuilder/goclaw/internal/memory"
	"github.com/nextlevelbuilder/goclaw/internal/store"
)

// setupMemoryDiskSeeder wires the disk → Postgres memory sweeper.
// One sweeper per agent listed in cfg.Channels.MemorySeeder.AgentKeys.
// Runs an initial sweep then re-sweeps on a ticker so PR merges into
// the cloned memory vault propagate without a goclaw restart.
//
// Project-agnostic: the seeder doesn't know or care about a specific
// vault layout. The consumer's config picks which agents to sweep;
// each sweep walks <workspace>/memory/**/*.md and upserts into the
// MemoryStore. Frontmatter + wikilinks parsing happens inside
// IndexDocument (memory_docs.go), so a different consumer's vault
// works identically as long as it uses Obsidian conventions.
func setupMemoryDiskSeeder(ctx context.Context, cfg *config.Config, pgStores *store.Stores) {
	if cfg == nil || cfg.Channels.MemorySeeder == nil {
		return
	}
	seederCfg := cfg.Channels.MemorySeeder
	if len(seederCfg.AgentKeys) == 0 {
		slog.Info("memory disk seeder: no agent_keys configured, skipping")
		return
	}
	if pgStores == nil || pgStores.Memory == nil || pgStores.Agents == nil {
		slog.Warn("memory disk seeder: required stores missing (memory or agents), skipping")
		return
	}

	interval := time.Duration(seederCfg.IntervalMs) * time.Millisecond
	if seederCfg.IntervalMs == 0 {
		interval = 5 * time.Minute
	}
	// Negative => one-shot only (run startup pass, never re-sweep).
	oneShot := seederCfg.IntervalMs < 0

	for _, key := range seederCfg.AgentKeys {
		agent, err := pgStores.Agents.GetByKey(ctx, key)
		if err != nil {
			slog.Warn("memory disk seeder: agent lookup failed",
				"agent_key", key, "err", err)
			continue
		}
		if agent.Workspace == "" {
			slog.Warn("memory disk seeder: agent has no workspace, skipping",
				"agent_key", key)
			continue
		}

		seeder := &memory.DiskSeeder{
			Store:        pgStores.Memory,
			Workspace:    agent.Workspace,
			AgentID:      agent.ID.String(),
			Log:          slog.Default(),
			MaxFileBytes: seederCfg.MaxFileBytes,
		}

		// Run initial pass on a goroutine so a slow first sweep doesn't
		// stall gateway startup. Each agent gets its own goroutine.
		go runSeederLoop(ctx, seeder, key, interval, oneShot)
		slog.Info("memory disk seeder started",
			"agent_key", key,
			"workspace", agent.Workspace,
			"interval", interval,
			"one_shot", oneShot)
	}
}

func runSeederLoop(ctx context.Context, seeder *memory.DiskSeeder, agentKey string, interval time.Duration, oneShot bool) {
	doSweep := func() {
		t0 := time.Now()
		res, err := seeder.Sweep(ctx)
		if err != nil {
			slog.Warn("memory disk seeder: sweep failed",
				"agent_key", agentKey, "err", err)
			return
		}
		if res.Indexed > 0 || res.Failed > 0 {
			slog.Info("memory disk seeder: sweep complete",
				"agent_key", agentKey,
				"indexed", res.Indexed,
				"skipped", res.Skipped,
				"failed", res.Failed,
				"elapsed", time.Since(t0))
		}
	}

	doSweep()
	if oneShot {
		return
	}

	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			doSweep()
		}
	}
}
