package agent

import (
	"context"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/nextlevelbuilder/goclaw/internal/pipeline"
	"github.com/nextlevelbuilder/goclaw/internal/providers"
	"github.com/nextlevelbuilder/goclaw/internal/tools"
)

func TestTeamWorkDirectivePromptRequiresWorkflowTool(t *testing.T) {
	prompt := buildTeamWorkDirectivePrompt(&TeamWorkDirective{
		Mode:            "team",
		Source:          "llm",
		Reason:          "requires strategy and content members",
		OriginalMessage: "lập kế hoạch chiến dịch",
		RequiredTool:    "team_tasks",
		WorkflowHint:    `Use team_tasks(action="create", task_type="request") because this agent is a member requesting help.`,
	})
	for _, want := range []string{"TEAM WORK ROUTING LOCK", "team_tasks", "must not complete", "requires strategy and content members", `task_type="request"`} {
		if !strings.Contains(prompt, want) {
			t.Fatalf("prompt missing %q:\n%s", want, prompt)
		}
	}
}

func TestTeamWorkDirectiveMultiRoleLeadRequiresWorkflowAction(t *testing.T) {
	directive := &TeamWorkDirective{Mode: "team", RequiredTool: "team_tasks", WorkflowMode: "multi_role", CanAssignTeamTasks: true, CoordinatorAgentKey: "lead-agent", PlanConstraint: &tools.TeamWorkPlanConstraint{PlanHash: strings.Repeat("a", 64)}}
	step := teamWorkDirectiveNextStep(directive, []providers.Message{
		{Role: "user", Content: "do multi role work"},
		{Role: "assistant", ToolCalls: []providers.ToolCall{{ID: "search-1", Name: "team_tasks", Arguments: map[string]any{"action": "search"}}}},
		{Role: "tool", ToolCallID: "search-1", Content: `{"count":0,"tasks":[]}`},
	})
	if step.RequiredAction != "get_or_create_workflow" || step.Satisfied {
		t.Fatalf("step = %+v", step)
	}
	if !teamWorkDirectiveResponseAdvances(step, &providers.ChatResponse{ToolCalls: []providers.ToolCall{{Name: "team_tasks", Arguments: map[string]any{"action": "create_workflow"}}}}) {
		t.Fatal("create_workflow must advance multi-role lead directive")
	}
	regularGet := teamWorkDirectiveNextStep(directive, []providers.Message{
		{Role: "user", Content: "do multi role work"},
		{Role: "assistant", ToolCalls: []providers.ToolCall{{ID: "search-1", Name: "team_tasks", Arguments: map[string]any{"action": "search"}}}},
		{Role: "tool", ToolCallID: "search-1", Content: `{"count":1,"tasks":[{}]}`},
		{Role: "assistant", ToolCalls: []providers.ToolCall{{ID: "get-1", Name: "team_tasks", Arguments: map[string]any{"action": "get", "task_id": uuid.NewString()}}}},
		{Role: "tool", ToolCallID: "get-1", Content: `{"task":{}}`},
	})
	if regularGet.Satisfied || regularGet.RequiredAction != "get_or_create_workflow" {
		t.Fatalf("single task get must not satisfy multi-role workflow: %+v", regularGet)
	}
}

func TestTeamWorkDirectiveMultiRoleMemberLocksCoordinator(t *testing.T) {
	directive := &TeamWorkDirective{Mode: "team", RequiredTool: "team_tasks", WorkflowMode: "multi_role", TeamRole: "member", MemberRequestsEnabled: true, CoordinatorAgentKey: "team-lead", PlanConstraint: &tools.TeamWorkPlanConstraint{PlanHash: strings.Repeat("b", 64)}}
	step := teamWorkDirectiveNextStep(directive, []providers.Message{
		{Role: "user", Content: "do multi role work"},
		{Role: "assistant", ToolCalls: []providers.ToolCall{{ID: "search-1", Name: "team_tasks", Arguments: map[string]any{"action": "search"}}}},
		{Role: "tool", ToolCallID: "search-1", Content: `{"count":0,"tasks":[]}`},
	})
	if step.RequiredAction != "get_or_create_workflow_request" || step.ExpectedOwner != "team-lead" {
		t.Fatalf("step = %+v", step)
	}
	wrong := &providers.ChatResponse{ToolCalls: []providers.ToolCall{{Name: "team_tasks", Arguments: map[string]any{"action": "create", "task_type": "request", "assignee": "specialist"}}}}
	if teamWorkDirectiveResponseAdvances(step, wrong) {
		t.Fatal("wrong coordinator must not advance directive")
	}
	correct := &providers.ChatResponse{ToolCalls: []providers.ToolCall{{Name: "team_tasks", Arguments: map[string]any{"action": "create", "task_type": "request", "assignee": "team-lead"}}}}
	if !teamWorkDirectiveResponseAdvances(step, correct) {
		t.Fatal("canonical coordinator request must advance directive")
	}
}

