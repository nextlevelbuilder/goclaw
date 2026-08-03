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

// ApprovalOutcome is a resolved decision plus, when the user denied, whatever
// they said to do instead.
//
// Reason exists because a bare "no" is a worse answer than a redirected one: the
// coding CLIs put the user's words in front of the model as the tool result, so
// "don't push, open a PR" keeps the turn going where a plain refusal makes the
// model guess. Only ever populated for a deny — an allow needs no justification,
// and carrying free text on the allow path would invite treating it as an
// instruction nobody audited.
type ApprovalOutcome struct {
	Decision ApprovalDecision
	Reason   string
}

// ToolNameExec is the ToolName used for approvals raised by the exec tool.
// Kept explicit so a payload always says which tool is asking, even for the
// original exec path whose requests predate the generalisation.
const ToolNameExec = "exec"

// ApprovalRequest describes one action awaiting a user's decision.
//
// It is deliberately tool-agnostic: `exec` fills Command with the shell command,
// while any other caller (e.g. a delegated CLI run) fills ToolName + Detail with
// a human-readable description of what would happen. UserID/TenantID are NOT
// cosmetic — they scope the WS event to the asking user (see
// internal/gateway/event_filter.go), so an approval never surfaces in someone
// else's dashboard.
type ApprovalRequest struct {
	ToolName string // "exec", "delegate_external", …
	Command  string // exec path: the shell command (empty for non-exec tools)
	Detail   string // human-readable "what would run" (defaults to Command)
	// Input is the tool's RAW arguments, so a client can render what the action
	// actually does rather than a one-line summary. For an Edit that means showing
	// the diff instead of only the file path — approving a change you cannot see is
	// a decision about WHERE, not WHAT.
	//
	// Bounded, not unbounded: see clampApprovalInput. A tool input can carry a
	// whole file, and this rides on a WS event to every client the approval is
	// visible to.
	Input      json.RawMessage
	AgentID    string
	SessionKey string
	UserID     string
	TenantID   uuid.UUID
}

// NewApprovalRequest seeds a request with the identity/scope fields carried on
// ctx, so every call site scopes its approval the same way.
func NewApprovalRequest(ctx context.Context, toolName, agentID string) ApprovalRequest {
	return ApprovalRequest{
		ToolName:   toolName,
		AgentID:    agentID,
		SessionKey: ToolSessionKeyFromCtx(ctx),
		UserID:     store.UserIDFromContext(ctx),
		TenantID:   store.TenantIDFromContext(ctx),
	}
}

// PendingApproval is an in-flight approval request.
//
// Command stays in place (and keeps its JSON name) for the exec path and its UI;
// ToolName/Detail/SessionKey/UserID generalise the record so a non-exec action
// can describe itself.
type PendingApproval struct {
	ID         string          `json:"id"`
	ToolName   string          `json:"toolName"`
	Command    string          `json:"command"`
	Detail     string          `json:"detail,omitempty"`
	Input      json.RawMessage `json:"input,omitempty"`
	AgentID    string          `json:"agentId"`
	SessionKey string          `json:"sessionKey,omitempty"`
	UserID     string          `json:"userId,omitempty"`
	CreatedAt  time.Time       `json:"createdAt"`

	tenantID uuid.UUID // event scope only — never serialised to clients
	resultCh chan ApprovalOutcome
}

// Wire renders the approval for the wire: the exec.approval.requested/resolved
// event payloads and the exec.approval.list response all use this one shape, so
// the UI describes a pending action the same way however it learned about it.
//
// `userId` MUST be present: the gateway event filter scopes exec.approval.*
// events by it and falls back to a tenant-wide broadcast when it is missing.
func (pa *PendingApproval) Wire() map[string]any {
	if pa == nil {
		return map[string]any{}
	}
	return map[string]any{
		"id":       pa.ID,
		"toolName": pa.ToolName,
		"command":  pa.Command,
		"detail":   pa.Detail,
		// Omitted entirely when absent or oversized, so a client can rely on
		// "present means renderable".
		"input":      inputOrNil(pa.Input),
		"agentId":    pa.AgentID,
		"sessionKey": pa.SessionKey,
		"userId":     pa.UserID,
		"createdAt":  pa.CreatedAt.UnixMilli(),
	}
}

// maxApprovalInputBytes bounds the raw tool input carried to clients.
//
// Generous enough for a real edit (a few hundred lines of before/after) and far
// below what a whole-file Write can be. Past it the input is DROPPED rather than
// truncated: half a JSON document is not renderable, and a diff cut off mid-hunk
// would misrepresent the change someone is approving — the summary in Detail is
// the honest fallback.
const maxApprovalInputBytes = 24 * 1024

