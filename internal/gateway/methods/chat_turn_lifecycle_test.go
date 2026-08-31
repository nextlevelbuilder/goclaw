package methods

import (
	"context"
	"encoding/json"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/nextlevelbuilder/goclaw/internal/agent"
	"github.com/nextlevelbuilder/goclaw/internal/config"
	"github.com/nextlevelbuilder/goclaw/internal/gateway"
	"github.com/nextlevelbuilder/goclaw/internal/i18n"
	"github.com/nextlevelbuilder/goclaw/internal/permissions"
	"github.com/nextlevelbuilder/goclaw/internal/providers"
	"github.com/nextlevelbuilder/goclaw/internal/store"
	"github.com/nextlevelbuilder/goclaw/pkg/protocol"
)

// Phase 7 Decision 4 — stable turnId + terminal chat.turn lifecycle.
//
// A logical turn (one debounce batch) carries a stable turnId assigned before
// enqueue and shared by pointer across every copy of the batch. Its lifecycle is
// published on the EventChatTurn transport as status-only hints: queued →
// running (links turnId→runId) → EXACTLY ONE terminal (completed | cancelled |
// failed). The terminal sync.Once latch is INDEPENDENT of the per-request
// respondedOnce RPC latch (Decision 4 point 8): a turn whose RPC was already
// consumed by a {queued:true} ack still emits its terminal lifecycle, and a
// queued ack never suppresses a later terminal event (nor vice versa).
//
// These frames carry NO assistant content (point 7); the assistant result flows
// only through the normal run/history path (proven end-to-end in Decision 5).

// turnEvent is a decoded EventChatTurn frame's payload fields under test.
type turnEvent struct {
	turnID string
	state  string
	runID  string
}

// decodeFrame classifies one raw frame off a capturing client's channel. The
// client multiplexes RPC responses (type:"res") and events (type:"event") onto a
// single byte channel, so a lifecycle test must sort them: a chat.turn event
// yields a *turnEvent, an RPC frame yields a *protocol.ResponseFrame, and any
// other event yields (nil, nil) so callers ignore unrelated pushes.
func decodeFrame(raw []byte) (*protocol.ResponseFrame, *turnEvent) {
	var head struct {
		Type  string `json:"type"`
		Event string `json:"event"`
	}
	_ = json.Unmarshal(raw, &head)
	if head.Type == protocol.FrameTypeEvent {
		if head.Event != protocol.EventChatTurn {
			return nil, nil
		}
		var ef protocol.EventFrame
		_ = json.Unmarshal(raw, &ef)
		p, _ := ef.Payload.(map[string]any)
		te := &turnEvent{}
		te.turnID, _ = p["turnId"].(string)
		te.state, _ = p["state"].(string)
		te.runID, _ = p["runId"].(string)
		return nil, te
	}
	var rf protocol.ResponseFrame
	_ = json.Unmarshal(raw, &rf)
	return &rf, nil
}

// drainFrames reads every frame off ch until `quiet` elapses with none arriving
// (bounded by a hard cap), classifying each into responses and chat.turn events.
// The returned slices preserve true channel/emission order, so a test can assert
// both cardinality (exactly one completed) and ordering (queued before running
// before terminal) from a single drain.
func drainFrames(t *testing.T, ch <-chan []byte, quiet time.Duration) (resps []protocol.ResponseFrame, turns []turnEvent) {
	t.Helper()
	hardCap := time.After(3 * time.Second)
	for {
		select {
		case raw := <-ch:
			if resp, te := decodeFrame(raw); resp != nil {
				resps = append(resps, *resp)
			} else if te != nil {
				turns = append(turns, *te)
			}
		case <-time.After(quiet):
			return
		case <-hardCap:
			return
		}
	}
}

// readNextResponse reads frames until the next RPC response arrives, buffering
// any chat.turn events it passes into *turns so a later drain sees them in order.
func readNextResponse(t *testing.T, ch <-chan []byte, turns *[]turnEvent) *protocol.ResponseFrame {
	t.Helper()
	deadline := time.After(2 * time.Second)
	for {
		select {
		case raw := <-ch:
			if resp, te := decodeFrame(raw); resp != nil {
				return resp
			} else if te != nil {
				*turns = append(*turns, *te)
			}
		case <-deadline:
			t.Fatal("timeout waiting for RPC response")
			return nil
		}
	}
}

