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
	"github.com/nextlevelbuilder/goclaw/internal/providers"
	"github.com/nextlevelbuilder/goclaw/internal/store"
	"github.com/nextlevelbuilder/goclaw/internal/tools"
	"github.com/nextlevelbuilder/goclaw/pkg/protocol"
)

// dispatchLifecycleLoop is a controllable agent.Agent for exercising the WS
// dispatch lifecycle (Phase 7 mandatory fixes #3 and #5). Its Run blocks until
// the test releases it or the run context is cancelled, and it records whether
// Run was ever entered so a test can prove a cancelled batch never ran.
type dispatchLifecycleLoop struct {
	id       uuid.UUID
	provider providers.Provider

	mu       sync.Mutex
	ranCount int

	// entered is closed the first time Run is entered.
	entered chan struct{}
	// release, when closed, lets a blocked Run return successfully.
	release chan struct{}
	// blockForever, when true, makes Run ignore ctx cancellation and only
	// return on release — modelling a genuinely stuck run whose own defers
	// (UnregisterRun) never fire until forced abort releases the router Done.
	blockForever bool
}

func newDispatchLifecycleLoop(blockForever bool) *dispatchLifecycleLoop {
	return &dispatchLifecycleLoop{
		id:           uuid.New(),
		provider:     &chatTeamWorkTestProvider{},
		entered:      make(chan struct{}),
		release:      make(chan struct{}),
		blockForever: blockForever,
	}
}

func (l *dispatchLifecycleLoop) ID() string                   { return "dispatch-lifecycle" }
func (l *dispatchLifecycleLoop) UUID() uuid.UUID              { return l.id }
func (l *dispatchLifecycleLoop) OtherConfig() json.RawMessage { return nil }
func (l *dispatchLifecycleLoop) IsRunning() bool              { return false }
func (l *dispatchLifecycleLoop) Model() string                { return "test-model" }
func (l *dispatchLifecycleLoop) ProviderName() string         { return l.provider.Name() }
func (l *dispatchLifecycleLoop) Provider() providers.Provider { return l.provider }

func (l *dispatchLifecycleLoop) ranTimes() int {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.ranCount
}

