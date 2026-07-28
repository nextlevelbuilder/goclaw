package tools

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/nextlevelbuilder/goclaw/internal/store"
)

func (t *TeamTasksTool) workflowStore() (store.TeamWorkflowStore, error) {
	workflowStore, ok := t.manager.Store().(store.TeamWorkflowStore)
	if !ok {
		return nil, fmt.Errorf("team workflow store is unavailable")
	}
	return workflowStore, nil
}

func (t *TeamTasksTool) executeCreateWorkflow(ctx context.Context, _ map[string]any) *Result {
	// Generic team_tasks schemas may cause a model to echo irrelevant fields.
	// Deliberately ignore them: the canonical graph is read only from context.
	constraint := TeamWorkPlanConstraintFromCtx(ctx)
	if constraint == nil {
		return ErrorResult("create_workflow requires a validated Team Work plan")
	}
	team, agentID, err := t.manager.ResolveTeam(ctx)
	if err != nil {
		return ErrorResult(err.Error())
	}
	if err := t.manager.RequireLead(ctx, team, agentID); err != nil {
		return ErrorResult(err.Error())
	}
	if agentID != team.LeadAgentID || constraint.CoordinatorAgentID != team.LeadAgentID {
		return ErrorResult("workflow coordinator must be the canonical team lead")
	}
	if ptd := PendingTeamDispatchFromCtx(ctx); ptd != nil && !ptd.HasListed() {
		return ErrorResult("You must search or list current-turn team work before creating a workflow")
	}
	workflowStore, err := t.workflowStore()
	if err != nil {
		return ErrorResult(err.Error())
	}
	workflow, tasks, err := buildDurableWorkflow(ctx, team, agentID, constraint, store.TeamWorkflowStatusRunning, false)
	if err != nil {
		return ErrorResult(err.Error())
	}
	setWorkflowTaskWorkspace(ctx, t.manager, team, tasks)
	members, err := t.manager.CachedListMembers(ctx, team.ID, agentID)
	if err != nil {
		return ErrorResult("failed to revalidate workflow roster: " + err.Error())
	}
	memberIDs := make(map[uuid.UUID]struct{}, len(members))
	for _, member := range members {
		memberIDs[member.AgentID] = struct{}{}
	}
	for _, workflowTask := range tasks {
		if workflowTask.OwnerAgentID == nil {
			return ErrorResult("workflow step has no canonical owner")
		}
		if _, exists := memberIDs[*workflowTask.OwnerAgentID]; !exists {
			return ErrorResult("workflow roster changed; re-plan is required")
		}
	}
	if err := workflowStore.CreateWorkflowWithTasks(ctx, workflow, tasks); err != nil {
		if existing, findErr := workflowStore.FindWorkflowByCreationKey(ctx, team.ID, workflow.OriginRunID, workflow.PlanHash); findErr == nil && existing != nil {
			if existingTasks, listErr := workflowStore.ListWorkflowTasks(ctx, existing.ID); listErr == nil {
				queueWorkflowRoots(ctx, team.ID, existingTasks)
			}
			return NewResult(fmt.Sprintf("Workflow already exists (id=%s, status=%s).", existing.ID, existing.Status))
		}
		return ErrorResult("failed to create workflow: " + err.Error())
	}
	queueWorkflowRoots(ctx, team.ID, tasks)
	return NewResult(fmt.Sprintf("Workflow created (id=%s, steps=%d).", workflow.ID, len(tasks)))
}

