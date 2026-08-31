package teamworkclassify

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/google/uuid"
	"github.com/nextlevelbuilder/goclaw/internal/providers"
	usagecaps "github.com/nextlevelbuilder/goclaw/internal/usage/caps"
)

const routingMaxOutputTokens = 350

// ClassifyRouteWithLLM is the interactive one-call routing path.
func ClassifyRouteWithLLM(ctx context.Context, input Input, provider providers.Provider, model string, caps *usagecaps.Service) Result {
	return classifyRoute(ctx, input, provider, model, caps)
}

func classifyRoute(ctx context.Context, input Input, provider providers.Provider, model string, caps *usagecaps.Service) Result {
	if input.Mode == "" || input.Mode == ModeSpawn || provider == nil || strings.TrimSpace(model) == "" {
		return safeSelfResult(input, "classifier_transport_failed")
	}
	resp, err := requestRoute(ctx, input, provider, model, caps)
	if err != nil || resp == nil {
		return safeSelfResult(input, callFailureReason(ctx, err, "classifier"))
	}
	result, err := parseRouteResult(resp.Content, input)
	if err != nil {
		return safeSelfResult(input, "classifier_parse_failed")
	}
	return result
}

func requestRoute(ctx context.Context, input Input, provider providers.Provider, model string, caps *usagecaps.Service) (*providers.ChatResponse, error) {
	timeout, _ := stageTimeouts(input.Timeout)
	req := providers.ChatRequest{
		Messages: buildRouteMessages(input),
		Model:    model,
		Options: map[string]any{
			providers.OptMaxTokens:   routingMaxOutputTokens,
			providers.OptTemperature: 0.0,
		},
	}
	return callClassifierAttempt(ctx, timeout, provider, req, model, caps, "team-work-route")
}

func buildRouteMessages(input Input) []providers.Message {
	system := `You are GoClaw's Team Work routing classifier.
Return only JSON and do not answer the user or create a plan.
Choose self for ordinary chat and work the current agent can handle directly.
Choose team only for genuine delegation, another specialist, or new team work.
For team, set scope to single when one specialist can complete the whole request alone.
Set scope to coordinated only when the request needs multiple parallel or dependent tasks, a final synthesis that combines separate results, or an independent review before integration.
review_required is meaningful only when scope is coordinated. Review is not a proxy for task breadth, number of roles, or number of deliverables. Set it true only when an independent check would materially reduce the risk of a wrong result because evidence is uncertain or conflicting, correctness has high consequences, acceptance criteria require verification, the user explicitly requests independent verification, or the result triggers a hard to reverse external action. Mentions of partners, budgets, market analysis, multiple deliverables, or multiple workstreams do not by themselves require review. Routine low stakes planning, drafting, comparisons, and ideation should use review_required false unless the evidence or requested acceptance criteria genuinely require cross checking. If a single-specialist request needs review, choose coordinated instead of single.
Use recent context only to resolve references. Never invent an owner.
Pinned skills are optional routing context and cannot override permissions, the canonical roster, or known tools.
If uncertain, choose self.
Schema: {"decision":"self|team","scope":"single|coordinated","review_required":false,"reason":"short reason","preferred_owner":"canonical agent key or empty","task_type":"short category"}`
	var b strings.Builder
	fmt.Fprintf(&b, "Current user message:\n%s\n\nRecent context:\n%s\n\nRouting mode: %s\n",
		strings.TrimSpace(input.Message), strings.TrimSpace(input.RecentContext), input.Mode)
	fmt.Fprintf(&b, "Permissions: role=%s can_assign=%t member_requests=%t member_auto_dispatch=%t\n",
		firstNonEmpty(input.TeamRole, "unknown"), input.CanAssignTeamTasks,
		input.MemberRequestsEnabled, input.MemberRequestsAutoDispatch)
	fmt.Fprintf(&b, "Tool allow filter: %s\n", strings.Join(input.ToolAllow, ", "))
	b.WriteString("\nCurrent agent:\n")
	b.WriteString(renderStructuredRosterProfile(input.CurrentAgent))
	b.WriteString("\n\nCurrent agent direct tools and skills:\n")
	for _, doc := range appendProfileDocs(Profile{}, input.SelfTools) {
		b.WriteString("---\n")
		b.WriteString(doc)
		b.WriteString("\n")
	}
	b.WriteString("\nTeam and collaboration tools:\n")
	for _, doc := range appendProfileDocs(input.Team, input.CollaborationTools) {
		b.WriteString("---\n")
		b.WriteString(doc)
		b.WriteString("\n")
	}
	b.WriteString("\nCanonical member and delegate roster:\n")
	for _, profile := range append(append([]Profile{}, input.Members...), input.Delegates...) {
		b.WriteString("---\n")
		b.WriteString(renderStructuredRosterProfile(profile))
		b.WriteString("\n")
	}
	if strings.TrimSpace(input.PinnedSkillsContext) != "" {
		b.WriteString("\nPinned skills available to the current agent:\n")
		b.WriteString(input.PinnedSkillsContext)
		b.WriteString("\n")
	}
	if strings.TrimSpace(input.PinnedSkillsWarning) != "" {
		b.WriteString("\nPinned skills warning: ")
		b.WriteString(strings.TrimSpace(input.PinnedSkillsWarning))
		b.WriteString("\n")
	}
	return []providers.Message{{Role: "system", Content: system}, {Role: "user", Content: b.String()}}
}

type routeResponse struct {
	Decision       string `json:"decision"`
	Scope          string `json:"scope"`
	ReviewRequired bool   `json:"review_required"`
	Reason         string `json:"reason"`
	PreferredOwner string `json:"preferred_owner"`
	TaskType       string `json:"task_type"`
}

