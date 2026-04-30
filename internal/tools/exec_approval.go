package tools

import (
	"crypto/rand"
	"fmt"
	"log/slog"
	"math/big"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/nextlevelbuilder/goclaw/internal/bus"
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
	ShortCode string    `json:"shortCode"` // 6-char code for text-based approval
	Channel   string    `json:"channel"`   // originating channel name
	ChatID    string    `json:"chatId"`    // originating chat ID
	SenderID  string    `json:"senderId"`  // user who triggered the run
	TenantID  string    `json:"tenantId"`  // tenant scope
	resultCh  chan ApprovalDecision
}

// PendingApprovalSnapshot is a sanitized copy for event payloads (no channel field).
type PendingApprovalSnapshot struct {
	ID        string    `json:"id"`
	Command   string    `json:"command"`
	AgentID   string    `json:"agentId"`
	CreatedAt time.Time `json:"createdAt"`
	ShortCode string    `json:"shortCode"`
	Channel   string    `json:"channel"`
	ChatID    string    `json:"chatId"`
	SenderID  string    `json:"senderId"`
	TenantID  string    `json:"tenantId"`
}

// Snapshot returns a sanitized copy of the approval for event payloads.
func (pa *PendingApproval) Snapshot() PendingApprovalSnapshot {
	return PendingApprovalSnapshot{
		ID:        pa.ID,
		Command:   pa.Command,
		AgentID:   pa.AgentID,
		CreatedAt: pa.CreatedAt,
		ShortCode: pa.ShortCode,
		Channel:   pa.Channel,
		ChatID:    pa.ChatID,
		SenderID:  pa.SenderID,
		TenantID:  pa.TenantID,
	}
}

// ApprovalContext carries channel routing info for approval notifications.
type ApprovalContext struct {
	Channel  string
	ChatID   string
	SenderID string
	TenantID string
}

// shortCodeCharset excludes I, O, 0, 1 to avoid visual confusion.
const shortCodeCharset = "ABCDEFGHJKLMNPQRSTUVWXYZ23456789"

func generateShortCode() string {
	code := make([]byte, 6)
	max := big.NewInt(int64(len(shortCodeCharset)))
	for i := range code {
		n, _ := rand.Int(rand.Reader, max)
		code[i] = shortCodeCharset[n.Int64()]
	}
	return string(code)
}

// ExecApprovalManager manages pending approval requests and the dynamic allowlist.
type ExecApprovalManager struct {
	config          ExecApprovalConfig
	pending         map[string]*PendingApproval
	shortCodeIndex  map[string]string // shortCode → approval ID
	alwaysAllow     map[string]bool   // patterns added via "allow-always" decisions
	mu              sync.Mutex
	nextID          int
	msgBus          *bus.MessageBus
}

// NewExecApprovalManager creates an approval manager with the given config.
func NewExecApprovalManager(cfg ExecApprovalConfig) *ExecApprovalManager {
	return &ExecApprovalManager{
		config:         cfg,
		pending:        make(map[string]*PendingApproval),
		shortCodeIndex: make(map[string]string),
		alwaysAllow:    make(map[string]bool),
	}
}

