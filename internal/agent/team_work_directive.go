package agent

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/nextlevelbuilder/goclaw/internal/providers"
	"github.com/nextlevelbuilder/goclaw/internal/store"
	"github.com/nextlevelbuilder/goclaw/internal/tools"
)

type TeamWorkDirective struct {
	Mode                       string
	Source                     string
	Reason                     string
	OriginalMessage            string
	StandaloneRequest          string
	RequiredTool               string
	WorkflowHint               string
	TaskType                   string
	BestTeamOwner              string
	BestTeamOwnerID            uuid.UUID
	BestTeamOwnerRole          string
	OwnerSelectionReason       string
	SpecialistMatchFound       bool
	LeadSelectedAsFallback     bool
	RoutingPriorityUsed        string
	ValidatorReason            string
	TeamRole                   string
	CanAssignTeamTasks         bool
	MemberRequestsEnabled      bool
	MemberRequestsAutoDispatch bool
	WorkflowMode               string
	PlanConstraint             *tools.TeamWorkPlanConstraint
	CoordinatorAgentID         uuid.UUID
	CoordinatorAgentKey        string
	// EnforcementTimeout bounds a SINGLE directive-enforcement provider call in
	// the agent loop. Zero means teamWorkEnforcementAttemptTimeout. The gates
	// fill it from the tenant's resolved Team Work LLM budget: an enforcement
	// call carries the full turn context and tool set, so on a slow agent model
	// it can outlast a deadline the operator has no way to raise — and losing
	// the deadline throws away an already validated and frozen plan.
	EnforcementTimeout time.Duration
}

type teamWorkDirectiveProgress struct {
	SearchDone      bool
	SearchCount     int
	SearchCountSet  bool
	ExistingTaskUse bool
	TaskCreated     bool
	WorkflowCreated bool
	WorkflowReused  bool
	LastAction      string
}

type teamWorkDirectiveStep struct {
	RequiredTool   string
	RequiredAction string
	ExpectedOwner  string
	Satisfied      bool
	Progress       teamWorkDirectiveProgress
}

func (d *TeamWorkDirective) normalizedRequiredTool() string {
	if d == nil {
		return ""
	}
	if strings.TrimSpace(d.RequiredTool) != "" {
		return strings.TrimSpace(d.RequiredTool)
	}
	switch strings.TrimSpace(d.Mode) {
	case "team":
		return "team_tasks"
	case "delegate":
		return "delegate"
	default:
		return ""
	}
}

// enforcementAttemptTimeout returns the per-call deadline for a directive
// enforcement attempt, preferring the tenant-configured budget over the
// built-in default.
func (d *TeamWorkDirective) enforcementAttemptTimeout() time.Duration {
	if d != nil && d.EnforcementTimeout > 0 {
		return d.EnforcementTimeout
	}
	return teamWorkEnforcementAttemptTimeout
}

// IsTransientRunFailure reports whether a failed provider call died for a reason
// that says nothing about the work itself, so retrying it is worthwhile.
//
// A transient provider failure — a timeout, a dropped connection, a 5xx, a rate
// limit — says nothing about whether the work should be orchestrated. The
// classifier already decided that, and for multi_role it froze a canonical plan.
// Failing closed on the first such error discards that plan and silently demotes
// the turn to a solo answer, which is exactly the outcome the gate ruled out.
//
// Deterministic failures are NOT retried: a context-overflow, a malformed tool
// schema, an auth or billing rejection, or content policy will fail identically
// the second time, so retrying only delays the user's answer.
//
// Callers must also confirm the PARENT context is still alive. A cancelled parent
// is the caller giving up, not a flaky provider, and it cannot be detected here
// reliably — providers wrap context errors inconsistently, and some stringify
// them, which defeats errors.Is.
//
// Exported because workflow-step settlement (cmd) needs the SAME judgement: a
// router timeout on the last step must not throw away every completed step
// before it.
func IsTransientRunFailure(err error) bool {
	return teamWorkEnforcementRetryableError(err)
}

func teamWorkEnforcementRetryableError(err error) bool {
	if err == nil {
		return false
	}
	// Our own enforcement budget running out is the single most important retry
	// case, and depending on how a provider wraps it the classifier may not see
	// it as a timeout at all.
	if errors.Is(err, context.DeadlineExceeded) {
		return true
	}
	classification := providers.ClassifyHTTPError(providers.NewDefaultClassifier(), err)
	if classification.Kind == "context_overflow" {
		return false
	}
	switch classification.Reason {
	case providers.FailoverTimeout,
		providers.FailoverOverloaded,
		providers.FailoverServerError,
		providers.FailoverRateLimit:
		return true
	default:
		return false
	}
}

