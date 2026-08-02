package cmd

import (
	"context"
	"errors"
	"slices"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/nextlevelbuilder/goclaw/internal/agent"
	"github.com/nextlevelbuilder/goclaw/internal/bus"
	"github.com/nextlevelbuilder/goclaw/internal/channels"
	"github.com/nextlevelbuilder/goclaw/internal/i18n"
	"github.com/nextlevelbuilder/goclaw/internal/providers"
	"github.com/nextlevelbuilder/goclaw/internal/scheduler"
	"github.com/nextlevelbuilder/goclaw/internal/store"
	"github.com/nextlevelbuilder/goclaw/internal/tools"
)

// buildInboundPreRun is the seam that replaced the enqueue-time gate wiring with
// a dequeue-time hook (Phase 7 review 7A-H1). These tests exercise the returned
// closure directly — the same closure production hands the scheduler — so the
// gate → *agent.RunRequest + context wiring is verified in code, not asserted by
// a static-comment reference to gateway_consumer_normal.go line numbers.

// A fully validated orchestrating decision with a SUCCESSFUL audit must, at
// dequeue: copy the directive onto the request, leave team work enabled and no
// tools blocked, preserve the message, and thread BOTH the shared
// pending-dispatch tracker and the audit ID onto the run context.
func TestBuildInboundPreRun_OrchestratingDecisionWiresRequestAndContext(t *testing.T) {
	tenantID, leadID, teamID := uuid.New(), uuid.New(), uuid.New()
	teamStore := orchestratingInboundTeamStore(nil, leadID, teamID)
	deps := orchestratingInboundDeps(teamStore, leadID, tenantID)
	provider := &inboundOrchestratingProvider{}
	agentLoop := deliveryRuntimeTestAgent{uuid: leadID, provider: provider, model: "test-model"}

	ptd := tools.NewPendingTeamDispatch()
	msg := bus.InboundMessage{Content: inboundIngressMsg, Metadata: map[string]string{}}
	preRun := buildInboundPreRun(deps, msg, "session:test", "team-lead", "direct", agentLoop, nil, "run:test", ptd, map[string]string{})

	req := &agent.RunRequest{Message: inboundIngressMsg}
	runCtx, err := preRun(store.WithTenantID(context.Background(), tenantID), req)
	if err != nil {
		t.Fatalf("inbound hook must not abort the run, got err %v", err)
	}

	if !provider.called {
		t.Fatal("PreRun did not run the classifier")
	}
	if req.TeamWorkDirective == nil {
		t.Fatal("orchestrating decision must set the directive on the request at dequeue")
	}
	if req.DisableTeamWork {
		t.Fatal("orchestrating decision must not disable team work")
	}
	if len(req.BlockedTools) != 0 {
		t.Fatalf("orchestrating decision must not block tools, got %v", req.BlockedTools)
	}
	if req.Message != inboundIngressMsg {
		t.Fatalf("PreRun must preserve the run message, got %q", req.Message)
	}
	if got := tools.PendingTeamDispatchFromCtx(runCtx); got != ptd {
		t.Fatal("PreRun must thread the shared pending-dispatch tracker onto the run context")
	}
	if tools.TeamWorkClassificationAuditIDFromCtx(runCtx) == uuid.Nil {
		t.Fatal("successful audit must thread a non-nil audit ID onto the run context")
	}
	if teamStore.lastAudit == nil || teamStore.lastAudit.ID != tools.TeamWorkClassificationAuditIDFromCtx(runCtx) {
		t.Fatalf("context audit ID must match the persisted row: ctx=%s row=%+v",
			tools.TeamWorkClassificationAuditIDFromCtx(runCtx), teamStore.lastAudit)
	}
}

// A durable audit WRITE FAILURE on an orchestrating decision must fail closed at
// dequeue: no directive on the request, team work disabled, the three canonical
// orchestration tools blocked, and NO audit ID on the context (so a workflow
// cannot link back to a decision that was never durably recorded). The
// pending-dispatch tracker is still threaded — it is unconditional.
func TestBuildInboundPreRun_AuditFailureFailsClosedAtDequeue(t *testing.T) {
	tenantID, leadID, teamID := uuid.New(), uuid.New(), uuid.New()
	teamStore := orchestratingInboundTeamStore(errors.New("audit db down"), leadID, teamID)
	deps := orchestratingInboundDeps(teamStore, leadID, tenantID)
	provider := &inboundOrchestratingProvider{}
	agentLoop := deliveryRuntimeTestAgent{uuid: leadID, provider: provider, model: "test-model"}

	ptd := tools.NewPendingTeamDispatch()
	msg := bus.InboundMessage{Content: inboundIngressMsg, Metadata: map[string]string{}}
	preRun := buildInboundPreRun(deps, msg, "session:test", "team-lead", "direct", agentLoop, nil, "run:test", ptd, map[string]string{})

	req := &agent.RunRequest{Message: inboundIngressMsg}
	runCtx, err := preRun(store.WithTenantID(context.Background(), tenantID), req)
	if err != nil {
		t.Fatalf("inbound hook fails safe, it must not abort the run, got err %v", err)
	}

	if req.TeamWorkDirective != nil {
		t.Fatalf("audit failure must drop the directive, got %+v", req.TeamWorkDirective)
	}
	if !req.DisableTeamWork {
		t.Fatal("audit failure must disable team work on the request")
	}
	for _, tool := range []string{"team_tasks", "delegate", "spawn"} {
		if !slices.Contains(req.BlockedTools, tool) {
			t.Fatalf("blocked tools = %v, missing %s", req.BlockedTools, tool)
		}
	}
	if tools.TeamWorkClassificationAuditIDFromCtx(runCtx) != uuid.Nil {
		t.Fatal("failed audit must not thread an audit ID onto the run context")
	}
	if got := tools.PendingTeamDispatchFromCtx(runCtx); got != ptd {
		t.Fatal("PreRun must thread the pending-dispatch tracker even on a fail-closed decision")
	}
}

