// Package consolidation provides event-driven async workers for the
// session → episodic → semantic memory pipeline.
//
// V3 design: Phase 3 — consolidation pipeline.
package consolidation

import (
	"context"
	"log/slog"
	"time"

	"github.com/nextlevelbuilder/goclaw/internal/bgalert"
	"github.com/nextlevelbuilder/goclaw/internal/eventbus"
	"github.com/nextlevelbuilder/goclaw/internal/providers"
	"github.com/nextlevelbuilder/goclaw/internal/store"
)

// ConsolidationDeps bundles all dependencies for the consolidation pipeline.
type ConsolidationDeps struct {
	EpisodicStore store.EpisodicStore
	MemoryStore   store.MemoryStore
	KGStore       store.KnowledgeGraphStore
	SessionStore  store.SessionCoreStore // for reading session messages during summarization
	EventBus      eventbus.DomainEventBus
	SystemConfigs store.SystemConfigStore // per-tenant provider config
	Registry      *providers.Registry     // provider resolution
	Extractor     EntityExtractor
	AlertDeps     bgalert.AlertDeps // for reporting non-retryable LLM errors
	// AgentStore is optional: when present, the dreaming worker reads
	// per-agent overrides from MemoryConfig.Dreaming. If nil, the worker
	// uses its built-in defaults for every agent.
	AgentStore store.AgentCRUDStore
}

// Register wires all consolidation workers to the event bus.
// Returns a cleanup function that unsubscribes all handlers.
func Register(deps ConsolidationDeps) func() {
	episodic := &episodicWorker{
		store:         deps.EpisodicStore,
		sessions:      deps.SessionStore,
		systemConfigs: deps.SystemConfigs,
		registry:      deps.Registry,
		eventBus:      deps.EventBus,
		alertDeps:     deps.AlertDeps,
	}
	semantic := &semanticWorker{
		kgStore:   deps.KGStore,
		extractor: deps.Extractor,
		eventBus:  deps.EventBus,
		alertDeps: deps.AlertDeps,
	}
	dedup := &dedupWorker{
		kgStore: deps.KGStore,
	}

	dreaming := &dreamingWorker{
		episodicStore: deps.EpisodicStore,
		memoryStore:   deps.MemoryStore,
		systemConfigs: deps.SystemConfigs,
		registry:      deps.Registry,
		alertDeps:     deps.AlertDeps,
		threshold:     dreamingDefaultThreshold,
		debounce:      dreamingDefaultDebounce,
		resolveConfig: newAgentStoreResolver(deps.AgentStore),
	}

	unsub1 := deps.EventBus.Subscribe(eventbus.EventSessionCompleted, episodic.Handle)
	unsub2 := deps.EventBus.Subscribe(eventbus.EventEpisodicCreated, semantic.Handle)
	unsub3 := deps.EventBus.Subscribe(eventbus.EventEntityUpserted, dedup.Handle)
	unsub4 := deps.EventBus.Subscribe(eventbus.EventEpisodicCreated, dreaming.Handle)

	// Periodic pruning of expired episodic summaries (runs every 6 hours).
	pruneStop := make(chan struct{})
	go func() {
		ticker := time.NewTicker(6 * time.Hour)
		defer ticker.Stop()
		for {
			select {
			case <-ticker.C:
				n, err := deps.EpisodicStore.PruneExpired(context.Background())
				if err != nil {
					slog.Warn("episodic prune failed", "error", err)
				} else if n > 0 {
					slog.Info("episodic prune completed", "deleted", n)
				}
			case <-pruneStop:
				return
			}
		}
	}()

	return func() { unsub1(); unsub2(); unsub3(); unsub4(); close(pruneStop) }
}

// summarizationPrompt for LLM session summarization.
const summarizationPrompt = `Summarize this conversation session concisely. Focus on:
- Key decisions made
- Facts learned about the user or project
- Tasks completed or in-progress
- Important technical details
- User preferences expressed

Output: 2-4 paragraph summary. Include entity names explicitly.
Do NOT include greetings, filler, or metadata.`
