package tools

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/nextlevelbuilder/goclaw/internal/bus"
	"github.com/nextlevelbuilder/goclaw/internal/store"
)

func TestDispatchTaskToAgentUsesRequesterAsFromAgentNotTeamLead(t *testing.T) {
	teamID := uuid.MustParse("10000000-0000-0000-0000-000000000001")
	leadID := uuid.MustParse("10000000-0000-0000-0000-000000000002")
	requesterID := uuid.MustParse("10000000-0000-0000-0000-000000000003")
	ownerID := uuid.MustParse("10000000-0000-0000-0000-000000000004")
	taskID := uuid.MustParse("10000000-0000-0000-0000-000000000005")

	team := &store.TeamData{
		BaseModel:   store.BaseModel{ID: teamID},
		Name:        "Marketing Team",
		LeadAgentID: leadID,
		Status:      store.TeamStatusActive,
	}
	members := []store.TeamMemberData{
		{TeamID: teamID, AgentID: leadID, Role: store.TeamRoleLead, AgentKey: "bao-an"},
		{TeamID: teamID, AgentID: requesterID, Role: store.TeamRoleMember, AgentKey: "khanh-developer"},
		{TeamID: teamID, AgentID: ownerID, Role: store.TeamRoleMember, AgentKey: "minh-strategy"},
	}
	teamStore := newMockTaskStore(team, members)
	agents := map[uuid.UUID]*store.AgentData{
		leadID: {
			BaseModel:   store.BaseModel{ID: leadID},
			AgentKey:    "bao-an",
			DisplayName: "Bao An",
		},
		requesterID: {
			BaseModel:   store.BaseModel{ID: requesterID},
			AgentKey:    "khanh-developer",
			DisplayName: "Bao Khanh",
		},
		ownerID: {
			BaseModel:   store.BaseModel{ID: ownerID},
			AgentKey:    "minh-strategy",
			DisplayName: "Huy Minh",
		},
	}
	agentStore := &stubAgentStore{agentsByID: agents, agentsByKey: map[string]*store.AgentData{
		"bao-an":          agents[leadID],
		"khanh-developer": agents[requesterID],
		"minh-strategy":   agents[ownerID],
	}}
	msgBus := bus.New()
	manager := NewTeamToolManager(teamStore, agentStore, msgBus, t.TempDir())
	task := &store.TeamTaskData{
		BaseModel:        store.BaseModel{ID: taskID},
		TeamID:           teamID,
		Subject:          "Research USD exchange rate",
		Status:           store.TeamTaskStatusInProgress,
		OwnerAgentID:     &ownerID,
		CreatedByAgentID: &requesterID,
		Metadata:         map[string]any{},
		TaskNumber:       1,
		Channel:          ChannelDashboard,
		ChatID:           "chat-1",
	}

	manager.dispatchTaskToAgent(t.Context(), task, team, ownerID)

	msg, ok := msgBus.ConsumeInbound(t.Context())
	if !ok {
		t.Fatal("expected dispatch inbound message")
	}
	if got := msg.Metadata[MetaFromAgent]; got != "khanh-developer" {
		t.Fatalf("expected from_agent requester khanh-developer, got %q", got)
	}
	if got := msg.Metadata[MetaToAgent]; got != "minh-strategy" {
		t.Fatalf("expected to_agent minh-strategy, got %q", got)
	}
	if got := msg.Metadata[MetaLeaderAgentID]; got != leadID.String() {
		t.Fatalf("expected leader_agent_id %s, got %q", leadID, got)
	}
}

