package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/google/uuid"

	"github.com/nextlevelbuilder/goclaw/internal/providers"
	"github.com/nextlevelbuilder/goclaw/internal/store"
	"github.com/nextlevelbuilder/goclaw/internal/tracing"
)

// ---------------------------------------------------------------------------
// Two-phase subagent LLM span: start (running) + end (completed/error)
// ---------------------------------------------------------------------------

// emitLLMSpanStart emits a "running" LLM span before the subagent LLM call.
// providerName is the resolved provider (may differ from sm.provider for inherited providers).
func (sm *SubagentManager) emitLLMSpanStart(ctx context.Context, start time.Time, iteration int, model, providerName string, messages []providers.Message) uuid.UUID {
	collector := tracing.CollectorFromContext(ctx)
	traceID := tracing.TraceIDFromContext(ctx)
	if collector == nil || traceID == uuid.Nil {
		return uuid.Nil
	}

	spanID := store.GenNewID()
	span := store.SpanData{
		ID:        spanID,
		TraceID:   traceID,
		SpanType:  store.SpanTypeLLMCall,
		Name:      fmt.Sprintf("%s/%s #%d", providerName, model, iteration),
		StartTime: start,
		Status:    store.SpanStatusRunning,
		Level:     store.SpanLevelDefault,
		Model:     model,
		Provider:  providerName,
		CreatedAt: start,
	}
	if parentID := tracing.ParentSpanIDFromContext(ctx); parentID != uuid.Nil {
		span.ParentSpanID = &parentID
	}
	span.TeamID = tracing.TraceTeamIDPtrFromContext(ctx)
	if collector.Verbose() && len(messages) > 0 {
		if b, err := json.Marshal(messages); err == nil {
			span.InputPreview = truncate(string(b), 100000)
		}
	}
	collector.EmitSpan(span)
	return spanID
}

// emitLLMSpanEnd finalizes a running subagent LLM span.
func (sm *SubagentManager) emitLLMSpanEnd(ctx context.Context, spanID uuid.UUID, start time.Time, resp *providers.ChatResponse, callErr error) {
	if spanID == uuid.Nil {
		return
	}
	collector := tracing.CollectorFromContext(ctx)
	traceID := tracing.TraceIDFromContext(ctx)
	if collector == nil || traceID == uuid.Nil {
		return
	}

	now := time.Now().UTC()
	updates := map[string]any{
		"end_time":    now,
		"duration_ms": int(now.Sub(start).Milliseconds()),
		"status":      store.SpanStatusCompleted,
	}
	if callErr != nil {
		updates["status"] = store.SpanStatusError
		updates["error"] = callErr.Error()
	} else if resp != nil {
		if resp.Usage != nil {
			updates["input_tokens"] = resp.Usage.PromptTokens
			updates["output_tokens"] = resp.Usage.CompletionTokens
			if resp.Usage.CacheCreationTokens > 0 || resp.Usage.CacheReadTokens > 0 {
				meta := map[string]int{
					"cache_creation_tokens": resp.Usage.CacheCreationTokens,
					"cache_read_tokens":     resp.Usage.CacheReadTokens,
				}
				if b, err := json.Marshal(meta); err == nil {
					updates["metadata"] = b
				}
			}
		}
		updates["finish_reason"] = resp.FinishReason
		previewLimit := 500
		if collector.Verbose() {
			previewLimit = 100000
		}
		updates["output_preview"] = truncate(resp.Content, previewLimit)
	}
	collector.EmitSpanUpdate(spanID, traceID, updates)
}

// ---------------------------------------------------------------------------
// Two-phase subagent tool span: start (running) + end (completed/error)
// ---------------------------------------------------------------------------

