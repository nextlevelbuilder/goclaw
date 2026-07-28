package store

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/google/uuid"
)

// ValidateWorkflowReplan defensively verifies the backend-frozen replacement
// plan and concrete task graph before either store backend opens a transaction.
func ValidateWorkflowReplan(replan WorkflowReplan) error {
	if len(replan.CanonicalPlan) == 0 || !json.Valid(replan.CanonicalPlan) {
		return fmt.Errorf("replan canonical plan is not valid JSON")
	}
	sum := sha256.Sum256(replan.CanonicalPlan)
	if replan.PlanHash != hex.EncodeToString(sum[:]) {
		return fmt.Errorf("replan canonical plan hash mismatch")
	}
	if len(replan.Tasks) == 0 {
		return fmt.Errorf("replan requires at least one task")
	}

	taskIDs := make(map[uuid.UUID]struct{}, len(replan.Tasks))
	stepIDs := make(map[string]struct{}, len(replan.Tasks))
	for i := range replan.Tasks {
		task := &replan.Tasks[i]
		if task.ID == uuid.Nil {
			return fmt.Errorf("replacement task %d has a nil ID", i)
		}
		if _, exists := taskIDs[task.ID]; exists {
			return fmt.Errorf("duplicate replacement task ID %s", task.ID)
		}
		taskIDs[task.ID] = struct{}{}

		stepID := strings.TrimSpace(task.WorkflowStepID)
		if stepID == "" {
			return fmt.Errorf("replacement task %s has an empty workflow step ID", task.ID)
		}
		if _, exists := stepIDs[stepID]; exists {
			return fmt.Errorf("duplicate replacement workflow step ID %q", stepID)
		}
		stepIDs[stepID] = struct{}{}

		if task.OwnerAgentID == nil || *task.OwnerAgentID == uuid.Nil {
			return fmt.Errorf("replacement task %s has no canonical owner", task.ID)
		}
	}

	terminalCount := 0
	for i := range replan.Tasks {
		task := &replan.Tasks[i]
		if task.WorkflowTerminal {
			terminalCount++
		}

		wantStatus := TeamTaskStatusPending
		if len(task.BlockedBy) > 0 {
			wantStatus = TeamTaskStatusBlocked
		}
		if task.Status != wantStatus {
			return fmt.Errorf(
				"replacement task %s status %q does not match dependencies (want %q)",
				task.ID, task.Status, wantStatus,
			)
		}

		dependencies := make(map[uuid.UUID]struct{}, len(task.BlockedBy))
		for _, dependencyID := range task.BlockedBy {
			if dependencyID == uuid.Nil {
				return fmt.Errorf("replacement task %s has a nil dependency", task.ID)
			}
			if dependencyID == task.ID {
				return fmt.Errorf("replacement task %s depends on itself", task.ID)
			}
			if _, exists := taskIDs[dependencyID]; !exists {
				return fmt.Errorf("replacement task %s depends on unknown task %s", task.ID, dependencyID)
			}
			if _, exists := dependencies[dependencyID]; exists {
				return fmt.Errorf("replacement task %s repeats dependency %s", task.ID, dependencyID)
			}
			dependencies[dependencyID] = struct{}{}
		}
	}

	if terminalCount != 1 {
		return fmt.Errorf("replan requires exactly one terminal replacement, got %d", terminalCount)
	}
	return nil
}
