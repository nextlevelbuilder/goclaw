package agent

import (
	"context"
	"encoding/json"
	"log/slog"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"

	"github.com/nextlevelbuilder/goclaw/internal/store"
	"github.com/nextlevelbuilder/goclaw/internal/tools"
	"github.com/nextlevelbuilder/goclaw/internal/tracing"
	"github.com/nextlevelbuilder/goclaw/pkg/protocol"
)

const runTimelinePreviewLimit = 2000

// RunTimelineRecorder persists display-safe run events without blocking delivery.
type RunTimelineRecorder struct {
	store   store.RunTimelineStore
	timeout time.Duration

	mu      sync.Mutex
	nextSeq map[string]int
}

func NewRunTimelineRecorder(timelineStore store.RunTimelineStore) *RunTimelineRecorder {
	return &RunTimelineRecorder{
		store:   timelineStore,
		timeout: 2 * time.Second,
		nextSeq: make(map[string]int),
	}
}

func (r *RunTimelineRecorder) Record(event AgentEvent) {
	if r == nil || r.store == nil {
		return
	}
	if event.RunID == "" || event.SessionKey == "" || event.TenantID == uuid.Nil {
		return
	}
	if _, _, ok := timelineKindForEvent(event); !ok {
		return
	}
	seq := r.reserveSeq(event.RunID)
	item, ok := runTimelineItemFromEvent(event, seq)
	if !ok {
		return
	}
	if isTerminalRunTimelineEvent(event.Type) {
		defer r.forgetRun(event.RunID)
	}
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), r.timeout)
		defer cancel()
		ctx = store.WithTenantID(ctx, item.TenantID)
		if err := r.store.AppendRunTimelineItem(ctx, &item); err != nil {
			slog.Warn("run_timeline.persist_failed", "run_id", event.RunID, "event", event.Type, "error", err)
		}
	}()
}

func (r *RunTimelineRecorder) reserveSeq(runID string) int {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.nextSeq[runID]++
	return r.nextSeq[runID]
}

func (r *RunTimelineRecorder) forgetRun(runID string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	delete(r.nextSeq, runID)
}

func isTerminalRunTimelineEvent(eventType string) bool {
	switch eventType {
	case protocol.AgentEventRunCompleted, protocol.AgentEventRunFailed, protocol.AgentEventRunCancelled:
		return true
	default:
		return false
	}
}

func runTimelineItemFromEvent(event AgentEvent, seq int) (store.RunTimelineItem, bool) {
	if event.RunID == "" || event.SessionKey == "" || event.TenantID == uuid.Nil {
		return store.RunTimelineItem{}, false
	}
	itemType, status, ok := timelineKindForEvent(event)
	if !ok {
		return store.RunTimelineItem{}, false
	}
	metadata := timelineMetadata(event)
	agentUUID, agentIsUUID := parseOptionalUUID(event.AgentID)
	if event.AgentID != "" && !agentIsUUID {
		metadata["agent_key"] = event.AgentID
	}
	traceID, spanID := payloadTraceIDs(event.Payload)
	item := store.RunTimelineItem{
		TenantID:   event.TenantID,
		RunID:      event.RunID,
		SessionKey: event.SessionKey,
		AgentID:    agentUUID,
		UserID:     event.UserID,
		Channel:    event.Channel,
		ChatID:     event.ChatID,
		Seq:        seq,
		ItemType:   itemType,
		Status:     status,
		Title:      timelineTitle(event),
		Preview:    timelinePreview(event),
		ToolName:   payloadString(event.Payload, "name"),
		ToolCallID: payloadString(event.Payload, "id"),
		TraceID:    traceID,
		SpanID:     spanID,
		Metadata:   mustJSON(metadata),
	}
	return item, true
}

func timelineKindForEvent(event AgentEvent) (string, string, bool) {
	switch event.Type {
	case protocol.AgentEventRunStarted:
		return store.RunTimelineItemTypeRunStatus, store.RunTimelineStatusStarted, true
	case protocol.AgentEventRunCompleted:
		return store.RunTimelineItemTypeRunStatus, store.RunTimelineStatusCompleted, true
	case protocol.AgentEventRunFailed:
		return store.RunTimelineItemTypeRunStatus, store.RunTimelineStatusFailed, true
	case protocol.AgentEventRunCancelled:
		return store.RunTimelineItemTypeRunStatus, store.RunTimelineStatusCancelled, true
	case protocol.AgentEventActivity:
		return store.RunTimelineItemTypeActivity, store.RunTimelineStatusRunning, true
	case protocol.AgentEventBlockReply:
		return store.RunTimelineItemTypeAssistantMessage, store.RunTimelineStatusCompleted, true
	case protocol.AgentEventToolCall:
		return store.RunTimelineItemTypeToolCall, store.RunTimelineStatusRunning, true
	case protocol.AgentEventToolResult:
		if payloadBool(event.Payload, "is_error") {
			return store.RunTimelineItemTypeToolResult, store.RunTimelineStatusFailed, true
		}
		return store.RunTimelineItemTypeToolResult, store.RunTimelineStatusCompleted, true
	default:
		return "", "", false
	}
}