// countTerminals reports how many of the given turn events are terminal states.
func countTerminals(turns []turnEvent) int {
	n := 0
	for _, te := range turns {
		switch te.state {
		case protocol.ChatTurnCompleted, protocol.ChatTurnCancelled, protocol.ChatTurnFailed:
			n++
		}
	}
	return n
}

// firstOfState returns the first turn event with the given state, or nil.
func firstOfState(turns []turnEvent, state string) *turnEvent {
	for i := range turns {
		if turns[i].state == state {
			return &turns[i]
		}
	}
	return nil
}

// labeledSessionStore is a minimal store.SessionStore whose GetLabel returns a
// non-empty label so the WS success path SKIPS the async title-generation
// goroutine (which would otherwise touch usage caps / event bus). Every other
// method is an inert no-op: Decision 4 asserts lifecycle EVENTS, not persistence
// (the real session/history integration is Decision 5). It exists only so the
// completed path does not nil-deref m.sessions.GetLabel.
type labeledSessionStore struct{}

func (labeledSessionStore) GetOrCreate(context.Context, string) *store.SessionData {
	return &store.SessionData{}
}
func (labeledSessionStore) Get(context.Context, string) *store.SessionData          { return nil }
func (labeledSessionStore) AddMessage(context.Context, string, providers.Message)   {}
func (labeledSessionStore) GetHistory(context.Context, string) []providers.Message  { return nil }
func (labeledSessionStore) GetSummary(context.Context, string) string               { return "" }
func (labeledSessionStore) SetSummary(context.Context, string, string)              {}
func (labeledSessionStore) GetLabel(context.Context, string) string                 { return "titled" }
func (labeledSessionStore) SetLabel(context.Context, string, string)                {}
func (labeledSessionStore) SetAgentInfo(context.Context, string, uuid.UUID, string) {}
func (labeledSessionStore) TruncateHistory(context.Context, string, int)            {}
func (labeledSessionStore) SetHistory(context.Context, string, []providers.Message) {}
func (labeledSessionStore) Reset(context.Context, string)                           {}
func (labeledSessionStore) Delete(context.Context, string) error                    { return nil }
func (labeledSessionStore) Save(context.Context, string) error                      { return nil }

func (labeledSessionStore) UpdateMetadata(context.Context, string, string, string, string) {}
func (labeledSessionStore) AccumulateTokens(context.Context, string, int64, int64)         {}
func (labeledSessionStore) IncrementCompaction(context.Context, string)                    {}
func (labeledSessionStore) GetCompactionCount(context.Context, string) int                 { return 0 }
func (labeledSessionStore) GetMemoryFlushCompactionCount(context.Context, string) int      { return 0 }
func (labeledSessionStore) SetMemoryFlushDone(context.Context, string)                     {}
func (labeledSessionStore) GetSessionMetadata(context.Context, string) map[string]string   { return nil }
func (labeledSessionStore) SetSessionMetadata(context.Context, string, map[string]string)  {}
func (labeledSessionStore) SetSpawnInfo(context.Context, string, string, int)              {}
func (labeledSessionStore) SetContextWindow(context.Context, string, int)                  {}
func (labeledSessionStore) GetContextWindow(context.Context, string) int                   { return 0 }
func (labeledSessionStore) SetLastPromptTokens(context.Context, string, int, int)          {}
func (labeledSessionStore) GetLastPromptTokens(context.Context, string) (int, int)         { return 0, 0 }

func (labeledSessionStore) List(context.Context, string) []store.SessionInfo { return nil }
func (labeledSessionStore) ListPaged(context.Context, store.SessionListOpts) store.SessionListResult {
	return store.SessionListResult{}
}
func (labeledSessionStore) ListPagedRich(context.Context, store.SessionListOpts) store.SessionListRichResult {
	return store.SessionListRichResult{}
}
func (labeledSessionStore) LastUsedChannel(context.Context, string) (string, string) {
	return "", ""
}

// panicLoop is an agent.Agent whose Run panics, exercising the async-run panic
// recover arm (chat.go): the recover resolves the batch with a terminal RPC
// error AND emits exactly one failed chat.turn lifecycle.
type panicLoop struct {
	id       uuid.UUID
	provider providers.Provider
	entered  chan struct{}
	once     sync.Once
}

