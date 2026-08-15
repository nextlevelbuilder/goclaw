package agent

import (
	"context"

	"github.com/nextlevelbuilder/goclaw/internal/memory"
)

// RetrievalConfig controls auto-inject behavior. Per-agent configurable.
type RetrievalConfig struct {
	Enabled            bool    `json:"enabled"`              // default true
	RelevanceThreshold float64 `json:"relevance_threshold"`  // default 0.3
	MaxL0Tokens        int     `json:"max_l0_tokens"`        // default 200
	MaxL0Items         int     `json:"max_l0_items"`         // default 5
	BM25Weight         float64 `json:"bm25_weight"`          // default 0.4
	EmbeddingWeight    float64 `json:"embedding_weight"`     // default 0.6
}

// DefaultRetrievalConfig returns sensible defaults.
func DefaultRetrievalConfig() RetrievalConfig {
	return RetrievalConfig{
		Enabled:            true,
		RelevanceThreshold: 0.3,
		MaxL0Tokens:        200,
		MaxL0Items:         5,
		BM25Weight:         0.4,
		EmbeddingWeight:    0.6,
	}
}

// Retriever performs auto-inject L0 retrieval for ContextStage.
type Retriever interface {
	// RetrieveL0 returns L0 summaries relevant to the query.
	// Called once per turn in ContextStage.
	RetrieveL0(ctx context.Context, agentID, userID, query string, cfg RetrievalConfig) ([]memory.L0Summary, error)
}
