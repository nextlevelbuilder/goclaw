package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"sync"

	"github.com/google/uuid"

	"github.com/nextlevelbuilder/goclaw/internal/skills"
	"github.com/nextlevelbuilder/goclaw/internal/store"
)

// tenantIndexState holds a per-tenant BM25 index and the loader version
// it was built against, so tenants never share (or clobber) each other's
// cached search index.
type tenantIndexState struct {
	index       *skills.Index
	lastVersion int64 // tracks loader version for lazy rebuild
}

// SkillSearchTool implements the skill_search tool with BM25 search
// and optional hybrid search (BM25 + embedding).
type SkillSearchTool struct {
	mu      sync.Mutex
	indexes map[uuid.UUID]*tenantIndexState // keyed by tenant ID (uuid.Nil = master/no-tenant)

	loader *skills.Loader

	// Optional: embedding-based search
	embSearcher store.EmbeddingSkillSearcher
	embProvider store.EmbeddingProvider

	// Optional: per-agent skill access filtering.
	// When set, search results are filtered to only include skills
	// accessible to the calling agent (public + agent-granted internal).
	skillAccess store.SkillAccessStore
}

// NewSkillSearchTool creates a skill_search tool backed by per-tenant BM25 indexes.
func NewSkillSearchTool(loader *skills.Loader) *SkillSearchTool {
	t := &SkillSearchTool{indexes: make(map[uuid.UUID]*tenantIndexState), loader: loader}
	t.ensureIndex(context.Background())
	return t
}

// SetEmbeddingSearcher enables hybrid search by providing a vector search backend.
func (t *SkillSearchTool) SetEmbeddingSearcher(searcher store.EmbeddingSkillSearcher, provider store.EmbeddingProvider) {
	t.embSearcher = searcher
	t.embProvider = provider
}

// SetSkillAccessStore enables per-agent skill filtering on search results.
func (t *SkillSearchTool) SetSkillAccessStore(sas store.SkillAccessStore) {
	t.skillAccess = sas
}

// ensureIndex returns the BM25 index for the tenant resolved from ctx,
// building or rebuilding it if it doesn't exist yet or skills have changed
// since it was last built. Each tenant's index is isolated in its own
// map entry so search results never leak across tenants.
func (t *SkillSearchTool) ensureIndex(ctx context.Context) *skills.Index {
	tenantID := store.TenantIDFromContext(ctx)
	current := t.loader.Version()

	t.mu.Lock()
	defer t.mu.Unlock()

	state, ok := t.indexes[tenantID]
	if !ok {
		state = &tenantIndexState{index: skills.NewIndex(), lastVersion: -1}
		t.indexes[tenantID] = state
	}

	if state.lastVersion >= current {
		return state.index
	}

	allSkills := t.loader.ListSkills(ctx)
	state.index.Build(allSkills)
	state.lastVersion = current
	slog.Info("skill_search index rebuilt", "docs", len(allSkills), "version", current, "tenant", tenantID)

	return state.index
}

func (t *SkillSearchTool) Name() string { return "skill_search" }

func (t *SkillSearchTool) Description() string {
	return "Search for available skills by keyword. Returns matching skills with name, description, and SKILL.md location for reading with read_file."
}

func (t *SkillSearchTool) Parameters() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"query": map[string]any{
				"type":        "string",
				"description": "Search keywords to find relevant skills (use English keywords)",
			},
			"max_results": map[string]any{
				"type":        "integer",
				"description": "Maximum number of results to return (default: 5)",
			},
		},
		"required": []string{"query"},
	}
}

func (t *SkillSearchTool) Execute(ctx context.Context, args map[string]any) *Result {
	query, _ := args["query"].(string)
	if query == "" {
		return ErrorResult("query parameter is required")
	}

	maxResults := 5
	if mr, ok := args["max_results"].(float64); ok && int(mr) > 0 {
		maxResults = int(mr)
	}

	// Lazy rebuild: resolve the tenant-scoped index, rebuilding if skills changed
	index := t.ensureIndex(ctx)

	// BM25 search (always available)
	bm25Results := index.Search(query, maxResults*2)

	// If embedding searcher is available, run hybrid search
	var results []skills.SkillSearchResult
	if t.embSearcher != nil && t.embProvider != nil {
		results = t.hybridSearch(ctx, query, bm25Results, maxResults)
	} else {
		// BM25-only: truncate to maxResults
		if len(bm25Results) > maxResults {
			bm25Results = bm25Results[:maxResults]
		}
		results = bm25Results
	}

	// Per-agent filtering: if SkillAccessStore is set, restrict results
	// to skills accessible to the calling agent.
	results = t.filterByAccess(ctx, results)

	slog.Info("skill_search executed", "query", query, "results", len(results),
		"hybrid", t.embSearcher != nil)

	if len(results) == 0 {
		return NewResult(fmt.Sprintf("No skills found matching: %s", query))
	}

	data, _ := json.MarshalIndent(map[string]any{
		"results": results,
		"count":   len(results),
	}, "", "  ")

	// Include explicit next-step instruction in the result so the model follows through.
	instruction := fmt.Sprintf(
		"\n\nACTION REQUIRED: Call use_skill with name \"%s\", then read_file with path \"%s\" to read the skill instructions, then follow them.",
		results[0].Name, results[0].Location,
	)

	return NewResult(string(data) + instruction)
}

