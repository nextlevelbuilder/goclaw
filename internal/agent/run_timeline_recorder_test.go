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

func (s *recordingTimelineStore) RecoverInterruptedRuns(context.Context) (int64, error) {
	return 0, nil
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

func TestRunTimelineItemFromEventDropsUnsupportedAndStreamsArchived(t *testing.T) {
	tenantID := uuid.Must(uuid.NewV7())

	// Unsupported event types (tool.log) are not archived.
	if _, ok := runTimelineItemFromEvent(AgentEvent{
		Type:       protocol.AgentEventToolLog,
		RunID:      "run-1",
		SessionKey: "session-1",
		TenantID:   tenantID,
	}, 1); ok {
		t.Fatal("tool.log event should not be archived")
	}

	// Thinking events (G4) are now archived with type=thinking/status=thinking
	// and full content persisted for stream replay.
	thinking, ok := runTimelineItemFromEvent(AgentEvent{
		Type:       protocol.ChatEventThinking,
		RunID:      "run-1",
		SessionKey: "session-1",
		TenantID:   tenantID,
		Payload:    map[string]string{"content": "reasoning trace"},
	}, 2)
	if !ok {
		t.Fatal("thinking event should be archived")
	}
	if thinking.ItemType != store.RunTimelineItemTypeThinking || thinking.Status != store.RunTimelineStatusThinking {
		t.Fatalf("thinking item = %q/%q", thinking.ItemType, thinking.Status)
	}
	if thinking.Content != "reasoning trace" {
		t.Fatalf("thinking content = %q", thinking.Content)
	}

	completed, ok := runTimelineItemFromEvent(AgentEvent{
		Type:       protocol.AgentEventRunCompleted,
		RunID:      "run-1",
		SessionKey: "session-1",
		TenantID:   tenantID,
		Payload: map[string]any{
			"content":  "visible <thinking>hidden chain</thinking> done",
			"thinking": "raw hidden chain",
		},
	}, 3)
	if !ok {
		t.Fatal("expected completed item")
	}
	if strings.Contains(completed.Preview, "hidden chain") || strings.Contains(completed.Preview, "raw hidden") {
		t.Fatalf("preview leaked thinking: %q", completed.Preview)
	}
	if completed.Preview != "visible  done" {
		t.Fatalf("Preview = %q", completed.Preview)
	}
}

func TestRunTimelineItemArchivesStreamChunkAndToolStarted(t *testing.T) {
	tenantID := uuid.Must(uuid.NewV7())

	chunk, ok := runTimelineItemFromEvent(AgentEvent{
		Type:       protocol.ChatEventChunk,
		RunID:      "run-1",
		SessionKey: "session-1",
		TenantID:   tenantID,
		Payload:    map[string]string{"content": "streamed delta"},
	}, 4)
	if !ok {
		t.Fatal("chunk event should be archived")
	}
	if chunk.ItemType != store.RunTimelineItemTypeChunk || chunk.Status != store.RunTimelineStatusRunning {
		t.Fatalf("chunk item = %q/%q", chunk.ItemType, chunk.Status)
	}
	if chunk.Content != "streamed delta" {
		t.Fatalf("chunk content = %q", chunk.Content)
	}

	started, ok := runTimelineItemFromEvent(AgentEvent{
		Type:       protocol.AgentEventToolStarted,
		RunID:      "run-1",
		SessionKey: "session-1",
		TenantID:   tenantID,
		Payload:    map[string]any{"name": "read_file", "id": "tool-1", "rawName": "read_file"},
	}, 5)
	if !ok {
		t.Fatal("tool.started event should be archived")
	}
	if started.ItemType != store.RunTimelineItemTypeToolStarted || started.Status != store.RunTimelineStatusWaitingTool {
		t.Fatalf("tool.started item = %q/%q", started.ItemType, started.Status)
	}
	if started.ToolName != "read_file" || started.ToolCallID != "tool-1" {
		t.Fatalf("tool identity = %q/%q", started.ToolName, started.ToolCallID)
	}
	if !strings.Contains(started.Content, "read_file") {
		t.Fatalf("tool.started content = %q", started.Content)
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

	// Unsupported event (tool.log) does not create a seq track.
	recorder.Record(AgentEvent{Type: protocol.AgentEventToolLog, RunID: base.RunID, SessionKey: base.SessionKey, TenantID: base.TenantID})
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

	// G4: thinking is now a supported stream item — it archives and advances seq.
	recorder.Record(AgentEvent{Type: protocol.ChatEventThinking, RunID: base.RunID, SessionKey: base.SessionKey, TenantID: base.TenantID})
	if seq := recorderSeq(recorder, base.RunID); seq != 2 {
		t.Fatalf("seq after thinking event = %d, want 2", seq)
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