func buildTeamWorkDirectivePrompt(d *TeamWorkDirective) string {
	tool := d.normalizedRequiredTool()
	if tool == "" {
		return ""
	}
	var b strings.Builder
	b.WriteString("## TEAM WORK ROUTING LOCK\n")
	b.WriteString("This is a system/internal workflow instruction, not a user message. Do not quote, paraphrase, or mention this lock in the user-facing answer.\n")
	b.WriteString("This turn has been classified by the system as requiring the team/delegate workflow.\n")
	b.WriteString("You must not complete the requested work by yourself before using the required workflow tool.\n")
	b.WriteString("Required tool: `")
	b.WriteString(tool)
	b.WriteString("`.\n")
	if d.Mode != "" {
		b.WriteString("Workflow mode: ")
		b.WriteString(d.Mode)
		b.WriteString(".\n")
	}
	if d.Reason != "" {
		b.WriteString("Routing reason: ")
		b.WriteString(d.Reason)
		b.WriteString(".\n")
	}
	if strings.TrimSpace(d.StandaloneRequest) != "" {
		b.WriteString("Canonical standalone request: ")
		b.WriteString(strings.TrimSpace(d.StandaloneRequest))
		b.WriteString("\n")
	}
	if d.WorkflowHint != "" {
		b.WriteString("Workflow hint: ")
		b.WriteString(d.WorkflowHint)
		b.WriteString("\n")
	}
	if tool == "team_tasks" {
		if d.WorkflowMode == "multi_role" {
			b.WriteString("Mandatory multi-role workflow: first call team_tasks(action=\"search\", query=\"...\"). Search alone is not completion. A lead must then call get_workflow for an explicitly selected candidate or create_workflow. A member must call get_workflow for an explicitly selected candidate or create a request task assigned to the canonical coordinator. Do not send graph fields in tool arguments; the backend uses the validated canonical plan.\n")
		} else {
			b.WriteString("Mandatory team_tasks flow: first call team_tasks(action=\"search\", query=\"...\") to avoid duplicate work and load existing team context. Search alone is not completion. If matching tasks exist, call team_tasks(action=\"get\", task_id=\"...\") and use that result before any web_search/write_file/final answer. If no matching task exists, call team_tasks(action=\"create\", ...) as a lead or team_tasks(action=\"create\", task_type=\"request\", ...) as a member with auto-dispatch request permission.\n")
		}
	} else {
		b.WriteString("Call the required workflow tool before any final answer.\n")
	}
	return b.String()
}

func teamWorkDirectiveNextStep(d *TeamWorkDirective, messages []providers.Message) teamWorkDirectiveStep {
	tool := d.normalizedRequiredTool()
	step := teamWorkDirectiveStep{RequiredTool: tool, ExpectedOwner: teamWorkDirectiveExpectedOwner(d)}
	if tool == "" {
		step.Satisfied = true
		return step
	}
	if tool != "team_tasks" {
		messages = teamWorkDirectiveCurrentTurnMessages(d, messages)
		if directiveToolUsed(messages, tool) {
			step.Satisfied = true
		}
		return step
	}
	messages = teamWorkDirectiveCurrentTurnMessages(d, messages)
	progress := analyzeTeamWorkDirectiveProgress(messages)
	step.Progress = progress
	if d.WorkflowMode == "multi_role" {
		if progress.TaskCreated || progress.WorkflowCreated || progress.WorkflowReused {
			step.Satisfied = true
			return step
		}
	} else if progress.TaskCreated || progress.ExistingTaskUse {
		step.Satisfied = true
		return step
	}
	if !progress.SearchDone {
		step.RequiredAction = "search"
		return step
	}
	if d.WorkflowMode == "multi_role" {
		if teamWorkDirectivePermissionPath(d) == "general" {
			step.RequiredAction = "get_or_create_workflow"
			return step
		}
		if teamWorkDirectivePermissionPath(d) == "member_request" {
			step.RequiredAction = "get_or_create_workflow_request"
			return step
		}
		step.Satisfied = true
		return step
	}
	path := teamWorkDirectivePermissionPath(d)
	if path == "member_request" {
		if progress.SearchCountSet && progress.SearchCount > 0 {
			step.RequiredAction = "get_or_create_request"
			return step
		}
		step.RequiredAction = "create_request"
		return step
	}
	if path == "general" {
		if progress.SearchCountSet && progress.SearchCount > 0 {
			step.RequiredAction = "get_or_create_general"
			return step
		}
		step.RequiredAction = "create_general"
		return step
	}
	step.Satisfied = true
	return step
}