func (t *TeamTasksTool) executeGetWorkflow(ctx context.Context, args map[string]any) *Result {
	rawID, _ := args["workflow_id"].(string)
	workflowID, err := uuid.Parse(strings.TrimSpace(rawID))
	if err != nil {
		return ErrorResult("workflow_id must be a valid UUID")
	}
	team, agentID, err := t.manager.ResolveTeam(ctx)
	if err != nil {
		return ErrorResult(err.Error())
	}
	workflowStore, err := t.workflowStore()
	if err != nil {
		return ErrorResult(err.Error())
	}
	workflow, err := workflowStore.GetWorkflow(ctx, workflowID)
	if err != nil || workflow.TeamID != team.ID {
		return ErrorResult("workflow not found")
	}
	constraint := TeamWorkPlanConstraintFromCtx(ctx)
	reusable := false
	if constraint != nil && workflow.PlanHash == constraint.PlanHash {
		switch workflow.Status {
		case store.TeamWorkflowStatusCompleted:
			reusable = true
		case store.TeamWorkflowStatusPendingExpansion, store.TeamWorkflowStatusRunning:
			reusable = workflow.OriginAgentID == agentID && workflow.OriginSessionKey == ToolSessionKeyFromCtx(ctx)
		}
	}
	payload := map[string]any{
		"id": workflow.ID, "status": workflow.Status, "plan_hash": workflow.PlanHash,
		"schema_version": workflow.SchemaVersion, "canonical_plan": json.RawMessage(workflow.CanonicalPlan),
		"reusable": reusable, "result_summary": workflow.ResultSummary,
	}
	if reusable {
		if tasks, listErr := workflowStore.ListWorkflowTasks(ctx, workflow.ID); listErr == nil {
			payload["tasks"] = tasks
		}
	}
	encoded, _ := json.Marshal(payload)
	return NewResult(string(encoded))
}

func (t *TeamTasksTool) createPendingWorkflowRequest(ctx context.Context, team *store.TeamData, agentID uuid.UUID, auditTask *store.TeamTaskData, constraint *TeamWorkPlanConstraint, autoExpand bool) error {
	workflowStore, err := t.workflowStore()
	if err != nil {
		return err
	}
	workflow, tasks, err := buildDurableWorkflow(ctx, team, agentID, constraint, store.TeamWorkflowStatusPendingExpansion, autoExpand)
	if err != nil {
		return err
	}
	InheritWorkflowTaskContext(tasks, auditTask)
	members, err := t.manager.CachedListMembers(ctx, team.ID, agentID)
	if err != nil {
		return fmt.Errorf("failed to revalidate workflow roster: %w", err)
	}
	memberIDs := make(map[uuid.UUID]struct{}, len(members))
	for _, member := range members {
		memberIDs[member.AgentID] = struct{}{}
	}
	for _, workflowTask := range tasks {
		if workflowTask.OwnerAgentID == nil {
			return fmt.Errorf("workflow step has no canonical owner")
		}
		if _, exists := memberIDs[*workflowTask.OwnerAgentID]; !exists {
			return fmt.Errorf("workflow roster changed; re-plan is required")
		}
	}
	auditTask.Status = store.TeamTaskStatusPending
	auditTask.WorkflowKind = store.TeamWorkflowTaskKindAudit
	auditTask.WorkflowStepID = ""
	auditTask.WorkflowTerminal = false
	if err := workflowStore.CreatePendingWorkflowRequest(ctx, workflow, auditTask); err != nil {
		if _, findErr := workflowStore.FindWorkflowByCreationKey(ctx, team.ID, workflow.OriginRunID, workflow.PlanHash); findErr == nil {
			return nil
		}
		return err
	}
	if autoExpand {
		expansionToken, claimErr := workflowStore.ClaimPendingWorkflowExpansion(ctx, workflow.ID, constraint.CoordinatorAgentID, time.Now().Add(2*time.Minute))
		if claimErr != nil {
			slog.Warn("team workflow request durable; auto-expansion claim deferred to recovery", "workflow_id", workflow.ID, "error", claimErr)
			return nil
		}
		if err := workflowStore.ExpandPendingWorkflow(ctx, workflow.ID, constraint.CoordinatorAgentID, expansionToken, tasks); err != nil {
			slog.Warn("team workflow request durable; auto-expansion deferred to recovery", "workflow_id", workflow.ID, "error", err)
			return nil
		}
		queueWorkflowRoots(ctx, team.ID, tasks)
	}
	return nil
}

