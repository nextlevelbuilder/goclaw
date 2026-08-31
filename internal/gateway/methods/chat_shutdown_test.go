package methods

import (
	"context"
	"encoding/json"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/nextlevelbuilder/goclaw/internal/agent"
	"github.com/nextlevelbuilder/goclaw/internal/config"
	"github.com/nextlevelbuilder/goclaw/internal/gateway"
	"github.com/nextlevelbuilder/goclaw/internal/permissions"
	"github.com/nextlevelbuilder/goclaw/internal/store"
	"github.com/nextlevelbuilder/goclaw/pkg/protocol"
)

// Phase 7 Decision 6 — minimal graceful WS chat-dispatch shutdown.
//
// ChatMethods.Shutdown() quiesces the dispatch path at process teardown, called
// from the gateway signal handler BEFORE the scheduler/providers are torn down.
// Its contract (Decision 6 points 1-8):
//
//   - New chat.send submissions are latched closed (rejected with a terminal
//     error) so shutdown races against a bounded, non-growing set of work.
//   - Requests still inside a debounce window (buffered, never dispatched) are
//     resolved with a terminal shutdown error — their RPC ID is still unanswered.
//   - Batches sitting in the FIFO queue (already {queued:true}-acked) get a
//     terminal chat.turn lifecycle event but NO second RPC response.
//   - A batch the worker has popped and is classifying is cancelled before it
//     registers a run.
//   - Active registered runs are bounded-aborted via the router's per-run 3s
//     grace + force-release; the run goroutine resolves its own chat.send.
//
// Every path resolves each unacked RPC ID exactly once, and every acked turn
// gets exactly one terminal lifecycle — so no client hangs to its timeout.

// shutdownTestMethods builds a ChatMethods with a real router + real FIFO queue,
// Team Work classification DISABLED so dispatch exercises the pure
// reserve/classify/register/run lifecycle (the classifying-batch case wires its
// own held classifier). Returns the router so tests can observe run registration.
func shutdownTestMethods() (*ChatMethods, *agent.Router) {
	router := agent.NewRouter()
	m := NewChatMethods(router, labeledSessionStore{}, &config.Config{}, nil, nil)
	m.debouncer = newChatDebouncer(m.dispatchChatSends)
	return m, router
}

// TestShutdown_BufferedRequestResolved proves a chat.send still sitting in a
// debounce window at shutdown — never dispatched, so it holds neither a queued
// ack nor a started run — is resolved with a terminal error rather than left to
// hang. Shutdown's DrainAll takes it out of the debouncer WITHOUT flushing it
// into the already-shutting-down queue.
func TestShutdown_BufferedRequestResolved(t *testing.T) {
	m, _ := shutdownTestMethods()

	loop := newDispatchLifecycleLoop(false)
	c, ch := gateway.NewCapturingTestClient(permissions.RoleOperator, uuid.Nil, "u", 8)
	sessionKey := "session-shutdown-buffered"

	// Push into the debouncer with a long delay so it stays buffered (never flushed).
	req := lifecycleRequest(c, "r1", loop, sessionKey, "buffered message")
	m.debouncer.Push(chatDebounceKey("u", sessionKey), time.Hour, req)

	// Sanity: nothing dispatched yet — the loop never ran, no reservation exists.
	if loop.ranTimes() != 0 {
		t.Fatalf("buffered request should not have run yet; ran %d times", loop.ranTimes())
	}
	if m.runQueue.HasReservation(sessionKey) {
		t.Fatal("buffered request should not have reserved the FIFO queue")
	}

	m.Shutdown()

	resps, turns := drainFrames(t, ch, 200*time.Millisecond)

	// Exactly one RPC for r1, and it is a terminal error (not a result/ack).
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
		t.Fatalf("buffered request at shutdown must resolve with an error, got OK: %+v", r1[0])
	}

	// It holds a turnLifecycle, so a terminal (failed) lifecycle resolves any turnId
	// the client already holds. Exactly one terminal.
	if got := countTerminals(turns); got != 1 {
		t.Fatalf("expected exactly one terminal lifecycle event, got %d: %+v", got, turns)
	}
	if failed := firstOfState(turns, protocol.ChatTurnFailed); failed == nil {
		t.Fatalf("buffered request at shutdown must emit a failed terminal, got %+v", turns)
	}

	// The buffered request must never have run.
	if loop.ranTimes() != 0 {
		t.Fatalf("buffered request ran %d times at/after shutdown; must be 0", loop.ranTimes())
	}
}

