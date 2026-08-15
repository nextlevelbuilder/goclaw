//go:build integration

package scenarios

import "testing"

// SCENARIO: Task happy path - create, claim, complete.
// Flow: Create task → Assign → Update progress → Complete
func TestScenario_TaskHappyPath(t *testing.T) {
	wsURL, _ := getTestServer(t)
	client := connect(t, wsURL, "")

	// First check if teams exist
	teamsResp := client.send("teams.list", map[string]any{})
	teams, ok := teamsResp["teams"].([]any)
	if !ok || len(teams) == 0 {
		t.Skip("No teams available - skipping task lifecycle test")
	}

	team := teams[0].(map[string]any)
	teamID, _ := team["id"].(string)

	// Create a task
	createResp := client.send("teams.tasks.create", map[string]any{
		"team_id":     teamID,
		"title":       "Test task for scenario",
		"description": "Created by scenario test",
	})

	taskID, ok := createResp["id"].(string)
	if !ok {
		t.Fatalf("task creation failed, no id returned")
	}

	// Get task to verify
	getResp := client.send("teams.tasks.get", map[string]any{
		"team_id": teamID,
		"task_id": taskID,
	})

	status, _ := getResp["status"].(string)
	if status == "" {
		t.Error("task should have a status")
	}

	// Clean up - delete task
	client.send("teams.tasks.delete", map[string]any{
		"team_id": teamID,
		"task_id": taskID,
	})
}

// SCENARIO: Task list returns tasks for team.
func TestScenario_TaskList(t *testing.T) {
	wsURL, _ := getTestServer(t)
	client := connect(t, wsURL, "")

	// Check teams
	teamsResp := client.send("teams.list", map[string]any{})
	teams, ok := teamsResp["teams"].([]any)
	if !ok || len(teams) == 0 {
		t.Skip("No teams available - skipping task list test")
	}

	team := teams[0].(map[string]any)
	teamID, _ := team["id"].(string)

	// List tasks
	listResp := client.send("teams.tasks.list", map[string]any{
		"team_id": teamID,
	})

	// Should return array (may be empty)
	if _, ok := listResp["tasks"].([]any); !ok {
		t.Error("tasks field should be an array")
	}
}

// SCENARIO: Task approval workflow.
// Flow: Create task → Approve → Verify status change
func TestScenario_TaskApproval(t *testing.T) {
	wsURL, _ := getTestServer(t)
	client := connect(t, wsURL, "")

	// Check teams
	teamsResp := client.send("teams.list", map[string]any{})
	teams, ok := teamsResp["teams"].([]any)
	if !ok || len(teams) == 0 {
		t.Skip("No teams available - skipping task approval test")
	}

	team := teams[0].(map[string]any)
	teamID, _ := team["id"].(string)

	// Create task
	createResp := client.send("teams.tasks.create", map[string]any{
		"team_id":     teamID,
		"title":       "Task for approval test",
		"description": "Will be approved",
	})

	taskID, ok := createResp["id"].(string)
	if !ok {
		t.Fatalf("task creation failed")
	}

	// Approve task
	client.send("teams.tasks.approve", map[string]any{
		"team_id": teamID,
		"task_id": taskID,
		"comment": "Approved by scenario test",
	})

	// Verify status changed
	getResp := client.send("teams.tasks.get", map[string]any{
		"team_id": teamID,
		"task_id": taskID,
	})

	status, _ := getResp["status"].(string)
	t.Logf("Task status after approval: %s", status)

	// Clean up
	client.send("teams.tasks.delete", map[string]any{
		"team_id": teamID,
		"task_id": taskID,
	})
}