func buildDurableWorkflow(ctx context.Context, team *store.TeamData, originAgentID uuid.UUID, constraint *TeamWorkPlanConstraint, status string, autoExpand bool) (*store.TeamWorkflowData, []store.TeamTaskData, error) {
	if constraint == nil || len(constraint.Steps) == 0 || len(constraint.CanonicalPlan) == 0 || len(constraint.PlanHash) != 64 {
		return nil, nil, fmt.Errorf("validated workflow plan is incomplete")
	}
	originRunID := strings.TrimSpace(ToolRunIDFromCtx(ctx))
	if originRunID == "" {
		originRunID = uuid.NewString()
	}
	workflow := &store.TeamWorkflowData{
		TeamID: team.ID, Status: status, CanonicalPlan: append([]byte(nil), constraint.CanonicalPlan...),
		SchemaVersion: constraint.SchemaVersion, PlanHash: constraint.PlanHash,
		CoordinatorAgentID: constraint.CoordinatorAgentID, CoordinatorAgentKey: constraint.CoordinatorAgentKey,
		OriginAgentID: originAgentID, OriginAgentKey: ToolAgentKeyFromCtx(ctx), OriginRunID: originRunID,
		OriginSessionKey: ToolSessionKeyFromCtx(ctx), OriginChannel: ToolChannelFromCtx(ctx), OriginChatID: ToolChatIDFromCtx(ctx),
		OriginPeerKind: ToolPeerKindFromCtx(ctx), OriginLocalKey: ToolLocalKeyFromCtx(ctx),
		OriginUserID: store.UserIDFromContext(ctx), OriginSenderID: store.SenderIDFromContext(ctx), OriginRole: store.RoleFromContext(ctx),
		AutoExpand: autoExpand,
	}
	if routing, marshalErr := json.Marshal(ToolRoutingMetadataFromCtx(ctx)); marshalErr == nil {
		workflow.OriginRouting = routing
	}
	if auditID := TeamWorkClassificationAuditIDFromCtx(ctx); auditID != uuid.Nil {
		workflow.ClassificationAuditID = &auditID
	}
	tasks, err := buildWorkflowTasks(constraint, workflow)
	if err != nil {
		return nil, nil, err
	}
	return workflow, tasks, nil
}

// BuildWorkflowReplanTasks constructs concrete tasks only from a backend-frozen
// plan constraint and persisted workflow origin metadata. It deliberately has no
// model/tool argument surface: callers must first validate and freeze the plan
// through teamworkclassify.BuildPlanConstraint.
func BuildWorkflowReplanTasks(constraint *TeamWorkPlanConstraint, workflow *store.TeamWorkflowData) ([]store.TeamTaskData, error) {
	return buildWorkflowTasks(constraint, workflow)
}

func buildWorkflowTasks(constraint *TeamWorkPlanConstraint, workflow *store.TeamWorkflowData) ([]store.TeamTaskData, error) {
	stepTaskIDs := make(map[string]uuid.UUID, len(constraint.Steps))
	for _, step := range constraint.Steps {
		stepTaskIDs[step.ID] = store.GenNewID()
	}
	tasks := make([]store.TeamTaskData, 0, len(constraint.Steps))
	for _, step := range constraint.Steps {
		blockedBy := make([]uuid.UUID, 0, len(step.DependsOn))
		for _, dep := range step.DependsOn {
			depID, ok := stepTaskIDs[dep]
			if !ok {
				return nil, fmt.Errorf("workflow dependency %q is missing", dep)
			}
			blockedBy = append(blockedBy, depID)
		}
		status := store.TeamTaskStatusPending
		if len(blockedBy) > 0 {
			status = store.TeamTaskStatusBlocked
		}
		description := buildWorkflowStepDescription(constraint, step)
		ownerID := step.OwnerAgentID
		tasks = append(tasks, store.TeamTaskData{
			BaseModel: store.BaseModel{ID: stepTaskIDs[step.ID]}, TeamID: workflow.TeamID,
			Subject: step.Title, Description: description, Status: status, OwnerAgentID: &ownerID,
			BlockedBy: blockedBy, TaskType: "general", CreatedByAgentID: &workflow.OriginAgentID,
			UserID: workflow.OriginUserID, Channel: workflow.OriginChannel, ChatID: workflow.OriginChatID,
			WorkflowStepID: step.ID, WorkflowKind: store.TeamWorkflowTaskKindWork, WorkflowTerminal: step.Terminal,
			Metadata: workflowStepMetadata(workflow, blockedBy),
		})
	}
	return tasks, nil
}

