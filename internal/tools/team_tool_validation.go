package tools

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/nextlevelbuilder/goclaw/internal/bus"
	"github.com/nextlevelbuilder/goclaw/internal/store"
	"github.com/nextlevelbuilder/goclaw/pkg/protocol"
)

// PostTurnProcessor validates and dispatches pending team tasks after an agent turn.
type PostTurnProcessor interface {
	ProcessPendingTasks(ctx context.Context, teamID uuid.UUID, taskIDs []uuid.UUID) error
	// DispatchUnblockedTasks finds pending tasks with an owner and dispatches them.
	// Called by the consumer after auto-completing a task to unblock dependent work.
	DispatchUnblockedTasks(ctx context.Context, teamID uuid.UUID)
}

// ProcessPendingTasks validates tasks created during a turn and dispatches unblocked ones.
// Called by the consumer after an agent turn ends.
func (m *TeamToolManager) ProcessPendingTasks(ctx context.Context, teamID uuid.UUID, taskIDs []uuid.UUID) error {
	if len(taskIDs) == 0 {
		return nil
	}

	// Fetch all tasks created in this turn.
	tasks := make([]*store.TeamTaskData, 0, len(taskIDs))
	for _, id := range taskIDs {
		t, err := m.teamStore.GetTask(ctx, id)
		if err != nil {
			slog.Warn("post_turn: cannot fetch task", "task_id", id, "error", err)
			continue
		}
		tasks = append(tasks, t)
	}
	if len(tasks) == 0 {
		return nil
	}

	// Build lookup: taskID → task (for cycle/ref validation).
	taskMap := make(map[uuid.UUID]*store.TeamTaskData, len(tasks))
	for _, t := range tasks {
		taskMap[t.ID] = t
	}

	// Validate blocked_by references and detect cycles.
	cycled, invalidRef := validateBlockedBy(taskMap)

	// Fail tasks with invalid blocked_by references.
	for taskID, badRef := range invalidRef {
		task := taskMap[taskID]
		errMsg := fmt.Sprintf("blocked_by references non-existent task %s", badRef)
		if err := m.teamStore.FailPendingTask(ctx, taskID, teamID, errMsg); err != nil {
			slog.Warn("post_turn: FailPendingTask error", "task_id", taskID, "error", err)
		}
		m.broadcastTeamEvent(protocol.EventTeamTaskFailed, protocol.TeamTaskEventPayload{
			TeamID:    teamID.String(),
			TaskID:    taskID.String(),
			Status:    store.TeamTaskStatusFailed,
			Timestamp: time.Now().UTC().Format("2006-01-02T15:04:05Z"),
			ActorType: "system",
			ActorID:   "post_turn",
		})
		slog.Warn("post_turn: task failed — invalid blocked_by",
			"task_id", taskID, "identifier", task.Identifier, "bad_ref", badRef)
	}

	// Fail cycled tasks and notify leader.
	if len(cycled) > 0 {
		m.failCycledTasks(ctx, teamID, cycled, taskMap)
	}

	// Dispatch pending assigned tasks (not blocked, not failed).
	for _, task := range tasks {
		if _, isCycled := cycled[task.ID]; isCycled {
			continue
		}
		if _, isInvalid := invalidRef[task.ID]; isInvalid {
			continue
		}
		if task.Status != store.TeamTaskStatusPending || task.OwnerAgentID == nil {
			continue
		}
		if err := m.teamStore.AssignTask(ctx, task.ID, *task.OwnerAgentID, teamID); err != nil {
			slog.Warn("post_turn: assign failed", "task_id", task.ID, "error", err)
			continue
		}
		m.broadcastTeamEvent(protocol.EventTeamTaskAssigned, protocol.TeamTaskEventPayload{
			TeamID:        teamID.String(),
			TaskID:        task.ID.String(),
			Status:        store.TeamTaskStatusInProgress,
			OwnerAgentKey: m.agentKeyFromID(ctx, *task.OwnerAgentID),
			Timestamp:     time.Now().UTC().Format("2006-01-02T15:04:05Z"),
			ActorType:     "system",
			ActorID:       "post_turn",
		})
		// Restore leader's trace context from task metadata (ctx here is the
		// consumer goroutine context which has no trace after the turn ends).
		dispatchCtx := m.restoreTraceContext(ctx, task)
		m.dispatchTaskToAgent(dispatchCtx, task, teamID, *task.OwnerAgentID)
	}

	slog.Info("post_turn: processed pending tasks",
		"team_id", teamID,
		"total", len(tasks),
		"dispatched", countPendingAssigned(tasks, cycled, invalidRef),
	)
	return nil
}

