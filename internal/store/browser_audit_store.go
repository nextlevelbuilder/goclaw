package store

import (
	"context"
	"encoding/json"
	"time"
)

// BrowserAuditEntry represents a browser action audit log entry.
type BrowserAuditEntry struct {
	ID         string          `json:"id"`
	TenantID   string          `json:"tenantId"`
	UserID     string          `json:"userId,omitempty"`
	AgentID    string          `json:"agentId,omitempty"`
	SessionID  string          `json:"sessionId,omitempty"`
	Action     string          `json:"action"`
	TargetID   string          `json:"targetId,omitempty"`
	Args       json.RawMessage `json:"args,omitempty"`
	Result     string          `json:"result,omitempty"`
	ErrorText  string          `json:"errorText,omitempty"`
	DurationMs int             `json:"durationMs"`
	CreatedAt  time.Time       `json:"createdAt"`
}

// AuditListOpts configures audit log listing.
type AuditListOpts struct {
	SessionID string
	Action    string
	Limit     int
	Offset    int
}

// BrowserAuditStore manages browser audit logs.
type BrowserAuditStore interface {
	Log(ctx context.Context, e *BrowserAuditEntry) error
	List(ctx context.Context, tenantID string, opts AuditListOpts) ([]*BrowserAuditEntry, int, error)
}
