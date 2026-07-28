package teamworkclassify

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/nextlevelbuilder/goclaw/internal/providers"
	usagecaps "github.com/nextlevelbuilder/goclaw/internal/usage/caps"
)

// ReplanOptions carries only backend-derived constraints. InheritedReviewRequired
// comes from the stored validated plan and cannot be downgraded by the planner.
type ReplanOptions struct {
	InheritedReviewRequired bool
}

// PlanWorkflowReplacement runs the mandatory planning and independent critique
// stages for workflow recovery. Unlike ClassifyWithLLM, every transport, parse,
// validation, or critique failure is returned to the caller; recovery must never
// degrade to a self decision or mutate the workflow without a validated plan.
func PlanWorkflowReplacement(
	ctx context.Context,
	input Input,
	provider providers.Provider,
	model string,
	caps *usagecaps.Service,
	options ReplanOptions,
) (Result, error) {
	if input.Mode != ModeTeam {
		return Result{}, fmt.Errorf("workflow replan requires team mode")
	}
	if provider == nil || strings.TrimSpace(model) == "" {
		return Result{}, fmt.Errorf("workflow replan planner is unavailable")
	}

	// Replan is a planner-class call, so it takes the planner share of a
	// configured budget rather than the arbiter value (see stageTimeouts).
	timeout := plannerStageTimeout(input.Timeout)
	evidence := BuildEmbeddingEvidence(ctx, input)
	assessment := WorkAssessment{
		WorkflowMode:              WorkflowModeMultiRole,
		IndependentReviewRequired: options.InheritedReviewRequired,
		Reason:                    "replace a blocked multi-role workflow without weakening its validated constraints",
		WorkUnits: []WorkUnit{
			{ID: "replace", Description: "replace the blocked workflow with canonical executable work", RequiredOutput: "replacement workflow outputs"},
			{ID: "integrate", Description: "integrate replacement workflow outputs", RequiredOutput: "final workflow result"},
		},
		Dependencies:    []WorkDependency{{From: "replace", To: "integrate"}},
		RequiredOutputs: []string{"replacement workflow outputs", "final workflow result"},
	}
	if options.InheritedReviewRequired {
		assessment.WorkUnits = []WorkUnit{
			{ID: "draft", Description: "produce the replacement draft", RequiredOutput: "replacement draft"},
			{ID: "review", Description: "independently review the replacement draft", RequiredOutput: "independent critique"},
			{ID: "integrate", Description: "integrate the reviewed replacement", RequiredOutput: "final workflow result"},
		}
		assessment.Dependencies = []WorkDependency{{From: "draft", To: "review"}, {From: "review", To: "integrate"}}
		assessment.RequiredOutputs = []string{"replacement draft", "independent critique", "final workflow result"}
	}

	request := providers.ChatRequest{
		Messages: buildWorkflowReplanMessages(input, evidence, assessment, options.InheritedReviewRequired),
		Model:    model,
		Options:  map[string]any{providers.OptTemperature: 0.0},
	}
	response, err := callClassifierAttempt(ctx, timeout, provider, request, model, caps, "team-work-replan")
	if err != nil {
		return Result{}, fmt.Errorf("workflow replan planner call failed: %w", err)
	}
	if response == nil {
		return Result{}, fmt.Errorf("workflow replan planner returned no response")
	}

	validated, err := parseAndValidateWorkflowReplacement(input, evidence, response.Content, options.InheritedReviewRequired)
	if err != nil {
		repaired, repairErr := reviseWorkflowReplacement(ctx, timeout, input, evidence, provider, model, caps, request, response.Content, err, options.InheritedReviewRequired)
		if repairErr != nil {
			return Result{}, fmt.Errorf("workflow replan validation failed: %v; repair failed: %w", err, repairErr)
		}
		validated = repaired
		validated.PlannerRepaired = true
	}

	critique, err := critiqueAssignment(ctx, timeout, input, assessment, validated, provider, model, caps)
	if err != nil {
		return Result{}, fmt.Errorf("workflow replan critic failed: %w", err)
	}
	if !critique.Valid {
		if len(critique.Issues) == 0 {
			return Result{}, fmt.Errorf("workflow replan critic rejected without actionable issues")
		}
		revised, reviseErr := reviseWorkflowReplacementWithIssues(ctx, timeout, input, evidence, provider, model, caps, request, response.Content, critique.Issues, options.InheritedReviewRequired)
		if reviseErr != nil {
			return Result{}, fmt.Errorf("workflow replan critic rejected plan: %w", reviseErr)
		}
		validated = revised
		validated.PlannerRepaired = true
	}
	validated.PlannerValidationReason = "accepted fail-closed canonical workflow replacement"
	return validated, nil
}

