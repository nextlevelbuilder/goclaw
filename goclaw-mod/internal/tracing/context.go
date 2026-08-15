package tracing

import (
	"context"

	"github.com/google/uuid"
)

type contextKey string

const (
	traceIDKey              contextKey = "goclaw_trace_id"
	parentSpanKey           contextKey = "goclaw_parent_span_id"
	collectorKey            contextKey = "goclaw_trace_collector"
	announceParentKey       contextKey = "goclaw_announce_parent_span_id"
	delegateParentTraceKey  contextKey = "goclaw_delegate_parent_trace_id"
	traceTeamIDKey          contextKey = "goclaw_trace_team_id"
)

// WithTraceID returns a context with the given trace ID.
func WithTraceID(ctx context.Context, id uuid.UUID) context.Context {
	return context.WithValue(ctx, traceIDKey, id)
}

// TraceIDFromContext extracts the trace ID from context. Returns uuid.Nil if not set.
func TraceIDFromContext(ctx context.Context) uuid.UUID {
	if v, ok := ctx.Value(traceIDKey).(uuid.UUID); ok {
		return v
	}
	return uuid.Nil
}

// WithParentSpanID returns a context with the given parent span ID.
func WithParentSpanID(ctx context.Context, id uuid.UUID) context.Context {
	return context.WithValue(ctx, parentSpanKey, id)
}

// ParentSpanIDFromContext extracts the parent span ID. Returns uuid.Nil if not set.
func ParentSpanIDFromContext(ctx context.Context) uuid.UUID {
	if v, ok := ctx.Value(parentSpanKey).(uuid.UUID); ok {
		return v
	}
	return uuid.Nil
}

// WithCollector returns a context with the given Collector.
func WithCollector(ctx context.Context, c *Collector) context.Context {
	return context.WithValue(ctx, collectorKey, c)
}

// CollectorFromContext extracts the Collector from context. Returns nil if not set.
func CollectorFromContext(ctx context.Context) *Collector {
	if v, ok := ctx.Value(collectorKey).(*Collector); ok {
		return v
	}
	return nil
}

// WithAnnounceParentSpanID returns a context indicating this run's agent span
// should be nested under the given parent span (used for announce runs).
func WithAnnounceParentSpanID(ctx context.Context, id uuid.UUID) context.Context {
	return context.WithValue(ctx, announceParentKey, id)
}

// AnnounceParentSpanIDFromContext extracts the announce parent span ID.
// Returns uuid.Nil if not set (i.e., this is a normal run, not an announce).
func AnnounceParentSpanIDFromContext(ctx context.Context) uuid.UUID {
	if v, ok := ctx.Value(announceParentKey).(uuid.UUID); ok {
		return v
	}
	return uuid.Nil
}

// WithDelegateParentTraceID sets the parent trace ID for delegation linking.
// Unlike ParentTraceID on RunRequest (which reuses the parent trace for announces),
// this creates a NEW trace that links back to the parent via parent_trace_id.
func WithDelegateParentTraceID(ctx context.Context, id uuid.UUID) context.Context {
	return context.WithValue(ctx, delegateParentTraceKey, id)
}

// DelegateParentTraceIDFromContext extracts the delegation parent trace ID.
func DelegateParentTraceIDFromContext(ctx context.Context) uuid.UUID {
	if v, ok := ctx.Value(delegateParentTraceKey).(uuid.UUID); ok {
		return v
	}
	return uuid.Nil
}

// WithTraceTeamID sets the team ID for trace/span scoping.
func WithTraceTeamID(ctx context.Context, id uuid.UUID) context.Context {
	return context.WithValue(ctx, traceTeamIDKey, id)
}

// TraceTeamIDFromContext extracts the team ID for trace/span scoping.
// Returns uuid.Nil if not set (non-team run).
func TraceTeamIDFromContext(ctx context.Context) uuid.UUID {
	if v, ok := ctx.Value(traceTeamIDKey).(uuid.UUID); ok {
		return v
	}
	return uuid.Nil
}

// TraceTeamIDPtrFromContext returns *uuid.UUID for setting on SpanData.TeamID.
// Returns nil if not set (non-team run).
func TraceTeamIDPtrFromContext(ctx context.Context) *uuid.UUID {
	if v, ok := ctx.Value(traceTeamIDKey).(uuid.UUID); ok && v != uuid.Nil {
		return &v
	}
	return nil
}
