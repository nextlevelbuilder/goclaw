package vault

import (
	"context"

	"github.com/nextlevelbuilder/goclaw/internal/memory"
)

// RetrieverConfig controls retrieval behavior for VaultRetriever.
// Mirrors agent.RetrievalConfig but avoids an import cycle with the agent package.
type RetrieverConfig struct {
	RelevanceThreshold float64
	MaxL0Items         int
	TenantID           string
}

// VaultRetriever fetches L0 summaries relevant to a query via VaultSearchService.
// To use as agent.Retriever, wrap with an adapter that maps agent.RetrievalConfig → RetrieverConfig.
type VaultRetriever struct {
	search *VaultSearchService
}

// NewVaultRetriever creates a retriever backed by a VaultSearchService.
func NewVaultRetriever(search *VaultSearchService) *VaultRetriever {
	return &VaultRetriever{search: search}
}

// RetrieveL0 returns L0 summaries relevant to the query.
func (v *VaultRetriever) RetrieveL0(ctx context.Context, agentID, userID, query string, cfg RetrieverConfig) ([]memory.L0Summary, error) {
	maxItems := cfg.MaxL0Items
	if maxItems <= 0 {
		maxItems = 5
	}

	threshold := cfg.RelevanceThreshold
	if threshold <= 0 {
		threshold = 0.3
	}

	results, err := v.search.Search(ctx, UnifiedSearchOptions{
		Query:      query,
		AgentID:    agentID,
		UserID:     userID,
		TenantID:   cfg.TenantID,
		MaxResults: maxItems,
		MinScore:   threshold,
		Weights:    DefaultSearchWeights(),
	})
	if err != nil {
		return nil, err
	}

	summaries := make([]memory.L0Summary, 0, len(results))
	for _, r := range results {
		snippet := r.Snippet
		if snippet == "" {
			snippet = r.Title
		}
		summaries = append(summaries, memory.L0Summary{
			Topic:   r.Title,
			Summary: snippet,
			ID:      r.ID,
		})
	}

	return summaries, nil
}
