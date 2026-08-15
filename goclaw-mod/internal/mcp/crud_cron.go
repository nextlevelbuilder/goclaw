package mcp

import (
	"context"

	mcpgo "github.com/mark3labs/mcp-go/mcp"
	mcpserver "github.com/mark3labs/mcp-go/server"

	"github.com/nextlevelbuilder/goclaw/internal/store"
)

// registerCronCRUDTools registers the goclaw_cron_* MCP tools backed by store.CronStore.
func registerCronCRUDTools(srv *mcpserver.MCPServer, cron store.CronStore) {
	srv.AddTool(mcpgo.NewTool("goclaw_cron_list",
		mcpgo.WithDescription("List scheduled cron jobs."),
		mcpgo.WithBoolean("include_disabled", mcpgo.Description("Include disabled jobs (default false).")),
		mcpgo.WithString("agent_id", mcpgo.Description("Filter by agent ID.")),
		mcpgo.WithString("user_id", mcpgo.Description("Filter by user ID.")),
		mcpgo.WithReadOnlyHintAnnotation(true),
	), handleCronList(cron))

	// goclaw_cron_get is a goclaw-specific extra (not present in the
	// reference tool set, which only exposes cron.status for scheduler-wide
	// state) — kept for convenience since store.CronStore.GetJob supports it
	// directly and it's useful for single-job lookups.
	srv.AddTool(mcpgo.NewTool("goclaw_cron_get",
		mcpgo.WithDescription("Get a single cron job by ID."),
		mcpgo.WithString("job_id", mcpgo.Required(), mcpgo.Description("Cron job ID.")),
		mcpgo.WithReadOnlyHintAnnotation(true),
	), handleCronGet(cron))

	srv.AddTool(mcpgo.NewTool("goclaw_cron_create",
		mcpgo.WithDescription("Create a message-delivery cron job (schedule kind \"at\", \"every\", or \"cron\")."),
		mcpgo.WithString("name", mcpgo.Required(), mcpgo.Description("Job name.")),
		mcpgo.WithString("schedule_kind", mcpgo.Required(), mcpgo.Enum("at", "every", "cron"), mcpgo.Description("Schedule kind.")),
		mcpgo.WithNumber("at_ms", mcpgo.Description("Unix ms timestamp; required when schedule_kind is \"at\".")),
		mcpgo.WithNumber("every_ms", mcpgo.Description("Interval in ms; required when schedule_kind is \"every\".")),
		mcpgo.WithString("expr", mcpgo.Description("Cron expression; required when schedule_kind is \"cron\".")),
		mcpgo.WithString("tz", mcpgo.Description("IANA timezone for the schedule.")),
		mcpgo.WithString("message", mcpgo.Required(), mcpgo.Description("Message text delivered when the job fires.")),
		mcpgo.WithBoolean("deliver", mcpgo.Description("Whether to deliver the message to a channel (default false).")),
		mcpgo.WithString("channel", mcpgo.Description("Delivery channel, when deliver is true.")),
		mcpgo.WithString("to", mcpgo.Description("Delivery recipient/chat ID, when deliver is true.")),
		mcpgo.WithString("agent_id", mcpgo.Description("Owning agent ID.")),
		mcpgo.WithString("user_id", mcpgo.Description("Owning user ID.")),
	), handleCronCreate(cron))

	srv.AddTool(mcpgo.NewTool("goclaw_cron_update",
		mcpgo.WithDescription("Apply a partial update to an existing cron job."),
		mcpgo.WithString("job_id", mcpgo.Required(), mcpgo.Description("Cron job ID.")),
		mcpgo.WithString("name", mcpgo.Description("New job name.")),
		mcpgo.WithBoolean("enabled", mcpgo.Description("New enabled state.")),
		mcpgo.WithString("message", mcpgo.Description("New message text.")),
		mcpgo.WithString("schedule_kind", mcpgo.Enum("at", "every", "cron"), mcpgo.Description("New schedule kind (requires the matching at_ms/every_ms/expr field).")),
		mcpgo.WithNumber("at_ms", mcpgo.Description("New unix ms timestamp for schedule_kind \"at\".")),
		mcpgo.WithNumber("every_ms", mcpgo.Description("New interval in ms for schedule_kind \"every\".")),
		mcpgo.WithString("expr", mcpgo.Description("New cron expression for schedule_kind \"cron\".")),
		mcpgo.WithString("tz", mcpgo.Description("New IANA timezone.")),
		mcpgo.WithBoolean("deliver", mcpgo.Description("New deliver flag.")),
		mcpgo.WithString("deliver_channel", mcpgo.Description("New delivery channel.")),
		mcpgo.WithString("deliver_to", mcpgo.Description("New delivery recipient/chat ID.")),
	), handleCronUpdate(cron))

	srv.AddTool(mcpgo.NewTool("goclaw_cron_delete",
		mcpgo.WithDescription("Delete a cron job by ID."),
		mcpgo.WithString("job_id", mcpgo.Required(), mcpgo.Description("Cron job ID.")),
		mcpgo.WithDestructiveHintAnnotation(true),
	), handleCronDelete(cron))

	srv.AddTool(mcpgo.NewTool("goclaw_cron_toggle",
		mcpgo.WithDescription("Enable or disable a cron job."),
		mcpgo.WithString("job_id", mcpgo.Required(), mcpgo.Description("Cron job ID.")),
		mcpgo.WithBoolean("enabled", mcpgo.Required(), mcpgo.Description("Desired enabled state.")),
	), handleCronToggle(cron))

	srv.AddTool(mcpgo.NewTool("goclaw_cron_run",
		mcpgo.WithDescription("Trigger an immediate (background) run of a cron job."),
		mcpgo.WithString("job_id", mcpgo.Required(), mcpgo.Description("Cron job ID.")),
		mcpgo.WithString("mode", mcpgo.Enum("force", "due"), mcpgo.Description("\"force\" runs regardless of schedule; \"due\" (default) only runs if due.")),
	), handleCronRun(cron))

	srv.AddTool(mcpgo.NewTool("goclaw_cron_runs",
		mcpgo.WithDescription("Return the run log entries for a cron job."),
		mcpgo.WithString("job_id", mcpgo.Description("Cron job ID; empty returns entries across all jobs, if supported.")),
		mcpgo.WithNumber("limit", mcpgo.Description("Maximum entries to return.")),
		mcpgo.WithNumber("offset", mcpgo.Description("Pagination offset.")),
		mcpgo.WithReadOnlyHintAnnotation(true),
	), handleCronRuns(cron))

	srv.AddTool(mcpgo.NewTool("goclaw_cron_status",
		mcpgo.WithDescription("Return the cron scheduler's overall status."),
		mcpgo.WithReadOnlyHintAnnotation(true),
	), handleCronStatus(cron))
}

