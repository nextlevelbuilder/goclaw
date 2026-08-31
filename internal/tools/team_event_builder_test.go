package tools

import (
	"context"
	"errors"
	"sync"
	"testing"

	"github.com/google/uuid"

	"github.com/nextlevelbuilder/goclaw/internal/bus"
	"github.com/nextlevelbuilder/goclaw/internal/store"
	"github.com/nextlevelbuilder/goclaw/pkg/protocol"
)

// ownerDisplayResolverStub is a minimal AgentDisplayResolver for builder/publish
// tests. It maps an agent key to a display name; a missing key returns an error
// so the not-found / lookup-failure branch can be exercised.
type ownerDisplayResolverStub struct {
	mu        sync.Mutex
	byKey     map[string]string
	calls     int
	forceErr  bool
	lastKey   string
	lastCtxTn uuid.UUID
}

func newOwnerDisplayResolverStub() *ownerDisplayResolverStub {
	return &ownerDisplayResolverStub{byKey: map[string]string{}}
}

func (r *ownerDisplayResolverStub) GetByKey(ctx context.Context, key string) (*store.AgentData, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.calls++
	r.lastKey = key
	r.lastCtxTn = store.TenantIDFromContext(ctx)
	if r.forceErr {
		return nil, errors.New("boom")
	}
	name, ok := r.byKey[key]
	if !ok {
		return nil, store.ErrTaskNotFound
	}
	return &store.AgentData{AgentKey: key, DisplayName: name}, nil
}

func workflowUUID(t *testing.T, s string) *uuid.UUID {
	t.Helper()
	id := uuid.MustParse(s)
	return &id
}

// (1) create event carries full committed identity/workflow/revision.
func TestBuildTeamTaskEventPayload_CreateCarriesCommittedIdentity(t *testing.T) {
	wf := workflowUUID(t, "11111111-1111-1111-1111-111111111111")
	task := &store.TeamTaskData{
		BaseModel:      store.BaseModel{ID: uuid.New()},
		TeamID:         uuid.New(),
		TenantID:       uuid.New(),
		TaskNumber:     42,
		Subject:        "Committed subject",
		Status:         store.TeamTaskStatusPending,
		WorkflowID:     wf,
		WorkflowStepID: "step-A",
		PlanRevision:   3,
	}
	got := BuildTeamTaskEventPayload(task, "", TeamTaskEventOptions{
		UserID: "u1", Channel: "dashboard", ActorType: "human", ActorID: "u1",
	})
	if got.TeamID != task.TeamID.String() || got.TaskID != task.ID.String() {
		t.Fatalf("identity not derived from row: %+v", got)
	}
	if got.TaskNumber != 42 || got.Subject != "Committed subject" || got.Status != store.TeamTaskStatusPending {
		t.Fatalf("identity/status not from row: %+v", got)
	}
	if got.WorkflowID != wf.String() || got.WorkflowStepID != "step-A" || got.PlanRevision != 3 {
		t.Fatalf("workflow/revision not from row: %+v", got)
	}
	if got.Timestamp == "" {
		t.Fatal("timestamp must default to now")
	}
}

// (2) auto-assign & manual assign resolve owner key/display from the row + resolver.
func TestBuildTeamTaskEventPayload_AssignResolvesOwner(t *testing.T) {
	task := &store.TeamTaskData{
		BaseModel:     store.BaseModel{ID: uuid.New()},
		TeamID:        uuid.New(),
		Status:        store.TeamTaskStatusInProgress,
		OwnerAgentKey: "worker-key",
	}
	got := BuildTeamTaskEventPayload(task, "Worker Display", TeamTaskEventOptions{ActorType: "human"})
	if got.OwnerAgentKey != "worker-key" {
		t.Fatalf("owner key = %q, want worker-key", got.OwnerAgentKey)
	}
	if got.OwnerDisplayName != "Worker Display" {
		t.Fatalf("owner display = %q, want Worker Display", got.OwnerDisplayName)
	}
}

