package cmd

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/google/uuid"

	"github.com/nextlevelbuilder/goclaw/internal/providerresolve"
	"github.com/nextlevelbuilder/goclaw/internal/providers"
	"github.com/nextlevelbuilder/goclaw/internal/store"
	"github.com/nextlevelbuilder/goclaw/internal/teamworkclassify"
	"github.com/nextlevelbuilder/goclaw/internal/teamworkconfig"
	"github.com/nextlevelbuilder/goclaw/internal/tools"
	usagecaps "github.com/nextlevelbuilder/goclaw/internal/usage/caps"
	"github.com/nextlevelbuilder/goclaw/internal/workflowactions"
)

// buildWorkflowReplanner returns the backend-only replacement-plan service used
// by coordinator recovery. It reloads all recovery identity from the store, uses
// the persisted coordinator's provider/model plus tenant classifier overrides,
// and accepts no client/model graph, hash, token, or target UUID.
func buildWorkflowReplanner(
	stores *store.Stores,
	providerReg *providers.Registry,
	teamWorkCfg *teamworkconfig.Resolver,
	profileStores teamworkclassify.ProfileStores,
	embedder teamworkclassify.Embedder,
	usageCaps *usagecaps.Service,
	dataDir string,
) workflowactions.ReplanFunc {
	return func(ctx context.Context, request workflowactions.ReplanRequest) (store.WorkflowActionResult, error) {
		if stores == nil || stores.Teams == nil || stores.Agents == nil {
			return store.WorkflowActionResult{}, fmt.Errorf("workflow replan dependencies are unavailable")
		}
		workflowStore, ok := stores.Teams.(store.TeamWorkflowStore)
		if !ok {
			return store.WorkflowActionResult{}, fmt.Errorf("team workflow store is unavailable")
		}
		if request.Workflow == nil || request.Blocked == nil || request.Team == nil || request.CoordinatorID == uuid.Nil {
			return store.WorkflowActionResult{}, fmt.Errorf("workflow replan recovery state is incomplete")
		}
		if err := request.Guard.Validate(); err != nil {
			return store.WorkflowActionResult{}, err
		}
		if request.Guard.Action != store.WorkflowActionApplyReplan {
			return store.WorkflowActionResult{}, store.ErrWorkflowActionInvalid
		}

		workflow, err := workflowStore.GetWorkflow(ctx, request.Guard.WorkflowID)
		if err != nil {
			return store.WorkflowActionResult{}, fmt.Errorf("reload workflow for replan: %w", err)
		}
		blocked, err := stores.Teams.GetTask(ctx, *request.Guard.TaskID)
		if err != nil {
			return store.WorkflowActionResult{}, fmt.Errorf("reload blocked step for replan: %w", err)
		}
		if workflow.TenantID != store.TenantIDFromContext(ctx) || workflow.TeamID != request.Guard.TeamID ||
			workflow.ID != request.Workflow.ID || workflow.CoordinatorAgentID != request.CoordinatorID ||
			blocked.WorkflowID == nil || *blocked.WorkflowID != workflow.ID || blocked.TeamID != workflow.TeamID {
			return staleWorkflowReplanResult(workflow), nil
		}
		if workflow.Status != store.TeamWorkflowStatusNeedsRevision ||
			workflow.PlanRevision != request.Guard.ExpectedPlanRevision ||
			(request.Guard.ExpectedStatus != "" && workflow.Status != request.Guard.ExpectedStatus) ||
			blocked.Status != store.TeamTaskStatusBlocked || blocked.WorkflowKind != store.TeamWorkflowTaskKindWork ||
			blocked.PlanRevision != workflow.PlanRevision ||
			(request.Guard.ExpectedTaskStatus != "" && blocked.Status != request.Guard.ExpectedTaskStatus) {
			return staleWorkflowReplanResult(workflow), nil
		}

		team, err := stores.Teams.GetTeam(ctx, workflow.TeamID)
		if err != nil {
			return store.WorkflowActionResult{}, fmt.Errorf("reload workflow team for replan: %w", err)
		}
		if team.LeadAgentID != workflow.CoordinatorAgentID || team.ID != request.Team.ID {
			return staleWorkflowReplanResult(workflow), nil
		}
		coordinator, err := stores.Agents.GetByID(ctx, workflow.CoordinatorAgentID)
		if err != nil || coordinator == nil || coordinator.TenantID != workflow.TenantID {
			if err != nil {
				return store.WorkflowActionResult{}, fmt.Errorf("load workflow coordinator for replan: %w", err)
			}
			return store.WorkflowActionResult{}, fmt.Errorf("workflow coordinator is unavailable")
		}

		storedPlan, err := workflowReplanPlan(workflow)
		if err != nil {
			return store.WorkflowActionResult{}, err
		}
		goal := storedPlan.Goal
		tasksBeforeReplan, err := workflowStore.ListWorkflowTasks(ctx, workflow.ID)
		if err != nil {
			return store.WorkflowActionResult{}, fmt.Errorf("load workflow evidence for replan: %w", err)
		}
		comments, err := stores.Teams.ListRecentTaskComments(ctx, blocked.ID, workflowReplanCommentLimit)
		if err != nil {
			return store.WorkflowActionResult{}, fmt.Errorf("load blocker comments for replan: %w", err)
		}
		message, recentContext := buildWorkflowReplanInput(goal, blocked, request.Guard.Reason, tasksBeforeReplan, comments)
		input := teamworkclassify.BuildInputFromStores(ctx, profileStores, teamworkclassify.BuildInputOptions{
			Mode:          teamworkclassify.ModeTeam,
			Message:       message,
			RecentContext: recentContext,
			AgentID:       coordinator.ID,
			TeamID:        workflow.TeamID,
			Embedder:      embedder,
		})
		if input.CoordinatorAgentID != workflow.CoordinatorAgentID || input.CoordinatorAgentKey != workflow.CoordinatorAgentKey {
			return store.WorkflowActionResult{}, fmt.Errorf("canonical workflow coordinator changed; replan is unavailable")
		}

		provider, err := providerresolve.ResolveAgentProvider(providerReg, coordinator)
		if err != nil {
			return store.WorkflowActionResult{}, fmt.Errorf("resolve workflow coordinator provider: %w", err)
		}
		settings := teamworkconfig.Settings{}
		if teamWorkCfg != nil {
			settings = teamWorkCfg.Resolve(ctx)
		}
		selection := providerresolve.ResolveTeamWorkClassifier(ctx, providerReg, settings.ClassifierProvider, settings.ClassifierModel, provider, coordinator.Model)
		result, err := teamworkclassify.PlanWorkflowReplacement(
			ctx,
			input,
			selection.Provider,
			selection.Model,
			usageCaps,
			teamworkclassify.ReplanOptions{
				InheritedReviewRequired: workflowReplanReviewRequired(storedPlan),
			},
		)
		if err != nil {
			return store.WorkflowActionResult{}, fmt.Errorf("plan replacement workflow: %w", err)
		}
		constraint, err := teamworkclassify.BuildPlanConstraint(result.Plan)
		if err != nil {
			return store.WorkflowActionResult{}, fmt.Errorf("freeze replacement workflow plan: %w", err)
		}
		if constraint.CoordinatorAgentID != workflow.CoordinatorAgentID || constraint.CoordinatorAgentKey != workflow.CoordinatorAgentKey {
			return store.WorkflowActionResult{}, fmt.Errorf("replacement workflow coordinator is not canonical")
		}

		replacement := *workflow
		replacement.CanonicalPlan = append([]byte(nil), constraint.CanonicalPlan...)
		replacement.PlanHash = constraint.PlanHash
		replacement.SchemaVersion = constraint.SchemaVersion
		tasks, err := tools.BuildWorkflowReplanTasks(constraint, &replacement)
		if err != nil {
			return store.WorkflowActionResult{}, fmt.Errorf("build replacement workflow tasks: %w", err)
		}
		// Preserve the existing workflow's resolved routing/trace metadata, then
		// calculate the replacement workspace from the authoritative tenant/team.
		tools.InheritWorkflowTaskContext(tasks, blocked)
		tools.SetWorkflowReplanTaskWorkspace(ctx, dataDir, team, tasks)

		// The planner call can take long enough for the lead, membership, agent
		// tools, or grants to change. Rebuild the canonical roster snapshot and
		// validate the frozen replacement immediately before the store CAS.
		latestTeam, err := stores.Teams.GetTeam(ctx, workflow.TeamID)
		if err != nil {
			return store.WorkflowActionResult{}, fmt.Errorf("revalidate workflow team for replan: %w", err)
		}
		if latestTeam.LeadAgentID != workflow.CoordinatorAgentID {
			return staleWorkflowReplanResult(workflow), nil
		}
		latestInput := teamworkclassify.BuildInputFromStores(ctx, profileStores, teamworkclassify.BuildInputOptions{
			Mode:    teamworkclassify.ModeTeam,
			Message: message,
			AgentID: coordinator.ID,
			TeamID:  workflow.TeamID,
		})
		if err := validateWorkflowReplanRoster(latestInput, constraint); err != nil {
			return store.WorkflowActionResult{}, err
		}
		return workflowStore.CommitWorkflowReplan(ctx, store.WorkflowReplan{
			Guard:         request.Guard,
			CoordinatorID: workflow.CoordinatorAgentID,
			CanonicalPlan: constraint.CanonicalPlan,
			PlanHash:      constraint.PlanHash,
			Tasks:         tasks,
		})
	}
}

