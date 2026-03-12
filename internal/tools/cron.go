package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/nextlevelbuilder/goclaw/internal/store"
)

// CronTool lets agents manage Gateway cron jobs.
// Matching OpenClaw src/agents/tools/cron-tool.ts.
type CronTool struct {
	cronStore        store.CronStore
	groupWriterCache *store.GroupWriterCache // nil = no group restriction
}

func NewCronTool(cronStore store.CronStore) *CronTool {
	return &CronTool{cronStore: cronStore}
}

// SetGroupWriterCache enables group cron mutation restriction.
func (t *CronTool) SetGroupWriterCache(c *store.GroupWriterCache) {
	t.groupWriterCache = c
}

func (t *CronTool) Name() string { return "cron" }

func (t *CronTool) Description() string {
	return `Manage Gateway cron jobs (status/list/add/update/remove/run/runs).

ACTIONS:
- status: Check cron scheduler status
- list: List jobs (use includeDisabled:true to include disabled)
- add: Create job (requires job object, see schema below)
- update: Modify job (requires jobId + patch object)
- remove: Delete job (requires jobId)
- run: Trigger job immediately (requires jobId)
- runs: Get job run history (requires jobId)

JOB SCHEMA (for add action):
{
  "name": "string (required, lowercase slug)",
  "schedule": { ... },      // Required: when to run
  "message": "string",      // Required: what message to send to the agent
  "deliver": true|false,    // Optional: deliver result to channel (default false)
  "channel": "channel-name", // Optional: target channel for delivery (auto-filled from context)
  "to": "chat-id",          // Optional: target chat/recipient ID
  "agentId": "agent-uuid",  // Optional: which agent handles the job (default: current)
  "deleteAfterRun": true    // Optional: auto-delete after execution (default true for "at" schedule)
}

SCHEDULE TYPES (schedule.kind):
- "at": One-shot at absolute time
  { "kind": "at", "atMs": <unix-milliseconds> }
- "every": Recurring interval
  { "kind": "every", "everyMs": <interval-ms> }
- "cron": Cron expression
  { "kind": "cron", "expr": "<5-field cron expression>", "tz": "<optional IANA timezone, e.g. Asia/Ho_Chi_Minh; omit to use gateway default>" }

CRITICAL CONSTRAINTS:
- name must be a valid slug (lowercase letters, numbers, hyphens only)
- message is required for add action
- schedule is required for add action
- Default: jobs run as isolated agent turns with the specified message

Use jobId as the canonical identifier; id is accepted for compatibility.`
}

func (t *CronTool) Parameters() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"action": map[string]any{
				"type":        "string",
				"description": "The cron action to perform",
				"enum":        []string{"status", "list", "add", "update", "remove", "run", "runs"},
			},
			"includeDisabled": map[string]any{
				"type":        "boolean",
				"description": "Include disabled jobs in list (default false)",
			},
			"job": map[string]any{
				"type":                 "object",
				"description":          "Job definition for add action (name, schedule, message, deliver, channel, to, agentId, deleteAfterRun)",
				"additionalProperties": true,
			},
			"jobId": map[string]any{
				"type":        "string",
				"description": "Job ID for update/remove/run/runs actions",
			},
			"id": map[string]any{
				"type":        "string",
				"description": "Backward compatibility alias for jobId",
			},
			"patch": map[string]any{
				"type":                 "object",
				"description":          "Patch object for update action",
				"additionalProperties": true,
			},
			"runMode": map[string]any{
				"type":        "string",
				"description": "Run mode: 'due' (only if due) or 'force' (immediate)",
				"enum":        []string{"due", "force"},
			},
		},
		"required": []string{"action"},
	}
}

func (t *CronTool) Execute(ctx context.Context, args map[string]any) *Result {
	action, _ := args["action"].(string)
	if action == "" {
		return ErrorResult("action parameter is required")
	}

	// Group write permission check for mutation actions
	if t.groupWriterCache != nil && (action == "add" || action == "update" || action == "remove") {
		if err := store.CheckGroupWritePermission(ctx, t.groupWriterCache); err != nil {
			return ErrorResult("permission denied: only file writers can manage cron jobs in group chats")
		}
	}

	agentID := resolveAgentIDString(ctx)
	userID := store.UserIDFromContext(ctx)

	switch action {
	case "status":
		return t.handleStatus()
	case "list":
		return t.handleList(args, agentID, userID)
	case "add":
		return t.handleAdd(ctx, args, agentID, userID)
	case "update":
		return t.handleUpdate(args, agentID, userID)
	case "remove":
		return t.handleRemove(args, agentID, userID)
	case "run":
		return t.handleRun(args, agentID, userID)
	case "runs":
		return t.handleRuns(args, agentID, userID)
	default:
		return ErrorResult(fmt.Sprintf("unknown action: %s", action))
	}
}