func newPanicLoop() *panicLoop {
	return &panicLoop{id: uuid.New(), provider: &chatTeamWorkTestProvider{}, entered: make(chan struct{})}
}

func (l *panicLoop) ID() string                   { return "panic-loop" }
func (l *panicLoop) UUID() uuid.UUID              { return l.id }
func (l *panicLoop) OtherConfig() json.RawMessage { return nil }
func (l *panicLoop) IsRunning() bool              { return false }
func (l *panicLoop) Model() string                { return "test-model" }
func (l *panicLoop) ProviderName() string         { return l.provider.Name() }
func (l *panicLoop) Provider() providers.Provider { return l.provider }
func (l *panicLoop) Run(context.Context, agent.RunRequest) (*agent.RunResult, error) {
	l.once.Do(func() { close(l.entered) })
	panic("boom in run")
}

// errorLoop is an agent.Agent whose Run returns a non-context error, exercising
// the run goroutine's error branch (sendChatError + failed lifecycle) WITHOUT a
// context cancellation (so it is failed, not cancelled).
type errorLoop struct {
	id       uuid.UUID
	provider providers.Provider
	entered  chan struct{}
	once     sync.Once
}

func newErrorLoop() *errorLoop {
	return &errorLoop{id: uuid.New(), provider: &chatTeamWorkTestProvider{}, entered: make(chan struct{})}
}

func (l *errorLoop) ID() string                   { return "error-loop" }
func (l *errorLoop) UUID() uuid.UUID              { return l.id }
func (l *errorLoop) OtherConfig() json.RawMessage { return nil }
func (l *errorLoop) IsRunning() bool              { return false }
func (l *errorLoop) Model() string                { return "test-model" }
func (l *errorLoop) ProviderName() string         { return l.provider.Name() }
func (l *errorLoop) Provider() providers.Provider { return l.provider }
func (l *errorLoop) Run(context.Context, agent.RunRequest) (*agent.RunResult, error) {
	l.once.Do(func() { close(l.entered) })
	return nil, errors.New("deliberate run failure")
}

// lifecycleRequest builds a chatSendRequest wired exactly as production wires it
// at chat.go request construction (Decision 4): a pre-enqueue turnLifecycle
// (stable turnId + terminal latch) AND the per-request exactly-one-RPC latch,
// both shared by pointer across every copy through the FIFO queue and run.
func lifecycleRequest(client *gateway.Client, requestID string, loop agent.Agent, sessionKey, message string) chatSendRequest {
	return chatSendRequest{
		ctx:           context.Background(),
		client:        client,
		requestID:     requestID,
		loop:          loop,
		sessionKey:    sessionKey,
		params:        chatSendParams{Message: message, SessionKey: sessionKey},
		respondedOnce: &sync.Once{},
		turnLifecycle: &turnLifecycle{turnID: uuid.NewString()},
	}
}

// TestTurnLifecycle_BusyAckCarriesStableTurnId proves the busy follow-up's
// {queued:true} RPC ack carries a stable turnId, and that the SAME turnId keys
// the subsequent queued chat.turn lifecycle event — so a client that received
// only the ack can correlate every later status hint for the turn.
func TestTurnLifecycle_BusyAckCarriesStableTurnId(t *testing.T) {
	m, _ := newLifecycleChatMethods()

	primary := newDispatchLifecycleLoop(false)
	followup := newDispatchLifecycleLoop(false)

	c1, _ := gateway.NewCapturingTestClient(permissions.RoleOperator, uuid.Nil, "u", 8)
	c2, ch2 := gateway.NewCapturingTestClient(permissions.RoleOperator, uuid.Nil, "u", 8)
	sessionKey := "session-turn-ack"

	m.dispatchChatSends([]chatSendRequest{
		lifecycleRequest(c1, "r1", primary, sessionKey, "first"),
	})
	select {
	case <-primary.entered:
	case <-time.After(2 * time.Second):
		t.Fatal("primary run never started")
	}

	m.dispatchChatSends([]chatSendRequest{
		lifecycleRequest(c2, "r2", followup, sessionKey, "second"),
	})

	var seen []turnEvent
	ack := readNextResponse(t, ch2, &seen)
	if ack.ID != "r2" {
		t.Fatalf("queued ack ID = %q, want r2", ack.ID)
	}
	assertQueuedAck(t, ack)

	payload := ack.Payload.(map[string]any)
	ackTurnID, _ := payload["turnId"].(string)
	if ackTurnID == "" {
		t.Fatalf("queued ack missing stable turnId: %#v", payload)
	}

	// The queued lifecycle event (already buffered by readNextResponse, or arriving
	// right after) must carry the same turnId as the ack.
	_, turns := drainFrames(t, ch2, 150*time.Millisecond)
	turns = append(seen, turns...)
	queued := firstOfState(turns, protocol.ChatTurnQueued)
	if queued == nil {
		t.Fatalf("no queued chat.turn lifecycle event observed; got %+v", turns)
	}
	if queued.turnID != ackTurnID {
		t.Fatalf("queued event turnId %q != ack turnId %q", queued.turnID, ackTurnID)
	}

	// Cleanup: let both runs finish so the worker drains.
	close(primary.release)
	close(followup.release)
	waitChatReservationCleared(t, m.runQueue, sessionKey)
}