// inboundCountingAgent wraps the shared deliveryRuntimeTestAgent fixture so the
// end-to-end test can assert the agent runner is NEVER invoked on the
// fail-closed path. deliveryRuntimeTestAgent.Run returns a silent nil result
// without counting, so a bare fixture could not prove zero runs.
type inboundCountingAgent struct {
	deliveryRuntimeTestAgent
	runs atomic.Int32
}

func (a *inboundCountingAgent) Run(context.Context, agent.RunRequest) (*agent.RunResult, error) {
	a.runs.Add(1)
	return &agent.RunResult{Content: "should never run on the fail-closed path"}, nil
}

// inboundCoordinatedProvider returns a coordinated-scope team route — the
// classifier verdict makeCoordinatedTeamResult checks against the durable
// roster via workflowExecutability. A single-owner route (the
// inboundOrchestratingProvider fixture) never reaches the coordinated
// fail-closed branch, so this case needs its own coordinated verdict.
type inboundCoordinatedProvider struct {
	calls int
}

func (p *inboundCoordinatedProvider) Name() string         { return "ingress-coordinated" }
func (p *inboundCoordinatedProvider) DefaultModel() string { return "test-model" }
func (p *inboundCoordinatedProvider) ChatStream(context.Context, providers.ChatRequest, func(providers.StreamChunk)) (*providers.ChatResponse, error) {
	return nil, errors.New("streaming not supported")
}
func (p *inboundCoordinatedProvider) Chat(context.Context, providers.ChatRequest) (*providers.ChatResponse, error) {
	p.calls++
	return &providers.ChatResponse{Content: `{"decision":"team","scope":"coordinated","reason":"multi-step campaign needs parallel specialists and a synthesis","preferred_owner":"","task_type":"content"}`}, nil
}

// inboundSessionStore is a no-op SessionStore so the real processNormalMessage
// path (SetSessionMetadata / SetLabel, plus the gate's GetHistory read via
// recentMessagesForTeamWorkGate) runs without a database.
type inboundSessionStore struct {
	store.SessionStore
}

func (inboundSessionStore) SetSessionMetadata(context.Context, string, map[string]string) {}
func (inboundSessionStore) SetLabel(context.Context, string, string)                      {}
func (inboundSessionStore) GetHistory(context.Context, string) []providers.Message {
	return nil
}

// inboundFailClosedTeamStore wraps the shared gate fixture so the real
// processNormalMessage path can fire its auto-clear-followup goroutine without
// hitting the nil embedded store.TeamStore. The gate methods
// (GetTeamForAgent/ListMembers/RecordTeamWorkClassificationAudit) delegate to
// the embedded fixture unchanged.
type inboundFailClosedTeamStore struct {
	*teamWorkGateTeamStore
}

func (inboundFailClosedTeamStore) ClearFollowupByScope(context.Context, string, string) (int, error) {
	return 0, nil
}

