package store

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
)

// Config type constants for agent_config_permissions.config_type column.
const (
	ConfigTypeFileWriter = "file_writer" // Group file write access
	ConfigTypeHeartbeat  = "heartbeat"   // Heartbeat config access
	ConfigTypeCron       = "cron"        // Cron job management access
)

// ConfigPermission represents an allow/deny rule for agent configuration.
type ConfigPermission struct {
	ID         uuid.UUID       `json:"id" db:"id"`
	AgentID    uuid.UUID       `json:"agentId" db:"agent_id"`
	Scope      string          `json:"scope" db:"scope"`           // "agent" | "group:telegram:-100456" | "group:*" | "*"
	ConfigType string          `json:"configType" db:"config_type"` // "heartbeat" | "cron" | "context_files" | "file_writer" | "*"
	UserID     string          `json:"userId" db:"user_id"`
	Permission string          `json:"permission" db:"permission"` // "allow" | "deny"
	GrantedBy  *string         `json:"grantedBy,omitempty" db:"granted_by"`
	Metadata   json.RawMessage `json:"metadata,omitempty" db:"metadata"`
	CreatedAt  time.Time       `json:"createdAt" db:"created_at"`
	UpdatedAt  time.Time       `json:"updatedAt" db:"updated_at"`
}

// ConfigPermissionStore manages agent configuration permissions with wildcard scope matching.
type ConfigPermissionStore interface {
	// CheckPermission checks if a user has permission for a given config action.
	// Evaluates deny rules first, then allow rules, using Go-level wildcard matching.
	CheckPermission(ctx context.Context, agentID uuid.UUID, scope, configType, userID string) (bool, error)

	Grant(ctx context.Context, perm *ConfigPermission) error
	Revoke(ctx context.Context, agentID uuid.UUID, scope, configType, userID string) error
	// List returns permissions for agentID+configType. If scope != "" only rows with that scope are returned.
	List(ctx context.Context, agentID uuid.UUID, configType, scope string) ([]ConfigPermission, error)
	// ListFileWriters returns cached file_writer allow permissions for a given agentID+scope (hot-path).
	ListFileWriters(ctx context.Context, agentID uuid.UUID, scope string) ([]ConfigPermission, error)
}

// CheckFileWriterPermission returns an error if the caller is in a group context
// and is not a file writer. Returns nil if write is allowed.
//
// Policy in group/guild context (#915):
//   - empty SenderID        → DENY  (system turn lost the real user — security gap if allowed)
//   - synthetic SenderID    → DENY  (subagent:, notification:, teammate:, system:, ticker:, session_send_tool)
//   - real numeric SenderID → DB lookup; deny if no grant
//   - DB errors             → fail-open (preserve availability over strictness)
//
// Outside group/guild context (DM, HTTP, cron-direct): always allow — no per-user
// writer gate applies.
//
// Upstream callers should propagate the real acting sender through wrappers
// (subagent announce, delegate announce, teammate dispatch) so legitimate
// re-ingress turns carry the original user's sender.
func CheckFileWriterPermission(ctx context.Context, permStore ConfigPermissionStore) error {
	if permStore == nil {
		return nil
	}
	userID := UserIDFromContext(ctx)
	if !strings.HasPrefix(userID, "group:") && !strings.HasPrefix(userID, "guild:") {
		return nil // not a group context
	}
	agentID := AgentIDFromContext(ctx)
	if agentID == uuid.Nil {
		return nil // no agent context
	}
	// RBAC bypass: admin / operator / owner roles are pre-authenticated by
	// the tenant RBAC system (dashboard users, tenant admins). File-writer
	// grants exist to gate random group members; authenticated admins
	// shouldn't trip over them when dispatching work that writes files.
	if isAdminRole(ctx) {
		return nil
	}
	senderID := SenderIDFromContext(ctx)
	if senderID == "" || isSyntheticSender(senderID) {
		return fmt.Errorf("permission denied: system context cannot write files in group chats. If this is a legitimate user action, ensure the acting sender is preserved through the tool chain")
	}
	numericID := strings.SplitN(senderID, "|", 2)[0]
	allowed, err := permStore.CheckPermission(ctx, agentID, userID, ConfigTypeFileWriter, numericID)
	if err != nil {
		return nil // fail-open on DB error only (availability)
	}
	if !allowed {
		return fmt.Errorf("permission denied: only file writers can modify files in this group. Use /addwriter to get write access")
	}
	return nil
}

// isAdminRole reports whether ctx carries an elevated RBAC role
// (admin / operator / owner) that should bypass per-user file-writer
// grants. Tenant-authenticated identities pre-pass RBAC at the gateway
// edge; re-checking per-channel grants here is redundant and blocks
// legitimate dashboard-dispatched work (#915).
func isAdminRole(ctx context.Context) bool {
	switch RoleFromContext(ctx) {
	case "admin", "operator", RoleOwner:
		return true
	}
	return false
}

// isSyntheticSender reports whether senderID is an internal system component
// (not a real user). Mirrors bus.IsInternalSender — kept here to avoid the
// store→bus import dependency. If prefixes change, update both.
func isSyntheticSender(senderID string) bool {
	return strings.HasPrefix(senderID, "system:") ||
		strings.HasPrefix(senderID, "notification:") ||
		strings.HasPrefix(senderID, "teammate:") ||
		strings.HasPrefix(senderID, "ticker:") ||
		strings.HasPrefix(senderID, "subagent:") ||
		senderID == "session_send_tool"
}

// CheckCronPermission returns an error if the caller is in a group context
// and does not have cron or file_writer permission. Returns nil if allowed.
// Same sender-policy as CheckFileWriterPermission (see that fn's docstring).
func CheckCronPermission(ctx context.Context, permStore ConfigPermissionStore) error {
	if permStore == nil {
		return nil
	}
	userID := UserIDFromContext(ctx)
	if !strings.HasPrefix(userID, "group:") && !strings.HasPrefix(userID, "guild:") {
		return nil // not a group context
	}
	agentID := AgentIDFromContext(ctx)
	if agentID == uuid.Nil {
		return nil // no agent context
	}
	if isAdminRole(ctx) {
		return nil // RBAC bypass (admin/operator/owner)
	}
	senderID := SenderIDFromContext(ctx)
	if senderID == "" || isSyntheticSender(senderID) {
		return fmt.Errorf("permission denied: system context cannot manage cron jobs in group chats")
	}
	numericID := strings.SplitN(senderID, "|", 2)[0]

	// Check cron-specific permission first.
	allowed, err := permStore.CheckPermission(ctx, agentID, userID, ConfigTypeCron, numericID)
	if err != nil {
		return nil // fail-open
	}
	if allowed {
		return nil
	}
	// Fall back to file_writer (implies full mutation access).
	allowed, err = permStore.CheckPermission(ctx, agentID, userID, ConfigTypeFileWriter, numericID)
	if err != nil {
		return nil // fail-open
	}
	if !allowed {
		return fmt.Errorf("permission denied: only users with cron or file_writer permission can manage cron jobs in group chats")
	}
	return nil
}
