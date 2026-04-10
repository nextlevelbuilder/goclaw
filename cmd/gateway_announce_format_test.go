package cmd

import (
	"strings"
	"testing"
	"time"

	"github.com/nextlevelbuilder/goclaw/internal/tools"
)

// TestBuildMergedAnnounceContent_SingleSuccess tests single completed task announcement.
func TestBuildMergedAnnounceContent_SingleSuccess(t *testing.T) {
	entries := []announceEntry{
		{
			MemberAgent:       "researcher",
			MemberDisplayName: "Nhà Nghiên Cứu",
			Content:           "Found 5 relevant papers",
		},
	}
	result := buildMergedAnnounceContent(entries, "", "")

	if !strings.Contains(result, "[System Message]") {
		t.Error("result missing [System Message]")
	}
	if !strings.Contains(result, "Nhà Nghiên Cứu (researcher)") {
		t.Error("result missing member display name with agent key")
	}
	if !strings.Contains(result, "completed task") {
		t.Error("result missing 'completed task' text")
	}
	if !strings.Contains(result, "Found 5 relevant papers") {
		t.Error("result missing task content")
	}
}

// TestBuildMergedAnnounceContent_SingleFailed tests single failed task announcement.
func TestBuildMergedAnnounceContent_SingleFailed(t *testing.T) {
	entries := []announceEntry{
		{
			MemberAgent:       "reviewer",
			MemberDisplayName: "",
			Content:           "[FAILED] Database connection timeout",
		},
	}
	result := buildMergedAnnounceContent(entries, "", "")

	if !strings.Contains(result, "[System Message]") {
		t.Error("result missing [System Message]")
	}
	if !strings.Contains(result, "failed to complete task") {
		t.Error("result missing 'failed to complete task' text")
	}
	if !strings.Contains(result, "Database connection timeout") {
		t.Error("result missing error message")
	}
	if !strings.Contains(result, "team_tasks(action=\"retry\"") {
		t.Error("result missing retry suggestion")
	}
}

// TestBuildMergedAnnounceContent_BatchMixed tests multiple tasks with mixed success/failure.
func TestBuildMergedAnnounceContent_BatchMixed(t *testing.T) {
	entries := []announceEntry{
		{
			MemberAgent:       "researcher",
			MemberDisplayName: "Researcher",
			Content:           "Analysis complete",
		},
		{
			MemberAgent:       "reviewer",
			MemberDisplayName: "Reviewer",
			Content:           "[FAILED] Permission denied",
		},
		{
			MemberAgent:       "writer",
			MemberDisplayName: "Writer",
			Content:           "Draft ready",
		},
	}
	result := buildMergedAnnounceContent(entries, "", "")

	if !strings.Contains(result, "2 task(s) completed, 1 task(s) failed") {
		t.Error("result missing batch summary with counts")
	}
	if !strings.Contains(result, "Analysis complete") {
		t.Error("result missing first success content")
	}
	if !strings.Contains(result, "Permission denied") {
		t.Error("result missing failure content")
	}
	if !strings.Contains(result, "Draft ready") {
		t.Error("result missing second success content")
	}
}

// TestBuildMergedAnnounceContent_WithSnapshot tests annotation with task board snapshot.
func TestBuildMergedAnnounceContent_WithSnapshot(t *testing.T) {
	entries := []announceEntry{
		{
			MemberAgent:       "agent1",
			MemberDisplayName: "Agent One",
			Content:           "Task done",
		},
	}
	snapshot := "Task Board:\n- Task 1: completed\n- Task 2: in progress"

	result := buildMergedAnnounceContent(entries, snapshot, "")

	if !strings.Contains(result, snapshot) {
		t.Error("result missing task board snapshot")
	}
	if !strings.Contains(result, "Some tasks are still in progress") {
		t.Error("result missing progress acknowledgement message")
	}
}

// TestBuildMergedAnnounceContent_AllDone tests when all tasks are completed.
func TestBuildMergedAnnounceContent_AllDone(t *testing.T) {
	entries := []announceEntry{
		{
			MemberAgent:       "agent1",
			MemberDisplayName: "Agent One",
			Content:           "Finished",
		},
	}
	snapshot := "Task Board:\nAll 3 tasks completed"

	result := buildMergedAnnounceContent(entries, snapshot, "")

	if !strings.Contains(result, "All tasks in this batch are completed") {
		t.Error("result missing 'All tasks completed' summary message")
	}
	if !strings.Contains(result, "comprehensive summary of ALL results") {
		t.Error("result missing summary instruction")
	}
}

