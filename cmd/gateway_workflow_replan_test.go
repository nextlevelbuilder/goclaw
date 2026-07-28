package cmd

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/nextlevelbuilder/goclaw/internal/config"
	"github.com/nextlevelbuilder/goclaw/internal/providers"
	"github.com/nextlevelbuilder/goclaw/internal/store"
	"github.com/nextlevelbuilder/goclaw/internal/teamworkclassify"
	"github.com/nextlevelbuilder/goclaw/internal/tools"
	"github.com/nextlevelbuilder/goclaw/internal/workflowactions"
)

func canonicalWorkflowPlanForReplanTest(t *testing.T, reviewStatus string) (*teamworkclassify.WorkflowPlan, []byte, string) {
	t.Helper()
	input := plannerTestInputForWorkflowReplan(t)
	plan := &teamworkclassify.WorkflowPlan{
		SchemaVersion:       teamworkclassify.WorkflowPlanSchemaVersion,
		Goal:                "recover the workflow",
		CoordinatorAgentID:  input.CoordinatorAgentID,
		CoordinatorAgentKey: input.CoordinatorAgentKey,
		FinalOwnerAgentID:   input.Members[0].AgentID,
		FinalOwnerAgentKey:  input.Members[0].AgentKey,
		ReviewStatus:        reviewStatus,
		TerminalStepID:      "integrate",
		Steps: []teamworkclassify.WorkflowStep{
			{ID: "work", Title: "Work", Instruction: "Produce work", OwnerAgentID: input.Members[1].AgentID, OwnerAgentKey: input.Members[1].AgentKey, CapabilityKey: "general", WorkflowRole: "work", RequiredTools: []string{}, DependsOn: []string{}, RequiredOutput: true},
			{ID: "integrate", Title: "Integrate", Instruction: "Integrate work", OwnerAgentID: input.Members[0].AgentID, OwnerAgentKey: input.Members[0].AgentKey, CapabilityKey: "general", WorkflowRole: "integration", RequiredTools: []string{}, DependsOn: []string{"work"}, RequiredOutput: true, Terminal: true},
		},
	}
	constraint, err := teamworkclassify.BuildPlanConstraint(plan)
	if err != nil {
		t.Fatalf("BuildPlanConstraint: %v", err)
	}
	return plan, constraint.CanonicalPlan, constraint.PlanHash
}

func plannerTestInputForWorkflowReplan(t *testing.T) teamworkclassify.Input {
	t.Helper()
	// UUID values only need to be canonical and distinct for freeze/hash tests.
	return teamworkclassify.Input{
		CoordinatorAgentID:  mustNewUUIDForReplanTest(t),
		CoordinatorAgentKey: "lead",
		Members: []teamworkclassify.Profile{
			{AgentID: mustNewUUIDForReplanTest(t), AgentKey: "integrator"},
			{AgentID: mustNewUUIDForReplanTest(t), AgentKey: "worker"},
		},
	}
}

func mustNewUUIDForReplanTest(t *testing.T) uuid.UUID {
	t.Helper()
	return uuid.New()
}

func TestWorkflowReplanPlanVerifiesCanonicalHashAcrossJSONRepresentation(t *testing.T) {
	plan, canonical, hash := canonicalWorkflowPlanForReplanTest(t, "none")
	pretty := []byte(strings.ReplaceAll(string(canonical), `,"`, ",\n  \""))
	workflow := &store.TeamWorkflowData{CanonicalPlan: pretty, PlanHash: hash}

	got, err := workflowReplanPlan(workflow)
	if err != nil {
		t.Fatalf("workflowReplanPlan: %v", err)
	}
	if got.Goal != plan.Goal {
		t.Fatalf("goal = %q, want %q", got.Goal, plan.Goal)
	}
}

func TestWorkflowReplanPlanRejectsHashMismatch(t *testing.T) {
	_, canonical, _ := canonicalWorkflowPlanForReplanTest(t, "none")
	workflow := &store.TeamWorkflowData{CanonicalPlan: canonical, PlanHash: strings.Repeat("0", 64)}
	if _, err := workflowReplanPlan(workflow); err == nil || !strings.Contains(err.Error(), "hash mismatch") {
		t.Fatalf("workflowReplanPlan error = %v, want hash mismatch", err)
	}
}