// (3) an unassigned committed row clears stale caller owner key/display input.
func TestBuildTeamTaskEventPayload_UnassignClearsStaleOwner(t *testing.T) {
	task := &store.TeamTaskData{
		BaseModel:     store.BaseModel{ID: uuid.New()},
		TeamID:        uuid.New(),
		Status:        store.TeamTaskStatusPending,
		OwnerAgentKey: "", // committed row has no owner
	}
	got := enrichTaskEventPayloadFromAuthoritative(context.Background(), nil,
		protocol.TeamTaskEventPayload{OwnerAgentKey: "stale-owner", OwnerDisplayName: "Stale Name"},
		task, protocol.EventTeamTaskUpdated)
	if got.OwnerAgentKey != "" {
		t.Fatalf("unassign must clear owner key, got %q", got.OwnerAgentKey)
	}
	if got.OwnerDisplayName != "" {
		t.Fatalf("unassign must clear owner display, got %q", got.OwnerDisplayName)
	}
}

// (4) stale caller subject/status/workflow/revision does NOT beat the committed row.
func TestPublishTaskEvent_StaleCallerLosesToCommittedRow(t *testing.T) {
	teamStore := newEventClaimingTaskStore()
	tenant := uuid.New()
	taskID := uuid.New()
	teamID := uuid.New()
	wf := workflowUUID(t, "22222222-2222-2222-2222-222222222222")
	teamStore.tasks[taskID] = &store.TeamTaskData{
		BaseModel: store.BaseModel{ID: taskID}, TeamID: teamID, TenantID: tenant,
		TaskNumber: 9, Subject: "Authoritative", Status: store.TeamTaskStatusInProgress,
		WorkflowID: wf, WorkflowStepID: "real-step", PlanRevision: 5,
		OwnerAgentKey: "real-owner",
	}
	resolver := newOwnerDisplayResolverStub()
	resolver.byKey["real-owner"] = "Real Owner"

	msgBus := bus.New()
	var events []bus.Event
	msgBus.Subscribe("cap", func(evt bus.Event) { events = append(events, evt) })

	stale := protocol.TeamTaskEventPayload{
		TaskID: taskID.String(), TeamID: uuid.NewString(),
		Subject: "STALE", Status: store.TeamTaskStatusPending,
		WorkflowID: uuid.NewString(), WorkflowStepID: "STALE-step", PlanRevision: 99,
		OwnerAgentKey: "STALE-owner", OwnerDisplayName: "STALE Name",
	}
	if !PublishTaskEventWithResolver(teamStore, msgBus, resolver, protocol.EventTeamTaskProgress, stale, uuid.New()) {
		t.Fatal("event must publish")
	}
	if len(events) != 1 {
		t.Fatalf("fanout = %d, want 1", len(events))
	}
	p := events[0].Payload.(protocol.TeamTaskEventPayload)
	if p.TeamID != teamID.String() || p.Subject != "Authoritative" || p.Status != store.TeamTaskStatusInProgress {
		t.Fatalf("stale identity/status leaked: %+v", p)
	}
	if p.WorkflowID != wf.String() || p.WorkflowStepID != "real-step" || p.PlanRevision != 5 {
		t.Fatalf("stale workflow/revision leaked: %+v", p)
	}
	if p.OwnerAgentKey != "real-owner" || p.OwnerDisplayName != "Real Owner" {
		t.Fatalf("owner not authoritative: %+v", p)
	}
	if resolver.lastCtxTn != tenant {
		t.Fatalf("owner lookup ran under tenant %s, want %s", resolver.lastCtxTn, tenant)
	}
}

// (5) replay publication keeps the same EventID security + authoritative payload.
func TestPublishTaskEvent_ReplayKeepsEventIDSecurityAndAuthority(t *testing.T) {
	teamStore := newEventClaimingTaskStore()
	tenant := uuid.New()
	taskID := uuid.New()
	teamID := uuid.New()
	teamStore.tasks[taskID] = &store.TeamTaskData{
		BaseModel: store.BaseModel{ID: taskID}, TeamID: teamID, TenantID: tenant,
		Subject: "Auth", Status: store.TeamTaskStatusInProgress, OwnerAgentKey: "o",
	}
	resolver := newOwnerDisplayResolverStub()
	resolver.byKey["o"] = "Owner"
	msgBus := bus.New()
	var events []bus.Event
	msgBus.Subscribe("cap", func(evt bus.Event) { events = append(events, evt) })

	eventID := uuid.New()
	payload := protocol.TeamTaskEventPayload{TaskID: taskID.String()}
	if !PublishTaskEventWithResolver(teamStore, msgBus, resolver, protocol.EventTeamTaskProgress, payload, eventID) {
		t.Fatal("first publish must succeed")
	}
	// Exact replay: same EventID + type must not fanout again.
	if PublishTaskEventWithResolver(teamStore, msgBus, resolver, protocol.EventTeamTaskProgress, payload, eventID) {
		t.Fatal("replay must not fanout")
	}
	// Same EventID, different type -> rejected.
	if PublishTaskEventWithResolver(teamStore, msgBus, resolver, protocol.EventTeamTaskCommented, payload, eventID) {
		t.Fatal("EventID reuse with different type must be rejected")
	}
	if len(events) != 1 || len(teamStore.claims) != 1 {
		t.Fatalf("replay leaked: events=%d claims=%d", len(events), len(teamStore.claims))
	}
	p := events[0].Payload.(protocol.TeamTaskEventPayload)
	if p.Subject != "Auth" || p.OwnerDisplayName != "Owner" {
		t.Fatalf("first payload not authoritative: %+v", p)
	}
}

