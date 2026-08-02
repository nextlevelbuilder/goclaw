package methods

import (
	"context"
	"strings"
	"sync"
	"sync/atomic"
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

// Phase 7 closure item 5 — production-facing integration proof.
//
// The Decision 4 lifecycle tests use synthetic loops and a label-only store; they
// prove the queue/lifecycle MECHANICS but not that a queued follow-up actually
// produces a durable assistant history entry through the REAL agent pipeline. This
// test closes that gap: it wires two real *agent.Loop instances (via NewLoop) onto
// a shared in-memory session store and lets the production pipeline
// (Loop.Run → runViaPipeline → FinalizeStage → makeFlushMessages/makeUpdateMetadata)
// be the SOLE writer of history. The test never calls AddMessage/Save directly.
// Team Work classification is enabled and driven by a deterministic provider so the
// gate classifies each dequeued turn exactly once. The eight-point contract for a
// busy follow-up:
//
//  1. an active run holds the session,
//  2. the busy follow-up receives {queued:true, turnId},
//  3. the follow-up classifier count is 0 while queued and exactly 1 after the
//     active run completes,
//  4. the follow-up runs, and a reload sees exactly one user + one assistant entry
//     it contributed,
//  5. exactly ONE completed chat.turn lifecycle event, linked turnId→runId,
//  6. exactly ONE production run.completed event for the follow-up,
//  7. a reload (GetHistory) sees the follow-up's assistant output,
//  8. NO second RPC response for the follow-up's original chat.send request ID.

// memSessionStore is a minimal thread-safe in-memory store.SessionStore that
// actually records message history per key, so a reload path (GetHistory) can be
// asserted. Only the methods the production run/success path and this test
// exercise carry behavior; the rest are inert. GetLabel returns a non-empty label
// so the async title goroutine is skipped.
type memSessionStore struct {
	mu       sync.Mutex
	messages map[string][]providers.Message
	saves    map[string]int
}

func newMemSessionStore() *memSessionStore {
	return &memSessionStore{
		messages: make(map[string][]providers.Message),
		saves:    make(map[string]int),
	}
}

func (s *memSessionStore) AddMessage(_ context.Context, key string, msg providers.Message) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.messages[key] = append(s.messages[key], msg)
}

func (s *memSessionStore) GetHistory(_ context.Context, key string) []providers.Message {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]providers.Message, len(s.messages[key]))
	copy(out, s.messages[key])
	return out
}

// countRole returns how many stored messages for key have the given role and,
// when content != "", exactly that content. Used to prove the follow-up
// contributed exactly one user + one assistant entry via the production pipeline.
func (s *memSessionStore) countRole(key, role, content string) int {
	s.mu.Lock()
	defer s.mu.Unlock()
	n := 0
	for _, m := range s.messages[key] {
		if m.Role != role {
			continue
		}
		if content != "" && m.Content != content {
			continue
		}
		n++
	}
	return n
}

func (s *memSessionStore) assistantCount(key string) int {
	return s.countRole(key, "assistant", "")
}

func (s *memSessionStore) GetOrCreate(_ context.Context, key string) *store.SessionData {
	return &store.SessionData{Key: key}
}
func (s *memSessionStore) Get(context.Context, string) *store.SessionData          { return nil }
func (s *memSessionStore) GetSummary(context.Context, string) string               { return "" }
func (s *memSessionStore) SetSummary(context.Context, string, string)              {}
func (s *memSessionStore) GetLabel(context.Context, string) string                 { return "titled" }
func (s *memSessionStore) SetLabel(context.Context, string, string)                {}
func (s *memSessionStore) SetAgentInfo(context.Context, string, uuid.UUID, string) {}
func (s *memSessionStore) TruncateHistory(context.Context, string, int)            {}
func (s *memSessionStore) SetHistory(_ context.Context, key string, msgs []providers.Message) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.messages[key] = append([]providers.Message(nil), msgs...)
}
func (s *memSessionStore) Reset(context.Context, string)        {}
func (s *memSessionStore) Delete(context.Context, string) error { return nil }
func (s *memSessionStore) Save(_ context.Context, key string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.saves[key]++
	return nil
}

