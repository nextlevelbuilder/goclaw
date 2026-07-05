package mcp

import (
	"context"
	"fmt"

	"github.com/google/uuid"

	mcpgo "github.com/mark3labs/mcp-go/mcp"
	mcpserver "github.com/mark3labs/mcp-go/server"

	"github.com/nextlevelbuilder/goclaw/internal/store"
)

// maxTaskCommentLength mirrors internal/gateway/methods/teams_tasks_mutations.go's
// maxCommentLength cap on comment/reason content, to prevent DB bloat.
const maxTaskCommentLength = 10000

// registerTeamsTasksCRUDTools registers the goclaw_teams_tasks_* MCP tools
// backed by store.TeamStore. agents resolves agent_key/UUID inputs for assign.
func registerTeamsTasksCRUDTools(srv *mcpserver.MCPServer, teams store.TeamStore, agents store.AgentStore) {
	srv.AddTool(mcpgo.NewTool("goclaw_teams_tasks_list",
		mcpgo.WithDescription("List a team's tasks, optionally filtered by status/channel/chatID."),
		mcpgo.WithString("team_id", mcpgo.Required(), mcpgo.Description("Team UUID.")),
		mcpgo.WithString("status", mcpgo.Description("Status filter: \"\" (active), \"completed\", or \"all\".")),
		mcpgo.WithString("channel", mcpgo.Description("Scope filter: channel name.")),
		mcpgo.WithString("chat_id", mcpgo.Description("Scope filter: chat ID.")),
		mcpgo.WithReadOnlyHintAnnotation(true),
	), handleTeamsTasksList(teams))

	srv.AddTool(mcpgo.NewTool("goclaw_teams_tasks_active_by_session",
		mcpgo.WithDescription("List active tasks scoped to a session/chat ID (for sidebar-style views)."),
		mcpgo.WithString("session_key", mcpgo.Required(), mcpgo.Description("Session/chat key.")),
		mcpgo.WithReadOnlyHintAnnotation(true),
	), handleTeamsTasksActiveBySession(teams))

	srv.AddTool(mcpgo.NewTool("goclaw_teams_tasks_events",
		mcpgo.WithDescription("List audit events for a single task."),
		mcpgo.WithString("team_id", mcpgo.Required(), mcpgo.Description("Team UUID.")),
		mcpgo.WithString("task_id", mcpgo.Required(), mcpgo.Description("Task UUID.")),
		mcpgo.WithReadOnlyHintAnnotation(true),
	), handleTeamsTasksEvents(teams))

	srv.AddTool(mcpgo.NewTool("goclaw_teams_tasks_create",
		mcpgo.WithDescription("Create a new task in a team's shared task list."),
		mcpgo.WithString("team_id", mcpgo.Required(), mcpgo.Description("Team UUID.")),
		mcpgo.WithString("subject", mcpgo.Required(), mcpgo.Description("Task subject (max 500 chars).")),
		mcpgo.WithString("description", mcpgo.Description("Task description.")),
		mcpgo.WithNumber("priority", mcpgo.Description("Task priority.")),
		mcpgo.WithString("task_type", mcpgo.Description("Task type; defaults to \"general\".")),
		mcpgo.WithString("assign_to", mcpgo.Description("Agent UUID to assign immediately after creation.")),
		mcpgo.WithString("channel", mcpgo.Description("Origin channel; defaults to \"dashboard\".")),
		mcpgo.WithString("chat_id", mcpgo.Description("Origin chat ID; defaults to the team ID.")),
	), handleTeamsTasksCreate(teams))

	srv.AddTool(mcpgo.NewTool("goclaw_teams_tasks_delete",
		mcpgo.WithDescription("Hard-delete a task in a terminal status."),
		mcpgo.WithString("team_id", mcpgo.Required(), mcpgo.Description("Team UUID.")),
		mcpgo.WithString("task_id", mcpgo.Required(), mcpgo.Description("Task UUID.")),
		mcpgo.WithDestructiveHintAnnotation(true),
	), handleTeamsTasksDelete(teams))

	srv.AddTool(mcpgo.NewTool("goclaw_teams_tasks_delete_bulk",
		mcpgo.WithDescription("Hard-delete multiple tasks in a terminal status."),
		mcpgo.WithString("team_id", mcpgo.Required(), mcpgo.Description("Team UUID.")),
		mcpgo.WithArray("task_ids", mcpgo.Required(), mcpgo.Description("Task UUIDs to delete.")),
		mcpgo.WithDestructiveHintAnnotation(true),
	), handleTeamsTasksDeleteBulk(teams))

	srv.AddTool(mcpgo.NewTool("goclaw_teams_tasks_assign",
		mcpgo.WithDescription("Assign a task to a team member (does not dispatch to the agent runtime — MCP surface only)."),
		mcpgo.WithString("team_id", mcpgo.Required(), mcpgo.Description("Team UUID.")),
		mcpgo.WithString("task_id", mcpgo.Required(), mcpgo.Description("Task UUID.")),
		mcpgo.WithString("agent_id", mcpgo.Required(), mcpgo.Description("Assignee agent key or UUID.")),
	), handleTeamsTasksAssign(teams, agents))

	srv.AddTool(mcpgo.NewTool("goclaw_teams_tasks_get",
		mcpgo.WithDescription("Fetch a task with its comments, events, and attachments."),
		mcpgo.WithString("team_id", mcpgo.Required(), mcpgo.Description("Team UUID.")),
		mcpgo.WithString("task_id", mcpgo.Required(), mcpgo.Description("Task UUID.")),
		mcpgo.WithReadOnlyHintAnnotation(true),
	), handleTeamsTasksGet(teams))

	srv.AddTool(mcpgo.NewTool("goclaw_teams_tasks_get_light",
		mcpgo.WithDescription("Fetch a task only (no comments/events/attachments)."),
		mcpgo.WithString("team_id", mcpgo.Required(), mcpgo.Description("Team UUID.")),
		mcpgo.WithString("task_id", mcpgo.Required(), mcpgo.Description("Task UUID.")),
		mcpgo.WithReadOnlyHintAnnotation(true),
	), handleTeamsTasksGetLight(teams))

	srv.AddTool(mcpgo.NewTool("goclaw_teams_tasks_approve",
		mcpgo.WithDescription("Approve a task in review, optionally with a comment."),
		mcpgo.WithString("team_id", mcpgo.Required(), mcpgo.Description("Team UUID.")),
		mcpgo.WithString("task_id", mcpgo.Required(), mcpgo.Description("Task UUID.")),
		mcpgo.WithString("comment", mcpgo.Description("Optional approval comment.")),
	), handleTeamsTasksApprove(teams))

	srv.AddTool(mcpgo.NewTool("goclaw_teams_tasks_reject",
		mcpgo.WithDescription("Reject a task in review, with a reason."),
		mcpgo.WithString("team_id", mcpgo.Required(), mcpgo.Description("Team UUID.")),
		mcpgo.WithString("task_id", mcpgo.Required(), mcpgo.Description("Task UUID.")),
		mcpgo.WithString("reason", mcpgo.Description("Rejection reason; defaults to \"Rejected by human\".")),
	), handleTeamsTasksReject(teams))

	srv.AddTool(mcpgo.NewTool("goclaw_teams_tasks_comment",
		mcpgo.WithDescription("Add a comment to a task."),
		mcpgo.WithString("team_id", mcpgo.Required(), mcpgo.Description("Team UUID.")),
		mcpgo.WithString("task_id", mcpgo.Required(), mcpgo.Description("Task UUID.")),
		mcpgo.WithString("content", mcpgo.Required(), mcpgo.Description("Comment content.")),
		mcpgo.WithString("user_id", mcpgo.Description("Author user ID.")),
	), handleTeamsTasksComment(teams))

	srv.AddTool(mcpgo.NewTool("goclaw_teams_tasks_comments",
		mcpgo.WithDescription("List comments on a task."),
		mcpgo.WithString("team_id", mcpgo.Required(), mcpgo.Description("Team UUID.")),
		mcpgo.WithString("task_id", mcpgo.Required(), mcpgo.Description("Task UUID.")),
		mcpgo.WithReadOnlyHintAnnotation(true),
	), handleTeamsTasksComments(teams))
}