func (t *CronTool) handleStatus() *Result {
	status := t.cronStore.Status()
	data, _ := json.MarshalIndent(status, "", "  ")
	return NewResult(string(data))
}

func (t *CronTool) handleList(args map[string]any, agentID, userID string) *Result {
	includeDisabled, _ := args["includeDisabled"].(bool)
	jobs := t.cronStore.ListJobs(includeDisabled, agentID, userID)

	result := map[string]any{
		"jobs":  jobs,
		"count": len(jobs),
	}
	data, _ := json.MarshalIndent(result, "", "  ")
	return NewResult(string(data))
}

func (t *CronTool) handleAdd(ctx context.Context, args map[string]any, agentID, userID string) *Result {
	jobObj, ok := args["job"].(map[string]any)
	if !ok {
		return ErrorResult("job object is required for add action")
	}

	name, _ := jobObj["name"].(string)
	if name == "" {
		return ErrorResult("job.name is required")
	}

	scheduleObj, ok := jobObj["schedule"].(map[string]any)
	if !ok {
		return ErrorResult("job.schedule is required")
	}

	message, _ := jobObj["message"].(string)
	if message == "" {
		return ErrorResult("job.message is required")
	}

	// Parse schedule
	schedule := store.CronSchedule{
		Kind: stringFromMap(scheduleObj, "kind"),
	}
	if schedule.Kind == "" {
		return ErrorResult("job.schedule.kind is required (at, every, or cron)")
	}

	switch schedule.Kind {
	case "at":
		if v, ok := numberFromMap(scheduleObj, "atMs"); ok {
			ms := int64(v)
			if ms <= time.Now().UnixMilli() {
				return ErrorResult(fmt.Sprintf("job.schedule.atMs is in the past (%d). Use a future Unix timestamp in milliseconds. Current time is %d ms", ms, time.Now().UnixMilli()))
			}
			schedule.AtMS = &ms
		} else {
			return ErrorResult("job.schedule.atMs is required for 'at' schedule")
		}
	case "every":
		if v, ok := numberFromMap(scheduleObj, "everyMs"); ok {
			ms := int64(v)
			schedule.EveryMS = &ms
		} else {
			return ErrorResult("job.schedule.everyMs is required for 'every' schedule")
		}
	case "cron":
		schedule.Expr = stringFromMap(scheduleObj, "expr")
		if schedule.Expr == "" {
			return ErrorResult("job.schedule.expr is required for 'cron' schedule")
		}
		schedule.TZ = stringFromMap(scheduleObj, "tz")
		if schedule.TZ != "" {
			if _, err := time.LoadLocation(schedule.TZ); err != nil {
				return ErrorResult(fmt.Sprintf("invalid timezone '%s': use IANA names like 'Asia/Ho_Chi_Minh', 'America/New_York'", schedule.TZ))
			}
		}
	default:
		return ErrorResult(fmt.Sprintf("invalid schedule kind: %s (must be at, every, or cron)", schedule.Kind))
	}

	// Optional fields
	deliver, _ := jobObj["deliver"].(bool)
	channel, _ := jobObj["channel"].(string)
	to, _ := jobObj["to"].(string)

	// Auto-default deliver=true when the request comes from a real channel
	// (not CLI/system/subagent). Users chatting on Zalo/Telegram expect
	// cron results delivered back to the same chat.
	if !deliver {
		if ctxChannel := ToolChannelFromCtx(ctx); ctxChannel != "" {
			switch ctxChannel {
			case "cli", "system", "subagent", "cron", "delegate":
				// internal channels — don't auto-deliver
			default:
				deliver = true
			}
		}
	}

	// Auto-fill channel and to from context when deliver is requested.
	// Always prefer context values over LLM-provided values to prevent
	// misrouted deliveries (e.g. LLM confusing guild ID with channel ID).
	if deliver {
		if ctxChannel := ToolChannelFromCtx(ctx); ctxChannel != "" {
			channel = ctxChannel
		}
		if ctxChatID := ToolChatIDFromCtx(ctx); ctxChatID != "" {
			to = ctxChatID
		}
	}

	// Use agent ID from job object if explicitly provided, otherwise from context
	if explicit, _ := jobObj["agentId"].(string); explicit != "" {
		agentID = explicit
	}

	job, err := t.cronStore.AddJob(name, schedule, message, deliver, channel, to, agentID, userID)
	if err != nil {
		return ErrorResult(fmt.Sprintf("failed to create cron job: %v", err))
	}

	data, _ := json.MarshalIndent(map[string]any{"job": job}, "", "  ")
	return NewResult(string(data))
}

