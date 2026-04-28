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

// validForgePhases enumerates the phases the eng agent's plan flow uses.
// Anything else is rejected at the tool boundary so a typo doesn't reach
// the agent service. Aligned with AGENTS.md step 4.
var validForgePhases = map[string]bool{
	"autoplan": true,
	"impl":     true,
	"review":   true,
	"qa":       true,
	"ship":     true,
}

// SpawnForgeJobTool delegates a forge run to a k8s Job created by the
// sibling agent service. The tool itself is fire-and-forget: it
// HMAC-signs a POST to the agent service on the in-pod loopback,
// records a subagent_tasks row so the recovery loop can reconcile a
// pod restart mid-Job, and returns a job_id to the LLM.
//
// Why a separate tool instead of just shelling out forge inline: forge
// runs 5–30 min for a real implementation pass, and goclaw's exec tool
// caps shell calls at 60s. Daemonizing forge inside goclaw works (we
// shipped it in PR #4456) but doesn't survive goclaw OOMs/restarts.
// k8s Jobs run as their own pods with podAffinity to the goclaw node
// (RWO PVC constraint), so they keep going even when goclaw dies.
//
// The agent service owns Job creation/watching (it has client-go);
// goclaw only sees the row + the eventual completion callback that
// posts results to the originating Discord thread.
type SpawnForgeJobTool struct {
	taskStore     store.SubagentTaskStore
	agentURL      string // http://127.0.0.1:18789 in-pod
	hmacSecret    []byte // shared with agent service for HMAC
	httpClient    *http.Client
	tenantChecker ChannelTenantChecker
}

// NewSpawnForgeJobTool creates a tool wired against the in-pod agent
// service. agentURL defaults to http://127.0.0.1:18789 when empty —
// matches the existing internalListen used by /github-token,
// /docs-sync-config, /mercury-token, etc.
func NewSpawnForgeJobTool(taskStore store.SubagentTaskStore, agentURL string, hmacSecret []byte) *SpawnForgeJobTool {
	if agentURL == "" {
		agentURL = "http://127.0.0.1:18789"
	}
	return &SpawnForgeJobTool{
		taskStore:  taskStore,
		agentURL:   strings.TrimRight(agentURL, "/"),
		hmacSecret: hmacSecret,
		httpClient: &http.Client{Timeout: 10 * time.Second},
	}
}

func (t *SpawnForgeJobTool) Name() string { return "spawn_forge_job" }

func (t *SpawnForgeJobTool) Description() string {
	return "Spawn a forge run as a k8s Job for a long-running phase " +
		"(autoplan / impl / review / qa / ship). Returns immediately with " +
		"a job_id; progress + completion are posted back to the originating " +
		"Discord thread automatically. Use this for the plan-flow phases — " +
		"never run forge synchronously via exec for these, the 60s exec cap " +
		"will SIGTERM mid-implementation."
}

func (t *SpawnForgeJobTool) Parameters() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"phase": map[string]any{
				"type":        "string",
				"description": "Plan-flow phase: autoplan, impl, review, qa, or ship.",
				"enum":        []string{"autoplan", "impl", "review", "qa", "ship"},
			},
			"forge_prompt": map[string]any{
				"type":        "string",
				"description": "The prompt forge runs. For impl: the plan body. For other phases: '/review' / '/qa' / '/ship' / '/autoplan <ask>'.",
			},
			"worktree_path": map[string]any{
				"type":        "string",
				"description": "Absolute path to the worktree forge runs in (e.g. /data/workspace-eng/worktrees/<task-name>-<uuid>). MUST be unique per concurrent task — the PVC is RWO, concurrent jobs sharing a worktree corrupt git state.",
			},
			"thread_id": map[string]any{
				"type":        "string",
				"description": "Discord thread ID where progress + completion messages are posted. Required for plan-flow phases.",
			},
			"owner": map[string]any{
				"type":        "string",
				"description": "GitHub owner (e.g. cartridge-gg).",
			},
			"repo": map[string]any{
				"type":        "string",
				"description": "GitHub repo slug (e.g. controller-rs).",
			},
		},
		"required": []string{"phase", "forge_prompt", "worktree_path", "thread_id", "owner", "repo"},
	}
}

// SetChannelTenantChecker is a no-op for this tool today — the only
// caller is goclaw's own agent loop, which already runs in a tenant
// context. We accept the setter for symmetry with other channel-aware
// tools.
func (t *SpawnForgeJobTool) SetChannelTenantChecker(c ChannelTenantChecker) {
	t.tenantChecker = c
}

// SetAgentServiceURL injects the in-pod URL for the sibling agent
// service. Wired post-construction so gateway_tools_wiring.go doesn't
// need a cfg param. Empty value falls back to the default
// http://127.0.0.1:18789.
func (t *SpawnForgeJobTool) SetAgentServiceURL(url string) {
	if url == "" {
		return
	}
	t.agentURL = strings.TrimRight(url, "/")
}