func TestWorkflowReplanReviewRequired(t *testing.T) {
	for _, status := range []string{"required", "included", " INCLUDED "} {
		if !workflowReplanReviewRequired(&teamworkclassify.WorkflowPlan{ReviewStatus: status}) {
			t.Fatalf("review status %q must be inherited", status)
		}
	}
	if workflowReplanReviewRequired(&teamworkclassify.WorkflowPlan{ReviewStatus: "none"}) {
		t.Fatal("review_status=none must not require review")
	}
}

func TestBuildWorkflowReplanInputBoundsEvidenceAndIncludesRecentComments(t *testing.T) {
	blocked := &store.TeamTaskData{
		WorkflowStepID: strings.Repeat("s", workflowReplanStepIDRunes+10),
		Subject:        strings.Repeat("u", workflowReplanSubjectRunes+10),
		BlockerReason:  strings.Repeat("b", workflowReplanBlockerReasonRunes+10),
	}
	completedResult := strings.Repeat("r", workflowReplanResultRunes+10)
	tasks := []store.TeamTaskData{{
		WorkflowKind:   store.TeamWorkflowTaskKindWork,
		Status:         store.TeamTaskStatusCompleted,
		WorkflowStepID: "completed",
		Subject:        "Completed evidence",
		Result:         &completedResult,
	}}
	comments := make([]store.TeamTaskCommentData, workflowReplanCommentLimit+1)
	for i := range comments {
		comments[i].Content = strings.Repeat(string(rune('a'+i)), workflowReplanCommentRunes+10)
	}

	message, context := buildWorkflowReplanInput(
		strings.Repeat("g", workflowReplanGoalRunes+10),
		blocked,
		strings.Repeat("q", store.MaxWorkflowActionReasonRunes+10),
		tasks,
		comments,
	)

	if len([]rune(context)) > workflowReplanContextRunes {
		t.Fatalf("context has %d runes, want at most %d", len([]rune(context)), workflowReplanContextRunes)
	}
	if strings.Contains(context, strings.Repeat("a", workflowReplanCommentRunes)) {
		t.Fatal("oldest comment must be excluded from bounded recent evidence")
	}
	if !strings.Contains(context, strings.Repeat("b", workflowReplanCommentRunes)) ||
		!strings.Contains(context, strings.Repeat("f", workflowReplanCommentRunes)) {
		t.Fatal("recent comments are missing from replanning evidence")
	}
	if strings.Contains(context, strings.Repeat("r", workflowReplanResultRunes+1)) {
		t.Fatal("completed result exceeded its per-field bound")
	}
	wantMessageRunes := workflowReplanGoalRunes + len([]rune("\n\nReplacement-plan requirements from the coordinator:\n")) + store.MaxWorkflowActionReasonRunes
	if got := len([]rune(message)); got != wantMessageRunes {
		t.Fatalf("message has %d runes, want %d", got, wantMessageRunes)
	}
}

func TestValidateWorkflowReplanRosterRechecksMembersAndTools(t *testing.T) {
	leadID, ownerID := uuid.New(), uuid.New()
	constraint := &tools.TeamWorkPlanConstraint{
		CoordinatorAgentID: leadID, CoordinatorAgentKey: "lead",
		Steps: []tools.TeamWorkPlanStepConstraint{{
			OwnerAgentID: ownerID, OwnerAgentKey: "worker", RequiredTools: []string{"read_file"},
		}},
	}
	input := teamworkclassify.Input{
		CoordinatorAgentID: leadID, CoordinatorAgentKey: "lead",
		Members: []teamworkclassify.Profile{{
			AgentID: ownerID, AgentKey: "worker", AvailableTools: []string{"read_file"},
		}},
	}
	if err := validateWorkflowReplanRoster(input, constraint); err != nil {
		t.Fatalf("valid roster rejected: %v", err)
	}

	input.Members = nil
	if err := validateWorkflowReplanRoster(input, constraint); err == nil || !strings.Contains(err.Error(), "roster changed") {
		t.Fatalf("removed owner error = %v", err)
	}

	input.Members = []teamworkclassify.Profile{{AgentID: ownerID, AgentKey: "worker"}}
	if err := validateWorkflowReplanRoster(input, constraint); err == nil || !strings.Contains(err.Error(), "required tool") {
		t.Fatalf("revoked tool error = %v", err)
	}
}

