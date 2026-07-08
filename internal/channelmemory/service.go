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
	Config                  Config                              `json:"config"`
	LastRun                 *store.ChannelMemoryExtractionRun   `json:"last_run,omitempty"`
	PendingCount            int                                 `json:"pending_count"`
	UnprocessedMessageCount int                                 `json:"unprocessed_message_count"`
	RecentItems             []store.ChannelMemoryExtractionItem `json:"recent_items"`
}

type ProcessAllResult struct {
	Runs              []store.ChannelMemoryExtractionRun `json:"runs"`
	RunCount          int                                `json:"run_count"`
	MessageCount      int                                `json:"message_count"`
	ItemCount         int                                `json:"item_count"`
	SkippedGroupCount int                                `json:"skipped_group_count"`
	ErrorCount        int                                `json:"error_count"`
}

type ProcessAllEvent struct {
	Type              string                            `json:"type"`
	ChannelName       string                            `json:"channel_name,omitempty"`
	HistoryKey        string                            `json:"history_key,omitempty"`
	GroupMessageCount int                               `json:"group_message_count,omitempty"`
	Run               *store.ChannelMemoryExtractionRun `json:"run,omitempty"`
	Error             string                            `json:"error,omitempty"`
	RunCount          int                               `json:"run_count"`
	MessageCount      int                               `json:"message_count"`
	ItemCount         int                               `json:"item_count"`
	SkippedGroupCount int                               `json:"skipped_group_count"`
	ErrorCount        int                               `json:"error_count"`
}

type GroupOption struct {
	ChannelName      string    `json:"channel_name"`
	HistoryKey       string    `json:"history_key"`
	ParentHistoryKey string    `json:"parent_history_key,omitempty"`
	GroupTitle       string    `json:"group_title,omitempty"`
	MessageCount     int       `json:"message_count"`
	LastActivity     time.Time `json:"last_activity"`
	Excluded         bool      `json:"excluded"`
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
	pending, err := s.Extractions.CountItems(ctx, store.ChannelMemoryItemListOptions{
		ChannelInstanceID: inst.ID,
		Status:            store.ChannelMemoryItemPendingReview,
	})
	if err != nil {
		return nil, err
	}
	unprocessed, err := s.UnprocessedMessageCount(ctx, inst)
	if err != nil {
		return nil, err
	}
	var last *store.ChannelMemoryExtractionRun
	if len(runs) > 0 {
		last = &runs[0]
	}
	return &Status{Config: cfg, LastRun: last, PendingCount: pending, UnprocessedMessageCount: unprocessed, RecentItems: items}, nil
}

func (s *Service) GroupOptions(ctx context.Context, inst *store.ChannelInstanceData) ([]GroupOption, error) {
	cfg := ParseConfig(inst.Config)
	groups, err := s.Pending.ListGroups(ctx)
	if err != nil {
		return nil, err
	}
	titles, err := s.Pending.ResolveGroupTitles(ctx, groups)
	if err != nil {
		titles = nil
	}
	out := make([]GroupOption, 0, len(groups))
	for _, group := range groups {
		if group.ChannelName != inst.Name || group.HistoryKey == "" {
			continue
		}
		out = append(out, GroupOption{
			ChannelName:      group.ChannelName,
			HistoryKey:       group.HistoryKey,
			ParentHistoryKey: group.ParentHistoryKey,
			GroupTitle:       titles[group.ChannelName+":"+group.HistoryKey],
			MessageCount:     group.MessageCount,
			LastActivity:     group.LastActivity,
			Excluded:         contains(cfg.ExcludeHistoryKeys, group.HistoryKey) || contains(cfg.ExcludeHistoryKeys, group.ParentHistoryKey),
		})
	}
	return out, nil
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
		if group.ChannelName != inst.Name || !eligibleHistoryGroup(group, cfg) {
			continue
		}
		messages, err := s.unprocessedMessages(ctx, inst.ID, group)
		if err != nil {
			return nil, err
		}
		if len(messages) == 0 {
			continue
		}
		if trigger != "manual" && !s.shouldRunScheduled(ctx, inst.ID, group, cfg, len(messages)) {
			continue
		}
		return s.runMessages(ctx, inst, cfg, group, messages, trigger)
	}
	return nil, fmt.Errorf("no eligible channel messages")
}

func (s *Service) RunAll(ctx context.Context, inst *store.ChannelInstanceData, trigger string) (*ProcessAllResult, error) {
	return s.RunAllWithProgress(ctx, inst, trigger, nil)
}