// SetHMACSecret injects the shared HMAC secret used to sign requests
// to the agent service. Without it, requests will be rejected by the
// agent service's VerifySignature and Execute returns an error to the
// LLM rather than panicking.
func (t *SpawnForgeJobTool) SetHMACSecret(secret []byte) {
	t.hmacSecret = secret
}

// SpawnForgeJobRequest is the wire shape sent to the agent service.
type SpawnForgeJobRequest struct {
	JobID        string `json:"job_id"`        // pre-allocated by goclaw, used as subagent_tasks row id + Job label
	Phase        string `json:"phase"`         // autoplan|impl|review|qa|ship
	ForgePrompt  string `json:"forge_prompt"`  // raw prompt forge runs
	WorktreePath string `json:"worktree_path"` // absolute path
	ThreadID     string `json:"thread_id"`     // Discord thread for progress + completion posts
	Channel      string `json:"channel"`       // channel instance name (e.g. discord-eng) so the agent service knows where to route the callback
	Owner        string `json:"owner"`         // GitHub owner — used in Job labels for ops queries
	Repo         string `json:"repo"`          // GitHub repo
	TenantID     string `json:"tenant_id"`     // tenant uuid as string; agent service echoes to goclaw on callback
	ParentSession string `json:"parent_session_key,omitempty"` // origin session — populated when known so the completion callback can re-wake the parent agent
}

// SpawnForgeJobResponse is what the agent service returns on success.
type SpawnForgeJobResponse struct {
	JobID       string `json:"job_id"`        // echoed back
	K8sJobName  string `json:"k8s_job_name"`  // batch/v1.Job .metadata.name
	K8sJobUID   string `json:"k8s_job_uid"`   // .metadata.uid for cross-correlation
}

