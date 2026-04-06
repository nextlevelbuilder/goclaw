// Package consolidation provides event-driven async workers for the
// session → episodic → semantic memory pipeline.
//
// V3 design: Phase 3 — consolidation pipeline.
package consolidation

import (
	"github.com/nextlevelbuilder/goclaw/internal/eventbus"
	"github.com/nextlevelbuilder/goclaw/internal/knowledgegraph"
	"github.com/nextlevelbuilder/goclaw/internal/providers"
	"github.com/nextlevelbuilder/goclaw/internal/store"
)

// ConsolidationDeps bundles all dependencies for the consolidation pipeline.
type ConsolidationDeps struct {
	EpisodicStore store.EpisodicStore
	KGStore       store.KnowledgeGraphStore
	EventBus      eventbus.DomainEventBus
	Provider      providers.Provider // for LLM summarization
	Model         string
	Extractor     *knowledgegraph.Extractor
}

// Register wires all consolidation workers to the event bus.
// Returns a cleanup function that unsubscribes all handlers.
func Register(deps ConsolidationDeps) func() {
	episodic := &episodicWorker{
		store:    deps.EpisodicStore,
		provider: deps.Provider,
		model:    deps.Model,
		eventBus: deps.EventBus,
	}
	semantic := &semanticWorker{
		kgStore:   deps.KGStore,
		extractor: deps.Extractor,
		eventBus:  deps.EventBus,
	}
	dedup := &dedupWorker{
		kgStore: deps.KGStore,
	}

	unsub1 := deps.EventBus.Subscribe(eventbus.EventSessionCompleted, episodic.Handle)
	unsub2 := deps.EventBus.Subscribe(eventbus.EventEpisodicCreated, semantic.Handle)
	unsub3 := deps.EventBus.Subscribe(eventbus.EventEntityUpserted, dedup.Handle)
	return func() { unsub1(); unsub2(); unsub3() }
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
