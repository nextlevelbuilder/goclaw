// Package jobs provides an in-process API for spawning agent-service
// jobs (the same K8s-pod-based work the spawn_job tool dispatches).
//
// Why a service exists separate from the SpawnJobTool: the tool is
// agent-callable only — it parses LLM-shaped arguments, validates,
// and POSTs. Other goclaw subsystems (the voice supervisor, the cron
// runner, future internal flows) need to spawn jobs without going
// through the tool's argument layer. This package factors out the
// HMAC-signing + agent-service POST + task-row writing so those
// callers get the same lifecycle without duplicating logic.
//
// Project-agnostic: the agent-service URL + HMAC secret + task store
// are all injected. No assumptions about specific consumers.
package jobs

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
)

// Request is the wire shape posted to the agent service. Mirrors
// SpawnJobRequest in internal/tools/spawn_job.go — kept structurally
// identical so the agent-service contract is one struct, not two.
// (We can't import the tools package from here without an import cycle
// once tools depends on jobs, so the type is duplicated. The tool
// converts between them via field copy.)
type Request struct {
	JobID         string            `json:"job_id"`
	Kind          string            `json:"kind"`
	Command       string            `json:"command"`
	Args          []string          `json:"args,omitempty"`
	Cwd           string            `json:"cwd,omitempty"`
	WorkspaceRoot string            `json:"workspace_root,omitempty"`
	WorktreePath  string            `json:"worktree_path"`
	Timeout       string            `json:"timeout,omitempty"`
	Resources     Resources         `json:"resources,omitempty"`
	Env           map[string]string `json:"env,omitempty"`
	Sinks         []Sink            `json:"sinks"`
	TenantID      string            `json:"tenant_id,omitempty"`
	ParentSession string            `json:"parent_session_key,omitempty"`
	Model         string            `json:"model,omitempty"`
	Provider      string            `json:"provider,omitempty"`
	ActivateSkill string            `json:"activate_skill,omitempty"`
}

type Resources struct {
	CPURequest    string `json:"cpu_request,omitempty"`
	CPULimit      string `json:"cpu_limit,omitempty"`
	MemoryRequest string `json:"memory_request,omitempty"`
	MemoryLimit   string `json:"memory_limit,omitempty"`
}

type Sink struct {
	Type       string `json:"type"`
	Channel    string `json:"channel,omitempty"`
	ThreadID   string `json:"thread_id,omitempty"`
	MessageID  string `json:"message_id,omitempty"` // for discord sink edit mode (PR 3)
	Action     string `json:"action,omitempty"`     // discord sink: "post" (default) | "edit"
	Owner      string `json:"owner,omitempty"`
	Repo       string `json:"repo,omitempty"`
	CheckRunID int64  `json:"check_run_id,omitempty"`
	PRNumber   int    `json:"pr_number,omitempty"`
}

// Response is the agent service's reply on a successful spawn.
type Response struct {
	JobID      string `json:"job_id"`
	K8sJobName string `json:"k8s_job_name"`
	K8sJobUID  string `json:"k8s_job_uid"`
}

// Service is the in-process job spawner. Construct one per goclaw
// process and share across callers (it's safe for concurrent use —
// the underlying http.Client + hmac slice are immutable).
type Service struct {
	agentURL   string
	hmacSecret []byte
	httpClient *http.Client
}

// NewService creates a job-spawning service wired against the in-pod
// agent service.
func NewService(agentURL string, hmacSecret []byte) *Service {
	if agentURL == "" {
		agentURL = "http://127.0.0.1:18789"
	}
	return &Service{
		agentURL:   strings.TrimRight(agentURL, "/"),
		hmacSecret: hmacSecret,
		httpClient: &http.Client{Timeout: 10 * time.Second},
	}
}

// AgentURL returns the configured agent-service base URL. Useful for
// callers that need to log it or share the same target endpoint.
func (s *Service) AgentURL() string { return s.agentURL }

// HMACSecret returns a copy of the secret so SpawnJobTool can stay in
// sync (it owns the same wire contract). Callers must not mutate the
// returned slice.
func (s *Service) HMACSecret() []byte {
	if len(s.hmacSecret) == 0 {
		return nil
	}
	out := make([]byte, len(s.hmacSecret))
	copy(out, s.hmacSecret)
	return out
}

// Spawn POSTs req to the agent service, signed with HMAC-SHA256 in
// the X-Hub-Signature-256 header. Returns the agent service's
// Response on success.
//
// Caller responsibilities:
//   - Set req.JobID (or leave empty and check the returned Response).
//   - Validate req.Sinks + WorktreePath etc. — Spawn does no semantic
//     validation; it's a transport. Use the SpawnJobTool's parsing
//     path (or duplicate its checks) for agent-driven invocations.
func (s *Service) Spawn(ctx context.Context, req Request) (*Response, error) {
	if len(s.hmacSecret) == 0 {
		return nil, fmt.Errorf("jobs.Service: HMAC secret not configured")
	}

	body, err := json.Marshal(req)
	if err != nil {
		return nil, fmt.Errorf("marshal request: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, s.agentURL+"/jobs", bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("build request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("X-Hub-Signature-256", "sha256="+computeHMACSignature(body, s.hmacSecret))

	resp, err := s.httpClient.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("agent service POST failed: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	respBody, _ := io.ReadAll(resp.Body)
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("agent service returned %d: %s", resp.StatusCode, truncate(string(respBody), 500))
	}

	var out Response
	if err := json.Unmarshal(respBody, &out); err != nil {
		return nil, fmt.Errorf("parse response: %w", err)
	}
	slog.Info("jobs.Spawn", "job_id", out.JobID, "kind", req.Kind, "k8s_job_name", out.K8sJobName,
		"model", req.Model, "provider", req.Provider, "activate_skill", req.ActivateSkill)
	return &out, nil
}

func computeHMACSignature(body, secret []byte) string {
	mac := hmac.New(sha256.New, secret)
	mac.Write(body)
	return hex.EncodeToString(mac.Sum(nil))
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}