// TestTurnLifecycle_QueuedThenCancelled proves a turn cancelled WHILE queued
// (never popped/classified) resolves with exactly one RPC (the queued ack it
// already received) and exactly one terminal cancelled chat.turn event, and its
// agent loop never runs. The RPC latch (consumed by the ack) does not suppress
// the terminal lifecycle event (Decision 4 point 8).
func TestTurnLifecycle_QueuedThenCancelled(t *testing.T) {
	m, _ := newLifecycleChatMethods()

	primary := newDispatchLifecycleLoop(true) // blocks so the followup stays queued
	followup := newDispatchLifecycleLoop(false)

	c1, _ := gateway.NewCapturingTestClient(permissions.RoleOperator, uuid.Nil, "u", 8)
	c2, ch2 := gateway.NewCapturingTestClient(permissions.RoleOperator, uuid.Nil, "u", 8)
	sessionKey := "session-turn-queued-cancel"

	m.dispatchChatSends([]chatSendRequest{
		lifecycleRequest(c1, "r1", primary, sessionKey, "first"),
	})
	select {
	case <-primary.entered:
	case <-time.After(2 * time.Second):
		t.Fatal("primary run never started")
	}

	m.dispatchChatSends([]chatSendRequest{
		lifecycleRequest(c2, "r2", followup, sessionKey, "second"),
	})

	var seen []turnEvent
	ack := readNextResponse(t, ch2, &seen)
	assertQueuedAck(t, ack)
	ackTurnID := ack.Payload.(map[string]any)["turnId"].(string)

	// Cancel drains the still-queued followup batch: sendChatCancelled (suppressed
	// by the already-claimed RPC latch) + a terminal cancelled lifecycle event.
	m.runQueue.Cancel(sessionKey)

	resps, turns := drainFrames(t, ch2, 200*time.Millisecond)
	turns = append(seen, turns...)

	// Exactly one terminal, and it is cancelled with the stable turnId.
	if got := countTerminals(turns); got != 1 {
		t.Fatalf("expected exactly one terminal lifecycle event, got %d: %+v", got, turns)
	}
	cancelled := firstOfState(turns, protocol.ChatTurnCancelled)
	if cancelled == nil {
		t.Fatalf("no cancelled terminal event: %+v", turns)
	}
	if cancelled.turnID != ackTurnID {
		t.Fatalf("cancelled event turnId %q != ack turnId %q", cancelled.turnID, ackTurnID)
	}

	// No SECOND RPC response for r2: the queued ack already claimed its latch.
	for _, r := range resps {
		if r.ID == "r2" {
			t.Fatalf("unexpected second RPC response for r2 after queued ack: %+v", r)
		}
	}

	// The cancelled follow-up's loop must never have run.
	if followup.ranTimes() != 0 {
		t.Fatalf("queued-then-cancelled follow-up ran %d times; must be 0", followup.ranTimes())
	}

	close(primary.release)
	waitChatReservationCleared(t, m.runQueue, sessionKey)
}