// checkJobOwnership validates that the job belongs to the current agent+user scope.
// When agentID/userID is empty, all jobs are accessible.
func (t *CronTool) checkJobOwnership(jobID, agentID, userID string) (*store.CronJob, *Result) {
	job, ok := t.cronStore.GetJob(jobID)
	if !ok {
		return nil, ErrorResult(fmt.Sprintf("job %s not found", jobID))
	}

	// Verify ownership
	if agentID != "" && job.AgentID != agentID {
		return nil, ErrorResult(fmt.Sprintf("job %s not found", jobID))
	}
	if userID != "" && job.UserID != userID {
		return nil, ErrorResult(fmt.Sprintf("job %s not found", jobID))
	}

	return job, nil
}

func (t *CronTool) handleUpdate(args map[string]any, agentID, userID string) *Result {
	jobID := resolveJobID(args)
	if jobID == "" {
		return ErrorResult("jobId is required for update action")
	}

	if _, errResult := t.checkJobOwnership(jobID, agentID, userID); errResult != nil {
		return errResult
	}

	patchObj, ok := args["patch"].(map[string]any)
	if !ok {
		return ErrorResult("patch object is required for update action")
	}

	var patch store.CronJobPatch
	// Re-marshal and unmarshal to leverage JSON tags
	patchJSON, _ := json.Marshal(patchObj)
	json.Unmarshal(patchJSON, &patch)

	job, err := t.cronStore.UpdateJob(jobID, patch)
	if err != nil {
		return ErrorResult(fmt.Sprintf("failed to update cron job: %v", err))
	}

	data, _ := json.MarshalIndent(map[string]any{"job": job}, "", "  ")
	return NewResult(string(data))
}

func (t *CronTool) handleRemove(args map[string]any, agentID, userID string) *Result {
	jobID := resolveJobID(args)
	if jobID == "" {
		return ErrorResult("jobId is required for remove action")
	}

	if _, errResult := t.checkJobOwnership(jobID, agentID, userID); errResult != nil {
		return errResult
	}

	if err := t.cronStore.RemoveJob(jobID); err != nil {
		return ErrorResult(fmt.Sprintf("failed to remove cron job: %v", err))
	}

	data, _ := json.MarshalIndent(map[string]any{"deleted": true, "jobId": jobID}, "", "  ")
	return NewResult(string(data))
}

func (t *CronTool) handleRun(args map[string]any, agentID, userID string) *Result {
	jobID := resolveJobID(args)
	if jobID == "" {
		return ErrorResult("jobId is required for run action")
	}

	if _, errResult := t.checkJobOwnership(jobID, agentID, userID); errResult != nil {
		return errResult
	}

	runMode, _ := args["runMode"].(string)
	force := runMode == "force"

	ran, reason, err := t.cronStore.RunJob(jobID, force)
	if err != nil {
		return ErrorResult(fmt.Sprintf("failed to run cron job: %v", err))
	}

	result := map[string]any{
		"ran":   ran,
		"jobId": jobID,
	}
	if !ran && reason != "" {
		result["reason"] = reason
	}
	data, _ := json.MarshalIndent(result, "", "  ")
	return NewResult(string(data))
}

func (t *CronTool) handleRuns(args map[string]any, agentID, userID string) *Result {
	jobID := resolveJobID(args)

	// Validate ownership if a specific job is requested
	if jobID != "" {
		if _, errResult := t.checkJobOwnership(jobID, agentID, userID); errResult != nil {
			return errResult
		}
	}

	limit := 20
	if v, ok := numberFromMap(args, "limit"); ok {
		limit = int(v)
	}

	entries, total := t.cronStore.GetRunLog(jobID, limit, 0)

	result := map[string]any{
		"entries": entries,
		"count":   len(entries),
		"total":   total,
	}
	data, _ := json.MarshalIndent(result, "", "  ")
	return NewResult(string(data))
}

// --- helpers ---

func resolveJobID(args map[string]any) string {
	if id, ok := args["jobId"].(string); ok && id != "" {
		return id
	}
	if id, ok := args["id"].(string); ok && id != "" {
		return id
	}
	return ""
}

func stringFromMap(m map[string]any, key string) string {
	v, _ := m[key].(string)
	return v
}

func numberFromMap(m map[string]any, key string) (float64, bool) {
	v, ok := m[key].(float64)
	return v, ok
}