// filterByAccess filters search results to only include skills accessible to the calling agent.
// If no SkillAccessStore is set or no agent ID is in context, returns results unfiltered.
func (t *SkillSearchTool) filterByAccess(ctx context.Context, results []skills.SkillSearchResult) []skills.SkillSearchResult {
	if t.skillAccess == nil {
		return results
	}
	agentID := store.AgentIDFromContext(ctx)
	if agentID == uuid.Nil {
		return results
	}
	userID := store.UserIDFromContext(ctx)
	accessible, err := t.skillAccess.ListAccessible(ctx, agentID, userID)
	if err != nil {
		slog.Warn("skill_search: failed to load accessible skills, returning unfiltered", "error", err)
		return results
	}
	allowed := make(map[string]struct{}, len(accessible))
	for _, s := range accessible {
		allowed[s.Slug] = struct{}{}
	}
	// Filesystem skills (source != "managed") are always allowed
	filtered := make([]skills.SkillSearchResult, 0, len(results))
	for _, r := range results {
		if r.Source != "managed" {
			filtered = append(filtered, r)
		} else if _, ok := allowed[r.Slug]; ok {
			filtered = append(filtered, r)
		} else {
			slog.Debug("skill_search: filtered out inaccessible managed skill", "slug", r.Slug, "name", r.Name)
		}
	}
	return filtered
}

// hybridSearch merges BM25 and embedding results with weighted scoring.
// Weights: BM25 0.3, vector 0.7 (same as memory hybrid search).
func (t *SkillSearchTool) hybridSearch(ctx context.Context, query string, bm25Results []skills.SkillSearchResult, maxResults int) []skills.SkillSearchResult {
	// Generate query embedding
	embeddings, err := t.embProvider.Embed(ctx, []string{query})
	if err != nil || len(embeddings) == 0 || len(embeddings[0]) == 0 {
		slog.Warn("skill_search embedding failed, falling back to BM25", "error", err)
		if len(bm25Results) > maxResults {
			bm25Results = bm25Results[:maxResults]
		}
		return bm25Results
	}

	// Vector search
	vecResults, err := t.embSearcher.SearchByEmbedding(ctx, embeddings[0], maxResults*2)
	if err != nil {
		slog.Warn("skill_search vector search failed, falling back to BM25", "error", err)
		if len(bm25Results) > maxResults {
			bm25Results = bm25Results[:maxResults]
		}
		return bm25Results
	}

	// Merge: normalize weights when one channel has no results
	textW, vecW := 0.3, 0.7
	if len(bm25Results) == 0 && len(vecResults) > 0 {
		textW, vecW = 0, 1.0
	} else if len(vecResults) == 0 && len(bm25Results) > 0 {
		textW, vecW = 1.0, 0
	}

	// Deduplicate by skill name, accumulate scores
	type merged struct {
		result skills.SkillSearchResult
		score  float64
	}
	seen := make(map[string]*merged)

	// Normalize BM25 scores to 0-1 range
	var maxBM25 float64
	for _, r := range bm25Results {
		if r.Score > maxBM25 {
			maxBM25 = r.Score
		}
	}

	for _, r := range bm25Results {
		normalizedScore := r.Score
		if maxBM25 > 0 {
			normalizedScore = r.Score / maxBM25
		}
		if existing, ok := seen[r.Name]; ok {
			existing.score += normalizedScore * textW
		} else {
			seen[r.Name] = &merged{result: r, score: normalizedScore * textW}
		}
	}

	for _, r := range vecResults {
		if existing, ok := seen[r.Name]; ok {
			existing.score += r.Score * vecW
		} else {
			seen[r.Name] = &merged{
				result: skills.SkillSearchResult{
					Name:        r.Name,
					Slug:        r.Slug,
					Description: r.Description,
					Location:    r.Path,
					Source:      "managed",
					Score:       0,
				},
				score: r.Score * vecW,
			}
		}
	}

	// Collect and sort
	results := make([]skills.SkillSearchResult, 0, len(seen))
	for _, m := range seen {
		m.result.Score = m.score
		results = append(results, m.result)
	}

	// Sort descending by score
	for i := 0; i < len(results); i++ {
		for j := i + 1; j < len(results); j++ {
			if results[j].Score > results[i].Score {
				results[i], results[j] = results[j], results[i]
			}
		}
	}

	if len(results) > maxResults {
		results = results[:maxResults]
	}
	return results
}
