package mcp

import (
	"context"
	"database/sql"
	"sort"

	mcpgo "github.com/mark3labs/mcp-go/mcp"
	mcpserver "github.com/mark3labs/mcp-go/server"

	"github.com/nextlevelbuilder/goclaw/internal/channels"
	"github.com/nextlevelbuilder/goclaw/internal/store"
)

// mcpUsageRecord mirrors internal/gateway/methods/usage.go's UsageRecord.
// Cost enrichment (GetSessionCosts via a tracing store) is not wired on this
// standalone MCP surface — see final report.
type mcpUsageRecord struct {
	AgentID      string `json:"agentId"`
	SessionKey   string `json:"sessionKey"`
	Model        string `json:"model"`
	Provider     string `json:"provider"`
	InputTokens  int64  `json:"inputTokens"`
	OutputTokens int64  `json:"outputTokens"`
	TotalTokens  int64  `json:"totalTokens"`
	Timestamp    int64  `json:"timestamp"`
}

// extractAgentIDFromSessionKey mirrors internal/gateway/methods/usage.go's
// unexported extractAgentIDFromKey helper. Session keys follow the format
// "agent:<agentID>:<scopeKey>".
func extractAgentIDFromSessionKey(key string) string {
	const prefix = "agent:"
	if len(key) > len(prefix) && key[:len(prefix)] == prefix {
		rest := key[len(prefix):]
		for i, c := range rest {
			if c == ':' {
				return rest[:i]
			}
		}
		return rest
	}
	return key
}

// registerUsageCRUDTools registers the goclaw_usage_* MCP tools backed by
// store.SessionStore. Mirrors internal/gateway/methods/usage.go.
func registerUsageCRUDTools(srv *mcpserver.MCPServer, sessions store.SessionStore) {
	srv.AddTool(mcpgo.NewTool("goclaw_usage_get",
		mcpgo.WithDescription("List per-session token usage records, optionally filtered by agent."),
		mcpgo.WithString("agent_id", mcpgo.Description("Filter by agent ID.")),
		mcpgo.WithNumber("limit", mcpgo.Description("Maximum records to return; defaults to 20.")),
		mcpgo.WithNumber("offset", mcpgo.Description("Pagination offset.")),
		mcpgo.WithReadOnlyHintAnnotation(true),
	), handleUsageGet(sessions))

	srv.AddTool(mcpgo.NewTool("goclaw_usage_summary",
		mcpgo.WithDescription("Return aggregate token usage summary, grouped by agent."),
		mcpgo.WithReadOnlyHintAnnotation(true),
	), handleUsageSummary(sessions))
}

func handleUsageGet(sessions store.SessionStore) mcpserver.ToolHandlerFunc {
	return func(ctx context.Context, req mcpgo.CallToolRequest) (*mcpgo.CallToolResult, error) {
		const defaultLimit = 20
		limit := int(req.GetFloat("limit", 0))
		if limit <= 0 {
			limit = defaultLimit
		}
		offset := int(req.GetFloat("offset", 0))

		const fetchBatch = 10000
		result := sessions.ListPagedRich(ctx, store.SessionListOpts{
			AgentID: req.GetString("agent_id", ""),
			Limit:   fetchBatch,
		})

		records := make([]mcpUsageRecord, 0, len(result.Sessions))
		for _, s := range result.Sessions {
			if s.InputTokens == 0 && s.OutputTokens == 0 {
				continue
			}
			records = append(records, mcpUsageRecord{
				AgentID: extractAgentIDFromSessionKey(s.Key), SessionKey: s.Key,
				Model: s.Model, Provider: s.Provider,
				InputTokens: s.InputTokens, OutputTokens: s.OutputTokens,
				TotalTokens: s.InputTokens + s.OutputTokens, Timestamp: s.Updated.UnixMilli(),
			})
		}
		sort.Slice(records, func(i, j int) bool { return records[i].Timestamp > records[j].Timestamp })

		total := len(records)
		start := min(offset, total)
		end := min(start+limit, total)
		records = records[start:end]

		return jsonToolResult(map[string]any{
			"records": records, "total": total, "limit": limit, "offset": start,
		})
	}
}

func handleUsageSummary(sessions store.SessionStore) mcpserver.ToolHandlerFunc {
	return func(ctx context.Context, _ mcpgo.CallToolRequest) (*mcpgo.CallToolResult, error) {
		const fetchBatch = 10000
		result := sessions.ListPagedRich(ctx, store.SessionListOpts{Limit: fetchBatch})

		type agentSummary struct {
			InputTokens  int64 `json:"inputTokens"`
			OutputTokens int64 `json:"outputTokens"`
			TotalTokens  int64 `json:"totalTokens"`
			Sessions     int   `json:"sessions"`
		}
		byAgent := make(map[string]*agentSummary)
		var totalRecords int
		for _, s := range result.Sessions {
			if s.InputTokens == 0 && s.OutputTokens == 0 {
				continue
			}
			agentID := extractAgentIDFromSessionKey(s.Key)
			if byAgent[agentID] == nil {
				byAgent[agentID] = &agentSummary{}
			}
			byAgent[agentID].InputTokens += s.InputTokens
			byAgent[agentID].OutputTokens += s.OutputTokens
			byAgent[agentID].TotalTokens += s.InputTokens + s.OutputTokens
			byAgent[agentID].Sessions++
			totalRecords++
		}
		return jsonToolResult(map[string]any{"byAgent": byAgent, "totalRecords": totalRecords})
	}
}

// registerQuotaCRUDTools registers the goclaw_quota_usage MCP tool backed by
// *channels.QuotaChecker. Mirrors internal/gateway/methods/quota_methods.go.
// Both checker and db may be nil — degrades to {enabled: false} with an
// empty entries list, matching the WS twin's nil-safe contract.
func registerQuotaCRUDTools(srv *mcpserver.MCPServer, checker *channels.QuotaChecker, db *sql.DB) {
	srv.AddTool(mcpgo.NewTool("goclaw_quota_usage",
		mcpgo.WithDescription("Return per-user/group channel quota consumption for today."),
		mcpgo.WithReadOnlyHintAnnotation(true),
	), handleQuotaUsage(checker, db))
}

func handleQuotaUsage(checker *channels.QuotaChecker, db *sql.DB) mcpserver.ToolHandlerFunc {
	return func(ctx context.Context, _ mcpgo.CallToolRequest) (*mcpgo.CallToolResult, error) {
		if checker == nil {
			result := channels.QuotaUsageResult{Enabled: false, Entries: []channels.QuotaUsageEntry{}}
			if db != nil {
				channels.QueryTodaySummary(ctx, db, &result)
			}
			return jsonToolResult(result)
		}
		return jsonToolResult(checker.Usage(ctx))
	}
}