func TestTruncateWorkflowReplanTextIsRuneSafe(t *testing.T) {
	if got := truncateWorkflowReplanText("  甲乙丙丁  ", 3); got != "甲乙丙" {
		t.Fatalf("truncateWorkflowReplanText() = %q", got)
	}
}

type workflowReplanTestAgentStore struct {
	store.AgentStore
	agents map[uuid.UUID]store.AgentData
}

func (s *workflowReplanTestAgentStore) GetByID(_ context.Context, id uuid.UUID) (*store.AgentData, error) {
	agent, ok := s.agents[id]
	if !ok {
		return nil, store.ErrTaskNotFound
	}
	return &agent, nil
}

func (s *workflowReplanTestAgentStore) GetByIDs(_ context.Context, ids []uuid.UUID) ([]store.AgentData, error) {
	result := make([]store.AgentData, 0, len(ids))
	for _, id := range ids {
		if agent, ok := s.agents[id]; ok {
			result = append(result, agent)
		}
	}
	return result, nil
}

type workflowReplanTestTeamStore struct {
	store.TeamStore
	store.TeamWorkflowStore
	teams            map[uuid.UUID]store.TeamData
	members          map[uuid.UUID][]store.TeamMemberData
	workflow         *store.TeamWorkflowData
	blocked          *store.TeamTaskData
	workflowTasks    []store.TeamTaskData
	teamForAgent     uuid.UUID
	commitCalls      int
	committedReplans []store.WorkflowReplan
}

func (s *workflowReplanTestTeamStore) GetTeam(_ context.Context, id uuid.UUID) (*store.TeamData, error) {
	team, ok := s.teams[id]
	if !ok {
		return nil, store.ErrTaskNotFound
	}
	return &team, nil
}

func (s *workflowReplanTestTeamStore) GetTeamForAgent(ctx context.Context, _ uuid.UUID) (*store.TeamData, error) {
	return s.GetTeam(ctx, s.teamForAgent)
}

func (s *workflowReplanTestTeamStore) ListMembers(_ context.Context, id uuid.UUID) ([]store.TeamMemberData, error) {
	return append([]store.TeamMemberData(nil), s.members[id]...), nil
}

func (s *workflowReplanTestTeamStore) GetWorkflow(_ context.Context, id uuid.UUID) (*store.TeamWorkflowData, error) {
	if s.workflow == nil || s.workflow.ID != id {
		return nil, store.ErrTaskNotFound
	}
	copy := *s.workflow
	copy.CanonicalPlan = append([]byte(nil), s.workflow.CanonicalPlan...)
	return &copy, nil
}

func (s *workflowReplanTestTeamStore) GetTask(_ context.Context, id uuid.UUID) (*store.TeamTaskData, error) {
	if s.blocked == nil || s.blocked.ID != id {
		return nil, store.ErrTaskNotFound
	}
	copy := *s.blocked
	return &copy, nil
}

func (s *workflowReplanTestTeamStore) ListWorkflowTasks(_ context.Context, id uuid.UUID) ([]store.TeamTaskData, error) {
	if s.workflow == nil || s.workflow.ID != id {
		return nil, store.ErrTaskNotFound
	}
	return append([]store.TeamTaskData(nil), s.workflowTasks...), nil
}

func (s *workflowReplanTestTeamStore) ListRecentTaskComments(context.Context, uuid.UUID, int) ([]store.TeamTaskCommentData, error) {
	return nil, nil
}

func (s *workflowReplanTestTeamStore) CommitWorkflowReplan(_ context.Context, replan store.WorkflowReplan) (store.WorkflowActionResult, error) {
	s.commitCalls++
	s.committedReplans = append(s.committedReplans, replan)
	return store.WorkflowActionResult{Outcome: store.WorkflowActionApplied, Action: store.WorkflowActionApplyReplan, Workflow: s.workflow, Tasks: replan.Tasks}, nil
}

