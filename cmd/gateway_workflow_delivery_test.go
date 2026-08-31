package cmd

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/nextlevelbuilder/goclaw/internal/agent"
	"github.com/nextlevelbuilder/goclaw/internal/bus"
	"github.com/nextlevelbuilder/goclaw/internal/providers"
	"github.com/nextlevelbuilder/goclaw/internal/scheduler"
	"github.com/nextlevelbuilder/goclaw/internal/store"
	"github.com/nextlevelbuilder/goclaw/internal/tools"
)

// --- deliverWorkflowFinalOutput: inbound (channel) path ---

// workflowDeliveryRecordingStore drives the real deliverWorkflowFinalOutput. It
// embeds the full TeamWorkflowStore interface and overrides only the delivery
// CAS methods: ClaimWorkflowDelivery returns the workflow + token;
// CompleteWorkflowDelivery and FailWorkflowDeliveryAttempt record calls.
type workflowDeliveryRecordingStore struct {
	store.TeamWorkflowStore

	workflow   *store.TeamWorkflowData
	claimToken uuid.UUID

	deliveryClaims    int
	deliveryCompletes int
	deliveryFails     int
}

func (s *workflowDeliveryRecordingStore) ClaimWorkflowDelivery(_ context.Context, _ uuid.UUID, _ time.Time) (*store.TeamWorkflowData, uuid.UUID, error) {
	s.deliveryClaims++
	return s.workflow, s.claimToken, nil
}

func (s *workflowDeliveryRecordingStore) CompleteWorkflowDelivery(_ context.Context, _ uuid.UUID, _ uuid.UUID) error {
	s.deliveryCompletes++
	return nil
}

func (s *workflowDeliveryRecordingStore) FailWorkflowDeliveryAttempt(_ context.Context, _ uuid.UUID, _ uuid.UUID, _ string) (*store.TeamWorkflowData, error) {
	s.deliveryFails++
	return s.workflow, nil
}

