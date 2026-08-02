package teamworkclassify

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/nextlevelbuilder/goclaw/internal/providers"
)

func routeTestInput() Input {
	return Input{
		Mode:                ModeTeam,
		Message:             "Can you explain this result?",
		RecentContext:       "user: previous question\nassistant: previous answer",
		TeamRole:            "lead",
		CanAssignTeamTasks:  true,
		CurrentAgent:        Profile{AgentID: uuid.New(), AgentKey: "lead", Name: "Lead"},
		Members:             []Profile{{AgentID: uuid.New(), AgentKey: "researcher", Name: "Researcher", TeamRole: "member"}},
		SelfTools:           []Profile{{Kind: "tool", Name: "read_file", Text: "read files"}},
		Team:                Profile{Kind: "team", Name: "Test Team", Text: "coordinate specialist work"},
		CollaborationTools:  []Profile{{Kind: "tool", Name: "team_tasks", Text: "assign native team tasks"}},
		ToolAllow:           []string{"read_file", "team_tasks"},
		PinnedSkillsContext: "PINNED_WORKFLOW_BODY",
	}
}

func TestClassifyRouteWithLLMMakesOneCallWithFullContext(t *testing.T) {
	provider := &fakeArbiterProvider{content: `{"decision":"self","reason":"direct answer","preferred_owner":"","task_type":"other"}`}
	input := routeTestInput()
	result := ClassifyRouteWithLLM(context.Background(), input, provider, "route-model", nil)
	if result.Decision != DecisionSelf || len(provider.requests) != 1 {
		t.Fatalf("result=%+v calls=%d, want self and one call", result, len(provider.requests))
	}
	payload := provider.requests[0].Messages[1].Content
	for _, want := range []string{input.Message, input.RecentContext, "researcher", "can_assign=true", "read_file", "team_tasks", "Test Team", input.PinnedSkillsContext} {
		if !strings.Contains(payload, want) {
			t.Fatalf("routing payload missing %q", want)
		}
	}
	if got := provider.requests[0].Options[providers.OptMaxTokens]; got != routingMaxOutputTokens {
		t.Fatalf("max tokens=%v, want %d", got, routingMaxOutputTokens)
	}
}

func TestClassifyRouteWithLLMReturnsNativeSingleOwnerTeamRoute(t *testing.T) {
	provider := &fakeArbiterProvider{content: `{"decision":"team","reason":"needs research specialist","preferred_owner":"researcher","task_type":"research"}`}
	input := routeTestInput()
	result := ClassifyRouteWithLLM(context.Background(), input, provider, "route-model", nil)

	if len(provider.requests) != 1 {
		t.Fatalf("calls=%d, want one", len(provider.requests))
	}
	if result.Decision != DecisionTeam || result.RequiredTool != "team_tasks" {
		t.Fatalf("result=%+v, want native team_tasks route", result)
	}
	if result.WorkflowMode != WorkflowModeSingleOwner || result.RequestedWorkflowMode != WorkflowModeSingleOwner || result.EffectiveWorkflowMode != WorkflowModeSingleOwner {
		t.Fatalf("workflow modes=%q/%q/%q, want single_owner", result.WorkflowMode, result.RequestedWorkflowMode, result.EffectiveWorkflowMode)
	}
	if result.Plan != nil {
		t.Fatalf("plan=%+v, want nil on routing hot path", result.Plan)
	}
	if result.BestTeamOwner != input.Members[0].AgentKey || result.BestTeamOwnerID != input.Members[0].AgentID {
		t.Fatalf("owner=%q/%s, want canonical member %q/%s", result.BestTeamOwner, result.BestTeamOwnerID, input.Members[0].AgentKey, input.Members[0].AgentID)
	}
}

func TestClassifyRouteWithLLMBlocksTeamRouteWithoutMemberPermission(t *testing.T) {
	provider := &fakeArbiterProvider{content: `{"decision":"team","reason":"delegate work","preferred_owner":"researcher","task_type":"research"}`}
	input := routeTestInput()
	input.TeamRole = "member"
	input.CanAssignTeamTasks = false
	input.MemberRequestsEnabled = false
	input.MemberRequestsAutoDispatch = false

	result := ClassifyRouteWithLLM(context.Background(), input, provider, "route-model", nil)
	if len(provider.requests) != 1 {
		t.Fatalf("calls=%d, want one", len(provider.requests))
	}
	if result.Decision != DecisionSelf || result.DecisionBeforeValidation != DecisionTeam {
		t.Fatalf("result=%+v, want model team decision blocked to self", result)
	}
	if result.RequiredTool != "" || result.WorkflowMode != WorkflowModeSelf || result.Plan != nil {
		t.Fatalf("blocked result retained workflow state: %+v", result)
	}
}

