package hooks

import (
	"context"
	"encoding/json"
	"time"

	"github.com/google/uuid"

	"github.com/nextlevelbuilder/goclaw/internal/store"
	"github.com/nextlevelbuilder/goclaw/internal/tracing"
)

// EmitHookSpan records a tracing span for a hook execution.
//
// The span name follows the plan's convention: "hook.<handlerType>.<event>"
// (e.g., "hook.command.pre_tool_use"). Duration is taken from durationMS
// (the caller already measured it); status is "completed" on success,
// "error" when decision == DecisionError or errMsg is non-empty. The decision
// and hook identity are persisted into Metadata so dashboards can aggregate
// allow/block/error ratios per event and hook.
//
// Fields lifted from ctx:
//   - trace id         (tracing.TraceIDFromContext)
//   - parent span id   (tracing.ParentSpanIDFromContext, omitted when nil)
//   - agent id         (store.AgentIDFromContext, omitted when zero)
//   - team id          (tracing.TraceTeamIDPtrFromContext)
//   - tenant id        (store.TenantIDFromContext, falls back to MasterTenantID)
//
// No-op when ctx has no collector attached — safe in tests and for tenants
// without tracing enabled.
func EmitHookSpan(
	ctx context.Context,
	event HookEvent,
	ht HandlerType,
	cfg HookConfig,
	startedAt time.Time,
	decision Decision,
	durationMS int,
	errMsg string,
	input string,
	output string,
) {
	const eventPreviewLimit = 40_000

	end := time.Now().UTC()

	status := store.SpanStatusCompleted
	if decision == DecisionError || errMsg != "" {
		status = store.SpanStatusError
	}

	// Mirror emitLLMSpanStart's ctx-extraction pattern for agent/team/tenant.
	var agentID *uuid.UUID
	if a := store.AgentIDFromContext(ctx); a != uuid.Nil {
		agentID = &a
	}
	tenantID := store.TenantIDFromContext(ctx)
	if tenantID == uuid.Nil {
		tenantID = store.MasterTenantID
	}

	errorMsg := errMsg
	if len(errorMsg) > 200 {
		errorMsg = errorMsg[:200]
	}

	metadata := map[string]any{
		"decision":     string(decision),
		"hook_id":      cfg.ID.String(),
		"hook_name":    cfg.Name,
		"hook_event":   string(event),
		"handler_type": string(ht),
		"duration_ms":  durationMS,
	}
	var metadataJSON json.RawMessage
	if b, err := json.Marshal(metadata); err == nil {
		metadataJSON = b
	}

	span := store.SpanData{
		TraceID:       tracing.TraceIDFromContext(ctx),
		SpanType:      store.SpanTypeEvent,
		Name:          "hook." + string(ht) + "." + string(event),
		StartTime:     startedAt,
		EndTime:       &end,
		DurationMS:    durationMS,
		Status:        status,
		Error:         errorMsg,
		Level:         store.SpanLevelDefault,
		InputPreview:  tracing.TruncateJSON(input, eventPreviewLimit),
		OutputPreview: tracing.TruncateMid(output, eventPreviewLimit),
		AgentID:       agentID,
		TeamID:        tracing.TraceTeamIDPtrFromContext(ctx),
		TenantID:      tenantID,
		Metadata:      metadataJSON,
		CreatedAt:     end,
	}

	// Attach parent only when present — leaving it nil avoids bogus FK edges.
	if parent := tracing.ParentSpanIDFromContext(ctx); parent != uuid.Nil {
		p := parent
		span.ParentSpanID = &p
	}

	collector := tracing.CollectorFromContext(ctx)
	if collector == nil {
		return // tracing disabled — no collector attached
	}
	collector.EmitSpan(span)
}
