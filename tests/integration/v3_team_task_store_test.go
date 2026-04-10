//go:build integration

package integration

import (
	"sync"
	"testing"

	"github.com/google/uuid"
	"github.com/nextlevelbuilder/goclaw/internal/store"
	"github.com/nextlevelbuilder/goclaw/internal/store/pg"
)

func makeTask(teamID uuid.UUID, subject, status string) *store.TeamTaskData {
	return &store.TeamTaskData{
		TeamID:  teamID,
		Subject: subject,
		Status:  status,
	}
}

func TestStoreTask_CreateAndGet(t *testing.T) {
	db := testDB(t)
	pg.InitSqlx(db)
	tenantID, agentID := seedTenantAgent(t, db)
	ctx := tenantCtx(tenantID)
	ts := pg.NewPGTeamStore(db)

	teamID, _ := seedTeam(t, db, tenantID, agentID)

	task := makeTask(teamID, "write unit tests", store.TeamTaskStatusPending)
	if err := ts.CreateTask(ctx, task); err != nil {
		t.Fatalf("CreateTask: %v", err)
	}
	if task.ID == uuid.Nil {
		t.Fatal("CreateTask did not assign ID")
	}
	if task.TaskNumber == 0 {
		t.Error("task_number not generated")
	}
	if task.Identifier == "" {
		t.Error("identifier not generated")
	}

	got, err := ts.GetTask(ctx, task.ID)
	if err != nil {
		t.Fatalf("GetTask: %v", err)
	}
	if got.Subject != task.Subject {
		t.Errorf("Subject: expected %q, got %q", task.Subject, got.Subject)
	}
	if got.TaskNumber != task.TaskNumber {
		t.Errorf("TaskNumber: expected %d, got %d", task.TaskNumber, got.TaskNumber)
	}
	if got.Status != store.TeamTaskStatusPending {
		t.Errorf("Status: expected %q, got %q", store.TeamTaskStatusPending, got.Status)
	}
}

func TestStoreTask_ConcurrentCreate(t *testing.T) {
	db := testDB(t)
	pg.InitSqlx(db)
	tenantID, agentID := seedTenantAgent(t, db)
	ctx := tenantCtx(tenantID)
	ts := pg.NewPGTeamStore(db)

	teamID, _ := seedTeam(t, db, tenantID, agentID)

	const n = 10
	errs := make([]error, n)
	nums := make([]int, n)
	var wg sync.WaitGroup
	wg.Add(n)
	for i := range n {
		go func(i int) {
			defer wg.Done()
			task := makeTask(teamID, "concurrent task", store.TeamTaskStatusPending)
			errs[i] = ts.CreateTask(ctx, task)
			nums[i] = task.TaskNumber
		}(i)
	}
	wg.Wait()

	for i, err := range errs {
		if err != nil {
			t.Errorf("goroutine %d CreateTask: %v", i, err)
		}
	}

	// All task_numbers must be unique.
	seen := map[int]bool{}
	for _, n := range nums {
		if seen[n] {
			t.Errorf("duplicate task_number %d", n)
		}
		seen[n] = true
	}
}

func TestStoreTask_ListWithFilters(t *testing.T) {
	db := testDB(t)
	pg.InitSqlx(db)
	tenantID, agentID := seedTenantAgent(t, db)
	ctx := tenantCtx(tenantID)
	ts := pg.NewPGTeamStore(db)

	teamID, memberID := seedTeam(t, db, tenantID, agentID)

	// Create a pending task.
	pending := makeTask(teamID, "pending task", store.TeamTaskStatusPending)
	if err := ts.CreateTask(ctx, pending); err != nil {
		t.Fatalf("CreateTask pending: %v", err)
	}

	// Create and claim a task to make it in_progress.
	inprog := makeTask(teamID, "in progress task", store.TeamTaskStatusPending)
	if err := ts.CreateTask(ctx, inprog); err != nil {
		t.Fatalf("CreateTask inprog: %v", err)
	}
	if err := ts.ClaimTask(ctx, inprog.ID, memberID, teamID); err != nil {
		t.Fatalf("ClaimTask: %v", err)
	}

	// Filter active: should include both pending + in_progress.
	active, err := ts.ListTasks(ctx, teamID, "", store.TeamTaskFilterActive, "", "", "", 30, 0)
	if err != nil {
		t.Fatalf("ListTasks active: %v", err)
	}
	if len(active) < 2 {
		t.Errorf("active filter: expected >= 2, got %d", len(active))
	}

	// Complete the in-progress task so we can test completed filter.
	if err := ts.CompleteTask(ctx, inprog.ID, teamID, "done"); err != nil {
		t.Fatalf("CompleteTask: %v", err)
	}

	completed, err := ts.ListTasks(ctx, teamID, "", store.TeamTaskFilterCompleted, "", "", "", 30, 0)
	if err != nil {
		t.Fatalf("ListTasks completed: %v", err)
	}
	found := false
	for _, tk := range completed {
		if tk.ID == inprog.ID {
			found = true
		}
	}
	if !found {
		t.Error("completed filter: task not found in results")
	}
}

func TestStoreTask_DeleteSingleAndBulk(t *testing.T) {
	db := testDB(t)
	pg.InitSqlx(db)
	tenantID, agentID := seedTenantAgent(t, db)
	ctx := tenantCtx(tenantID)
	ts := pg.NewPGTeamStore(db)

	teamID, _ := seedTeam(t, db, tenantID, agentID)

	// Single delete: task must be in terminal status.
	task1 := makeTask(teamID, "delete me", store.TeamTaskStatusPending)
	if err := ts.CreateTask(ctx, task1); err != nil {
		t.Fatalf("CreateTask: %v", err)
	}
	// Cancel to make it terminal.
	if err := ts.CancelTask(ctx, task1.ID, teamID, "test cancel"); err != nil {
		t.Fatalf("CancelTask: %v", err)
	}
	if err := ts.DeleteTask(ctx, task1.ID, teamID); err != nil {
		t.Fatalf("DeleteTask: %v", err)
	}
	if _, err := ts.GetTask(ctx, task1.ID); err != store.ErrTaskNotFound {
		t.Errorf("after DeleteTask: expected ErrTaskNotFound, got %v", err)
	}

	// Bulk delete: create 2 completed tasks.
	task2 := makeTask(teamID, "bulk delete 1", store.TeamTaskStatusPending)
	task3 := makeTask(teamID, "bulk delete 2", store.TeamTaskStatusPending)
	for _, tk := range []*store.TeamTaskData{task2, task3} {
		if err := ts.CreateTask(ctx, tk); err != nil {
			t.Fatalf("CreateTask bulk: %v", err)
		}
		if err := ts.CancelTask(ctx, tk.ID, teamID, "bulk cancel"); err != nil {
			t.Fatalf("CancelTask bulk: %v", err)
		}
	}

	deleted, err := ts.DeleteTasks(ctx, []uuid.UUID{task2.ID, task3.ID}, teamID)
	if err != nil {
		t.Fatalf("DeleteTasks: %v", err)
	}
	if len(deleted) != 2 {
		t.Errorf("DeleteTasks: expected 2 deleted, got %d", len(deleted))
	}
}
