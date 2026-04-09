package consolidation

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/nextlevelbuilder/goclaw/internal/eventbus"
	"github.com/nextlevelbuilder/goclaw/internal/store"
)

// semanticWorker handles episodic.created events → extracts KG facts from summaries.
type semanticWorker struct {
	kgStore   store.KnowledgeGraphStore
	extractor EntityExtractor
	eventBus  eventbus.DomainEventBus
}

// Handle extracts entities and relations from an episodic summary.
func (w *semanticWorker) Handle(ctx context.Context, event eventbus.DomainEvent) error {
	payload, ok := event.Payload.(*eventbus.EpisodicCreatedPayload)
	if !ok {
		return fmt.Errorf("semantic: unexpected payload type %T", event.Payload)
	}

	if w.extractor == nil || payload.Summary == "" {
		return nil
	}

	// Extract entities/relations from summary (much cheaper than full session)
	result, err := w.extractor.Extract(ctx, payload.Summary)
	if err != nil {
		slog.Warn("semantic: extraction failed", "episodic_id", payload.EpisodicID, "err", err)
		return nil // non-fatal: extraction failure doesn't block pipeline
	}
	if len(result.Entities) == 0 && len(result.Relations) == 0 {
		return nil
	}

	// Set temporal fields + scoping on extracted entities
	now := time.Now().UTC()
	for i := range result.Entities {
		result.Entities[i].AgentID = event.AgentID
		result.Entities[i].UserID = event.UserID
		result.Entities[i].ValidFrom = &now
	}
	for i := range result.Relations {
		result.Relations[i].AgentID = event.AgentID
		result.Relations[i].UserID = event.UserID
		result.Relations[i].ValidFrom = &now
	}

	// Ingest into KG store
	entityIDs, err := w.kgStore.IngestExtraction(ctx, event.AgentID, event.UserID,
		result.Entities, result.Relations)
	if err != nil {
		return fmt.Errorf("semantic: ingest: %w", err)
	}

	// Publish entity.upserted for dedup worker
	if len(entityIDs) > 0 {
		w.eventBus.Publish(eventbus.DomainEvent{
			Type:     eventbus.EventEntityUpserted,
			SourceID: payload.EpisodicID,
			TenantID: event.TenantID,
			AgentID:  event.AgentID,
			UserID:   event.UserID,
			Payload:  &eventbus.EntityUpsertedPayload{EntityIDs: entityIDs},
		})
	}

	slog.Info("semantic: extracted", "entities", len(result.Entities),
		"relations", len(result.Relations), "episodic_id", payload.EpisodicID)
	return nil
}
