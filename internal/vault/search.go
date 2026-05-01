// Package vault implements Knowledge Vault search integration.
// Phase 4: fan-out search across vault, episodic, and knowledge graph stores.
package vault

import (
	"context"
	"slices"
	"sort"
	"sync"

	"github.com/nextlevelbuilder/goclaw/internal/store"
)

// shouldFanout returns true when a store with the given key participates in
// the current fan-out. An empty DocTypes list fans out to all sources
// (backwards-compatible default); otherwise the key must be present.
func shouldFanout(docTypes []string, key string) bool {
	if len(docTypes) == 0 {
		return true
	}
	return slices.Contains(docTypes, key)
}

// SearchWeights controls relative weighting of each search source.
type SearchWeights struct {
	Vault    float64
	Episodic float64
	KG       float64
}

// DefaultSearchWeights returns the standard weight distribution.
func DefaultSearchWeights() SearchWeights {
	return SearchWeights{Vault: 0.4, Episodic: 0.3, KG: 0.3}
}

// UnifiedSearchOptions configures a cross-store search query.
type UnifiedSearchOptions struct {
	Query        string
	AgentID      string
	UserID       string
	TenantID     string
	TeamID       *string // nil = no filter (owner), ptr-to-empty = personal, ptr-to-uuid = team
	ChatID       *string // isolated-team scope: filter docs to (chat_id = ChatID OR chat_id IS NULL)
	TeamIsolated bool    // true = apply ChatID filter; false = shared/personal (ignore ChatID)
	Scope        string
	DocTypes     []string
	MaxResults   int
	MinScore     float64
	Weights      SearchWeights
}

// UnifiedSearchResult is a normalized result from any search source.
type UnifiedSearchResult struct {
	ID      string
	Title   string
	Path    string
	Source  string // "vault", "episodic", "kg"
	Score   float64
	DocType string
	Snippet string
}

// VaultSearchService coordinates fan-out search across all registered stores.
type VaultSearchService struct {
	vaultStore    store.VaultStore          // may be nil if vault disabled
	episodicStore store.EpisodicStore       // may be nil
	kgStore       store.KnowledgeGraphStore // may be nil
}

// NewVaultSearchService creates a search service. Any store may be nil (skipped).
func NewVaultSearchService(vs store.VaultStore, es store.EpisodicStore, kg store.KnowledgeGraphStore) *VaultSearchService {
	return &VaultSearchService{vaultStore: vs, episodicStore: es, kgStore: kg}
}

// Search executes parallel fan-out search, normalizes scores, applies weights, and deduplicates.
func (s *VaultSearchService) Search(ctx context.Context, opts UnifiedSearchOptions) ([]UnifiedSearchResult, error) {
	if opts.MaxResults <= 0 {
		opts.MaxResults = 10
	}
	if opts.Weights == (SearchWeights{}) {
		opts.Weights = DefaultSearchWeights()
	}

	var mu sync.Mutex
	var wg sync.WaitGroup

	// Bucket results per source before normalization
	vaultResults := make([]UnifiedSearchResult, 0)
	episodicResults := make([]UnifiedSearchResult, 0)
	kgResults := make([]UnifiedSearchResult, 0)

	// Fan-out: vault
	if s.vaultStore != nil {
		wg.Go(func() {
			results, err := s.vaultStore.Search(ctx, store.VaultSearchOptions{
				Query:        opts.Query,
				AgentID:      opts.AgentID,
				TenantID:     opts.TenantID,
				TeamID:       opts.TeamID,
				ChatID:       opts.ChatID,
				TeamIsolated: opts.TeamIsolated,
				Scope:        opts.Scope,
				DocTypes:     opts.DocTypes,
				MaxResults:   opts.MaxResults * 2,
				MinScore:     opts.MinScore,
			})
			if err != nil {
				return
			}
			converted := make([]UnifiedSearchResult, 0, len(results))
			for _, r := range results {
				converted = append(converted, UnifiedSearchResult{
					ID:      r.Document.ID,
					Title:   r.Document.Title,
					Path:    r.Document.Path,
					Source:  "vault",
					Score:   r.Score,
					DocType: r.Document.DocType,
					Snippet: r.Document.Path, // path as snippet fallback
				})
			}
			mu.Lock()
			vaultResults = converted
			mu.Unlock()
		})
	}

	// Fan-out: episodic
	if s.episodicStore != nil && shouldFanout(opts.DocTypes, "episodic") {
		wg.Go(func() {
			results, err := s.episodicStore.Search(ctx, opts.Query, opts.AgentID, opts.UserID, store.EpisodicSearchOptions{
				MaxResults: opts.MaxResults * 2,
				MinScore:   opts.MinScore,
			})
			if err != nil {
				return
			}
			converted := make([]UnifiedSearchResult, 0, len(results))
			for _, r := range results {
				converted = append(converted, UnifiedSearchResult{
					ID:      r.EpisodicID,
					Title:   r.SessionKey,
					Path:    "",
					Source:  "episodic",
					Score:   r.Score,
					DocType: "episodic",
					Snippet: r.L0Abstract,
				})
			}
			mu.Lock()
			episodicResults = converted
			mu.Unlock()
		})
	}

	// Fan-out: knowledge graph
	if s.kgStore != nil && shouldFanout(opts.DocTypes, "kg") {
		wg.Go(func() {
			entities, err := s.kgStore.SearchEntities(ctx, opts.AgentID, opts.UserID, opts.Query, opts.MaxResults*2)
			if err != nil {
				return
			}
			converted := make([]UnifiedSearchResult, 0, len(entities))
			for _, e := range entities {
				converted = append(converted, UnifiedSearchResult{
					ID:      e.ID,
					Title:   e.Name,
					Path:    "",
					Source:  "kg",
					Score:   e.Confidence,
					DocType: e.EntityType,
					Snippet: e.Description,
				})
			}
			mu.Lock()
			kgResults = converted
			mu.Unlock()
		})
	}

	wg.Wait()

	// Normalize scores per source and apply weights
	normalizeAndWeight(vaultResults, opts.Weights.Vault)
	normalizeAndWeight(episodicResults, opts.Weights.Episodic)
	normalizeAndWeight(kgResults, opts.Weights.KG)

	// Merge all results, dedup by ID
	all := make([]UnifiedSearchResult, 0, len(vaultResults)+len(episodicResults)+len(kgResults))
	seen := make(map[string]struct{})

	for _, bucket := range [][]UnifiedSearchResult{vaultResults, episodicResults, kgResults} {
		for _, r := range bucket {
			if _, exists := seen[r.ID]; exists {
				continue
			}
			seen[r.ID] = struct{}{}
			all = append(all, r)
		}
	}

	// Sort by final score DESC
	sort.Slice(all, func(i, j int) bool {
		return all[i].Score > all[j].Score
	})

	// Cap at maxResults
	if len(all) > opts.MaxResults {
		all = all[:opts.MaxResults]
	}

	return all, nil
}

// normalizeAndWeight normalizes scores by max score, then multiplies by weight.
func normalizeAndWeight(results []UnifiedSearchResult, weight float64) {
	if len(results) == 0 || weight == 0 {
		return
	}
	var maxScore float64
	for _, r := range results {
		if r.Score > maxScore {
			maxScore = r.Score
		}
	}
	if maxScore == 0 {
		return
	}
	for i := range results {
		results[i].Score = (results[i].Score / maxScore) * weight
	}
}
