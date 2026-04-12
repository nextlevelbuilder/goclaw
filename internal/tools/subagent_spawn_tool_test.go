package tools

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/nextlevelbuilder/goclaw/internal/pipeline"
	"github.com/nextlevelbuilder/goclaw/internal/store"
)

func TestSpawnTool_EmitsCapacityConstraintForChildLimit(t *testing.T) {
	tenantID := uuid.New()
	manager := NewSubagentManager(nil, nil, "", nil, nil, SubagentConfig{
		MaxConcurrent:       4,
		MaxSpawnDepth:       3,
		MaxChildrenPerAgent: 1,
	})
	manager.tasks["child-1"] = &SubagentTask{
		ID:             "child-1",
		ParentID:       "parent-1",
		Status:         TaskStatusRunning,
		OriginTenantID: tenantID,
	}

	tool := NewSpawnTool(manager, "parent-1", 0)
	ctx := store.WithTenantID(context.Background(), tenantID)
	result := tool.Execute(ctx, map[string]any{"task": "analyze session"})

	if !result.IsError {
		t.Fatal("expected error result")
	}
	if len(result.Constraints) != 1 {
		t.Fatalf("constraint count = %d, want 1", len(result.Constraints))
	}
	if result.Constraints[0].Kind != pipeline.ConstraintCapacityExhausted {
		t.Fatalf("constraint kind = %q, want %q", result.Constraints[0].Kind, pipeline.ConstraintCapacityExhausted)
	}
	if result.Constraints[0].Subject != string(SpawnLimitChildren) {
		t.Fatalf("constraint subject = %q, want %q", result.Constraints[0].Subject, SpawnLimitChildren)
	}
}

func TestSpawnTool_EmitsCapacityConstraintForDepthLimit(t *testing.T) {
	manager := NewSubagentManager(nil, nil, "", nil, nil, SubagentConfig{
		MaxConcurrent:       4,
		MaxSpawnDepth:       1,
		MaxChildrenPerAgent: 4,
	})
	tool := NewSpawnTool(manager, "parent-1", 1)
	ctx := store.WithTenantID(context.Background(), uuid.New())
	result := tool.Execute(ctx, map[string]any{"task": "analyze session"})

	if !result.IsError {
		t.Fatal("expected error result")
	}
	if len(result.Constraints) != 1 {
		t.Fatalf("constraint count = %d, want 1", len(result.Constraints))
	}
	if result.Constraints[0].Subject != string(SpawnLimitDepth) {
		t.Fatalf("constraint subject = %q, want %q", result.Constraints[0].Subject, SpawnLimitDepth)
	}
}
