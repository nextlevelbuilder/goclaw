package pg

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/nextlevelbuilder/goclaw/internal/bus"
	"github.com/nextlevelbuilder/goclaw/internal/providers"
	"github.com/nextlevelbuilder/goclaw/internal/store"
	"github.com/nextlevelbuilder/goclaw/internal/teamworkclassify"
	"github.com/nextlevelbuilder/goclaw/internal/tools"
)

// pgTeamWorkProvider dispatches on the system prompt of each classifier stage so
// the multi-call production pipeline (intent resolve -> intent critic ->
// independent shape verifier -> decomposition -> roster-aware planning ->
// assignment critic) drives end to end. The two intent stages are mechanical and
// identical across flows, so they are answered inline; each test supplies the
// shape/assessment/planning/critique responses that define its flow.
type pgTeamWorkProvider struct {
	shape      string
	assessment string
	planning   string
	critique   string
}

func (p *pgTeamWorkProvider) Chat(_ context.Context, req providers.ChatRequest) (*providers.ChatResponse, error) {
	if len(req.Messages) == 0 {
		return nil, errors.New("empty classifier request")
	}
	system := req.Messages[0].Content
	switch {
	case strings.Contains(system, "Resolve the current user message into a complete standalone request"):
		var payload struct {
			CurrentUserMessage string `json:"current_user_message"`
		}
		_ = json.Unmarshal([]byte(req.Messages[len(req.Messages)-1].Content), &payload)
		return &providers.ChatResponse{Content: fmt.Sprintf(`{"standalone_request":%q,"relation":"new","user_intent":"execute the current request","inherited_scope":[],"requested_deliverables":[],"quality_requirements":[],"explicit_constraints":[],"ambiguities":[],"needs_clarification":false}`, payload.CurrentUserMessage)}, nil
	case strings.Contains(system, "Independently verify that the draft standalone request"):
		return &providers.ChatResponse{Content: `{"valid":true,"issues":[],"corrected_resolution":null}`}, nil
	case strings.Contains(system, "You independently verify the semantic work shape"):
		return &providers.ChatResponse{Content: p.shape}, nil
	case strings.Contains(system, "decompose one already-resolved standalone user request"):
		return &providers.ChatResponse{Content: p.assessment}, nil
	case strings.Contains(system, "independently critique a proposed execution assignment"):
		return &providers.ChatResponse{Content: p.critique}, nil
	default:
		return &providers.ChatResponse{Content: p.planning}, nil
	}
}

func (*pgTeamWorkProvider) ChatStream(context.Context, providers.ChatRequest, func(providers.StreamChunk)) (*providers.ChatResponse, error) {
	return nil, errors.New("streaming is not supported")
}

func (*pgTeamWorkProvider) DefaultModel() string { return "team-work-integration" }
func (*pgTeamWorkProvider) Name() string         { return "team-work-integration" }

func TestPGGetTeamForAgentBuildsCanonicalCoordinator(t *testing.T) {
	db := hooksTestDB(t)
	tenantID, leadID := seedTenantAndAgent(t, db)
	teamID := uuid.New()
	leadKey := "hook-agent-" + leadID.String()
	if _, err := db.Exec(`UPDATE agents SET display_name='Canonical Lead' WHERE id=$1`, leadID); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO agent_teams (id,name,lead_agent_id,status,settings,created_by,tenant_id)
		VALUES ($1,'Runtime Team',$2,'active','{}','owner',$3)`, teamID, leadID, tenantID); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO agent_team_members (team_id,agent_id,role,tenant_id) VALUES ($1,$2,'lead',$3)`, teamID, leadID, tenantID); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _, _ = db.Exec(`DELETE FROM agent_teams WHERE id=$1`, teamID) })

	ctx := store.WithTenantID(context.Background(), tenantID)
	teamStore := NewPGTeamStore(db)
	team, err := teamStore.GetTeamForAgent(ctx, leadID)
	if err != nil {
		t.Fatal(err)
	}
	if team == nil || team.LeadAgentID != leadID || team.LeadAgentKey != leadKey || team.LeadDisplayName != "Canonical Lead" {
		t.Fatalf("canonical team = %+v", team)
	}
	input := teamworkclassify.BuildInputFromStores(ctx, teamworkclassify.ProfileStores{
		Agents: NewPGAgentStore(db), Teams: teamStore,
	}, teamworkclassify.BuildInputOptions{Mode: teamworkclassify.ModeTeam, AgentID: leadID, Message: "coordinate staged work"})
	if input.CoordinatorAgentID != leadID || input.CoordinatorAgentKey != leadKey || input.TeamRole != "lead" {
		t.Fatalf("runtime classifier input coordinator=%s/%q role=%q", input.CoordinatorAgentID, input.CoordinatorAgentKey, input.TeamRole)
	}
}

