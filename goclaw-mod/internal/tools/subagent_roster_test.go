package tools

import (
	"strings"
	"testing"

	"github.com/google/uuid"
)

func TestRosterForParentSeparatesActiveFromRetainedTasks(t *testing.T) {
	tenantID := uuid.New()
	rootID := uuid.New()
	manager := NewSubagentManager(nil, nil, "", nil, nil, SubagentConfig{
		MaxChildrenPerAgent: 5,
	})
	manager.tasks = map[string]*SubagentTask{
		"completed": {
			ID:             "completed",
			ParentID:       "parent",
			RootAgentID:    rootID,
			RootAgentKey:   "parent",
			OriginTenantID: tenantID,
			Label:          "completed task",
			Status:         TaskStatusCompleted,
			CreatedAt:      1,
			spawnConfig:    SubagentConfig{MaxChildrenPerAgent: 3},
		},
		"running": {
			ID:             "running",
			ParentID:       "parent",
			RootAgentID:    rootID,
			RootAgentKey:   "parent",
			OriginTenantID: tenantID,
			Label:          "running task",
			Status:         TaskStatusRunning,
			CreatedAt:      2,
			spawnConfig:    SubagentConfig{MaxChildrenPerAgent: 7},
		},
		"failed": {
			ID:             "failed",
			ParentID:       "parent",
			RootAgentID:    rootID,
			RootAgentKey:   "parent",
			OriginTenantID: tenantID,
			Label:          "failed task",
			Status:         TaskStatusFailed,
			CreatedAt:      3,
			spawnConfig:    SubagentConfig{MaxChildrenPerAgent: 9},
		},
	}

	roster := manager.RosterForParent(TaskScope{TenantID: tenantID, RootAgentID: rootID, RootAgentKey: "parent"})
	if roster.Active != 1 || roster.Total != 3 || roster.MaxPerAgent != 9 {
		t.Fatalf("roster counts = %#v, want 1 active / 3 retained / max 9", roster)
	}

	instruction := BuildReplyInstruction(roster)
	if !strings.Contains(instruction, "1 active / 9 max; 3 retained total") {
		t.Fatalf("instruction missing active/retained counts:\n%s", instruction)
	}
	if !strings.Contains(instruction, "1 subagent(s) still running") {
		t.Fatalf("instruction missing running state:\n%s", instruction)
	}
}
