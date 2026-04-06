// Package consolidation provides event-driven async workers for the
// session → episodic → semantic memory pipeline.
//
// V3 design: Phase 3 — consolidation pipeline.
package consolidation

import (
	"context"

	"github.com/nextlevelbuilder/goclaw/internal/eventbus"
	"github.com/nextlevelbuilder/goclaw/internal/store"
)

// EpisodicWorkerDeps bundles dependencies for EpisodicWorker.
type EpisodicWorkerDeps struct {
	EpisodicStore store.EpisodicStore
	EventBus      eventbus.DomainEventBus
}

// SemanticWorkerDeps bundles dependencies for SemanticExtractionWorker.
type SemanticWorkerDeps struct {
	KGStore  store.KnowledgeGraphStore
	EventBus eventbus.DomainEventBus
}

// DedupWorkerDeps bundles dependencies for DedupWorker.
type DedupWorkerDeps struct {
	KGStore store.KnowledgeGraphStore
}

// Register wires all consolidation workers to the event bus.
// Called at gateway startup.
// Register wires all consolidation workers to the event bus.
// Returns a cleanup function that unsubscribes all handlers.
func Register(eb eventbus.DomainEventBus, episodic EpisodicWorkerDeps, semantic SemanticWorkerDeps, dedup DedupWorkerDeps) func() {
	unsub1 := eb.Subscribe(eventbus.EventSessionCompleted, newEpisodicHandler(episodic))
	unsub2 := eb.Subscribe(eventbus.EventEpisodicCreated, newSemanticHandler(semantic))
	unsub3 := eb.Subscribe(eventbus.EventEntityUpserted, newDedupHandler(dedup))
	return func() { unsub1(); unsub2(); unsub3() }
}

// Handler stubs — implementation deferred to implementation phase.

func newEpisodicHandler(deps EpisodicWorkerDeps) eventbus.DomainEventHandler {
	return func(ctx context.Context, event eventbus.DomainEvent) error {
		// TODO(v3): summarize session → store episodic → publish episodic.created
		return nil
	}
}

func newSemanticHandler(deps SemanticWorkerDeps) eventbus.DomainEventHandler {
	return func(ctx context.Context, event eventbus.DomainEvent) error {
		// TODO(v3): extract KG entities from episodic summary → upsert → publish entity.upserted
		return nil
	}
}

func newDedupHandler(deps DedupWorkerDeps) eventbus.DomainEventHandler {
	return func(ctx context.Context, event eventbus.DomainEvent) error {
		// TODO(v3): compare embeddings → auto-merge or flag → handle temporal supersession
		return nil
	}
}
