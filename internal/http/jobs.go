package http

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/nextlevelbuilder/goclaw/internal/store"
)

// JobsHandler serves the two HTTP endpoints that close the loop on a
// forge Job spawned via the spawn_job tool:
//
//	POST /v1/agents/jobs/{id}/progress
//	POST /v1/agents/jobs/{id}/complete
//
// Two callers post here:
//   - stream-task running inside the Job pod (success path) — direct
//     POST to goclaw via the Service ClusterIP.
//   - The agent service's k8s informer (failure / OOM safety net) —
//     synthetic POST to /complete with exit_code=137 etc.
//
// Both authenticate by HMAC-SHA256 over the body, signed with the
// shared `agent-github-webhook-secret` (same secret stream-review uses
// to PATCH GitHub check runs — keeps secret rotation a single event).
//
// Security guard: every request must reference an existing
// subagent_tasks row. Without that check, a leaked HMAC + a
// well-formed body could post arbitrary content to arbitrary Discord
// threads. With the row existence check, the attack surface is the
// (job_id, channel, thread_id) tuple goclaw itself wrote at spawn time.
type JobsHandler struct {
	taskStore  store.SubagentTaskStore
	hmacSecret []byte
	sender     ChannelSender
}

// ChannelSender posts a message to a named channel + chat. Implemented
// by *channels.Manager.SendToChannel — kept as an interface so this
// package doesn't pull the channel manager (avoids an import cycle
// with internal/agent which the channel manager already imports), and
// tests can swap in a fake.
type ChannelSender interface {
	SendToChannel(ctx context.Context, channelName, chatID, content string) error
}

// NewJobsHandler builds a JobsHandler. taskStore is required (rows
// are written by spawn_job and read here for the security guard
// + status updates). sender is required (we need to post to Discord).
// hmacSecret is required.
func NewJobsHandler(taskStore store.SubagentTaskStore, sender ChannelSender, hmacSecret []byte) *JobsHandler {
	return &JobsHandler{
		taskStore:  taskStore,
		sender:     sender,
		hmacSecret: hmacSecret,
	}
}

// RegisterRoutes wires the two endpoints onto the given mux. Mounted
// on the public gateway listener — Job pods reach it via the goclaw
// Service ClusterIP. HMAC + row-existence check are the only auth.
// Deliberately NOT using the gateway-token resolveAuth: stream-task
// runs in a pod that already has access to the webhook secret via the
// CSI mount, but plumbing the gateway token there would expand the
// in-Job attack surface for marginal benefit.
func (h *JobsHandler) RegisterRoutes(mux *http.ServeMux) {
	mux.HandleFunc("POST /v1/agents/jobs/{id}/progress", h.handleProgress)
	mux.HandleFunc("POST /v1/agents/jobs/{id}/complete", h.handleComplete)
}

// progressRequest is the body for /progress.
type progressRequest struct {
	Content  string `json:"content"`   // text to post into the thread
	Channel  string `json:"channel"`   // channel instance name (echoed from spawn_job)
	ThreadID string `json:"thread_id"` // Discord thread id
}

// completeRequest is the body for /complete. `source` distinguishes
// stream-task's real exit from the informer's synthetic exit so
// telemetry can split the two.
type completeRequest struct {
	ExitCode int    `json:"exit_code"`
	Result   string `json:"result"` // human-readable summary or final content
	Channel  string `json:"channel"`
	ThreadID string `json:"thread_id"`
	Source   string `json:"source,omitempty"` // "stream-task" | "informer"
}

