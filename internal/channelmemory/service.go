package channelmemory

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/nextlevelbuilder/goclaw/internal/eventbus"
	"github.com/nextlevelbuilder/goclaw/internal/providerresolve"
	"github.com/nextlevelbuilder/goclaw/internal/providers"
	"github.com/nextlevelbuilder/goclaw/internal/store"
	usagecaps "github.com/nextlevelbuilder/goclaw/internal/usage/caps"
)

type Service struct {
	Channels      store.ChannelInstanceStore
	Pending       store.PendingMessageStore
	Extractions   store.ChannelMemoryExtractionStore
	Episodic      store.EpisodicStore
	EventBus      eventbus.DomainEventBus
	SystemConfigs store.SystemConfigStore
	Registry      *providers.Registry
	UsageCaps     *usagecaps.Service
	Redactor      *Redactor
}

type Status struct {
	Config       Config                              `json:"config"`
	LastRun      *store.ChannelMemoryExtractionRun   `json:"last_run,omitempty"`
	PendingCount int                                 `json:"pending_count"`
	RecentItems  []store.ChannelMemoryExtractionItem `json:"recent_items"`
}

func (s *Service) Status(ctx context.Context, inst *store.ChannelInstanceData) (*Status, error) {
	cfg := ParseConfig(inst.Config)
	runs, err := s.Extractions.ListRuns(ctx, store.ChannelMemoryRunListOptions{ChannelInstanceID: inst.ID, Limit: 1})
	if err != nil {
		return nil, err
	}
	items, err := s.Extractions.ListItems(ctx, store.ChannelMemoryItemListOptions{ChannelInstanceID: inst.ID, Limit: 25})
	if err != nil {
		return nil, err
	}
	pending := 0
	for _, item := range items {
		if item.Status == store.ChannelMemoryItemPendingReview {
			pending++
		}
	}
	var last *store.ChannelMemoryExtractionRun
	if len(runs) > 0 {
		last = &runs[0]
	}
	return &Status{Config: cfg, LastRun: last, PendingCount: pending, RecentItems: items}, nil
}

func (s *Service) RunNow(ctx context.Context, inst *store.ChannelInstanceData, trigger string) (*store.ChannelMemoryExtractionRun, error) {
	cfg := ParseConfig(inst.Config)
	if !cfg.Enabled && trigger != "manual" {
		return nil, fmt.Errorf("passive memory disabled")
	}
	groups, err := s.Pending.ListGroups(ctx)
	if err != nil {
		return nil, err
	}
	for _, group := range groups {
		if group.ChannelName != inst.Name || !eligibleHistoryKey(group.HistoryKey, cfg) {
			continue
		}
		if trigger != "manual" && !s.shouldRunScheduled(ctx, inst.ID, group, cfg) {
			continue
		}
		return s.runGroup(ctx, inst, cfg, group, trigger)
	}
	return nil, fmt.Errorf("no eligible channel messages")
}

func (s *Service) shouldRunScheduled(ctx context.Context, instID uuid.UUID, group store.PendingMessageGroup, cfg Config) bool {
	if group.MessageCount >= cfg.MessageCap {
		return true
	}
	runs, err := s.Extractions.ListRuns(ctx, store.ChannelMemoryRunListOptions{
		ChannelInstanceID: instID,
		HistoryKey:        group.HistoryKey,
		Status:            store.ChannelMemoryRunCompleted,
		Limit:             1,
	})
	if err != nil || len(runs) == 0 {
		return true
	}
	return time.Since(runs[0].CreatedAt) >= cfg.Interval()
}

func (s *Service) runGroup(ctx context.Context, inst *store.ChannelInstanceData, cfg Config, group store.PendingMessageGroup, trigger string) (*store.ChannelMemoryExtractionRun, error) {
	messages, err := s.Pending.ListByKey(ctx, group.ChannelName, group.HistoryKey)
	if err != nil {
		return nil, err
	}
	if len(messages) < cfg.MinMessages {
		return nil, fmt.Errorf("not enough useful messages")
	}
	redactor := s.Redactor
	if redactor == nil {
		redactor = NewRedactor()
	}
	redacted := redactor.Redact(messages, cfg)
	if len(redacted.Messages) < cfg.MinMessages {
		return nil, fmt.Errorf("not enough redacted messages")
	}
	start, end := redacted.Messages[0], redacted.Messages[len(redacted.Messages)-1]
	redactionTypes, _ := json.Marshal(redacted.Types)
	run := &store.ChannelMemoryExtractionRun{
		ChannelInstanceID: inst.ID,
		ChannelName:       inst.Name,
		AgentID:           inst.AgentID,
		UserID:            inst.CreatedBy,
		HistoryKey:        group.HistoryKey,
		Trigger:           trigger,
		Status:            store.ChannelMemoryRunRunning,
		SourceStartID:     messageSourceID(start),
		SourceEndID:       messageSourceID(end),
		SourceStartAt:     &start.CreatedAt,
		SourceEndAt:       &end.CreatedAt,
		MessageCount:      len(redacted.Messages),
		RedactionCount:    redacted.Count,
		RedactionTypes:    redactionTypes,
		StartedAt:         new(time.Now().UTC()),
	}
	if err := s.Extractions.CreateRun(ctx, run); err != nil {
		return nil, err
	}
	provider, model := providerresolve.ResolveBackgroundProvider(ctx, run.TenantID, s.Registry, s.SystemConfigs)
	items, err := Extract(ctx, provider, model, s.UsageCaps, redacted.Messages, cfg.AllowedTypes)
	if err != nil {
		_ = s.Extractions.UpdateRun(ctx, run.ID, map[string]any{
			"status": store.ChannelMemoryRunFailed, "error_message": err.Error(), "completed_at": time.Now().UTC(),
		})
		return run, err
	}
	for _, extracted := range items {
		if !contains(cfg.AllowedTypes, extracted.Type) {
			continue
		}
		item := s.itemFromExtracted(run, extracted)
		if err := s.Extractions.CreateItem(ctx, item); err != nil {
			return run, err
		}
		if !cfg.ReviewMode {
			if _, err := s.Approve(ctx, item.ID, "system"); err != nil {
				return run, err
			}
		}
		run.ItemCount++
	}
	status := store.ChannelMemoryRunCompleted
	_ = s.Extractions.UpdateRun(ctx, run.ID, map[string]any{
		"status": status, "item_count": run.ItemCount, "completed_at": time.Now().UTC(),
	})
	run.Status = status
	return run, nil
}
