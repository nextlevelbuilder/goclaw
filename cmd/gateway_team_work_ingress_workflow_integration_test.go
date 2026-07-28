//go:build integration

package cmd

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/golang-migrate/migrate/v4"
	_ "github.com/golang-migrate/migrate/v4/database/postgres"
	_ "github.com/golang-migrate/migrate/v4/source/file"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	_ "github.com/jackc/pgx/v5/stdlib"

	"github.com/nextlevelbuilder/goclaw/internal/agent"
	"github.com/nextlevelbuilder/goclaw/internal/bus"
	"github.com/nextlevelbuilder/goclaw/internal/config"
	"github.com/nextlevelbuilder/goclaw/internal/providers"
	"github.com/nextlevelbuilder/goclaw/internal/scheduler"
	"github.com/nextlevelbuilder/goclaw/internal/store"
	"github.com/nextlevelbuilder/goclaw/internal/store/pg"
	"github.com/nextlevelbuilder/goclaw/internal/teamworkclassify"
	"github.com/nextlevelbuilder/goclaw/internal/tools"
)

const gatewayWorkflowIngressMessage = "score and recommend one option, then independently critique the choice"

type gatewayWorkflowLeadProvider struct {
	db       *sql.DB
	tenantID uuid.UUID
	leadID   uuid.UUID
	planJSON string

	classifyCycles atomic.Int32
	realCalls      atomic.Int32
	auditBeforeRun atomic.Bool

	mu      sync.Mutex
	auditID uuid.UUID
	errs    []error
}

func (p *gatewayWorkflowLeadProvider) Name() string         { return "phase10-lead" }
func (p *gatewayWorkflowLeadProvider) DefaultModel() string { return "test-model" }
func (p *gatewayWorkflowLeadProvider) ChatStream(ctx context.Context, req providers.ChatRequest, _ func(providers.StreamChunk)) (*providers.ChatResponse, error) {
	return p.Chat(ctx, req)
}