func (s *memSessionStore) UpdateMetadata(context.Context, string, string, string, string) {}
func (s *memSessionStore) AccumulateTokens(context.Context, string, int64, int64)         {}
func (s *memSessionStore) IncrementCompaction(context.Context, string)                    {}
func (s *memSessionStore) GetCompactionCount(context.Context, string) int                 { return 0 }
func (s *memSessionStore) GetMemoryFlushCompactionCount(context.Context, string) int      { return 0 }
func (s *memSessionStore) SetMemoryFlushDone(context.Context, string)                     {}
func (s *memSessionStore) GetSessionMetadata(context.Context, string) map[string]string   { return nil }
func (s *memSessionStore) SetSessionMetadata(context.Context, string, map[string]string)  {}
func (s *memSessionStore) SetSpawnInfo(context.Context, string, string, int)              {}
func (s *memSessionStore) SetContextWindow(context.Context, string, int)                  {}
func (s *memSessionStore) GetContextWindow(context.Context, string) int                   { return 0 }
func (s *memSessionStore) SetLastPromptTokens(context.Context, string, int, int)          {}
func (s *memSessionStore) GetLastPromptTokens(context.Context, string) (int, int)         { return 0, 0 }

func (s *memSessionStore) List(context.Context, string) []store.SessionInfo { return nil }
func (s *memSessionStore) ListPaged(context.Context, store.SessionListOpts) store.SessionListResult {
	return store.SessionListResult{}
}
func (s *memSessionStore) ListPagedRich(context.Context, store.SessionListOpts) store.SessionListRichResult {
	return store.SessionListRichResult{}
}
func (s *memSessionStore) LastUsedChannel(context.Context, string) (string, string) {
	return "", ""
}

// integrationClassifyProvider serves both the one-call Team Work router and the
// real agent turn. A SELF route lets the request continue as a plain agent turn.
type integrationClassifyProvider struct {
	answer string

	classifyCount atomic.Int32

	enteredOnce sync.Once
	entered     chan struct{} // closed when the agent turn is first entered

	release    chan struct{} // if non-nil, agent turn blocks until closed
	blockOnCtx bool          // if true, agent turn blocks until ctx is cancelled
}

func newIntegrationClassifyProvider(answer string) *integrationClassifyProvider {
	return &integrationClassifyProvider{answer: answer, entered: make(chan struct{})}
}

func (p *integrationClassifyProvider) Name() string         { return "integration-provider" }
func (p *integrationClassifyProvider) DefaultModel() string { return "test-model" }

func (p *integrationClassifyProvider) ChatStream(ctx context.Context, req providers.ChatRequest, _ func(providers.StreamChunk)) (*providers.ChatResponse, error) {
	return p.Chat(ctx, req)
}

func (p *integrationClassifyProvider) Chat(ctx context.Context, req providers.ChatRequest) (*providers.ChatResponse, error) {
	system := ""
	if len(req.Messages) > 0 {
		system = req.Messages[0].Content
	}
	switch {
	case strings.Contains(system, "GoClaw's Team Work routing classifier"):
		p.classifyCount.Add(1)
		return &providers.ChatResponse{Content: `{"decision":"self","reason":"current agent owns the task","preferred_owner":"","task_type":"dev"}`}, nil
	default:
		// Real agent turn.
		p.enteredOnce.Do(func() { close(p.entered) })
		if p.blockOnCtx {
			<-ctx.Done()
			return nil, ctx.Err()
		}
		if p.release != nil {
			select {
			case <-p.release:
			case <-ctx.Done():
				return nil, ctx.Err()
			}
		}
		return &providers.ChatResponse{Content: p.answer, FinishReason: "stop"}, nil
	}
}