// BuildWorkflowTasksFromStoredPlan reconstructs the execution graph from the
// canonical plan persisted with a pending workflow. It verifies the backend
// hash before returning tasks, so approval never trusts audit-task metadata or
// model-supplied graph arguments.
func BuildWorkflowTasksFromStoredPlan(workflow *store.TeamWorkflowData) ([]store.TeamTaskData, error) {
	if workflow == nil || len(workflow.CanonicalPlan) == 0 {
		return nil, fmt.Errorf("stored canonical workflow plan is missing")
	}
	sum := sha256.Sum256(workflow.CanonicalPlan)
	if fmt.Sprintf("%x", sum[:]) != workflow.PlanHash {
		return nil, fmt.Errorf("stored canonical workflow plan hash mismatch")
	}
	var plan struct {
		SchemaVersion       int       `json:"schema_version"`
		Goal                string    `json:"goal"`
		CoordinatorAgentID  uuid.UUID `json:"coordinator_agent_id"`
		CoordinatorAgentKey string    `json:"coordinator_agent_key"`
		FinalOwnerAgentID   uuid.UUID `json:"final_owner_agent_id"`
		FinalOwnerAgentKey  string    `json:"final_owner_agent_key"`
		TerminalStepID      string    `json:"terminal_step_id"`
		Steps               []struct {
			ID             string    `json:"id"`
			Title          string    `json:"title"`
			Instruction    string    `json:"instruction"`
			OwnerAgentID   uuid.UUID `json:"owner_agent_id"`
			OwnerAgentKey  string    `json:"owner_agent_key"`
			RequiredTools  []string  `json:"required_tools"`
			DependsOn      []string  `json:"depends_on"`
			RequiredOutput bool      `json:"required_output"`
			Terminal       bool      `json:"terminal"`
		} `json:"steps"`
	}
	if err := json.Unmarshal(workflow.CanonicalPlan, &plan); err != nil {
		return nil, fmt.Errorf("decode stored canonical plan: %w", err)
	}
	constraint := &TeamWorkPlanConstraint{
		SchemaVersion: plan.SchemaVersion, Goal: plan.Goal,
		CoordinatorAgentID: plan.CoordinatorAgentID, CoordinatorAgentKey: plan.CoordinatorAgentKey,
		FinalOwnerAgentID: plan.FinalOwnerAgentID, FinalOwnerAgentKey: plan.FinalOwnerAgentKey,
		TerminalStepID: plan.TerminalStepID, CanonicalPlan: workflow.CanonicalPlan, PlanHash: workflow.PlanHash,
	}
	for _, step := range plan.Steps {
		constraint.Steps = append(constraint.Steps, TeamWorkPlanStepConstraint{
			ID: step.ID, Title: step.Title, Instruction: step.Instruction,
			OwnerAgentID: step.OwnerAgentID, OwnerAgentKey: step.OwnerAgentKey,
			RequiredTools: step.RequiredTools, DependsOn: step.DependsOn,
			RequiredOutput: step.RequiredOutput, Terminal: step.Terminal,
		})
	}
	if constraint.CoordinatorAgentID != workflow.CoordinatorAgentID || constraint.CoordinatorAgentKey != workflow.CoordinatorAgentKey {
		return nil, fmt.Errorf("stored workflow coordinator mismatch")
	}
	return buildWorkflowTasks(constraint, workflow)
}