func (p *gatewayWorkflowLeadProvider) Chat(ctx context.Context, req providers.ChatRequest) (*providers.ChatResponse, error) {
	all := gatewayWorkflowRequestText(req)
	system := ""
	if len(req.Messages) > 0 {
		system = req.Messages[0].Content
	}
	switch {
	case strings.Contains(system, "Resolve the current user message into a complete standalone request"):
		p.classifyCycles.Add(1)
		return &providers.ChatResponse{Content: `{"standalone_request":"` + gatewayWorkflowIngressMessage + `","relation":"new","user_intent":"produce a reviewed recommendation","inherited_scope":[],"requested_deliverables":["reviewed recommendation"],"quality_requirements":["independent critique"],"explicit_constraints":[],"ambiguities":[],"needs_clarification":false}`}, nil
	case strings.Contains(system, "Independently verify that the draft standalone request"):
		return &providers.ChatResponse{Content: `{"valid":true,"issues":[],"corrected_resolution":null}`}, nil
	case strings.Contains(system, "You independently verify the semantic work shape"):
		return &providers.ChatResponse{Content: `{"work_shape":"reviewed_decision","shape_traits":[{"type":"score_or_rank","source":"current_request","evidence":"score and recommend one option"},{"type":"explicit_critique","source":"current_request","evidence":"independently critique the choice"}],"independent_review_required":true}`}, nil
	case strings.Contains(system, "decompose one already-resolved standalone user request"):
		return &providers.ChatResponse{Content: `{"workflow_mode":"multi_role","independent_review_required":true,"reason":"independent review is required","work_units":[{"id":"draft","description":"score and recommend an option","required_output":"recommendation"},{"id":"review","description":"independently critique the recommendation","required_output":"critique"},{"id":"integrate","description":"integrate the critique","required_output":"reviewed recommendation"}],"dependencies":[{"from":"draft","to":"review"},{"from":"review","to":"integrate"}],"required_outputs":["reviewed recommendation"]}`}, nil
	case strings.Contains(system, "You select canonical owners and, when needed, build a team workflow plan"):
		return &providers.ChatResponse{Content: p.planJSON}, nil
	case strings.Contains(system, "independently critique a proposed execution assignment"):
		return &providers.ChatResponse{Content: `{"valid":true,"issues":[]}`}, nil
	case strings.Contains(all, "[Internal workflow finalization]"):
		return &providers.ChatResponse{Content: "Final reviewed recommendation", FinishReason: "stop"}, nil
	}

	call := p.realCalls.Add(1)
	if call == 1 {
		var count int
		var auditID uuid.UUID
		err := p.db.QueryRowContext(ctx, `SELECT id,count(*) OVER() FROM team_work_classification_audits WHERE tenant_id=$1 AND ingress=$2 AND agent_id=$3 LIMIT 1`, p.tenantID, store.TeamWorkIngressInbound, p.leadID).Scan(&auditID, &count)
		if err != nil || count != 1 || auditID == uuid.Nil {
			p.recordError(fmt.Errorf("first lead call observed audit count=%d id=%s err=%v", count, auditID, err))
		} else {
			p.mu.Lock()
			p.auditID = auditID
			p.mu.Unlock()
			p.auditBeforeRun.Store(true)
		}
		return &providers.ChatResponse{ToolCalls: []providers.ToolCall{{ID: "workflow-search", Name: "team_tasks", Arguments: map[string]any{"action": "search", "query": "reviewed recommendation"}}}, FinishReason: "tool_calls"}, nil
	}
	if call == 2 {
		return &providers.ChatResponse{ToolCalls: []providers.ToolCall{{ID: "workflow-create", Name: "team_tasks", Arguments: map[string]any{"action": "create_workflow"}}}, FinishReason: "tool_calls"}, nil
	}
	return &providers.ChatResponse{Content: "NO_REPLY", FinishReason: "stop"}, nil
}

func (p *gatewayWorkflowLeadProvider) recordError(err error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.errs = append(p.errs, err)
}

func (p *gatewayWorkflowLeadProvider) snapshot() (uuid.UUID, []error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.auditID, append([]error(nil), p.errs...)
}

type gatewayWorkflowSequence struct {
	mu    sync.Mutex
	steps []string
}

func (s *gatewayWorkflowSequence) append(step string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.steps = append(s.steps, step)
}

func (s *gatewayWorkflowSequence) snapshot() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]string(nil), s.steps...)
}

type gatewayWorkflowMemberProvider struct {
	db       *sql.DB
	tenantID uuid.UUID
	agentID  uuid.UUID
	answer   string
	sequence *gatewayWorkflowSequence

	mu       sync.Mutex
	steps    []string
	accepted map[string]bool
	errs     []error
}

func (p *gatewayWorkflowMemberProvider) Name() string         { return "phase10-member" }
func (p *gatewayWorkflowMemberProvider) DefaultModel() string { return "test-model" }
func (p *gatewayWorkflowMemberProvider) ChatStream(ctx context.Context, req providers.ChatRequest, _ func(providers.StreamChunk)) (*providers.ChatResponse, error) {
	return p.Chat(ctx, req)
}
func (p *gatewayWorkflowMemberProvider) Chat(ctx context.Context, _ providers.ChatRequest) (*providers.ChatResponse, error) {
	var step, status string
	err := p.db.QueryRowContext(ctx, `SELECT workflow_step_id,status FROM team_tasks WHERE tenant_id=$1 AND owner_agent_id=$2 AND workflow_id IS NOT NULL AND workflow_kind=$3 AND status=$4 ORDER BY updated_at DESC LIMIT 1`, p.tenantID, p.agentID, store.TeamWorkflowTaskKindWork, store.TeamTaskStatusInProgress).Scan(&step, &status)
	p.mu.Lock()
	defer p.mu.Unlock()
	if err != nil {
		p.errs = append(p.errs, fmt.Errorf("member provider called before an accepted attempt: %w", err))
	} else {
		p.steps = append(p.steps, step)
		if p.sequence != nil {
			p.sequence.append(step)
		}
		if p.accepted == nil {
			p.accepted = make(map[string]bool)
		}
		p.accepted[step] = status == store.TeamTaskStatusInProgress
	}
	return &providers.ChatResponse{Content: p.answer, FinishReason: "stop"}, nil
}

