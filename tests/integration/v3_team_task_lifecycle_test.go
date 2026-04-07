//go:build integration

package integration

import (
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/nextlevelbuilder/goclaw/internal/store"
	"github.com/nextlevelbuilder/goclaw/internal/store/pg"
)

func TestStoreTask_ClaimAndComplete(t *testing.T) {
	db := testDB(t)
	pg.InitSqlx(db)
	tenantID, agentID := seedTenantAgent(t, db)
	ctx := tenantCtx(tenantID)
	ts := pg.NewPGTeamStore(db)

	teamID, memberID := seedTeam(t, db, tenantID, agentID)

	task := makeTask(teamID, "claim and complete", store.TeamTaskStatusPending)
	if err := ts.CreateTask(ctx, task); err != nil {
		t.Fatalf("CreateTask: %v", err)
	}

	if err := ts.ClaimTask(ctx, task.ID, memberID, teamID); err != nil {
		t.Fatalf("ClaimTask: %v", err)
	}

	// Verify in_progress + lock fields set.
	got, err := ts.GetTask(ctx, task.ID)
	if err != nil {
		t.Fatalf("GetTask after claim: %v", err)
	}
	if got.Status != store.TeamTaskStatusInProgress {
		t.Errorf("Status after claim: expected %q, got %q", store.TeamTaskStatusInProgress, got.Status)
	}
	if got.OwnerAgentID == nil || *got.OwnerAgentID != memberID {
		t.Errorf("OwnerAgentID after claim: expected %v, got %v", memberID, got.OwnerAgentID)
	}
	if got.LockedAt == nil {
		t.Error("LockedAt should be set after claim")
	}
	if got.LockExpiresAt == nil {
		t.Error("LockExpiresAt should be set after claim")
	}

	// Complete.
	if err := ts.CompleteTask(ctx, task.ID, teamID, "all done"); err != nil {
		t.Fatalf("CompleteTask: %v", err)
	}

	completed, err := ts.GetTask(ctx, task.ID)
	if err != nil {
		t.Fatalf("GetTask after complete: %v", err)
	}
	if completed.Status != store.TeamTaskStatusCompleted {
		t.Errorf("Status after complete: expected %q, got %q", store.TeamTaskStatusCompleted, completed.Status)
	}
	if completed.LockedAt != nil {
		t.Error("LockedAt should be cleared after complete")
	}
	if completed.Result == nil || *completed.Result != "all done" {
		t.Errorf("Result: expected %q, got %v", "all done", completed.Result)
	}
}

func TestStoreTask_ReviewFlow(t *testing.T) {
	db := testDB(t)
	pg.InitSqlx(db)
	tenantID, agentID := seedTenantAgent(t, db)
	ctx := tenantCtx(tenantID)
	ts := pg.NewPGTeamStore(db)

	teamID, memberID := seedTeam(t, db, tenantID, agentID)

	// --- Approve path ---
	taskA := makeTask(teamID, "review approve", store.TeamTaskStatusPending)
	if err := ts.CreateTask(ctx, taskA); err != nil {
		t.Fatalf("CreateTask A: %v", err)
	}
	if err := ts.ClaimTask(ctx, taskA.ID, memberID, teamID); err != nil {
		t.Fatalf("ClaimTask A: %v", err)
	}
	if err := ts.ReviewTask(ctx, taskA.ID, teamID); err != nil {
		t.Fatalf("ReviewTask A: %v", err)
	}
	gotA, _ := ts.GetTask(ctx, taskA.ID)
	if gotA.Status != store.TeamTaskStatusInReview {
		t.Errorf("after ReviewTask: expected in_review, got %q", gotA.Status)
	}
	if err := ts.ApproveTask(ctx, taskA.ID, teamID, "looks good"); err != nil {
		t.Fatalf("ApproveTask: %v", err)
	}
	gotA2, _ := ts.GetTask(ctx, taskA.ID)
	if gotA2.Status != store.TeamTaskStatusCompleted {
		t.Errorf("after ApproveTask: expected completed, got %q", gotA2.Status)
	}

	// --- Reject path ---
	taskB := makeTask(teamID, "review reject", store.TeamTaskStatusPending)
	if err := ts.CreateTask(ctx, taskB); err != nil {
		t.Fatalf("CreateTask B: %v", err)
	}
	if err := ts.ClaimTask(ctx, taskB.ID, memberID, teamID); err != nil {
		t.Fatalf("ClaimTask B: %v", err)
	}
	if err := ts.ReviewTask(ctx, taskB.ID, teamID); err != nil {
		t.Fatalf("ReviewTask B: %v", err)
	}
	if err := ts.RejectTask(ctx, taskB.ID, teamID, "needs rework"); err != nil {
		t.Fatalf("RejectTask: %v", err)
	}
	gotB, _ := ts.GetTask(ctx, taskB.ID)
	if gotB.Status != store.TeamTaskStatusCancelled {
		t.Errorf("after RejectTask: expected cancelled, got %q", gotB.Status)
	}
}

