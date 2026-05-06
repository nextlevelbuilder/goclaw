package tools

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/nextlevelbuilder/goclaw/internal/store"
)

// SpawnJobTool delegates long-running work to the sibling agent service,
// which creates a Kubernetes Job. Discord-originated jobs get a
// subagent_tasks row before the POST so progress and recovery callbacks have
// a stable row to target.
type SpawnJobTool struct {
	taskStore     store.SubagentTaskStore
	agentURL      string
	hmacSecret    []byte
	httpClient    *http.Client
	tenantChecker ChannelTenantChecker
}

// NewSpawnJobTool creates a tool wired against the in-pod agent service.
func NewSpawnJobTool(taskStore store.SubagentTaskStore, agentURL string, hmacSecret []byte) *SpawnJobTool {
	if agentURL == "" {
		agentURL = "http://127.0.0.1:18789"
	}
	return &SpawnJobTool{
		taskStore:  taskStore,
		agentURL:   strings.TrimRight(agentURL, "/"),
		hmacSecret: hmacSecret,
		httpClient: &http.Client{Timeout: 10 * time.Second},
	}
}

func (t *SpawnJobTool) Name() string { return "spawn_job" }

func (t *SpawnJobTool) Description() string {
	return "Spawn a long-running command as a Kubernetes Job. The Job uses the trusted goclaw image, mounts /data, streams progress to configured sinks, and returns immediately with a job_id."
}

func (t *SpawnJobTool) Parameters() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"kind": map[string]any{
				"type":        "string",
				"description": "Job kind, for example autoplan, impl, review, qa, ship, pr-review, docs-sync, or cron.",
			},
			"command": map[string]any{
				"type":        "string",
				"description": "Executable to run inside the trusted Job image, for example forge or /app/agent/bin/run-pr-review.",
			},
			"args": map[string]any{
				"type":        "array",
				"items":       map[string]any{"type": "string"},
				"description": "Command arguments.",
			},
			"cwd": map[string]any{
				"type":        "string",
				"description": "Absolute working directory inside /data.",
			},
			"workspace_root": map[string]any{
				"type":        "string",
				"description": "Absolute workspace root inside /data.",
			},
			"worktree_path": map[string]any{
				"type":        "string",
				"description": "Dedicated absolute worktree path inside /data. Write-capable jobs must not share a worktree.",
			},
			"timeout": map[string]any{
				"type":        "string",
				"description": "Optional duration such as 30m or 2h.",
			},
			"resources": map[string]any{
				"type":        "object",
				"description": "Optional cpu_request, cpu_limit, memory_request, memory_limit overrides.",
			},
			"env": map[string]any{
				"type":        "object",
				"description": "Optional literal environment variables. Do not put secrets here.",
			},
			"sinks": map[string]any{
				"type":        "array",
				"description": "At least one sink. Discord sinks use type=discord, channel, thread_id. GitHub sinks use type=github_check or github_pr_review.",
				"items": map[string]any{
					"type": "object",
					"properties": map[string]any{
						"type":         map[string]any{"type": "string"},
						"channel":      map[string]any{"type": "string"},
						"thread_id":    map[string]any{"type": "string"},
						"owner":        map[string]any{"type": "string"},
						"repo":         map[string]any{"type": "string"},
						"check_run_id": map[string]any{"type": "integer"},
						"pr_number":    map[string]any{"type": "integer"},
					},
				},
			},
			"model": map[string]any{
				"type":        "string",
				"description": "Optional model override for the spawned worker (e.g. \"deepseek/deepseek-v4-pro\"). Empty = inherit from the worker's bound agent.",
			},
			"provider": map[string]any{
				"type":        "string",
				"description": "Optional provider override (e.g. \"openrouter\"). Empty = inherit from the worker's bound agent. Pair with model when routing to a non-default provider.",
			},
			"activate_skill": map[string]any{
				"type":        "string",
				"description": "Optional skill name (not slug) to pre-load into the worker's first turn so its instructions land before the user message. Empty = no pre-activation.",
			},
		},
		"required": []string{"kind", "command", "worktree_path", "sinks"},
	}
}

func (t *SpawnJobTool) SetChannelTenantChecker(c ChannelTenantChecker) {
	t.tenantChecker = c
}