func buildWorkflowReplanMessages(input Input, evidence Evidence, assessment WorkAssessment, reviewRequired bool) []providers.Message {
	messages := BuildPlanningMessages(input, evidence, assessment, reviewRequired)
	messages[0].Content += `

This is a backend workflow-recovery planner. A replacement is mandatory: return decision="team", workflow_mode="multi_role", workflow_executable=true, required_tool="team_tasks", and a non-null canonical plan. Never return self, single_owner, staffing_gaps, or a partial plan. Use only the supplied canonical roster and known tools. Preserve the canonical coordinator. The replacement must have at least two distinct non-lead owners, one terminal integration step, and every step must converge to that terminal. Every step owner and final_owner_agent_id must come from step_owner_candidates: the coordinator never owns a step, including the terminal one.`
	if reviewRequired {
		messages[0].Content += ` The stored validated plan requires independent review. Set review_status="included" and keep a critic step that reviews work owned by a DIFFERENT agent and whose result reaches the terminal step. The producer, the critic, and the terminal integrator may be three different agents.`
	}
	return messages
}

func parseAndValidateWorkflowReplacement(input Input, evidence Evidence, content string, reviewRequired bool) (Result, error) {
	result, err := ParseArbiterResult(content, ModeTeam)
	if err != nil {
		return Result{}, err
	}
	result.EffectiveReviewRequired = reviewRequired
	result.RequestedReviewRequired = result.RequestedReviewRequired || reviewRequired
	result.EffectiveWorkflowMode = WorkflowModeMultiRole
	if result.Decision != DecisionTeam || result.WorkflowMode != WorkflowModeMultiRole || result.Plan == nil {
		return Result{}, fmt.Errorf("replacement planner must return a multi_role team plan")
	}
	if !result.WorkflowExecutable || strings.TrimSpace(result.RequiredTool) != "team_tasks" {
		return Result{}, fmt.Errorf("replacement planner must return executable team_tasks workflow")
	}
	if len(result.StaffingGaps) != 0 {
		return Result{}, fmt.Errorf("replacement planner cannot return staffing gaps")
	}
	validated, err := ValidatePlannerResult(input, applyEvidenceToResult(input, evidence, result))
	if err != nil {
		return Result{}, err
	}
	validated.EffectiveWorkflowMode = WorkflowModeMultiRole
	return validated, nil
}

func reviseWorkflowReplacement(
	ctx context.Context,
	timeout time.Duration,
	input Input,
	evidence Evidence,
	provider providers.Provider,
	model string,
	caps *usagecaps.Service,
	base providers.ChatRequest,
	previous string,
	validationErr error,
	reviewRequired bool,
) (Result, error) {
	return reviseWorkflowReplacementWithIssues(ctx, timeout, input, evidence, provider, model, caps, base, previous, []string{validationErr.Error()}, reviewRequired)
}

func reviseWorkflowReplacementWithIssues(
	ctx context.Context,
	timeout time.Duration,
	input Input,
	evidence Evidence,
	provider providers.Provider,
	model string,
	caps *usagecaps.Service,
	base providers.ChatRequest,
	previous string,
	issues []string,
	reviewRequired bool,
) (Result, error) {
	issueJSON, _ := json.Marshal(issues)
	request := base
	request.Messages = append(append([]providers.Message(nil), base.Messages...),
		providers.Message{Role: "assistant", Content: previous},
		providers.Message{Role: "system", Content: "Repair the mandatory workflow replacement for these issues: " + string(issueJSON) + `. Return one corrected JSON object only. Keep decision="team", workflow_mode="multi_role", workflow_executable=true, required_tool="team_tasks", a non-null canonical plan, canonical owners/tools/coordinator, DAG convergence, and exactly one terminal integration step. Do not return self, single_owner, staffing_gaps, or commentary.`},
	)
	if reviewRequired {
		request.Messages[len(request.Messages)-1].Content += ` Keep review_status="included" and a critic step that reviews work owned by a DIFFERENT agent and whose result reaches the terminal step; the producer, critic, and terminal integrator may be three different agents.`
	}
	response, err := callClassifierAttempt(ctx, timeout, provider, request, model, caps, "team-work-replan-repair")
	if err != nil {
		return Result{}, err
	}
	if response == nil {
		return Result{}, fmt.Errorf("workflow replan repair returned no response")
	}
	return parseAndValidateWorkflowReplacement(input, evidence, response.Content, reviewRequired)
}