func TestBuildWorkflowTasksPersistsEdgeResultsAndSharedWorkspace(t *testing.T) {
	teamID, workflowID := uuid.New(), uuid.New()
	ownerID := uuid.New()
	workflow := &store.TeamWorkflowData{BaseModel: store.BaseModel{ID: workflowID}, TeamID: teamID, OriginSessionKey: "session", OriginRouting: []byte(`{"local_key":"topic"}`)}
	constraint := &TeamWorkPlanConstraint{Steps: []TeamWorkPlanStepConstraint{
		{ID: "draft", Title: "Draft", Instruction: "draft", OwnerAgentID: ownerID},
		{ID: "review", Title: "Review", Instruction: "review", OwnerAgentID: ownerID, DependsOn: []string{"draft"}, Terminal: true},
	}}
	tasks, err := buildWorkflowTasks(constraint, workflow)
	if err != nil {
		t.Fatal(err)
	}
	if len(tasks[1].BlockedBy) != 1 {
		t.Fatalf("blocked_by=%v", tasks[1].BlockedBy)
	}
	raw, ok := tasks[1].Metadata["original_blocked_by"].([]string)
	if !ok || len(raw) != 1 || raw[0] != tasks[0].ID.String() {
		t.Fatalf("original_blocked_by=%#v", tasks[1].Metadata["original_blocked_by"])
	}
	source := &store.TeamTaskData{Metadata: map[string]any{TaskMetaTeamWorkspace: "/shared/team"}}
	InheritWorkflowTaskContext(tasks, source)
	for _, task := range tasks {
		if task.Metadata[TaskMetaTeamWorkspace] != "/shared/team" {
			t.Fatalf("task %s workspace=%v", task.WorkflowStepID, task.Metadata[TaskMetaTeamWorkspace])
		}
	}
}

func TestQueueWorkflowRootsDefersUntilPostTurn(t *testing.T) {
	teamID := uuid.New()
	rootID := uuid.New()
	ptd := NewPendingTeamDispatch()
	ctx := WithPendingTeamDispatch(context.Background(), ptd)
	queueWorkflowRoots(ctx, teamID, []store.TeamTaskData{{
		BaseModel: store.BaseModel{ID: rootID}, Status: store.TeamTaskStatusPending,
		WorkflowKind: store.TeamWorkflowTaskKindWork,
	}})
	drained := ptd.Drain()
	if len(drained[teamID]) != 1 || drained[teamID][0] != rootID {
		t.Fatalf("queued roots=%v", drained)
	}
}

func TestBuildWorkflowStepDescriptionCarriesGoalAndUpstream(t *testing.T) {
	minh, ly := uuid.New(), uuid.New()
	constraint := &TeamWorkPlanConstraint{
		Goal: "Produce the integrated pitch document set for next week",
		Steps: []TeamWorkPlanStepConstraint{
			{ID: "market", Title: "Market Analysis", Instruction: "analyse the market", OwnerAgentID: minh, OwnerAgentKey: "minh-strategy"},
			{ID: "deck", Title: "Slide Content", Instruction: "write the slides", OwnerAgentID: ly, OwnerAgentKey: "ly-content",
				DependsOn: []string{"market"}, RequiredTools: []string{"write_file"}, Terminal: true},
		},
	}

	// A leaf step still needs the goal: without it the owner cannot tell which
	// product the analysis is for and blocks to ask the coordinator.
	leaf := buildWorkflowStepDescription(constraint, constraint.Steps[0])
	for _, want := range []string{"Produce the integrated pitch document set", "[Your step]", "analyse the market"} {
		if !strings.Contains(leaf, want) {
			t.Fatalf("leaf description missing %q:\n%s", want, leaf)
		}
	}
	if strings.Contains(leaf, "builds on") {
		t.Fatalf("leaf step must not claim upstream work:\n%s", leaf)
	}

	terminal := buildWorkflowStepDescription(constraint, constraint.Steps[1])
	for _, want := range []string{
		"Produce the integrated pitch document set",
		"write the slides",
		"builds on: Market Analysis (minh-strategy)",
		"final integration step",
		"Required tools: write_file",
	} {
		if !strings.Contains(terminal, want) {
			t.Fatalf("terminal description missing %q:\n%s", want, terminal)
		}
	}
}

