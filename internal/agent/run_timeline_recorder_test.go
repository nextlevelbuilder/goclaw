package agent

import (
	"context"
	"strings"
	"sync"
	"testing"

	"github.com/google/uuid"

	"github.com/nextlevelbuilder/goclaw/internal/store"
	"github.com/nextlevelbuilder/goclaw/pkg/protocol"
)

type recordingTimelineStore struct {
	mu    sync.Mutex
	items []store.RunTimelineItem
}

func (s *recordingTimelineStore) AppendRunTimelineItem(_ context.Context, item *store.RunTimelineItem) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.items = append(s.items, *item)
	return nil
}

func (s *recordingTimelineStore) ListRunTimelineItems(context.Context, store.RunTimelineListOpts) ([]store.RunTimelineItem, error) {
	return nil, nil
}

func TestRunTimelineItemFromEventScrubsToolArguments(t *testing.T) {
	tenantID := uuid.Must(uuid.NewV7())
	item, ok := runTimelineItemFromEvent(AgentEvent{
		Type:       protocol.AgentEventToolCall,
		AgentID:    "default",
		RunID:      "run-1",
		UserID:     "user-1",
		Channel:    "web",
		ChatID:     "chat-1",
		SessionKey: "session-1",
		TenantID:   tenantID,
		Payload: map[string]any{
			"name":      "exec_command",
			"id":        "call-1",
			"arguments": map[string]any{"cmd": "echo sk-abcdefghijklmnopqrstuvwxyz123456"},
		},
	}, 7)
	if !ok {
		t.Fatal("expected timeline item")
	}
	if item.ItemType != store.RunTimelineItemTypeToolCall {
		t.Fatalf("ItemType = %q", item.ItemType)
	}
	if item.Seq != 7 {
		t.Fatalf("Seq = %d, want 7", item.Seq)
	}
	if item.ToolName != "exec_command" || item.ToolCallID != "call-1" {
		t.Fatalf("tool fields = %q/%q", item.ToolName, item.ToolCallID)
	}
	if strings.Contains(item.Preview, "sk-abcdefghijklmnopqrstuvwxyz123456") {
		t.Fatalf("preview leaked secret: %s", item.Preview)
	}
	if !strings.Contains(item.Preview, "[REDACTED]") {
		t.Fatalf("preview missing redaction: %s", item.Preview)
	}
	if item.AgentID != nil {
		t.Fatalf("AgentID = %v, want nil for agent key", item.AgentID)
	}
	if !strings.Contains(string(item.Metadata), `"agent_key":"default"`) {
		t.Fatalf("metadata missing agent_key: %s", item.Metadata)
	}
}

func TestRunTimelineItemFromEventDropsUnsupportedAndThinking(t *testing.T) {
	tenantID := uuid.Must(uuid.NewV7())
	if _, ok := runTimelineItemFromEvent(AgentEvent{
		Type:       protocol.ChatEventThinking,
		RunID:      "run-1",
		SessionKey: "session-1",
		TenantID:   tenantID,
	}, 1); ok {
		t.Fatal("thinking event should not be archived")
	}

	item, ok := runTimelineItemFromEvent(AgentEvent{
		Type:       protocol.AgentEventRunCompleted,
		RunID:      "run-1",
		SessionKey: "session-1",
		TenantID:   tenantID,
		Payload: map[string]any{
			"content":  "visible <thinking>hidden chain</thinking> done",
			"thinking": "raw hidden chain",
		},
	}, 1)
	if !ok {
		t.Fatal("expected completed item")
	}
	if strings.Contains(item.Preview, "hidden chain") || strings.Contains(item.Preview, "raw hidden") {
		t.Fatalf("preview leaked thinking: %q", item.Preview)
	}
	if item.Preview != "visible  done" {
		t.Fatalf("Preview = %q", item.Preview)
	}
}

func TestRunTimelinePreviewStripsDeliveryFileTokens(t *testing.T) {
	tenantID := uuid.Must(uuid.NewV7())
	item, ok := runTimelineItemFromEvent(AgentEvent{
		Type:       protocol.AgentEventRunCompleted,
		RunID:      "run-1",
		SessionKey: "session-1",
		TenantID:   tenantID,
		Payload: map[string]any{
			"content": "See ![a](/v1/files/work/a.png?ft=signed.123) and /v1/media/b.txt?x=1&ft=stale.456",
		},
	}, 1)
	if !ok {
		t.Fatal("expected completed item")
	}
	if strings.Contains(item.Preview, "ft=") || strings.Contains(item.Preview, "signed.123") || strings.Contains(item.Preview, "stale.456") {
		t.Fatalf("preview leaked delivery token: %q", item.Preview)
	}
	if !strings.Contains(item.Preview, "/v1/files/work/a.png") || !strings.Contains(item.Preview, "/v1/media/b.txt?x=1") {
		t.Fatalf("preview lost clean file URLs: %q", item.Preview)
	}
}

func TestRunTimelineRecorderOnlyTracksSupportedActiveRuns(t *testing.T) {
	tenantID := uuid.Must(uuid.NewV7())
	recorder := NewRunTimelineRecorder(&recordingTimelineStore{})
	base := AgentEvent{
		RunID:      "run-1",
		SessionKey: "session-1",
		TenantID:   tenantID,
	}

	recorder.Record(AgentEvent{Type: protocol.ChatEventThinking, RunID: base.RunID, SessionKey: base.SessionKey, TenantID: base.TenantID})
	if got := recorderTrackedRuns(recorder); got != 0 {
		t.Fatalf("tracked runs after unsupported event = %d, want 0", got)
	}

	recorder.Record(AgentEvent{Type: protocol.AgentEventToolCall, RunID: base.RunID, SessionKey: base.SessionKey, TenantID: base.TenantID})
	if got := recorderTrackedRuns(recorder); got != 1 {
		t.Fatalf("tracked runs after supported event = %d, want 1", got)
	}
	if seq := recorderSeq(recorder, base.RunID); seq != 1 {
		t.Fatalf("seq after first supported event = %d, want 1", seq)
	}

	recorder.Record(AgentEvent{Type: protocol.ChatEventThinking, RunID: base.RunID, SessionKey: base.SessionKey, TenantID: base.TenantID})
	if seq := recorderSeq(recorder, base.RunID); seq != 1 {
		t.Fatalf("seq after unsupported event = %d, want 1", seq)
	}

	recorder.Record(AgentEvent{Type: protocol.AgentEventRunCompleted, RunID: base.RunID, SessionKey: base.SessionKey, TenantID: base.TenantID})
	if got := recorderTrackedRuns(recorder); got != 0 {
		t.Fatalf("tracked runs after terminal event = %d, want 0", got)
	}
}

func recorderTrackedRuns(r *RunTimelineRecorder) int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return len(r.nextSeq)
}

func recorderSeq(r *RunTimelineRecorder, runID string) int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.nextSeq[runID]
}
