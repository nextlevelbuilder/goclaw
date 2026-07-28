package teamworkclassify

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/google/uuid"
)

func plannerTestInput() Input {
	lead := structuredProfile("team_member", "Team Lead", "team-lead", "lead", "coordinates workflows", CapabilityLeadCoordinator)
	owner := structuredProfile("team_member", "Content Owner", "content-owner", "member", "owns content output", CapabilityContentLead)
	reviewer := structuredProfile("team_member", "QA Reviewer", "qa-reviewer", "reviewer", "independent quality review", CapabilityQA)
	lead.AvailableToolsStatus = DataStatusKnown
	lead.AvailableTools = []string{"team_tasks"}
	owner.AvailableToolsStatus = DataStatusKnown
	owner.AvailableTools = []string{"read_file", "write_file"}
	reviewer.AvailableToolsStatus = DataStatusKnown
	reviewer.AvailableTools = []string{"read_file"}
	return Input{
		Mode:                       ModeTeam,
		Message:                    "draft, independently review, and integrate the final content",
		CurrentAgent:               lead,
		Team:                       Profile{Kind: "team", Name: "Editorial", Text: "multi-role editorial team"},
		Members:                    []Profile{lead, owner, reviewer},
		TeamRole:                   "lead",
		CanAssignTeamTasks:         true,
		CoordinatorAgentID:         lead.AgentID,
		CoordinatorAgentKey:        lead.AgentKey,
		MemberRequestsEnabled:      true,
		MemberRequestsAutoDispatch: true,
	}
}

func validPlannerTestPlan(input Input) *WorkflowPlan {
	owner := input.Members[1]
	reviewer := input.Members[2]
	return &WorkflowPlan{
		SchemaVersion:       WorkflowPlanSchemaVersion,
		Goal:                "produce reviewed final content",
		CoordinatorAgentID:  input.CoordinatorAgentID,
		CoordinatorAgentKey: input.CoordinatorAgentKey,
		FinalOwnerAgentID:   owner.AgentID,
		FinalOwnerAgentKey:  owner.AgentKey,
		ReviewStatus:        "included",
		TerminalStepID:      "integrate",
		Steps: []WorkflowStep{
			{ID: "draft", Title: "Draft", Instruction: "Produce the first draft", OwnerAgentID: owner.AgentID, OwnerAgentKey: owner.AgentKey, CapabilityKey: string(CapabilityContentLead), RequiredTools: []string{"write_file"}, RequiredOutput: true},
			{ID: "review", Title: "Review", Instruction: "Review the draft independently", OwnerAgentID: reviewer.AgentID, OwnerAgentKey: reviewer.AgentKey, CapabilityKey: string(CapabilityQA), RequiredTools: []string{"read_file"}, DependsOn: []string{"draft"}, RequiredOutput: true},
			{ID: "integrate", Title: "Integrate", Instruction: "Integrate review into final output", OwnerAgentID: owner.AgentID, OwnerAgentKey: owner.AgentKey, CapabilityKey: string(CapabilityContentLead), RequiredTools: []string{"read_file", "write_file"}, DependsOn: []string{"review"}, RequiredOutput: true, Terminal: true},
		},
	}
}

func multiRoleResult(input Input, plan *WorkflowPlan) Result {
	return Result{
		Decision:                DecisionTeam,
		WorkflowMode:            WorkflowModeMultiRole,
		RequiredTool:            "team_tasks",
		WorkflowExecutable:      true,
		EffectiveReviewRequired: true,
		BestTeamFit:             "strong",
		Plan:                    plan,
	}
}

func multiRoleJSON(t *testing.T, input Input, plan *WorkflowPlan) string {
	t.Helper()
	payload := map[string]any{
		"workflow_mode": "multi_role", "current_agent_role": "lead", "task_type": "content",
		"current_agent_fit": "partial", "best_team_owner": input.Members[1].AgentKey,
		"best_team_owner_role": "content", "best_team_fit": "strong", "specialist_match_found": true,
		"lead_selected_as_fallback": false, "routing_priority_used": "role_task_match",
		"owner_selection_reason":                   "multiple roles and independent review are required",
		"followup_context_used_for_reference_only": true, "workflow_executable": true,
		"decision": "team", "required_tool": "team_tasks", "reason": "validated multi-role workflow", "plan": plan,
	}
	raw, err := json.Marshal(payload)
	if err != nil {
		t.Fatal(err)
	}
	return string(raw)
}

func TestValidatePlannerResultAcceptsCanonicalConvergentPlan(t *testing.T) {
	input := plannerTestInput()
	result, err := ValidatePlannerResult(input, multiRoleResult(input, validPlannerTestPlan(input)))
	if err != nil {
		t.Fatalf("ValidatePlannerResult error = %v", err)
	}
	if result.BestTeamOwnerID != input.Members[1].AgentID || result.Plan.TerminalStepID != "integrate" {
		t.Fatalf("validated result = %+v", result)
	}
}

func TestValidatePlannerResultDerivesIncludedReviewStatus(t *testing.T) {
	input := plannerTestInput()
	plan := validPlannerTestPlan(input)
	plan.ReviewStatus = "required"
	result, err := ValidatePlannerResult(input, multiRoleResult(input, plan))
	if err != nil {
		t.Fatal(err)
	}
	if result.Plan.ReviewStatus != "included" {
		t.Fatalf("review status = %q", result.Plan.ReviewStatus)
	}
}

func TestBuildPlanningMessagesIncludesCanonicalReviewerCandidates(t *testing.T) {
	input := plannerTestInput()
	messages := BuildPlanningMessages(input, Evidence{}, WorkAssessment{WorkflowMode: WorkflowModeMultiRole, IndependentReviewRequired: true}, true)
	var payload struct {
		ReviewerCandidates []map[string]string `json:"reviewer_candidates"`
	}
	if err := json.Unmarshal([]byte(messages[1].Content), &payload); err != nil {
		t.Fatal(err)
	}
	if len(payload.ReviewerCandidates) != 2 || payload.ReviewerCandidates[1]["agent_id"] != input.Members[2].AgentID.String() {
		t.Fatalf("reviewer candidates = %+v", payload.ReviewerCandidates)
	}
}