func (l *dispatchLifecycleLoop) Run(ctx context.Context, _ agent.RunRequest) (*agent.RunResult, error) {
	l.mu.Lock()
	l.ranCount++
	first := l.ranCount == 1
	l.mu.Unlock()
	if first {
		close(l.entered)
	}
	if l.blockForever {
		// Genuinely stuck: ignore ctx cancellation, return only on explicit release.
		<-l.release
		return &agent.RunResult{RunID: "stuck"}, nil
	}
	select {
	case <-l.release:
		return &agent.RunResult{RunID: "released"}, nil
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

// newLifecycleChatMethods builds a ChatMethods with Team Work classification
// left DISABLED (cfg.Gateway.TeamWorkClassify nil), so applyTeamWorkGate returns
// immediately and dispatch exercises the pure queue/register/run lifecycle. The
// router is real so RegisterRun/RunDone/AbortRun behave as in production.
func newLifecycleChatMethods() (*ChatMethods, *agent.Router) {
	router := agent.NewRouter()
	m := NewChatMethods(router, nil, &config.Config{}, nil, nil)
	return m, router
}

// TestDispatchForcedAbortReleasesFIFOWorker proves Phase 7 mandatory fix #3 at
// the real WS dispatch seam: a genuinely stuck run (whose goroutine never reaches
// its UnregisterRun defer) must not wedge the per-session FIFO reservation. A
// forced abort (AbortRun's 3s grace timeout → UnregisterRun → router Done close)
// must release the worker so a queued follow-up proceeds. The worker waits on the
// ROUTER Done (fix #3), not a local channel the stuck goroutine would never close.
func TestDispatchForcedAbortReleasesFIFOWorker(t *testing.T) {
	m, router := newLifecycleChatMethods()

	stuck := newDispatchLifecycleLoop(true) // never returns until released
	followup := newDispatchLifecycleLoop(false)

	c1, _ := gateway.NewCapturingTestClient(permissions.RoleOperator, uuid.Nil, "u", 4)
	c2, ch2 := gateway.NewCapturingTestClient(permissions.RoleOperator, uuid.Nil, "u", 4)
	sessionKey := "session-stuck-fifo"

	// First send starts the (stuck) run + arms the reservation.
	m.dispatchChatSends([]chatSendRequest{{
		ctx: context.Background(), client: c1, requestID: "r1",
		loop: stuck, sessionKey: sessionKey,
		params: chatSendParams{Message: "first", SessionKey: sessionKey},
	}})

	select {
	case <-stuck.entered:
	case <-time.After(2 * time.Second):
		t.Fatal("first (stuck) run never started")
	}

	// Grab the stuck run's ID from the router so we can force-abort it.
	runID, ok := router.SessionRunID(sessionKey)
	if !ok {
		t.Fatal("stuck run not registered with router")
	}

	// Second send joins the reservation as a queued batch; must not run yet.
	m.dispatchChatSends([]chatSendRequest{{
		ctx: context.Background(), client: c2, requestID: "r2",
		loop: followup, sessionKey: sessionKey,
		params: chatSendParams{Message: "second", SessionKey: sessionKey},
	}})

	select {
	case <-followup.entered:
		t.Fatal("follow-up ran while the first run was still (stuck) in flight")
	case <-time.After(100 * time.Millisecond):
	}

	// Force-abort the stuck run. Its goroutine is blocked ignoring ctx, so only
	// the router's 3s grace-timeout path (UnregisterRun → close Done) can release
	// the FIFO worker. If the worker waited on a local channel, it would hang here.
	res := router.AbortRun(runID, sessionKey)
	if !res.Forced {
		t.Fatalf("expected Forced abort for a stuck run, got %+v", res)
	}

	// The follow-up must now proceed because the worker was released by router Done.
	select {
	case <-followup.entered:
	case <-time.After(2 * time.Second):
		t.Fatal("forced abort did not release the FIFO worker; follow-up never ran")
	}

	// Let the follow-up complete and observe its OK response so nothing leaks.
	close(followup.release)
	if resp := readResponse(t, ch2); resp.ID != "r2" {
		t.Fatalf("follow-up response ID = %q, want r2", resp.ID)
	}

	// Release the stuck run's goroutine so it exits cleanly (idempotent unregister).
	close(stuck.release)
}

// heldClassifierProvider blocks inside Chat until released, modelling a slow
// classifier. It lets a test cancel a popped/classifying batch while the gate is
// still running (Phase 7 mandatory fix #5).
type heldClassifierProvider struct {
	entered chan struct{}
	exited  chan struct{}
	release chan struct{}
	once    sync.Once
}

func (p *heldClassifierProvider) Name() string         { return "held-classifier" }
func (p *heldClassifierProvider) DefaultModel() string { return "test-model" }
func (p *heldClassifierProvider) ChatStream(context.Context, providers.ChatRequest, func(providers.StreamChunk)) (*providers.ChatResponse, error) {
	return nil, context.Canceled
}
func (p *heldClassifierProvider) Chat(ctx context.Context, _ providers.ChatRequest) (*providers.ChatResponse, error) {
	p.once.Do(func() { close(p.entered) })
	select {
	case <-p.release:
	case <-ctx.Done():
	}
	if p.exited != nil {
		close(p.exited)
	}
	// Return malformed content so, if the batch were NOT cancelled, the gate would
	// fail safe to a self decision rather than forming a spurious team directive.
	return &providers.ChatResponse{Content: "not-json"}, nil
}

func waitClosed(t *testing.T, ch <-chan struct{}, what string) {
	t.Helper()
	select {
	case <-ch:
	case <-time.After(2 * time.Second):
		t.Fatalf("timed out waiting for %s", what)
	}
}

// wireHeldClassifierChatMethods builds a ChatMethods with a REAL router and Team
// Work classification ENABLED, wired to the orchestrating stores so
// applyTeamWorkGate reaches the classifier (mode != spawn) instead of
// short-circuiting. The classifier itself is supplied by the dispatch loop's
// Provider() (ResolveTeamWorkClassifier falls back to loop.Provider() when no
// override is configured), so a held provider blocks the gate mid-classification.
func wireHeldClassifierChatMethods(t *testing.T, router *agent.Router, teamStore *chatTeamWorkTeamStore, leadID, tenantID uuid.UUID) *ChatMethods {
	t.Helper()
	enabled := true
	m := NewChatMethods(router, nil, &config.Config{Gateway: config.GatewayConfig{TeamWorkClassify: &enabled}}, nil, nil)
	m.SetTeamWorkClassification(
		&chatTeamWorkAgentStore{agent: &store.AgentData{
			BaseModel: store.BaseModel{ID: leadID}, TenantID: tenantID,
			AgentKey: "team-lead", DisplayName: "Team Lead",
		}},
		teamStore,
		nil, // linkStore
		nil, // skillsLoader
		chatIngressMCPStore{},
		chatIngressBuiltinToolStore{},
		chatIngressTenantToolStore{},
		tools.NewPolicyEngine(&config.ToolsConfig{}),
		tools.NewRegistry(),
	)
	return m
}

// TestDispatchCancelDuringClassificationPreventsRun proves Phase 7 mandatory fix
// #5 at the real WS dispatch seam: a cancel that lands WHILE the Team Work
// classifier is blocking inside applyTeamWorkGate — the window between the FIFO
// worker popping the batch and RegisterRun, which the reservation drain and the
// router abort both miss — must stop the batch before it registers or runs, and
// deliver exactly one cancelled response.
//
// The classifier is held open on its first call; the test cancels the popped
// batch through the queue's armed handle (queue.Cancel → cancelCurrent). The
// cancellation bridge must itself release the provider through ctx.Done(). When
// the gate returns, the post-classify cancellation recheck resolves the batch
// cancelled BEFORE RegisterRun and loop.Run — so no router run is created and the
// loop never runs.
func TestDispatchCancelDuringClassificationPreventsRun(t *testing.T) {
	tenantID, leadID, teamID := uuid.New(), uuid.New(), uuid.New()
	router := agent.NewRouter()
	teamStore := orchestratingChatTeamStore(nil, leadID, teamID)
	m := wireHeldClassifierChatMethods(t, router, teamStore, leadID, tenantID)

	// The dispatch loop's provider IS the held classifier, so the gate blocks on
	// its first classify call. Its UUID matches the team lead so the gate resolves
	// a non-spawn mode and actually classifies. Run must never be entered.
	held := &heldClassifierProvider{
		entered: make(chan struct{}),
		exited:  make(chan struct{}),
		release: make(chan struct{}),
	}
	loop := newDispatchLifecycleLoop(false)
	loop.provider = held
	loop.id = leadID

	sessionKey := "session-cancel-classify"
	c, ch := gateway.NewCapturingTestClient(permissions.RoleOperator, uuid.Nil, "u", 4)
	req := chatSendRequest{
		ctx:        store.WithTenantID(context.Background(), tenantID),
		client:     c,
		requestID:  "r1",
		loop:       loop,
		userID:     "u",
		sessionKey: sessionKey,
		params:     chatSendParams{Message: chatIngressMsg, AgentID: "team-lead", SessionKey: sessionKey},
	}

	// Top-level dispatch: reserves the session and starts the FIFO worker, which
	// pops the batch, arms the cancellation handle, and re-enters serialized —
	// blocking inside applyTeamWorkGate on the held classifier.
	m.dispatchChatSends([]chatSendRequest{req})

	// Classifier is now blocking inside the gate: the batch is popped (off the
	// reservation queue) but not yet registered with the router.
	waitClosed(t, held.entered, "held classifier to enter (batch classifying)")
	if _, ok := router.SessionRunID(sessionKey); ok {
		t.Fatal("a run registered with the router while the classifier was still blocking")
	}

	// Cancel the in-flight popped batch through the queue's armed handle. This
	// cancels the worker's per-batch context; the batch is invisible to both the
	// reservation drain (already popped) and the router (no run yet), so only this
	// handle can stop it.
	m.runQueue.Cancel(sessionKey)

	// Cancellation must propagate into the classifier context itself; do not close
	// held.release. If the gate still uses the detached request context, this wait
	// times out and proves Cancel cannot interrupt classification.
	waitClosed(t, held.exited, "held classifier to exit on batch cancellation")

	// Exactly one cancelled response, and no run ever registered or executed.
	assertChatCancelled(t, ch, "r1")
	if loop.ranTimes() != 0 {
		t.Fatalf("loop.Run entered %d times; a cancelled-during-classify batch must never run", loop.ranTimes())
	}
	if _, ok := router.SessionRunID(sessionKey); ok {
		t.Fatal("a run registered with the router despite cancellation during classification")
	}
	waitChatReservationCleared(t, m.runQueue, sessionKey)
}

// latchedLifecycleRequest builds a chatSendRequest wired exactly as production
// wires it at chat.go request construction: a per-request exactly-one-RPC latch
// (*sync.Once) shared by pointer across every copy the request makes through the
// FIFO queue and the serialized run. Tests that assert the queued-ack contract
// MUST build requests this way — the inline requests elsewhere in this file
// deliberately omit the latch (nil = direct send) and so do not exercise it.
func latchedLifecycleRequest(client *gateway.Client, requestID string, loop agent.Agent, sessionKey, message string) chatSendRequest {
	return chatSendRequest{
		ctx:           context.Background(),
		client:        client,
		requestID:     requestID,
		loop:          loop,
		sessionKey:    sessionKey,
		params:        chatSendParams{Message: message, SessionKey: sessionKey},
		respondedOnce: &sync.Once{},
	}
}

// TestDispatchBusyFollowupQueuedAckThenSingleFinal proves Phase 7 review
// deviation #1 at the real WS dispatch seam: an accepted busy follow-up is
// acknowledged IMMEDIATELY with a structural {queued:true} RPC response — not a
// deferred result — the moment it joins the per-session FIFO behind the active
// run. Its assistant output is then delivered later, exactly once, through the
// normal run/event/history path when the worker dequeues and runs it. The two
// are distinct lifecycle events: the queued ack must arrive BEFORE the follow-up
// run even starts, and the follow-up's serialized run must NOT emit a second RPC
// response for the same request ID (the per-request exactly-one-response latch,
// claimed by the ack, suppresses the run's terminal sendChatOK).
func TestDispatchBusyFollowupQueuedAckThenSingleFinal(t *testing.T) {
	m, _ := newLifecycleChatMethods()

	primary := newDispatchLifecycleLoop(false)
	followup := newDispatchLifecycleLoop(false)

	c1, ch1 := gateway.NewCapturingTestClient(permissions.RoleOperator, uuid.Nil, "u", 4)
	c2, ch2 := gateway.NewCapturingTestClient(permissions.RoleOperator, uuid.Nil, "u", 4)
	sessionKey := "session-queued-ack"

	// First send creates the reservation and runs immediately (primary turn). It is
	// NOT a queued turn, so it takes the pre-existing deferred-result path — its
	// chat.send resolves with the run result, not a queued ack.
	m.dispatchChatSends([]chatSendRequest{
		latchedLifecycleRequest(c1, "r1", primary, sessionKey, "first"),
	})

	select {
	case <-primary.entered:
	case <-time.After(2 * time.Second):
		t.Fatal("primary run never started")
	}

	// Second send joins the reservation as a queued follow-up. Per deviation #1 it
	// must be acknowledged with {queued:true} IMMEDIATELY — while the primary is
	// still in flight and before the follow-up run is entered.
	m.dispatchChatSends([]chatSendRequest{
		latchedLifecycleRequest(c2, "r2", followup, sessionKey, "second"),
	})

	ack := readResponse(t, ch2)
	if ack.ID != "r2" {
		t.Fatalf("queued ack ID = %q, want r2", ack.ID)
	}
	assertQueuedAck(t, ack)

	// The queued ack is a DISTINCT lifecycle event from the run: the follow-up must
	// not have started yet — it is serialized behind the still-running primary.
	select {
	case <-followup.entered:
		t.Fatal("follow-up run started before the primary completed; queued turn was not serialized")
	case <-time.After(100 * time.Millisecond):
	}

	// Complete the primary. Its own chat.send (r1) resolves via the deferred path —
	// the non-queued primary keeps its pre-deviation behavior. We assert only that
	// r1 receives exactly one response for its own ID; its payload SHAPE is a harness
	// artifact here (newLifecycleChatMethods wires sessions=nil, so the post-run
	// title-generation path nil-derefs, is recovered by the run goroutine's outer
	// recover, and resolves r1 with a terminal error). The queued-ack contract under
	// test concerns r2, not the primary's payload.
	close(primary.release)
	primaryResp := readResponse(t, ch1)
	if primaryResp.ID != "r1" {
		t.Fatalf("primary response ID = %q, want r1", primaryResp.ID)
	}

	// Now the follow-up is dequeued and run.
	select {
	case <-followup.entered:
	case <-time.After(2 * time.Second):
		t.Fatal("follow-up run never started after the primary completed")
	}

	// Let the follow-up complete. Its serialized run finishes and would normally
	// call sendChatOK for r2 — but the queued ack already claimed r2's latch, so
	// the run emits NO second RPC response. The follow-up's assistant output is
	// carried out-of-band via the run/event/history path (not this RPC channel).
	close(followup.release)

	// r2 must receive exactly ONE terminal RPC response — the queued ack already
	// read above. Any further frame on ch2 is a contract violation (duplicate
	// response for the same request ID).
	assertNoFurtherResponse(t, ch2, "r2")

	waitChatReservationCleared(t, m.runQueue, sessionKey)
}

// assertQueuedAck asserts an OK response whose payload is the structural
// {queued:true} acknowledgement (Phase 7 review deviation #1) and NOT a result
// or a cancellation.
func assertQueuedAck(t *testing.T, resp *protocol.ResponseFrame) {
	t.Helper()
	if resp.Error != nil {
		t.Fatalf("queued ack carried an error: %+v", resp.Error)
	}
	payload, ok := resp.Payload.(map[string]any)
	if !ok {
		t.Fatalf("queued ack has non-object payload: %#v", resp.Payload)
	}
	if queued, _ := payload["queued"].(bool); !queued {
		t.Fatalf("queued ack missing queued=true: %#v", payload)
	}
	// A queued ack is purely structural: it must not double as a result (no
	// content/runId) or a cancellation.
	if _, hasContent := payload["content"]; hasContent {
		t.Fatalf("queued ack must not carry a result payload: %#v", payload)
	}
	if cancelled, _ := payload["cancelled"].(bool); cancelled {
		t.Fatalf("queued ack must not be a cancellation: %#v", payload)
	}
}

// assertNoFurtherResponse fails if any additional RPC frame arrives for a request
// ID that has already received its single terminal response (Phase 7 review
// deviation #1 / trace item C: exactly-one-RPC-response per chat.send).
func assertNoFurtherResponse(t *testing.T, ch <-chan []byte, id string) {
	t.Helper()
	select {
	case raw := <-ch:
		var resp protocol.ResponseFrame
		_ = json.Unmarshal(raw, &resp)
		t.Fatalf("unexpected second RPC response for %q: %+v", id, resp)
	case <-time.After(150 * time.Millisecond):
	}
}