// TestDeliverWorkflowFinalOutputInboundSendsResultSummary proves the channel
// delivery path sends ONLY workflow.ResultSummary to the requester's origin
// channel/chat — nothing else from the task list leaks.
func TestDeliverWorkflowFinalOutputInboundSendsResultSummary(t *testing.T) {
	workflowID := uuid.New()
	terminal := "The one and only terminal answer for the requester."
	workflow := &store.TeamWorkflowData{
		BaseModel:     store.BaseModel{ID: workflowID},
		OriginChannel: "telegram",
		OriginChatID:  "chat-123",
		ResultSummary: terminal,
	}
	mock := &workflowDeliveryRecordingStore{workflow: workflow, claimToken: uuid.New()}
	mb := bus.New()
	deps := &ConsumerDeps{MsgBus: mb}

	deliverWorkflowFinalOutput(context.Background(), deps, mock, workflowID)

	if mock.deliveryClaims != 1 {
		t.Fatalf("ClaimWorkflowDelivery calls = %d, want 1", mock.deliveryClaims)
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	msg, ok := mb.SubscribeOutbound(ctx)
	if !ok {
		t.Fatal("no outbound message published to requester channel")
	}
	if msg.Content != terminal {
		t.Errorf("outbound content = %q, want ResultSummary %q", msg.Content, terminal)
	}
	if msg.Channel != "telegram" || msg.ChatID != "chat-123" {
		t.Errorf("outbound target = %s/%s, want telegram/chat-123", msg.Channel, msg.ChatID)
	}
	// Simulate the channel dispatcher's ack to exercise the completion path.
	msg.DeliveryAck(nil)
	if mock.deliveryCompletes != 1 {
		t.Errorf("CompleteWorkflowDelivery calls = %d, want 1 after ack", mock.deliveryCompletes)
	}
}

// --- deliverWorkflowFinalOutput: WS path ---

// wsSessionMock satisfies store.SessionStore (via nil-interface embedding) and
// records AddMessage/Save calls, proving the WS delivery path persists
// ResultSummary to session history rather than publishing to the outbound bus.
type wsSessionMock struct {
	store.SessionStore

	addedKeys     []string
	addedMessages []providers.Message
	saveCalls     int
}

func (m *wsSessionMock) GetHistory(_ context.Context, _ string) []providers.Message { return nil }
func (m *wsSessionMock) AddMessage(_ context.Context, key string, msg providers.Message) {
	m.addedKeys = append(m.addedKeys, key)
	m.addedMessages = append(m.addedMessages, msg)
}
func (m *wsSessionMock) Save(_ context.Context, _ string) error {
	m.saveCalls++
	return nil
}

// TestDeliverWorkflowFinalOutputWSPersistsToSessionHistory proves the WS delivery
// path writes ResultSummary as an assistant message to the origin session and
// does NOT publish to the outbound bus.
func TestDeliverWorkflowFinalOutputWSPersistsToSessionHistory(t *testing.T) {
	workflowID := uuid.New()
	terminal := "WS terminal answer persisted to session."
	workflow := &store.TeamWorkflowData{
		BaseModel:        store.BaseModel{ID: workflowID},
		OriginChannel:    "ws",
		OriginSessionKey: "ws-session-1",
		ResultSummary:    terminal,
	}
	mock := &workflowDeliveryRecordingStore{workflow: workflow, claimToken: uuid.New()}
	sessMock := &wsSessionMock{}
	mb := bus.New()
	deps := &ConsumerDeps{MsgBus: mb, SessStore: sessMock}

	deliverWorkflowFinalOutput(context.Background(), deps, mock, workflowID)

	if len(sessMock.addedMessages) != 1 {
		t.Fatalf("AddMessage calls = %d, want 1", len(sessMock.addedMessages))
	}
	if sessMock.addedMessages[0].Role != "assistant" || sessMock.addedMessages[0].Content != terminal {
		t.Errorf("persisted message = %q (role %q), want assistant/%q",
			sessMock.addedMessages[0].Content, sessMock.addedMessages[0].Role, terminal)
	}
	if sessMock.addedKeys[0] != "ws-session-1" {
		t.Errorf("persisted to session %q, want ws-session-1", sessMock.addedKeys[0])
	}
	if sessMock.saveCalls != 1 {
		t.Errorf("Save calls = %d, want 1", sessMock.saveCalls)
	}
	if mock.deliveryCompletes != 1 {
		t.Errorf("CompleteWorkflowDelivery calls = %d, want 1 (WS ack is immediate)", mock.deliveryCompletes)
	}
	// WS must NOT publish to the outbound bus.
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	if _, ok := mb.SubscribeOutbound(ctx); ok {
		t.Fatal("WS delivery must not publish to outbound bus")
	}
}

// --- resolveWorkflowTaskOutcome: intermediate vs terminal finalization gate ---

// workflowSettleRecordingStore drives the real resolveWorkflowTaskOutcome. It
// embeds both TeamStore (for GetTask) and TeamWorkflowStore (for the cast +
// settle + finalize methods), overriding only what the settle path calls.
type workflowSettleRecordingStore struct {
	store.TeamStore
	store.TeamWorkflowStore

	task       *store.TeamTaskData
	transition store.WorkflowTaskTransition

	settleCalls       int
	finalizationCalls int
	finalizationDone  chan struct{}
}

func (s *workflowSettleRecordingStore) GetTask(_ context.Context, _ uuid.UUID) (*store.TeamTaskData, error) {
	return s.task, nil
}

// GetWorkflow returns a not-yet-finalized workflow so finalizeWorkflow proceeds
// to ClaimWorkflowFinalization (the call we count) instead of the
// already-finalized delivery shortcut.
func (s *workflowSettleRecordingStore) GetWorkflow(_ context.Context, _ uuid.UUID) (*store.TeamWorkflowData, error) {
	return &store.TeamWorkflowData{BaseModel: store.BaseModel{ID: s.transition.WorkflowID}}, nil
}

func (s *workflowSettleRecordingStore) CompleteWorkflowTaskAttempt(_ context.Context, _ store.WorkflowTaskAttempt, _ string) (store.WorkflowTaskTransition, error) {
	s.settleCalls++
	return s.transition, nil
}

func (s *workflowSettleRecordingStore) ClaimWorkflowFinalization(_ context.Context, _ uuid.UUID, _ time.Time) (*store.TeamWorkflowData, uuid.UUID, error) {
	s.finalizationCalls++
	if s.finalizationDone != nil {
		select {
		case <-s.finalizationDone:
		default:
			close(s.finalizationDone)
		}
	}
	return nil, uuid.Nil, errors.New("finalization disabled in settle test")
}

func usableRunOutcome() scheduler.RunOutcome {
	return scheduler.RunOutcome{
		Result: &agent.RunResult{Content: "A substantive step result long enough to pass the usability floor."},
	}
}

// TestResolveWorkflowTaskOutcomeIntermediateDoesNotFinalize proves that an
// intermediate task completion (ReadyToFinalize=false) settles the task and
// dispatches dependents but NEVER triggers finalizeWorkflow — the requester sees
// nothing until the terminal task completes.
func TestResolveWorkflowTaskOutcomeIntermediateDoesNotFinalize(t *testing.T) {
	workflowID := uuid.New()
	task := &store.TeamTaskData{Status: store.TeamTaskStatusInProgress}
	mock := &workflowSettleRecordingStore{
		task: task,
		transition: store.WorkflowTaskTransition{
			Outcome:         store.WorkflowMutationApplied,
			WorkflowID:      workflowID,
			WorkflowStatus:  store.TeamWorkflowStatusRunning,
			ReadyToFinalize: false,
		},
	}
	deps := &ConsumerDeps{TeamStore: mock}
	flags := &tools.TaskActionFlags{Completed: true}
	attempt := &store.WorkflowTaskAttempt{
		WorkflowID:    workflowID,
		TaskID:        uuid.New(),
		TeamID:        uuid.New(),
		DispatchToken: uuid.New(),
	}

	resolveWorkflowTaskOutcome(context.Background(), deps, usableRunOutcome(), flags, map[string]string{}, attempt)

	if mock.settleCalls != 1 {
		t.Fatalf("CompleteWorkflowTaskAttempt calls = %d, want 1", mock.settleCalls)
	}
	if mock.finalizationCalls != 0 {
		t.Fatalf("ClaimWorkflowFinalization calls = %d, want 0 (intermediate must not finalize)", mock.finalizationCalls)
	}
}

// TestResolveWorkflowTaskOutcomeTerminalTriggersFinalize proves that when the
// last task settles with ReadyToFinalize=true, resolveWorkflowTaskOutcome
// launches finalizeWorkflow (which then delivers the terminal result).
func TestResolveWorkflowTaskOutcomeTerminalTriggersFinalize(t *testing.T) {
	workflowID := uuid.New()
	task := &store.TeamTaskData{Status: store.TeamTaskStatusInProgress}
	done := make(chan struct{})
	mock := &workflowSettleRecordingStore{
		task: task,
		transition: store.WorkflowTaskTransition{
			Outcome:         store.WorkflowMutationApplied,
			WorkflowID:      workflowID,
			WorkflowStatus:  store.TeamWorkflowStatusRunning,
			ReadyToFinalize: true,
		},
		finalizationDone: done,
	}
	deps := &ConsumerDeps{TeamStore: mock}
	flags := &tools.TaskActionFlags{Completed: true}
	attempt := &store.WorkflowTaskAttempt{
		WorkflowID:    workflowID,
		TaskID:        uuid.New(),
		TeamID:        uuid.New(),
		DispatchToken: uuid.New(),
	}

	resolveWorkflowTaskOutcome(context.Background(), deps, usableRunOutcome(), flags, map[string]string{}, attempt)

	// finalizeWorkflow is launched as a goroutine; wait for it to reach the claim.
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("finalizeWorkflow was not triggered within 2s after ReadyToFinalize")
	}
	if mock.finalizationCalls != 1 {
		t.Fatalf("ClaimWorkflowFinalization calls = %d, want 1", mock.finalizationCalls)
	}
}