func TestBuildWorkflowStepDescriptionWithoutGoalKeepsInstructionFirst(t *testing.T) {
	// Schema-v1 plans and hand-built constraints may carry no goal; the
	// instruction must still be the description, with no empty header.
	constraint := &TeamWorkPlanConstraint{Steps: []TeamWorkPlanStepConstraint{
		{ID: "solo", Title: "Solo", Instruction: "do the work", OwnerAgentID: uuid.New()},
	}}
	got := buildWorkflowStepDescription(constraint, constraint.Steps[0])
	if got != "do the work" {
		t.Fatalf("description=%q", got)
	}
}

func TestBuildWorkflowTasksAppliesStepDescription(t *testing.T) {
	owner := uuid.New()
	workflow := &store.TeamWorkflowData{BaseModel: store.BaseModel{ID: uuid.New()}, TeamID: uuid.New()}
	constraint := &TeamWorkPlanConstraint{Goal: "ship the thing", Steps: []TeamWorkPlanStepConstraint{
		{ID: "a", Title: "A", Instruction: "step a", OwnerAgentID: owner, OwnerAgentKey: "khanh-developer"},
		{ID: "b", Title: "B", Instruction: "step b", OwnerAgentID: owner, OwnerAgentKey: "khanh-developer", DependsOn: []string{"a"}, Terminal: true},
	}}
	tasks, err := buildWorkflowTasks(constraint, workflow)
	if err != nil {
		t.Fatal(err)
	}
	for _, task := range tasks {
		if !strings.Contains(task.Description, "ship the thing") {
			t.Fatalf("step %s lost the workflow goal:\n%s", task.WorkflowStepID, task.Description)
		}
	}
	if !strings.Contains(tasks[1].Description, "builds on: A (khanh-developer)") {
		t.Fatalf("dependent step lost upstream context:\n%s", tasks[1].Description)
	}
}

// dispatchOnce wires a minimal team + agent store and returns the inbound
// content a single dispatch would send to the owner.
func dispatchOnce(t *testing.T, mutate func(*store.TeamTaskData)) string {
	t.Helper()
	teamID := uuid.New()
	leadID, ownerID, taskID := uuid.New(), uuid.New(), uuid.New()
	team := &store.TeamData{BaseModel: store.BaseModel{ID: teamID}, Name: "Marketing Team", LeadAgentID: leadID, Status: store.TeamStatusActive}
	members := []store.TeamMemberData{
		{TeamID: teamID, AgentID: leadID, Role: store.TeamRoleLead, AgentKey: "bao-an"},
		{TeamID: teamID, AgentID: ownerID, Role: store.TeamRoleMember, AgentKey: "minh-strategy"},
	}
	agents := map[uuid.UUID]*store.AgentData{
		leadID:  {BaseModel: store.BaseModel{ID: leadID}, AgentKey: "bao-an", DisplayName: "Bao An"},
		ownerID: {BaseModel: store.BaseModel{ID: ownerID}, AgentKey: "minh-strategy", DisplayName: "Huy Minh"},
	}
	agentStore := &stubAgentStore{agentsByID: agents, agentsByKey: map[string]*store.AgentData{
		"bao-an": agents[leadID], "minh-strategy": agents[ownerID],
	}}
	msgBus := bus.New()
	manager := NewTeamToolManager(newMockTaskStore(team, members), agentStore, msgBus, t.TempDir())
	task := &store.TeamTaskData{
		BaseModel: store.BaseModel{ID: taskID}, TeamID: teamID, Subject: "Market analysis",
		Description: "analyse the market", Status: store.TeamTaskStatusInProgress,
		OwnerAgentID: &ownerID, Metadata: map[string]any{}, TaskNumber: 7,
		Channel: ChannelDashboard, ChatID: "chat-1",
	}
	mutate(task)
	manager.dispatchTaskToAgent(t.Context(), task, team, ownerID)
	msg, ok := msgBus.ConsumeInbound(t.Context())
	if !ok {
		t.Fatal("expected dispatch inbound message")
	}
	return msg.Content
}