func (s *Service) RunAllWithProgress(ctx context.Context, inst *store.ChannelInstanceData, trigger string, emit func(ProcessAllEvent) error) (*ProcessAllResult, error) {
	cfg := ParseConfig(inst.Config)
	if !cfg.Enabled && trigger != "manual_all" {
		return nil, fmt.Errorf("passive memory disabled")
	}
	groups, err := s.Pending.ListGroups(ctx)
	if err != nil {
		return nil, err
	}
	result := &ProcessAllResult{}
	for _, group := range groups {
		if group.ChannelName != inst.Name || !eligibleHistoryGroup(group, cfg) {
			continue
		}
		messages, err := s.unprocessedMessages(ctx, inst.ID, group)
		if err != nil {
			return result, err
		}
		if len(messages) == 0 {
			continue
		}
		if trigger != "manual_all" && !s.shouldRunScheduled(ctx, inst.ID, group, cfg, len(messages)) {
			continue
		}
		if len(messages) < cfg.MinMessages {
			result.SkippedGroupCount++
			if err := emitProcessAllEvent(emit, "group_skipped", group, len(messages), nil, "", result); err != nil {
				return result, err
			}
			continue
		}
		run, err := s.runMessages(ctx, inst, cfg, group, messages, trigger)
		if err != nil {
			result.ErrorCount++
			if emitErr := emitProcessAllEvent(emit, "group_failed", group, len(messages), nil, err.Error(), result); emitErr != nil {
				return result, emitErr
			}
			continue
		}
		result.Runs = append(result.Runs, *run)
		result.RunCount++
		result.MessageCount += run.MessageCount
		result.ItemCount += run.ItemCount
		if err := emitProcessAllEvent(emit, "group_completed", group, run.MessageCount, run, "", result); err != nil {
			return result, err
		}
	}
	if err := emitProcessAllEvent(emit, "final", store.PendingMessageGroup{}, 0, nil, "", result); err != nil {
		return result, err
	}
	return result, nil
}

func emitProcessAllEvent(emit func(ProcessAllEvent) error, typ string, group store.PendingMessageGroup, groupMessageCount int, run *store.ChannelMemoryExtractionRun, errMsg string, result *ProcessAllResult) error {
	if emit == nil {
		return nil
	}
	return emit(ProcessAllEvent{
		Type:              typ,
		ChannelName:       group.ChannelName,
		HistoryKey:        group.HistoryKey,
		GroupMessageCount: groupMessageCount,
		Run:               run,
		Error:             errMsg,
		RunCount:          result.RunCount,
		MessageCount:      result.MessageCount,
		ItemCount:         result.ItemCount,
		SkippedGroupCount: result.SkippedGroupCount,
		ErrorCount:        result.ErrorCount,
	})
}

func (s *Service) UnprocessedMessageCount(ctx context.Context, inst *store.ChannelInstanceData) (int, error) {
	cfg := ParseConfig(inst.Config)
	groups, err := s.Pending.ListGroups(ctx)
	if err != nil {
		return 0, err
	}
	total := 0
	for _, group := range groups {
		if group.ChannelName != inst.Name || !eligibleHistoryGroup(group, cfg) {
			continue
		}
		messages, err := s.unprocessedMessages(ctx, inst.ID, group)
		if err != nil {
			return 0, err
		}
		total += len(messages)
	}
	return total, nil
}

func (s *Service) shouldRunScheduled(ctx context.Context, instID uuid.UUID, group store.PendingMessageGroup, cfg Config, unprocessedCount int) bool {
	if unprocessedCount >= cfg.MessageCap {
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

func (s *Service) unprocessedMessages(ctx context.Context, instID uuid.UUID, group store.PendingMessageGroup) ([]store.PendingMessage, error) {
	messages, err := s.Pending.ListByKey(ctx, group.ChannelName, group.HistoryKey)
	if err != nil {
		return nil, err
	}
	if len(messages) == 0 {
		return nil, nil
	}
	runs, err := s.Extractions.ListRuns(ctx, store.ChannelMemoryRunListOptions{
		ChannelInstanceID: instID,
		HistoryKey:        group.HistoryKey,
		Status:            store.ChannelMemoryRunCompleted,
		Limit:             1,
	})
	if err != nil {
		return nil, err
	}
	if len(runs) == 0 {
		return messages, nil
	}
	run := runs[0]
	for idx, msg := range messages {
		if messageSourceID(msg) == run.SourceEndID {
			return messages[idx+1:], nil
		}
	}
	if run.SourceEndAt == nil {
		return messages, nil
	}
	for idx, msg := range messages {
		if msg.CreatedAt.After(*run.SourceEndAt) {
			return messages[idx:], nil
		}
	}
	return nil, nil
}

func (s *Service) runMessages(ctx context.Context, inst *store.ChannelInstanceData, cfg Config, group store.PendingMessageGroup, messages []store.PendingMessage, trigger string) (run *store.ChannelMemoryExtractionRun, err error) {
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
	consumed := messagesWithinExtractionBudget(redacted.Messages, extractionRetryMaxInputChars)
	if len(consumed) < cfg.MinMessages {
		return nil, fmt.Errorf("not enough extractable messages")
	}
	start, end := consumed[0], consumed[len(consumed)-1]
	redactionTypes, _ := json.Marshal(redacted.Types)
	run = &store.ChannelMemoryExtractionRun{
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
		MessageCount:      len(consumed),
		RedactionCount:    redacted.Count,
		RedactionTypes:    redactionTypes,
		StartedAt:         timePtr(time.Now().UTC()),
	}
	if err := s.Extractions.CreateRun(ctx, run); err != nil {
		return nil, err
	}
	completed := false
	defer func() {
		if err == nil || completed {
			return
		}
		_ = s.Extractions.UpdateRun(ctx, run.ID, map[string]any{
			"status":        store.ChannelMemoryRunFailed,
			"error_message": err.Error(),
			"item_count":    run.ItemCount,
			"completed_at":  time.Now().UTC(),
		})
		run.Status = store.ChannelMemoryRunFailed
		run.ErrorMessage = err.Error()
	}()
	provider, model := providerresolve.ResolveBackgroundProvider(ctx, run.TenantID, s.Registry, s.SystemConfigs)
	items, err := Extract(ctx, provider, model, s.UsageCaps, consumed, cfg.AllowedTypes)
	if err != nil {
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
	completed = true
	return run, nil
}