func (p *gatewayWorkflowMemberProvider) snapshot() ([]string, map[string]bool, []error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	steps := append([]string(nil), p.steps...)
	accepted := make(map[string]bool, len(p.accepted))
	for step, ok := range p.accepted {
		accepted[step] = ok
	}
	return steps, accepted, append([]error(nil), p.errs...)
}

func gatewayWorkflowRequestText(req providers.ChatRequest) string {
	var b strings.Builder
	for _, msg := range req.Messages {
		b.WriteString(msg.Content)
		b.WriteByte('\n')
	}
	return b.String()
}

func gatewayWorkflowIntegrationDB(t *testing.T) *sql.DB {
	t.Helper()
	dsn := os.Getenv("TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("TEST_DATABASE_URL is not set; skipping PostgreSQL integration test")
	}
	parsed, err := pgx.ParseConfig(dsn)
	if err != nil {
		t.Fatal("TEST_DATABASE_URL is invalid")
	}
	if parsed.Host != "localhost" || parsed.Port != 55433 || parsed.Database != "goclaw_test" {
		t.Fatal("refusing to run against an unapproved PostgreSQL target")
	}
	db, err := sql.Open("pgx", dsn)
	if err != nil {
		t.Fatal("open approved PostgreSQL test database failed")
	}
	if err := db.Ping(); err != nil {
		db.Close()
		t.Fatal("approved PostgreSQL test database is unavailable")
	}
	m, err := migrate.New("file://../migrations", dsn)
	if err != nil {
		db.Close()
		t.Fatal("initialize PostgreSQL test migrations failed")
	}
	if err := m.Up(); err != nil && !errors.Is(err, migrate.ErrNoChange) {
		m.Close()
		db.Close()
		t.Fatal("apply PostgreSQL test migrations failed")
	}
	_, _ = m.Close()
	pg.InitSqlx(db)
	t.Cleanup(func() { _ = db.Close() })
	return db
}