func TestClassifyRouteWithLLMBlocksSingleScopeWithoutMemberPermission(t *testing.T) {
	provider := &fakeArbiterProvider{content: `{"decision":"team","scope":"single","reason":"delegate work","preferred_owner":"researcher","task_type":"research"}`}
	input := routeTestInput()
	input.TeamRole = "member"
	input.CanAssignTeamTasks = false
	input.MemberRequestsEnabled = false
	input.MemberRequestsAutoDispatch = false

	result := ClassifyRouteWithLLM(context.Background(), input, provider, "route-model", nil)
	if result.Decision != DecisionSelf || result.DecisionBeforeValidation != DecisionTeam {
		t.Fatalf("result=%+v, want explicit single scope blocked to self by permissions", result)
	}
	if result.NonExecutable || result.WorkflowMode != WorkflowModeSelf {
		t.Fatalf("single-scope permission block must not take the coordinated fail-closed path: %+v", result)
	}
}

func TestClassifyRouteWithLLMBlocksUnknownTeamOwner(t *testing.T) {
	provider := &fakeArbiterProvider{content: `{"decision":"team","reason":"delegate work","preferred_owner":"invented-agent","task_type":"research"}`}
	input := routeTestInput()

	result := ClassifyRouteWithLLM(context.Background(), input, provider, "route-model", nil)
	if result.Decision != DecisionSelf || result.DecisionBeforeValidation != DecisionTeam {
		t.Fatalf("result=%+v, want unknown owner blocked to self", result)
	}
	if result.RequiredTool != "" || result.WorkflowExecutable || result.Plan != nil {
		t.Fatalf("invalid owner retained executable workflow state: %+v", result)
	}
}

func coordinatedRouteInput() Input {
	input := routeTestInput()
	input.CurrentAgent.AvailableTools = []string{"read_file", "team_tasks"}
	input.CoordinatorAgentID = uuid.New()
	input.CoordinatorAgentKey = "team-lead"
	return input
}

func TestClassifyRouteWithLLMCoordinatedScopeRoutesToCanonicalLead(t *testing.T) {
	provider := &fakeArbiterProvider{content: `{"decision":"team","scope":"coordinated","reason":"parallel research and synthesis","preferred_owner":"","task_type":"research"}`}
	input := coordinatedRouteInput()
	result := ClassifyRouteWithLLM(context.Background(), input, provider, "route-model", nil)

	if len(provider.requests) != 1 {
		t.Fatalf("calls=%d, want one", len(provider.requests))
	}
	if result.Decision != DecisionTeam || result.NonExecutable {
		t.Fatalf("result=%+v, want executable coordinated team route", result)
	}
	if result.WorkflowMode != WorkflowModeMultiRole || result.RequestedWorkflowMode != WorkflowModeMultiRole || result.EffectiveWorkflowMode != WorkflowModeMultiRole {
		t.Fatalf("workflow modes=%q/%q/%q, want multi_role", result.WorkflowMode, result.RequestedWorkflowMode, result.EffectiveWorkflowMode)
	}
	if result.BestTeamOwner != input.CoordinatorAgentKey || result.BestTeamOwnerID != input.CoordinatorAgentID || result.BestTeamOwnerRole != "lead" {
		t.Fatalf("owner=%q/%s/%q, want canonical coordinator %q/%s/lead", result.BestTeamOwner, result.BestTeamOwnerID, result.BestTeamOwnerRole, input.CoordinatorAgentKey, input.CoordinatorAgentID)
	}
	if result.RequiredTool != "team_tasks" || !result.WorkflowExecutable || result.Plan != nil {
		t.Fatalf("coordinated route state=%+v, want executable team_tasks route without a plan", result)
	}
}