// (6) workflow-action task event carries the current revision/step from the row.
func TestPublishTaskEvent_WorkflowActionCarriesCurrentRevisionStep(t *testing.T) {
	teamStore := newEventClaimingTaskStore()
	tenant := uuid.New()
	taskID := uuid.New()
	teamID := uuid.New()
	wf := workflowUUID(t, "33333333-3333-3333-3333-333333333333")
	teamStore.tasks[taskID] = &store.TeamTaskData{
		BaseModel: store.BaseModel{ID: taskID}, TeamID: teamID, TenantID: tenant,
		Status: store.TeamTaskStatusPending, WorkflowID: wf,
		WorkflowStepID: "recover-step", PlanRevision: 7, OwnerAgentKey: "coord",
	}
	resolver := newOwnerDisplayResolverStub()
	resolver.byKey["coord"] = "Coordinator"
	msgBus := bus.New()
	var events []bus.Event
	msgBus.Subscribe("cap", func(evt bus.Event) { events = append(events, evt) })

	// Recovery events flow through BuildTaskEventPayload; the publish path
	// re-derives workflow/revision/step from the committed row.
	payload := BuildTaskEventPayload(teamID.String(), taskID.String(), store.TeamTaskStatusPending, "agent", "coord",
		WithReason("revised instruction"))
	if !PublishTaskEventWithResolver(teamStore, msgBus, resolver, protocol.EventTeamTaskUpdated, payload, uuid.New()) {
		t.Fatal("workflow-action event must publish")
	}
	p := events[0].Payload.(protocol.TeamTaskEventPayload)
	if p.WorkflowID != wf.String() || p.WorkflowStepID != "recover-step" || p.PlanRevision != 7 {
		t.Fatalf("workflow action event missing current revision/step: %+v", p)
	}
	if p.OwnerDisplayName != "Coordinator" {
		t.Fatalf("owner display not resolved: %+v", p)
	}
}

// owner-lookup failure must not fail publication; owner key kept, display empty.
func TestPublishTaskEvent_OwnerLookupFailureDoesNotFailPublication(t *testing.T) {
	teamStore := newEventClaimingTaskStore()
	tenant := uuid.New()
	taskID := uuid.New()
	teamStore.tasks[taskID] = &store.TeamTaskData{
		BaseModel: store.BaseModel{ID: taskID}, TeamID: uuid.New(), TenantID: tenant,
		Status: store.TeamTaskStatusInProgress, OwnerAgentKey: "missing-owner",
	}
	resolver := newOwnerDisplayResolverStub()
	resolver.forceErr = true
	msgBus := bus.New()
	var events []bus.Event
	msgBus.Subscribe("cap", func(evt bus.Event) { events = append(events, evt) })

	payload := protocol.TeamTaskEventPayload{TaskID: taskID.String()}
	if !PublishTaskEventWithResolver(teamStore, msgBus, resolver, protocol.EventTeamTaskProgress, payload, uuid.New()) {
		t.Fatal("publication must not fail on owner-lookup error")
	}
	p := events[0].Payload.(protocol.TeamTaskEventPayload)
	if p.OwnerAgentKey != "missing-owner" {
		t.Fatalf("authoritative owner key must be kept, got %q", p.OwnerAgentKey)
	}
	if p.OwnerDisplayName != "" {
		t.Fatalf("owner display must be empty on lookup failure, got %q", p.OwnerDisplayName)
	}
}
