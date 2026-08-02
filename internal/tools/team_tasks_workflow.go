package tools

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"strings"

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
