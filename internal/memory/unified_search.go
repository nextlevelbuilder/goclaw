package memory

import (
	"context"
	"sort"

	"github.com/nextlevelbuilder/goclaw/internal/store"
)

// UnifiedSearchOptions configures cross-tier memory search.
type UnifiedSearchOptions struct {
	MaxResults int
	MinScore   float64
	Tiers      []string // "episodic", "document" — empty = all
	Depth      string   // "l0", "l1", "l2" — default "l1"
}

// UnifiedSearchResult extends MemorySearchResult with tier info.
type UnifiedSearchResult struct {
	store.MemorySearchResult
	Tier       string `json:"tier"`
	EpisodicID string `json:"episodic_id,omitempty"`
	L0Abstract string `json:"l0_abstract,omitempty"`
}

// UnifiedSearch provides cross-tier memory search (episodic + documents).
type UnifiedSearch struct {
	episodicStore store.EpisodicStore
	memStore      store.MemoryStore
	l1Cache       *L1Cache
}

// NewUnifiedSearch creates a cross-tier memory search.
func NewUnifiedSearch(es store.EpisodicStore, ms store.MemoryStore, l1 *L1Cache) *UnifiedSearch {
	return &UnifiedSearch{episodicStore: es, memStore: ms, l1Cache: l1}
}

// Search queries both episodic and document tiers, merges by score.
func (u *UnifiedSearch) Search(ctx context.Context, query, agentID, userID string, opts UnifiedSearchOptions) ([]UnifiedSearchResult, error) {
	maxResults := opts.MaxResults
	if maxResults <= 0 {
		maxResults = 10
	}

	tiers := opts.Tiers
	if len(tiers) == 0 {
		tiers = []string{"episodic", "document"}
	}

	var results []UnifiedSearchResult

	// Episodic tier
	if containsTier(tiers, "episodic") && u.episodicStore != nil {
		epResults, err := u.episodicStore.Search(ctx, query, agentID, userID,
			store.EpisodicSearchOptions{MaxResults: maxResults, MinScore: opts.MinScore})
		if err == nil {
			for _, r := range epResults {
				content := r.L0Abstract
				if opts.Depth == "l1" && u.l1Cache != nil {
					if l1, ok := u.l1Cache.Get(r.EpisodicID); ok {
						content = l1
					}
				}
				results = append(results, UnifiedSearchResult{
					MemorySearchResult: store.MemorySearchResult{
						Path:    "episodic:" + r.SessionKey,
						Score:   r.Score,
						Snippet: content,
						Source:  "episodic",
					},
					Tier:       "episodic",
					EpisodicID: r.EpisodicID,
					L0Abstract: r.L0Abstract,
				})
			}
		}
	}

	// Document tier (existing memory store)
	if containsTier(tiers, "document") && u.memStore != nil {
		docResults, err := u.memStore.Search(ctx, query, agentID, userID,
			store.MemorySearchOptions{MaxResults: maxResults, MinScore: opts.MinScore})
		if err == nil {
			for _, r := range docResults {
				results = append(results, UnifiedSearchResult{
					MemorySearchResult: r,
					Tier:               "document",
				})
			}
		}
	}

	// Sort by score descending, cap at maxResults
	sort.Slice(results, func(i, j int) bool { return results[i].Score > results[j].Score })
	if len(results) > maxResults {
		results = results[:maxResults]
	}
	return results, nil
}

func containsTier(tiers []string, tier string) bool {
	for _, t := range tiers {
		if t == tier {
			return true
		}
	}
	return false
}