// TestShutdown_QueuedAckedTurnGetsTerminalNoSecondRPC proves a busy follow-up
// that was already {queued:true}-acked and is still sitting in the FIFO queue at
// shutdown gets exactly one terminal chat.turn lifecycle event (so the client's
// turnId resolves) and NO second RPC response (its queued ack was its single
// terminal RPC). Decision 6 points 3 & 8.
func TestShutdown_QueuedAckedTurnGetsTerminalNoSecondRPC(t *testing.T) {
	m, _ := shutdownTestMethods()

	primary := newDispatchLifecycleLoop(false)
	followup := newDispatchLifecycleLoop(false)

	c1, _ := gateway.NewCapturingTestClient(permissions.RoleOperator, uuid.Nil, "u", 8)
	c2, ch2 := gateway.NewCapturingTestClient(permissions.RoleOperator, uuid.Nil, "u", 16)
	sessionKey := "session-shutdown-queued"

	// Primary occupies the session.
	m.dispatchChatSends([]chatSendRequest{
		lifecycleRequest(c1, "r1", primary, sessionKey, "first"),
	})
	select {
	case <-primary.entered:
	case <-time.After(2 * time.Second):
		t.Fatal("primary run never started")
	}

	// Follow-up joins the FIFO → queued ack + queued lifecycle. It stays queued
	// because the primary is still blocking.
	m.dispatchChatSends([]chatSendRequest{
		lifecycleRequest(c2, "r2", followup, sessionKey, "second"),
	})
	var seen []turnEvent
	ack := readNextResponse(t, ch2, &seen)
	if ack.ID != "r2" {
		t.Fatalf("queued ack ID = %q, want r2", ack.ID)
	}
	assertQueuedAck(t, ack)
	ackTurnID := ack.Payload.(map[string]any)["turnId"].(string)

	// Shutdown while the follow-up is still queued behind the blocked primary.
	// The primary's active run is force-aborted (bounded); the queued follow-up is
	// drained with a failed terminal + shutdown error. Release the primary loop so
	// its goroutine can unwind after the abort (blockForever=false honors ctx).
	go func() {
		time.Sleep(50 * time.Millisecond)
		close(primary.release)
		close(followup.release)
	}()
	m.Shutdown()

	resps, turns := drainFrames(t, ch2, 400*time.Millisecond)
	turns = append(seen, turns...)

	// The queued follow-up must NOT have run (it was drained, not dequeued).
	if followup.ranTimes() != 0 {
		t.Fatalf("queued follow-up ran %d times at shutdown; a drained queued turn must not run", followup.ranTimes())
	}

	// Exactly one terminal lifecycle for the follow-up's turnId.
	terminalCount := 0
	for _, te := range turns {
		if te.turnID != ackTurnID {
			continue
		}
		switch te.state {
		case protocol.ChatTurnCompleted, protocol.ChatTurnCancelled, protocol.ChatTurnFailed:
			terminalCount++
		}
	}
	if terminalCount != 1 {
		t.Fatalf("expected exactly one terminal lifecycle for the queued turn, got %d: %+v", terminalCount, turns)
	}

	// No SECOND RPC response for r2 — the queued ack already claimed its latch.
	for _, r := range resps {
		if r.ID == "r2" {
			t.Fatalf("unexpected second RPC response for r2 at shutdown: %+v", r)
		}
	}
}

// TestShutdown_ClassifyingBatchCancelledBeforeRegister proves a batch the FIFO
// worker has popped and is classifying (blocked inside applyTeamWorkGate, before
// RegisterRun) is cancelled by Shutdown so it never registers a run. Shutdown
// cancels each reservation's armed cancelCurrent handle; when the held classifier
// is released, the post-classify recheck resolves the batch cancelled. Decision 6
// point 5.
func TestShutdown_ClassifyingBatchCancelledBeforeRegister(t *testing.T) {
	tenantID, leadID, teamID := uuid.New(), uuid.New(), uuid.New()
	router := agent.NewRouter()
	teamStore := orchestratingChatTeamStore(nil, leadID, teamID)
	m := wireHeldClassifierChatMethods(t, router, teamStore, leadID, tenantID)

	held := &heldClassifierProvider{entered: make(chan struct{}), release: make(chan struct{})}
	loop := newDispatchLifecycleLoop(false)
	loop.provider = held
	loop.id = leadID

	sessionKey := "session-shutdown-classify"
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

	// Shutdown cancels the classifying batch's armed handle; releasing the
	// classifier lets the post-classify recheck resolve it cancelled before register.
	go func() {
		time.Sleep(50 * time.Millisecond)
		close(held.release)
	}()
	m.Shutdown()

	resps, turns := drainFrames(t, ch, 400*time.Millisecond)

	// Exactly one RPC for r1, and it is a cancellation (post-classify recheck).
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
		t.Fatalf("classifying batch at shutdown must resolve cancelled, got %+v", r1[0])
	}

	// Exactly one terminal cancelled lifecycle with the stable turnId.
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

	// The classifying batch must never have registered a run or run its loop.
	if loop.ranTimes() != 0 {
		t.Fatalf("classifying batch ran %d times at shutdown; must be 0", loop.ranTimes())
	}
	if _, ok := router.SessionRunID(sessionKey); ok {
		t.Fatal("a run registered despite shutdown during classification")
	}
}

