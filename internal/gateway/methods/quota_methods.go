package methods

import (
	"context"
	"database/sql"

	"github.com/nextlevelbuilder/goclaw/internal/channels"
	"github.com/nextlevelbuilder/goclaw/internal/gateway"
	"github.com/nextlevelbuilder/goclaw/pkg/protocol"
)

// QuotaMethods handles quota.usage — returns per-user quota consumption for the dashboard.
// Nil-safe: returns {enabled: false} when quotaChecker is nil (standalone mode).
// When checker is nil but db is available, still queries today's summary from traces.
type QuotaMethods struct {
	checker *channels.QuotaChecker
	db      *sql.DB
}

func NewQuotaMethods(checker *channels.QuotaChecker, db *sql.DB) *QuotaMethods {
	return &QuotaMethods{checker: checker, db: db}
}

func (m *QuotaMethods) Register(router *gateway.MethodRouter) {
	router.Register(protocol.MethodQuotaUsage, m.handleUsage)
}

func (m *QuotaMethods) handleUsage(ctx context.Context, client *gateway.Client, req *protocol.RequestFrame) {
	if m.checker == nil {
		result := channels.QuotaUsageResult{
			Enabled: false,
			Entries: []channels.QuotaUsageEntry{},
		}
		// Still query today's summary from traces when DB is available
		if m.db != nil {
			channels.QueryTodaySummary(ctx, m.db, &result)
		}
		client.SendResponse(protocol.NewOKResponse(req.ID, result))
		return
	}

	result := m.checker.Usage(ctx)
	client.SendResponse(protocol.NewOKResponse(req.ID, result))
}