// G7 end-to-end fail-closed regression: a coordinated request against a team
// whose roster has NO canonical member (lead-only) must fail closed through the
// REAL inbound consumer path — processNormalMessage → scheduler → PreRun hook —
// not degrade to self and not run. The classifier runs exactly once; the hook
// publishes the user-facing i18n.MsgTeamNotExecutable config error as the first
// outbound and aborts with errInboundConfigDelivered, which the consumer result
// goroutine answers with exactly one empty cleanup outbound; the agent runner is
// never invoked; a bounded wait proves no third outbound; and scheduler + run
// goroutines are deterministically torn down.
func TestInboundPreRun_NonExecutableCoordinatedTeamFailsClosedEndToEnd(t *testing.T) {
	tenantID, leadID, teamID := uuid.New(), uuid.New(), uuid.New()
	// Empty roster with a canonical lead: the team still has a coordinator
	// (LeadAgentID/LeadAgentKey set), but ListMembers returns NO members, so
	// workflowExecutability counts zero canonical members and fails the
	// coordinated route with insufficient_canonical_members — BEFORE the
	// lead-role shortcut, which only applies once at least one member exists.
	// (A lead-only member list would count the lead as a canonical member and
	// stay executable; emptiness is what forces the fail-closed path.)
	teamStore := &teamWorkGateTeamStore{
		team: &store.TeamData{
			BaseModel:    store.BaseModel{ID: teamID},
			Name:         "growth-team",
			LeadAgentID:  leadID,
			LeadAgentKey: "team-lead",
		},
		members: []store.TeamMemberData{},
	}
	deps := orchestratingInboundDeps(teamStore, leadID, tenantID)
	deps.SessStore = inboundSessionStore{}
	deps.TeamStore = inboundFailClosedTeamStore{teamStore}
	provider := &inboundCoordinatedProvider{}
	// Router.Get resolves under a tenant-scoped canonical key
	// ("<tenantID>:<agentKey>") because processNormalMessage always injects the
	// tenant into ctx — mirroring the production resolver's canonicalization.
	// Register stores under ag.ID(), so ID() must be that canonical key or the
	// inbound lookup misses and aborts before the classifier runs.
	agentLoop := &inboundCountingAgent{
		deliveryRuntimeTestAgent: deliveryRuntimeTestAgent{
			id:       tenantID.String() + ":team-lead",
			uuid:     leadID,
			model:    "test-model",
			provider: provider,
		},
	}
	// Router lookup + a live channel so processNormalMessage reaches the
	// scheduling seam instead of an early agent-lookup abort.
	router := agent.NewRouter()
	router.Register(agentLoop)
	deps.Agents = router
	msgBus := bus.New()
	deps.MsgBus = msgBus
	channelMgr := channels.NewManager(msgBus)
	channelMgr.RegisterChannel("telegram", consumerTestChannel{
		name:        "telegram",
		channelType: channels.TypeTelegram,
		running:     true,
	})
	deps.ChannelMgr = channelMgr
	// The real inbound scheduler with a counting runFn: a fail-closed PreRun must
	// abort BEFORE either runFn path executes. Stop() deterministically drains the
	// scheduler lanes and run goroutines on teardown.
	sched := scheduler.NewScheduler(nil, scheduler.QueueConfig{Mode: scheduler.QueueModeQueue, Cap: 4, MaxConcurrent: 1},
		func(context.Context, agent.RunRequest) (*agent.RunResult, error) {
			agentLoop.runs.Add(1)
			return &agent.RunResult{Content: "should never run on the fail-closed path"}, nil
		})
	defer sched.Stop()
	deps.Sched = sched

	// Subscribe BEFORE driving so the FIFO outbound channel cannot race the hook.
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	processNormalMessage(context.Background(), bus.InboundMessage{
		Channel:  "telegram",
		SenderID: "user-1",
		ChatID:   "chat-1",
		Content:  inboundIngressMsg,
		PeerKind: "direct",
		AgentID:  "team-lead",
		TenantID: tenantID,
		Metadata: map[string]string{},
	}, deps)

	// First outbound: the user-facing config error the hook published at dequeue.
	// Receiving it proves the classifier ran (the hook publishes only after the
	// gate returns), so the call-count assert below is race-free.
	first, ok := msgBus.SubscribeOutbound(ctx)
	if !ok {
		t.Fatal("fail-closed hook published no outbound config error")
	}
	if provider.calls != 1 {
		t.Fatalf("classifier provider calls = %d, want exactly 1", provider.calls)
	}
	wantErr := i18n.T(i18n.Normalize(""), i18n.MsgTeamNotExecutable)
	if wantErr == "" {
		t.Fatal("i18n.MsgTeamNotExecutable rendered empty; the assertion below would be vacuous")
	}
	if first.Content != wantErr {
		t.Fatalf("first outbound content = %q, want exact i18n.MsgTeamNotExecutable %q", first.Content, wantErr)
	}

	// Second outbound: exactly one empty cleanup from the consumer's
	// errInboundConfigDelivered branch — no error text republished.
	second, ok := msgBus.SubscribeOutbound(ctx)
	if !ok {
		t.Fatal("no empty cleanup outbound followed the config error")
	}
	if second.Content != "" {
		t.Fatalf("second outbound content = %q, want exactly empty cleanup", second.Content)
	}

	// The agent runner must never have executed the aborted run.
	if agentLoop.runs.Load() != 0 {
		t.Fatalf("agent runner ran %d times; non-executable team work must never execute", agentLoop.runs.Load())
	}

	// Bounded wait proves a third outbound never arrives.
	shortCtx, shortCancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer shortCancel()
	if third, ok := msgBus.SubscribeOutbound(shortCtx); ok {
		t.Fatalf("unexpected third outbound %+v; fail-closed must emit exactly config error + cleanup", third)
	}
}