func TestTeamWorkDirectiveRetriesWhenRequiredToolMissing(t *testing.T) {
	directive := &TeamWorkDirective{Mode: "team", RequiredTool: "team_tasks"}
	if !teamWorkDirectiveNeedsRetry(directive, 0, &providers.ChatResponse{Content: "em tự làm xong rồi"}) {
		t.Fatal("text-only first response should require retry")
	}
	if teamWorkDirectiveNeedsRetry(directive, 1, &providers.ChatResponse{Content: "em tự làm xong rồi"}) {
		t.Fatal("second iteration should not retry workflow directive")
	}
	if !teamWorkDirectiveNeedsRetry(directive, 0, &providers.ChatResponse{ToolCalls: []providers.ToolCall{{Name: "team_tasks"}}}) {
		t.Fatal("team_tasks without required action should still retry")
	}
	if teamWorkDirectiveNeedsRetry(directive, 0, &providers.ChatResponse{ToolCalls: []providers.ToolCall{{Name: "team_tasks", Arguments: map[string]any{"action": "search"}}}}) {
		t.Fatal("response with required team_tasks search action should not retry")
	}
}

func TestTeamWorkDirectiveRetryRequestRequiresToolChoice(t *testing.T) {
	directive := &TeamWorkDirective{Mode: "delegate", RequiredTool: "delegate"}
	req := providers.ChatRequest{
		Messages: []providers.Message{{Role: "user", Content: "làm việc này theo link"}},
		Options:  map[string]any{"existing": true},
	}
	retry := buildTeamWorkDirectiveRetryRequest(req, directive)
	if retry.Options[providers.OptToolChoice] != "required" {
		t.Fatalf("tool_choice = %v, want required", retry.Options[providers.OptToolChoice])
	}
	if retry.Options["existing"] != true {
		t.Fatalf("existing option was not preserved: %+v", retry.Options)
	}
	if len(retry.Messages) != 2 || !strings.Contains(retry.Messages[1].Content, "delegate") {
		t.Fatalf("retry messages not reinforced: %+v", retry.Messages)
	}
}

func TestTeamWorkDirectiveRequiresSearchBeforeAnyTeamTask(t *testing.T) {
	directive := &TeamWorkDirective{Mode: "team", RequiredTool: "team_tasks"}
	step := teamWorkDirectiveNextStep(directive, nil)
	if step.Satisfied {
		t.Fatal("team workflow should not be satisfied before any team_tasks action")
	}
	if step.RequiredTool != "team_tasks" || step.RequiredAction != "search" {
		t.Fatalf("step = %+v, want team_tasks search", step)
	}
}

func TestTeamWorkDirectiveSearchResultAllowsGetOrCreateGeneral(t *testing.T) {
	directive := &TeamWorkDirective{Mode: "team", RequiredTool: "team_tasks", CanAssignTeamTasks: true}
	messages := []providers.Message{
		{Role: "assistant", ToolCalls: []providers.ToolCall{{ID: "call_search", Name: "team_tasks", Arguments: map[string]any{"action": "search", "query": "claude reseller"}}}},
		{Role: "tool", ToolCallID: "call_search", Content: `{"count":1,"tasks":[{"identifier":"T-001","subject":"existing research"}]}`},
	}
	step := teamWorkDirectiveNextStep(directive, messages)
	if step.Satisfied {
		t.Fatal("search alone must not satisfy team workflow when matching tasks exist")
	}
	if step.RequiredAction != "get_or_create_general" {
		t.Fatalf("step = %+v, want get_or_create_general after search finds existing task", step)
	}
}