// failCycledTasks fails all tasks in the cycle and notifies the leader.
func (m *TeamToolManager) failCycledTasks(ctx context.Context, teamID uuid.UUID, cycled map[uuid.UUID]bool, taskMap map[uuid.UUID]*store.TeamTaskData) {
	// Build cycle description using task identifiers.
	var ids []string
	for id := range cycled {
		if t := taskMap[id]; t != nil {
			ids = append(ids, t.Identifier)
		}
	}
	cycleDesc := fmt.Sprintf("Circular dependency detected among tasks: %s", strings.Join(ids, " → "))

	for id := range cycled {
		if err := m.teamStore.FailPendingTask(ctx, id, teamID, cycleDesc); err != nil {
			slog.Warn("post_turn: FailPendingTask (cycle) error", "task_id", id, "error", err)
		}
		m.broadcastTeamEvent(protocol.EventTeamTaskFailed, protocol.TeamTaskEventPayload{
			TeamID:    teamID.String(),
			TaskID:    id.String(),
			Status:    store.TeamTaskStatusFailed,
			Timestamp: time.Now().UTC().Format("2006-01-02T15:04:05Z"),
			ActorType: "system",
			ActorID:   "post_turn",
		})
	}

	// Notify leader via system message.
	m.notifyLeaderCycleError(ctx, teamID, cycleDesc)
}

// notifyLeaderCycleError sends a system message to the leader about cycled tasks.
func (m *TeamToolManager) notifyLeaderCycleError(ctx context.Context, teamID uuid.UUID, cycleDesc string) {
	if m.msgBus == nil {
		return
	}
	team, err := m.teamStore.GetTeam(ctx, teamID)
	if err != nil {
		return
	}
	leadAgent, err := m.cachedGetAgentByID(ctx, team.LeadAgentID)
	if err != nil {
		return
	}
	content := fmt.Sprintf("[System] %s\nPlease recreate these tasks with corrected dependencies.", cycleDesc)
	m.msgBus.TryPublishInbound(bus.InboundMessage{
		Channel:  "system",
		SenderID: "teammate:system",
		ChatID:   teamID.String(),
		AgentID:  leadAgent.AgentKey,
		UserID:   team.CreatedBy,
		Content:  content,
		Metadata: map[string]string{
			"team_id":    teamID.String(),
			"from_agent": leadAgent.AgentKey,
			"to_agent":   leadAgent.AgentKey,
		},
	})
}

// validateBlockedBy checks blocked_by references and detects cycles using Kahn's algorithm.
// Only validates within the batch — out-of-batch blocked_by refs are assumed valid
// (already validated by executeCreate). Self-blocking is caught as invalid.
//
// Note: Cross-turn blocked_by references (tasks from previous turns) work at the DB
// level via unblockDependentTasks (WHERE $1 = ANY(blocked_by)). Cycle detection here
// only considers edges within the current batch — cross-turn deps are external.
//
// Returns: cycled task IDs, and map of taskID → first invalid blocked_by ref.
func validateBlockedBy(taskMap map[uuid.UUID]*store.TeamTaskData) (cycled map[uuid.UUID]bool, invalidRef map[uuid.UUID]uuid.UUID) {
	invalidRef = make(map[uuid.UUID]uuid.UUID)

	// Collect all task IDs in this batch.
	batchIDs := make(map[uuid.UUID]bool, len(taskMap))
	for id := range taskMap {
		batchIDs[id] = true
	}

	// Check for self-blocking only. Out-of-batch blocked_by refs are valid
	// (tasks from previous turns, already validated by executeCreate).
	for id, task := range taskMap {
		for _, dep := range task.BlockedBy {
			if dep == id {
				invalidRef[id] = dep
				break
			}
		}
	}

	// Cycle detection via Kahn's algorithm.
	// Only considers edges within the batch — out-of-batch deps are external
	// and don't participate in cycle detection.
	inDegree := make(map[uuid.UUID]int)
	adj := make(map[uuid.UUID][]uuid.UUID) // blocker → dependents

	for id, task := range taskMap {
		if _, bad := invalidRef[id]; bad {
			continue
		}
		if _, exists := inDegree[id]; !exists {
			inDegree[id] = 0
		}
		for _, dep := range task.BlockedBy {
			if _, bad := invalidRef[dep]; bad {
				continue
			}
			// Only consider edges within the batch for cycle detection.
			if !batchIDs[dep] {
				continue
			}
			adj[dep] = append(adj[dep], id)
			inDegree[id]++
		}
	}

	// BFS: process nodes with in-degree 0.
	var queue []uuid.UUID
	for id, deg := range inDegree {
		if deg == 0 {
			queue = append(queue, id)
		}
	}
	processed := make(map[uuid.UUID]bool)
	for len(queue) > 0 {
		node := queue[0]
		queue = queue[1:]
		processed[node] = true
		for _, dependent := range adj[node] {
			inDegree[dependent]--
			if inDegree[dependent] == 0 {
				queue = append(queue, dependent)
			}
		}
	}

	// Any node not processed is part of a cycle.
	cycled = make(map[uuid.UUID]bool)
	for id := range inDegree {
		if !processed[id] {
			cycled[id] = true
		}
	}
	return cycled, invalidRef
}

// countPendingAssigned counts tasks that were eligible for dispatch.
func countPendingAssigned(tasks []*store.TeamTaskData, cycled map[uuid.UUID]bool, invalidRef map[uuid.UUID]uuid.UUID) int {
	n := 0
	for _, t := range tasks {
		if _, c := cycled[t.ID]; c {
			continue
		}
		if _, i := invalidRef[t.ID]; i {
			continue
		}
		if t.Status == store.TeamTaskStatusPending && t.OwnerAgentID != nil {
			n++
		}
	}
	return n
}