func parseTeamTaskIDs(req mcpgo.CallToolRequest) (teamID, taskID uuid.UUID, err error) {
	teamID, err = parseTeamID(req)
	if err != nil {
		return uuid.Nil, uuid.Nil, err
	}
	taskIDStr, err := req.RequireString("task_id")
	if err != nil {
		return uuid.Nil, uuid.Nil, err
	}
	taskID, err = uuid.Parse(taskIDStr)
	if err != nil {
		return uuid.Nil, uuid.Nil, fmt.Errorf("invalid task_id: %w", err)
	}
	return teamID, taskID, nil
}

// getTaskInTeam fetches a task and verifies it belongs to teamID, preventing
// cross-team IDOR — mirrors the belongs-to-team check in
// internal/gateway/methods/teams_tasks.go.
func getTaskInTeam(ctx context.Context, teams store.TeamStore, teamID, taskID uuid.UUID) (*store.TeamTaskData, error) {
	task, err := teams.GetTask(ctx, taskID)
	if err != nil {
		return nil, err
	}
	if task.TeamID != teamID {
		return nil, fmt.Errorf("task not found in team")
	}
	return task, nil
}

func handleTeamsTasksList(teams store.TeamStore) mcpserver.ToolHandlerFunc {
	return func(ctx context.Context, req mcpgo.CallToolRequest) (*mcpgo.CallToolResult, error) {
		teamID, err := parseTeamID(req)
		if err != nil {
			return toolError("teams.tasks.list", err)
		}
		const dashboardLimit = 200
		status := req.GetString("status", "")
		channel := req.GetString("channel", "")
		chatID := req.GetString("chat_id", "")
		tasks, err := teams.ListTasks(ctx, teamID, "newest", status, "", channel, chatID, dashboardLimit, 0)
		if err != nil {
			return toolError("teams.tasks.list", err)
		}
		if len(tasks) > dashboardLimit {
			tasks = tasks[:dashboardLimit]
		}
		return jsonToolResult(map[string]any{"tasks": tasks, "count": len(tasks)})
	}
}

