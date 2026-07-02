package agent

import (
	"context"
	"encoding/json"
	"log/slog"
	"slices"
	"time"

	"github.com/google/uuid"

	"github.com/nextlevelbuilder/goclaw/internal/store"
	"github.com/nextlevelbuilder/goclaw/internal/tools"
	"github.com/nextlevelbuilder/goclaw/internal/tracing"
)

type mcpUsageTool interface {
	ServerName() string
	OriginalName() string
}

func (l *Loop) canonicalToolName(name string) string {
	if l.registry == nil {
		return name
	}
	if tool, ok := l.registry.Get(name); ok && tool != nil {
		return tool.Name()
	}
	return name
}

func (l *Loop) recordToolUsageEvent(ctx context.Context, req *RunRequest, canonicalName, rawName, toolCallID string, args map[string]any, start time.Time, result *tools.Result, spanID uuid.UUID) {
	if l.usageEvents == nil || result == nil {
		return
	}
	if tracing.TraceIDFromContext(ctx) == uuid.Nil {
		return
	}

	resourceName := canonicalName
	resourceID := canonicalName
	eventType := store.UsageEventTypeToolCall
	resourceType := store.UsageResourceTypeTool
	source := store.UsageSourceToolCall
	metadata := map[string]any{}

	if canonicalName == "use_skill" {
		eventType = store.UsageEventTypeSkillActivation
		resourceType = store.UsageResourceTypeSkill
		source = store.UsageSourceUseSkill
		if skill, _ := args["name"].(string); skill != "" {
			resourceName = skill
			resourceID = skill
		}
	} else if l.registry != nil {
		tool, ok := l.registry.Get(canonicalName)
		if !ok {
			tool = nil
		}
		if mcpTool, ok := tool.(mcpUsageTool); ok {
			eventType = store.UsageEventTypeMCPToolCall
			resourceType = store.UsageResourceTypeMCPTool
			resourceName = mcpTool.ServerName() + "/" + mcpTool.OriginalName()
			resourceID = mcpTool.OriginalName()
			metadata["server"] = mcpTool.ServerName()
			metadata["tool"] = mcpTool.OriginalName()
		} else if l.isRuntimeTool(canonicalName) {
			eventType = store.UsageEventTypeRuntimeToolCall
			resourceType = store.UsageResourceTypeRuntimeTool
		}
	}

	if rawName != "" && rawName != canonicalName {
		metadata["raw_tool_name"] = rawName
	}

	event := l.baseUsageEvent(ctx, req, start, eventType, resourceType, resourceName, resourceID, source)
	event.SpanID = uuidPtr(spanID)
	event.Status = "completed"
	event.Provider = result.Provider
	event.Model = result.Model
	if result.IsError {
		event.Status = "error"
		event.ErrorCount = 1
	}
	event.DurationMS = int(time.Since(start).Milliseconds())
	if result.Usage != nil {
		event.InputTokens = int64(result.Usage.PromptTokens)
		event.OutputTokens = int64(result.Usage.CompletionTokens)
		event.TotalTokens = int64(result.Usage.TotalTokens)
		event.CacheReadTokens = int64(result.Usage.CacheReadTokens)
		event.CacheCreateTokens = int64(result.Usage.CacheCreationTokens)
		event.ThinkingTokens = int64(result.Usage.ThinkingTokens)
		if event.TotalTokens == 0 {
			event.TotalTokens = event.InputTokens + event.OutputTokens
		}
		if result.Usage.PromptTokensIncludeCachedSegments {
			metadata["prompt_tokens_include_cached_segments"] = true
		}
		event.CostUSD = l.calculateLLMCost(ctx, event.Provider, event.Model, result.Usage)
	}
	event.Metadata = usageMetadata(metadata)
	l.insertUsageEventBestEffort(ctx, event)
}

func (l *Loop) recordSkillSlashUsageEvent(ctx context.Context, skillSlug string) {
	if l.usageEvents == nil || skillSlug == "" {
		return
	}
	traceID := tracing.TraceIDFromContext(ctx)
	if traceID == uuid.Nil {
		return
	}
	rc := store.RunContextFromCtx(ctx)
	event := l.baseUsageEvent(ctx, nil, time.Now().UTC(),
		store.UsageEventTypeSkillActivation,
		store.UsageResourceTypeSkill,
		skillSlug,
		skillSlug,
		store.UsageSourceSlashCommand,
	)
	event.TraceID = uuidPtr(traceID)
	if rc != nil {
		event.RunID = rc.RunID
		event.SessionKey = rc.SessionKey
		event.Channel = rc.Channel
		if rc.TeamID != "" {
			if teamID, err := uuid.Parse(rc.TeamID); err == nil {
				event.TeamID = &teamID
			}
		}
	}
	event.Metadata = usageMetadata(map[string]any{"activation_source": store.UsageSourceSlashCommand})
	l.insertUsageEventBestEffort(ctx, event)
}

func (l *Loop) baseUsageEvent(ctx context.Context, req *RunRequest, eventTime time.Time, eventType, resourceType, resourceName, resourceID, source string) store.UsageEvent {
	tenantID := store.TenantIDFromContext(ctx)
	if tenantID == uuid.Nil {
		tenantID = l.tenantID
	}
	traceID := tracing.TraceIDFromContext(ctx)
	event := store.UsageEvent{
		ID:           uuid.New(),
		TenantID:     tenantID,
		EventTime:    eventTime.UTC(),
		BucketHour:   eventTime.UTC().Truncate(time.Hour),
		EventType:    eventType,
		ResourceType: resourceType,
		ResourceName: resourceName,
		ResourceID:   resourceID,
		Source:       source,
		AgentID:      uuidPtr(l.agentUUID),
		TeamID:       tracing.TraceTeamIDPtrFromContext(ctx),
		TraceID:      uuidPtr(traceID),
		Status:       "completed",
		CallCount:    1,
	}
	if req != nil {
		event.RunID = req.RunID
		event.SessionKey = req.SessionKey
		event.Channel = req.Channel
		if req.TeamID != "" && event.TeamID == nil {
			if teamID, err := uuid.Parse(req.TeamID); err == nil {
				event.TeamID = &teamID
			}
		}
	}
	return event
}

func (l *Loop) isRuntimeTool(toolName string) bool {
	if l.registry == nil {
		return false
	}
	members, ok := l.registry.GetToolGroup("runtime")
	return ok && slices.Contains(members, toolName)
}

func (l *Loop) insertUsageEventBestEffort(ctx context.Context, event store.UsageEvent) {
	tenantID := event.TenantID
	go func() {
		bgCtx, cancel := context.WithTimeout(store.WithTenantID(context.Background(), tenantID), 5*time.Second)
		defer cancel()
		if err := l.usageEvents.InsertEvent(bgCtx, &event); err != nil {
			slog.Debug("usage.event.record_failed", "resource", event.ResourceName, "error", err)
		}
	}()
}

func uuidPtr(id uuid.UUID) *uuid.UUID {
	if id == uuid.Nil {
		return nil
	}
	return &id
}

func usageMetadata(values map[string]any) json.RawMessage {
	if len(values) == 0 {
		return nil
	}
	data, err := json.Marshal(values)
	if err != nil {
		return nil
	}
	return data
}