func TestGatewayTeamWorkIngressToDurableWorkflow(t *testing.T) {
	db := gatewayWorkflowIntegrationDB(t)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	tenantStore := pg.NewPGTenantStore(db)
	agentStore := pg.NewPGAgentStore(db)
	teamStore := pg.NewPGTeamStore(db)
	sessionStore := pg.NewPGSessionStore(db)

	tenantID, otherTenantID := store.GenNewID(), store.GenNewID()
	leadID, ownerID, reviewerID, teamID := store.GenNewID(), store.GenNewID(), store.GenNewID(), store.GenNewID()
	leadKey := "phase10-lead-" + leadID.String()
	ownerKey := "phase10-owner-" + ownerID.String()
	reviewerKey := "phase10-reviewer-" + reviewerID.String()

	for _, tenant := range []*store.TenantData{
		{ID: tenantID, Name: "Phase 10 tenant", Slug: "phase10-" + tenantID.String(), Status: store.TenantStatusActive},
		{ID: otherTenantID, Name: "Phase 10 other tenant", Slug: "phase10-" + otherTenantID.String(), Status: store.TenantStatusActive},
	} {
		if err := tenantStore.CreateTenant(ctx, tenant); err != nil {
			t.Fatalf("create tenant: %v", err)
		}
	}
	t.Cleanup(func() {
		_, _ = db.Exec("DELETE FROM tenants WHERE id IN ($1,$2)", tenantID, otherTenantID)
	})

	tenantCtx := store.WithTenantID(ctx, tenantID)
	for _, fixture := range []struct {
		id, tenant uuid.UUID
		key, name  string
		isDefault  bool
	}{
		{id: leadID, tenant: tenantID, key: leadKey, name: "Canonical Lead", isDefault: true},
		{id: ownerID, tenant: tenantID, key: ownerKey, name: "Content Owner"},
		{id: reviewerID, tenant: tenantID, key: reviewerKey, name: "Independent Reviewer"},
	} {
		ag := &store.AgentData{BaseModel: store.BaseModel{ID: fixture.id}, TenantID: fixture.tenant, AgentKey: fixture.key, DisplayName: fixture.name, OwnerID: "phase10", Provider: "test", Model: "test-model", AgentType: store.AgentTypePredefined, Status: store.AgentStatusActive, IsDefault: fixture.isDefault, MaxToolIterations: 6}
		if err := agentStore.Create(tenantCtx, ag); err != nil {
			t.Fatalf("create agent %s: %v", fixture.key, err)
		}
	}
	team := &store.TeamData{BaseModel: store.BaseModel{ID: teamID}, Name: "Phase 10 reviewed workflow", LeadAgentID: leadID, Status: store.TeamStatusActive, Settings: json.RawMessage(`{"version":2}`), CreatedBy: "phase10"}
	if err := teamStore.CreateTeam(tenantCtx, team); err != nil {
		t.Fatalf("create team: %v", err)
	}
	for _, membership := range []struct {
		agentID uuid.UUID
		role    string
	}{{leadID, store.TeamRoleLead}, {ownerID, store.TeamRoleMember}, {reviewerID, store.TeamRoleReviewer}} {
		if err := teamStore.AddMember(tenantCtx, teamID, membership.agentID, membership.role); err != nil {
			t.Fatalf("add team member: %v", err)
		}
	}

	plan := &teamworkclassify.WorkflowPlan{
		SchemaVersion:      teamworkclassify.WorkflowPlanSchemaVersion,
		Goal:               "produce a reviewed recommendation",
		CoordinatorAgentID: leadID, CoordinatorAgentKey: leadKey,
		FinalOwnerAgentID: ownerID, FinalOwnerAgentKey: ownerKey,
		ReviewStatus: "included", TerminalStepID: "integrate",
		Steps: []teamworkclassify.WorkflowStep{
			{ID: "draft", Title: "Draft", Instruction: "Score and recommend", OwnerAgentID: ownerID, OwnerAgentKey: ownerKey, CapabilityKey: "content_lead", WorkflowRole: "draft", RequiredOutput: true},
			{ID: "review", Title: "Review", Instruction: "Critique independently", OwnerAgentID: reviewerID, OwnerAgentKey: reviewerKey, CapabilityKey: "qa", WorkflowRole: "critic", DependsOn: []string{"draft"}, RequiredOutput: true},
			{ID: "integrate", Title: "Integrate", Instruction: "Integrate the critique", OwnerAgentID: ownerID, OwnerAgentKey: ownerKey, CapabilityKey: "content_lead", WorkflowRole: "integration", DependsOn: []string{"review"}, RequiredOutput: true, Terminal: true},
		},
	}
	planJSON, err := json.Marshal(map[string]any{
		"workflow_mode": "multi_role", "current_agent_role": "lead", "task_type": "analytics", "current_agent_fit": "partial",
		"best_team_owner": ownerKey, "best_team_owner_role": "content", "best_team_fit": "strong", "specialist_match_found": true,
		"lead_selected_as_fallback": false, "routing_priority_used": "role_task_match", "owner_selection_reason": "reviewed workflow",
		"followup_context_used_for_reference_only": false, "workflow_executable": true, "decision": "team", "required_tool": "team_tasks", "reason": "review required", "plan": plan,
	})
	if err != nil {
		t.Fatal(err)
	}

	leadProvider := &gatewayWorkflowLeadProvider{db: db, tenantID: tenantID, leadID: leadID, planJSON: string(planJSON)}
	executionSequence := &gatewayWorkflowSequence{}
	ownerProvider := &gatewayWorkflowMemberProvider{db: db, tenantID: tenantID, agentID: ownerID, answer: "Owner step completed", sequence: executionSequence}
	reviewerProvider := &gatewayWorkflowMemberProvider{db: db, tenantID: tenantID, agentID: reviewerID, answer: "Independent review completed", sequence: executionSequence}

	msgBus := bus.New()
	dataDir := t.TempDir()
	manager := tools.NewTeamToolManager(teamStore, agentStore, msgBus, dataDir)
	leadTools := tools.NewRegistry()
	leadTools.Register(tools.NewTeamTasksTool(manager, tools.FullTeamPolicy{}))
	toolPolicy := tools.NewPolicyEngine(&config.ToolsConfig{})
	newLoop := func(id string, agentID uuid.UUID, provider providers.Provider, registry *tools.Registry, isLead bool) *agent.Loop {
		return agent.NewLoop(agent.LoopConfig{ID: id, AgentUUID: agentID, TenantID: tenantID, DisplayName: id, IsTeamLead: isLead, Provider: provider, Model: "test-model", Sessions: sessionStore, Workspace: t.TempDir(), DataDir: dataDir, Tools: registry, ToolPolicy: toolPolicy, TeamStore: teamStore, InjectionAction: "off"})
	}
	leadLoop := newLoop(leadKey, leadID, leadProvider, leadTools, true)
	ownerLoop := newLoop(ownerKey, ownerID, ownerProvider, tools.NewRegistry(), false)
	reviewerLoop := newLoop(reviewerKey, reviewerID, reviewerProvider, tools.NewRegistry(), false)

	router := agent.NewRouter()
	router.SetResolver(func(resolveCtx context.Context, key string) (agent.Agent, error) {
		if store.TenantIDFromContext(resolveCtx) != tenantID {
			return nil, fmt.Errorf("unexpected tenant")
		}
		switch key {
		case leadKey:
			return leadLoop, nil
		case ownerKey:
			return ownerLoop, nil
		case reviewerKey:
			return reviewerLoop, nil
		default:
			return nil, fmt.Errorf("unknown agent %q", key)
		}
	})

	cfg := config.Default()
	cfg.DataDir = dataDir
	cfg.Gateway.InboundDebounceMs = 0
	enabled := true
	cfg.Gateway.TeamWorkClassify = &enabled
	cfg.Gateway.TeamWorkClassifyModel = "test-model"
	cfg.Agents.List = map[string]config.AgentSpec{leadKey: {DisplayName: "Canonical Lead", Provider: "test", Model: "test-model", Default: true}}
	queueCfg := scheduler.DefaultQueueConfig()
	queueCfg.DebounceMs = 0
	sched := scheduler.NewScheduler(scheduler.DefaultLanes(), queueCfg, makeSchedulerRunFunc(router, cfg))

	providerReg := providers.NewRegistry(store.TenantIDFromContext)
	providerReg.RegisterForTenant(tenantID, leadProvider)
	consumerCtx, consumerCancel := context.WithCancel(context.Background())
	consumerDone := make(chan struct{})
	go func() {
		defer close(consumerDone)
		consumeInboundMessages(consumerCtx, msgBus, router, cfg, sched, nil, teamStore, nil, nil, sessionStore, agentStore, nil, manager, nil, nil, providerReg, &teamWorkGateTestEmbedder{}, nil, inboundMCPStore{}, inboundBuiltinToolStore{}, inboundTenantToolStore{}, toolPolicy, leadTools, nil)
	}()
	defer func() {
		consumerCancel()
		select {
		case <-consumerDone:
		case <-time.After(5 * time.Second):
			t.Error("inbound consumer did not drain")
		}
		sched.Stop()
	}()

	inbound := bus.InboundMessage{Channel: "phase10", SenderID: "user-1", ChatID: "chat-1", Content: gatewayWorkflowIngressMessage, PeerKind: "direct", TenantID: tenantID, AgentID: leadKey, UserID: "user-1", Metadata: map[string]string{"message_id": "phase10-duplicate-ingress", "locale": "en"}}
	msgBus.PublishInbound(inbound)
	msgBus.PublishInbound(inbound)

	var delivery bus.OutboundMessage
	deliveryCtx, cancelDelivery := context.WithTimeout(ctx, 20*time.Second)
	defer cancelDelivery()
	for delivery.DeliveryAck == nil {
		out, ok := msgBus.SubscribeOutbound(deliveryCtx)
		if !ok {
			t.Fatal("outbound subscription ended before workflow delivery")
		}
		if out.Metadata["workflow_delivery_id"] != "" {
			delivery = out
		}
	}

	// Prove exactly one final workflow delivery: after receiving the first
	// delivery, keep draining outbound for a bounded window and fail if a
	// second message carries a workflow_delivery_id. The duplicate inbound
	// ingress above and any replay/concurrency must not produce a second
	// delivery. The ack is intentionally deferred until after assertions.
	drainCtx, cancelDrain := context.WithTimeout(ctx, time.Second)
	defer cancelDrain()
	for {
		out, ok := msgBus.SubscribeOutbound(drainCtx)
		if !ok {
			break
		}
		if out.Metadata["workflow_delivery_id"] != "" {
			t.Fatalf("received a second workflow delivery %q; want exactly one final delivery", out.Metadata["workflow_delivery_id"])
		}
	}

	if got := leadProvider.classifyCycles.Load(); got != 1 {
		t.Fatalf("classification cycles = %d, want 1", got)
	}
	var auditCount int
	if err := db.QueryRowContext(ctx, `SELECT count(*) FROM team_work_classification_audits WHERE tenant_id=$1 AND ingress=$2 AND agent_id=$3`, tenantID, store.TeamWorkIngressInbound, leadID).Scan(&auditCount); err != nil {
		t.Fatal(err)
	}
	if auditCount != 1 {
		t.Fatalf("inbound classification audit rows = %d, want 1", auditCount)
	}
	auditID, leadErrs := leadProvider.snapshot()
	if !leadProvider.auditBeforeRun.Load() || auditID == uuid.Nil || len(leadErrs) != 0 {
		t.Fatalf("audit-before-lead-run proof failed: before=%v id=%s errors=%v", leadProvider.auditBeforeRun.Load(), auditID, leadErrs)
	}

	var workflowCount int
	if err := db.QueryRowContext(ctx, `SELECT count(*) FROM team_workflows WHERE tenant_id=$1`, tenantID).Scan(&workflowCount); err != nil {
		t.Fatal(err)
	}
	if workflowCount != 1 {
		t.Fatalf("workflow count = %d, want 1", workflowCount)
	}
	workflowID, err := uuid.Parse(delivery.Metadata["workflow_delivery_id"])
	if err != nil {
		t.Fatalf("invalid workflow delivery id: %v", err)
	}
	workflow, err := teamStore.GetWorkflow(tenantCtx, workflowID)
	if err != nil {
		t.Fatal(err)
	}
	if workflow.ClassificationAuditID == nil || *workflow.ClassificationAuditID != auditID {
		t.Fatalf("workflow classification audit = %v, want %s", workflow.ClassificationAuditID, auditID)
	}
	tasks, err := teamStore.ListWorkflowTasks(tenantCtx, workflowID)
	if err != nil {
		t.Fatal(err)
	}
	if len(tasks) != 3 {
		t.Fatalf("workflow work task count = %d, want 3", len(tasks))
	}
	byStep := make(map[string]store.TeamTaskData, len(tasks))
	for _, task := range tasks {
		byStep[task.WorkflowStepID] = task
		if task.Status != store.TeamTaskStatusCompleted {
			t.Errorf("task %s status = %s, want completed", task.WorkflowStepID, task.Status)
		}
		if task.DispatchCount != 1 {
			t.Errorf("task %s dispatch_count = %d, want 1", task.WorkflowStepID, task.DispatchCount)
		}
	}
	for _, step := range []string{"draft", "review", "integrate"} {
		if _, ok := byStep[step]; !ok {
			t.Errorf("missing workflow step %s", step)
		}
	}
	ownerSteps, ownerAccepted, ownerErrs := ownerProvider.snapshot()
	reviewerSteps, reviewerAccepted, reviewerErrs := reviewerProvider.snapshot()
	if fmt.Sprint(ownerSteps) != fmt.Sprint([]string{"draft", "integrate"}) || fmt.Sprint(reviewerSteps) != fmt.Sprint([]string{"review"}) {
		t.Errorf("member executions = owner %v reviewer %v, want owner [draft integrate] reviewer [review]", ownerSteps, reviewerSteps)
	}
	dispatchOrder := executionSequence.snapshot()
	wantOrder := []string{"draft", "review", "integrate"}
	if fmt.Sprint(dispatchOrder) != fmt.Sprint(wantOrder) {
		t.Errorf("dispatch order = %v, want %v", dispatchOrder, wantOrder)
	}
	if len(ownerErrs) != 0 || len(reviewerErrs) != 0 {
		t.Errorf("member acceptance proof errors: owner=%v reviewer=%v", ownerErrs, reviewerErrs)
	}
	for step, accepted := range map[string]bool{"draft": ownerAccepted["draft"], "review": reviewerAccepted["review"], "integrate": ownerAccepted["integrate"]} {
		if !accepted {
			t.Errorf("attempt for %s was not accepted before production member run", step)
		}
	}
	if byStep["review"].UpdatedAt.Before(byStep["draft"].UpdatedAt) || byStep["integrate"].UpdatedAt.Before(byStep["review"].UpdatedAt) {
		t.Errorf("dependency completion ordering violated: draft=%v review=%v integrate=%v", byStep["draft"].UpdatedAt, byStep["review"].UpdatedAt, byStep["integrate"].UpdatedAt)
	}
	if workflow.Status != store.TeamWorkflowStatusCompleted || workflow.FinalizedAt == nil {
		t.Fatalf("workflow terminal state = %s finalized_at=%v", workflow.Status, workflow.FinalizedAt)
	}
	if workflow.DeliveryStatus != store.TeamWorkflowDeliveryEnqueuing || workflow.DeliveredAt != nil {
		t.Fatalf("delivery before ack: status=%s delivered_at=%v", workflow.DeliveryStatus, workflow.DeliveredAt)
	}

	delivery.DeliveryAck(nil)
	var delivered *store.TeamWorkflowData
	for until := time.Now().Add(3 * time.Second); time.Now().Before(until); {
		delivered, err = teamStore.GetWorkflow(tenantCtx, workflowID)
		if err == nil && delivered.DeliveryStatus == store.TeamWorkflowDeliveryDelivered && delivered.DeliveredAt != nil {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	if err != nil || delivered == nil || delivered.DeliveryStatus != store.TeamWorkflowDeliveryDelivered || delivered.DeliveredAt == nil {
		t.Fatalf("delivery after ack: workflow=%+v err=%v", delivered, err)
	}

	_, err = teamStore.GetWorkflow(store.WithTenantID(context.Background(), otherTenantID), workflowID)
	if !errors.Is(err, store.ErrTaskNotFound) {
		t.Fatalf("cross-tenant GetWorkflow error = %v, want ErrTaskNotFound", err)
	}
}
