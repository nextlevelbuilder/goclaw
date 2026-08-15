package tools

import (
	"fmt"
	"log/slog"
	"path/filepath"
	"strings"
	"sync"
	"time"
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
	resultCh  chan ApprovalDecision
}

// ExecApprovalManager manages pending approval requests and the dynamic allowlist.
type ExecApprovalManager struct {
	config       ExecApprovalConfig
	pending      map[string]*PendingApproval
	alwaysAllow  map[string]bool // patterns added via "allow-always" decisions
	mu           sync.Mutex
	nextID       int
}

// NewExecApprovalManager creates an approval manager with the given config.
func NewExecApprovalManager(cfg ExecApprovalConfig) *ExecApprovalManager {
	return &ExecApprovalManager{
		config:      cfg,
		pending:     make(map[string]*PendingApproval),
		alwaysAllow: make(map[string]bool),
	}
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
func (m *ExecApprovalManager) RequestApproval(command, agentID string, timeout time.Duration) (ApprovalDecision, error) {
	m.mu.Lock()
	m.nextID++
	id := fmt.Sprintf("exec-%d", m.nextID)
	pa := &PendingApproval{
		ID:        id,
		Command:   command,
		AgentID:   agentID,
		CreatedAt: time.Now(),
		resultCh:  make(chan ApprovalDecision, 1),
	}
	m.pending[id] = pa
	m.mu.Unlock()

	slog.Info("exec approval requested", "id", id, "command", truncateCmd(command, 100))

	// Wait for resolution or timeout
	select {
	case decision := <-pa.resultCh:
		m.mu.Lock()
		delete(m.pending, id)
		m.mu.Unlock()

		// If allow-always, add the command's base binary to the dynamic allowlist
		if decision == ApprovalAllowAlways {
			bin := extractBin(command)
			if bin != "" {
				m.mu.Lock()
				m.alwaysAllow[bin] = true
				m.mu.Unlock()
				slog.Info("exec approval: added to always-allow", "bin", bin)
			}
		}

		return decision, nil

	case <-time.After(timeout):
		m.mu.Lock()
		delete(m.pending, id)
		m.mu.Unlock()
		return ApprovalDeny, fmt.Errorf("approval timed out after %s", timeout)
	}
}

// Resolve resolves a pending approval request.
func (m *ExecApprovalManager) Resolve(id string, decision ApprovalDecision) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	pa, ok := m.pending[id]
	if !ok {
		return fmt.Errorf("approval %q not found or already resolved", id)
	}

	pa.resultCh <- decision
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