func handleCronList(cron store.CronStore) mcpserver.ToolHandlerFunc {
	return func(ctx context.Context, req mcpgo.CallToolRequest) (*mcpgo.CallToolResult, error) {
		includeDisabled := req.GetBool("include_disabled", false)
		agentID := req.GetString("agent_id", "")
		userID := req.GetString("user_id", "")
		list := cron.ListJobs(ctx, includeDisabled, agentID, userID)
		return jsonToolResult(list)
	}
}

func handleCronGet(cron store.CronStore) mcpserver.ToolHandlerFunc {
	return func(ctx context.Context, req mcpgo.CallToolRequest) (*mcpgo.CallToolResult, error) {
		jobID, err := req.RequireString("job_id")
		if err != nil {
			return toolError("cron.get", err)
		}
		job, ok := cron.GetJob(ctx, jobID)
		if !ok {
			return mcpgo.NewToolResultError("cron.get: job not found: " + jobID), nil
		}
		return jsonToolResult(job)
	}
}

func handleCronCreate(cron store.CronStore) mcpserver.ToolHandlerFunc {
	return func(ctx context.Context, req mcpgo.CallToolRequest) (*mcpgo.CallToolResult, error) {
		name, err := req.RequireString("name")
		if err != nil {
			return toolError("cron.create", err)
		}
		kind, err := req.RequireString("schedule_kind")
		if err != nil {
			return toolError("cron.create", err)
		}
		message, err := req.RequireString("message")
		if err != nil {
			return toolError("cron.create", err)
		}

		schedule := store.CronSchedule{Kind: kind, TZ: req.GetString("tz", "")}
		switch kind {
		case "at":
			ms := int64(req.GetFloat("at_ms", 0))
			schedule.AtMS = &ms
		case "every":
			ms := int64(req.GetFloat("every_ms", 0))
			schedule.EveryMS = &ms
		case "cron":
			schedule.Expr = req.GetString("expr", "")
		}

		deliver := req.GetBool("deliver", false)
		channel := req.GetString("channel", "")
		to := req.GetString("to", "")
		agentID := req.GetString("agent_id", "")
		userID := req.GetString("user_id", "")

		job, err := cron.AddJob(ctx, name, schedule, message, deliver, channel, to, agentID, userID)
		if err != nil {
			return toolError("cron.create", err)
		}
		return jsonToolResult(job)
	}
}