func (t *SpawnJobTool) SetAgentServiceURL(url string) {
	if url != "" {
		t.agentURL = strings.TrimRight(url, "/")
	}
}

func (t *SpawnJobTool) SetHMACSecret(secret []byte) {
	t.hmacSecret = secret
}

// SpawnJobRequest is the HMAC-signed wire contract with the agent service.
type SpawnJobRequest struct {
	JobID         string            `json:"job_id"`
	Kind          string            `json:"kind"`
	Command       string            `json:"command"`
	Args          []string          `json:"args,omitempty"`
	Cwd           string            `json:"cwd,omitempty"`
	WorkspaceRoot string            `json:"workspace_root,omitempty"`
	WorktreePath  string            `json:"worktree_path"`
	Timeout       string            `json:"timeout,omitempty"`
	Resources     JobResources      `json:"resources,omitempty"`
	Env           map[string]string `json:"env,omitempty"`
	Sinks         []JobSink         `json:"sinks"`
	TenantID      string            `json:"tenant_id,omitempty"`
	ParentSession string            `json:"parent_session_key,omitempty"`

	// Optional model/provider override for the spawned worker. When set,
	// the worker's agent loop uses these instead of the registered
	// agent's default. Useful for routing heavy summarization /
	// reasoning work to a different model than the agent's chat path.
	// Empty = inherit from the worker's bound agent.
	Model    string `json:"model,omitempty"`
	Provider string `json:"provider,omitempty"`

	// ActivateSkill pre-loads a named skill into the worker's first
	// turn (equivalent to the agent calling use_skill on its first
	// step). Useful when the spawned job has a fixed purpose — e.g.
	// "voice-session-summarization" — and you want the skill's
	// instructions to land before the user message instead of after a
	// skill_search round-trip. Empty = no pre-activation.
	ActivateSkill string `json:"activate_skill,omitempty"`
}

type JobResources struct {
	CPURequest    string `json:"cpu_request,omitempty"`
	CPULimit      string `json:"cpu_limit,omitempty"`
	MemoryRequest string `json:"memory_request,omitempty"`
	MemoryLimit   string `json:"memory_limit,omitempty"`
}

type JobSink struct {
	Type       string `json:"type"`
	Channel    string `json:"channel,omitempty"`
	ThreadID   string `json:"thread_id,omitempty"`
	Owner      string `json:"owner,omitempty"`
	Repo       string `json:"repo,omitempty"`
	CheckRunID int64  `json:"check_run_id,omitempty"`
	PRNumber   int    `json:"pr_number,omitempty"`
}

// SpawnJobResponse is what the agent service returns on success.
type SpawnJobResponse struct {
	JobID      string `json:"job_id"`
	K8sJobName string `json:"k8s_job_name"`
	K8sJobUID  string `json:"k8s_job_uid"`
}