func validateWorkflowReplanRoster(input teamworkclassify.Input, constraint *tools.TeamWorkPlanConstraint) error {
	if constraint == nil || input.CoordinatorAgentID != constraint.CoordinatorAgentID ||
		input.CoordinatorAgentKey != constraint.CoordinatorAgentKey {
		return fmt.Errorf("workflow coordinator or roster changed; replan is unavailable")
	}
	profiles := make(map[uuid.UUID]teamworkclassify.Profile, len(input.Members))
	for i := range input.Members {
		profiles[input.Members[i].AgentID] = input.Members[i]
	}
	for i := range constraint.Steps {
		step := &constraint.Steps[i]
		owner, ok := profiles[step.OwnerAgentID]
		if !ok || !strings.EqualFold(strings.TrimSpace(owner.AgentKey), strings.TrimSpace(step.OwnerAgentKey)) {
			return fmt.Errorf("replacement workflow roster changed; replan is required")
		}
		available := make(map[string]struct{}, len(owner.AvailableTools))
		for _, name := range owner.AvailableTools {
			available[strings.ToLower(strings.TrimSpace(name))] = struct{}{}
		}
		for _, required := range step.RequiredTools {
			if _, ok := available[strings.ToLower(strings.TrimSpace(required))]; !ok {
				return fmt.Errorf("replacement workflow owner %q no longer has required tool %q", step.OwnerAgentKey, required)
			}
		}
	}
	return nil
}