// TestTurnLifecycle_ClassifyingThenCancelled proves a primary turn cancelled
// WHILE classifying (popped by the worker, blocking in applyTeamWorkGate, before
// RegisterRun) resolves with exactly one RPC (the cancelled response) and exactly
// one terminal cancelled chat.turn event, and never registers a run or runs its
// loop. There is no queued ack here — the primary was not a busy follow-up.
func TestTurnLifecycle_ClassifyingThenCancelled(t *testing.T) {
	tenantID, leadID, teamID := uuid.New(), uuid.New(), uuid.New()
	router := agent.NewRouter()
	teamStore := orchestratingChatTeamStore(nil, leadID, teamID)
	m := wireHeldClassifierChatMethods(t, router, teamStore, leadID, tenantID)

	held := &heldClassifierProvider{entered: make(chan struct{}), release: make(chan struct{})}
	loop := newDispatchLifecycleLoop(false)
	loop.provider = held
	loop.id = leadID

	sessionKey := "session-turn-classify-cancel"
	c, ch := gateway.NewCapturingTestClient(permissions.RoleOperator, uuid.Nil, "u", 8)
	req := chatSendRequest{
		ctx:           store.WithTenantID(context.Background(), tenantID),
		client:        c,
		requestID:     "r1",
		loop:          loop,
		userID:        "u",
		sessionKey:    sessionKey,
		params:        chatSendParams{Message: chatIngressMsg, AgentID: "team-lead", SessionKey: sessionKey},
		respondedOnce: &sync.Once{},
		turnLifecycle: &turnLifecycle{turnID: uuid.NewString()},
	}
	turnID := req.turnLifecycle.turnID

	m.dispatchChatSends([]chatSendRequest{req})

	waitClosed(t, held.entered, "held classifier to enter (batch classifying)")
	if _, ok := router.SessionRunID(sessionKey); ok {
		t.Fatal("a run registered while the classifier was still blocking")
	}

	// Cancel the popped/classifying batch through the queue's armed handle, then
	// release the classifier so the post-classify recheck resolves it cancelled.
	m.runQueue.Cancel(sessionKey)
	close(held.release)

	resps, turns := drainFrames(t, ch, 300*time.Millisecond)

	// Exactly one RPC (cancelled) for r1.
	var r1 []protocol.ResponseFrame
	for _, r := range resps {
		if r.ID == "r1" {
			r1 = append(r1, r)
		}
	}
	if len(r1) != 1 {
		t.Fatalf("expected exactly one RPC response for r1, got %d: %+v", len(r1), r1)
	}
	if payload, _ := r1[0].Payload.(map[string]any); payload == nil || payload["cancelled"] != true {
		t.Fatalf("r1 RPC is not a cancellation: %+v", r1[0])
	}

	// Exactly one terminal cancelled lifecycle event with the stable turnId.
	if got := countTerminals(turns); got != 1 {
		t.Fatalf("expected exactly one terminal lifecycle event, got %d: %+v", got, turns)
	}
	cancelled := firstOfState(turns, protocol.ChatTurnCancelled)
	if cancelled == nil {
		t.Fatalf("no cancelled terminal event: %+v", turns)
	}
	if cancelled.turnID != turnID {
		t.Fatalf("cancelled event turnId %q != request turnId %q", cancelled.turnID, turnID)
	}

	if loop.ranTimes() != 0 {
		t.Fatalf("classifying-then-cancelled batch ran %d times; must be 0", loop.ranTimes())
	}
	if _, ok := router.SessionRunID(sessionKey); ok {
		t.Fatal("a run registered despite cancellation during classification")
	}
	waitChatReservationCleared(t, m.runQueue, sessionKey)
}