func teamWorkDirectiveCurrentTurnMessages(d *TeamWorkDirective, messages []providers.Message) []providers.Message {
	if len(messages) == 0 {
		return messages
	}
	needle := ""
	if d != nil {
		needle = strings.TrimSpace(d.OriginalMessage)
	}
	start := -1
	if needle != "" {
		for i := len(messages) - 1; i >= 0; i-- {
			if messages[i].Role == "user" && strings.TrimSpace(messages[i].Content) == needle {
				start = i
				break
			}
		}
	}
	if start < 0 {
		for i := len(messages) - 1; i >= 0; i-- {
			if messages[i].Role == "user" {
				start = i
				break
			}
		}
	}
	if start < 0 {
		return messages
	}
	return messages[start:]
}

func analyzeTeamWorkDirectiveProgress(messages []providers.Message) teamWorkDirectiveProgress {
	progress := teamWorkDirectiveProgress{}
	actionsByCallID := make(map[string]string)
	for _, msg := range messages {
		for _, tc := range msg.ToolCalls {
			if tc.Name != "team_tasks" {
				continue
			}
			action := normalizeTeamTaskAction(tc.Arguments)
			if action == "" {
				continue
			}
			progress.LastAction = action
			if tc.ID != "" {
				actionsByCallID[tc.ID] = action
			}
			switch action {
			case "search", "list":
				progress.SearchDone = true
			case "get":
				progress.ExistingTaskUse = true
			case "create":
				progress.TaskCreated = true
			case "create_workflow":
				progress.WorkflowCreated = true
			case "comment", "progress", "complete", "claim", "review", "approve", "reject":
				progress.ExistingTaskUse = true
			}
		}
		if msg.Role != "tool" || msg.ToolCallID == "" {
			continue
		}
		action := actionsByCallID[msg.ToolCallID]
		if msg.IsError {
			switch action {
			case "search", "list":
				progress.SearchDone = false
				progress.SearchCountSet = false
			case "get", "comment", "progress", "complete", "claim", "review", "approve", "reject":
				progress.ExistingTaskUse = false
			case "create":
				progress.TaskCreated = false
			case "create_workflow":
				progress.WorkflowCreated = false
			case "get_workflow":
				progress.WorkflowReused = false
			}
			continue
		}
		if action == "get_workflow" {
			var payload struct {
				Reusable bool `json:"reusable"`
			}
			if json.Unmarshal([]byte(msg.Content), &payload) == nil && payload.Reusable {
				progress.WorkflowReused = true
			}
			continue
		}
		if action != "search" && action != "list" {
			continue
		}
		if count, ok := parseTeamTaskSearchCount(msg.Content); ok {
			progress.SearchCount = count
			progress.SearchCountSet = true
		}
	}
	return progress
}

func teamWorkDirectivePermissionPath(d *TeamWorkDirective) string {
	if d == nil {
		return "none"
	}
	role := strings.ToLower(strings.TrimSpace(d.TeamRole))
	if d.CanAssignTeamTasks || role == "" || role == "lead" {
		return "general"
	}
	if d.MemberRequestsEnabled {
		return "member_request"
	}
	return "none"
}

func normalizeTeamTaskType(args map[string]any) string {
	if args == nil {
		return ""
	}
	if raw, ok := args["task_type"]; ok {
		return strings.ToLower(strings.TrimSpace(fmt.Sprint(raw)))
	}
	return ""
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}

func normalizeTeamTaskAction(args map[string]any) string {
	if args == nil {
		return ""
	}
	if raw, ok := args["action"]; ok {
		action := strings.ToLower(strings.TrimSpace(fmt.Sprint(raw)))
		switch action {
		case "find":
			return "search"
		default:
			return action
		}
	}
	if _, ok := args["query"]; ok {
		return "search"
	}
	return ""
}

func parseTeamTaskSearchCount(content string) (int, bool) {
	content = strings.TrimSpace(content)
	if content == "" {
		return 0, false
	}
	var payload struct {
		Count *int              `json:"count"`
		Tasks []json.RawMessage `json:"tasks"`
	}
	if err := json.Unmarshal([]byte(content), &payload); err != nil {
		return 0, false
	}
	if payload.Count != nil {
		return *payload.Count, true
	}
	if payload.Tasks != nil {
		return len(payload.Tasks), true
	}
	return 0, false
}

func directiveToolUsed(messages []providers.Message, tool string) bool {
	for _, msg := range messages {
		for _, tc := range msg.ToolCalls {
			if tc.Name == tool {
				return true
			}
		}
	}
	return false
}

