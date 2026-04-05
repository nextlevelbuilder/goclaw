package browser

import (
	"context"
	"encoding/json"
	"log/slog"
	"time"

	"github.com/google/uuid"

	"github.com/nextlevelbuilder/goclaw/internal/store"
)

// AuditLogger logs browser actions to the audit store.
type AuditLogger struct {
	store  store.BrowserAuditStore
	logger *slog.Logger
}

// NewAuditLogger creates an AuditLogger.
func NewAuditLogger(s store.BrowserAuditStore, l *slog.Logger) *AuditLogger {
	if l == nil {
		l = slog.Default()
	}
	return &AuditLogger{store: s, logger: l}
}

// Log records a browser action. Fire-and-forget: runs in a goroutine to avoid blocking.
func (al *AuditLogger) Log(ctx context.Context, tenantID, action, targetID string, args any, dur time.Duration, resultErr error) {
	entry := &store.BrowserAuditEntry{
		TenantID:   tenantID,
		Action:     action,
		TargetID:   targetID,
		DurationMs: int(dur.Milliseconds()),
		Result:     "success",
	}

	if args != nil {
		if raw, err := json.Marshal(args); err == nil {
			entry.Args = raw
		}
	}

	if resultErr != nil {
		entry.Result = "error"
		entry.ErrorText = resultErr.Error()
	}

	// Extract context IDs if available
	if uid := store.UserIDFromContext(ctx); uid != "" {
		entry.UserID = uid
	}
	if aid := store.AgentIDFromContext(ctx); aid != uuid.Nil {
		entry.AgentID = aid.String()
	}

	// Fire-and-forget: don't block the tool action on DB write
	go func() {
		bgCtx := context.Background()
		if err := al.store.Log(bgCtx, entry); err != nil {
			al.logger.Warn("failed to log browser audit", "action", action, "error", err)
		}
	}()
}

// List returns audit entries with pagination.
func (al *AuditLogger) List(ctx context.Context, tenantID string, opts store.AuditListOpts) ([]*store.BrowserAuditEntry, int, error) {
	return al.store.List(ctx, tenantID, opts)
}