// TestTurnLifecycle_QueuedRunningCompleted proves the full happy path for a busy
// follow-up: queued → running (links turnId→runId) → completed, in that order,
// all sharing the stable turnId; the running and completed events carry the same
// runId; exactly one completed terminal is emitted; and the follow-up receives
// exactly one RPC (the queued ack) — its serialized run's terminal sendChatOK is
// suppressed by the already-claimed RPC latch (no assistant content on the
// lifecycle frames; the result flows via the run/history path).
func TestTurnLifecycle_QueuedRunningCompleted(t *testing.T) {
	router := agent.NewRouter()
	m := NewChatMethods(router, labeledSessionStore{}, &config.Config{}, nil, nil)
	m.debouncer = newChatDebouncer(m.dispatchChatSends)

	primary := newDispatchLifecycleLoop(false)
	followup := newDispatchLifecycleLoop(false)

	c1, _ := gateway.NewCapturingTestClient(permissions.RoleOperator, uuid.Nil, "u", 8)
	c2, ch2 := gateway.NewCapturingTestClient(permissions.RoleOperator, uuid.Nil, "u", 16)
	sessionKey := "session-turn-complete"

	// Primary occupies the session.
	m.dispatchChatSends([]chatSendRequest{
		lifecycleRequest(c1, "r1", primary, sessionKey, "first"),
	})
	select {
	case <-primary.entered:
	case <-time.After(2 * time.Second):
		t.Fatal("primary run never started")
	}

	// Follow-up joins the FIFO → queued ack + queued lifecycle.
	m.dispatchChatSends([]chatSendRequest{
		lifecycleRequest(c2, "r2", followup, sessionKey, "second"),
	})
	var seen []turnEvent
	ack := readNextResponse(t, ch2, &seen)
	assertQueuedAck(t, ack)
	ackTurnID := ack.Payload.(map[string]any)["turnId"].(string)

	// Release the primary; the worker then dequeues + runs the follow-up.
	close(primary.release)
	select {
	case <-followup.entered:
	case <-time.After(2 * time.Second):
		t.Fatal("follow-up run never started after primary completed")
	}
	// Let the follow-up complete → success path → completed lifecycle.
	close(followup.release)

	resps, turns := drainFrames(t, ch2, 300*time.Millisecond)
	turns = append(seen, turns...)

	// Follow-up got exactly ONE RPC on ch2 (the queued ack); no second response.
	for _, r := range resps {
		if r.ID == "r2" {
			t.Fatalf("unexpected second RPC response for r2 (queued run must not re-respond): %+v", r)
		}
	}

	// Lifecycle order for the follow-up's turn: queued → running → completed.
	var order []string
	var runningRunID, completedRunID string
	for _, te := range turns {
		if te.turnID != ackTurnID {
			continue
		}
		order = append(order, te.state)
		if te.state == protocol.ChatTurnRunning {
			runningRunID = te.runID
		}
		if te.state == protocol.ChatTurnCompleted {
			completedRunID = te.runID
		}
	}
	if len(order) < 3 || order[0] != protocol.ChatTurnQueued ||
		order[1] != protocol.ChatTurnRunning || order[len(order)-1] != protocol.ChatTurnCompleted {
		t.Fatalf("lifecycle order for turn %q = %v, want queued→running→…→completed", ackTurnID, order)
	}

	// Exactly one completed terminal for this turn.
	completedCount := 0
	for _, te := range turns {
		if te.turnID == ackTurnID && te.state == protocol.ChatTurnCompleted {
			completedCount++
		}
	}
	if completedCount != 1 {
		t.Fatalf("expected exactly one completed lifecycle event, got %d", completedCount)
	}

	// running and completed link the SAME real runId (Decision 4 point 5).
	if runningRunID == "" {
		t.Fatal("running lifecycle event carried no runId")
	}
	if runningRunID != completedRunID {
		t.Fatalf("running runId %q != completed runId %q", runningRunID, completedRunID)
	}

	waitChatReservationCleared(t, m.runQueue, sessionKey)
}

// TestTurnLifecycle_PanicEmitsSingleFailed proves a run that panics resolves with
// exactly one RPC error and exactly one terminal failed chat.turn event (from the
// async-run panic recover arm), with no duplicate RPC and no assistant output.
func TestTurnLifecycle_PanicEmitsSingleFailed(t *testing.T) {
	m, _ := newLifecycleChatMethods()

	loop := newPanicLoop()
	c, ch := gateway.NewCapturingTestClient(permissions.RoleOperator, uuid.Nil, "u", 8)
	sessionKey := "session-turn-panic"

	req := lifecycleRequest(c, "r1", loop, sessionKey, "boom")
	turnID := req.turnLifecycle.turnID
	m.dispatchChatSends([]chatSendRequest{req})

	select {
	case <-loop.entered:
	case <-time.After(2 * time.Second):
		t.Fatal("panic loop never entered")
	}

	resps, turns := drainFrames(t, ch, 300*time.Millisecond)

	// Exactly one RPC for r1, and it is a terminal error (not a result).
	var r1 []protocol.ResponseFrame
	for _, r := range resps {
		if r.ID == "r1" {
			r1 = append(r1, r)
		}
	}
	if len(r1) != 1 {
		t.Fatalf("expected exactly one RPC response for r1, got %d: %+v", len(r1), r1)
	}
	if r1[0].OK {
		t.Fatalf("panic path must resolve r1 with an error, got OK payload: %+v", r1[0])
	}

	// Exactly one terminal, and it is failed with the stable turnId.
	if got := countTerminals(turns); got != 1 {
		t.Fatalf("expected exactly one terminal lifecycle event, got %d: %+v", got, turns)
	}
	failed := firstOfState(turns, protocol.ChatTurnFailed)
	if failed == nil {
		t.Fatalf("no failed terminal event: %+v", turns)
	}
	if failed.turnID != turnID {
		t.Fatalf("failed event turnId %q != request turnId %q", failed.turnID, turnID)
	}

	waitChatReservationCleared(t, m.runQueue, sessionKey)
}