func staleWorkflowReplanResult(workflow *store.TeamWorkflowData) store.WorkflowActionResult {
	return store.WorkflowActionResult{
		Outcome:  store.WorkflowActionConflict,
		Action:   store.WorkflowActionApplyReplan,
		Workflow: workflow,
	}
}

func workflowReplanPlan(workflow *store.TeamWorkflowData) (*teamworkclassify.WorkflowPlan, error) {
	if workflow == nil || len(workflow.CanonicalPlan) == 0 {
		return nil, fmt.Errorf("stored workflow plan is unavailable")
	}
	var plan teamworkclassify.WorkflowPlan
	if err := json.Unmarshal(workflow.CanonicalPlan, &plan); err != nil {
		return nil, fmt.Errorf("decode stored workflow plan: %w", err)
	}
	if strings.TrimSpace(plan.Goal) == "" {
		return nil, fmt.Errorf("stored workflow goal is missing")
	}
	// PostgreSQL persists canonical_plan as JSONB, so scanned bytes can differ
	// from the originally frozen bytes. Re-freezing the decoded typed plan through
	// the sole canonical freeze point verifies the persisted semantic plan against
	// the authoritative hash without hashing JSONB-normalized wire bytes.
	constraint, err := teamworkclassify.BuildPlanConstraint(&plan)
	if err != nil {
		return nil, fmt.Errorf("freeze stored workflow plan: %w", err)
	}
	if constraint.PlanHash != workflow.PlanHash {
		return nil, fmt.Errorf("stored workflow canonical plan hash mismatch")
	}
	return &plan, nil
}