func TestTeamWorkDirectiveExistingTaskUseSatisfiesWorkflow(t *testing.T) {
	directive := &TeamWorkDirective{Mode: "team", RequiredTool: "team_tasks"}
	messages := []providers.Message{
		{Role: "assistant", ToolCalls: []providers.ToolCall{{ID: "call_search", Name: "team_tasks", Arguments: map[string]any{"action": "search", "query": "claude reseller"}}}},
		{Role: "tool", ToolCallID: "call_search", Content: `{"count":1,"tasks":[{"identifier":"T-001"}]}`},
		{Role: "assistant", ToolCalls: []providers.ToolCall{{ID: "call_get", Name: "team_tasks", Arguments: map[string]any{"action": "get", "task_id": "T-001"}}}},
	}
	step := teamWorkDirectiveNextStep(directive, messages)
	if !step.Satisfied {
		t.Fatalf("get after search should satisfy team workflow, got %+v", step)
	}
}

func TestTeamWorkDirectiveNoSearchResultsRequiresCreateGeneralForLead(t *testing.T) {
	directive := &TeamWorkDirective{Mode: "team", RequiredTool: "team_tasks", CanAssignTeamTasks: true}
	messages := []providers.Message{
		{Role: "assistant", ToolCalls: []providers.ToolCall{{ID: "call_search", Name: "team_tasks", Arguments: map[string]any{"action": "search", "query": "new task"}}}},
		{Role: "tool", ToolCallID: "call_search", Content: `{"count":0,"tasks":[]}`},
	}
	step := teamWorkDirectiveNextStep(directive, messages)
	if step.Satisfied {
		t.Fatal("empty search must be followed by create/request, not treated as complete")
	}
	if step.RequiredAction != "create_general" {
		t.Fatalf("step = %+v, want create_general after empty search", step)
	}
}

func TestTeamWorkDirectiveUnknownSearchResultDoesNotForceBlindGet(t *testing.T) {
	directive := &TeamWorkDirective{Mode: "team", RequiredTool: "team_tasks"}
	messages := []providers.Message{
		{Role: "assistant", ToolCalls: []providers.ToolCall{{ID: "call_search", Name: "team_tasks", Arguments: map[string]any{"action": "search", "query": "research"}}}},
		{Role: "tool", ToolCallID: "call_search", Content: `Search completed, but no structured count was returned.`},
	}
	step := teamWorkDirectiveNextStep(directive, messages)
	if step.Satisfied {
		t.Fatal("unstructured search result must not satisfy team workflow")
	}
	if step.RequiredAction == "get" {
		t.Fatalf("step = %+v, must not force get without parsed count or task id", step)
	}
	if step.RequiredAction != "create_general" {
		t.Fatalf("step = %+v, want create_general after unstructured search result", step)
	}
}

func TestTeamWorkDirectiveResponseMustAdvanceRequiredAction(t *testing.T) {
	directive := &TeamWorkDirective{Mode: "team", RequiredTool: "team_tasks"}
	step := teamWorkDirectiveNextStep(directive, nil)
	if teamWorkDirectiveResponseAdvances(step, &providers.ChatResponse{Content: "em tự làm luôn"}) {
		t.Fatal("text-only response must not advance required team workflow action")
	}
	if teamWorkDirectiveResponseAdvances(step, &providers.ChatResponse{ToolCalls: []providers.ToolCall{{Name: "web_search"}}}) {
		t.Fatal("non-team tool must not advance required team workflow action")
	}
	if !teamWorkDirectiveResponseAdvances(step, &providers.ChatResponse{ToolCalls: []providers.ToolCall{{Name: "team_tasks", Arguments: map[string]any{"action": "search"}}}}) {
		t.Fatal("team_tasks search should advance the first required action")
	}
}