// clampApprovalInput returns the input if it is a sane, bounded JSON object, else
// nil.
func clampApprovalInput(raw json.RawMessage) json.RawMessage {
	if len(raw) == 0 || len(raw) > maxApprovalInputBytes {
		return nil
	}
	if !json.Valid(raw) {
		return nil
	}
	return raw
}

func inputOrNil(raw json.RawMessage) any {
	if len(raw) == 0 {
		return nil
	}
	return raw
}

// ExecApprovalManager manages pending approval requests and the dynamic allowlist.
type ExecApprovalManager struct {
	config      ExecApprovalConfig
	pending     map[string]*PendingApproval
	alwaysAllow map[string]bool // patterns added via "allow-always" decisions
	mu          sync.Mutex
	nextID      int

	// eventBus pushes exec.approval.requested/resolved to connected clients so a
	// pending approval appears without a manual page reload. Nil-safe: a nil bus
	// (tests, embedded callers) simply means no live notification.
	eventBus bus.EventPublisher
}

// NewExecApprovalManager creates an approval manager with the given config.
func NewExecApprovalManager(cfg ExecApprovalConfig) *ExecApprovalManager {
	return &ExecApprovalManager{
		config:      cfg,
		pending:     make(map[string]*PendingApproval),
		alwaysAllow: make(map[string]bool),
	}
}

// SetEventBus wires the publisher used for approval events. Safe to call with
// nil (no live notifications) and safe to leave unset.
func (m *ExecApprovalManager) SetEventBus(pub bus.EventPublisher) {
	if m == nil {
		return
	}
	m.mu.Lock()
	m.eventBus = pub
	m.mu.Unlock()
}