func TestClassifyRouteWithLLMExplicitSingleScopeKeepsSingleOwner(t *testing.T) {
	provider := &fakeArbiterProvider{content: `{"decision":"team","scope":"single","reason":"one specialist","preferred_owner":"researcher","task_type":"research"}`}
	input := coordinatedRouteInput()
	result := ClassifyRouteWithLLM(context.Background(), input, provider, "route-model", nil)

	if result.Decision != DecisionTeam || result.WorkflowMode != WorkflowModeSingleOwner {
		t.Fatalf("result=%+v, want explicit single scope to stay single_owner", result)
	}
	if result.BestTeamOwner != input.Members[0].AgentKey || result.BestTeamOwnerID != input.Members[0].AgentID {
		t.Fatalf("owner=%q/%s, want preferred member", result.BestTeamOwner, result.BestTeamOwnerID)
	}
}

func TestClassifyRouteWithLLMAbsentScopeKeepsSingleOwner(t *testing.T) {
	provider := &fakeArbiterProvider{content: `{"decision":"team","reason":"one specialist","preferred_owner":"researcher","task_type":"research"}`}
	input := coordinatedRouteInput()
	result := ClassifyRouteWithLLM(context.Background(), input, provider, "route-model", nil)

	if result.Decision != DecisionTeam || result.WorkflowMode != WorkflowModeSingleOwner {
		t.Fatalf("result=%+v, want absent scope to stay backward-compatible single_owner", result)
	}
}

// review_required is only enforceable on a coordinated route, so a contradictory
// single scope carrying it is promoted to coordinated — never left as a review-less
// single_owner, and never silently rerouted to self.
func TestClassifyRouteWithLLMReviewRequiredPromotesSingleScopeToCoordinated(t *testing.T) {
	provider := &fakeArbiterProvider{content: `{"decision":"team","scope":"single","review_required":true,"reason":"high stakes, needs independent QA","preferred_owner":"researcher","task_type":"research"}`}
	input := coordinatedRouteInput()
	result := ClassifyRouteWithLLM(context.Background(), input, provider, "route-model", nil)

	if result.Decision != DecisionTeam || result.NonExecutable {
		t.Fatalf("result=%+v, want executable team decision, not self/non-executable", result)
	}
	if result.WorkflowMode != WorkflowModeMultiRole || result.RequestedWorkflowMode != WorkflowModeMultiRole || result.EffectiveWorkflowMode != WorkflowModeMultiRole {
		t.Fatalf("workflow modes=%q/%q/%q, want multi_role after review promotion", result.WorkflowMode, result.RequestedWorkflowMode, result.EffectiveWorkflowMode)
	}
	if result.BestTeamOwner != input.CoordinatorAgentKey || result.BestTeamOwnerID != input.CoordinatorAgentID || result.BestTeamOwnerRole != "lead" {
		t.Fatalf("owner=%q/%s/%q, want canonical coordinator — the preferred single owner must not survive review promotion", result.BestTeamOwner, result.BestTeamOwnerID, result.BestTeamOwnerRole)
	}
	if !result.RequestedReviewRequired || !result.EffectiveReviewRequired {
		t.Fatalf("review flags=%v/%v, want both true after promotion", result.RequestedReviewRequired, result.EffectiveReviewRequired)
	}
	if !result.WorkflowExecutable || result.Plan != nil {
		t.Fatalf("promoted route state=%+v, want executable coordinated route without a plan", result)
	}
}

// An absent scope (empty scope) with review_required is the same under-record and
// promotes to coordinated — the absent-scope→single_owner back-compat rule applies
// only when no review is requested.
func TestClassifyRouteWithLLMReviewRequiredPromotesAbsentScopeToCoordinated(t *testing.T) {
	provider := &fakeArbiterProvider{content: `{"decision":"team","review_required":true,"reason":"needs cross-checking","preferred_owner":"researcher","task_type":"research"}`}
	input := coordinatedRouteInput()
	result := ClassifyRouteWithLLM(context.Background(), input, provider, "route-model", nil)

	if result.Decision != DecisionTeam || result.WorkflowMode != WorkflowModeMultiRole {
		t.Fatalf("result=%+v, want absent scope + review to promote to multi_role", result)
	}
	if !result.EffectiveReviewRequired {
		t.Fatalf("effective review=%v, want true", result.EffectiveReviewRequired)
	}
}

