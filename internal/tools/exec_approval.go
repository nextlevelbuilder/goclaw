package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/nextlevelbuilder/goclaw/internal/bus"
	"github.com/nextlevelbuilder/goclaw/internal/store"
	"github.com/nextlevelbuilder/goclaw/pkg/protocol"
)

// ExecSecurity determines the overall security mode for command execution.
type ExecSecurity string

const (
	// ExecSecurityDeny blocks all commands (no exec tool available).
	ExecSecurityDeny ExecSecurity = "deny"

	// ExecSecurityAllowlist only allows commands matching the allowlist.
	ExecSecurityAllowlist ExecSecurity = "allowlist"

	// ExecSecurityFull allows all commands (ask mode still applies).
	ExecSecurityFull ExecSecurity = "full"
)

// ExecAskMode determines when to prompt for user approval.
type ExecAskMode string

const (
	// ExecAskOff never asks — commands are auto-approved.
	ExecAskOff ExecAskMode = "off"

	// ExecAskOnMiss asks only when a command is not in the allowlist.
	ExecAskOnMiss ExecAskMode = "on-miss"

	// ExecAskAlways asks for every command execution.
	ExecAskAlways ExecAskMode = "always"
)

// ExecApprovalConfig configures command execution approval.
type ExecApprovalConfig struct {
	Security  ExecSecurity `json:"security"`  // "deny", "allowlist", "full" (default "full")
	Ask       ExecAskMode  `json:"ask"`       // "off", "on-miss", "always" (default "off")
	Allowlist []string     `json:"allowlist"` // glob patterns for allowed commands
}

// DefaultExecApprovalConfig returns the default (permissive) config.
func DefaultExecApprovalConfig() ExecApprovalConfig {
	return ExecApprovalConfig{
		Security: ExecSecurityFull,
		Ask:      ExecAskOff,
	}
}

// safeBins are command names that are always considered safe.
// Only includes read-only, text processing, and dev tools.
// Infrastructure/network tools (docker, kubectl, terraform, ansible,
// curl, wget, ssh, scp, rsync) are excluded — they require approval
// when ask mode is "on-miss".
var safeBins = map[string]bool{
	// Read-only / info tools
	"cat": true, "echo": true, "ls": true, "pwd": true, "head": true,
	"tail": true, "wc": true, "sort": true, "uniq": true, "grep": true,
	"find": true, "which": true, "whoami": true, "date": true,
	"uname": true, "hostname": true,
	"df": true, "du": true, "free": true, "uptime": true, "file": true,
	"stat": true, "dirname": true, "basename": true, "realpath": true,
	// Text processing
	"jq": true, "yq": true, "sed": true, "awk": true, "tr": true,
	"cut": true, "diff": true, "patch": true, "tee": true, "xargs": true,
	// Dev tools (core purpose of a coding agent)
	"git": true, "node": true, "npm": true, "npx": true, "yarn": true,
	"pnpm": true, "bun": true, "deno": true, "python": true, "python3": true,
	"pip": true, "pip3": true, "go": true, "cargo": true, "rustc": true,
	"make": true, "cmake": true, "gcc": true, "g++": true, "clang": true,
	"java": true, "javac": true, "mvn": true, "gradle": true,
}

// ApprovalDecision is the user's response to an approval request.
type ApprovalDecision string

const (
	ApprovalAllowOnce   ApprovalDecision = "allow-once"
	ApprovalAllowAlways ApprovalDecision = "allow-always"
	ApprovalDeny        ApprovalDecision = "deny"
)

// PendingApproval is an in-flight approval request.
type PendingApproval struct {
	ID        string    `json:"id"`
	Command   string    `json:"command"`
	AgentID   string    `json:"agentId"`
	CreatedAt time.Time `json:"createdAt"`
	TenantID  uuid.UUID `json:"-"` // tenant scope; guards cross-tenant resolves
	resultCh  chan ApprovalDecision
}