func (h *JobsHandler) handleProgress(w http.ResponseWriter, r *http.Request) {
	jobID, body, ok := h.authAndReadBody(w, r)
	if !ok {
		return
	}

	var req progressRequest
	if err := json.Unmarshal(body, &req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid json: " + err.Error()})
		return
	}
	if req.Content == "" || req.Channel == "" || req.ThreadID == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "content, channel, thread_id all required"})
		return
	}

	// Row-existence guard. Tenant scope comes from the row itself.
	row := h.lookupRow(r.Context(), jobID)
	if row == nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "job_id not found"})
		return
	}
	// Only running jobs accept progress. Done/failed/interrupted means
	// the job is over and stray progress posts shouldn't reopen it.
	if row.Status != "running" {
		// 200 silent — race between stream-task's last progress and
		// the informer's complete, or a dead Job replaying. Don't
		// 4xx the caller.
		writeJSON(w, http.StatusOK, map[string]string{"status": "ignored", "reason": "job not running"})
		return
	}

	// Security: the channel+thread we post to MUST match what the row
	// was created with. Otherwise a leaked HMAC could redirect posts
	// to a different thread the agent never asked to write to.
	if !rowMatchesTarget(row, req.Channel, req.ThreadID) {
		slog.Warn("security.jobs_progress_target_mismatch",
			"job_id", jobID,
			"req_channel", req.Channel,
			"req_thread", req.ThreadID,
			"row_channel", strDeref(row.OriginChannel),
			"row_thread", strDeref(row.OriginChatID),
		)
		writeJSON(w, http.StatusForbidden, map[string]string{"error": "channel/thread does not match job"})
		return
	}

	if h.sender != nil {
		ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
		defer cancel()
		if err := h.sender.SendToChannel(ctx, req.Channel, req.ThreadID, req.Content); err != nil {
			// Don't fail the whole call — the row stays running, the
			// caller can retry, or the next progress post will land.
			slog.Warn("jobs_progress_send_failed", "job_id", jobID, "err", err)
		}
	}

	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (h *JobsHandler) handleComplete(w http.ResponseWriter, r *http.Request) {
	jobID, body, ok := h.authAndReadBody(w, r)
	if !ok {
		return
	}

	var req completeRequest
	if err := json.Unmarshal(body, &req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid json: " + err.Error()})
		return
	}
	if req.Channel == "" || req.ThreadID == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "channel, thread_id required"})
		return
	}

	row := h.lookupRow(r.Context(), jobID)
	if row == nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "job_id not found"})
		return
	}

	// Idempotency: if the row is already terminal, no-op. Handles the
	// race between stream-task's direct callback and the informer's
	// synthetic callback for the same Job.
	//
	// Exception: older goclaw builds could mark spawn_job rows as
	// "interrupted" during a goclaw restart even though the Kubernetes Job was
	// still running. Let the real Job completion repair those rows.
	if row.Status == "done" || row.Status == "failed" || (row.Status == "interrupted" && !isExternalSpawnJobTask(row)) {
		writeJSON(w, http.StatusOK, map[string]string{"status": "already_terminal", "previous": row.Status})
		return
	}

	if !rowMatchesTarget(row, req.Channel, req.ThreadID) {
		slog.Warn("security.jobs_complete_target_mismatch",
			"job_id", jobID,
			"req_channel", req.Channel,
			"req_thread", req.ThreadID,
			"row_channel", strDeref(row.OriginChannel),
			"row_thread", strDeref(row.OriginChatID),
		)
		writeJSON(w, http.StatusForbidden, map[string]string{"error": "channel/thread does not match job"})
		return
	}

	// Status mapping. Use the existing UpdateStatus surface so tenant
	// scope and timestamp logic stay in one place.
	status := "done"
	if req.ExitCode != 0 {
		status = "failed"
	}
	tenantCtx := store.WithTenantID(r.Context(), row.TenantID)
	if err := h.taskStore.UpdateStatus(tenantCtx, row.ID, status, ptrIfNonEmpty(req.Result), row.Iterations, row.InputTokens, row.OutputTokens); err != nil {
		// Don't 5xx if the DB write fails — we still want to post the
		// completion to the user. Log and continue.
		slog.Warn("jobs_complete_update_failed", "job_id", jobID, "err", err)
	}

	// Post final summary to the thread. Best-effort; if this fails the
	// row state still flipped, and the recovery loop won't re-open the
	// row (it filters by status='running').
	if h.sender != nil && req.Result != "" {
		ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
		defer cancel()
		summary := req.Result
		if req.ExitCode != 0 {
			summary = "❌ forge exited " + itoa(req.ExitCode) + "\n\n" + req.Result
		}
		if err := h.sender.SendToChannel(ctx, req.Channel, req.ThreadID, summary); err != nil {
			slog.Warn("jobs_complete_send_failed", "job_id", jobID, "err", err)
		}
	}

	slog.Info("jobs_complete",
		"job_id", jobID,
		"exit_code", req.ExitCode,
		"source", req.Source,
		"status", status,
	)
	writeJSON(w, http.StatusOK, map[string]string{"status": status})
}

