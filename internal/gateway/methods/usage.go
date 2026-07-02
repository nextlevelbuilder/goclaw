package methods

import (
	"context"
	"encoding/json"
	"sort"

	"github.com/nextlevelbuilder/goclaw/internal/gateway"
	"github.com/nextlevelbuilder/goclaw/internal/store"
	"github.com/nextlevelbuilder/goclaw/pkg/protocol"
)

// UsageMethods handles usage.get, usage.summary.
// Queries SessionStore for real token data (accumulated via AccumulateTokens in agent loop).
type UsageMethods struct {
	sessions     store.SessionStore
	sessionCosts sessionCostReader
}

type sessionCostReader interface {
	GetSessionCosts(ctx context.Context, sessionKeys []string) (map[string]float64, error)
}

// UsageRecord is a single usage entry derived from session data.
type UsageRecord struct {
	AgentID      string  `json:"agentId"`
	SessionKey   string  `json:"sessionKey"`
	Model        string  `json:"model"`
	Provider     string  `json:"provider"`
	InputTokens  int64   `json:"inputTokens"`
	OutputTokens int64   `json:"outputTokens"`
	TotalTokens  int64   `json:"totalTokens"`
	Cost         float64 `json:"cost"`
	Timestamp    int64   `json:"timestamp"`
}

func NewUsageMethods(sessStore store.SessionStore, tracingStores ...store.TracingStore) *UsageMethods {
	m := &UsageMethods{sessions: sessStore}
	if len(tracingStores) > 0 && tracingStores[0] != nil {
		if costs, ok := tracingStores[0].(sessionCostReader); ok {
			m.sessionCosts = costs
		}
	}
	return m
}

func (m *UsageMethods) Register(router *gateway.MethodRouter) {
	router.Register(protocol.MethodUsageGet, m.handleGet)
	router.Register(protocol.MethodUsageSummary, m.handleSummary)
}

func (m *UsageMethods) handleGet(ctx context.Context, client *gateway.Client, req *protocol.RequestFrame) {
	var params struct {
		AgentID string `json:"agentId"`
		Limit   int    `json:"limit"`
		Offset  int    `json:"offset"`
	}
	if req.Params != nil {
		json.Unmarshal(req.Params, &params)
	}
	if params.Limit <= 0 {
		params.Limit = 20
	}

	// Use ListPagedRich: single query returns model, provider, tokens — no N+1 GetOrCreate loop.
	// Fetch large batch to filter non-zero tokens, then paginate in-memory.
	result := m.sessions.ListPagedRich(ctx, store.SessionListOpts{
		AgentID: params.AgentID,
		Limit:   10000,
	})

	records := make([]UsageRecord, 0, len(result.Sessions))
	for _, s := range result.Sessions {
		if s.InputTokens == 0 && s.OutputTokens == 0 {
			continue
		}
		agentID := extractAgentIDFromKey(s.Key)
		records = append(records, UsageRecord{
			AgentID:      agentID,
			SessionKey:   s.Key,
			Model:        s.Model,
			Provider:     s.Provider,
			InputTokens:  s.InputTokens,
			OutputTokens: s.OutputTokens,
			TotalTokens:  s.InputTokens + s.OutputTokens,
			Timestamp:    s.Updated.UnixMilli(),
		})
	}

	// Sort by timestamp desc (most recent first)
	sort.Slice(records, func(i, j int) bool {
		return records[i].Timestamp > records[j].Timestamp
	})

	total := len(records)

	// Apply offset + limit
	offset := min(params.Offset, total)
	end := min(offset+params.Limit, total)
	records = records[offset:end]
	if m.sessionCosts != nil && len(records) > 0 {
		sessionKeys := make([]string, 0, len(records))
		for _, record := range records {
			sessionKeys = append(sessionKeys, record.SessionKey)
		}
		if costs, err := m.sessionCosts.GetSessionCosts(ctx, sessionKeys); err == nil {
			for i := range records {
				records[i].Cost = costs[records[i].SessionKey]
			}
		}
	}

	client.SendResponse(protocol.NewOKResponse(req.ID, map[string]any{
		"records": records,
		"total":   total,
		"limit":   params.Limit,
		"offset":  offset,
	}))
}

func (m *UsageMethods) handleSummary(ctx context.Context, client *gateway.Client, req *protocol.RequestFrame) {
	// Use ListPagedRich: single query returns all token data — no N+1 GetOrCreate loop.
	result := m.sessions.ListPagedRich(ctx, store.SessionListOpts{Limit: 10000})

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

		agentID := extractAgentIDFromKey(s.Key)
		if byAgent[agentID] == nil {
			byAgent[agentID] = &agentSummary{}
		}

		byAgent[agentID].InputTokens += s.InputTokens
		byAgent[agentID].OutputTokens += s.OutputTokens
		byAgent[agentID].TotalTokens += s.InputTokens + s.OutputTokens
		byAgent[agentID].Sessions++
		totalRecords++
	}

	client.SendResponse(protocol.NewOKResponse(req.ID, map[string]any{
		"byAgent":      byAgent,
		"totalRecords": totalRecords,
	}))
}

// extractAgentIDFromKey extracts the agent ID from a session key.
// Session keys follow the format "agent:<agentID>:<scopeKey>".
func extractAgentIDFromKey(key string) string {
	// Find first colon after "agent:"
	if len(key) > 6 && key[:6] == "agent:" {
		rest := key[6:]
		for i, c := range rest {
			if c == ':' {
				return rest[:i]
			}
		}
		return rest
	}
	return key
}