type workflowReplanTestBuiltinStore struct {
	store.BuiltinToolStore
	revoked bool
}

func (s *workflowReplanTestBuiltinStore) List(context.Context) ([]store.BuiltinToolDef, error) {
	tools := []store.BuiltinToolDef{{Name: "team_tasks", Enabled: true}, {Name: "read_file", Enabled: true}}
	if s.revoked {
		tools[1].Enabled = false
	}
	return tools, nil
}

type workflowReplanTestTenantToolStore struct {
	store.BuiltinToolTenantConfigStore
}

func (workflowReplanTestTenantToolStore) ListAll(context.Context, uuid.UUID) (map[string]bool, error) {
	return nil, nil
}

type workflowReplanTestMCPStore struct{ store.MCPAgentGrantBatchStore }

func (workflowReplanTestMCPStore) ListServers(context.Context) ([]store.MCPServerData, error) {
	return nil, nil
}
func (workflowReplanTestMCPStore) ListAgentGrantsByAgentIDs(context.Context, []uuid.UUID) ([]store.MCPAgentGrant, error) {
	return nil, nil
}

type workflowReplanTestProvider struct {
	name      string
	planner   string
	critic    string
	err       error
	onPlanner func()
}

func (p *workflowReplanTestProvider) Name() string         { return p.name }
func (p *workflowReplanTestProvider) DefaultModel() string { return "replan-test-model" }
func (p *workflowReplanTestProvider) Chat(_ context.Context, req providers.ChatRequest) (*providers.ChatResponse, error) {
	if p.err != nil {
		return nil, p.err
	}
	system := ""
	if len(req.Messages) > 0 {
		system = req.Messages[0].Content
	}
	if strings.Contains(system, "backend workflow-recovery planner") {
		if p.onPlanner != nil {
			p.onPlanner()
		}
		return &providers.ChatResponse{Content: p.planner}, nil
	}
	if strings.Contains(system, "independently critique a proposed execution assignment") {
		return &providers.ChatResponse{Content: p.critic}, nil
	}
	return nil, errors.New("unexpected workflow replanner provider call")
}
func (*workflowReplanTestProvider) ChatStream(context.Context, providers.ChatRequest, func(providers.StreamChunk)) (*providers.ChatResponse, error) {
	return nil, nil
}

type workflowReplanFixture struct {
	tenantID         uuid.UUID
	teamAID          uuid.UUID
	teamBID          uuid.UUID
	leadID           uuid.UUID
	workerAID        uuid.UUID
	integratorAID    uuid.UUID
	workerBID        uuid.UUID
	workflow         *store.TeamWorkflowData
	blocked          *store.TeamTaskData
	teams            *workflowReplanTestTeamStore
	agents           *workflowReplanTestAgentStore
	builtinTools     *workflowReplanTestBuiltinStore
	provider         *workflowReplanTestProvider
	providerRegistry *providers.Registry
	request          workflowactions.ReplanRequest
}