// runEventCollector captures agent events emitted by a real Loop so the test can
// count production run.completed events for one loop.
type runEventCollector struct {
	mu     sync.Mutex
	events []agent.AgentEvent
}

func (c *runEventCollector) onEvent(e agent.AgentEvent) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.events = append(c.events, e)
}

func (c *runEventCollector) count(typ string) int {
	c.mu.Lock()
	defer c.mu.Unlock()
	n := 0
	for _, e := range c.events {
		if e.Type == typ {
			n++
		}
	}
	return n
}

// newIntegrationLoop builds a real production *agent.Loop wired to the shared
// session store, the given provider, and an event collector. AgentUUID is set to
// the team lead so applyTeamWorkGate classifies (loop.UUID() != Nil and the team
// store returns a team → ModeTeam).
func newIntegrationLoop(t *testing.T, leadID uuid.UUID, st *memSessionStore, provider providers.Provider, col *runEventCollector) *agent.Loop {
	t.Helper()
	return agent.NewLoop(agent.LoopConfig{
		ID:         "integration-agent",
		AgentUUID:  leadID,
		Provider:   provider,
		Model:      "test-model",
		Sessions:   st,
		Workspace:  t.TempDir(),
		Tools:      tools.NewRegistry(),
		ToolPolicy: tools.NewPolicyEngine(&config.ToolsConfig{}),
		OnEvent:    col.onEvent,
	})
}

// wireIntegrationChatMethods builds a ChatMethods with Team Work classification
// enabled and wired so applyTeamWorkGate classifies (the team store returns a team
// whose lead is leadID). The classifier provider falls back to each loop's own
// provider, so each loop's classification cycles are counted by that provider.
func wireIntegrationChatMethods(st *memSessionStore, leadID, teamID, tenantID uuid.UUID) *ChatMethods {
	enabled := true
	m := NewChatMethods(agent.NewRouter(), st, &config.Config{Gateway: config.GatewayConfig{TeamWorkClassify: &enabled}}, nil, nil)
	m.debouncer = newChatDebouncer(m.dispatchChatSends)
	m.SetTeamWorkClassification(
		&chatTeamWorkAgentStore{agent: &store.AgentData{
			BaseModel: store.BaseModel{ID: leadID}, TenantID: tenantID,
			AgentKey: "team-lead", DisplayName: "Team Lead",
		}},
		&chatTeamWorkTeamStore{
			team:    &store.TeamData{BaseModel: store.BaseModel{ID: teamID}, Name: "growth-team", LeadAgentID: leadID, LeadAgentKey: "team-lead"},
			members: []store.TeamMemberData{{TeamID: teamID, AgentID: leadID, AgentKey: "team-lead", Role: "lead"}},
		},
		nil, // linkStore
		nil, nil, nil, nil, nil, nil,
	)
	return m
}

