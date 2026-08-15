package consolidation

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/nextlevelbuilder/goclaw/internal/bgalert"
	"github.com/nextlevelbuilder/goclaw/internal/eventbus"
	"github.com/nextlevelbuilder/goclaw/internal/store"
)

// semanticWorker handles episodic.created events → extracts KG facts from summaries.
type semanticWorker struct {
	kgStore   store.KnowledgeGraphStore
	extractor EntityExtractor
	eventBus  eventbus.DomainEventBus
	alertDeps bgalert.AlertDeps
}

// Handle extracts entities and relations from an episodic summary.
func (w *semanticWorker) Handle(ctx context.Context, event eventbus.DomainEvent) error {
	payload, ok := event.Payload.(*eventbus.EpisodicCreatedPayload)
	if !ok {
		return fmt.Errorf("semantic: unexpected payload type %T", event.Payload)
	}

	// Inject tenant context so bgalert scopes correctly.
	if event.TenantID != "" {
		if tid, err := uuid.Parse(event.TenantID); err == nil {
			ctx = store.WithTenantID(ctx, tid)
		}
	}

	extractionInput := formatSemanticExtractionInput(payload)
	if w.extractor == nil || extractionInput == "" {
		return nil
	}

	// Extract entities/relations from summary (much cheaper than full session)
	result, err := w.extractor.Extract(ctx, extractionInput)
	if err != nil {
		bgalert.ReportProviderError(ctx, w.alertDeps, "kg_extraction", err)
		slog.Warn("semantic: extraction failed", "episodic_id", payload.EpisodicID, "err", err)
		return nil // non-fatal: extraction failure doesn't block pipeline
	}
	bgalert.ClearProviderError(ctx, w.alertDeps.SystemConfigs)
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

func formatSemanticExtractionInput(payload *eventbus.EpisodicCreatedPayload) string {
	if payload == nil {
		return ""
	}
	summary := strings.TrimSpace(payload.Summary)
	if summary == "" {
		return ""
	}
	topics := compactSemanticHints(payload.KeyTopics)
	entities := compactSemanticHints(payload.KeyEntities)
	if len(topics) == 0 && len(entities) == 0 {
		return summary
	}

	var b strings.Builder
	b.WriteString("Fact summary:\n")
	b.WriteString(summary)
	b.WriteString("\n\nContext hints for disambiguation only. Do not create entities or relations solely because a value appears below; only extract facts supported by the fact summary.")
	if len(entities) > 0 {
		b.WriteString("\n\nCandidate entities:\n")
		for _, entity := range entities {
			b.WriteString("- ")
			b.WriteString(entity)
			b.WriteByte('\n')
		}
	}
	if len(topics) > 0 {
		b.WriteString("\nTopics / context tags:\n")
		for _, topic := range topics {
			b.WriteString("- ")
			b.WriteString(topic)
			b.WriteByte('\n')
		}
	}
	return strings.TrimSpace(b.String())
}

func compactSemanticHints(values []string) []string {
	out := make([]string, 0, len(values))
	seen := make(map[string]struct{}, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		key := strings.ToLower(value)
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		out = append(out, value)
	}
	return out
}