// ExecApprovalManager manages pending approval requests and the dynamic allowlist.
// Persistence is best-effort: the manager keeps an in-memory fast path and
// mirrors every transition into the approval store when one is wired. A store
// write never blocks command execution — failures are logged and dropped.
type ExecApprovalManager struct {
	config       ExecApprovalConfig
	pending      map[string]*PendingApproval
	alwaysAllow  map[string]bool // patterns added via "allow-always" decisions
	durable      map[string]uuid.UUID // in-memory id → persisted row UUID
	mu           sync.Mutex
	nextID       int
	approvalStore store.ApprovalStore // optional; nil = in-memory only
	msgBus        bus.EventPublisher  // optional; nil = no push notifications
}

// NewExecApprovalManager creates an approval manager with the given config.
func NewExecApprovalManager(cfg ExecApprovalConfig) *ExecApprovalManager {
	return &ExecApprovalManager{
		config:      cfg,
		pending:     make(map[string]*PendingApproval),
		alwaysAllow: make(map[string]bool),
		durable:     make(map[string]uuid.UUID),
	}
}

// SetApprovalStore wires an optional durable store. Persist is best-effort:
// when the store is present, every request/resolve/timeout is mirrored in the
// background so the queue survives restarts; store errors never block exec.
func (m *ExecApprovalManager) SetApprovalStore(s store.ApprovalStore) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.approvalStore = s
}

// SetEventBus wires an optional event publisher for push notifications.
func (m *ExecApprovalManager) SetEventBus(pub bus.EventPublisher) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.msgBus = pub
}

// CheckCommand evaluates whether a command should be executed, blocked, or needs approval.
// Returns: "allow", "deny", or "ask".
func (m *ExecApprovalManager) CheckCommand(command string) string {
	switch m.config.Security {
	case ExecSecurityDeny:
		return "deny"

	case ExecSecurityAllowlist:
		if m.matchesAllowlist(command) {
			if m.config.Ask == ExecAskAlways {
				return "ask"
			}
			return "allow"
		}
		if m.config.Ask == ExecAskOff {
			return "deny" // not in allowlist, no asking
		}
		return "ask"

	case ExecSecurityFull:
		switch m.config.Ask {
		case ExecAskOff:
			return "allow"
		case ExecAskAlways:
			return "ask"
		case ExecAskOnMiss:
			if m.matchesAllowlist(command) || m.isSafeBin(command) {
				return "allow"
			}
			return "ask"
		}
	}

	return "allow"
}

// RequestApproval creates a pending approval and blocks until resolved or timeout.
// The context carries the tenant scope for persistence and event routing. The
// in-memory fast path stays authoritative for the block/wait; persistence is a
// best-effort background mirror with non-blocking failure handling.
func (m *ExecApprovalManager) RequestApproval(ctx context.Context, command, agentID string, timeout time.Duration) (ApprovalDecision, error) {
	m.mu.Lock()
	m.nextID++
	id := fmt.Sprintf("exec-%d", m.nextID)
	pa := &PendingApproval{
		ID:        id,
		Command:   command,
		AgentID:   agentID,
		CreatedAt: time.Now(),
		TenantID:  store.TenantIDFromContext(ctx),
		resultCh:  make(chan ApprovalDecision, 1),
	}
	m.pending[id] = pa
	st := m.approvalStore
	pub := m.msgBus
	m.mu.Unlock()

	slog.Info("exec approval requested", "id", id, "command", truncateCmd(command, 100))

	// Persist best-effort in a background goroutine: a DB hiccup must never
	// block command execution or leave the in-memory state half-updated.
	if st != nil {
		go func() {
			durableID, err := persistApprovalRequest(ctx, st, command, agentID, timeout)
			if err != nil {
				slog.Warn("exec approval: persist request failed (non-blocking)", "id", id, "err", err)
				return
			}
			m.mu.Lock()
			m.durable[id] = durableID
			m.mu.Unlock()
		}()
	}

	// Broadcast push notification to the tenant's clients.
	if pub != nil {
		tid := store.TenantIDFromContext(ctx)
		uid := store.UserIDFromContext(ctx)
		pub.Broadcast(bus.Event{
			Name:     protocol.EventExecApprovalReq,
			TenantID: tid,
			Payload: map[string]any{
				"id":        id,
				"command":   truncateCmd(command, 100),
				"agentId":   agentID,
				"createdAt": pa.CreatedAt.UnixMilli(),
				"userId":    uid,
			},
		})
	}

	// Wait for resolution or timeout. The authoritative resolve/broadcast
	// happens in Resolve (the WS handler) — this goroutine only continues the
	// exec once a decision has been granted.
	select {
	case decision := <-pa.resultCh:
		m.mu.Lock()
		delete(m.pending, id)
		delete(m.durable, id)
		if decision == ApprovalAllowAlways {
			bin := extractBin(command)
			if bin != "" {
				m.alwaysAllow[bin] = true
				slog.Info("exec approval: added to always-allow", "bin", bin)
			}
		}
		m.mu.Unlock()
		return decision, nil

	case <-time.After(timeout):
		m.mu.Lock()
		delete(m.pending, id)
		durID := m.durable[id]
		delete(m.durable, id)
		m.mu.Unlock()
		go m.markExpiredBestEffort(ctx, st, id, durID, pub)
		return ApprovalDeny, ErrApprovalTimedOut
	}
}