func TestTeamWorkDirectiveRejectsActionRequestAsCreateShortcut(t *testing.T) {
	directive := &TeamWorkDirective{Mode: "team", RequiredTool: "team_tasks"}
	messages := []providers.Message{
		{Role: "assistant", ToolCalls: []providers.ToolCall{{ID: "call_search", Name: "team_tasks", Arguments: map[string]any{"action": "search", "query": "research"}}}},
		{Role: "tool", ToolCallID: "call_search", Content: `{"count":0,"tasks":[]}`},
		{Role: "assistant", ToolCalls: []providers.ToolCall{{ID: "call_request", Name: "team_tasks", Arguments: map[string]any{"action": "request", "subject": "research"}}}},
	}
	step := teamWorkDirectiveNextStep(directive, messages)
	if step.Satisfied {
		t.Fatal(`team_tasks action="request" must not satisfy the create/request workflow`)
	}
	if step.RequiredAction != "create_general" {
		t.Fatalf("step = %+v, want create_general after invalid request action", step)
	}

	createStep := teamWorkDirectiveStep{RequiredTool: "team_tasks", RequiredAction: "create_request"}
	if teamWorkDirectiveResponseAdvances(createStep, &providers.ChatResponse{ToolCalls: []providers.ToolCall{{Name: "team_tasks", Arguments: map[string]any{"action": "request"}}}}) {
		t.Fatal(`team_tasks action="request" must not advance create step`)
	}
	if !teamWorkDirectiveResponseAdvances(createStep, &providers.ChatResponse{ToolCalls: []providers.ToolCall{{Name: "team_tasks", Arguments: map[string]any{"action": "create", "task_type": "request"}}}}) {
		t.Fatal(`team_tasks action="create" task_type="request" should advance create step`)
	}
}

func TestTeamWorkDirectiveRetryInstructionIsInternalNotUserFacing(t *testing.T) {
	directive := &TeamWorkDirective{Mode: "team", RequiredTool: "team_tasks"}
	step := teamWorkDirectiveNextStep(directive, nil)
	retry := buildTeamWorkDirectiveStepRequest(providers.ChatRequest{
		Messages: []providers.Message{{Role: "user", Content: "làm nghiên cứu này"}},
	}, directive, step, true)
	last := retry.Messages[len(retry.Messages)-1]
	if last.Role != "system" {
		t.Fatalf("retry instruction role = %q, want system", last.Role)
	}
	for _, forbidden := range []string{"explain the blocker", "If it is impossible", "user said"} {
		if strings.Contains(last.Content, forbidden) {
			t.Fatalf("retry instruction leaks user-facing/blocker wording %q:\n%s", forbidden, last.Content)
		}
	}
	if !strings.Contains(strings.ToLower(last.Content), "not a user message") {
		t.Fatalf("retry instruction should mark itself as internal, got:\n%s", last.Content)
	}
}

func TestTeamWorkDirectiveBlockerTextIsNotUserFacing(t *testing.T) {
	for _, output := range []string{"", "em sẽ tự xử lý tiếp cho anh"} {
		if strings.Contains(output, "model chưa gọi đúng công cụ team_tasks") || strings.Contains(output, "Nhiệm vụ chưa được chuyển đúng workflow") {
			t.Fatalf("technical blocker leaked to chat output: %q", output)
		}
	}
}

func TestTeamWorkDirectiveToolErrorDoesNotSatisfyWorkflow(t *testing.T) {
	directive := &TeamWorkDirective{Mode: "team", RequiredTool: "team_tasks"}
	messages := []providers.Message{
		{Role: "assistant", ToolCalls: []providers.ToolCall{{ID: "call_search", Name: "team_tasks", Arguments: map[string]any{"action": "search", "query": "research"}}}},
		{Role: "tool", ToolCallID: "call_search", Content: `{"count":0,"tasks":[]}`},
		{Role: "assistant", ToolCalls: []providers.ToolCall{{ID: "call_create", Name: "team_tasks", Arguments: map[string]any{"action": "create", "subject": "research", "assignee": "minh-strategy"}}}},
		{Role: "tool", ToolCallID: "call_create", IsError: true, Content: `Members can only create task_type="request". Use team_tasks(action="comment") to communicate.`},
	}
	step := teamWorkDirectiveNextStep(directive, messages)
	if step.Satisfied {
		t.Fatal("failed team_tasks create must not satisfy team workflow")
	}
	if step.RequiredAction != "create_general" {
		t.Fatalf("step = %+v, want create_general retry after failed create", step)
	}
}