func (t *SpawnJobTool) Execute(ctx context.Context, args map[string]any) *Result {
	if len(t.hmacSecret) == 0 {
		return ErrorResult("spawn_job: HMAC secret not configured (CARTRIDGE_WEBHOOK_SECRET / gateway.jobs_callback_secret)")
	}

	req, err := parseSpawnJobArgs(args)
	if err != nil {
		return ErrorResult("spawn_job: " + err.Error())
	}

	jobUUID := uuid.New()
	req.JobID = jobUUID.String()

	tenantID := store.TenantIDFromContext(ctx)
	parentSessionKey := ToolSessionKeyFromCtx(ctx)
	if tenantID != uuid.Nil {
		req.TenantID = tenantID.String()
	}
	req.ParentSession = parentSessionKey

	discordSink, hasDiscord := firstDiscordSink(req.Sinks)
	if hasDiscord {
		if tenantID == uuid.Nil {
			return ErrorResult("spawn_job: no tenant in context — refusing Discord job")
		}
		if discordSink.Channel == "" {
			discordSink.Channel = ToolChannelFromCtx(ctx)
		}
		if discordSink.ThreadID == "" {
			discordSink.ThreadID = ToolChatIDFromCtx(ctx)
		}
		if discordSink.Channel == "" || discordSink.ThreadID == "" {
			return ErrorResult("spawn_job: Discord sink requires channel and thread_id")
		}
		replaceFirstDiscordSink(req.Sinks, discordSink)
		if err := t.createTaskRow(ctx, jobUUID, tenantID, parentSessionKey, req, discordSink); err != nil {
			return ErrorResult(fmt.Sprintf("spawn_job: failed to write subagent_tasks row: %v", err))
		}
	}

	body, err := json.Marshal(req)
	if err != nil {
		return ErrorResult(fmt.Sprintf("spawn_job: marshal request: %v", err))
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, t.agentURL+"/jobs", bytes.NewReader(body))
	if err != nil {
		return ErrorResult(fmt.Sprintf("spawn_job: build request: %v", err))
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("X-Hub-Signature-256", "sha256="+computeHMACSignature(body, t.hmacSecret))

	resp, err := t.httpClient.Do(httpReq)
	if err != nil {
		return ErrorResult(fmt.Sprintf("spawn_job: agent service POST failed: %v", err))
	}
	defer func() { _ = resp.Body.Close() }()

	respBody, _ := io.ReadAll(resp.Body)
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return ErrorResult(fmt.Sprintf("spawn_job: agent service returned %d: %s", resp.StatusCode, truncate(string(respBody), 500)))
	}

	var out SpawnJobResponse
	if err := json.Unmarshal(respBody, &out); err != nil {
		return ErrorResult(fmt.Sprintf("spawn_job: parse response: %v", err))
	}
	if t.taskStore != nil && hasDiscord && out.K8sJobName != "" {
		_ = t.taskStore.UpdateMetadata(ctx, jobUUID, map[string]any{
			"k8s_job_name": out.K8sJobName,
			"k8s_job_uid":  out.K8sJobUID,
		})
	}

	slog.Info("spawn_job", "job_id", req.JobID, "kind", req.Kind, "k8s_job_name", out.K8sJobName)
	return NewResult(fmt.Sprintf("Spawned job %s (kind=%s, k8s_job=%s). Progress and completion will be delivered to the configured sinks.", req.JobID, req.Kind, out.K8sJobName))
}

func parseSpawnJobArgs(args map[string]any) (SpawnJobRequest, error) {
	req := SpawnJobRequest{
		Kind:          strings.TrimSpace(argString(args, "kind")),
		Command:       strings.TrimSpace(argString(args, "command")),
		Cwd:           strings.TrimSpace(argString(args, "cwd")),
		WorkspaceRoot: strings.TrimSpace(argString(args, "workspace_root")),
		WorktreePath:  strings.TrimSpace(argString(args, "worktree_path")),
		Timeout:       strings.TrimSpace(argString(args, "timeout")),
		Model:         strings.TrimSpace(argString(args, "model")),
		Provider:      strings.TrimSpace(argString(args, "provider")),
		ActivateSkill: strings.TrimSpace(argString(args, "activate_skill")),
	}
	if req.Kind == "" {
		return req, fmt.Errorf("kind is required")
	}
	if req.Command == "" {
		return req, fmt.Errorf("command is required")
	}
	if req.WorktreePath == "" {
		return req, fmt.Errorf("worktree_path is required")
	}
	if !strings.HasPrefix(req.WorktreePath, "/data/") {
		return req, fmt.Errorf("worktree_path must be under /data")
	}
	if req.Cwd != "" && !strings.HasPrefix(req.Cwd, "/data/") {
		return req, fmt.Errorf("cwd must be under /data")
	}
	if req.WorkspaceRoot != "" && !strings.HasPrefix(req.WorkspaceRoot, "/data/") {
		return req, fmt.Errorf("workspace_root must be under /data")
	}
	req.Args = argStringSlice(args, "args")
	req.Env = argStringMap(args, "env")
	req.Resources = parseResources(args["resources"])
	req.Sinks = parseSinks(args["sinks"])
	if len(req.Sinks) == 0 {
		return req, fmt.Errorf("at least one sink is required")
	}
	for _, sink := range req.Sinks {
		switch sink.Type {
		case "discord":
		case "github_check":
			if sink.Owner == "" || sink.Repo == "" || sink.CheckRunID == 0 {
				return req, fmt.Errorf("github_check sink requires owner, repo, and check_run_id")
			}
		case "github_pr_review":
			if sink.Owner == "" || sink.Repo == "" || sink.PRNumber == 0 {
				return req, fmt.Errorf("github_pr_review sink requires owner, repo, and pr_number")
			}
		default:
			return req, fmt.Errorf("unsupported sink type %q", sink.Type)
		}
	}
	return req, nil
}

