package consolidation

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/nextlevelbuilder/goclaw/internal/eventbus"
	"github.com/nextlevelbuilder/goclaw/internal/store"
)

// dedupWorker handles entity.upserted events → runs dedup checks on new entities.
// Terminal worker in the consolidation pipeline — no downstream events.
type dedupWorker struct {
	kgStore store.KnowledgeGraphStore
}

// Handle runs dedup detection on newly upserted entity IDs.
func (w *dedupWorker) Handle(ctx context.Context, event eventbus.DomainEvent) error {
	payload, ok := event.Payload.(*eventbus.EntityUpsertedPayload)
	if !ok {
		return fmt.Errorf("dedup: unexpected payload type %T", event.Payload)
	}
	if len(payload.EntityIDs) == 0 {
		return nil
	}

	merged, flagged, err := w.kgStore.DedupAfterExtraction(ctx, event.AgentID, event.UserID, payload.EntityIDs)
	if err != nil {
		slog.Warn("dedup: failed", "err", err, "agent", event.AgentID)
		return nil // non-fatal
	}

	if merged > 0 || flagged > 0 {
		slog.Info("dedup: processed", "merged", merged, "flagged", flagged, "agent", event.AgentID)
	}
	return nil
}