// TestBuildMergedSubagentAnnounce_Single tests single subagent completion.
func TestBuildMergedSubagentAnnounce_Single(t *testing.T) {
	entries := []subagentAnnounceEntry{
		{
			Label:        "Search Wikipedia",
			Status:       "completed",
			Content:      "Found article on neural networks",
			Runtime:      2500 * time.Millisecond,
			Iterations:   1,
			InputTokens:  500,
			OutputTokens: 250,
		},
	}
	roster := tools.SubagentRoster{}

	result := buildMergedSubagentAnnounce(entries, roster)

	if !strings.Contains(result, "[System Message]") {
		t.Error("result missing [System Message]")
	}
	if !strings.Contains(result, "Search Wikipedia") {
		t.Error("result missing task label")
	}
	if !strings.Contains(result, "completed successfully") {
		t.Error("result missing 'completed successfully' status")
	}
	if !strings.Contains(result, "Found article on neural networks") {
		t.Error("result missing task content")
	}
	if !strings.Contains(result, "2.5s") {
		t.Error("result missing runtime")
	}
	if !strings.Contains(result, "tokens 500 in / 250 out") {
		t.Error("result missing token counts")
	}
}

// TestBuildMergedSubagentAnnounce_Batch tests multiple subagent results.
func TestBuildMergedSubagentAnnounce_Batch(t *testing.T) {
	entries := []subagentAnnounceEntry{
		{
			Label:        "Fetch Data",
			Status:       "completed",
			Content:      "Data retrieved successfully",
			Runtime:      1000 * time.Millisecond,
			Iterations:   1,
			InputTokens:  100,
			OutputTokens: 150,
		},
		{
			Label:        "Validate Schema",
			Status:       "completed",
			Content:      "Schema is valid",
			Runtime:      500 * time.Millisecond,
			Iterations:   2,
			InputTokens:  80,
			OutputTokens: 120,
		},
		{
			Label:        "Process Results",
			Status:       "failed",
			Content:      "Timeout error",
			Runtime:      3000 * time.Millisecond,
			Iterations:   1,
			InputTokens:  200,
			OutputTokens: 0,
		},
	}
	roster := tools.SubagentRoster{}

	result := buildMergedSubagentAnnounce(entries, roster)

	if !strings.Contains(result, "2 subagent task(s) completed, 1 failed") {
		t.Error("result missing batch summary with counts")
	}
	if !strings.Contains(result, "Task #1:") {
		t.Error("result missing task #1")
	}
	if !strings.Contains(result, "Task #2:") {
		t.Error("result missing task #2")
	}
	if !strings.Contains(result, "Task #3:") {
		t.Error("result missing task #3")
	}
	if !strings.Contains(result, "Fetch Data") {
		t.Error("result missing first task label")
	}
	if !strings.Contains(result, "Process Results") {
		t.Error("result missing failed task label")
	}
	if !strings.Contains(result, "failed") {
		t.Error("result missing failed status")
	}
}

// TestMemberLabel_WithDisplayName tests member label formatting with display name.
func TestMemberLabel_WithDisplayName(t *testing.T) {
	e := announceEntry{
		MemberAgent:       "agent_key",
		MemberDisplayName: "Agent Display",
	}
	result := memberLabel(e)
	if result != "Agent Display (agent_key)" {
		t.Errorf("memberLabel = %q, want %q", result, "Agent Display (agent_key)")
	}
}

// TestMemberLabel_WithoutDisplayName tests member label formatting without display name.
func TestMemberLabel_WithoutDisplayName(t *testing.T) {
	e := announceEntry{
		MemberAgent:       "agent_key",
		MemberDisplayName: "",
	}
	result := memberLabel(e)
	if result != "agent_key" {
		t.Errorf("memberLabel = %q, want %q", result, "agent_key")
	}
}

// TestBuildMergedAnnounceContent_WithWorkspace tests workspace annotation in message.
func TestBuildMergedAnnounceContent_WithWorkspace(t *testing.T) {
	entries := []announceEntry{
		{
			MemberAgent:       "agent",
			MemberDisplayName: "Agent",
			Content:           "Complete",
		},
	}
	workspace := "/shared/workspace"

	result := buildMergedAnnounceContent(entries, "", workspace)

	if !strings.Contains(result, "Team workspace") {
		t.Error("result missing 'Team workspace' annotation")
	}
	if !strings.Contains(result, workspace) {
		t.Error("result missing workspace path")
	}
}