func newWorkflowReplanFixture(t *testing.T) *workflowReplanFixture {
	t.Helper()
	fixture := &workflowReplanFixture{
		tenantID: uuid.New(), teamAID: uuid.New(), teamBID: uuid.New(), leadID: uuid.New(),
		workerAID: uuid.New(), integratorAID: uuid.New(), workerBID: uuid.New(),
		builtinTools: &workflowReplanTestBuiltinStore{},
	}
	original := workflowReplanPlanForFixture(t, fixture, fixture.workerAID, fixture.integratorAID, "read_file")
	originalConstraint, err := teamworkclassify.BuildPlanConstraint(original)
	if err != nil {
		t.Fatalf("freeze stored plan: %v", err)
	}
	fixture.workflow = &store.TeamWorkflowData{
		BaseModel: store.BaseModel{ID: uuid.New()}, TenantID: fixture.tenantID, TeamID: fixture.teamAID,
		Status: store.TeamWorkflowStatusNeedsRevision, PlanRevision: 1,
		CoordinatorAgentID: fixture.leadID, CoordinatorAgentKey: "shared-lead",
		CanonicalPlan: originalConstraint.CanonicalPlan, PlanHash: originalConstraint.PlanHash, SchemaVersion: originalConstraint.SchemaVersion,
	}
	fixture.blocked = &store.TeamTaskData{
		BaseModel: store.BaseModel{ID: uuid.New()}, TenantID: fixture.tenantID, TeamID: fixture.teamAID,
		WorkflowID: &fixture.workflow.ID, WorkflowKind: store.TeamWorkflowTaskKindWork,
		WorkflowStepID: "work", Status: store.TeamTaskStatusBlocked, PlanRevision: 1, Subject: "blocked work", BlockerReason: "need replacement",
	}
	fixture.teams = &workflowReplanTestTeamStore{
		teams: map[uuid.UUID]store.TeamData{
			fixture.teamAID: {BaseModel: store.BaseModel{ID: fixture.teamAID}, Name: "Team A", LeadAgentID: fixture.leadID, LeadAgentKey: "shared-lead"},
			fixture.teamBID: {BaseModel: store.BaseModel{ID: fixture.teamBID}, Name: "Team B", LeadAgentID: fixture.leadID, LeadAgentKey: "shared-lead"},
		},
		members: map[uuid.UUID][]store.TeamMemberData{
			fixture.teamAID: {
				{TeamID: fixture.teamAID, AgentID: fixture.leadID, AgentKey: "shared-lead", Role: store.TeamRoleLead},
				{TeamID: fixture.teamAID, AgentID: fixture.workerAID, AgentKey: "team-a-worker", Role: store.TeamRoleMember},
				{TeamID: fixture.teamAID, AgentID: fixture.integratorAID, AgentKey: "team-a-integrator", Role: store.TeamRoleMember},
			},
			fixture.teamBID: {
				{TeamID: fixture.teamBID, AgentID: fixture.leadID, AgentKey: "shared-lead", Role: store.TeamRoleLead},
				{TeamID: fixture.teamBID, AgentID: fixture.workerBID, AgentKey: "team-b-only-worker", Role: store.TeamRoleMember},
			},
		},
		workflow: fixture.workflow, blocked: fixture.blocked, workflowTasks: []store.TeamTaskData{*fixture.blocked}, teamForAgent: fixture.teamBID,
	}
	fixture.agents = &workflowReplanTestAgentStore{agents: map[uuid.UUID]store.AgentData{
		fixture.leadID:        {BaseModel: store.BaseModel{ID: fixture.leadID}, TenantID: fixture.tenantID, AgentKey: "shared-lead", Provider: "replan-test", Model: "replan-test-model"},
		fixture.workerAID:     {BaseModel: store.BaseModel{ID: fixture.workerAID}, TenantID: fixture.tenantID, AgentKey: "team-a-worker", Provider: "replan-test"},
		fixture.integratorAID: {BaseModel: store.BaseModel{ID: fixture.integratorAID}, TenantID: fixture.tenantID, AgentKey: "team-a-integrator", Provider: "replan-test"},
		fixture.workerBID:     {BaseModel: store.BaseModel{ID: fixture.workerBID}, TenantID: fixture.tenantID, AgentKey: "team-b-only-worker", Provider: "replan-test"},
	}}
	replacement := workflowReplanPlanForFixture(t, fixture, fixture.workerAID, fixture.integratorAID, "read_file")
	fixture.provider = &workflowReplanTestProvider{name: "replan-test", planner: workflowReplanPlannerJSON(t, replacement), critic: `{"valid":true,"issues":[]}`}
	fixture.providerRegistry = providers.NewRegistry(store.TenantIDFromContext)
	fixture.providerRegistry.RegisterForTenant(fixture.tenantID, fixture.provider)
	lead := fixture.leadID
	teamA := fixture.teams.teams[fixture.teamAID]
	fixture.request = workflowactions.ReplanRequest{
		Team: &teamA, Workflow: fixture.workflow, Blocked: fixture.blocked, CoordinatorID: fixture.leadID,
		Guard: store.WorkflowActionGuard{
			Action: store.WorkflowActionApplyReplan, TeamID: fixture.teamAID, WorkflowID: fixture.workflow.ID,
			ExpectedStatus: store.TeamWorkflowStatusNeedsRevision, ExpectedPlanRevision: 1,
			TaskID: &fixture.blocked.ID, ExpectedTaskStatus: store.TeamTaskStatusBlocked, Reason: "replace blocked work",
			Actor: store.WorkflowActionActor{Kind: store.WorkflowActorCoordinator, AgentID: &lead},
		},
	}
	return fixture
}

