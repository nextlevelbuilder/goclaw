package tools

import (
	"context"
	"sync"
	"testing"

	"github.com/google/uuid"

	"github.com/nextlevelbuilder/goclaw/internal/bus"
	"github.com/nextlevelbuilder/goclaw/internal/store"
	"github.com/nextlevelbuilder/goclaw/pkg/protocol"
)

type claimedTaskEventIdentity struct {
	tenantID uuid.UUID
	taskID   uuid.UUID
	typeName string
}

type eventClaimingTaskStore struct {
	*mockTaskStore
	mu       sync.Mutex
	bindings map[uuid.UUID]claimedTaskEventIdentity
	claims   []store.TeamTaskEventData
}

func newEventClaimingTaskStore() *eventClaimingTaskStore {
	return &eventClaimingTaskStore{
		mockTaskStore: newMockTaskStore(nil, nil),
		bindings:      make(map[uuid.UUID]claimedTaskEventIdentity),
	}
}

func (s *eventClaimingTaskStore) ClaimTaskEvent(ctx context.Context, event *store.TeamTaskEventData) (store.TaskEventClaimResult, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	identity := claimedTaskEventIdentity{
		tenantID: store.TenantIDFromContext(ctx),
		taskID:   event.TaskID,
		typeName: event.EventType,
	}
	if existing, ok := s.bindings[event.ID]; ok {
		if existing == identity {
			return store.TaskEventDuplicate, nil
		}
		return store.TaskEventConflict, nil
	}
	s.bindings[event.ID] = identity
	s.claims = append(s.claims, *event)
	return store.TaskEventClaimed, nil
}

func (s *eventClaimingTaskStore) GetTaskEventIdentity(_ context.Context, eventID uuid.UUID) (uuid.UUID, uuid.UUID, string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	identity, ok := s.bindings[eventID]
	if !ok {
		return uuid.Nil, uuid.Nil, "", store.ErrTaskNotFound
	}
	return identity.tenantID, identity.taskID, identity.typeName, nil
}

func TestReviewOutboundMessage_UsesLocalKey(t *testing.T) {
	task := &store.TeamTaskData{
		Channel: "telegram",
		ChatID:  "-100123456",
		Metadata: map[string]any{
			TaskMetaLocalKey: "-100123456:topic:47",
		},
	}

	got := reviewOutboundMessage(task, "review needed")
	if got.Metadata == nil {
		t.Fatal("expected metadata to be populated")
	}
	if got.Metadata["local_key"] != "-100123456:topic:47" {
		t.Fatalf("local_key = %q, want %q", got.Metadata["local_key"], "-100123456:topic:47")
	}
}

func TestReviewOutboundMessage_OmitsLocalKeyWhenMissing(t *testing.T) {
	task := &store.TeamTaskData{
		Channel: "telegram",
		ChatID:  "-100123456",
	}

	got := reviewOutboundMessage(task, "review needed")
	if got.Metadata != nil {
		t.Fatalf("expected metadata to be nil, got %#v", got.Metadata)
	}
}

func TestTaskLocalKeyMetadata(t *testing.T) {
	t.Run("uses local key", func(t *testing.T) {
		task := &store.TeamTaskData{
			Metadata: map[string]any{
				TaskMetaLocalKey: "-100123456:topic:47",
			},
		}

		got := TaskLocalKeyMetadata(task)
		if got == nil {
			t.Fatal("expected metadata to be populated")
		}
		if got[TaskMetaLocalKey] != "-100123456:topic:47" {
			t.Fatalf("local_key = %q, want %q", got[TaskMetaLocalKey], "-100123456:topic:47")
		}
	})

	t.Run("omits local key when missing", func(t *testing.T) {
		if got := TaskLocalKeyMetadata(&store.TeamTaskData{}); got != nil {
			t.Fatalf("expected metadata to be nil, got %#v", got)
		}
	})
}