// persistApprovalRequest writes a pending row. The tenant comes from the
// context; rows never leak across tenants. Returns the persisted row UUID so
// the caller can route future resolve/expire operations at it.
func persistApprovalRequest(ctx context.Context, st store.ApprovalStore, command, agentID string, timeout time.Duration) (uuid.UUID, error) {
	tid := store.TenantIDFromContext(ctx)
	req := &store.ApprovalRequest{
		TenantID:       tid,
		ActionType:     "exec",
		Payload:        json.RawMessage(`{"command":` + marshalJSONString(command) + `}`),
		Command:        command,
		Status:         store.ApprovalStatusPending,
		TimeoutSeconds: int(timeout.Seconds()),
	}
	if req.TimeoutSeconds <= 0 {
		req.TimeoutSeconds = 120
	}
	// Record the deadline so operators auditing the queue see exactly how long a
	// request stays resolvable (mirrors the in-memory timeout in RequestApproval).
	expiresAt := time.Now().Add(time.Duration(req.TimeoutSeconds) * time.Second)
	req.ExpiredAt = &expiresAt
	if parsed, err := uuid.Parse(agentID); err == nil {
		req.AgentID = &parsed
	}
	req.RequesterType = "agent"
	if err := st.CreateRequest(ctx, req); err != nil {
		return uuid.Nil, err
	}
	return req.ID, nil
}

// markExpiredBestEffort transitions the persisted row to expired and notifies
// clients that the request is closed.
func (m *ExecApprovalManager) markExpiredBestEffort(ctx context.Context, st store.ApprovalStore, id string, durableID uuid.UUID, pub bus.EventPublisher) {
	if st != nil && durableID != uuid.Nil {
		if err := st.MarkExpired(ctx, durableID); err != nil {
			slog.Warn("exec approval: mark expired failed (non-blocking)", "id", id, "err", err)
		}
	}
	if pub != nil {
		tid := store.TenantIDFromContext(ctx)
		pub.Broadcast(bus.Event{
			Name:     protocol.EventExecApprovalRes,
			TenantID: tid,
			Payload: map[string]any{
				"id":        id,
				"decision":  "timeout",
				"status":    store.ApprovalStatusExpired,
				"userId":    store.UserIDFromContext(ctx),
				"tenantId":  tid.String(),
			},
		})
	}
}

// ErrApprovalTimedOut is returned when an approval waits too long for a decision.
var ErrApprovalTimedOut = fmt.Errorf("approval timed out")

// ErrApprovalNotFound is returned when no in-flight approval matches the id.
var ErrApprovalNotFound = fmt.Errorf("approval not found or already resolved")