// publish broadcasts an approval event, tenant-scoped so the gateway's
// fail-closed tenant filter delivers it. No-op when no bus is wired.
func (m *ExecApprovalManager) publish(name string, pa *PendingApproval, extra map[string]any) {
	if m == nil || pa == nil {
		return
	}
	m.mu.Lock()
	pub := m.eventBus
	m.mu.Unlock()
	if pub == nil {
		return
	}
	payload := pa.Wire()
	for k, v := range extra {
		payload[k] = v
	}
	pub.Broadcast(bus.Event{
		Name:     name,
		Payload:  payload,
		TenantID: pa.tenantID,
	})
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

// RequestApproval creates a pending approval and blocks until resolved or
// timeout, reporting only the decision. Callers that can act on a denial reason
// (the CLI relay, which hands it to the model) want RequestApprovalOutcome.
func (m *ExecApprovalManager) RequestApproval(req ApprovalRequest, timeout time.Duration) (ApprovalDecision, error) {
	outcome, err := m.RequestApprovalOutcome(req, timeout)
	return outcome.Decision, err
}

// RequestApprovalOutcome creates a pending approval and blocks until resolved or
// timeout.
//
// It publishes exec.approval.requested on entry and exec.approval.resolved on
// exit (including on timeout), so a connected dashboard shows the prompt live
// instead of only after a manual reload.
func (m *ExecApprovalManager) RequestApprovalOutcome(req ApprovalRequest, timeout time.Duration) (ApprovalOutcome, error) {
	if req.ToolName == "" {
		req.ToolName = ToolNameExec
	}
	if req.Detail == "" {
		req.Detail = req.Command
	}

	m.mu.Lock()
	m.nextID++
	id := fmt.Sprintf("%s-%d", ToolNameExec, m.nextID)
	pa := &PendingApproval{
		ID:         id,
		ToolName:   req.ToolName,
		Command:    req.Command,
		Detail:     req.Detail,
		AgentID:    req.AgentID,
		SessionKey: req.SessionKey,
		UserID:     req.UserID,
		CreatedAt:  time.Now(),
		tenantID:   req.TenantID,
		Input:      clampApprovalInput(req.Input),
		resultCh:   make(chan ApprovalOutcome, 1),
	}
	m.pending[id] = pa
	m.mu.Unlock()

	slog.Info("approval requested", "id", id, "tool", pa.ToolName, "detail", truncateCmd(pa.Detail, 100))
	m.publish(protocol.EventExecApprovalReq, pa, nil)

	// Wait for resolution or timeout
	select {
	case outcome := <-pa.resultCh:
		decision := outcome.Decision
		m.mu.Lock()
		delete(m.pending, id)
		m.mu.Unlock()

		// If allow-always, add the command's base binary to the dynamic allowlist.
		// Only meaningful for the exec path, where Command is a shell command.
		if decision == ApprovalAllowAlways && pa.Command != "" {
			bin := extractBin(pa.Command)
			if bin != "" {
				m.mu.Lock()
				m.alwaysAllow[bin] = true
				m.mu.Unlock()
				slog.Info("exec approval: added to always-allow", "bin", bin)
			}
		}

		return outcome, nil

	case <-time.After(timeout):
		m.mu.Lock()
		delete(m.pending, id)
		m.mu.Unlock()
		// Tell the UI the row is gone, otherwise a timed-out prompt lingers until
		// the next reload.
		m.publish(protocol.EventExecApprovalRes, pa, map[string]any{
			"decision": string(ApprovalDeny),
			"reason":   "timeout",
		})
		return ApprovalOutcome{Decision: ApprovalDeny}, fmt.Errorf("approval timed out after %s", timeout)
	}
}

// RequestExecApproval is the exec-shaped convenience wrapper: it keeps the
// original (command, agentID) call shape while filling the generalised fields
// from ctx.
func (m *ExecApprovalManager) RequestExecApproval(ctx context.Context, command, agentID string, timeout time.Duration) (ApprovalDecision, error) {
	req := NewApprovalRequest(ctx, ToolNameExec, agentID)
	req.Command = command
	req.Detail = command
	return m.RequestApproval(req, timeout)
}

// Resolve resolves a pending approval request.
func (m *ExecApprovalManager) Resolve(id string, decision ApprovalDecision) error {
	return m.ResolveWithReason(id, decision, "")
}

// ResolveWithReason resolves a pending approval, carrying the user's words back
// to whoever asked. reason is only meaningful on a deny (see ApprovalOutcome).
//
// The reason is NOT published on the resolved event: that event fans out to every
// client the approval is visible to, and a denial reason is a message aimed at
// the agent, not a broadcast. It reaches the model through resultCh alone.
func (m *ExecApprovalManager) ResolveWithReason(id string, decision ApprovalDecision, reason string) error {
	m.mu.Lock()
	pa, ok := m.pending[id]
	m.mu.Unlock()

	if !ok {
		return fmt.Errorf("approval %q not found or already resolved", id)
	}

	if decision != ApprovalDeny {
		reason = ""
	}

	// resultCh is buffered (cap 1) so this never blocks the caller; the waiting
	// RequestApproval goroutine removes the pending entry once it reads.
	pa.resultCh <- ApprovalOutcome{Decision: decision, Reason: reason}
	m.publish(protocol.EventExecApprovalRes, pa, map[string]any{"decision": string(decision)})
	return nil
}

// ListPending returns all pending approval requests, UNSCOPED. It is for
// in-process/admin use only. Do NOT serve it to a client: see ListPendingFor.
func (m *ExecApprovalManager) ListPending() []*PendingApproval {
	m.mu.Lock()
	defer m.mu.Unlock()

	result := make([]*PendingApproval, 0, len(m.pending))
	for _, pa := range m.pending {
		result = append(result, pa)
	}
	return result
}

// visibleTo reports whether a caller may see and answer this approval. An
// approval belongs to the user who triggered it; approvals raised with no user
// (cron, system exec) are visible to any caller in the same tenant, since
// otherwise nobody could ever answer them. Tenant must always match, and a
// zero-tenant caller can only see zero-tenant approvals — fail closed rather
// than letting an unauthenticated context match everything.
func (pa *PendingApproval) visibleTo(tenantID uuid.UUID, userID string) bool {
	if pa.tenantID != tenantID {
		return false
	}
	return pa.UserID == "" || pa.UserID == userID
}

// ListPendingFor returns only the approvals the caller may see. This is what a
// client-facing handler must use: the unscoped list would disclose OTHER users'
// pending actions, including the Detail describing exactly what is about to run.
func (m *ExecApprovalManager) ListPendingFor(tenantID uuid.UUID, userID string) []*PendingApproval {
	m.mu.Lock()
	defer m.mu.Unlock()

	result := make([]*PendingApproval, 0, len(m.pending))
	for _, pa := range m.pending {
		if pa.visibleTo(tenantID, userID) {
			result = append(result, pa)
		}
	}
	return result
}

// ResolveFor answers an approval only if the caller owns it.
//
// Approval ids are short and sequential ("exec-3"), so an id is not a secret and
// must never be the only thing standing between a caller and someone else's
// pending action. Without this check any operator-scoped user — in ANY tenant —
// could authorise code execution inside another user's session by guessing an id.
// A non-visible id reports the same "not found" as a missing one, so this cannot
// be used to probe which ids exist.
func (m *ExecApprovalManager) ResolveFor(id string, decision ApprovalDecision, tenantID uuid.UUID, userID string) error {
	return m.ResolveForWithReason(id, decision, "", tenantID, userID)
}

// ResolveForWithReason is ResolveFor carrying the user's redirect for a denial.
func (m *ExecApprovalManager) ResolveForWithReason(id string, decision ApprovalDecision, reason string, tenantID uuid.UUID, userID string) error {
	m.mu.Lock()
	pa, ok := m.pending[id]
	m.mu.Unlock()

	if !ok || !pa.visibleTo(tenantID, userID) {
		return fmt.Errorf("approval %q not found or already resolved", id)
	}
	return m.ResolveWithReason(id, decision, reason)
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