// TestShutdown_ActiveRunBoundedAbort proves an active registered run is aborted
// within a bounded window at shutdown, and its chat.send resolves (cancelled)
// rather than hanging. The loop honors ctx cancellation, so AbortAllRuns' cancel
// unwinds it well inside AbortRun's 3s grace. Decision 6 point 6.
func TestShutdown_ActiveRunBoundedAbort(t *testing.T) {
	m, router := shutdownTestMethods()

	// blockForever=false: the run returns on ctx.Done(), so the shutdown abort's
	// cancel resolves it via the runCtx.Err() cancelled branch.
	loop := newDispatchLifecycleLoop(false)
	c, ch := gateway.NewCapturingTestClient(permissions.RoleOperator, uuid.Nil, "u", 8)
	sessionKey := "session-shutdown-active"

	m.dispatchChatSends([]chatSendRequest{
		lifecycleRequest(c, "r1", loop, sessionKey, "long-running"),
	})
	select {
	case <-loop.entered:
	case <-time.After(2 * time.Second):
		t.Fatal("active run never started")
	}
	if _, ok := router.SessionRunID(sessionKey); !ok {
		t.Fatal("active run did not register with the router")
	}

	start := time.Now()
	m.Shutdown()
	elapsed := time.Since(start)

	// Bounded: even a well-behaved run must be aborted well within the 3s grace.
	if elapsed > 2*time.Second {
		t.Fatalf("Shutdown took %v; active-run abort must be bounded (< grace)", elapsed)
	}

	resps, turns := drainFrames(t, ch, 400*time.Millisecond)

	// r1 resolves exactly once, as a cancellation (runCtx.Err() branch).
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
		t.Fatalf("active run aborted at shutdown must resolve cancelled, got %+v", r1[0])
	}

	// Exactly one terminal (cancelled) lifecycle for the run's turn.
	if got := countTerminals(turns); got != 1 {
		t.Fatalf("expected exactly one terminal lifecycle event, got %d: %+v", got, turns)
	}
	if cancelled := firstOfState(turns, protocol.ChatTurnCancelled); cancelled == nil {
		t.Fatalf("active run aborted at shutdown must emit a cancelled terminal, got %+v", turns)
	}

	// The router must no longer hold the run.
	if _, ok := router.SessionRunID(sessionKey); ok {
		t.Fatal("router still holds the run after shutdown abort")
	}
}

// TestShutdown_NewSubmissionRejected proves that once Shutdown has latched, a new
// chat.send is rejected at the single admission point (handleSend) with a terminal
// error before it can buffer in the debouncer or reserve in the FIFO queue.
// Decision 6 point 1. Uses handleSend directly so the gate at the top of the real
// admission path is exercised.
func TestShutdown_NewSubmissionRejected(t *testing.T) {
	m, _ := shutdownTestMethods()

	// Register a loop so handleSend's m.agents.Get would otherwise succeed — proving
	// the rejection comes from the shutdown gate, not a missing agent. Get resolves
	// by ID(), so the request's AgentID must be loop.ID().
	loop := newDispatchLifecycleLoop(false)
	m.agents.Register(loop)

	m.Shutdown()

	c, ch := gateway.NewCapturingTestClient(permissions.RoleOperator, uuid.Nil, "u", 8)
	params, err := json.Marshal(chatSendParams{Message: "after shutdown", AgentID: loop.ID(), SessionKey: "session-shutdown-new"})
	if err != nil {
		t.Fatalf("marshal params: %v", err)
	}
	req := &protocol.RequestFrame{
		Type:   protocol.FrameTypeRequest,
		ID:     "r-new",
		Method: protocol.MethodChatSend,
		Params: params,
	}
	m.handleSend(store.WithUserID(context.Background(), "u"), c, req)

	resps, _ := drainFrames(t, ch, 200*time.Millisecond)
	var r []protocol.ResponseFrame
	for _, resp := range resps {
		if resp.ID == "r-new" {
			r = append(r, resp)
		}
	}
	if len(r) != 1 {
		t.Fatalf("expected exactly one RPC response for r-new, got %d: %+v", len(r), r)
	}
	if r[0].OK {
		t.Fatalf("new submission after shutdown must be rejected with an error, got OK: %+v", r[0])
	}

	// It must never have buffered or reserved.
	if m.runQueue.HasReservation("session-shutdown-new") {
		t.Fatal("new submission after shutdown reserved the FIFO queue; must be rejected before reserve")
	}
	if loop.ranTimes() != 0 {
		t.Fatalf("new submission after shutdown ran %d times; must be 0", loop.ranTimes())
	}
}