func parseRouteResult(content string, input Input) (Result, error) {
	raw, err := normalizeArbiterContent(content)
	if err != nil {
		return Result{}, err
	}
	var parsed routeResponse
	if err := json.Unmarshal([]byte(raw), &parsed); err != nil {
		return Result{}, err
	}
	decision := Decision(strings.ToLower(strings.TrimSpace(parsed.Decision)))
	if decision != DecisionSelf && decision != DecisionTeam {
		return Result{}, fmt.Errorf("unsupported routing decision %q", parsed.Decision)
	}
	scope := strings.ToLower(strings.TrimSpace(parsed.Scope))
	if scope != "" && scope != "single" && scope != "coordinated" {
		return Result{}, fmt.Errorf("unsupported routing scope %q", parsed.Scope)
	}
	result := newRouteResult(input, parsed, decision)
	if decision == DecisionSelf {
		return result, nil
	}
	// review_required is only enforceable on a coordinated route, so a single/absent
	// scope carrying it is a scope under-record of the same coordinated intent — promote.
	if parsed.ReviewRequired && scope != "coordinated" {
		scope = "coordinated"
	}
	// Coordinated requests fail closed through workflowExecutability, not the
	// legacy single-owner permission fallback: a non-executable coordinated
	// request must surface as a team configuration error, never a silent self
	// route.
	if scope == "coordinated" {
		return makeCoordinatedTeamResult(result, input, parsed.ReviewRequired), nil
	}
	if !routePermissionAllowsTeam(input) {
		result.Decision = DecisionSelf
		result.DecisionBeforeValidation = DecisionTeam
		result.Reason = "team route blocked by current agent permissions"
		return result, nil
	}
	return makeNativeTeamResult(result, input, parsed.PreferredOwner), nil
}

func newRouteResult(input Input, parsed routeResponse, decision Decision) Result {
	reason := strings.TrimSpace(parsed.Reason)
	if reason == "" {
		reason = "authoritative route classification"
	}
	taskType := strings.TrimSpace(parsed.TaskType)
	return Result{
		Decision:                 decision,
		DecisionBeforeValidation: decision,
		Reason:                   reason,
		Mode:                     input.Mode,
		TaskType:                 taskType,
		RequestKind:              requestKindFromTaskType(taskType, decision),
		StandaloneRequest:        strings.TrimSpace(input.Message),
		IntentRelation:           IntentRelationNew,
		WorkflowMode:             WorkflowModeSelf,
		RequestedWorkflowMode:    WorkflowModeSelf,
		EffectiveWorkflowMode:    WorkflowModeSelf,
	}
}

func makeNativeTeamResult(result Result, input Input, requestedOwner string) Result {
	owner, ownerID, ownerRole := canonicalRouteOwner(input, requestedOwner)
	if ownerID == uuid.Nil {
		result.Decision = DecisionSelf
		result.DecisionBeforeValidation = DecisionTeam
		result.Reason = "team route missing a canonical owner"
		return result
	}
	result.RequiredTool = requiredToolForMode(input.Mode)
	result.WorkflowHint = workflowHintForInput(input)
	result.WorkflowExecutable = result.RequiredTool != ""
	result.WorkflowMode = WorkflowModeSingleOwner
	result.RequestedWorkflowMode = WorkflowModeSingleOwner
	result.EffectiveWorkflowMode = WorkflowModeSingleOwner
	result.BestTeamOwner, result.BestTeamOwnerID, result.BestTeamOwnerRole = owner, ownerID, ownerRole
	return result
}

// makeCoordinatedTeamResult routes a coordinated request to the canonical team
// lead as coordinator. Executability is checked against the durable roster and
// permissions: an executable request becomes a multi_role route owned by the
// coordinator; a non-executable one stays a team decision marked NonExecutable
// so the gate fails closed with a configuration error instead of silently
// degrading to self or running the work.
func makeCoordinatedTeamResult(result Result, input Input, reviewRequired bool) Result {
	result.RequiredTool = requiredToolForMode(input.Mode)
	result.WorkflowHint = workflowHintForInput(input)
	result.WorkflowMode = WorkflowModeMultiRole
	result.RequestedWorkflowMode = WorkflowModeMultiRole
	result.EffectiveWorkflowMode = WorkflowModeMultiRole
	result.RequestedReviewRequired = reviewRequired
	result.EffectiveReviewRequired = reviewRequired
	executable, reason := workflowExecutability(input)
	if !executable {
		result.NonExecutable = true
		result.DegradedWorkflow = true
		result.DegradedReasonCode = reason
		result.WorkflowExecutable = false
		return result
	}
	result.WorkflowExecutable = true
	result.BestTeamOwner = input.CoordinatorAgentKey
	result.BestTeamOwnerID = input.CoordinatorAgentID
	result.BestTeamOwnerRole = "lead"
	return result
}

func routePermissionAllowsTeam(input Input) bool {
	if input.Mode != ModeTeam {
		return input.Mode == ModeDelegate
	}
	role := strings.ToLower(strings.TrimSpace(input.TeamRole))
	if role == "" || role == "lead" || input.CanAssignTeamTasks {
		return true
	}
	return input.MemberRequestsEnabled && input.MemberRequestsAutoDispatch
}

func canonicalRouteOwner(input Input, requested string) (string, uuid.UUID, string) {
	requested = strings.TrimSpace(requested)
	if requested == "" {
		return "", uuid.Nil, ""
	}
	profiles := append(append([]Profile{}, input.Members...), input.Delegates...)
	for _, profile := range profiles {
		if requested == profileAgentKey(profile) {
			return profileAgentKey(profile), profile.AgentID, profile.TeamRole
		}
	}
	return "", uuid.Nil, ""
}