func TestDispatchWorkflowStepDemandsSubstantiveCompletion(t *testing.T) {
	// Observed live: agents ended a step turn with a bare "..." and no complete
	// call, so the step settled carrying "..." as the deliverable the critic and
	// the terminal integrator then consumed (workflows 019f9efa, 019f9f21).
	workflowID := uuid.New()
	content := dispatchOnce(t, func(task *store.TeamTaskData) {
		task.WorkflowID = &workflowID
		task.WorkflowKind = store.TeamWorkflowTaskKindWork
		task.WorkflowStepID = "market-research"
	})
	for _, want := range []string{
		"[This is a workflow step — mandatory]",
		"MUST finish by calling team_tasks(action=\"complete\"",
		"only thing the next steps and the reviewer receive",
		"is not a deliverable",
		"type=\"blocker\"",
	} {
		if !strings.Contains(content, want) {
			t.Fatalf("workflow-step dispatch missing %q:\n%s", want, content)
		}
	}
}

func TestDispatchPlainTaskKeepsInstructionsUnchanged(t *testing.T) {
	// The mandatory block is scoped to workflow work steps: ordinary delegated
	// tasks keep their existing, lighter instruction set.
	content := dispatchOnce(t, func(*store.TeamTaskData) {})
	if strings.Contains(content, "[This is a workflow step") {
		t.Fatalf("plain task must not get the workflow-step block:\n%s", content)
	}
	if !strings.Contains(content, "When done: team_tasks(action=\"complete\"") {
		t.Fatalf("plain task lost its baseline instructions:\n%s", content)
	}
}

func TestDispatchWorkflowAuditTaskSkipsStepBlock(t *testing.T) {
	// The audit/request task is not an executable step; it must not be told its
	// result feeds a reviewer.
	workflowID := uuid.New()
	content := dispatchOnce(t, func(task *store.TeamTaskData) {
		task.WorkflowID = &workflowID
		task.WorkflowKind = store.TeamWorkflowTaskKindAudit
	})
	if strings.Contains(content, "[This is a workflow step") {
		t.Fatalf("audit task must not get the workflow-step block:\n%s", content)
	}
}

// dispatchLeadOwnedOnce wires a minimal team whose owner IS the lead, runs a
// single dispatchTaskToAgent against the lead, and reports whether the inbound
// dispatch message was emitted (allowed) or suppressed (auto-failed). The task
// is mutated by the caller to set the workflow kind/terminal flags.
func dispatchLeadOwnedOnce(t *testing.T, mutate func(*store.TeamTaskData)) (dispatched bool, failedResult string) {
	t.Helper()
	teamID := uuid.New()
	leadID := uuid.New()
	taskID := uuid.New()
	team := &store.TeamData{BaseModel: store.BaseModel{ID: teamID}, Name: "Marketing Team", LeadAgentID: leadID, Status: store.TeamStatusActive}
	members := []store.TeamMemberData{
		{TeamID: teamID, AgentID: leadID, Role: store.TeamRoleLead, AgentKey: "bao-an"},
	}
	agents := map[uuid.UUID]*store.AgentData{
		leadID: {BaseModel: store.BaseModel{ID: leadID}, AgentKey: "bao-an", DisplayName: "Bao An"},
	}
	agentStore := &stubAgentStore{agentsByID: agents, agentsByKey: map[string]*store.AgentData{
		"bao-an": agents[leadID],
	}}
	msgBus := bus.New()
	teamStore := newMockTaskStore(team, members)
	manager := NewTeamToolManager(teamStore, agentStore, msgBus, t.TempDir())
	task := &store.TeamTaskData{
		BaseModel: store.BaseModel{ID: taskID}, TeamID: teamID, Subject: "Integrate results",
		Description: "synthesise the final answer", Status: store.TeamTaskStatusDispatching,
		OwnerAgentID: &leadID, Metadata: map[string]any{}, TaskNumber: 9,
		Channel: ChannelDashboard, ChatID: "chat-1",
	}
	mutate(task)
	manager.dispatchTaskToAgent(t.Context(), task, team, leadID)

	// dispatchTaskToAgent is synchronous: PublishInbound (when the guard allows)
	// happens inline before the call returns, so after it returns the inbound
	// channel either has the dispatch message or never will (the guard
	// auto-failed). A blocking ConsumeInbound would hang forever on the rejected
	// path — wait briefly with a short timeout and treat no message as rejected.
	probeCtx, cancel := context.WithTimeout(t.Context(), 200*time.Millisecond)
	defer cancel()
	_, dispatched = msgBus.ConsumeInbound(probeCtx)
	if dispatched {
		return true, ""
	}
	// Not dispatched → the lead-guard auto-failed it. The mock store's plain
	// FailTask path (non-workflow) stamps "FAILED: <reason>"; the workflow path
	// settles via the workflow store which the mock does not implement, so for
	// workflow tasks the failure surfaces as the task being left undelivered
	// with no inbound message. We assert the guard blocked it by the absence of
	// a dispatch and, for plain tasks, the FAILED result stamp.
	if r := taskResult(teamStore, taskID); r != "" {
		return false, r
	}
	return false, ""
}