// TestTurnIntegration_QueuedFollowupProducesDurableResult is the closure item 5
// production-facing integration proof. See the file header for the eight-point
// contract it verifies. Both runs execute through the REAL agent pipeline; the
// production FinalizeStage is the only writer of session history.
func TestTurnIntegration_QueuedFollowupProducesDurableResult(t *testing.T) {
	st := newMemSessionStore()
	leadID, teamID, tenantID := uuid.New(), uuid.New(), uuid.New()
	m := wireIntegrationChatMethods(st, leadID, teamID, tenantID)

	primaryProv := newIntegrationClassifyProvider("primary-answer")
	primaryProv.release = make(chan struct{}) // primary holds the session open until released
	primaryCol := &runEventCollector{}
	primary := newIntegrationLoop(t, leadID, st, primaryProv, primaryCol)

	followProv := newIntegrationClassifyProvider("followup-answer") // release nil → agent turn returns immediately
	followCol := &runEventCollector{}
	followup := newIntegrationLoop(t, leadID, st, followProv, followCol)

	c1, _ := gateway.NewCapturingTestClient(permissions.RoleOperator, uuid.Nil, "u", 16)
	c2, ch2 := gateway.NewCapturingTestClient(permissions.RoleOperator, uuid.Nil, "u", 16)
	sessionKey := "session-integration-primary"

	ctx := store.WithTenantID(context.Background(), tenantID)

	// (1) Active run holds the session. Dispatch the primary and wait until its real
	// agent turn is executing (provider blocked on release), which guarantees the run
	// has registered and owns the session.
	primaryReq := lifecycleRequest(c1, "r1", primary, sessionKey, "first question")
	primaryReq.ctx = ctx
	m.dispatchChatSends([]chatSendRequest{primaryReq})
	select {
	case <-primaryProv.entered:
	case <-time.After(2 * time.Second):
		t.Fatal("primary run never reached its agent turn")
	}

	// (2) Busy follow-up receives {queued:true, turnId}.
	followReq := lifecycleRequest(c2, "r2", followup, sessionKey, "second question")
	followReq.ctx = ctx
	m.dispatchChatSends([]chatSendRequest{followReq})
	var seen []turnEvent
	ack := readNextResponse(t, ch2, &seen)
	if ack.ID != "r2" {
		t.Fatalf("queued ack ID = %q, want r2", ack.ID)
	}
	assertQueuedAck(t, ack)
	ackTurnID, _ := ack.Payload.(map[string]any)["turnId"].(string)
	if ackTurnID == "" {
		t.Fatalf("queued ack missing stable turnId: %#v", ack.Payload)
	}

	// (3a) While the primary is still in flight the queued follow-up must NOT run or
	// classify: its classifier count stays 0.
	select {
	case <-followProv.entered:
		t.Fatal("follow-up ran before the primary completed")
	case <-time.After(100 * time.Millisecond):
	}
	if got := followProv.classifyCount.Load(); got != 0 {
		t.Fatalf("follow-up classifier count = %d while queued, want 0", got)
	}

	// (3b) Active run completes: release the primary's agent turn.
	close(primaryProv.release)

	// (4) The follow-up now dequeues, classifies once, and runs its real agent turn.
	select {
	case <-followProv.entered:
	case <-time.After(2 * time.Second):
		t.Fatal("follow-up run never reached its agent turn after primary completed")
	}

	resps, turns := drainFrames(t, ch2, 500*time.Millisecond)
	turns = append(seen, turns...)

	// (3c) Exactly one classification cycle for the follow-up, after the primary
	// completed.
	if got := followProv.classifyCount.Load(); got != 1 {
		t.Fatalf("follow-up classifier count = %d after completion, want exactly 1", got)
	}

	// (4/7) The production pipeline persisted exactly one user + one assistant entry
	// for the follow-up. A reload (GetHistory) sees the follow-up's assistant output.
	if got := st.countRole(sessionKey, "user", "second question"); got != 1 {
		t.Fatalf("follow-up user history entries = %d, want 1 (production pipeline persists the user turn)", got)
	}
	if got := st.countRole(sessionKey, "assistant", "followup-answer"); got != 1 {
		t.Fatalf("follow-up assistant history entries = %d, want 1 (production pipeline persists the answer)", got)
	}
	if got := st.assistantCount(sessionKey); got != 2 {
		t.Fatalf("total assistant history entries = %d, want 2 (primary + follow-up)", got)
	}

	// (5) Exactly one completed terminal lifecycle event for the follow-up turn,
	// linked turnId→runId.
	var running, completed *turnEvent
	completedCount := 0
	for i := range turns {
		if turns[i].turnID != ackTurnID {
			continue
		}
		switch turns[i].state {
		case protocol.ChatTurnRunning:
			running = &turns[i]
		case protocol.ChatTurnCompleted:
			completed = &turns[i]
			completedCount++
		}
	}
	if completedCount != 1 {
		t.Fatalf("expected exactly one completed lifecycle event for the follow-up turn, got %d: %+v", completedCount, turns)
	}
	if running == nil {
		t.Fatalf("no running lifecycle event for the follow-up turn: %+v", turns)
	}
	if running.runID == "" || running.runID != completed.runID {
		t.Fatalf("running/completed runId mismatch: running=%q completed=%q", running.runID, completed.runID)
	}

	// (6) Exactly one production run.completed for the follow-up loop.
	if got := followCol.count(protocol.AgentEventRunCompleted); got != 1 {
		t.Fatalf("production run.completed events for the follow-up loop = %d, want exactly 1", got)
	}

	// (7) Reload path sees the final assistant output.
	sawFollowupAnswer := false
	for _, msg := range st.GetHistory(context.Background(), sessionKey) {
		if msg.Role == "assistant" && msg.Content == "followup-answer" {
			sawFollowupAnswer = true
		}
	}
	if !sawFollowupAnswer {
		t.Fatalf("reload (GetHistory) did not see the follow-up's assistant output")
	}

	// (8) No second RPC response for r2 (the queued ack was its single terminal RPC).
	for _, r := range resps {
		if r.ID == "r2" {
			t.Fatalf("unexpected second RPC response for r2: %+v", r)
		}
	}

	waitChatReservationCleared(t, m.runQueue, sessionKey)
}