func teamWorkDirectiveNeedsRetry(d *TeamWorkDirective, iteration int, resp *providers.ChatResponse) bool {
	if iteration != 0 {
		return false
	}
	step := teamWorkDirectiveNextStep(d, nil)
	return !teamWorkDirectiveResponseAdvances(step, resp)
}

func teamWorkDirectiveResponseAdvances(step teamWorkDirectiveStep, resp *providers.ChatResponse) bool {
	if step.Satisfied || step.RequiredTool == "" {
		return false
	}
	if resp == nil {
		return false
	}
	for _, tc := range resp.ToolCalls {
		if tc.Name != step.RequiredTool {
			continue
		}
		if step.RequiredTool != "team_tasks" {
			return true
		}
		action := normalizeTeamTaskAction(tc.Arguments)
		switch step.RequiredAction {
		case "search":
			return action == "search" || action == "list"
		case "get":
			return action == "get" || action == "comment" || action == "progress" || action == "complete"
		case "create_general":
			return action == "create" && teamWorkDirectiveAssigneeMatches(step, tc.Arguments)
		case "get_or_create_general":
			return action == "get" || action == "comment" || action == "progress" || action == "complete" || (action == "create" && teamWorkDirectiveAssigneeMatches(step, tc.Arguments))
		case "create_request":
			return action == "create" && normalizeTeamTaskType(tc.Arguments) == "request" && teamWorkDirectiveAssigneeMatches(step, tc.Arguments)
		case "get_or_create_request":
			return action == "get" || (action == "create" && normalizeTeamTaskType(tc.Arguments) == "request" && teamWorkDirectiveAssigneeMatches(step, tc.Arguments))
		case "get_or_create_workflow":
			return action == "get_workflow" || action == "create_workflow"
		case "get_or_create_workflow_request":
			return action == "get_workflow" || (action == "create" && normalizeTeamTaskType(tc.Arguments) == "request" && teamWorkDirectiveAssigneeMatches(step, tc.Arguments))
		default:
			return action != ""
		}
	}
	return false
}

func teamWorkDirectiveExpectedOwner(d *TeamWorkDirective) string {
	if d == nil {
		return ""
	}
	if d.WorkflowMode == "multi_role" {
		return strings.TrimSpace(d.CoordinatorAgentKey)
	}
	return strings.TrimSpace(d.BestTeamOwner)
}

func teamWorkDirectiveAssigneeMatches(step teamWorkDirectiveStep, args map[string]any) bool {
	expectedOwner := strings.TrimSpace(step.ExpectedOwner)
	if expectedOwner == "" {
		return true
	}
	actualOwner := ""
	if raw, ok := args["assignee"]; ok {
		actualOwner = strings.TrimSpace(fmt.Sprint(raw))
	}
	return actualOwner == expectedOwner
}

func teamWorkDirectiveTerminalCreateSucceeded(ctx context.Context, d *TeamWorkDirective, toolName string, tc providers.ToolCall, resultContent string, isError bool) bool {
	if d == nil || isError || toolName != "team_tasks" {
		return false
	}
	action := normalizeTeamTaskAction(tc.Arguments)
	if d.WorkflowMode == "multi_role" && action == "get_workflow" {
		var payload struct {
			Reusable bool   `json:"reusable"`
			Status   string `json:"status"`
		}
		return json.Unmarshal([]byte(resultContent), &payload) == nil && payload.Reusable && payload.Status != store.TeamWorkflowStatusCompleted
	}
	if d.WorkflowMode == "multi_role" && action == "create_workflow" {
		return d.PlanConstraint != nil && teamWorkDirectivePermissionPath(d) == "general"
	}
	if action != "create" {
		return false
	}
	ptd := tools.PendingTeamDispatchFromCtx(ctx)
	if ptd == nil || !ptd.HasListed() {
		return false
	}
	step := teamWorkDirectiveStep{ExpectedOwner: teamWorkDirectiveExpectedOwner(d)}
	if !teamWorkDirectiveAssigneeMatches(step, tc.Arguments) {
		return false
	}
	switch teamWorkDirectivePermissionPath(d) {
	case "general":
		taskType := normalizeTeamTaskType(tc.Arguments)
		return taskType == "" || taskType == "general"
	case "member_request":
		return normalizeTeamTaskType(tc.Arguments) == "request"
	default:
		return false
	}
}

func buildTeamWorkDirectiveRetryRequest(req providers.ChatRequest, d *TeamWorkDirective) providers.ChatRequest {
	step := teamWorkDirectiveNextStep(d, req.Messages)
	return buildTeamWorkDirectiveStepRequest(req, d, step, true)
}