func timelineTitle(event AgentEvent) string {
	if name := payloadString(event.Payload, "name"); name != "" {
		return name
	}
	switch event.Type {
	case protocol.AgentEventRunStarted:
		return "Run started"
	case protocol.AgentEventRunCompleted:
		return "Run completed"
	case protocol.AgentEventRunFailed:
		return "Run failed"
	case protocol.AgentEventRunCancelled:
		return "Run cancelled"
	case protocol.AgentEventBlockReply:
		return "Assistant message"
	case protocol.AgentEventActivity:
		return "Activity"
	default:
		return event.Type
	}
}

func timelinePreview(event AgentEvent) string {
	switch event.Type {
	case protocol.AgentEventRunStarted:
		return sanitizeTimelinePreview(payloadString(event.Payload, "message"))
	case protocol.AgentEventRunCompleted, protocol.AgentEventBlockReply:
		return sanitizeTimelinePreview(payloadString(event.Payload, "content"))
	case protocol.AgentEventRunFailed:
		return sanitizeTimelinePreview(payloadString(event.Payload, "error"))
	case protocol.AgentEventActivity:
		return sanitizeTimelinePreview(payloadAnyString(event.Payload))
	case protocol.AgentEventToolCall:
		return sanitizeTimelinePreview(payloadJSON(event.Payload, "arguments"))
	case protocol.AgentEventToolResult:
		if result := payloadString(event.Payload, "result"); result != "" {
			return sanitizeTimelinePreview(result)
		}
		return sanitizeTimelinePreview(payloadString(event.Payload, "content"))
	default:
		return ""
	}
}

func sanitizeTimelinePreview(value string) string {
	value = strings.TrimSpace(stripThinkingTags(value))
	value = stripDeliveryFileTokens(value)
	value = tools.ScrubCredentials(value)
	return tracing.TruncateMid(value, runTimelinePreviewLimit)
}

var deliveryFileTokenRe = regexp.MustCompile(`([?&])ft=[^)\]'"<>\s&]+`)

func stripDeliveryFileTokens(value string) string {
	if !strings.Contains(value, "ft=") {
		return value
	}
	value = deliveryFileTokenRe.ReplaceAllStringFunc(value, func(match string) string {
		if strings.HasPrefix(match, "&") {
			return ""
		}
		return "?"
	})
	value = strings.ReplaceAll(value, "?&", "?")
	value = strings.ReplaceAll(value, "?)", ")")
	value = strings.ReplaceAll(value, "?]", "]")
	value = strings.TrimSuffix(value, "?")
	return strings.TrimSuffix(value, "&")
}

func timelineMetadata(event AgentEvent) map[string]any {
	metadata := map[string]any{"event_type": event.Type}
	if event.RunKind != "" {
		metadata["run_kind"] = event.RunKind
	}
	if event.DelegationID != "" {
		metadata["delegation_id"] = event.DelegationID
	}
	if event.TeamID != "" {
		metadata["team_id"] = event.TeamID
	}
	if event.TeamTaskID != "" {
		metadata["team_task_id"] = event.TeamTaskID
	}
	if event.ParentAgentID != "" {
		metadata["parent_agent_id"] = event.ParentAgentID
	}
	if event.SenderID != "" {
		metadata["sender_id"] = event.SenderID
	}
	if payloadBool(event.Payload, "is_error") {
		metadata["is_error"] = true
	}
	return metadata
}

func payloadString(payload any, key string) string {
	switch m := payload.(type) {
	case map[string]string:
		return m[key]
	case map[string]any:
		if v, ok := m[key].(string); ok {
			return v
		}
	}
	return ""
}

func payloadBool(payload any, key string) bool {
	if m, ok := payload.(map[string]any); ok {
		if v, ok := m[key].(bool); ok {
			return v
		}
	}
	return false
}

func payloadAnyString(payload any) string {
	switch v := payload.(type) {
	case string:
		return v
	case map[string]string:
		if c := v["content"]; c != "" {
			return c
		}
		if m := v["message"]; m != "" {
			return m
		}
	case map[string]any:
		for _, key := range []string{"content", "message", "status", "step"} {
			if s, ok := v[key].(string); ok && s != "" {
				return s
			}
		}
	}
	raw, _ := json.Marshal(payload)
	return string(raw)
}

func payloadJSON(payload any, key string) string {
	if m, ok := payload.(map[string]any); ok {
		if v, has := m[key]; has {
			raw, _ := json.Marshal(v)
			return string(raw)
		}
	}
	return ""
}

func payloadTraceIDs(payload any) (*uuid.UUID, *uuid.UUID) {
	traceID := parsePayloadUUID(payload, "trace_id", "traceId")
	spanID := parsePayloadUUID(payload, "span_id", "spanId")
	return traceID, spanID
}

func parsePayloadUUID(payload any, keys ...string) *uuid.UUID {
	for _, key := range keys {
		if v := payloadString(payload, key); v != "" {
			if parsed, err := uuid.Parse(v); err == nil {
				return &parsed
			}
		}
	}
	return nil
}

func parseOptionalUUID(value string) (*uuid.UUID, bool) {
	if value == "" {
		return nil, false
	}
	parsed, err := uuid.Parse(value)
	if err != nil {
		return nil, false
	}
	return &parsed, true
}

func mustJSON(v any) json.RawMessage {
	raw, err := json.Marshal(v)
	if err != nil {
		return json.RawMessage(`{}`)
	}
	return raw
}