func handleCronUpdate(cron store.CronStore) mcpserver.ToolHandlerFunc {
	return func(ctx context.Context, req mcpgo.CallToolRequest) (*mcpgo.CallToolResult, error) {
		jobID, err := req.RequireString("job_id")
		if err != nil {
			return toolError("cron.update", err)
		}

		args := req.GetArguments()
		patch := store.CronJobPatch{Name: req.GetString("name", "")}
		if v, ok := args["enabled"]; ok {
			if b, ok := v.(bool); ok {
				patch.Enabled = &b
			}
		}
		if msg := req.GetString("message", ""); msg != "" {
			patch.Message = msg
		}
		if kind := req.GetString("schedule_kind", ""); kind != "" {
			schedule := store.CronSchedule{Kind: kind, TZ: req.GetString("tz", "")}
			switch kind {
			case "at":
				ms := int64(req.GetFloat("at_ms", 0))
				schedule.AtMS = &ms
			case "every":
				ms := int64(req.GetFloat("every_ms", 0))
				schedule.EveryMS = &ms
			case "cron":
				schedule.Expr = req.GetString("expr", "")
			}
			patch.Schedule = &schedule
		}
		if v, ok := args["deliver"]; ok {
			if b, ok := v.(bool); ok {
				patch.Deliver = &b
			}
		}
		if ch := req.GetString("deliver_channel", ""); ch != "" {
			patch.DeliverChannel = &ch
		}
		if to := req.GetString("deliver_to", ""); to != "" {
			patch.DeliverTo = &to
		}

		job, err := cron.UpdateJob(ctx, jobID, patch)
		if err != nil {
			return toolError("cron.update", err)
		}
		return jsonToolResult(map[string]any{"job": job})
	}
}

func handleCronDelete(cron store.CronStore) mcpserver.ToolHandlerFunc {
	return func(ctx context.Context, req mcpgo.CallToolRequest) (*mcpgo.CallToolResult, error) {
		jobID, err := req.RequireString("job_id")
		if err != nil {
			return toolError("cron.delete", err)
		}
		if err := cron.RemoveJob(ctx, jobID); err != nil {
			return toolError("cron.delete", err)
		}
		return jsonToolResult(map[string]bool{"deleted": true})
	}
}

func handleCronToggle(cron store.CronStore) mcpserver.ToolHandlerFunc {
	return func(ctx context.Context, req mcpgo.CallToolRequest) (*mcpgo.CallToolResult, error) {
		jobID, err := req.RequireString("job_id")
		if err != nil {
			return toolError("cron.toggle", err)
		}
		args := req.GetArguments()
		enabled, _ := args["enabled"].(bool)
		if err := cron.EnableJob(ctx, jobID, enabled); err != nil {
			return toolError("cron.toggle", err)
		}
		return jsonToolResult(map[string]any{"jobId": jobID, "enabled": enabled})
	}
}

func handleCronRun(cron store.CronStore) mcpserver.ToolHandlerFunc {
	return func(ctx context.Context, req mcpgo.CallToolRequest) (*mcpgo.CallToolResult, error) {
		jobID, err := req.RequireString("job_id")
		if err != nil {
			return toolError("cron.run", err)
		}
		force := req.GetString("mode", "due") == "force"
		ran, _, err := cron.RunJob(ctx, jobID, force)
		if err != nil {
			return toolError("cron.run", err)
		}
		return jsonToolResult(map[string]bool{"ok": true, "ran": ran})
	}
}

func handleCronRuns(cron store.CronStore) mcpserver.ToolHandlerFunc {
	return func(ctx context.Context, req mcpgo.CallToolRequest) (*mcpgo.CallToolResult, error) {
		jobID := req.GetString("job_id", "")
		limit := int(req.GetFloat("limit", 0))
		offset := int(req.GetFloat("offset", 0))
		entries, total := cron.GetRunLog(ctx, jobID, limit, offset)
		return jsonToolResult(map[string]any{"entries": entries, "total": total})
	}
}

func handleCronStatus(cron store.CronStore) mcpserver.ToolHandlerFunc {
	return func(_ context.Context, _ mcpgo.CallToolRequest) (*mcpgo.CallToolResult, error) {
		return jsonToolResult(map[string]any{"status": cron.Status()})
	}
}