func TestValidateAssessedResultAcceptsExplicitStaffingGapAsDegradedSelf(t *testing.T) {
	input := plannerTestInput()
	result, err := validateAssessedResult(input, Evidence{}, ShapeAssessment{WorkShape: WorkShapeReviewedDecision, IndependentReviewRequired: true}, Result{
		Decision: DecisionSelf, WorkflowMode: WorkflowModeSelf, WorkflowExecutable: false,
		StaffingGaps: []string{"review: no distinct suitable reviewer"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if !result.DegradedWorkflow || result.DegradedReasonCode != "insufficient_canonical_members" || result.Decision != DecisionSelf {
		t.Fatalf("staffing-gap result = %+v", result)
	}
}

func TestValidateWorkflowRoleRejectsTerminalCritic(t *testing.T) {
	step := WorkflowStep{ID: "review", WorkflowRole: "critic", Terminal: true}
	if err := validateWorkflowRole(&step); err == nil {
		t.Fatal("terminal critic role was accepted")
	}
}

func TestValidatePlannerResultDoesNotRequireCriticForNonReviewWorkflow(t *testing.T) {
	input := plannerTestInput()
	technical := structuredProfile("team_member", "Technical Specialist", "technical-specialist", "member", "implements a bounded dependency", CapabilityTechnical)
	technical.AvailableToolsStatus = DataStatusKnown
	input.Members = append(input.Members, technical)
	owner := input.Members[1]
	plan := &WorkflowPlan{
		SchemaVersion: WorkflowPlanSchemaVersion, Goal: "complete a staged implementation",
		CoordinatorAgentID: input.CoordinatorAgentID, CoordinatorAgentKey: input.CoordinatorAgentKey,
		FinalOwnerAgentID: owner.AgentID, FinalOwnerAgentKey: owner.AgentKey,
		ReviewStatus: "none", TerminalStepID: "integrate",
		Steps: []WorkflowStep{
			{ID: "draft", Title: "Draft", Instruction: "Prepare the bounded input", OwnerAgentID: owner.AgentID, OwnerAgentKey: owner.AgentKey, CapabilityKey: string(CapabilityContentLead), RequiredOutput: true},
			{ID: "implement", Title: "Implement", Instruction: "Implement the dependent part", OwnerAgentID: technical.AgentID, OwnerAgentKey: technical.AgentKey, CapabilityKey: string(CapabilityTechnical), DependsOn: []string{"draft"}, RequiredOutput: true},
			{ID: "integrate", Title: "Integrate", Instruction: "Integrate the implementation", OwnerAgentID: owner.AgentID, OwnerAgentKey: owner.AgentKey, CapabilityKey: string(CapabilityContentLead), DependsOn: []string{"implement"}, RequiredOutput: true, Terminal: true},
		},
	}
	result := multiRoleResult(input, plan)
	result.EffectiveReviewRequired = false
	if _, err := ValidatePlannerResult(input, result); err != nil {
		t.Fatalf("non-review staged workflow was rejected: %v", err)
	}
}

func TestValidatePlannerResultRejectsNonReviewerAsIndependentCritic(t *testing.T) {
	input := plannerTestInput()
	technical := structuredProfile("team_member", "Technical Specialist", "technical-specialist", "member", "implements a bounded dependency", CapabilityTechnical)
	technical.AvailableToolsStatus = DataStatusKnown
	input.Members = append(input.Members, technical)
	plan := validPlannerTestPlan(input)
	plan.Steps[1].OwnerAgentID = technical.AgentID
	plan.Steps[1].OwnerAgentKey = technical.AgentKey
	plan.Steps[1].CapabilityKey = string(CapabilityTechnical)
	plan.Steps[1].RequiredTools = nil
	if _, err := ValidatePlannerResult(input, multiRoleResult(input, plan)); err == nil {
		t.Fatal("technical collaborator without qa/analytics_critic capability was accepted as critic")
	}
}

func TestValidatePlannerResultFindsReviewerAfterUnrelatedStep(t *testing.T) {
	input := plannerTestInput()
	technical := structuredProfile("team_member", "Technical Specialist", "technical-specialist", "member", "collects supporting data", CapabilityTechnical)
	technical.AvailableToolsStatus = DataStatusKnown
	input.Members = append(input.Members, technical)
	plan := validPlannerTestPlan(input)
	plan.Steps = append([]WorkflowStep{
		{ID: "research", Title: "Research", Instruction: "Collect supporting data", OwnerAgentID: technical.AgentID, OwnerAgentKey: technical.AgentKey, CapabilityKey: string(CapabilityTechnical), RequiredOutput: true},
	}, plan.Steps...)
	plan.Steps[1].DependsOn = []string{"research"}
	if _, err := ValidatePlannerResult(input, multiRoleResult(input, plan)); err != nil {
		t.Fatalf("valid reviewer after unrelated step was rejected: %v", err)
	}
}

func TestBuildPlanConstraintCanonicalizesTextAndStepOrder(t *testing.T) {
	input := plannerTestInput()
	first := validPlannerTestPlan(input)
	second := validPlannerTestPlan(input)
	second.Goal = "  produce reviewed final content\r\n"
	second.Steps[0], second.Steps[2] = second.Steps[2], second.Steps[0]
	second.Steps[0].RequiredTools = []string{"write_file", "read_file", "read_file"}
	firstConstraint, err := BuildPlanConstraint(first)
	if err != nil {
		t.Fatal(err)
	}
	secondConstraint, err := BuildPlanConstraint(second)
	if err != nil {
		t.Fatal(err)
	}
	if firstConstraint.PlanHash != secondConstraint.PlanHash {
		t.Fatalf("canonical hashes differ: %s != %s", firstConstraint.PlanHash, secondConstraint.PlanHash)
	}
}

func TestValidatePlannerResultRejectsLeadOwnedWorkStep(t *testing.T) {
	input := plannerTestInput()
	plan := validPlannerTestPlan(input)
	plan.Steps[0].OwnerAgentID = input.CoordinatorAgentID
	plan.Steps[0].OwnerAgentKey = input.CoordinatorAgentKey
	if _, err := ValidatePlannerResult(input, multiRoleResult(input, plan)); err == nil || !strings.Contains(err.Error(), "team lead coordinator") {
		t.Fatalf("lead-owned step validation error = %v", err)
	}
}

func TestValidatePlannerResultRejectsOrphanBranch(t *testing.T) {
	input := plannerTestInput()
	plan := validPlannerTestPlan(input)
	owner := input.Members[1]
	plan.Steps = append(plan.Steps, WorkflowStep{ID: "orphan", Title: "Orphan", Instruction: "Unused output", OwnerAgentID: owner.AgentID, OwnerAgentKey: owner.AgentKey, CapabilityKey: string(CapabilityContentLead), RequiredOutput: true})
	if _, err := ValidatePlannerResult(input, multiRoleResult(input, plan)); err == nil || !strings.Contains(err.Error(), "does not converge") {
		t.Fatalf("orphan validation error = %v", err)
	}
}

func TestValidatePlannerResultRejectsCycleAndStepLimit(t *testing.T) {
	input := plannerTestInput()
	cycle := validPlannerTestPlan(input)
	cycle.Steps[0].DependsOn = []string{"integrate"}
	if _, err := ValidatePlannerResult(input, multiRoleResult(input, cycle)); err == nil || !strings.Contains(err.Error(), "cycle") {
		t.Fatalf("cycle validation error = %v", err)
	}

	tooLarge := validPlannerTestPlan(input)
	owner := input.Members[1]
	for len(tooLarge.Steps) <= MaxWorkflowSteps {
		id := "extra-" + uuid.NewString()
		tooLarge.Steps = append(tooLarge.Steps, WorkflowStep{ID: id, Title: "Extra", Instruction: "Extra required output", OwnerAgentID: owner.AgentID, OwnerAgentKey: owner.AgentKey, CapabilityKey: string(CapabilityContentLead), DependsOn: []string{"draft"}, RequiredOutput: true})
	}
	if _, err := ValidatePlannerResult(input, multiRoleResult(input, tooLarge)); err == nil || !strings.Contains(err.Error(), "between 2 and") {
		t.Fatalf("step limit validation error = %v", err)
	}
}

func TestValidatePlannerResultRejectsUnknownRequiredTool(t *testing.T) {
	input := plannerTestInput()
	plan := validPlannerTestPlan(input)
	plan.Steps[0].RequiredTools = []string{"web_search"}
	if _, err := ValidatePlannerResult(input, multiRoleResult(input, plan)); err == nil || !strings.Contains(err.Error(), "not known available") {
		t.Fatalf("tool validation error = %v", err)
	}
}

func TestValidatePlannerResultRejectsMultiRoleWithSingleOwner(t *testing.T) {
	input := plannerTestInput()
	plan := validPlannerTestPlan(input)
	owner := input.Members[1]
	for i := range plan.Steps {
		plan.Steps[i].OwnerAgentID = owner.AgentID
		plan.Steps[i].OwnerAgentKey = owner.AgentKey
		plan.Steps[i].CapabilityKey = string(CapabilityContentLead)
		plan.Steps[i].RequiredTools = nil
	}
	if _, err := ValidatePlannerResult(input, multiRoleResult(input, plan)); err == nil || !strings.Contains(err.Error(), "at least two distinct") {
		t.Fatalf("single-owner multi-role validation error=%v", err)
	}
}

func TestValidatePlannerResultRejectsNonCanonicalCoordinator(t *testing.T) {
	input := plannerTestInput()
	plan := validPlannerTestPlan(input)
	plan.CoordinatorAgentID = uuid.New()
	if _, err := ValidatePlannerResult(input, multiRoleResult(input, plan)); err == nil || !strings.Contains(err.Error(), "canonical team lead") {
		t.Fatalf("coordinator validation error = %v", err)
	}
}

func TestValidatePlannerResultAllowsUnknownCapabilityWithoutInventingMatch(t *testing.T) {
	input := plannerTestInput()
	input.Members[1].Capabilities = nil
	input.Members[1].CapabilitiesStatus = DataStatusUnknown
	plan := validPlannerTestPlan(input)
	plan.FinalOwnerAgentID = input.Members[1].AgentID
	plan.Steps[0].OwnerAgentID = input.Members[1].AgentID
	plan.Steps[2].OwnerAgentID = input.Members[1].AgentID
	if _, err := ValidatePlannerResult(input, multiRoleResult(input, plan)); err != nil {
		t.Fatalf("unknown capability should remain soft evidence, error = %v", err)
	}
}

func TestClassifyWithLLMRepairsParseableInvalidMultiRoleOnce(t *testing.T) {
	input := plannerTestInput()
	invalid := validPlannerTestPlan(input)
	invalid.Steps[0].RequiredTools = []string{"not_available"}
	valid := validPlannerTestPlan(input)
	provider := &fakeArbiterProvider{contents: []string{multiRoleJSON(t, input, invalid), multiRoleJSON(t, input, valid)}}
	result := ClassifyWithLLM(context.Background(), input, provider, "arbiter-model", nil)
	if result.WorkflowMode != WorkflowModeMultiRole || !result.PlannerRepaired || result.Plan == nil {
		t.Fatalf("repaired result = %+v", result)
	}
	if len(provider.requests) != 7 {
		t.Fatalf("classifier calls = %d, want intent resolver + critic + shape + assessment + initial + revision + critic", len(provider.requests))
	}
	if _, hasCap := provider.requests[5].Options["max_tokens"]; hasCap {
		t.Fatalf("revision request must not add a small output cap: %+v", provider.requests[5].Options)
	}
}

func TestClassifyWithLLMRepairsParseableMultiRoleSchemaFailureOnce(t *testing.T) {
	input := plannerTestInput()
	validJSON := multiRoleJSON(t, input, validPlannerTestPlan(input))
	var payload map[string]any
	if err := json.Unmarshal([]byte(validJSON), &payload); err != nil {
		t.Fatal(err)
	}
	delete(payload, "current_agent_role")
	invalidJSON, err := json.Marshal(payload)
	if err != nil {
		t.Fatal(err)
	}
	provider := &fakeArbiterProvider{contents: []string{string(invalidJSON), validJSON}}
	result := ClassifyWithLLM(context.Background(), input, provider, "arbiter-model", nil)
	if result.WorkflowMode != WorkflowModeMultiRole || len(provider.requests) != 7 {
		t.Fatalf("schema repair result=%+v calls=%d", result, len(provider.requests))
	}
}

func TestParseStructuredCapabilitiesDoesNotInferFromExpertiseProse(t *testing.T) {
	capabilities, status := ParseStructuredCapabilities(json.RawMessage(`{"description":"research strategy qa"}`))
	if status != DataStatusUnknown || len(capabilities) != 0 {
		t.Fatalf("capabilities=%v status=%s, want unknown without structured field", capabilities, status)
	}
	capabilities, status = ParseStructuredCapabilities(json.RawMessage(`{"capabilities":["research",{"key":"custom:legal_review","label":"Legal review"}]}`))
	if status != DataStatusKnown || len(capabilities) != 2 {
		t.Fatalf("capabilities=%v status=%s, want two structured declarations", capabilities, status)
	}
}

func TestToolAvailabilityDoesNotBecomeOwnershipCapability(t *testing.T) {
	toolOnly := Profile{
		Kind: "team_member", Name: "Tool User", AgentID: uuid.New(), AgentKey: "tool-user",
		CapabilitiesStatus: DataStatusUnknown, AvailableToolsStatus: DataStatusKnown,
		AvailableTools: []string{"web_search", "write_file"}, ExpertiseSummary: "general assistant",
	}
	input := Input{Mode: ModeTeam, Message: "research the market", Members: []Profile{toolOnly}}
	if candidates := FindBestOwnerCandidates(input, "research"); len(candidates) != 0 {
		t.Fatalf("tool grants must not produce ownership candidates: %+v", candidates)
	}
}

func TestBuildPlanConstraintComputesStableBackendHash(t *testing.T) {
	ownerID := uuid.MustParse("22222222-2222-2222-2222-222222222222")
	plan := &WorkflowPlan{
		SchemaVersion: WorkflowPlanSchemaVersion, Goal: "goal",
		CoordinatorAgentID: uuid.MustParse("11111111-1111-1111-1111-111111111111"), CoordinatorAgentKey: "lead",
		FinalOwnerAgentID: ownerID, FinalOwnerAgentKey: "owner", TerminalStepID: "final",
		Steps: []WorkflowStep{{ID: "final", Title: "Final", Instruction: "Integrate", OwnerAgentID: ownerID, OwnerAgentKey: "owner", Terminal: true}},
	}
	first, err := BuildPlanConstraint(plan)
	if err != nil {
		t.Fatal(err)
	}
	second, err := BuildPlanConstraint(plan)
	if err != nil {
		t.Fatal(err)
	}
	if len(first.PlanHash) != 64 || first.PlanHash != second.PlanHash {
		t.Fatalf("unstable plan hash: %q vs %q", first.PlanHash, second.PlanHash)
	}
	if len(first.Steps) != 1 || first.Steps[0].ID != "final" {
		t.Fatalf("constraint = %+v", first)
	}
}

// Live regression: a khanh-developer multi_role plan named its terminal step in
// terminal_step_id and labelled that step workflow_role "integration", but left
// terminal=false. The plan was rejected with `terminal state conflicts with
// workflow_role "integration"` and a validated 6-step workflow was thrown away
// over a boolean the plan itself had already decided.
func TestValidatePlannerResultReconcilesTerminalFlagFromTerminalStepID(t *testing.T) {
	input := plannerTestInput()
	plan := validPlannerTestPlan(input)
	plan.Steps[0].WorkflowRole = "draft"
	plan.Steps[1].WorkflowRole = "critic"
	plan.Steps[2].WorkflowRole = "integration"
	// The exact live contradiction: terminal_step_id says "integrate", the step says it is not.
	plan.Steps[2].Terminal = false

	result, err := ValidatePlannerResult(input, multiRoleResult(input, plan))
	if err != nil {
		t.Fatalf("self-contradictory terminal flag must be reconciled, not rejected: %v", err)
	}
	terminal := result.Plan.Steps[len(result.Plan.Steps)-1]
	for _, step := range result.Plan.Steps {
		if step.ID == "integrate" {
			terminal = step
		} else if step.Terminal {
			t.Fatalf("step %q must not be terminal: %+v", step.ID, step)
		}
	}
	if !terminal.Terminal || terminal.WorkflowRole != "integration" {
		t.Fatalf("terminal step = %+v, want terminal integration step", terminal)
	}
}

// The mirror slip: the terminal step is correctly flagged but labelled with a
// work role. Repairing the label keeps the plan; a "critic" terminal role is a
// substantive shape error and must still be rejected (covered by
// TestValidateWorkflowRoleRejectsTerminalCritic).
func TestValidatePlannerResultReconcilesTerminalWorkRoleToIntegration(t *testing.T) {
	input := plannerTestInput()
	plan := validPlannerTestPlan(input)
	plan.Steps[0].WorkflowRole = "draft"
	plan.Steps[1].WorkflowRole = "critic"
	plan.Steps[2].WorkflowRole = "work"

	result, err := ValidatePlannerResult(input, multiRoleResult(input, plan))
	if err != nil {
		t.Fatalf("mislabelled terminal role must be reconciled: %v", err)
	}
	for _, step := range result.Plan.Steps {
		if step.ID == "integrate" && step.WorkflowRole != "integration" {
			t.Fatalf("terminal step role = %q, want integration", step.WorkflowRole)
		}
	}
}

// A non-terminal step wrongly claiming the integration role is demoted by DAG
// position instead of failing the plan.
func TestValidatePlannerResultDemotesNonTerminalIntegrationRole(t *testing.T) {
	input := plannerTestInput()
	plan := validPlannerTestPlan(input)
	plan.Steps[0].WorkflowRole = "integration" // entry step, no dependencies
	plan.Steps[1].WorkflowRole = "critic"
	plan.Steps[2].WorkflowRole = "integration"

	result, err := ValidatePlannerResult(input, multiRoleResult(input, plan))
	if err != nil {
		t.Fatalf("duplicate integration role must be reconciled: %v", err)
	}
	roles := map[string]string{}
	for _, step := range result.Plan.Steps {
		roles[step.ID] = step.WorkflowRole
	}
	if roles["draft"] != "draft" {
		t.Fatalf("entry step role = %q, want draft", roles["draft"])
	}
	if roles["integrate"] != "integration" {
		t.Fatalf("terminal step role = %q, want integration", roles["integrate"])
	}
}

// HASH STABILITY: reconciliation must be a no-op on a plan that already
// validates, including one carrying EMPTY workflow_role (persisted schema-v1
// plans do). WorkflowRole and Terminal are inside the canonical JSON that
// BuildPlanConstraint hashes, so any rewrite here would make
// RevalidateStoredWorkflow reject every stored workflow with "canonical hash
// changed".
func TestReconcileTerminalAndRolesIsNoOpOnAlreadyValidPlans(t *testing.T) {
	input := plannerTestInput()
	for _, tc := range []struct {
		name  string
		roles []string
	}{
		{name: "empty schema-v1 roles", roles: []string{"", "", ""}},
		{name: "fully labelled roles", roles: []string{"draft", "critic", "integration"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			plan := validPlannerTestPlan(input)
			for i := range plan.Steps {
				plan.Steps[i].WorkflowRole = tc.roles[i]
			}
			before, err := BuildPlanConstraint(plan)
			if err != nil {
				t.Fatal(err)
			}
			reconcileTerminalAndRoles(plan)
			after, err := BuildPlanConstraint(plan)
			if err != nil {
				t.Fatal(err)
			}
			if before.PlanHash != after.PlanHash {
				t.Fatalf("reconciliation changed a valid plan's hash (%s -> %s); stored workflows would fail revalidation",
					before.PlanHash, after.PlanHash)
			}
		})
	}
}

// wideTeamInput builds a 6-agent team (lead + 5 non-lead specialists) so plans can
// fan out far past the owner->critic->owner minimum.
func wideTeamInput() Input {
	lead := structuredProfile("team_member", "Lead", "team-lead", "lead", "coordinates", CapabilityLeadCoordinator)
	research := structuredProfile("team_member", "Researcher", "researcher", "member", "investigates", CapabilityResearch)
	tech := structuredProfile("team_member", "Engineer", "engineer", "member", "builds", CapabilityTechnical)
	strat := structuredProfile("team_member", "Strategist", "strategist", "member", "synthesizes", CapabilityStrategy)
	qa := structuredProfile("team_member", "QA Reviewer", "qa-reviewer", "reviewer", "independent review", CapabilityQA)
	writer := structuredProfile("team_member", "Writer", "writer", "member", "writes", CapabilityContentLead)
	members := []Profile{lead, research, tech, strat, qa, writer}
	for i := range members {
		members[i].AvailableToolsStatus = DataStatusKnown
		members[i].AvailableTools = []string{"read_file", "write_file"}
	}
	members[0].AvailableTools = []string{"team_tasks", "read_file", "write_file"}
	return Input{
		Mode: ModeTeam, Message: "complex multi-stage program", CurrentAgent: members[0],
		Team:    Profile{Kind: "team", Name: "Program", Text: "large cross-functional team"},
		Members: members, TeamRole: "lead", CanAssignTeamTasks: true,
		CoordinatorAgentID: members[0].AgentID, CoordinatorAgentKey: members[0].AgentKey,
		MemberRequestsEnabled: true, MemberRequestsAutoDispatch: true,
	}
}

// The shape a team split into specialists actually needs: the PRODUCER, the
// CRITIC, and the INTEGRATOR are three different agents. The old rule required
// the reviewed step to be owned by the final owner, which rejected this outright
// — independence is about author-vs-reviewer, not about who integrates.
func TestValidatePlannerResultAcceptsProducerCriticIntegratorAsThreeAgents(t *testing.T) {
	input := wideTeamInput()
	research, qa, writer := input.Members[1], input.Members[4], input.Members[5]
	plan := &WorkflowPlan{
		SchemaVersion: WorkflowPlanSchemaVersion, Goal: "reviewed deliverable",
		CoordinatorAgentID: input.CoordinatorAgentID, CoordinatorAgentKey: input.CoordinatorAgentKey,
		FinalOwnerAgentID: writer.AgentID, FinalOwnerAgentKey: writer.AgentKey,
		ReviewStatus: "required", TerminalStepID: "integrate",
		Steps: []WorkflowStep{
			{ID: "produce", Title: "Produce", Instruction: "Produce the analysis", OwnerAgentID: research.AgentID, OwnerAgentKey: research.AgentKey, WorkflowRole: "draft", RequiredOutput: true},
			{ID: "review", Title: "Review", Instruction: "Critique the analysis", OwnerAgentID: qa.AgentID, OwnerAgentKey: qa.AgentKey, WorkflowRole: "critic", DependsOn: []string{"produce"}, RequiredOutput: true},
			{ID: "integrate", Title: "Integrate", Instruction: "Integrate into the deliverable", OwnerAgentID: writer.AgentID, OwnerAgentKey: writer.AgentKey, WorkflowRole: "integration", DependsOn: []string{"review"}, RequiredOutput: true, Terminal: true},
		},
	}
	result, err := ValidatePlannerResult(input, multiRoleResult(input, plan))
	if err != nil {
		t.Fatalf("producer/critic/integrator as three distinct agents was rejected: %v", err)
	}
	if result.Plan.ReviewStatus != "included" {
		t.Fatalf("review status = %q, want included", result.Plan.ReviewStatus)
	}
}

// A wide plan: two parallel entry branches, a mid synthesis, and a critic that is
// several hops downstream of the work it reviews. The review rule is reachability
// based, so distance and fan-out must not matter.
func TestValidatePlannerResultAcceptsWideParallelPlanWithDistantCritic(t *testing.T) {
	input := wideTeamInput()
	research, tech, strat, qa, writer := input.Members[1], input.Members[2], input.Members[3], input.Members[4], input.Members[5]
	plan := &WorkflowPlan{
		SchemaVersion: WorkflowPlanSchemaVersion, Goal: "ship the program",
		CoordinatorAgentID: input.CoordinatorAgentID, CoordinatorAgentKey: input.CoordinatorAgentKey,
		FinalOwnerAgentID: writer.AgentID, FinalOwnerAgentKey: writer.AgentKey,
		ReviewStatus: "required", TerminalStepID: "s6",
		Steps: []WorkflowStep{
			{ID: "s1", Title: "Research", Instruction: "Gather evidence", OwnerAgentID: research.AgentID, OwnerAgentKey: research.AgentKey, WorkflowRole: "work", RequiredOutput: true},
			{ID: "s2", Title: "Prototype", Instruction: "Build a prototype", OwnerAgentID: tech.AgentID, OwnerAgentKey: tech.AgentKey, WorkflowRole: "work", RequiredOutput: true},
			{ID: "s3", Title: "Draft", Instruction: "Draft the program plan", OwnerAgentID: writer.AgentID, OwnerAgentKey: writer.AgentKey, WorkflowRole: "draft", DependsOn: []string{"s1", "s2"}, RequiredOutput: true},
			{ID: "s4", Title: "Strategy", Instruction: "Synthesize the strategy", OwnerAgentID: strat.AgentID, OwnerAgentKey: strat.AgentKey, WorkflowRole: "work", DependsOn: []string{"s3"}, RequiredOutput: true},
			{ID: "s5", Title: "Review", Instruction: "Critique the strategy", OwnerAgentID: qa.AgentID, OwnerAgentKey: qa.AgentKey, WorkflowRole: "critic", DependsOn: []string{"s4"}, RequiredOutput: true},
			{ID: "s6", Title: "Integrate", Instruction: "Integrate everything", OwnerAgentID: writer.AgentID, OwnerAgentKey: writer.AgentKey, WorkflowRole: "integration", DependsOn: []string{"s5"}, RequiredOutput: true, Terminal: true},
		},
	}
	if _, err := ValidatePlannerResult(input, multiRoleResult(input, plan)); err != nil {
		t.Fatalf("wide 6-step plan across 5 owners was rejected: %v", err)
	}
}

// Two critics reviewing different aspects of the same work in parallel.
func TestValidatePlannerResultAcceptsTwoParallelCritics(t *testing.T) {
	input := wideTeamInput()
	analyst := structuredProfile("team_member", "Analyst", "analyst", "reviewer", "challenges data", CapabilityAnalyticsCritic)
	analyst.AvailableToolsStatus = DataStatusKnown
	analyst.AvailableTools = []string{"read_file"}
	input.Members = append(input.Members, analyst)
	research, qa, writer := input.Members[1], input.Members[4], input.Members[5]
	plan := &WorkflowPlan{
		SchemaVersion: WorkflowPlanSchemaVersion, Goal: "double reviewed deliverable",
		CoordinatorAgentID: input.CoordinatorAgentID, CoordinatorAgentKey: input.CoordinatorAgentKey,
		FinalOwnerAgentID: writer.AgentID, FinalOwnerAgentKey: writer.AgentKey,
		ReviewStatus: "required", TerminalStepID: "integrate",
		Steps: []WorkflowStep{
			{ID: "produce", Title: "Produce", Instruction: "Produce the analysis", OwnerAgentID: research.AgentID, OwnerAgentKey: research.AgentKey, WorkflowRole: "draft", RequiredOutput: true},
			{ID: "review-quality", Title: "Review quality", Instruction: "Critique the writing", OwnerAgentID: qa.AgentID, OwnerAgentKey: qa.AgentKey, WorkflowRole: "critic", DependsOn: []string{"produce"}, RequiredOutput: true},
			{ID: "review-data", Title: "Review data", Instruction: "Critique the data", OwnerAgentID: analyst.AgentID, OwnerAgentKey: analyst.AgentKey, WorkflowRole: "critic", DependsOn: []string{"produce"}, RequiredOutput: true},
			{ID: "integrate", Title: "Integrate", Instruction: "Integrate both reviews", OwnerAgentID: writer.AgentID, OwnerAgentKey: writer.AgentKey, WorkflowRole: "integration", DependsOn: []string{"review-quality", "review-data"}, RequiredOutput: true, Terminal: true},
		},
	}
	if _, err := ValidatePlannerResult(input, multiRoleResult(input, plan)); err != nil {
		t.Fatalf("two parallel critics were rejected: %v", err)
	}
}

// Relaxing the shape must not relax INDEPENDENCE. A critic reviewing only their
// own upstream work is self-review and stays rejected.
func TestValidatePlannerResultRejectsCriticReviewingOnlyOwnWork(t *testing.T) {
	input := wideTeamInput()
	qa, writer := input.Members[4], input.Members[5]
	plan := &WorkflowPlan{
		SchemaVersion: WorkflowPlanSchemaVersion, Goal: "self reviewed deliverable",
		CoordinatorAgentID: input.CoordinatorAgentID, CoordinatorAgentKey: input.CoordinatorAgentKey,
		FinalOwnerAgentID: writer.AgentID, FinalOwnerAgentKey: writer.AgentKey,
		ReviewStatus: "required", TerminalStepID: "integrate",
		Steps: []WorkflowStep{
			{ID: "produce", Title: "Produce", Instruction: "Produce the analysis", OwnerAgentID: qa.AgentID, OwnerAgentKey: qa.AgentKey, WorkflowRole: "draft", RequiredOutput: true},
			{ID: "review", Title: "Review", Instruction: "Critique it", OwnerAgentID: qa.AgentID, OwnerAgentKey: qa.AgentKey, WorkflowRole: "critic", DependsOn: []string{"produce"}, RequiredOutput: true},
			{ID: "integrate", Title: "Integrate", Instruction: "Integrate", OwnerAgentID: writer.AgentID, OwnerAgentKey: writer.AgentKey, WorkflowRole: "integration", DependsOn: []string{"review"}, RequiredOutput: true, Terminal: true},
		},
	}
	if _, err := ValidatePlannerResult(input, multiRoleResult(input, plan)); err == nil {
		t.Fatal("a critic reviewing only their own work was accepted as independent review")
	}
}

// The final owner may not be their own critic. isReviewerStep can infer reviewer
// intent from the OWNER's profile capability alone, so without this a plan with no
// declared critic at all would satisfy a hard review requirement just because the
// integrator happens to be QA-capable.
func TestValidatePlannerResultRejectsFinalOwnerAsOwnCritic(t *testing.T) {
	input := wideTeamInput()
	research, qa := input.Members[1], input.Members[4]
	plan := &WorkflowPlan{
		SchemaVersion: WorkflowPlanSchemaVersion, Goal: "integrator reviews itself",
		CoordinatorAgentID: input.CoordinatorAgentID, CoordinatorAgentKey: input.CoordinatorAgentKey,
		FinalOwnerAgentID: qa.AgentID, FinalOwnerAgentKey: qa.AgentKey,
		ReviewStatus: "required", TerminalStepID: "integrate",
		Steps: []WorkflowStep{
			{ID: "produce", Title: "Produce", Instruction: "Produce the analysis", OwnerAgentID: research.AgentID, OwnerAgentKey: research.AgentKey, WorkflowRole: "draft", RequiredOutput: true},
			{ID: "review", Title: "Review", Instruction: "Critique it", OwnerAgentID: qa.AgentID, OwnerAgentKey: qa.AgentKey, WorkflowRole: "critic", DependsOn: []string{"produce"}, RequiredOutput: true},
			{ID: "integrate", Title: "Integrate", Instruction: "Integrate", OwnerAgentID: qa.AgentID, OwnerAgentKey: qa.AgentKey, WorkflowRole: "integration", DependsOn: []string{"review"}, RequiredOutput: true, Terminal: true},
		},
	}
	if _, err := ValidatePlannerResult(input, multiRoleResult(input, plan)); err == nil {
		t.Fatal("the final owner was accepted as their own independent critic")
	}
}

// A critic whose findings never reach the terminal step is a review that can be
// ignored, and stays rejected.
func TestValidatePlannerResultRejectsCriticNotReachingTerminal(t *testing.T) {
	input := wideTeamInput()
	research, qa, writer := input.Members[1], input.Members[4], input.Members[5]
	plan := &WorkflowPlan{
		SchemaVersion: WorkflowPlanSchemaVersion, Goal: "ignorable review",
		CoordinatorAgentID: input.CoordinatorAgentID, CoordinatorAgentKey: input.CoordinatorAgentKey,
		FinalOwnerAgentID: writer.AgentID, FinalOwnerAgentKey: writer.AgentKey,
		ReviewStatus: "required", TerminalStepID: "integrate",
		Steps: []WorkflowStep{
			{ID: "produce", Title: "Produce", Instruction: "Produce the analysis", OwnerAgentID: research.AgentID, OwnerAgentKey: research.AgentKey, WorkflowRole: "draft", RequiredOutput: true},
			{ID: "review", Title: "Review", Instruction: "Critique it", OwnerAgentID: qa.AgentID, OwnerAgentKey: qa.AgentKey, WorkflowRole: "critic", DependsOn: []string{"produce"}, RequiredOutput: true},
			// Integration depends on the raw work, NOT on the review.
			{ID: "integrate", Title: "Integrate", Instruction: "Integrate", OwnerAgentID: writer.AgentID, OwnerAgentKey: writer.AgentKey, WorkflowRole: "integration", DependsOn: []string{"produce"}, RequiredOutput: true, Terminal: true},
		},
	}
	if _, err := ValidatePlannerResult(input, multiRoleResult(input, plan)); err == nil {
		t.Fatal("a critic whose findings never reach the terminal step was accepted")
	}
}

// The planner prompt must not teach a shape the validator no longer requires, and
// must tell the model the real step/agent ceilings.
func TestBuildPlanningMessagesDoesNotPrescribeDrafterEqualsIntegrator(t *testing.T) {
	messages := BuildPlanningMessages(plannerTestInput(), Evidence{}, WorkAssessment{WorkflowMode: WorkflowModeMultiRole, IndependentReviewRequired: true}, true)
	system := messages[0].Content
	for _, banned := range []string{
		"The final owner must first own a workflow_role=\"draft\" step",
		"owner -> different reviewer -> owner integration",
		"same owner terminal integration",
	} {
		if strings.Contains(system, banned) {
			t.Fatalf("planning prompt still prescribes the rigid shape: %q", banned)
		}
	}
	if strings.Contains(system, "%!d") || strings.Contains(system, "%!s") {
		t.Fatalf("planning prompt has a broken format verb: %s", system)
	}
	for _, want := range []string{"16 steps", "12 distinct non-lead agents", "DIFFERENT agent"} {
		if !strings.Contains(system, want) {
			t.Fatalf("planning prompt missing %q", want)
		}
	}
}

// Live regression 2026-07-26: two consecutive turns lost a valid plan to
// `step "final-integration" cannot be owned by the team lead coordinator`. The
// planner payload listed the coordinator inside `members` with no positive
// allow-list of eligible owners, and the planner prompt never stated the
// exclusion at all — so the model kept handing the lead the integration step.
func TestBuildPlanningMessagesExcludesCoordinatorFromStepOwnerCandidates(t *testing.T) {
	input := plannerTestInput()
	messages := BuildPlanningMessages(input, Evidence{}, WorkAssessment{WorkflowMode: WorkflowModeMultiRole, IndependentReviewRequired: true}, true)

	if !strings.Contains(messages[0].Content, "must NEVER own an executable step") {
		t.Fatal("planner prompt does not state the lead-exclusion hard constraint")
	}
	if !strings.Contains(messages[0].Content, "step_owner_candidates") {
		t.Fatal("planner prompt does not point the model at step_owner_candidates")
	}

	var payload struct {
		StepOwnerCandidates []map[string]string `json:"step_owner_candidates"`
		CoordinatorKey      string              `json:"coordinator_key"`
	}
	if err := json.Unmarshal([]byte(messages[1].Content), &payload); err != nil {
		t.Fatal(err)
	}
	if len(payload.StepOwnerCandidates) != 2 {
		t.Fatalf("step_owner_candidates = %+v, want the 2 non-lead members", payload.StepOwnerCandidates)
	}
	for _, candidate := range payload.StepOwnerCandidates {
		if candidate["agent_key"] == input.CoordinatorAgentKey || candidate["agent_id"] == input.CoordinatorAgentID.String() {
			t.Fatalf("coordinator %q must never be an eligible step owner: %+v", input.CoordinatorAgentKey, candidate)
		}
		if candidate["agent_id"] == "" || candidate["agent_key"] == "" {
			t.Fatalf("candidate missing canonical identity: %+v", candidate)
		}
	}
	if payload.CoordinatorKey != input.CoordinatorAgentKey {
		t.Fatalf("coordinator_key = %q, want %q", payload.CoordinatorKey, input.CoordinatorAgentKey)
	}
}

// A member-role turn resolves its coordinator to a lead that is NOT the current
// agent; that lead must still be excluded, and the current agent (a member) must
// still be eligible.
func TestStepOwnerCandidateRefsExcludesCoordinatorNotCurrentAgent(t *testing.T) {
	input := plannerTestInput()
	member := input.Members[1]
	input.CurrentAgent = member
	input.TeamRole = "member"
	candidates := stepOwnerCandidateRefs(input)
	foundMember := false
	for _, candidate := range candidates {
		if candidate["agent_id"] == input.CoordinatorAgentID.String() {
			t.Fatalf("coordinator leaked into candidates: %+v", candidates)
		}
		if candidate["agent_id"] == member.AgentID.String() {
			foundMember = true
		}
	}
	if !foundMember {
		t.Fatalf("the requesting member must remain an eligible owner: %+v", candidates)
	}
}

// Live regression 2026-07-26: a khanh-developer reviewed_decision turn was lost
// with `cannot unmarshal object into Go struct field .staffing_gaps of type
// string`. The prompt asks for "the unfilled work-unit IDs and reasons", so models
// emit objects. staffing_gaps is decoded as part of the WHOLE arbiter response, so
// its shape killed the entire plan — even for an EMPTY array claiming no gap.
func TestParseArbiterResultAcceptsBothStaffingGapShapes(t *testing.T) {
	base := func(gaps string) string {
		return `{"workflow_mode":"self","decision":"self","required_tool":"","workflow_executable":false,` +
			`"current_agent_role":"lead","task_type":"research","current_agent_fit":"weak",` +
			`"best_team_owner":"","best_team_owner_role":"","best_team_fit":"none",` +
			`"specialist_match_found":false,"lead_selected_as_fallback":false,` +
			`"routing_priority_used":"no_specialist","owner_selection_reason":"none",` +
			`"followup_context_used_for_reference_only":true,"reason":"no fit",` +
			`"staffing_gaps":` + gaps + `,"plan":null}`
	}
	for _, tc := range []struct {
		name string
		gaps string
		want []string
	}{
		{name: "empty array", gaps: `[]`, want: nil},
		{name: "string array", gaps: `["review: no reviewer"]`, want: []string{"review: no reviewer"}},
		{
			name: "object array with id and reason",
			gaps: `[{"id":"unit-2","reason":"no distinct reviewer"}]`,
			want: []string{"unit-2: no distinct reviewer"},
		},
		{
			name: "object array with alternate keys",
			gaps: `[{"work_unit":"unit-3","detail":"no technical owner"}]`,
			want: []string{"unit-3: no technical owner"},
		},
		{name: "object array reason only", gaps: `[{"reason":"roster too small"}]`, want: []string{"roster too small"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			result, err := ParseArbiterResult(base(tc.gaps), ModeTeam)
			if err != nil {
				t.Fatalf("staffing_gaps shape %s must not fail the whole response: %v", tc.gaps, err)
			}
			if len(result.StaffingGaps) != len(tc.want) {
				t.Fatalf("StaffingGaps = %#v, want %#v", result.StaffingGaps, tc.want)
			}
			for i := range tc.want {
				if result.StaffingGaps[i] != tc.want[i] {
					t.Fatalf("StaffingGaps[%d] = %q, want %q", i, result.StaffingGaps[i], tc.want[i])
				}
			}
		})
	}
}

// An unrecognised gap element must never be silently dropped: a real staffing gap
// that decodes to nothing would let an unstaffable workflow through.
func TestParseArbiterResultKeepsUnrecognisedStaffingGapNonEmpty(t *testing.T) {
	raw := `{"workflow_mode":"self","decision":"self","required_tool":"","workflow_executable":false,` +
		`"current_agent_role":"lead","task_type":"research","current_agent_fit":"weak",` +
		`"best_team_owner":"","best_team_owner_role":"","best_team_fit":"none",` +
		`"specialist_match_found":false,"lead_selected_as_fallback":false,` +
		`"routing_priority_used":"no_specialist","owner_selection_reason":"none",` +
		`"followup_context_used_for_reference_only":true,"reason":"no fit",` +
		`"staffing_gaps":[["unit-9","weird"]],"plan":null}`
	result, err := ParseArbiterResult(raw, ModeTeam)
	if err != nil {
		t.Fatalf("unexpected parse error: %v", err)
	}
	if len(result.StaffingGaps) != 1 || strings.TrimSpace(result.StaffingGaps[0]) == "" {
		t.Fatalf("unrecognised gap was dropped: %#v", result.StaffingGaps)
	}
}

// terminal_step_id naming no existing step is a real defect: nothing is silently
// invented, and validation still reports it.
func TestReconcileTerminalAndRolesLeavesUnknownTerminalStepToValidation(t *testing.T) {
	input := plannerTestInput()
	plan := validPlannerTestPlan(input)
	plan.TerminalStepID = "does-not-exist"
	if _, err := ValidatePlannerResult(input, multiRoleResult(input, plan)); err == nil {
		t.Fatal("unknown terminal_step_id must still be rejected")
	}
}

// Live regression: a structurally sound plan was thrown away and the turn
// degraded to self with `assignment_revision_failed` because its review step was
// tagged workflow_role="review" instead of "critic". workflow_role is a
// bookkeeping label — the substantive rules read the graph and the profiles — so
// a synonym must normalise, not destroy the plan.
func TestValidatePlannerResultNormalisesWorkflowRoleSynonyms(t *testing.T) {
	for _, tc := range []struct {
		name, reviewRole, terminalRole, workRole string
	}{
		{"review", "review", "integration", "work"},
		{"reviewer", "reviewer", "integrate", "research"},
		{"qa", "qa", "synthesis", "analysis"},
		{"verify", "verify", "final", "execute"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			input := wideTeamInput()
			research, qa, writer := input.Members[1], input.Members[4], input.Members[5]
			plan := &WorkflowPlan{
				SchemaVersion: WorkflowPlanSchemaVersion, Goal: "reviewed deliverable",
				CoordinatorAgentID: input.CoordinatorAgentID, CoordinatorAgentKey: input.CoordinatorAgentKey,
				FinalOwnerAgentID: writer.AgentID, FinalOwnerAgentKey: writer.AgentKey,
				ReviewStatus: "required", TerminalStepID: "integrate",
				Steps: []WorkflowStep{
					{ID: "produce", Title: "Produce", Instruction: "Produce the analysis", OwnerAgentID: research.AgentID, OwnerAgentKey: research.AgentKey, WorkflowRole: tc.workRole, RequiredOutput: true},
					{ID: "review", Title: "Review", Instruction: "Critique the analysis", OwnerAgentID: qa.AgentID, OwnerAgentKey: qa.AgentKey, WorkflowRole: tc.reviewRole, DependsOn: []string{"produce"}, RequiredOutput: true},
					{ID: "integrate", Title: "Integrate", Instruction: "Integrate into the deliverable", OwnerAgentID: writer.AgentID, OwnerAgentKey: writer.AgentKey, WorkflowRole: tc.terminalRole, DependsOn: []string{"review"}, RequiredOutput: true, Terminal: true},
				},
			}
			result, err := ValidatePlannerResult(input, multiRoleResult(input, plan))
			if err != nil {
				t.Fatalf("plan with role synonyms was rejected: %v", err)
			}
			roles := map[string]string{}
			for _, step := range result.Plan.Steps {
				roles[step.ID] = step.WorkflowRole
			}
			if roles["review"] != "critic" {
				t.Errorf("review role = %q, want critic", roles["review"])
			}
			if roles["integrate"] != "integration" {
				t.Errorf("terminal role = %q, want integration", roles["integrate"])
			}
			if roles["produce"] != "work" && roles["produce"] != "draft" {
				t.Errorf("work role = %q, want work or draft", roles["produce"])
			}
		})
	}
}

// A genuinely unknown role is still an error: normalising must not turn into
// silently accepting anything.
func TestValidatePlannerResultStillRejectsUnknownWorkflowRole(t *testing.T) {
	input := wideTeamInput()
	research, qa, writer := input.Members[1], input.Members[4], input.Members[5]
	plan := &WorkflowPlan{
		SchemaVersion: WorkflowPlanSchemaVersion, Goal: "reviewed deliverable",
		CoordinatorAgentID: input.CoordinatorAgentID, CoordinatorAgentKey: input.CoordinatorAgentKey,
		FinalOwnerAgentID: writer.AgentID, FinalOwnerAgentKey: writer.AgentKey,
		ReviewStatus: "required", TerminalStepID: "integrate",
		Steps: []WorkflowStep{
			{ID: "produce", Title: "Produce", Instruction: "Produce", OwnerAgentID: research.AgentID, OwnerAgentKey: research.AgentKey, WorkflowRole: "banana", RequiredOutput: true},
			{ID: "review", Title: "Review", Instruction: "Critique", OwnerAgentID: qa.AgentID, OwnerAgentKey: qa.AgentKey, WorkflowRole: "critic", DependsOn: []string{"produce"}, RequiredOutput: true},
			{ID: "integrate", Title: "Integrate", Instruction: "Integrate", OwnerAgentID: writer.AgentID, OwnerAgentKey: writer.AgentKey, WorkflowRole: "integration", DependsOn: []string{"review"}, RequiredOutput: true, Terminal: true},
		},
	}
	if _, err := ValidatePlannerResult(input, multiRoleResult(input, plan)); err == nil {
		t.Fatal("expected an unknown workflow_role to be rejected")
	}
}

// Hash stability: every plan that could already be STORED carries canonical roles
// (the old validator rejected anything else), so synonym normalisation must be a
// no-op on them and must not change the canonical plan hash.
func TestWorkflowRoleNormalisationIsNoOpOnCanonicalRoles(t *testing.T) {
	for _, role := range []string{"", "draft", "work", "critic", "integration"} {
		step := WorkflowStep{ID: "s", WorkflowRole: role, Terminal: role == "integration"}
		if err := validateWorkflowRole(&step); err != nil {
			t.Fatalf("canonical role %q rejected: %v", role, err)
		}
		if step.WorkflowRole != role {
			t.Fatalf("canonical role %q was rewritten to %q", role, step.WorkflowRole)
		}
	}
}