func TestTeamWorkDirectiveMemberInstructionRequiresRequestTaskType(t *testing.T) {
	directive := &TeamWorkDirective{
		Mode:                       "team",
		RequiredTool:               "team_tasks",
		TeamRole:                   "member",
		MemberRequestsEnabled:      true,
		MemberRequestsAutoDispatch: true,
		BestTeamOwner:              "minh-strategy",
		WorkflowHint:               `As a team member, do not create or assign general team tasks. Use team_tasks(action="create", task_type="request", ...) to ask a teammate for help.`,
	}
	messages := []providers.Message{
		{Role: "assistant", ToolCalls: []providers.ToolCall{{ID: "call_search", Name: "team_tasks", Arguments: map[string]any{"action": "search", "query": "research"}}}},
		{Role: "tool", ToolCallID: "call_search", Content: `{"count":0,"tasks":[]}`},
	}
	step := teamWorkDirectiveNextStep(directive, messages)
	req := buildTeamWorkDirectiveStepRequest(providers.ChatRequest{
		Messages: []providers.Message{{Role: "user", Content: "nghiên cứu lại"}},
	}, directive, step, false)
	last := req.Messages[len(req.Messages)-1].Content
	for _, want := range []string{`task_type="request"`, "team member", "do not create or assign general team tasks"} {
		if !strings.Contains(last, want) {
			t.Fatalf("member workflow instruction missing %q:\n%s", want, last)
		}
	}
}

func TestTeamWorkDirectiveMemberNoSearchResultsRequiresCreateRequest(t *testing.T) {
	directive := &TeamWorkDirective{Mode: "team", RequiredTool: "team_tasks", TeamRole: "member", MemberRequestsEnabled: true, MemberRequestsAutoDispatch: true, BestTeamOwner: "minh-strategy"}
	messages := []providers.Message{
		{Role: "assistant", ToolCalls: []providers.ToolCall{{ID: "call_search", Name: "team_tasks", Arguments: map[string]any{"action": "search", "query": "gold research"}}}},
		{Role: "tool", ToolCallID: "call_search", Content: `{"count":0,"tasks":[]}`},
	}
	step := teamWorkDirectiveNextStep(directive, messages)
	if step.RequiredAction != "create_request" {
		t.Fatalf("step = %+v, want create_request", step)
	}
}

func TestTeamWorkDirectiveMemberSearchResultsAllowGetOrCreateRequest(t *testing.T) {
	directive := &TeamWorkDirective{Mode: "team", RequiredTool: "team_tasks", TeamRole: "member", MemberRequestsEnabled: true, MemberRequestsAutoDispatch: true, BestTeamOwner: "minh-strategy"}
	messages := []providers.Message{
		{Role: "assistant", ToolCalls: []providers.ToolCall{{ID: "call_search", Name: "team_tasks", Arguments: map[string]any{"action": "search", "query": "gold research"}}}},
		{Role: "tool", ToolCallID: "call_search", Content: `{"count":2,"tasks":[{"identifier":"old"}]}`},
	}
	step := teamWorkDirectiveNextStep(directive, messages)
	if step.RequiredAction != "get_or_create_request" {
		t.Fatalf("step = %+v, want get_or_create_request", step)
	}
	if teamWorkDirectiveResponseAdvances(step, &providers.ChatResponse{ToolCalls: []providers.ToolCall{{Name: "team_tasks", Arguments: map[string]any{"action": "assign", "assignee": "minh-strategy"}}}}) {
		t.Fatal("member request path must not advance assign")
	}
	if teamWorkDirectiveResponseAdvances(step, &providers.ChatResponse{ToolCalls: []providers.ToolCall{{Name: "team_tasks", Arguments: map[string]any{"action": "create", "assignee": "minh-strategy"}}}}) {
		t.Fatal("member request path must not advance general create without task_type=request")
	}
	if teamWorkDirectiveResponseAdvances(step, &providers.ChatResponse{ToolCalls: []providers.ToolCall{{Name: "team_tasks", Arguments: map[string]any{"action": "create", "task_type": "request", "assignee": "ngoc-analyst"}}}}) {
		t.Fatal("member request path must not advance create task_type=request with the wrong assignee")
	}
	if !teamWorkDirectiveResponseAdvances(step, &providers.ChatResponse{ToolCalls: []providers.ToolCall{{Name: "team_tasks", Arguments: map[string]any{"action": "create", "task_type": "request", "assignee": "minh-strategy"}}}}) {
		t.Fatal("member request path should advance create task_type=request")
	}
}

