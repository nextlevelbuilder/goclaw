package agent

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/nextlevelbuilder/goclaw/internal/store"
	"github.com/nextlevelbuilder/goclaw/internal/tools"
	"github.com/nextlevelbuilder/goclaw/internal/tracing"
)

type fakeUsageEventStore struct {
	events chan store.UsageEvent
}

func newFakeUsageEventStore() *fakeUsageEventStore {
	return &fakeUsageEventStore{events: make(chan store.UsageEvent, 8)}
}

func (s *fakeUsageEventStore) InsertEvent(_ context.Context, event *store.UsageEvent) error {
	if event != nil {
		s.events <- *event
	}
	return nil
}

func (s *fakeUsageEventStore) InsertEvents(ctx context.Context, events []store.UsageEvent) error {
	for i := range events {
		if err := s.InsertEvent(ctx, &events[i]); err != nil {
			return err
		}
	}
	return nil
}

func (s *fakeUsageEventStore) GetEventTimeSeries(context.Context, store.UsageEventQuery) ([]store.UsageEventTimeSeries, error) {
	return nil, nil
}

func (s *fakeUsageEventStore) RefreshEventRollupHour(context.Context, time.Time) error {
	return nil
}

func (s *fakeUsageEventStore) GetLatestEventRollupBucket(context.Context) (*time.Time, error) {
	return nil, nil
}

func (s *fakeUsageEventStore) GetEventBreakdown(context.Context, store.UsageEventQuery) ([]store.UsageEventBreakdown, error) {
	return nil, nil
}

func (s *fakeUsageEventStore) GetEventSummary(context.Context, store.UsageEventQuery) (*store.UsageEventSummary, error) {
	return nil, nil
}

type usageFakeTool struct {
	name string
}

func (t usageFakeTool) Name() string               { return t.name }
func (t usageFakeTool) Description() string        { return "" }
func (t usageFakeTool) Parameters() map[string]any { return nil }
func (t usageFakeTool) Execute(context.Context, map[string]any) *tools.Result {
	return tools.NewResult("ok")
}

func TestRecordToolUsageEvent_RuntimeAliasUsesCanonicalName(t *testing.T) {
	storeSpy := newFakeUsageEventStore()
	registry := tools.NewRegistry()
	registry.Register(usageFakeTool{name: "exec"})
	registry.RegisterAlias("Bash", "exec")
	loop := &Loop{registry: registry, usageEvents: storeSpy, agentUUID: uuid.New(), tenantID: uuid.New()}
	ctx := tracing.WithTraceID(store.WithTenantID(t.Context(), loop.tenantID), uuid.New())

	canonical := loop.canonicalToolName("Bash")
	loop.recordToolUsageEvent(ctx, &RunRequest{RunID: "run-1", SessionKey: "session-1", Channel: "telegram"}, canonical, "Bash", "call-1",
		map[string]any{"command": "echo secret"}, time.Now().Add(-time.Millisecond), tools.NewResult("ok"), uuid.New())

	event := waitUsageEvent(t, storeSpy)
	if event.EventType != store.UsageEventTypeRuntimeToolCall {
		t.Fatalf("event type = %q, want %q", event.EventType, store.UsageEventTypeRuntimeToolCall)
	}
	if event.ResourceName != "exec" {
		t.Fatalf("resource = %q, want canonical exec", event.ResourceName)
	}
	if string(event.Metadata) == "" || json.Valid(event.Metadata) == false {
		t.Fatalf("metadata should contain valid alias metadata, got %q", string(event.Metadata))
	}
	if containsJSONKey(event.Metadata, "command") {
		t.Fatalf("metadata leaked raw command args: %s", string(event.Metadata))
	}
}

func TestRecordToolUsageEvent_UseSkillCountsSkillName(t *testing.T) {
	storeSpy := newFakeUsageEventStore()
	registry := tools.NewRegistry()
	registry.Register(tools.NewUseSkillTool())
	loop := &Loop{registry: registry, usageEvents: storeSpy, agentUUID: uuid.New(), tenantID: uuid.New()}
	ctx := tracing.WithTraceID(store.WithTenantID(t.Context(), loop.tenantID), uuid.New())

	loop.recordToolUsageEvent(ctx, &RunRequest{RunID: "run-1", SessionKey: "session-1"}, "use_skill", "use_skill", "call-1",
		map[string]any{"name": "ck:plan"}, time.Now().Add(-time.Millisecond), tools.NewResult("ok"), uuid.New())

	event := waitUsageEvent(t, storeSpy)
	if event.EventType != store.UsageEventTypeSkillActivation {
		t.Fatalf("event type = %q, want skill activation", event.EventType)
	}
	if event.ResourceType != store.UsageResourceTypeSkill || event.ResourceName != "ck:plan" {
		t.Fatalf("resource = %s/%s, want skill ck:plan", event.ResourceType, event.ResourceName)
	}
	if event.Source != store.UsageSourceUseSkill {
		t.Fatalf("source = %q, want use_skill", event.Source)
	}
}

func TestRecordSkillSlashUsageEvent_RequiresTraceContext(t *testing.T) {
	storeSpy := newFakeUsageEventStore()
	loop := &Loop{usageEvents: storeSpy, agentUUID: uuid.New(), tenantID: uuid.New()}
	ctx := store.WithRunContext(store.WithTenantID(t.Context(), loop.tenantID), &store.RunContext{
		RunID:      "run-1",
		SessionKey: "session-1",
		Channel:    "web",
	})

	loop.recordSkillSlashUsageEvent(ctx, "ck:plan")
	select {
	case event := <-storeSpy.events:
		t.Fatalf("unexpected event without trace: %+v", event)
	case <-time.After(50 * time.Millisecond):
	}

	loop.recordSkillSlashUsageEvent(tracing.WithTraceID(ctx, uuid.New()), "ck:plan")
	event := waitUsageEvent(t, storeSpy)
	if event.Source != store.UsageSourceSlashCommand || event.ResourceName != "ck:plan" {
		t.Fatalf("slash event = %s/%s, want slash-command ck:plan", event.Source, event.ResourceName)
	}
	if event.RunID != "run-1" || event.SessionKey != "session-1" || event.Channel != "web" {
		t.Fatalf("run context not copied: run=%q session=%q channel=%q", event.RunID, event.SessionKey, event.Channel)
	}
}

func waitUsageEvent(t *testing.T, storeSpy *fakeUsageEventStore) store.UsageEvent {
	t.Helper()
	select {
	case event := <-storeSpy.events:
		return event
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for usage event")
		return store.UsageEvent{}
	}
}

func containsJSONKey(raw json.RawMessage, key string) bool {
	var values map[string]any
	if err := json.Unmarshal(raw, &values); err != nil {
		return false
	}
	_, ok := values[key]
	return ok
}