func workflowReplanPlanForFixture(t *testing.T, fixture *workflowReplanFixture, workerID, integratorID uuid.UUID, requiredTool string) *teamworkclassify.WorkflowPlan {
	t.Helper()
	workerKey, integratorKey := "team-a-worker", "team-a-integrator"
	if workerID == fixture.workerBID {
		workerKey = "team-b-only-worker"
	}
	if integratorID == fixture.workerBID {
		integratorKey = "team-b-only-worker"
	}
	return &teamworkclassify.WorkflowPlan{
		SchemaVersion: teamworkclassify.WorkflowPlanSchemaVersion, Goal: "recover the workflow",
		CoordinatorAgentID: fixture.leadID, CoordinatorAgentKey: "shared-lead",
		FinalOwnerAgentID: integratorID, FinalOwnerAgentKey: integratorKey, ReviewStatus: "none", TerminalStepID: "integrate",
		Steps: []teamworkclassify.WorkflowStep{
			{ID: "work", Title: "Work", Instruction: "Produce work", OwnerAgentID: workerID, OwnerAgentKey: workerKey, CapabilityKey: "general", WorkflowRole: "work", RequiredTools: []string{requiredTool}, RequiredOutput: true},
			{ID: "integrate", Title: "Integrate", Instruction: "Integrate work", OwnerAgentID: integratorID, OwnerAgentKey: integratorKey, CapabilityKey: "general", WorkflowRole: "integration", RequiredTools: []string{requiredTool}, DependsOn: []string{"work"}, RequiredOutput: true, Terminal: true},
		},
	}
}

func workflowReplanPlannerJSON(t *testing.T, plan *teamworkclassify.WorkflowPlan) string {
	t.Helper()
	payload := map[string]any{
		"workflow_mode": "multi_role", "current_agent_role": "lead", "task_type": "ops", "current_agent_fit": "partial",
		"best_team_owner": "", "best_team_owner_role": "", "best_team_fit": "strong", "specialist_match_found": true,
		"lead_selected_as_fallback": false, "routing_priority_used": "role_task_match", "owner_selection_reason": "canonical team roster",
		"followup_context_used_for_reference_only": true, "workflow_executable": true, "decision": "team", "required_tool": "team_tasks",
		"reason": "replace the blocked workflow", "plan": plan,
	}
	raw, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("marshal replacement planner response: %v", err)
	}
	return string(raw)
}

func (f *workflowReplanFixture) call(t *testing.T) (store.WorkflowActionResult, error) {
	t.Helper()
	replanner := buildWorkflowReplanner(
		&store.Stores{Teams: f.teams, Agents: f.agents}, f.providerRegistry, nil,
		teamworkclassify.ProfileStores{Agents: f.agents, Teams: f.teams, BuiltinTools: f.builtinTools, TenantToolConfigs: workflowReplanTestTenantToolStore{}, MCP: workflowReplanTestMCPStore{}, ToolPolicy: tools.NewPolicyEngine(&config.ToolsConfig{}), ToolRegistry: tools.NewRegistry()},
		nil, nil, t.TempDir(),
	)
	return replanner(store.WithTenantID(context.Background(), f.tenantID), f.request)
}

func workflowReplanFixtureState(t *testing.T, fixture *workflowReplanFixture) string {
	t.Helper()
	state := struct {
		Workflow store.TeamWorkflowData
		Blocked  store.TeamTaskData
		Tasks    []store.TeamTaskData
	}{Workflow: *fixture.workflow, Blocked: *fixture.blocked, Tasks: fixture.teams.workflowTasks}
	raw, err := json.Marshal(state)
	if err != nil {
		t.Fatalf("marshal fixture state: %v", err)
	}
	return string(raw)
}