func (t *SpawnForgeJobTool) Execute(ctx context.Context, args map[string]any) *Result {
	if len(t.hmacSecret) == 0 {
		// Configuration guard: the tool is registered unconditionally
		// at boot but useless without a shared HMAC secret. Surface a
		// clean error to the LLM instead of generating a 401 from the
		// agent service. Operators see the same message in the agent
		// trace. Likely cause: CARTRIDGE_WEBHOOK_SECRET not set in
		// the goclaw container's env (CSI mount unreachable, dev
		// build, etc.).
		return ErrorResult("spawn_forge_job: HMAC secret not configured (CARTRIDGE_WEBHOOK_SECRET / gateway.jobs_callback_secret). Tool not usable in this environment.")
	}

	// 1. Validate args. Phase enum + required fields.
	phase := strings.TrimSpace(argString(args, "phase"))
	if !validForgePhases[phase] {
		return ErrorResult(fmt.Sprintf("phase must be one of autoplan|impl|review|qa|ship; got %q", phase))
	}
	forgePrompt := argString(args, "forge_prompt")
	if forgePrompt == "" {
		return ErrorResult("forge_prompt is required")
	}
	worktreePath := strings.TrimSpace(argString(args, "worktree_path"))
	if worktreePath == "" {
		return ErrorResult("worktree_path is required")
	}
	threadID := strings.TrimSpace(argString(args, "thread_id"))
	if threadID == "" {
		return ErrorResult("thread_id is required (Discord thread for progress + completion posts)")
	}
	owner := strings.TrimSpace(argString(args, "owner"))
	if owner == "" {
		return ErrorResult("owner is required")
	}
	repo := strings.TrimSpace(argString(args, "repo"))
	if repo == "" {
		return ErrorResult("repo is required")
	}

	// 2. Pre-allocate the job_id so it's the SAME id used as subagent_tasks
	//    row id, k8s Job label, and the path component on the callback URL.
	//    Single source of truth means correlation across the three layers
	//    is just a string compare.
	jobUUID := uuid.New()
	jobID := jobUUID.String()

	// 3. Resolve tenant + origin context. Failing to resolve the tenant
	//    is a hard error — without it the row writes can't tenant-scope
	//    and recovery can't post to the right channel.
	tenantID := store.TenantIDFromContext(ctx)
	if tenantID == uuid.Nil {
		return ErrorResult("spawn_forge_job: no tenant in context — refusing to spawn")
	}
	channel := ToolChannelFromCtx(ctx)
	if channel == "" {
		return ErrorResult("spawn_forge_job: no channel in context — refusing to spawn (callbacks would have nowhere to post)")
	}
	parentSessionKey := ToolSessionKeyFromCtx(ctx)

	// 4. Insert the subagent_tasks row BEFORE calling the agent service.
	//    Order matters: if the row write fails, we never create the Job,
	//    so we never have an unrecoverable orphan. If the agent service
	//    POST fails after the row exists, the recovery loop on next boot
	//    will mark the row interrupted and notify the thread (PR #17).
	//    Subject is human-readable for the subagents-list tool.
	taskRow := &store.SubagentTaskData{
		BaseModel:        store.BaseModel{ID: jobUUID},
		TenantID:         tenantID,
		ParentAgentKey:   "eng",
		Subject:          fmt.Sprintf("forge %s — %s/%s", phase, owner, repo),
		Description:      truncate(forgePrompt, 500),
		Status:           "running",
		Depth:            1,
		OriginChannel:    strPtr(channel),
		OriginChatID:     strPtr(threadID),
		OriginPeerKind:   strPtr("group"), // Discord threads are always group-shaped
		Metadata: map[string]any{
			"phase":         phase,
			"worktree_path": worktreePath,
			"forge_prompt":  truncate(forgePrompt, 2000), // keep recovery context but cap
			"owner":         owner,
			"repo":          repo,
			"thread_id":     threadID,
		},
	}
	if parentSessionKey != "" {
		s := parentSessionKey
		taskRow.SessionKey = &s
	}
	if t.taskStore != nil {
		if err := t.taskStore.Create(ctx, taskRow); err != nil {
			return ErrorResult(fmt.Sprintf("spawn_forge_job: failed to write subagent_tasks row: %v", err))
		}
	}

	// 5. POST to the agent service. HMAC-sign the body using the same
	//    webhook secret stream-review uses for check-run callbacks; the
	//    agent service already has VerifySignature available for the
	//    incoming /jobs handler.
	body, err := json.Marshal(SpawnForgeJobRequest{
		JobID:         jobID,
		Phase:         phase,
		ForgePrompt:   forgePrompt,
		WorktreePath:  worktreePath,
		ThreadID:      threadID,
		Channel:       channel,
		Owner:         owner,
		Repo:          repo,
		TenantID:      tenantID.String(),
		ParentSession: parentSessionKey,
	})
	if err != nil {
		return ErrorResult(fmt.Sprintf("spawn_forge_job: marshal request: %v", err))
	}
	sig := computeHMACSignature(body, t.hmacSecret)

	url := t.agentURL + "/jobs"
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return ErrorResult(fmt.Sprintf("spawn_forge_job: build request: %v", err))
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Hub-Signature-256", "sha256="+sig)

	resp, err := t.httpClient.Do(req)
	if err != nil {
		// On POST failure, the row stays as 'running'. The recovery loop
		// will eventually catch + interrupt it. Surface the error to the
		// LLM so it can decide whether to retry or escalate.
		return ErrorResult(fmt.Sprintf("spawn_forge_job: agent service POST failed: %v", err))
	}
	defer func() { _ = resp.Body.Close() }()

	respBody, _ := io.ReadAll(resp.Body)
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return ErrorResult(fmt.Sprintf("spawn_forge_job: agent service returned %d: %s", resp.StatusCode, truncate(string(respBody), 500)))
	}

	var out SpawnForgeJobResponse
	if err := json.Unmarshal(respBody, &out); err != nil {
		return ErrorResult(fmt.Sprintf("spawn_forge_job: parse response: %v", err))
	}

	// 6. Update the row with the actual k8s Job name now that we have
	//    it. Best-effort — the row is functional without it (recovery
	//    just won't be able to look up the Job in k8s, falls back to
	//    interrupted-on-restart). Doesn't fail the tool call.
	if t.taskStore != nil && out.K8sJobName != "" {
		_ = t.taskStore.UpdateMetadata(ctx, jobUUID, map[string]any{
			"k8s_job_name": out.K8sJobName,
			"k8s_job_uid":  out.K8sJobUID,
		})
	}

	slog.Info("spawn_forge_job",
		"job_id", jobID,
		"phase", phase,
		"k8s_job_name", out.K8sJobName,
		"thread_id", threadID,
		"owner", owner,
		"repo", repo,
	)

	// 7. Tell the LLM what happened. The next agent turn (after the
	//    completion callback wakes the parent agent) will see the
	//    actual forge output. For now: just the bookkeeping.
	return NewResult(fmt.Sprintf(
		"Spawned forge job %s (phase=%s, k8s_job=%s). Progress + completion will land in thread %s automatically; end your turn and wait for the next wake.",
		jobID, phase, out.K8sJobName, threadID,
	))
}

// computeHMACSignature returns hex(HMAC-SHA256(secret, body)). Mirrors
// agent/github/signature.go so the agent service can verify with its
// existing VerifySignature.
func computeHMACSignature(body, secret []byte) string {
	mac := hmac.New(sha256.New, secret)
	mac.Write(body)
	return hex.EncodeToString(mac.Sum(nil))
}

func strPtr(s string) *string { return &s }
