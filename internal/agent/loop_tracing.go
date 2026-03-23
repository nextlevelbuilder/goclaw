package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"

	"github.com/nextlevelbuilder/goclaw/internal/providers"
	"github.com/nextlevelbuilder/goclaw/internal/store"
	"github.com/nextlevelbuilder/goclaw/internal/tools"
	"github.com/nextlevelbuilder/goclaw/internal/tracing"
)

func (l *Loop) emit(event AgentEvent) {
	if l.onEvent != nil {
		l.onEvent(event)
	}
}

// ID returns the agent's identifier.
func (l *Loop) ID() string { return l.id }

// Model returns the model identifier for this agent loop.
func (l *Loop) Model() string { return l.model }

// IsRunning returns whether the agent is currently processing.
func (l *Loop) IsRunning() bool { return l.activeRuns.Load() > 0 }

// ---------------------------------------------------------------------------
// Two-phase LLM span: start (running) + end (completed/error)
// ---------------------------------------------------------------------------

// emitLLMSpanStart emits a "running" LLM span before the LLM call begins.
// Returns the span ID so the caller can later call emitLLMSpanEnd to finalize it.
// Goroutine-safe: only reads immutable Loop fields and does a channel send.
func (l *Loop) emitLLMSpanStart(ctx context.Context, start time.Time, iteration int, messages []providers.Message) uuid.UUID {
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
		Name:      fmt.Sprintf("%s/%s #%d", l.provider.Name(), l.model, iteration),
		StartTime: start,
		Status:    store.SpanStatusRunning,
		Level:     store.SpanLevelDefault,
		Model:     l.model,
		Provider:  l.provider.Name(),
		CreatedAt: start,
	}
	if parentID := tracing.ParentSpanIDFromContext(ctx); parentID != uuid.Nil {
		span.ParentSpanID = &parentID
	}
	if l.agentUUID != uuid.Nil {
		span.AgentID = &l.agentUUID
	}
	span.TeamID = tracing.TraceTeamIDPtrFromContext(ctx)
	span.TenantID = store.TenantIDFromContext(ctx)
	if span.TenantID == uuid.Nil {
		span.TenantID = store.MasterTenantID
	}

	// Verbose mode: include input messages (same stripping as emitLLMSpan).
	if collector.Verbose() && len(messages) > 0 {
		stripped := make([]providers.Message, len(messages))
		copy(stripped, messages)
		for i := range stripped {
			if len(stripped[i].Images) > 0 {
				placeholder := make([]providers.ImageContent, len(stripped[i].Images))
				for j, img := range stripped[i].Images {
					placeholder[j] = providers.ImageContent{MimeType: img.MimeType, Data: fmt.Sprintf("[base64 %s, %d bytes]", img.MimeType, len(img.Data))}
				}
				stripped[i].Images = placeholder
			}
		}
		if b, err := json.Marshal(stripped); err == nil {
			span.InputPreview = truncateStr(string(b), 100000)
		}
	}

	collector.EmitSpan(span)
	return spanID
}

// emitLLMSpanEnd finalizes a running LLM span with results.
// Uses EmitSpanUpdate (channel send) — does NOT depend on ctx being alive,
// so it works correctly even after ctx cancellation or deadline exceeded.
func (l *Loop) emitLLMSpanEnd(ctx context.Context, spanID uuid.UUID, start time.Time, resp *providers.ChatResponse, callErr error) {
	if spanID == uuid.Nil {
		return // tracing disabled — no running span was emitted
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
			hasMeta := resp.Usage.CacheCreationTokens > 0 || resp.Usage.CacheReadTokens > 0 || resp.Usage.ThinkingTokens > 0
			if hasMeta {
				meta := map[string]int{}
				if resp.Usage.CacheCreationTokens > 0 {
					meta["cache_creation_tokens"] = resp.Usage.CacheCreationTokens
				}
				if resp.Usage.CacheReadTokens > 0 {
					meta["cache_read_tokens"] = resp.Usage.CacheReadTokens
				}
				if resp.Usage.ThinkingTokens > 0 {
					meta["thinking_tokens"] = resp.Usage.ThinkingTokens
				}
				if b, err := json.Marshal(meta); err == nil {
					updates["metadata"] = b
				}
			}
		}
		// Calculate cost if pricing config is available.
		if pricing := tracing.LookupPricing(l.modelPricing, l.provider.Name(), l.model); pricing != nil {
			cost := tracing.CalculateCost(pricing, resp.Usage)
			if cost > 0 {
				updates["total_cost"] = cost
			}
		}
		updates["finish_reason"] = resp.FinishReason
		verbose := collector.Verbose()
		if verbose {
			preview := resp.Content
			if resp.Thinking != "" {
				preview = "<thinking>\n" + resp.Thinking + "\n</thinking>\n" + resp.Content
			}
			updates["output_preview"] = truncateStr(preview, 100000)
		} else {
			updates["output_preview"] = truncateStr(resp.Content, 500)
		}
	}

	collector.EmitSpanUpdate(spanID, traceID, updates)
}