// A promoted request whose roster cannot support coordination still fails closed
// with a configuration error — it never degrades to self and never drops the
// review intent silently.
func TestClassifyRouteWithLLMReviewRequiredPromotionNonExecutableFailsClosed(t *testing.T) {
	provider := &fakeArbiterProvider{content: `{"decision":"team","scope":"single","review_required":true,"reason":"high stakes","preferred_owner":"","task_type":"research"}`}
	input := coordinatedRouteInput()
	input.Members = nil
	result := ClassifyRouteWithLLM(context.Background(), input, provider, "route-model", nil)

	if result.Decision != DecisionTeam || !result.NonExecutable {
		t.Fatalf("result=%+v, want promoted review request to fail closed as non-executable team, never self", result)
	}
	if result.WorkflowMode != WorkflowModeMultiRole || result.DegradedReasonCode != "insufficient_canonical_members" || result.WorkflowExecutable {
		t.Fatalf("non-executable state=%+v, want multi_role degraded with insufficient_canonical_members", result)
	}
	if result.BestTeamOwnerID != uuid.Nil || result.Plan != nil {
		t.Fatalf("non-executable promoted route retained owner/plan: %+v", result)
	}
}

// An explicit self decision is preserved as self even when the model also emits
// review_required — self never carries an effective review flag.
func TestClassifyRouteWithLLMSelfIgnoresReviewRequired(t *testing.T) {
	provider := &fakeArbiterProvider{content: `{"decision":"self","review_required":true,"reason":"direct answer","preferred_owner":"","task_type":"other"}`}
	input := coordinatedRouteInput()
	result := ClassifyRouteWithLLM(context.Background(), input, provider, "route-model", nil)

	if result.Decision != DecisionSelf || result.WorkflowMode != WorkflowModeSelf {
		t.Fatalf("result=%+v, want self to stay self despite review_required", result)
	}
	if result.RequestedReviewRequired || result.EffectiveReviewRequired {
		t.Fatalf("review flags=%v/%v, want both false on a self route", result.RequestedReviewRequired, result.EffectiveReviewRequired)
	}
}

func TestClassifyRouteWithLLMInvalidScopeFallsBackToSelf(t *testing.T) {
	provider := &fakeArbiterProvider{content: `{"decision":"team","scope":"parallel","reason":"bad scope","preferred_owner":"researcher","task_type":"research"}`}
	input := coordinatedRouteInput()
	result := ClassifyRouteWithLLM(context.Background(), input, provider, "route-model", nil)

	if result.Decision != DecisionSelf || result.DecisionBeforeValidation != DecisionSelf {
		t.Fatalf("result=%+v, want invalid scope treated as parse uncertainty and routed to self", result)
	}
	if result.DegradedReasonCode != "classifier_parse_failed" || result.WorkflowMode != WorkflowModeSelf || result.NonExecutable {
		t.Fatalf("invalid scope fallback state=%+v, want parse-failed self", result)
	}
}

func TestClassifyRouteWithLLMCoordinatedNonExecutableStaysTeam(t *testing.T) {
	cases := []struct {
		name   string
		mutate func(*Input)
		reason string
	}{
		{"missing coordinator", func(input *Input) { input.CoordinatorAgentID = uuid.Nil; input.CoordinatorAgentKey = "" }, "canonical_coordinator_unavailable"},
		{"no canonical members", func(input *Input) { input.Members = nil }, "insufficient_canonical_members"},
		{"required tool unavailable", func(input *Input) { input.CurrentAgent.AvailableTools = []string{"read_file"} }, "required_tool_unavailable"},
		{"member request path unavailable", func(input *Input) {
			input.TeamRole = "member"
			input.CanAssignTeamTasks = false
			input.MemberRequestsEnabled = false
			input.MemberRequestsAutoDispatch = false
		}, "member_request_path_unavailable"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			provider := &fakeArbiterProvider{content: `{"decision":"team","scope":"coordinated","reason":"parallel work","preferred_owner":"","task_type":"research"}`}
			input := coordinatedRouteInput()
			tc.mutate(&input)
			result := ClassifyRouteWithLLM(context.Background(), input, provider, "route-model", nil)

			if result.Decision != DecisionTeam || !result.NonExecutable {
				t.Fatalf("result=%+v, want non-executable team decision, never self", result)
			}
			if result.WorkflowMode != WorkflowModeMultiRole || result.DegradedReasonCode != tc.reason || result.WorkflowExecutable {
				t.Fatalf("non-executable state=%+v, want multi_role degraded with reason %q", result, tc.reason)
			}
			if result.BestTeamOwnerID != uuid.Nil || result.Plan != nil {
				t.Fatalf("non-executable route retained owner/plan: %+v", result)
			}
		})
	}
}