func TestStoreTask_RecoverStaleTasks(t *testing.T) {
	db := testDB(t)
	pg.InitSqlx(db)
	tenantID, agentID := seedTenantAgent(t, db)
	ctx := tenantCtx(tenantID)
	ts := pg.NewPGTeamStore(db)

	teamID, memberID := seedTeam(t, db, tenantID, agentID)

	task := makeTask(teamID, "stale task", store.TeamTaskStatusPending)
	if err := ts.CreateTask(ctx, task); err != nil {
		t.Fatalf("CreateTask: %v", err)
	}
	if err := ts.ClaimTask(ctx, task.ID, memberID, teamID); err != nil {
		t.Fatalf("ClaimTask: %v", err)
	}

	// Expire lock by backdating lock_expires_at to the past.
	past := time.Now().Add(-2 * time.Hour)
	if _, err := db.Exec(
		`UPDATE team_tasks SET lock_expires_at = $1 WHERE id = $2`,
		past, task.ID,
	); err != nil {
		t.Fatalf("backdate lock_expires_at: %v", err)
	}

	recovered, err := ts.RecoverAllStaleTasks(ctx)
	if err != nil {
		t.Fatalf("RecoverAllStaleTasks: %v", err)
	}

	found := false
	for _, r := range recovered {
		if r.ID == task.ID {
			found = true
		}
	}
	if !found {
		t.Error("stale task not found in recovered list")
	}

	// Verify task reset to pending.
	got, err := ts.GetTask(ctx, task.ID)
	if err != nil {
		t.Fatalf("GetTask after recovery: %v", err)
	}
	if got.Status != store.TeamTaskStatusPending {
		t.Errorf("after recovery: expected pending, got %q", got.Status)
	}
	if got.LockedAt != nil {
		t.Error("LockedAt should be cleared after recovery")
	}
}

func TestStoreTask_FixOrphanedBlocked(t *testing.T) {
	db := testDB(t)
	pg.InitSqlx(db)
	tenantID, agentID := seedTenantAgent(t, db)
	ctx := tenantCtx(tenantID)
	ts := pg.NewPGTeamStore(db)

	teamID, memberID := seedTeam(t, db, tenantID, agentID)

	// taskA: create and complete it.
	taskA := makeTask(teamID, "blocker task", store.TeamTaskStatusPending)
	if err := ts.CreateTask(ctx, taskA); err != nil {
		t.Fatalf("CreateTask A: %v", err)
	}
	if err := ts.ClaimTask(ctx, taskA.ID, memberID, teamID); err != nil {
		t.Fatalf("ClaimTask A: %v", err)
	}
	if err := ts.CompleteTask(ctx, taskA.ID, teamID, "done"); err != nil {
		t.Fatalf("CompleteTask A: %v", err)
	}

	// taskB: create as blocked by taskA, then manually force blocked status
	// (simulates a crash that left unblockDependentTasks incomplete).
	taskB := makeTask(teamID, "blocked task", store.TeamTaskStatusPending)
	if err := ts.CreateTask(ctx, taskB); err != nil {
		t.Fatalf("CreateTask B: %v", err)
	}
	// Force taskB into orphaned blocked state: blocked_by=[taskA.ID] but taskA is completed.
	if _, err := db.Exec(
		`UPDATE team_tasks SET status = 'blocked', blocked_by = $1 WHERE id = $2`,
		[]uuid.UUID{taskA.ID}, taskB.ID,
	); err != nil {
		t.Fatalf("force blocked: %v", err)
	}

	recovered, err := ts.FixOrphanedBlockedTasks(ctx)
	if err != nil {
		t.Fatalf("FixOrphanedBlockedTasks: %v", err)
	}

	found := false
	for _, r := range recovered {
		if r.ID == taskB.ID {
			found = true
		}
	}
	if !found {
		t.Error("orphaned blocked task not found in fixed list")
	}

	got, err := ts.GetTask(ctx, taskB.ID)
	if err != nil {
		t.Fatalf("GetTask B after fix: %v", err)
	}
	if got.Status != store.TeamTaskStatusPending {
		t.Errorf("after fix: expected pending, got %q", got.Status)
	}
	if len(got.BlockedBy) != 0 {
		t.Errorf("blocked_by should be empty after fix, got %v", got.BlockedBy)
	}
}

