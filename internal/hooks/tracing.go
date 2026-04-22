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
// (e.g., "hook.command.pre_tool_use"). Duration is computed from startedAt
// to now; status is "completed" on success, "error" when errMsg is non-empty.
// The decision is persisted into Metadata as `{"decision":"allow|block|..."}`
// so dashboards can aggregate allow/block ratios per event.
//
// Fields lifted from ctx:
//   - trace id         (tracing.TraceIDFromContext)
//   - parent span id   (tracing.ParentSpanIDFromContext, omitted when nil)
//   - team id          (tracing.TraceTeamIDPtrFromContext)
//   - tenant id        (store.TenantIDFromContext)
//
// No-op when ctx has no collector attached — safe in tests and for tenants
// without tracing enabled.
func EmitHookSpan(
	ctx context.Context,
	event HookEvent,
	ht HandlerType,
	startedAt time.Time,
	decision Decision,
	errMsg string,
) {
	collector := tracing.CollectorFromContext(ctx)
	if collector == nil {
		return
	}

	end := time.Now().UTC()
	durationMS := max(int(end.Sub(startedAt)/time.Millisecond), 0)

	status := store.SpanStatusCompleted
	if errMsg != "" {
		status = store.SpanStatusError
	}

	var metadata json.RawMessage
	if decision != "" {
		if b, err := json.Marshal(map[string]string{"decision": string(decision)}); err == nil {
			metadata = b
		}
	}

	span := store.SpanData{
		TraceID:    tracing.TraceIDFromContext(ctx),
		SpanType:   store.SpanTypeEvent,
		Name:       "hook." + string(ht) + "." + string(event),
		StartTime:  startedAt,
		EndTime:    &end,
		DurationMS: durationMS,
		Status:     status,
		Error:      errMsg,
		Metadata:   metadata,
		TeamID:     tracing.TraceTeamIDPtrFromContext(ctx),
		TenantID:   store.TenantIDFromContext(ctx),
		CreatedAt:  end,
	}

	// Attach parent only when present — leaving it nil avoids bogus FK edges.
	if parent := tracing.ParentSpanIDFromContext(ctx); parent != uuid.Nil {
		p := parent
		span.ParentSpanID = &p
	}

	collector.EmitSpan(span)
}