func TestPGRosterToClassifierAcceptsAtomicAndReviewedFlows(t *testing.T) {
	db := hooksTestDB(t)
	tenantID, leadID := seedTenantAndAgent(t, db)
	ownerID, reviewerID, teamID := uuid.New(), uuid.New(), uuid.New()
	leadKey := "hook-agent-" + leadID.String()
	for _, fixture := range []struct {
		id           uuid.UUID
		key, display string
		capability   string
	}{
		{id: ownerID, key: "runtime-owner", display: "Runtime Owner", capability: "content_lead"},
		{id: reviewerID, key: "runtime-reviewer", display: "Runtime Reviewer", capability: "qa"},
	} {
		otherConfig, err := json.Marshal(map[string]any{"capabilities": []string{fixture.capability}})
		if err != nil {
			t.Fatal(err)
		}
		if _, err := db.Exec(`INSERT INTO agents (id,tenant_id,agent_key,display_name,agent_type,status,provider,model,owner_id,other_config)
			VALUES ($1,$2,$3,$4,'predefined','active','test','test-model','owner',$5)`, fixture.id, tenantID, fixture.key, fixture.display, otherConfig); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := db.Exec(`UPDATE agents SET display_name='Canonical Lead', other_config='{"capabilities":["lead_coordinator"]}' WHERE id=$1`, leadID); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO agent_teams (id,name,lead_agent_id,status,settings,created_by,tenant_id)
		VALUES ($1,'Classifier Runtime Team',$2,'active','{}','owner',$3)`, teamID, leadID, tenantID); err != nil {
		t.Fatal(err)
	}
	for _, member := range []struct {
		id   uuid.UUID
		role string
	}{{leadID, "lead"}, {ownerID, "member"}, {reviewerID, "reviewer"}} {
		if _, err := db.Exec(`INSERT INTO agent_team_members (team_id,agent_id,role,tenant_id) VALUES ($1,$2,$3,$4)`, teamID, member.id, member.role, tenantID); err != nil {
			t.Fatal(err)
		}
	}
	t.Cleanup(func() {
		_, _ = db.Exec(`DELETE FROM agent_teams WHERE id=$1`, teamID)
		_, _ = db.Exec(`DELETE FROM agents WHERE id IN ($1,$2)`, ownerID, reviewerID)
	})

	ctx := store.WithTenantID(context.Background(), tenantID)
	input := teamworkclassify.BuildInputFromStores(ctx, teamworkclassify.ProfileStores{
		Agents: NewPGAgentStore(db), Teams: NewPGTeamStore(db),
	}, teamworkclassify.BuildInputOptions{Mode: teamworkclassify.ModeTeam, AgentID: leadID})
	input.CurrentAgent.AvailableToolsStatus = teamworkclassify.DataStatusKnown
	input.CurrentAgent.AvailableTools = []string{"team_tasks"}
	for i := range input.Members {
		input.Members[i].AvailableToolsStatus = teamworkclassify.DataStatusKnown
	}
	if input.CoordinatorAgentID != leadID || input.CoordinatorAgentKey != leadKey {
		t.Fatalf("database roster lost canonical coordinator: %s/%q", input.CoordinatorAgentID, input.CoordinatorAgentKey)
	}

	atomicMessage := "write one bounded summary"
	input.Message = atomicMessage
	atomicShape := marshalPGClassifierPayload(t, map[string]any{
		"work_shape":                  "atomic",
		"shape_traits":                []map[string]any{{"type": "single_bounded_output", "source": "current_request", "evidence": atomicMessage}},
		"independent_review_required": false,
	})
	atomicAssessment := marshalPGClassifierPayload(t, map[string]any{
		"workflow_mode": "self", "independent_review_required": false, "reason": "bounded self work",
		"work_units":       []map[string]any{{"id": "summary", "description": "write one bounded summary", "required_output": "summary"}},
		"required_outputs": []string{"summary"},
	})
	atomicPlanning := marshalPGClassifierPayload(t, map[string]any{
		"workflow_mode": "self", "current_agent_role": "lead", "task_type": "content", "current_agent_fit": "strong",
		"best_team_owner": "", "best_team_owner_role": "", "best_team_fit": "none", "specialist_match_found": false,
		"lead_selected_as_fallback": false, "routing_priority_used": "no_specialist", "owner_selection_reason": "current agent handles the bounded summary",
		"followup_context_used_for_reference_only": false, "workflow_executable": false, "decision": "self", "required_tool": "", "reason": "bounded self work",
	})
	atomicCritique := marshalPGClassifierPayload(t, map[string]any{"valid": true, "issues": []string{}})
	atomic := teamworkclassify.ClassifyWithLLM(ctx, input, &pgTeamWorkProvider{shape: atomicShape, assessment: atomicAssessment, planning: atomicPlanning, critique: atomicCritique}, "team-work-integration", nil)
	if atomic.Decision != teamworkclassify.DecisionSelf || atomic.DegradedWorkflow {
		t.Fatalf("atomic runtime result = %+v", atomic)
	}
	if atomic.EffectiveReviewRequired || atomic.VerifiedWorkShape != teamworkclassify.WorkShapeAtomic {
		t.Fatalf("atomic flow must verify an atomic no-review shape: %+v", atomic)
	}

	reviewedMessage := "score and recommend one option, then independently critique the choice"
	input.Message = reviewedMessage
	plan := &teamworkclassify.WorkflowPlan{
		SchemaVersion: teamworkclassify.WorkflowPlanSchemaVersion, Goal: "produce a reviewed recommendation",
		CoordinatorAgentID: leadID, CoordinatorAgentKey: leadKey,
		FinalOwnerAgentID: ownerID, FinalOwnerAgentKey: "runtime-owner", ReviewStatus: "included", TerminalStepID: "integrate",
		Steps: []teamworkclassify.WorkflowStep{
			{ID: "draft", Title: "Draft", Instruction: "Score and recommend", OwnerAgentID: ownerID, OwnerAgentKey: "runtime-owner", CapabilityKey: "content_lead", RequiredOutput: true},
			{ID: "review", Title: "Review", Instruction: "Critique independently", OwnerAgentID: reviewerID, OwnerAgentKey: "runtime-reviewer", CapabilityKey: "qa", DependsOn: []string{"draft"}, RequiredOutput: true},
			{ID: "integrate", Title: "Integrate", Instruction: "Integrate the critique", OwnerAgentID: ownerID, OwnerAgentKey: "runtime-owner", CapabilityKey: "content_lead", DependsOn: []string{"review"}, RequiredOutput: true, Terminal: true},
		},
	}
	// Under the narrowed shape policy score_or_rank/recommend_or_select are
	// descriptive and do NOT force review; only an explicit_critique (or
	// independent_verification) trait, with evidence literally present in the
	// request, forces the reviewed_decision multi_role chain.
	reviewedShape := marshalPGClassifierPayload(t, map[string]any{
		"work_shape": "reviewed_decision",
		"shape_traits": []map[string]any{
			{"type": "score_or_rank", "source": "current_request", "evidence": "score and recommend one option"},
			{"type": "explicit_critique", "source": "current_request", "evidence": "independently critique the choice"},
		},
		"independent_review_required": true,
	})
	reviewedPlanning := marshalPGClassifierPayload(t, map[string]any{
		"workflow_mode": "multi_role", "current_agent_role": "lead", "task_type": "analytics", "current_agent_fit": "partial",
		"best_team_owner": "runtime-owner", "best_team_owner_role": "content", "best_team_fit": "strong", "specialist_match_found": true,
		"lead_selected_as_fallback": false, "routing_priority_used": "role_task_match", "owner_selection_reason": "reviewed workflow",
		"followup_context_used_for_reference_only": false, "workflow_executable": true, "decision": "team", "required_tool": "team_tasks", "reason": "review required", "plan": plan,
	})
	reviewedAssessment := marshalPGClassifierPayload(t, map[string]any{
		"workflow_mode": "multi_role", "independent_review_required": true, "reason": "independent review is required",
		"work_units": []map[string]any{
			{"id": "draft", "description": "score and recommend an option", "required_output": "recommendation"},
			{"id": "review", "description": "independently critique the recommendation", "required_output": "critique"},
			{"id": "integrate", "description": "integrate the critique", "required_output": "reviewed recommendation"},
		},
		"dependencies":     []map[string]any{{"from": "draft", "to": "review"}, {"from": "review", "to": "integrate"}},
		"required_outputs": []string{"reviewed recommendation"},
	})
	critique := marshalPGClassifierPayload(t, map[string]any{"valid": true, "issues": []string{}})
	reviewed := teamworkclassify.ClassifyWithLLM(ctx, input, &pgTeamWorkProvider{shape: reviewedShape, assessment: reviewedAssessment, planning: reviewedPlanning, critique: critique}, "team-work-integration", nil)
	if reviewed.Decision != teamworkclassify.DecisionTeam || reviewed.DegradedWorkflow || reviewed.EffectiveWorkflowMode != teamworkclassify.WorkflowModeMultiRole || reviewed.Plan == nil {
		t.Fatalf("reviewed runtime result = %+v", reviewed)
	}
	if !reviewed.EffectiveReviewRequired || reviewed.VerifiedWorkShape != teamworkclassify.WorkShapeReviewedDecision {
		t.Fatalf("reviewed flow must verify an evidence-backed reviewed_decision shape: %+v", reviewed)
	}
}

func marshalPGClassifierPayload(t *testing.T, payload any) string {
	t.Helper()
	raw, err := json.Marshal(payload)
	if err != nil {
		t.Fatal(err)
	}
	return string(raw)
}

func TestPGTaskEventIdentityRejectsCrossTenantReplay(t *testing.T) {
	db := hooksTestDB(t)
	tenantA, agentA := seedTenantAndAgent(t, db)
	tenantB, agentB := seedTenantAndAgent(t, db)
	teamA, teamB := uuid.New(), uuid.New()
	for _, fixture := range []struct {
		teamID, leadID, tenantID uuid.UUID
		name                     string
	}{
		{teamID: teamA, leadID: agentA, tenantID: tenantA, name: "Event Team A"},
		{teamID: teamB, leadID: agentB, tenantID: tenantB, name: "Event Team B"},
	} {
		if _, err := db.Exec(`INSERT INTO agent_teams (id,name,lead_agent_id,status,settings,created_by,tenant_id)
			VALUES ($1,$2,$3,'active','{"version":2}','owner',$4)`, fixture.teamID, fixture.name, fixture.leadID, fixture.tenantID); err != nil {
			t.Fatal(err)
		}
	}
	t.Cleanup(func() {
		db.Exec("DELETE FROM agent_teams WHERE id IN ($1,$2)", teamA, teamB)
	})

	teamStore := NewPGTeamStore(db)
	taskA := &store.TeamTaskData{TeamID: teamA, Subject: "Task A", Status: store.TeamTaskStatusPending, OwnerAgentID: &agentA, Metadata: map[string]any{}}
	taskB := &store.TeamTaskData{TeamID: teamB, Subject: "Task B", Status: store.TeamTaskStatusPending, OwnerAgentID: &agentB, Metadata: map[string]any{}}
	if err := teamStore.CreateTask(store.WithTenantID(context.Background(), tenantA), taskA); err != nil {
		t.Fatal(err)
	}
	if err := teamStore.CreateTask(store.WithTenantID(context.Background(), tenantB), taskB); err != nil {
		t.Fatal(err)
	}

	eventID := uuid.New()
	claim := func(tenantID, taskID uuid.UUID, eventType string) store.TaskEventClaimResult {
		t.Helper()
		result, err := teamStore.ClaimTaskEvent(store.WithTenantID(context.Background(), tenantID), &store.TeamTaskEventData{
			ID: eventID, TaskID: taskID, EventType: eventType, ActorType: "system",
		})
		if err != nil {
			t.Fatal(err)
		}
		return result
	}
	if got := claim(tenantA, taskA.ID, "progress"); got != store.TaskEventClaimed {
		t.Fatalf("first claim = %q", got)
	}
	if got := claim(tenantA, taskA.ID, "progress"); got != store.TaskEventDuplicate {
		t.Fatalf("exact replay = %q", got)
	}
	if got := claim(tenantB, taskB.ID, "progress"); got != store.TaskEventConflict {
		t.Fatalf("cross-tenant replay = %q", got)
	}
	if got := claim(tenantA, taskA.ID, "commented"); got != store.TaskEventConflict {
		t.Fatalf("same-tenant type collision = %q", got)
	}
}

func TestPGWorkflowDispatchLimitSettlesWorkflowFailure(t *testing.T) {
	db := hooksTestDB(t)
	tenantID, leadID := seedTenantAndAgent(t, db)
	workerID, teamID := uuid.New(), uuid.New()
	if _, err := db.Exec(`INSERT INTO agents (id,tenant_id,agent_key,agent_type,status,provider,model,owner_id)
		VALUES ($1,$2,$3,'predefined','active','test','test-model','owner')`, workerID, tenantID, "workflow-worker-"+workerID.String()); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO agent_teams (id,name,lead_agent_id,status,settings,created_by,tenant_id)
		VALUES ($1,$2,$3,'active','{"version":2}','owner',$4)`, teamID, "Workflow Team", leadID, tenantID); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		db.Exec("DELETE FROM agent_teams WHERE id=$1", teamID)
		db.Exec("DELETE FROM agents WHERE id=$1", workerID)
	})

	ctx := store.WithTenantID(context.Background(), tenantID)
	teamStore := NewPGTeamStore(db)
	plan := []byte(`{"schema_version":1,"goal":"dispatch failure","steps":[]}`)
	workflow := &store.TeamWorkflowData{
		TeamID: teamID, Status: store.TeamWorkflowStatusRunning, CanonicalPlan: plan, SchemaVersion: 1,
		PlanHash: fmt.Sprintf("%x", sha256.Sum256(plan)), CoordinatorAgentID: leadID, CoordinatorAgentKey: "lead",
		OriginAgentID: leadID, OriginAgentKey: "lead", OriginRunID: "dispatch-limit-run",
		OriginSessionKey: "agent:lead:ws:direct:user", OriginChannel: "ws", OriginChatID: "user",
	}
	taskID := uuid.New()
	tasks := []store.TeamTaskData{{
		BaseModel: store.BaseModel{ID: taskID}, TeamID: teamID, Subject: "Exhausted step",
		Status: store.TeamTaskStatusPending, OwnerAgentID: &workerID, WorkflowStepID: "only",
		WorkflowKind: store.TeamWorkflowTaskKindWork, WorkflowTerminal: true,
		Metadata: map[string]any{"dispatch_count": float64(2)},
	}}
	if err := teamStore.CreateWorkflowWithTasks(ctx, workflow, tasks); err != nil {
		t.Fatal(err)
	}
	if _, err := teamStore.ClaimWorkflowTaskDispatch(ctx, taskID, teamID, time.Now().Add(-time.Second)); err != nil {
		t.Fatal(err)
	}
	task, err := teamStore.GetTask(ctx, taskID)
	if err != nil {
		t.Fatal(err)
	}
	if count, ok := task.Metadata["dispatch_count"].(float64); !ok || int(count) != 3 {
		t.Fatalf("durable dispatch_count=%v, want 3", task.Metadata["dispatch_count"])
	}
	if _, err := teamStore.RequeueExpiredWorkflowDispatches(ctx, time.Now()); err != nil {
		t.Fatal(err)
	}
	manager := tools.NewTeamToolManager(teamStore, nil, bus.New(), t.TempDir())
	manager.DispatchUnblockedTasks(ctx, teamID)

	failedTask, err := teamStore.GetTask(ctx, taskID)
	if err != nil || failedTask.Status != store.TeamTaskStatusFailed {
		t.Fatalf("task status=%v err=%v", failedTask.Status, err)
	}
	failedWorkflow, err := teamStore.GetWorkflow(ctx, workflow.ID)
	if err != nil || failedWorkflow.Status != store.TeamWorkflowStatusFailing {
		t.Fatalf("workflow status=%v err=%v", failedWorkflow.Status, err)
	}
}