func taskResult(s *mockTaskStore, taskID uuid.UUID) string {
	s.mu.Lock()
	defer s.mu.Unlock()
	if t, ok := s.tasks[taskID]; ok && t.Result != nil {
		return *t.Result
	}
	return ""
}

// TestDispatchLeadOwnedTerminalWorkflowTaskAllowed proves the G4 dispatch-time
// fix: a terminal workflow-work task owned by the lead is the integration step
// of a coordinator DAG (create_dag), and dispatchTaskToAgent MUST let it
// through to the lead rather than auto-fail it. Before the fix the hard
// `agentID == team.LeadAgentID` guard fired unconditionally and collapsed the
// whole DAG with zero requester delivery (live smoke: terminal task 019fb768
// failed with "Cannot dispatch task to the team lead").
func TestDispatchLeadOwnedTerminalWorkflowTaskAllowed(t *testing.T) {
	workflowID := uuid.New()
	dispatched, _ := dispatchLeadOwnedOnce(t, func(task *store.TeamTaskData) {
		task.WorkflowID = &workflowID
		task.WorkflowKind = store.TeamWorkflowTaskKindWork
		task.WorkflowStepID = "final"
		task.WorkflowTerminal = true
	})
	if !dispatched {
		t.Fatal("terminal workflow-work task owned by the lead MUST dispatch (create_dag integration step); guard auto-failed it")
	}
}

// TestDispatchLeadOwnedNonTerminalWorkflowTaskRejected proves the lead guard
// still auto-fails every other lead-owned task — non-terminal workflow work and
// plain tasks — so the lead cannot self-dispatch ordinary or looping work. The
// terminal exception is the ONLY hole in the guard.
func TestDispatchLeadOwnedNonTerminalWorkflowTaskRejected(t *testing.T) {
	workflowID := uuid.New()
	dispatched, _ := dispatchLeadOwnedOnce(t, func(task *store.TeamTaskData) {
		task.WorkflowID = &workflowID
		task.WorkflowKind = store.TeamWorkflowTaskKindWork
		task.WorkflowStepID = "research"
		task.WorkflowTerminal = false
	})
	if dispatched {
		t.Fatal("non-terminal workflow-work task owned by the lead MUST be auto-failed (only terminal integration may run as the lead)")
	}
}

// TestDispatchLeadOwnedPlainTaskRejected proves a plain (non-workflow) task
// owned by the lead is still auto-failed — the terminal exception is scoped to
// workflow work only. The robust observable is the absence of an inbound
// dispatch message: the guard fires (logged "blocked dispatch to lead agent")
// and never publishes to the lead, so no dispatch occurs.
func TestDispatchLeadOwnedPlainTaskRejected(t *testing.T) {
	dispatched, _ := dispatchLeadOwnedOnce(t, func(*store.TeamTaskData) {})
	if dispatched {
		t.Fatal("plain task owned by the lead MUST be auto-failed (no dispatch); the terminal exception is workflow-work only")
	}
}