func handleTeamsTasksActiveBySession(teams store.TeamStore) mcpserver.ToolHandlerFunc {
	return func(ctx context.Context, req mcpgo.CallToolRequest) (*mcpgo.CallToolResult, error) {
		sessionKey, err := req.RequireString("session_key")
		if err != nil {
			return toolError("teams.tasks.active-by-session", err)
		}
		tasks, err := teams.ListActiveTasksByChatID(ctx, sessionKey)
		if err != nil {
			return toolError("teams.tasks.active-by-session", err)
		}
		return jsonToolResult(map[string]any{"tasks": tasks})
	}
}

func handleTeamsTasksEvents(teams store.TeamStore) mcpserver.ToolHandlerFunc {
	return func(ctx context.Context, req mcpgo.CallToolRequest) (*mcpgo.CallToolResult, error) {
		teamID, taskID, err := parseTeamTaskIDs(req)
		if err != nil {
			return toolError("teams.tasks.events", err)
		}
		if _, err := getTaskInTeam(ctx, teams, teamID, taskID); err != nil {
			return toolError("teams.tasks.events", err)
		}
		events, err := teams.ListTaskEvents(ctx, taskID)
		if err != nil {
			return toolError("teams.tasks.events", err)
		}
		return jsonToolResult(map[string]any{"events": events})
	}
}

func handleTeamsTasksCreate(teams store.TeamStore) mcpserver.ToolHandlerFunc {
	return func(ctx context.Context, req mcpgo.CallToolRequest) (*mcpgo.CallToolResult, error) {
		teamID, err := parseTeamID(req)
		if err != nil {
			return toolError("teams.tasks.create", err)
		}
		subject, err := req.RequireString("subject")
		if err != nil {
			return toolError("teams.tasks.create", err)
		}
		const maxSubjectLen = 500
		if len(subject) > maxSubjectLen {
			return mcpgo.NewToolResultError("teams.tasks.create: subject too long"), nil
		}
		description := req.GetString("description", "")
		if len(description) > maxTaskCommentLength {
			return mcpgo.NewToolResultError("teams.tasks.create: description too long"), nil
		}
		taskType := req.GetString("task_type", "general")
		channel := req.GetString("channel", "dashboard")
		chatID := req.GetString("chat_id", teamID.String())

		task := &store.TeamTaskData{
			TeamID:      teamID,
			Subject:     subject,
			Description: description,
			Status:      store.TeamTaskStatusPending,
			Priority:    int(req.GetFloat("priority", 0)),
			TaskType:    taskType,
			Channel:     channel,
			ChatID:      chatID,
		}
		if err := teams.CreateTask(ctx, task); err != nil {
			return toolError("teams.tasks.create", err)
		}

		if assignTo := req.GetString("assign_to", ""); assignTo != "" {
			if agentID, err := uuid.Parse(assignTo); err == nil {
				if err := teams.AssignTask(ctx, task.ID, agentID, teamID); err == nil {
					task.Status = store.TeamTaskStatusInProgress
					task.OwnerAgentID = &agentID
				}
			}
		}
		return jsonToolResult(map[string]any{"task": task})
	}
}