func TestBuildWorkflowReplannerClosureFailuresDoNotCommitOrMutate(t *testing.T) {
	tests := []struct {
		name    string
		prepare func(t *testing.T, f *workflowReplanFixture)
		wantErr string
	}{
		{
			name: "provider resolution", wantErr: "resolve workflow coordinator provider",
			prepare: func(_ *testing.T, f *workflowReplanFixture) {
				f.providerRegistry = providers.NewRegistry(store.TenantIDFromContext)
			},
		},
		{
			name: "planner failure", wantErr: "plan replacement workflow",
			prepare: func(_ *testing.T, f *workflowReplanFixture) { f.provider.err = errors.New("planner unavailable") },
		},
		{
			name: "critic failure", wantErr: "plan replacement workflow",
			prepare: func(_ *testing.T, f *workflowReplanFixture) { f.provider.critic = "not json" },
		},
		{
			name: "canonical hash mismatch", wantErr: "canonical plan hash mismatch",
			prepare: func(_ *testing.T, f *workflowReplanFixture) { f.workflow.PlanHash = strings.Repeat("0", 64) },
		},
		{
			name: "team b only owner and tool", wantErr: "plan replacement workflow",
			prepare: func(t *testing.T, f *workflowReplanFixture) {
				f.provider.planner = workflowReplanPlannerJSON(t, workflowReplanPlanForFixture(t, f, f.workerBID, f.integratorAID, "team-b-only-tool"))
			},
		},
		{
			name: "delayed roster revocation", wantErr: "roster changed",
			prepare: func(_ *testing.T, f *workflowReplanFixture) {
				f.provider.onPlanner = func() { f.teams.members[f.teamAID] = f.teams.members[f.teamAID][:2] }
			},
		},
		{
			name: "delayed tool revocation", wantErr: "required tool",
			prepare: func(_ *testing.T, f *workflowReplanFixture) {
				f.provider.onPlanner = func() { f.builtinTools.revoked = true }
			},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			fixture := newWorkflowReplanFixture(t)
			test.prepare(t, fixture)
			before := workflowReplanFixtureState(t, fixture)
			result, err := fixture.call(t)
			if err == nil || !strings.Contains(err.Error(), test.wantErr) {
				t.Fatalf("replan result=%+v error=%v, want error containing %q", result, err, test.wantErr)
			}
			if fixture.teams.commitCalls != 0 {
				t.Fatalf("CommitWorkflowReplan calls = %d, want 0", fixture.teams.commitCalls)
			}
			if after := workflowReplanFixtureState(t, fixture); after != before {
				t.Fatalf("failed replan mutated workflow/task state\nbefore=%s\nafter=%s", before, after)
			}
		})
	}
}

func TestBuildWorkflowReplannerBindsSharedCoordinatorToWorkflowTeam(t *testing.T) {
	fixture := newWorkflowReplanFixture(t)
	before := workflowReplanFixtureState(t, fixture)
	result, err := fixture.call(t)
	if err != nil {
		t.Fatalf("replan: %v", err)
	}
	if !result.Applied() || fixture.teams.commitCalls != 1 {
		t.Fatalf("replan result=%+v commits=%d, want one applied Team A commit", result, fixture.teams.commitCalls)
	}
	committed := fixture.teams.committedReplans[0]
	if committed.CoordinatorID != fixture.leadID || len(committed.Tasks) != 2 {
		t.Fatalf("committed replan = %+v", committed)
	}
	for _, task := range committed.Tasks {
		if task.TeamID != fixture.teamAID || task.OwnerAgentID == nil || *task.OwnerAgentID == fixture.workerBID {
			t.Fatalf("Team B leaked into Team A replacement task: %+v", task)
		}
	}
	if after := workflowReplanFixtureState(t, fixture); after != before {
		t.Fatalf("successful fake commit must not mutate source workflow/task state\nbefore=%s\nafter=%s", before, after)
	}
	if got := fixture.teams.members[fixture.teamBID][1].AgentID; got != fixture.workerBID {
		t.Fatalf("Team B member = %s, want %s", got, fixture.workerBID)
	}
}
