package channelmemory

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/nextlevelbuilder/goclaw/internal/eventbus"
	"github.com/nextlevelbuilder/goclaw/internal/store"
)

func (s *Service) Approve(ctx context.Context, itemID uuid.UUID, approver string) (*store.ChannelMemoryExtractionItem, error) {
	item, err := s.Extractions.GetItem(ctx, itemID)
	if err != nil {
		return nil, err
	}
	if item.Status == store.ChannelMemoryItemWritten {
		return item, nil
	}
	if item.Status != store.ChannelMemoryItemPendingReview && item.Status != store.ChannelMemoryItemApproved {
		return nil, fmt.Errorf("item is not approvable")
	}
	sourceID := item.SourceID
	// Passive channel extraction is reusable channel context, not a personal
	// memory of the channel instance creator or current sender.
	memoryUserID := ""
	exists, err := s.Episodic.ExistsBySourceID(ctx, item.AgentID.String(), memoryUserID, sourceID)
	if err != nil {
		return nil, err
	}
	if !exists {
		retention := s.retentionDuration(ctx, item)
		keyTopics := memoryKeyTopics(item)
		ep := &store.EpisodicSummary{
			TenantID:   item.TenantID,
			AgentID:    item.AgentID,
			UserID:     memoryUserID,
			SessionKey: "channel:" + item.ChannelInstanceID.String(),
			Summary:    item.Summary,
			KeyTopics:  keyTopics,
			L0Abstract: item.Summary,
			SourceID:   sourceID,
			SourceType: "channel",
			ExpiresAt:  timePtr(time.Now().UTC().Add(retention)),
		}
		if err := s.Episodic.Create(ctx, ep); err != nil {
			return nil, err
		}
		item.EpisodicID = ep.ID.String()
		if s.EventBus != nil {
			s.EventBus.Publish(eventbus.DomainEvent{
				Type:     eventbus.EventEpisodicCreated,
				SourceID: ep.ID.String(),
				TenantID: item.TenantID.String(),
				AgentID:  item.AgentID.String(),
				UserID:   item.UserID,
				Payload: &eventbus.EpisodicCreatedPayload{
					EpisodicID:  ep.ID.String(),
					SessionKey:  ep.SessionKey,
					Summary:     item.Summary,
					KeyTopics:   keyTopics,
					KeyEntities: decodeStrings(item.Entities),
				},
			})
		}
	} else {
		ep, err := s.Episodic.GetBySourceID(ctx, item.AgentID.String(), memoryUserID, sourceID)
		if err != nil {
			return nil, err
		}
		if ep != nil {
			item.EpisodicID = ep.ID.String()
		}
	}
	now := time.Now().UTC()
	if err := s.Extractions.UpdateItem(ctx, item.ID, map[string]any{
		"status":      store.ChannelMemoryItemWritten,
		"approved_by": approver,
		"approved_at": now,
		"written_at":  now,
		"episodic_id": item.EpisodicID,
	}); err != nil {
		return nil, err
	}
	item.Status = store.ChannelMemoryItemWritten
	return item, nil
}

func (s *Service) retentionDuration(ctx context.Context, item *store.ChannelMemoryExtractionItem) time.Duration {
	cfg := DefaultConfig()
	if s.Channels != nil && item != nil {
		inst, err := s.Channels.Get(ctx, item.ChannelInstanceID)
		if err == nil && inst != nil {
			cfg = ParseConfig(inst.Config)
		}
	}
	return time.Duration(cfg.RetentionHours) * time.Hour
}

func (s *Service) Reject(ctx context.Context, itemID uuid.UUID, actor string) error {
	return s.updateItemTerminal(ctx, itemID, store.ChannelMemoryItemRejected, map[string]any{"rejected_by": actor, "rejected_at": time.Now().UTC()})
}

func (s *Service) Delete(ctx context.Context, itemID uuid.UUID) error {
	item, err := s.Extractions.GetItem(ctx, itemID)
	if err != nil {
		return err
	}
	if item.EpisodicID != "" && s.Episodic != nil {
		if err := s.Episodic.Delete(ctx, item.EpisodicID); err != nil {
			return err
		}
	}
	return s.updateItemTerminal(ctx, itemID, store.ChannelMemoryItemDeleted, map[string]any{"deleted_at": time.Now().UTC()})
}

func (s *Service) updateItemTerminal(ctx context.Context, itemID uuid.UUID, status string, updates map[string]any) error {
	item, err := s.Extractions.GetItem(ctx, itemID)
	if err != nil {
		return err
	}
	if item.Status == store.ChannelMemoryItemWritten && status != store.ChannelMemoryItemDeleted {
		return fmt.Errorf("written item cannot transition to %s", status)
	}
	updates["status"] = status
	return s.Extractions.UpdateItem(ctx, itemID, updates)
}