func TestClassifyRouteWithLLMTransportFailureStillSelf(t *testing.T) {
	provider := &fakeArbiterProvider{err: errors.New("provider down")}
	input := coordinatedRouteInput()
	result := ClassifyRouteWithLLM(context.Background(), input, provider, "route-model", nil)

	if result.Decision != DecisionSelf || result.NonExecutable {
		t.Fatalf("result=%+v, want transport failure to fall back to self", result)
	}
	if result.DegradedReasonCode != "classifier_transport_failed" {
		t.Fatalf("reason=%q, want classifier_transport_failed", result.DegradedReasonCode)
	}
}

func TestClassifyRouteWithLLMParseFailureStillSelf(t *testing.T) {
	provider := &fakeArbiterProvider{content: `not json at all`}
	input := coordinatedRouteInput()
	result := ClassifyRouteWithLLM(context.Background(), input, provider, "route-model", nil)

	if result.Decision != DecisionSelf || result.NonExecutable {
		t.Fatalf("result=%+v, want parse failure to fall back to self", result)
	}
	if result.DegradedReasonCode != "classifier_parse_failed" {
		t.Fatalf("reason=%q, want classifier_parse_failed", result.DegradedReasonCode)
	}
}

// The system prompt must contain the substance-based review decision boundary so
// the LLM has a concrete, testable criterion — not the vague old "decision stakes"
// trigger that produced false positives on routine business breadth.
func TestClassifierPromptContainsSubstanceReviewBoundary(t *testing.T) {
	msgs := buildRouteMessages(routeTestInput())
	if len(msgs) == 0 {
		t.Fatal("expected at least one message from buildRouteMessages")
	}
	system := msgs[0].Content
	for _, want := range []string{
		"Review is not a proxy for task breadth",
		"materially reduce the risk of a wrong result",
		"evidence is uncertain or conflicting",
		"correctness has high consequences",
		"acceptance criteria require verification",
		"hard to reverse external action",
		"do not by themselves require review",
		"Routine low stakes planning",
		"If a single-specialist request needs review, choose coordinated",
	} {
		if !strings.Contains(system, want) {
			t.Errorf("system prompt missing substance review boundary %q", want)
		}
	}
	if strings.Contains(system, "Set it true when decision stakes") {
		t.Error("system prompt still contains the old vague decision-stakes trigger")
	}
}

// A coordinated route where the LLM chose review_required=false must preserve
// that decision faithfully — routing must never inject review the model did not
// choose, because breadth or multiple deliverables alone do not justify it.
func TestClassifyRoutePreservesReviewRequiredFalse(t *testing.T) {
	provider := &fakeArbiterProvider{content: `{"decision":"team","scope":"coordinated","review_required":false,"reason":"routine multi-deliverable work","preferred_owner":"","task_type":"research"}`}
	input := coordinatedRouteInput()
	result := ClassifyRouteWithLLM(context.Background(), input, provider, "route-model", nil)

	if len(provider.requests) != 1 {
		t.Fatalf("calls=%d, want exactly one", len(provider.requests))
	}
	if result.EffectiveWorkflowMode != WorkflowModeMultiRole {
		t.Fatalf("workflow_mode=%q, want multi_role", result.EffectiveWorkflowMode)
	}
	if result.RequestedReviewRequired || result.EffectiveReviewRequired {
		t.Fatalf("review flags=%v/%v, want both false — routing must not inject review the LLM did not choose",
			result.RequestedReviewRequired, result.EffectiveReviewRequired)
	}
}

// A coordinated route where the LLM chose review_required=true on the direct
// path (no scope promotion) must preserve that decision faithfully.
func TestClassifyRoutePreservesReviewRequiredTrue(t *testing.T) {
	provider := &fakeArbiterProvider{content: `{"decision":"team","scope":"coordinated","review_required":true,"reason":"conflicting evidence, high consequences","preferred_owner":"","task_type":"research"}`}
	input := coordinatedRouteInput()
	result := ClassifyRouteWithLLM(context.Background(), input, provider, "route-model", nil)

	if len(provider.requests) != 1 {
		t.Fatalf("calls=%d, want exactly one", len(provider.requests))
	}
	if result.EffectiveWorkflowMode != WorkflowModeMultiRole {
		t.Fatalf("workflow_mode=%q, want multi_role", result.EffectiveWorkflowMode)
	}
	if !result.RequestedReviewRequired || !result.EffectiveReviewRequired {
		t.Fatalf("review flags=%v/%v, want both true — routing must preserve the LLM review decision",
			result.RequestedReviewRequired, result.EffectiveReviewRequired)
	}
}