func TestStoreTask_FollowupSchedule(t *testing.T) {
	db := testDB(t)
	pg.InitSqlx(db)
	tenantID, agentID := seedTenantAgent(t, db)
	ctx := tenantCtx(tenantID)
	ts := pg.NewPGTeamStore(db)

	teamID, memberID := seedTeam(t, db, tenantID, agentID)

	task := makeTask(teamID, "followup task", store.TeamTaskStatusPending)
	if err := ts.CreateTask(ctx, task); err != nil {
		t.Fatalf("CreateTask: %v", err)
	}
	if err := ts.ClaimTask(ctx, task.ID, memberID, teamID); err != nil {
		t.Fatalf("ClaimTask: %v", err)
	}

	futureAt := time.Now().Add(1 * time.Hour)
	if err := ts.SetTaskFollowup(ctx, task.ID, teamID, futureAt, 3, "check in", "telegram", "chat123"); err != nil {
		t.Fatalf("SetTaskFollowup: %v", err)
	}

	// Backdate followup_at to past so it becomes due.
	past := time.Now().Add(-1 * time.Minute)
	if _, err := db.Exec(
		`UPDATE team_tasks SET followup_at = $1 WHERE id = $2`,
		past, task.ID,
	); err != nil {
		t.Fatalf("backdate followup_at: %v", err)
	}

	due, err := ts.ListAllFollowupDueTasks(ctx)
	if err != nil {
		t.Fatalf("ListAllFollowupDueTasks: %v", err)
	}

	found := false
	for _, d := range due {
		if d.ID == task.ID {
			found = true
			if d.FollowupMessage != "check in" {
				t.Errorf("FollowupMessage: expected %q, got %q", "check in", d.FollowupMessage)
			}
		}
	}
	if !found {
		t.Error("due followup task not found")
	}
}

func TestStoreTask_Comments(t *testing.T) {
	db := testDB(t)
	pg.InitSqlx(db)
	tenantID, agentID := seedTenantAgent(t, db)
	ctx := tenantCtx(tenantID)
	ts := pg.NewPGTeamStore(db)

	teamID, _ := seedTeam(t, db, tenantID, agentID)

	task := makeTask(teamID, "comment task", store.TeamTaskStatusPending)
	if err := ts.CreateTask(ctx, task); err != nil {
		t.Fatalf("CreateTask: %v", err)
	}

	comment := &store.TeamTaskCommentData{
		TaskID:  task.ID,
		AgentID: &agentID,
		Content: "first comment",
	}
	if err := ts.AddTaskComment(ctx, comment); err != nil {
		t.Fatalf("AddTaskComment: %v", err)
	}
	if comment.ID == uuid.Nil {
		t.Error("AddTaskComment did not assign ID")
	}

	comments, err := ts.ListTaskComments(ctx, task.ID)
	if err != nil {
		t.Fatalf("ListTaskComments: %v", err)
	}
	if len(comments) != 1 {
		t.Fatalf("expected 1 comment, got %d", len(comments))
	}
	if comments[0].Content != "first comment" {
		t.Errorf("Content: expected %q, got %q", "first comment", comments[0].Content)
	}
	if comments[0].AgentID == nil || *comments[0].AgentID != agentID {
		t.Errorf("AgentID mismatch: expected %v, got %v", agentID, comments[0].AgentID)
	}
}

func TestStoreTask_AccessControl(t *testing.T) {
	db := testDB(t)
	pg.InitSqlx(db)
	tenantID, agentID := seedTenantAgent(t, db)
	ctx := tenantCtx(tenantID)
	ts := pg.NewPGTeamStore(db)

	teamID, _ := seedTeam(t, db, tenantID, agentID)

	userID := "user-" + uuid.New().String()[:8]

	// Grant access.
	if err := ts.GrantTeamAccess(ctx, teamID, userID, "viewer", "test"); err != nil {
		t.Fatalf("GrantTeamAccess: %v", err)
	}

	has, err := ts.HasTeamAccess(ctx, teamID, userID)
	if err != nil {
		t.Fatalf("HasTeamAccess: %v", err)
	}
	if !has {
		t.Error("HasTeamAccess: expected true after grant")
	}

	// Revoke access.
	if err := ts.RevokeTeamAccess(ctx, teamID, userID); err != nil {
		t.Fatalf("RevokeTeamAccess: %v", err)
	}

	has2, err := ts.HasTeamAccess(ctx, teamID, userID)
	if err != nil {
		t.Fatalf("HasTeamAccess after revoke: %v", err)
	}
	if has2 {
		t.Error("HasTeamAccess: expected false after revoke")
	}
}
