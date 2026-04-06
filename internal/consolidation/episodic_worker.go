package consolidation

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/google/uuid"
	"github.com/nextlevelbuilder/goclaw/internal/eventbus"
	"github.com/nextlevelbuilder/goclaw/internal/providers"
	"github.com/nextlevelbuilder/goclaw/internal/store"
)

// episodicWorker handles session.completed events → creates episodic summaries.
type episodicWorker struct {
	store    store.EpisodicStore
	provider providers.Provider
	model    string
	eventBus eventbus.DomainEventBus
}

// Handle processes a session.completed event into an episodic summary.
func (w *episodicWorker) Handle(ctx context.Context, event eventbus.DomainEvent) error {
	payload, ok := event.Payload.(*eventbus.SessionCompletedPayload)
	if !ok {
		return fmt.Errorf("episodic: unexpected payload type %T", event.Payload)
	}

	// Build source_id for idempotency
	sourceID := fmt.Sprintf("%s:%d", payload.SessionKey, payload.CompactionCount)
	exists, err := w.store.ExistsBySourceID(ctx, event.AgentID, event.UserID, sourceID)
	if err != nil {
		return fmt.Errorf("episodic: check source_id: %w", err)
	}
	if exists {
		slog.Debug("episodic: skipping duplicate", "source_id", sourceID)
		return nil
	}

	// Use compaction summary if available, else call LLM
	summary := payload.Summary
	if summary == "" && w.provider != nil {
		summary, err = w.summarizeSession(ctx, payload)
		if err != nil {
			return fmt.Errorf("episodic: summarize: %w", err)
		}
	}
	if summary == "" {
		slog.Debug("episodic: no summary available", "session", payload.SessionKey)
		return nil
	}

	// Create episodic summary
	l0 := generateL0Abstract(summary)
	entities := extractEntityNames(summary)
	expiresAt := time.Now().UTC().Add(90 * 24 * time.Hour)

	ep := &store.EpisodicSummary{
		TenantID:   uuid.MustParse(event.TenantID),
		AgentID:    uuid.MustParse(event.AgentID),
		UserID:     event.UserID,
		SessionKey: payload.SessionKey,
		Summary:    summary,
		KeyTopics:  entities,
		TurnCount:  payload.MessageCount,
		TokenCount: payload.TokensUsed,
		L0Abstract: l0,
		SourceID:   sourceID,
		SourceType: "session",
		ExpiresAt:  &expiresAt,
	}
	if err := w.store.Create(ctx, ep); err != nil {
		return fmt.Errorf("episodic: create: %w", err)
	}

	// Publish episodic.created event for downstream semantic extraction
	w.eventBus.Publish(eventbus.DomainEvent{
		Type:     eventbus.EventEpisodicCreated,
		SourceID: ep.ID.String(),
		TenantID: event.TenantID,
		AgentID:  event.AgentID,
		UserID:   event.UserID,
		Payload: &eventbus.EpisodicCreatedPayload{
			EpisodicID:  ep.ID.String(),
			SessionKey:  payload.SessionKey,
			Summary:     summary,
			KeyEntities: entities,
		},
	})

	slog.Info("episodic: created summary", "session", payload.SessionKey, "l0_len", len(l0))
	return nil
}

// summarizeSession calls LLM to summarize a session.
func (w *episodicWorker) summarizeSession(ctx context.Context, payload *eventbus.SessionCompletedPayload) (string, error) {
	resp, err := w.provider.Chat(ctx, providers.ChatRequest{
		Messages: []providers.Message{
			{Role: "system", Content: summarizationPrompt},
			{Role: "user", Content: fmt.Sprintf("Session: %s\nMessages: %d\nTokens: %d",
				payload.SessionKey, payload.MessageCount, payload.TokensUsed)},
		},
		Model: w.model,
	})
	if err != nil {
		return "", err
	}
	return resp.Content, nil
}
