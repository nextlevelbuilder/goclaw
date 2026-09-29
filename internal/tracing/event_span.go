package tracing

import (
	"context"
	"encoding/json"
	"time"

	"github.com/google/uuid"

	"github.com/nextlevelbuilder/goclaw/internal/store"
)

// EmitEventSpan records a finished internal step (memory flush, compaction) as an event span.
func EmitEventSpan(ctx context.Context, name string, start time.Time, err error, metadata map[string]any) {
	collector := CollectorFromContext(ctx)
	traceID := TraceIDFromContext(ctx)
	if collector == nil || traceID == uuid.Nil {
		return
	}

	end := time.Now().UTC()
	span := store.SpanData{
		TraceID:    traceID,
		SpanType:   store.SpanTypeEvent,
		Name:       name,
		StartTime:  start,
		EndTime:    &end,
		DurationMS: int(end.Sub(start).Milliseconds()),
		Status:     store.SpanStatusCompleted,
		Level:      store.SpanLevelDefault,
		TeamID:     TraceTeamIDPtrFromContext(ctx),
		TenantID:   store.TenantIDFromContext(ctx),
		CreatedAt:  end,
	}
	if span.TenantID == uuid.Nil {
		span.TenantID = store.MasterTenantID
	}
	if agentID := store.AgentIDFromContext(ctx); agentID != uuid.Nil {
		span.AgentID = &agentID
	}
	if parentID := ParentSpanIDFromContext(ctx); parentID != uuid.Nil {
		span.ParentSpanID = &parentID
	}
	if err != nil {
		span.Status = store.SpanStatusError
		span.Error = err.Error()
	}
	if len(metadata) > 0 {
		if b, mErr := json.Marshal(metadata); mErr == nil {
			span.Metadata = b
		}
	}
	collector.EmitSpan(RedactSpan(ctx, span))
}