func (t *SpawnJobTool) createTaskRow(ctx context.Context, id uuid.UUID, tenantID uuid.UUID, parentSessionKey string, req SpawnJobRequest, sink JobSink) error {
	row := &store.SubagentTaskData{
		BaseModel:      store.BaseModel{ID: id},
		TenantID:       tenantID,
		ParentAgentKey: "eng",
		Subject:        fmt.Sprintf("job %s", req.Kind),
		Description:    truncate(req.Command+" "+strings.Join(req.Args, " "), 500),
		Status:         "running",
		Depth:          1,
		OriginChannel:  strPtr(sink.Channel),
		OriginChatID:   strPtr(sink.ThreadID),
		OriginPeerKind: strPtr("group"),
		Metadata: map[string]any{
			"runner":         "spawn_job",
			"kind":           req.Kind,
			"command":        req.Command,
			"args":           req.Args,
			"cwd":            req.Cwd,
			"workspace_root": req.WorkspaceRoot,
			"worktree_path":  req.WorktreePath,
			"sinks":          req.Sinks,
		},
	}
	if parentSessionKey != "" {
		row.SessionKey = &parentSessionKey
	}
	if t.taskStore == nil {
		return nil
	}
	return t.taskStore.Create(ctx, row)
}

func firstDiscordSink(sinks []JobSink) (JobSink, bool) {
	for _, sink := range sinks {
		if sink.Type == "discord" {
			return sink, true
		}
	}
	return JobSink{}, false
}

func replaceFirstDiscordSink(sinks []JobSink, replacement JobSink) {
	for i := range sinks {
		if sinks[i].Type == "discord" {
			sinks[i] = replacement
			return
		}
	}
}

func parseResources(v any) JobResources {
	m, ok := v.(map[string]any)
	if !ok {
		return JobResources{}
	}
	return JobResources{
		CPURequest:    strings.TrimSpace(anyString(m["cpu_request"])),
		CPULimit:      strings.TrimSpace(anyString(m["cpu_limit"])),
		MemoryRequest: strings.TrimSpace(anyString(m["memory_request"])),
		MemoryLimit:   strings.TrimSpace(anyString(m["memory_limit"])),
	}
}

func parseSinks(v any) []JobSink {
	items, ok := v.([]any)
	if !ok {
		return nil
	}
	out := make([]JobSink, 0, len(items))
	for _, item := range items {
		m, ok := item.(map[string]any)
		if !ok {
			continue
		}
		out = append(out, JobSink{
			Type:       strings.TrimSpace(anyString(m["type"])),
			Channel:    strings.TrimSpace(anyString(m["channel"])),
			ThreadID:   strings.TrimSpace(anyString(m["thread_id"])),
			Owner:      strings.TrimSpace(anyString(m["owner"])),
			Repo:       strings.TrimSpace(anyString(m["repo"])),
			CheckRunID: int64(anyInt(m["check_run_id"])),
			PRNumber:   anyInt(m["pr_number"]),
		})
	}
	return out
}

func argStringSlice(args map[string]any, key string) []string {
	items, ok := args[key].([]any)
	if !ok {
		return nil
	}
	out := make([]string, 0, len(items))
	for _, item := range items {
		out = append(out, anyString(item))
	}
	return out
}

func argStringMap(args map[string]any, key string) map[string]string {
	raw, ok := args[key].(map[string]any)
	if !ok {
		return nil
	}
	out := make(map[string]string, len(raw))
	for k, v := range raw {
		out[k] = anyString(v)
	}
	return out
}

func anyString(v any) string {
	switch x := v.(type) {
	case string:
		return x
	case fmt.Stringer:
		return x.String()
	default:
		if v == nil {
			return ""
		}
		return fmt.Sprint(v)
	}
}

func anyInt(v any) int {
	switch x := v.(type) {
	case int:
		return x
	case int64:
		return int(x)
	case float64:
		return int(x)
	case json.Number:
		i, _ := x.Int64()
		return int(i)
	default:
		return 0
	}
}

func computeHMACSignature(body, secret []byte) string {
	mac := hmac.New(sha256.New, secret)
	mac.Write(body)
	return hex.EncodeToString(mac.Sum(nil))
}

func strPtr(s string) *string { return &s }
