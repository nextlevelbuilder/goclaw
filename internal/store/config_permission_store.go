package store

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
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
// Fail-open: returns nil on DB errors or missing context (cron, subagent).
// Replaces the deleted CheckGroupWritePermission / GroupWriterCache.
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
	senderID := SenderIDFromContext(ctx)
	if senderID == "" {
		return nil // system context (cron, subagent)
	}
	numericID := strings.SplitN(senderID, "|", 2)[0]

	// Use ListFileWriters (purpose-built for this check, already used by /addwriter)
	writers, err := permStore.ListFileWriters(ctx, agentID, userID)

	// DEBUG: log all runtime values so we can compare against the DB grant row.
	// Remove this block once root cause is confirmed.
	writerIDs := make([]string, len(writers))
	for i, w := range writers {
		writerIDs[i] = w.UserID
	}
	slog.Debug("debug.file_writer_permission_check",
		"scope", userID,
		"agent_id", agentID,
		"sender_id", senderID,
		"numeric_id", numericID,
		"list_err", err,
		"writer_count", len(writers),
		"writer_ids", writerIDs,
	)

	if err != nil {
		return nil // fail-open
	}

	// Check if sender is in the writer allowlist
	for _, w := range writers {
		if w.UserID == numericID && w.Permission == "allow" {
			return nil // found matching writer grant
		}
	}

	return fmt.Errorf("permission denied: only file writers can modify files in this group. Use /addwriter to get write access")
}

// CheckCronPermission returns an error if the caller is in a group context
// and does not have cron or file_writer permission. Returns nil if allowed.
// Fail-open: returns nil on DB errors or missing context (cron, subagent).
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
	senderID := SenderIDFromContext(ctx)
	if senderID == "" {
		return nil // system context (cron, subagent)
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