func TestTeamWorkDirectiveIgnoresStaleHistoryBeforeCurrentUser(t *testing.T) {
	directive := &TeamWorkDirective{
		Mode:                       "team",
		RequiredTool:               "team_tasks",
		OriginalMessage:            "nghiên cứu vàng trong nước",
		TeamRole:                   "member",
		MemberRequestsEnabled:      true,
		MemberRequestsAutoDispatch: true,
		BestTeamOwner:              "minh-strategy",
	}
	messages := []providers.Message{
		{Role: "user", Content: "task cũ"},
		{Role: "assistant", ToolCalls: []providers.ToolCall{{ID: "old_search", Name: "team_tasks", Arguments: map[string]any{"action": "search", "query": "old"}}}},
		{Role: "tool", ToolCallID: "old_search", Content: `{"count":0,"tasks":[]}`},
		{Role: "assistant", ToolCalls: []providers.ToolCall{{ID: "old_create", Name: "team_tasks", Arguments: map[string]any{"action": "create", "task_type": "request", "assignee": "minh-strategy"}}}},
		{Role: "tool", ToolCallID: "old_create", Content: `{"id":"old"}`},
		{Role: "user", Content: "nghiên cứu vàng trong nước"},
	}
	step := teamWorkDirectiveNextStep(directive, messages)
	if step.Satisfied {
		t.Fatalf("stale history must not satisfy new directive: %+v", step)
	}
	if step.RequiredAction != "search" {
		t.Fatalf("step = %+v, want current-turn search", step)
	}

	messages = append(messages,
		providers.Message{Role: "assistant", ToolCalls: []providers.ToolCall{{ID: "new_search", Name: "team_tasks", Arguments: map[string]any{"action": "search", "query": "gold"}}}},
		providers.Message{Role: "tool", ToolCallID: "new_search", Content: `{"count":0,"tasks":[]}`},
	)
	step = teamWorkDirectiveNextStep(directive, messages)
	if step.RequiredAction != "create_request" {
		t.Fatalf("step = %+v, want create_request after current-turn search", step)
	}
}

func TestTeamWorkDirectiveFilteredAllowedToolsBlockExtraToolExecution(t *testing.T) {
	l := &Loop{}
	gate := l.makeAuthorizeToolCall()
	state := &pipeline.RunState{Tool: pipeline.ToolState{AllowedTools: map[string]bool{"team_tasks": true}}}
	if ok, reason := gate(context.Background(), state, providers.ToolCall{Name: "team_tasks"}); !ok {
		t.Fatalf("team_tasks should be allowed, reason=%q", reason)
	}
	if ok, _ := gate(context.Background(), state, providers.ToolCall{Name: "web_search"}); ok {
		t.Fatal("web_search must not execute when directive request exposed only team_tasks")
	}
}

func TestTeamWorkDirectiveHardAllowlistBlocksDeferredMCPActivation(t *testing.T) {
	registry := tools.NewRegistry()
	activated := false
	registry.SetDeferredActivator(func(name string) bool {
		activated = true
		registry.Register(&mockExecTool{name: name})
		return true
	})
	loop := &Loop{tools: registry}
	gate := loop.makeAuthorizeToolCall()
	state := &pipeline.RunState{Tool: pipeline.ToolState{
		AllowedTools:      map[string]bool{"team_tasks": true},
		HardToolAllowlist: true,
	}}
	if ok, _ := gate(context.Background(), state, providers.ToolCall{Name: "mcp_svc__research"}); ok {
		t.Fatal("deferred MCP must not bypass a hard routing allowlist")
	}
	if activated {
		t.Fatal("hard routing allowlist must reject before deferred MCP activation")
	}
}

func TestTeamWorkDirectiveCreateRequestSuccessTerminatesRequesterTurn(t *testing.T) {
	l := &Loop{}
	rs := &runState{}
	ptd := tools.NewPendingTeamDispatch()
	ptd.MarkListed()
	ctx := tools.WithPendingTeamDispatch(context.Background(), ptd)
	req := &RunRequest{TeamWorkDirective: &TeamWorkDirective{
		Mode:                       "team",
		RequiredTool:               "team_tasks",
		TeamRole:                   "member",
		MemberRequestsEnabled:      true,
		MemberRequestsAutoDispatch: true,
		BestTeamOwner:              "minh-strategy",
	}}
	emitted := 0
	_, _, action := l.processToolResult(
		ctx,
		rs,
		req,
		func(AgentEvent) { emitted++ },
		providers.ToolCall{Name: "team_tasks", Arguments: map[string]any{"action": "create", "task_type": "request", "assignee": "minh-strategy"}},
		"team_tasks",
		tools.NewResult(`{"task_id":"T-1"}`),
		false,
	)
	if action != toolResultBreak {
		t.Fatalf("action = %v, want toolResultBreak", action)
	}
	if !rs.stopAfterTool || !rs.suppressUserOutput || rs.loopKilled {
		t.Fatalf("stopAfterTool=%v suppressUserOutput=%v loopKilled=%v, want silent clean stop", rs.stopAfterTool, rs.suppressUserOutput, rs.loopKilled)
	}
	if strings.TrimSpace(rs.finalContent) != "" {
		t.Fatalf("handoff must not inject user-facing acknowledgement, got %q", rs.finalContent)
	}
	if emitted == 0 {
		t.Fatal("tool result event should still be emitted")
	}
}