func TestPGTaskStoresRejectCanonicalOwnerMismatchBeforeMutation(t *testing.T) {
	teamStore := NewPGTeamStore(nil)
	expectedOwner, attemptedOwner := uuid.New(), uuid.New()
	ctx := store.WithExpectedTaskOwnerID(context.Background(), expectedOwner)
	if err := teamStore.CreateTask(ctx, &store.TeamTaskData{OwnerAgentID: &attemptedOwner}); err == nil {
		t.Fatal("CreateTask accepted an owner that differs from the canonical store constraint")
	}
	workflow := &store.TeamWorkflowData{CoordinatorAgentID: expectedOwner}
	if err := teamStore.CreatePendingWorkflowRequest(context.Background(), workflow, &store.TeamTaskData{OwnerAgentID: &attemptedOwner}); err == nil {
		t.Fatal("CreatePendingWorkflowRequest accepted an audit owner that differs from coordinator")
	}
}

// TestPGTaskReadPopulatesEventBuilderFields is the PG twin of Item 1 case (7):
// a task read must carry the owner agent_key (resolved via the agents JOIN,
// never a UUID), workflow ID, workflow step ID, and plan revision — the exact
// fields BuildTeamTaskEventPayload derives from the committed row. The
// authoritative event payload is only correct if the read populates these.
func TestPGTaskReadPopulatesEventBuilderFields(t *testing.T) {
	teamStore, ctx, leadID, workerID, teamID := pgRecoveryFixture(t)
	wantOwnerKey := "recovery-worker-" + workerID.String()
	rootID := uuid.New()
	workflow := pgMakeRunningWorkflow(t, teamStore, ctx, teamID, leadID, []store.TeamTaskData{
		{BaseModel: store.BaseModel{ID: rootID}, TeamID: teamID, Subject: "Root", Status: store.TeamTaskStatusPending, OwnerAgentID: &workerID, WorkflowStepID: "root", WorkflowKind: store.TeamWorkflowTaskKindWork, WorkflowTerminal: true},
	})

	task, err := teamStore.GetTask(ctx, rootID)
	if err != nil {
		t.Fatalf("GetTask: %v", err)
	}
	if task.OwnerAgentKey != wantOwnerKey {
		t.Fatalf("owner agent_key = %q, want %q (agents JOIN)", task.OwnerAgentKey, wantOwnerKey)
	}
	if task.WorkflowID == nil || *task.WorkflowID != workflow.ID {
		t.Fatalf("workflow ID = %v, want %s", task.WorkflowID, workflow.ID)
	}
	if task.WorkflowStepID != "root" || task.PlanRevision != 1 {
		t.Fatalf("workflow step/revision = %q/%d, want root/1", task.WorkflowStepID, task.PlanRevision)
	}

	payload := tools.BuildTeamTaskEventPayload(task, "Worker Display", tools.TeamTaskEventOptions{ActorType: "system"})
	if payload.OwnerAgentKey != wantOwnerKey || payload.OwnerDisplayName != "Worker Display" {
		t.Fatalf("payload owner = %q/%q, want %q/Worker Display", payload.OwnerAgentKey, payload.OwnerDisplayName, wantOwnerKey)
	}
	if payload.WorkflowID != workflow.ID.String() || payload.WorkflowStepID != "root" || payload.PlanRevision != 1 {
		t.Fatalf("payload workflow fields = %q/%q/%d", payload.WorkflowID, payload.WorkflowStepID, payload.PlanRevision)
	}
	if payload.TeamID != teamID.String() || payload.TaskID != rootID.String() || payload.Status != store.TeamTaskStatusPending {
		t.Fatalf("payload identity = %+v", payload)
	}
}