// coordinatedNonExecutableProvider returns a coordinated-scope team verdict so
// makeCoordinatedTeamResult runs and workflowExecutability is checked against the
// durable roster. Against an EMPTY roster canonicalMembers==0 forces
// insufficient_canonical_members → NonExecutable, the G7 fail-closed path. It is
// the WS twin of cmd.inboundCoordinatedProvider.
type coordinatedNonExecutableProvider struct {
	calls int
}

func (p *coordinatedNonExecutableProvider) Name() string         { return "coordinated-non-exec" }
func (p *coordinatedNonExecutableProvider) DefaultModel() string { return "test-model" }
func (p *coordinatedNonExecutableProvider) ChatStream(context.Context, providers.ChatRequest, func(providers.StreamChunk)) (*providers.ChatResponse, error) {
	return nil, errors.New("streaming not supported")
}
func (p *coordinatedNonExecutableProvider) Chat(context.Context, providers.ChatRequest) (*providers.ChatResponse, error) {
	p.calls++
	return &providers.ChatResponse{Content: `{"decision":"team","scope":"coordinated","reason":"multi-step campaign needs parallel specialists and a synthesis","preferred_owner":"","task_type":"content"}`}, nil
}

// TestTurnLifecycle_NonExecutableCoordinatedTeamFailsClosed proves the G7
// fail-closed path at the real WS dispatch seam: a coordinated request against a
// team whose roster has NO canonical members (empty members list with a canonical
// lead set) must fail closed BEFORE RegisterRun — exactly one ErrFailedPrecondition
// RPC error for the turn, exactly one failed chat.turn terminal lifecycle event
// carrying the stable turnId, and the agent runner never runs (no run registers).
// This is the WS twin of TestInboundPreRun_NonExecutableCoordinatedTeamFailsClosedEndToEnd.
func TestTurnLifecycle_NonExecutableCoordinatedTeamFailsClosed(t *testing.T) {
	tenantID, leadID, teamID := uuid.New(), uuid.New(), uuid.New()
	router := agent.NewRouter()
	// EMPTY members with a canonical lead: the team still has a coordinator
	// (LeadAgentID/LeadAgentKey set), but ListMembers returns NO members, so
	// workflowExecutability counts zero canonical members and fails the
	// coordinated route with insufficient_canonical_members → NonExecutable.
	teamStore := &chatTeamWorkTeamStore{
		team: &store.TeamData{
			BaseModel:    store.BaseModel{ID: teamID},
			Name:         "growth-team",
			LeadAgentID:  leadID,
			LeadAgentKey: "team-lead",
		},
		members: []store.TeamMemberData{},
	}
	m := wireHeldClassifierChatMethods(t, router, teamStore, leadID, tenantID)

	classifier := &coordinatedNonExecutableProvider{}
	loop := newDispatchLifecycleLoop(false)
	loop.provider = classifier
	loop.id = leadID

	sessionKey := "session-turn-nonexec"
	c, ch := gateway.NewCapturingTestClient(permissions.RoleOperator, uuid.Nil, "u", 8)
	req := chatSendRequest{
		ctx:           store.WithTenantID(context.Background(), tenantID),
		client:        c,
		requestID:     "r1",
		loop:          loop,
		userID:        "u",
		sessionKey:    sessionKey,
		params:        chatSendParams{Message: chatIngressMsg, AgentID: "team-lead", SessionKey: sessionKey},
		respondedOnce: &sync.Once{},
		turnLifecycle: &turnLifecycle{turnID: uuid.NewString()},
	}
	turnID := req.turnLifecycle.turnID

	m.dispatchChatSends([]chatSendRequest{req})

	resps, turns := drainFrames(t, ch, 300*time.Millisecond)

	// Exactly one RPC for r1, and it is the fail-closed precondition error.
	var r1 []protocol.ResponseFrame
	for _, r := range resps {
		if r.ID == "r1" {
			r1 = append(r1, r)
		}
	}
	if len(r1) != 1 {
		t.Fatalf("expected exactly one RPC response for r1, got %d: %+v", len(r1), r1)
	}
	if r1[0].OK {
		t.Fatalf("non-executable turn must resolve r1 with an error, got OK payload: %+v", r1[0])
	}
	if r1[0].Error == nil || r1[0].Error.Code != protocol.ErrFailedPrecondition {
		t.Fatalf("r1 error = %+v, want code %q", r1[0].Error, protocol.ErrFailedPrecondition)
	}
	wantMsg := i18n.T(i18n.Normalize(""), i18n.MsgTeamNotExecutable)
	if wantMsg == "" {
		t.Fatal("i18n.MsgTeamNotExecutable rendered empty; the assertion below would be vacuous")
	}
	if r1[0].Error.Message != wantMsg {
		t.Fatalf("r1 error message = %q, want exact i18n.MsgTeamNotExecutable %q", r1[0].Error.Message, wantMsg)
	}

	// Exactly one terminal, and it is failed with the stable turnId.
	if got := countTerminals(turns); got != 1 {
		t.Fatalf("expected exactly one terminal lifecycle event, got %d: %+v", got, turns)
	}
	failed := firstOfState(turns, protocol.ChatTurnFailed)
	if failed == nil {
		t.Fatalf("no failed terminal event: %+v", turns)
	}
	if failed.turnID != turnID {
		t.Fatalf("failed event turnId %q != request turnId %q", failed.turnID, turnID)
	}

	// The classifier ran exactly once; the runner never ran; no run registered.
	if classifier.calls != 1 {
		t.Fatalf("classifier provider calls = %d, want exactly 1", classifier.calls)
	}
	if loop.ranTimes() != 0 {
		t.Fatalf("non-executable turn ran the loop %d times; must be 0", loop.ranTimes())
	}
	if _, ok := router.SessionRunID(sessionKey); ok {
		t.Fatal("a run registered despite the non-executable fail-closed path")
	}
	waitChatReservationCleared(t, m.runQueue, sessionKey)
}