// TestTurnIntegration_CancelledRunProducesNoResult is the closure item 5
// cancellation variant: a real production run whose agent turn is blocked is
// cancelled through the router abort path. It must produce NO assistant history
// (the production FinalizeStage never runs), NO run.completed event, and exactly
// one cancelled chat.turn lifecycle event.
func TestTurnIntegration_CancelledRunProducesNoResult(t *testing.T) {
	st := newMemSessionStore()
	leadID, teamID, tenantID := uuid.New(), uuid.New(), uuid.New()
	m := wireIntegrationChatMethods(st, leadID, teamID, tenantID)

	prov := newIntegrationClassifyProvider("never-delivered")
	prov.blockOnCtx = true // real agent turn blocks until the run's context is cancelled
	col := &runEventCollector{}
	loop := newIntegrationLoop(t, leadID, st, prov, col)

	c, ch := gateway.NewCapturingTestClient(permissions.RoleOperator, uuid.Nil, "u", 16)
	sessionKey := "session-integration-cancel"
	ctx := store.WithTenantID(context.Background(), tenantID)

	req := lifecycleRequest(c, "r1", loop, sessionKey, "cancel me")
	req.ctx = ctx
	m.dispatchChatSends([]chatSendRequest{req})

	// Wait until the real agent turn is executing (blocked on ctx).
	select {
	case <-prov.entered:
	case <-time.After(2 * time.Second):
		t.Fatal("run never reached its agent turn")
	}

	// Cancel through the production router abort path (as chat.abort does).
	m.agents.AbortRunsForSession(sessionKey)

	_, turns := drainFrames(t, ch, 500*time.Millisecond)

	// No assistant result: the run errored before FinalizeStage, so the production
	// pipeline never persisted an assistant (or user) message.
	if got := st.assistantCount(sessionKey); got != 0 {
		t.Fatalf("assistant history entries = %d after cancellation, want 0", got)
	}

	// No production run.completed for a cancelled run.
	if got := col.count(protocol.AgentEventRunCompleted); got != 0 {
		t.Fatalf("production run.completed events = %d for a cancelled run, want 0", got)
	}

	// Exactly one terminal cancelled chat.turn lifecycle event.
	cancelledCount := 0
	for i := range turns {
		if turns[i].state == protocol.ChatTurnCancelled {
			cancelledCount++
		}
	}
	if cancelledCount != 1 {
		t.Fatalf("expected exactly one cancelled lifecycle event, got %d: %+v", cancelledCount, turns)
	}

	waitChatReservationCleared(t, m.runQueue, sessionKey)
}