func handleTeamsTasksDelete(teams store.TeamStore) mcpserver.ToolHandlerFunc {
	return func(ctx context.Context, req mcpgo.CallToolRequest) (*mcpgo.CallToolResult, error) {
		teamID, taskID, err := parseTeamTaskIDs(req)
		if err != nil {
			return toolError("teams.tasks.delete", err)
		}
		if _, err := getTaskInTeam(ctx, teams, teamID, taskID); err != nil {
			return toolError("teams.tasks.delete", err)
		}
		if err := teams.DeleteTask(ctx, taskID, teamID); err != nil {
			return toolError("teams.tasks.delete", err)
		}
		return jsonToolResult(map[string]bool{"ok": true})
	}
}

func handleTeamsTasksDeleteBulk(teams store.TeamStore) mcpserver.ToolHandlerFunc {
	return func(ctx context.Context, req mcpgo.CallToolRequest) (*mcpgo.CallToolResult, error) {
		teamID, err := parseTeamID(req)
		if err != nil {
			return toolError("teams.tasks.delete-bulk", err)
		}
		raw, err := req.RequireStringSlice("task_ids")
		if err != nil {
			return toolError("teams.tasks.delete-bulk", err)
		}
		taskUUIDs := make([]uuid.UUID, 0, len(raw))
		for _, s := range raw {
			if id, err := uuid.Parse(s); err == nil {
				taskUUIDs = append(taskUUIDs, id)
			}
		}
		if len(taskUUIDs) == 0 {
			return mcpgo.NewToolResultError("teams.tasks.delete-bulk: no valid task_ids"), nil
		}
		deleted, err := teams.DeleteTasks(ctx, taskUUIDs, teamID)
		if err != nil {
			return toolError("teams.tasks.delete-bulk", err)
		}
		return jsonToolResult(map[string]any{"deleted": len(deleted)})
	}
}

func handleTeamsTasksAssign(teams store.TeamStore, agents store.AgentStore) mcpserver.ToolHandlerFunc {
	return func(ctx context.Context, req mcpgo.CallToolRequest) (*mcpgo.CallToolResult, error) {
		teamID, taskID, err := parseTeamTaskIDs(req)
		if err != nil {
			return toolError("teams.tasks.assign", err)
		}
		agentRef, err := req.RequireString("agent_id")
		if err != nil {
			return toolError("teams.tasks.assign", err)
		}
		agentID, err := resolveAgentUUID(ctx, agents, agentRef)
		if err != nil {
			return toolError("teams.tasks.assign", fmt.Errorf("invalid agent_id: %w", err))
		}
		if _, err := getTaskInTeam(ctx, teams, teamID, taskID); err != nil {
			return toolError("teams.tasks.assign", err)
		}
		if err := teams.AssignTask(ctx, taskID, agentID, teamID); err != nil {
			return toolError("teams.tasks.assign", err)
		}
		return jsonToolResult(map[string]bool{"ok": true})
	}
}

func handleTeamsTasksGet(teams store.TeamStore) mcpserver.ToolHandlerFunc {
	return func(ctx context.Context, req mcpgo.CallToolRequest) (*mcpgo.CallToolResult, error) {
		teamID, taskID, err := parseTeamTaskIDs(req)
		if err != nil {
			return toolError("teams.tasks.get", err)
		}
		task, err := getTaskInTeam(ctx, teams, teamID, taskID)
		if err != nil {
			return toolError("teams.tasks.get", err)
		}
		comments, _ := teams.ListTaskComments(ctx, taskID)
		events, _ := teams.ListTaskEvents(ctx, taskID)
		attachments, _ := teams.ListTaskAttachments(ctx, taskID)
		return jsonToolResult(map[string]any{
			"task": task, "comments": comments, "events": events, "attachments": attachments,
		})
	}
}