func TestTeamWorkDirectiveCreateSuccessWithoutCurrentTurnListingDoesNotSuppress(t *testing.T) {
	l := &Loop{}
	rs := &runState{}
	req := &RunRequest{TeamWorkDirective: &TeamWorkDirective{
		Mode:               "team",
		RequiredTool:       "team_tasks",
		TeamRole:           "lead",
		CanAssignTeamTasks: true,
		BestTeamOwner:      "minh-strategy",
	}}
	_, _, action := l.processToolResult(
		context.Background(),
		rs,
		req,
		func(AgentEvent) {},
		providers.ToolCall{Name: "team_tasks", Arguments: map[string]any{"action": "create", "assignee": "minh-strategy"}},
		"team_tasks",
		tools.NewResult(`{"task_id":"T-1"}`),
		false,
	)
	if action == toolResultBreak || rs.stopAfterTool || rs.suppressUserOutput {
		t.Fatalf("create without current-turn listing must not suppress: action=%v stop=%v suppress=%v", action, rs.stopAfterTool, rs.suppressUserOutput)
	}
}

func TestTeamWorkDirectiveGeneralCreateSuccessTerminatesRequesterTurn(t *testing.T) {
	l := &Loop{}
	rs := &runState{}
	ptd := tools.NewPendingTeamDispatch()
	ptd.MarkListed()
	ctx := tools.WithPendingTeamDispatch(context.Background(), ptd)
	req := &RunRequest{TeamWorkDirective: &TeamWorkDirective{
		Mode:               "team",
		RequiredTool:       "team_tasks",
		TeamRole:           "lead",
		CanAssignTeamTasks: true,
		BestTeamOwner:      "minh-strategy",
	}}
	_, _, action := l.processToolResult(
		ctx,
		rs,
		req,
		func(AgentEvent) {},
		providers.ToolCall{Name: "team_tasks", Arguments: map[string]any{"action": "create", "assignee": "minh-strategy"}},
		"team_tasks",
		tools.NewResult(`{"task_id":"T-1"}`),
		false,
	)
	if action != toolResultBreak || !rs.stopAfterTool || !rs.suppressUserOutput || rs.loopKilled {
		t.Fatalf("general handoff action=%v stop=%v suppress=%v loopKilled=%v", action, rs.stopAfterTool, rs.suppressUserOutput, rs.loopKilled)
	}
}

func TestTeamWorkDirectiveCreateRequestSuccessRequiresExpectedOwner(t *testing.T) {
	l := &Loop{}
	rs := &runState{}
	ptd := tools.NewPendingTeamDispatch()
	ptd.MarkListed()
	ctx := tools.WithPendingTeamDispatch(context.Background(), ptd)
	req := &RunRequest{TeamWorkDirective: &TeamWorkDirective{
		Mode:                       "team",
		RequiredTool:               "team_tasks",
		TeamRole:                   "member",
		MemberRequestsEnabled:      true,
		MemberRequestsAutoDispatch: true,
		BestTeamOwner:              "minh-strategy",
	}}
	_, _, action := l.processToolResult(
		ctx,
		rs,
		req,
		func(AgentEvent) {},
		providers.ToolCall{Name: "team_tasks", Arguments: map[string]any{"action": "create", "task_type": "request", "assignee": "ngoc-analyst"}},
		"team_tasks",
		tools.NewResult(`{"task_id":"T-1"}`),
		false,
	)
	if action == toolResultBreak || rs.stopAfterTool {
		t.Fatalf("wrong owner must not terminate requester turn as successful handoff: action=%v stopAfterTool=%v", action, rs.stopAfterTool)
	}
}