func workflowReplanReviewRequired(plan *teamworkclassify.WorkflowPlan) bool {
	if plan == nil {
		return false
	}
	status := strings.ToLower(strings.TrimSpace(plan.ReviewStatus))
	return status == "required" || status == "included"
}

const (
	workflowReplanCommentLimit       = 5
	workflowReplanGoalRunes          = 4000
	workflowReplanStepIDRunes        = 256
	workflowReplanSubjectRunes       = 500
	workflowReplanBlockerReasonRunes = 2000
	workflowReplanCommentRunes       = 1000
	workflowReplanResultRunes        = 4000
	workflowReplanContextRunes       = 24000
)

func buildWorkflowReplanInput(
	goal string,
	blocked *store.TeamTaskData,
	requirements string,
	tasks []store.TeamTaskData,
	comments []store.TeamTaskCommentData,
) (string, string) {
	goal = truncateWorkflowReplanText(goal, workflowReplanGoalRunes)
	requirements = truncateWorkflowReplanText(requirements, store.MaxWorkflowActionReasonRunes)

	var message strings.Builder
	message.WriteString(goal)
	message.WriteString("\n\nReplacement-plan requirements from the coordinator:\n")
	message.WriteString(requirements)

	var context strings.Builder
	context.WriteString("Persisted workflow goal:\n")
	context.WriteString(goal)
	context.WriteString("\n\nBlocked current-revision step:\n")
	fmt.Fprintf(
		&context,
		"%s — %s\n",
		truncateWorkflowReplanText(blocked.WorkflowStepID, workflowReplanStepIDRunes),
		truncateWorkflowReplanText(blocked.Subject, workflowReplanSubjectRunes),
	)
	if reason := truncateWorkflowReplanText(blocked.BlockerReason, workflowReplanBlockerReasonRunes); reason != "" {
		context.WriteString("Blocker reason: ")
		context.WriteString(reason)
		context.WriteString("\n")
	}
	if len(comments) > 0 {
		context.WriteString("\nRecent blocker comments (chronological):\n")
		start := 0
		if len(comments) > workflowReplanCommentLimit {
			start = len(comments) - workflowReplanCommentLimit
		}
		for i := start; i < len(comments); i++ {
			text := truncateWorkflowReplanText(comments[i].Content, workflowReplanCommentRunes)
			if text != "" {
				fmt.Fprintf(&context, "- %s\n", text)
			}
		}
	}
	context.WriteString("\nCompleted current-workflow evidence:\n")
	found := false
	for i := range tasks {
		task := &tasks[i]
		if task.WorkflowKind != store.TeamWorkflowTaskKindWork || task.Status != store.TeamTaskStatusCompleted || task.Result == nil {
			continue
		}
		result := truncateWorkflowReplanText(*task.Result, workflowReplanResultRunes)
		if result == "" {
			continue
		}
		found = true
		fmt.Fprintf(
			&context,
			"- %s (%s): %s\n",
			truncateWorkflowReplanText(task.WorkflowStepID, workflowReplanStepIDRunes),
			truncateWorkflowReplanText(task.Subject, workflowReplanSubjectRunes),
			result,
		)
	}
	if !found {
		context.WriteString("(none)\n")
	}
	return message.String(), truncateWorkflowReplanText(context.String(), workflowReplanContextRunes)
}

func truncateWorkflowReplanText(value string, limit int) string {
	value = strings.TrimSpace(value)
	runes := []rune(value)
	if len(runes) <= limit {
		return value
	}
	return string(runes[:limit])
}