// ---------------------------------------------------------------------------
// Two-phase tool span: start (running) + end (completed/error)
// ---------------------------------------------------------------------------

// emitToolSpanStart emits a "running" tool span before tool execution begins.
// Returns the span ID so the caller can later call emitToolSpanEnd to finalize it.
// Goroutine-safe: only reads immutable Loop fields and does a channel send.
func (l *Loop) emitToolSpanStart(ctx context.Context, start time.Time, toolName, toolCallID, input string) uuid.UUID {
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
		InputPreview: truncateStr(input, previewLimit),
		Status:       store.SpanStatusRunning,
		Level:        store.SpanLevelDefault,
		CreatedAt:    start,
	}
	if parentID := tracing.ParentSpanIDFromContext(ctx); parentID != uuid.Nil {
		span.ParentSpanID = &parentID
	}
	if l.agentUUID != uuid.Nil {
		span.AgentID = &l.agentUUID
	}
	span.TeamID = tracing.TraceTeamIDPtrFromContext(ctx)
	span.TenantID = store.TenantIDFromContext(ctx)
	if span.TenantID == uuid.Nil {
		span.TenantID = store.MasterTenantID
	}

	collector.EmitSpan(span)
	return spanID
}

// emitToolSpanEnd finalizes a running tool span with execution results.
// Uses EmitSpanUpdate (channel send) — safe after ctx cancellation.
// Goroutine-safe: only does a channel send via EmitSpanUpdate.
func (l *Loop) emitToolSpanEnd(ctx context.Context, spanID uuid.UUID, start time.Time, result *tools.Result) {
	if spanID == uuid.Nil {
		return // tracing disabled
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
		"output_preview": truncateStr(result.ForLLM, previewLimit),
	}

	if result.IsError {
		updates["status"] = store.SpanStatusError
		updates["error"] = truncateStr(result.ForLLM, 200)
	}

	// Record token usage from tools that make internal LLM calls (e.g. read_image).
	if result.Usage != nil {
		updates["input_tokens"] = result.Usage.PromptTokens
		updates["output_tokens"] = result.Usage.CompletionTokens
		updates["provider"] = result.Provider
		updates["model"] = result.Model
		if result.Usage.CacheCreationTokens > 0 || result.Usage.CacheReadTokens > 0 {
			meta := map[string]int{
				"cache_creation_tokens": result.Usage.CacheCreationTokens,
				"cache_read_tokens":     result.Usage.CacheReadTokens,
			}
			if b, err := json.Marshal(meta); err == nil {
				updates["metadata"] = b
			}
		}
		// Calculate cost for tool's internal LLM calls.
		provider := result.Provider
		model := result.Model
		if pricing := tracing.LookupPricing(l.modelPricing, provider, model); pricing != nil {
			cost := tracing.CalculateCost(pricing, result.Usage)
			if cost > 0 {
				updates["total_cost"] = cost
			}
		}
	}

	collector.EmitSpanUpdate(spanID, traceID, updates)
}

// ---------------------------------------------------------------------------
// Two-phase agent span: start (running) + end (completed/error)
// ---------------------------------------------------------------------------