func TestPublishTaskEventUsesAuthoritativeTenantPolicyAndGlobalIdentity(t *testing.T) {
	teamStore := newEventClaimingTaskStore()
	firstTenant := uuid.New()
	secondTenant := uuid.New()
	firstTaskID := uuid.New()
	secondTaskID := uuid.New()
	firstTeamID := uuid.New()
	secondTeamID := uuid.New()
	teamStore.tasks[firstTaskID] = &store.TeamTaskData{
		BaseModel: store.BaseModel{ID: firstTaskID}, TeamID: firstTeamID, TenantID: firstTenant,
		TaskNumber: 7, Subject: "Authoritative subject", Status: store.TeamTaskStatusInProgress,
		OwnerAgentKey: "owner-agent",
	}
	teamStore.tasks[secondTaskID] = &store.TeamTaskData{
		BaseModel: store.BaseModel{ID: secondTaskID}, TeamID: secondTeamID, TenantID: secondTenant,
	}

	msgBus := bus.New()
	var events []bus.Event
	msgBus.Subscribe("capture", func(evt bus.Event) { events = append(events, evt) })
	eventID := uuid.New()
	// Caller supplies stale/attacker-controlled subject/status/owner + a foreign
	// team ID; the authoritative task row must win every enriched field.
	callerPayload := protocol.TeamTaskEventPayload{
		TaskID: firstTaskID.String(), TeamID: uuid.NewString(),
		Subject: "stale subject", Status: store.TeamTaskStatusPending,
		OwnerAgentKey: "11111111-1111-1111-1111-111111111111",
	}
	if !PublishTaskEventWithID(teamStore, msgBus, protocol.EventTeamTaskProgress, callerPayload, eventID) {
		t.Fatal("first authoritative event must publish")
	}
	if len(events) != 1 {
		t.Fatalf("fanout count = %d, want 1", len(events))
	}
	gotPayload := events[0].Payload.(protocol.TeamTaskEventPayload)
	if events[0].EventID != eventID || events[0].TenantID != firstTenant {
		t.Fatalf("event identity = (%s,%s), want (%s,%s)", events[0].TenantID, events[0].EventID, firstTenant, eventID)
	}
	if gotPayload.TeamID != firstTeamID.String() {
		t.Fatalf("payload team was not resolved from task: %+v", gotPayload)
	}
	if gotPayload.TaskNumber != 7 || gotPayload.Subject != "Authoritative subject" ||
		gotPayload.Status != store.TeamTaskStatusInProgress || gotPayload.OwnerAgentKey != "owner-agent" {
		t.Fatalf("authoritative enrichment did not override caller input: %+v", gotPayload)
	}
	if len(teamStore.claims) != 1 || teamStore.claims[0].ID != eventID {
		t.Fatalf("audit claims = %+v, want one row keyed by EventID", teamStore.claims)
	}

	if PublishTaskEventWithID(teamStore, msgBus, protocol.EventTeamTaskProgress, callerPayload, eventID) {
		t.Fatal("same-tenant exact replay must not fanout")
	}
	crossTenantPayload := protocol.TeamTaskEventPayload{TaskID: secondTaskID.String()}
	if PublishTaskEventWithID(teamStore, msgBus, protocol.EventTeamTaskProgress, crossTenantPayload, eventID) {
		t.Fatal("cross-tenant EventID replay must be rejected")
	}
	if PublishTaskEventWithID(teamStore, msgBus, protocol.EventTeamTaskCommented, callerPayload, eventID) {
		t.Fatal("same EventID with a different event type must be rejected")
	}
	if len(events) != 1 || len(teamStore.claims) != 1 {
		t.Fatalf("replay leaked fanout/audit: events=%d claims=%d", len(events), len(teamStore.claims))
	}
}

func TestPublishDeletedTaskEventUsesAuthoritativeSnapshotIdentity(t *testing.T) {
	tenantID := uuid.New()
	teamID := uuid.New()
	taskID := uuid.New()
	msgBus := bus.New()
	var events []bus.Event
	msgBus.Subscribe("capture", func(evt bus.Event) { events = append(events, evt) })
	task := &store.TeamTaskData{
		BaseModel: store.BaseModel{ID: taskID}, TeamID: teamID, TenantID: tenantID,
		Subject: "Authoritative delete subject",
	}
	if !PublishDeletedTaskEvent(msgBus, task, protocol.TeamTaskEventPayload{
		TaskID: uuid.NewString(), TeamID: uuid.NewString(), Subject: "stale subject",
	}) {
		t.Fatal("delete tombstone was not published")
	}
	if len(events) != 1 || events[0].EventID == uuid.Nil || events[0].TenantID != tenantID {
		t.Fatalf("delete event identity = %+v, want authoritative tenant and EventID", events)
	}
	payload, ok := events[0].Payload.(protocol.TeamTaskEventPayload)
	if !ok {
		t.Fatalf("delete payload type = %T", events[0].Payload)
	}
	if payload.TaskID != taskID.String() || payload.TeamID != teamID.String() || payload.Subject != "Authoritative delete subject" {
		t.Fatalf("delete payload did not use task snapshot: %+v", payload)
	}
}