// TestTurnLifecycle_RunErrorEmitsSingleFailed proves a run that returns a
// non-context error (not a cancellation) resolves with exactly one RPC error and
// exactly one terminal failed chat.turn event from the run goroutine's error
// branch — distinct from the cancelled branch, which requires runCtx.Err().
func TestTurnLifecycle_RunErrorEmitsSingleFailed(t *testing.T) {
	m, _ := newLifecycleChatMethods()

	loop := newErrorLoop()
	c, ch := gateway.NewCapturingTestClient(permissions.RoleOperator, uuid.Nil, "u", 8)
	sessionKey := "session-turn-runerr"

	req := lifecycleRequest(c, "r1", loop, sessionKey, "will-error")
	turnID := req.turnLifecycle.turnID
	m.dispatchChatSends([]chatSendRequest{req})

	select {
	case <-loop.entered:
	case <-time.After(2 * time.Second):
		t.Fatal("error loop never entered")
	}

	resps, turns := drainFrames(t, ch, 300*time.Millisecond)

	var r1 []protocol.ResponseFrame
	for _, r := range resps {
		if r.ID == "r1" {
			r1 = append(r1, r)
		}
	}
	if len(r1) != 1 {
		t.Fatalf("expected exactly one RPC response for r1, got %d: %+v", len(r1), r1)
	}
	if r1[0].OK {
		t.Fatalf("run error must resolve r1 with an error, got OK payload: %+v", r1[0])
	}
	if got := countTerminals(turns); got != 1 {
		t.Fatalf("expected exactly one terminal lifecycle event, got %d: %+v", got, turns)
	}
	failed := firstOfState(turns, protocol.ChatTurnFailed)
	if failed == nil || failed.turnID != turnID {
		t.Fatalf("expected one failed event with turnId %q, got %+v", turnID, turns)
	}

	waitChatReservationCleared(t, m.runQueue, sessionKey)
}