// emitToolSpanStart emits a "running" tool span before subagent tool execution.
func (sm *SubagentManager) emitToolSpanStart(ctx context.Context, start time.Time, toolName, toolCallID, input string) uuid.UUID {
	collector := tracing.CollectorFromContext(ctx)
	traceID := tracing.TraceIDFromContext(ctx)
	if collector == nil || traceID == uuid.Nil {
		return uuid.Nil
	}

	previewLimit := 500
	if collector.Verbose() {
		previewLimit = 100000
	}

	spanID := store.GenNewID()
	span := store.SpanData{
		ID:           spanID,
		TraceID:      traceID,
		SpanType:     store.SpanTypeToolCall,
		Name:         toolName,
		StartTime:    start,
		ToolName:     toolName,
		ToolCallID:   toolCallID,
		InputPreview: truncate(input, previewLimit),
		Status:       store.SpanStatusRunning,
		Level:        store.SpanLevelDefault,
		CreatedAt:    start,
	}
	if parentID := tracing.ParentSpanIDFromContext(ctx); parentID != uuid.Nil {
		span.ParentSpanID = &parentID
	}
	span.TeamID = tracing.TraceTeamIDPtrFromContext(ctx)
	collector.EmitSpan(span)
	return spanID
}

// emitToolSpanEnd finalizes a running subagent tool span.
func (sm *SubagentManager) emitToolSpanEnd(ctx context.Context, spanID uuid.UUID, start time.Time, output string, isError bool) {
	if spanID == uuid.Nil {
		return
	}
	collector := tracing.CollectorFromContext(ctx)
	traceID := tracing.TraceIDFromContext(ctx)
	if collector == nil || traceID == uuid.Nil {
		return
	}

	now := time.Now().UTC()
	previewLimit := 500
	if collector.Verbose() {
		previewLimit = 100000
	}
	updates := map[string]any{
		"end_time":       now,
		"duration_ms":    int(now.Sub(start).Milliseconds()),
		"status":         store.SpanStatusCompleted,
		"output_preview": truncate(output, previewLimit),
	}
	if isError {
		updates["status"] = store.SpanStatusError
		updates["error"] = truncate(output, 200)
	}
	collector.EmitSpanUpdate(spanID, traceID, updates)
}

// ---------------------------------------------------------------------------
// Two-phase subagent root span: start (running) + end (completed/error)
// ---------------------------------------------------------------------------

// emitSubagentSpanStart emits a "running" root subagent span at task start.
// providerName is the resolved provider (may differ from sm.provider for inherited providers).
func (sm *SubagentManager) emitSubagentSpanStart(ctx context.Context, spanID uuid.UUID, start time.Time, task *SubagentTask, model, providerName string) {
	collector := tracing.CollectorFromContext(ctx)
	traceID := tracing.TraceIDFromContext(ctx)
	if collector == nil || traceID == uuid.Nil {
		return
	}

	previewLimit := 500
	if collector.Verbose() {
		previewLimit = 100000
	}
	span := store.SpanData{
		ID:           spanID,
		TraceID:      traceID,
		SpanType:     store.SpanTypeAgent,
		Name:         fmt.Sprintf("subagent:%s", task.Label),
		StartTime:    start,
		Status:       store.SpanStatusRunning,
		Level:        store.SpanLevelDefault,
		Model:        model,
		Provider:     providerName,
		InputPreview: truncate(task.Task, previewLimit),
		CreatedAt:    start,
	}
	if parentSpanID := tracing.ParentSpanIDFromContext(ctx); parentSpanID != uuid.Nil {
		span.ParentSpanID = &parentSpanID
	}
	span.TeamID = tracing.TraceTeamIDPtrFromContext(ctx)
	collector.EmitSpan(span)
}

// emitSubagentSpanEnd finalizes the running subagent root span.
func (sm *SubagentManager) emitSubagentSpanEnd(ctx context.Context, spanID uuid.UUID, start time.Time, task *SubagentTask, output string) {
	if spanID == uuid.Nil {
		return
	}
	collector := tracing.CollectorFromContext(ctx)
	traceID := tracing.TraceIDFromContext(ctx)
	if collector == nil || traceID == uuid.Nil {
		return
	}

	now := time.Now().UTC()
	previewLimit := 500
	if collector.Verbose() {
		previewLimit = 100000
	}
	updates := map[string]any{
		"end_time":       now,
		"duration_ms":    int(now.Sub(start).Milliseconds()),
		"status":         store.SpanStatusCompleted,
		"output_preview": truncate(output, previewLimit),
	}
	if task.Status == TaskStatusFailed || task.Status == TaskStatusCancelled {
		updates["status"] = store.SpanStatusError
		updates["error"] = truncate(task.Result, 200)
	}
	collector.EmitSpanUpdate(spanID, traceID, updates)
}