func handleTeamsTasksGetLight(teams store.TeamStore) mcpserver.ToolHandlerFunc {
	return func(ctx context.Context, req mcpgo.CallToolRequest) (*mcpgo.CallToolResult, error) {
		teamID, taskID, err := parseTeamTaskIDs(req)
		if err != nil {
			return toolError("teams.tasks.get-light", err)
		}
		task, err := getTaskInTeam(ctx, teams, teamID, taskID)
		if err != nil {
			return toolError("teams.tasks.get-light", err)
		}
		return jsonToolResult(map[string]any{"task": task})
	}
}

func handleTeamsTasksApprove(teams store.TeamStore) mcpserver.ToolHandlerFunc {
	return func(ctx context.Context, req mcpgo.CallToolRequest) (*mcpgo.CallToolResult, error) {
		teamID, taskID, err := parseTeamTaskIDs(req)
		if err != nil {
			return toolError("teams.tasks.approve", err)
		}
		comment := req.GetString("comment", "")
		if len(comment) > maxTaskCommentLength {
			return mcpgo.NewToolResultError("teams.tasks.approve: comment too long"), nil
		}
		if err := teams.ApproveTask(ctx, taskID, teamID, comment); err != nil {
			return toolError("teams.tasks.approve", err)
		}
		if comment != "" {
			_ = teams.AddTaskComment(ctx, &store.TeamTaskCommentData{TaskID: taskID, Content: comment})
		}
		return jsonToolResult(map[string]bool{"ok": true})
	}
}

func handleTeamsTasksReject(teams store.TeamStore) mcpserver.ToolHandlerFunc {
	return func(ctx context.Context, req mcpgo.CallToolRequest) (*mcpgo.CallToolResult, error) {
		teamID, taskID, err := parseTeamTaskIDs(req)
		if err != nil {
			return toolError("teams.tasks.reject", err)
		}
		reason := req.GetString("reason", "Rejected by human")
		if len(reason) > maxTaskCommentLength {
			return mcpgo.NewToolResultError("teams.tasks.reject: reason too long"), nil
		}
		if err := teams.RejectTask(ctx, taskID, teamID, reason); err != nil {
			return toolError("teams.tasks.reject", err)
		}
		return jsonToolResult(map[string]bool{"ok": true})
	}
}

func handleTeamsTasksComment(teams store.TeamStore) mcpserver.ToolHandlerFunc {
	return func(ctx context.Context, req mcpgo.CallToolRequest) (*mcpgo.CallToolResult, error) {
		teamID, taskID, err := parseTeamTaskIDs(req)
		if err != nil {
			return toolError("teams.tasks.comment", err)
		}
		content, err := req.RequireString("content")
		if err != nil {
			return toolError("teams.tasks.comment", err)
		}
		if len(content) > maxTaskCommentLength {
			return mcpgo.NewToolResultError("teams.tasks.comment: comment too long"), nil
		}
		if _, err := getTaskInTeam(ctx, teams, teamID, taskID); err != nil {
			return toolError("teams.tasks.comment", err)
		}
		if err := teams.AddTaskComment(ctx, &store.TeamTaskCommentData{
			TaskID:  taskID,
			UserID:  req.GetString("user_id", ""),
			Content: content,
		}); err != nil {
			return toolError("teams.tasks.comment", err)
		}
		return jsonToolResult(map[string]bool{"ok": true})
	}
}

func handleTeamsTasksComments(teams store.TeamStore) mcpserver.ToolHandlerFunc {
	return func(ctx context.Context, req mcpgo.CallToolRequest) (*mcpgo.CallToolResult, error) {
		teamID, taskID, err := parseTeamTaskIDs(req)
		if err != nil {
			return toolError("teams.tasks.comments", err)
		}
		if _, err := getTaskInTeam(ctx, teams, teamID, taskID); err != nil {
			return toolError("teams.tasks.comments", err)
		}
		comments, err := teams.ListTaskComments(ctx, taskID)
		if err != nil {
			return toolError("teams.tasks.comments", err)
		}
		return jsonToolResult(map[string]any{"comments": comments})
	}
}