// SetMessageBus wires the message bus for broadcasting approval events.
func (m *ExecApprovalManager) SetMessageBus(b *bus.MessageBus) {
	m.msgBus = b
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
func (m *ExecApprovalManager) RequestApproval(command, agentID string, timeout time.Duration, approvalCtx ...ApprovalContext) (ApprovalDecision, error) {
	m.mu.Lock()
	m.nextID++
	id := fmt.Sprintf("exec-%d", m.nextID)

	// Generate a unique short code
	shortCode := generateShortCode()
	for m.shortCodeIndex[shortCode] != "" {
		shortCode = generateShortCode()
	}

	var ctx ApprovalContext
	if len(approvalCtx) > 0 {
		ctx = approvalCtx[0]
	}

	pa := &PendingApproval{
		ID:        id,
		Command:   command,
		AgentID:   agentID,
		CreatedAt: time.Now(),
		ShortCode: shortCode,
		Channel:   ctx.Channel,
		ChatID:    ctx.ChatID,
		SenderID:  ctx.SenderID,
		TenantID:  ctx.TenantID,
		resultCh:  make(chan ApprovalDecision, 1),
	}
	m.pending[id] = pa
	m.shortCodeIndex[shortCode] = id
	m.mu.Unlock()

	slog.Info("exec approval requested", "id", id, "shortCode", shortCode, "command", truncateCmd(command, 100), "channel", ctx.Channel, "chatId", ctx.ChatID)

	// Broadcast the approval request event for channel notification
	if m.msgBus != nil && ctx.Channel != "" {
		m.msgBus.Broadcast(bus.Event{
			Name:    protocol.EventExecApprovalReq,
			Payload: pa.Snapshot(),
		})
	}

	// Wait for resolution or timeout
	select {
	case decision := <-pa.resultCh:
		m.mu.Lock()
		delete(m.pending, id)
		delete(m.shortCodeIndex, shortCode)
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
		delete(m.shortCodeIndex, shortCode)
		m.mu.Unlock()
		return ApprovalDeny, fmt.Errorf("approval timed out after %s", timeout)
	}
}

// Resolve resolves a pending approval request.
func (m *ExecApprovalManager) Resolve(id string, decision ApprovalDecision) error {
	m.mu.Lock()

	pa, ok := m.pending[id]
	if !ok {
		m.mu.Unlock()
		return fmt.Errorf("approval %q not found or already resolved", id)
	}

	delete(m.shortCodeIndex, pa.ShortCode)
	pa.resultCh <- decision
	m.mu.Unlock()
	return nil
}

// ResolveByShortCode resolves a pending approval using its short code.
func (m *ExecApprovalManager) ResolveByShortCode(code string, decision ApprovalDecision) error {
	m.mu.Lock()
	id, ok := m.shortCodeIndex[code]
	if !ok {
		m.mu.Unlock()
		return fmt.Errorf("approval code %q not found or already resolved", code)
	}
	pa, ok := m.pending[id]
	if !ok {
		m.mu.Unlock()
		return fmt.Errorf("approval code %q not found or already resolved", code)
	}
	delete(m.shortCodeIndex, code)
	pa.resultCh <- decision
	m.mu.Unlock()
	return nil
}

// ResolveByChannel resolves the most recent pending approval for a channel+chatID pair.
// Used when users reply with simple "1" (approve) or "2" (deny) without a short code.
// Returns the short code and truncated command of the resolved approval.
func (m *ExecApprovalManager) ResolveByChannel(channel, chatID string, decision ApprovalDecision) (string, string, error) {
	m.mu.Lock()
	var match *PendingApproval
	for _, pa := range m.pending {
		if pa.Channel == channel && pa.ChatID == chatID {
			if match == nil || pa.CreatedAt.After(match.CreatedAt) {
				match = pa
			}
		}
	}
	if match == nil {
		m.mu.Unlock()
		return "", "", fmt.Errorf("no pending approval found for this chat")
	}
	shortCode := match.ShortCode
	cmd := truncateCmd(match.Command, 80)
	delete(m.shortCodeIndex, match.ShortCode)
	match.resultCh <- decision
	m.mu.Unlock()
	return shortCode, cmd, nil
}

// ListPendingByChannel returns all pending approvals for a given channel+chatID pair,
// sorted by creation time (oldest first).
func (m *ExecApprovalManager) ListPendingByChannel(channel, chatID string) []*PendingApproval {
	m.mu.Lock()
	defer m.mu.Unlock()

	var result []*PendingApproval
	for _, pa := range m.pending {
		if pa.Channel == channel && pa.ChatID == chatID {
			result = append(result, pa)
		}
	}
	sort.Slice(result, func(i, j int) bool {
		return result[i].CreatedAt.Before(result[j].CreatedAt)
	})
	return result
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