// emitAgentSpanStart emits a "running" root agent span at the beginning of a run.
// The span is identified by agentSpanID (pre-generated, same ID used as ParentSpanID
// for child LLM/tool spans).
func (l *Loop) emitAgentSpanStart(ctx context.Context, agentSpanID uuid.UUID, start time.Time, inputPreview string) {
	collector := tracing.CollectorFromContext(ctx)
	traceID := tracing.TraceIDFromContext(ctx)
	if collector == nil || traceID == uuid.Nil {
		return
	}

	previewLimit := 500
	if collector.Verbose() {
		previewLimit = 100000
	}

	spanName := l.id
	span := store.SpanData{
		ID:           agentSpanID,
		TraceID:      traceID,
		SpanType:     store.SpanTypeAgent,
		Name:         spanName,
		StartTime:    start,
		Status:       store.SpanStatusRunning,
		Level:        store.SpanLevelDefault,
		Model:        l.model,
		Provider:     l.provider.Name(),
		InputPreview: truncateStr(inputPreview, previewLimit),
		CreatedAt:    start,
	}
	// Nest under parent root span if this is an announce run.
	if announceParent := tracing.AnnounceParentSpanIDFromContext(ctx); announceParent != uuid.Nil {
		span.ParentSpanID = &announceParent
		span.Name = "announce:" + spanName
	}
	if l.agentUUID != uuid.Nil {
		span.AgentID = &l.agentUUID
	}
	span.TeamID = tracing.TraceTeamIDPtrFromContext(ctx)
	span.TenantID = store.TenantIDFromContext(ctx)
	if span.TenantID == uuid.Nil {
		span.TenantID = store.MasterTenantID
	}

	collector.EmitSpan(span)
}

// emitAgentSpanEnd finalizes the running root agent span with results.
// Uses EmitSpanUpdate (channel send) — safe after ctx cancellation.
func (l *Loop) emitAgentSpanEnd(ctx context.Context, agentSpanID uuid.UUID, start time.Time, result *RunResult, runErr error) {
	if agentSpanID == uuid.Nil {
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

	if runErr != nil {
		updates["status"] = store.SpanStatusError
		updates["error"] = runErr.Error()
	} else if result != nil {
		limit := 500
		if collector.Verbose() {
			limit = 100000
		}
		updates["output_preview"] = truncateStr(result.Content, limit)
		// Note: token counts are NOT set on agent spans to avoid double-counting
		// with child llm_call spans. Trace aggregation sums only llm_call spans.
	}

	collector.EmitSpanUpdate(agentSpanID, traceID, updates)
}

func truncateStr(s string, maxLen int) string {
	s = strings.ToValidUTF8(s, "")
	if len(s) <= maxLen {
		return s
	}
	// Don't cut in the middle of a multi-byte rune
	for maxLen > 0 && !utf8.RuneStart(s[maxLen]) {
		maxLen--
	}
	return s[:maxLen] + "..."
}

// EstimateTokens returns a rough token estimate for a slice of messages.
// Used internally for summarization thresholds and externally for adaptive throttle.
func EstimateTokens(messages []providers.Message) int {
	total := 0
	for _, m := range messages {
		total += utf8.RuneCountInString(m.Content) / 3
	}
	return total
}

// EstimateTokensWithCalibration uses actual prompt tokens from the last LLM
// response as a calibration base, then estimates only new messages on top.
// Falls back to EstimateTokens() when no calibration data is available.
func EstimateTokensWithCalibration(messages []providers.Message, lastPromptTokens, lastMsgCount int) int {
	if lastPromptTokens <= 0 || lastMsgCount <= 0 {
		return EstimateTokens(messages)
	}

	currentCount := len(messages)
	newMsgs := currentCount - lastMsgCount
	if newMsgs <= 0 {
		// No new messages since last calibration (or history was truncated).
		// Use calibration value as-is; it's the best estimate we have.
		return lastPromptTokens
	}

	// Estimate only the new messages with the heuristic and add to base.
	delta := 0
	for _, m := range messages[lastMsgCount:] {
		delta += utf8.RuneCountInString(m.Content) / 3
	}
	return lastPromptTokens + delta
}