// authAndReadBody verifies the HMAC, reads the body, and extracts the
// path's job_id. Returns (jobID, body, true) on success, or writes the
// appropriate error to w and returns ok=false. Reads the body ONCE —
// HMAC needs the bytes, then handlers parse them.
func (h *JobsHandler) authAndReadBody(w http.ResponseWriter, r *http.Request) (string, []byte, bool) {
	jobID := r.PathValue("id")
	if jobID == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "missing job id in path"})
		return "", nil, false
	}
	if _, err := uuid.Parse(jobID); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "job id is not a uuid"})
		return "", nil, false
	}

	body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, 1<<20))
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "read body: " + err.Error()})
		return "", nil, false
	}

	if !verifyHMAC(r.Header.Get("X-Hub-Signature-256"), body, h.hmacSecret) {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "invalid signature"})
		return "", nil, false
	}

	return jobID, body, true
}

// lookupRow runs Get with a master-scope context (tenant unset). The
// store's Get is tenant-scoped, but jobs callbacks come from outside
// the request lifecycle (a Job pod or the agent service informer), so
// we use master scope here. Tenant comes from the row itself for any
// downstream writes.
func (h *JobsHandler) lookupRow(ctx context.Context, jobID string) *store.SubagentTaskData {
	id, err := uuid.Parse(jobID)
	if err != nil {
		return nil
	}
	// We need the row regardless of tenant. Since the public Get is
	// tenant-scoped, do the equivalent unscoped read by setting a nil
	// tenant context — IsMasterScope returns true on uuid.Nil.
	masterCtx := store.WithTenantID(ctx, uuid.Nil)
	row, _ := h.taskStore.Get(masterCtx, id)
	return row
}

func verifyHMAC(header string, body, secret []byte) bool {
	const prefix = "sha256="
	if !strings.HasPrefix(header, prefix) {
		return false
	}
	gotHex := header[len(prefix):]
	got, err := hex.DecodeString(gotHex)
	if err != nil {
		return false
	}
	mac := hmac.New(sha256.New, secret)
	mac.Write(body)
	want := mac.Sum(nil)
	return hmac.Equal(got, want)
}

// rowMatchesTarget returns true if the row's origin matches the
// requested (channel, threadID). The HMAC + this check together prevent
// a leaked secret from being used to post to arbitrary threads.
func rowMatchesTarget(row *store.SubagentTaskData, channel, threadID string) bool {
	return strDeref(row.OriginChannel) == channel && strDeref(row.OriginChatID) == threadID
}

func strDeref(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}

func isExternalSpawnJobTask(row *store.SubagentTaskData) bool {
	if row == nil || len(row.Metadata) == 0 {
		return false
	}
	if metadataString(row.Metadata, "runner") == "spawn_job" {
		return true
	}
	if metadataString(row.Metadata, "k8s_job_name") != "" {
		return true
	}
	return metadataString(row.Metadata, "command") != "" &&
		metadataString(row.Metadata, "worktree_path") != "" &&
		row.Metadata["sinks"] != nil
}

func metadataString(metadata map[string]any, key string) string {
	if metadata == nil {
		return ""
	}
	if v, ok := metadata[key].(string); ok {
		return v
	}
	return ""
}

func ptrIfNonEmpty(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}

// itoa is the same as strconv.Itoa but kept inline to avoid an
// additional import for one call site. Trivial.
func itoa(n int) string {
	// stdlib strconv would be fine; this lets us stay zero-import.
	return jsonInt(n)
}

func jsonInt(n int) string {
	b, _ := json.Marshal(n)
	return string(b)
}