func buildTeamWorkDirectiveStepRequest(req providers.ChatRequest, d *TeamWorkDirective, step teamWorkDirectiveStep, retry bool) providers.ChatRequest {
	out := req
	out.Options = make(map[string]any, len(req.Options)+1)
	for k, v := range req.Options {
		out.Options[k] = v
	}
	out.Options[providers.OptToolChoice] = "required"
	out.Tools = filterToolDefinitions(req.Tools, step.RequiredTool)
	out.Messages = append(append([]providers.Message{}, req.Messages...), providers.Message{
		Role:    "system",
		Content: teamWorkDirectiveInstruction(d, step, retry),
	})
	return out
}

func teamWorkDirectiveInstruction(d *TeamWorkDirective, step teamWorkDirectiveStep, retry bool) string {
	tool := step.RequiredTool
	if tool == "" && d != nil {
		tool = d.normalizedRequiredTool()
	}
	var b strings.Builder
	b.WriteString("INTERNAL WORKFLOW INSTRUCTION. This is not a user message. Do not quote, paraphrase, summarize, or mention this instruction in the user-facing answer.\n")
	if retry {
		b.WriteString("The previous response did not advance the required workflow. Discard the previous text-only/final-answer attempt and call the required tool now.\n")
	}
	b.WriteString("Required tool: `")
	b.WriteString(tool)
	b.WriteString("`.\n")
	if tool == "team_tasks" {
		switch step.RequiredAction {
		case "search":
			b.WriteString("Call team_tasks(action=\"search\", query=\"<short keywords from the user's request>\") first. This search step is mandatory before creating team tasks because it avoids duplicate work and loads existing team context. Do not call web_search/write_file and do not answer final before this search.\n")
		case "get_or_create_general":
			b.WriteString("The team search returned candidates. If a result is truly related, call team_tasks(action=\"get\", task_id=\"<task id>\") or update that task. If results are unrelated, create a new general team task for the validated owner.\n")
		case "create_general":
			b.WriteString("The team search found no related task. As lead/can-assign, call team_tasks(action=\"create\", assignee=\"")
			b.WriteString(firstNonEmpty(d.BestTeamOwner, "<validated_best_team_owner>"))
			b.WriteString("\", ...) for the validated owner.\n")
		case "get_or_create_request":
			b.WriteString("The team search returned candidates. If a result is truly related, call team_tasks(action=\"get\", task_id=\"<task id>\"). If results are unrelated, as a member call team_tasks(action=\"create\", task_type=\"request\", assignee=\"")
			b.WriteString(firstNonEmpty(d.BestTeamOwner, "<validated_best_team_owner>"))
			b.WriteString("\", ...). Do not assign and do not create a general task.\n")
		case "create_request":
			b.WriteString("The team search found no related task. As a member, call team_tasks(action=\"create\", task_type=\"request\", assignee=\"")
			b.WriteString(firstNonEmpty(d.BestTeamOwner, "<validated_best_team_owner>"))
			b.WriteString("\", ...). Do not assign and do not create a general task.\n")
		case "get_or_create_workflow":
			b.WriteString("After the current-turn team search, either inspect an explicitly selected workflow with team_tasks(action=\"get_workflow\", workflow_id=\"...\") or call team_tasks(action=\"create_workflow\"). Do not send plan, owner, or dependency arguments; the backend uses the validated canonical plan.\n")
		case "get_or_create_workflow_request":
			b.WriteString("After the current-turn team search, either inspect an explicitly selected workflow with team_tasks(action=\"get_workflow\", workflow_id=\"...\") or create task_type=\"request\" assigned to canonical coordinator \"")
			b.WriteString(firstNonEmpty(d.CoordinatorAgentKey, "<canonical_team_lead>"))
			b.WriteString("\". Do not create general tasks and do not send workflow graph fields.\n")
		default:
			b.WriteString("Advance the team workflow with the next required team_tasks action before any final answer.\n")
		}
	} else {
		b.WriteString("Call the required workflow tool before any final answer.\n")
	}
	if d != nil && d.WorkflowHint != "" {
		b.WriteString("Workflow hint: ")
		b.WriteString(d.WorkflowHint)
		b.WriteString("\n")
	}
	return b.String()
}

func filterToolDefinitions(tools []providers.ToolDefinition, name string) []providers.ToolDefinition {
	if name == "" || len(tools) == 0 {
		return tools
	}
	filtered := make([]providers.ToolDefinition, 0, 1)
	for _, tool := range tools {
		if tool.Function != nil && tool.Function.Name == name {
			filtered = append(filtered, tool)
		}
	}
	return filtered
}