// buildWorkflowStepDescription renders the instruction a step owner actually
// receives. A step instruction alone is not self-sufficient: the planner writes
// it against the plan goal, so a worker handed only "conduct market analysis:
// competitive landscape, target segments..." cannot tell WHICH product it is
// analysing and blocks to ask the coordinator — burning a dispatch and stalling
// the DAG. The goal and the step's place in the plan are backend-derived from
// the frozen constraint, never from model tool arguments, so this adds context
// without widening the trusted surface.
func buildWorkflowStepDescription(constraint *TeamWorkPlanConstraint, step TeamWorkPlanStepConstraint) string {
	var b strings.Builder
	if goal := strings.TrimSpace(constraint.Goal); goal != "" {
		b.WriteString("[Workflow goal — the outcome this whole plan must deliver]\n")
		b.WriteString(goal)
		b.WriteString("\n\n[Your step]\n")
	}
	b.WriteString(step.Instruction)
	// Name the upstream steps by title so a worker knows what already exists and
	// who owns it. Results themselves arrive at dispatch time via
	// buildBlockerResultsSummary, which can only see COMPLETED blockers.
	if len(step.DependsOn) > 0 {
		titles := make([]string, 0, len(step.DependsOn))
		for _, dep := range step.DependsOn {
			for _, other := range constraint.Steps {
				if other.ID == dep {
					titles = append(titles, fmt.Sprintf("%s (%s)", other.Title, other.OwnerAgentKey))
					break
				}
			}
		}
		if len(titles) > 0 {
			b.WriteString("\n\nThis step builds on: " + strings.Join(titles, "; ") +
				". Their results are appended below when they finish.")
		}
	}
	if step.Terminal {
		b.WriteString("\n\nThis is the final integration step: your result is delivered to the requester.")
	}
	if len(step.RequiredTools) > 0 {
		b.WriteString("\n\nRequired tools: " + strings.Join(step.RequiredTools, ", "))
	}
	return b.String()
}

func workflowStepMetadata(workflow *store.TeamWorkflowData, blockedBy []uuid.UUID) map[string]any {
	metadata := map[string]any{
		TaskMetaOriginSession: workflow.OriginSessionKey,
		TaskMetaPeerKind:      workflow.OriginPeerKind,
		TaskMetaLocalKey:      workflow.OriginLocalKey,
		"origin_sender_id":    workflow.OriginSenderID,
		"origin_role":         workflow.OriginRole,
		TaskMetaOriginRouting: string(workflow.OriginRouting),
	}
	if len(blockedBy) > 0 {
		ids := make([]string, len(blockedBy))
		for i, id := range blockedBy {
			ids[i] = id.String()
		}
		metadata["original_blocked_by"] = ids
	}
	return metadata
}

// InheritWorkflowTaskContext copies the canonical shared-workspace metadata
// from the durable request audit task to reconstructed workflow steps.
func InheritWorkflowTaskContext(tasks []store.TeamTaskData, source *store.TeamTaskData) {
	if source == nil || source.Metadata == nil {
		return
	}
	for i := range tasks {
		if tasks[i].Metadata == nil {
			tasks[i].Metadata = make(map[string]any)
		}
		for _, key := range []string{TaskMetaTeamWorkspace, TaskMetaPeerKind, TaskMetaLocalKey, TaskMetaOriginSession, TaskMetaOriginTrace, TaskMetaOriginRootSpan} {
			if value, exists := source.Metadata[key]; exists {
				tasks[i].Metadata[key] = value
			}
		}
	}
}

func setWorkflowTaskWorkspace(ctx context.Context, manager TeamToolBackend, team *store.TeamData, tasks []store.TeamTaskData) {
	SetWorkflowReplanTaskWorkspace(ctx, manager.DataDir(), team, tasks)
}

// SetWorkflowReplanTaskWorkspace applies the same canonical team workspace
// metadata to backend-built replacement tasks as initial workflow creation.
func SetWorkflowReplanTaskWorkspace(ctx context.Context, dataDir string, team *store.TeamData, tasks []store.TeamTaskData) {
	workspace := ResolveWorkspace(dataDir,
		TenantLayer(store.TenantIDFromContext(ctx), store.TenantSlugFromContext(ctx)),
		TeamLayer(team.ID),
		UserChatLayer(ToolChatIDFromCtx(ctx), IsSharedWorkspace(team.Settings)),
	)
	for i := range tasks {
		if tasks[i].Metadata == nil {
			tasks[i].Metadata = make(map[string]any)
		}
		tasks[i].Metadata[TaskMetaTeamWorkspace] = workspace
	}
}

func queueWorkflowRoots(ctx context.Context, teamID uuid.UUID, tasks []store.TeamTaskData) {
	ptd := PendingTeamDispatchFromCtx(ctx)
	if ptd == nil {
		return
	}
	for _, task := range tasks {
		if task.Status == store.TeamTaskStatusPending && task.WorkflowKind == store.TeamWorkflowTaskKindWork {
			ptd.Add(teamID, task.ID)
		}
	}
}