// Resolve resolves a pending approval request.
//
// ctx carries the tenant scope (WS handler context) so cross-tenant resolves
// are refused. The decidedBy actor is recorded on the persisted row. The
// in-memory decision is delivered to the blocked exec; when a durable row
// exists it is transitioned best-effort and clients get a push notification.
func (m *ExecApprovalManager) Resolve(ctx context.Context, id string, decision ApprovalDecision, decidedBy *uuid.UUID) error {
	m.mu.Lock()
	pa, ok := m.pending[id]
	if !ok {
		m.mu.Unlock()
		return ErrApprovalNotFound
	}
	// Tenant guard: the caller must share the requesting tenant.
	tid := store.TenantIDFromContext(ctx)
	if tid != uuid.Nil && pa.TenantID != uuid.Nil && tid != pa.TenantID {
		m.mu.Unlock()
		return fmt.Errorf("approval %q belongs to another tenant", id)
	}
	durID := m.durable[id]
	st := m.approvalStore
	pub := m.msgBus
	m.mu.Unlock()

	// Deliver the decision to the blocked exec.
	pa.resultCh <- decision

	// Persist best-effort (log + drop on error; never block the WS reply).
	if st != nil && durID != uuid.Nil {
		go func() {
			allowOnce := decision == ApprovalAllowOnce
			allowAlways := decision == ApprovalAllowAlways
			if err := st.Resolve(ctx, durID, string(decision), decidedBy, allowOnce, allowAlways); err != nil {
				slog.Warn("exec approval: persist resolve failed (non-blocking)", "id", id, "err", err)
			}
		}()
	}
	if pub != nil {
		tid := store.TenantIDFromContext(ctx)
		status := store.ApprovalStatusDenied
		if decision != ApprovalDeny {
			status = store.ApprovalStatusApproved
		}
		pub.Broadcast(bus.Event{
			Name:     protocol.EventExecApprovalRes,
			TenantID: tid,
			Payload: map[string]any{
				"id":       id,
				"decision": string(decision),
				"status":   status,
				"userId":   store.UserIDFromContext(ctx),
			},
		})
	}
	return nil
}

// ListPending returns all pending approval requests.
func (m *ExecApprovalManager) ListPending() []*PendingApproval {
	m.mu.Lock()
	defer m.mu.Unlock()

	result := make([]*PendingApproval, 0, len(m.pending))
	for _, pa := range m.pending {
		result = append(result, pa)
	}
	return result
}

// matchesAllowlist checks if a command matches any allowlist pattern or dynamic always-allow.
func (m *ExecApprovalManager) matchesAllowlist(command string) bool {
	bin := extractBin(command)

	// Check dynamic always-allow
	m.mu.Lock()
	if m.alwaysAllow[bin] {
		m.mu.Unlock()
		return true
	}
	m.mu.Unlock()

	// Check static allowlist patterns
	for _, pattern := range m.config.Allowlist {
		if matched, _ := filepath.Match(pattern, bin); matched {
			return true
		}
		// Also match against full command
		if matched, _ := filepath.Match(pattern, command); matched {
			return true
		}
	}

	return false
}

// isSafeBin checks if the command's base binary is in the safe list.
func (m *ExecApprovalManager) isSafeBin(command string) bool {
	return safeBins[extractBin(command)]
}

// extractBin returns the first word of a command (the binary name).
func extractBin(command string) string {
	command = strings.TrimSpace(command)
	// Skip env var assignments like FOO=bar cmd
	for strings.Contains(command, "=") {
		parts := strings.SplitN(command, " ", 2)
		if !strings.Contains(parts[0], "=") {
			break
		}
		if len(parts) < 2 {
			return ""
		}
		command = strings.TrimSpace(parts[1])
	}

	fields := strings.Fields(command)
	if len(fields) == 0 {
		return ""
	}
	return filepath.Base(fields[0])
}

func truncateCmd(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "..."
}

// marshalJSONString returns the JSON string literal for s (with quotes and
// escaping), safe to embed in a JSON payload document without double-encoding.
func marshalJSONString(s string) string {
	b, err := json.Marshal(s)
	if err != nil {
		return `""`
	}
	return string(b)
}
