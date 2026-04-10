package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/nextlevelbuilder/goclaw/internal/store"
)

// MemorySearchTool implements the memory_search tool for hybrid semantic + FTS search.
type MemorySearchTool struct {
	memStore      store.MemoryStore              // Postgres-backed
	episodicStore store.EpisodicStore             // v3 episodic memory (nil = v2 fallback)
	metricsStore  store.EvolutionMetricsStore     // evolution metrics (nil = disabled)
	hasKG         bool                           // knowledge_graph_search tool is available
}

func NewMemorySearchTool() *MemorySearchTool {
	return &MemorySearchTool{}
}

// SetMemoryStore enables Postgres queries with agentID/userID scoping.
func (t *MemorySearchTool) SetMemoryStore(ms store.MemoryStore) {
	t.memStore = ms
}

// SetEpisodicStore enables v3 episodic search with tier awareness.
func (t *MemorySearchTool) SetEpisodicStore(es store.EpisodicStore) {
	t.episodicStore = es
}

// SetEvolutionMetricsStore enables retrieval metric recording.
func (t *MemorySearchTool) SetEvolutionMetricsStore(ms store.EvolutionMetricsStore) {
	t.metricsStore = ms
}

// SetHasKG enables the KG hint in search results.
func (t *MemorySearchTool) SetHasKG(has bool) {
	t.hasKG = has
}

func (t *MemorySearchTool) Name() string { return "memory_search" }

func (t *MemorySearchTool) Description() string {
	return "Mandatory recall step: semantically search MEMORY.md + memory/*.md before answering questions about prior work, decisions, dates, people, preferences, or todos; returns top snippets with path + lines. If response has disabled=true, memory retrieval is unavailable and should be surfaced to the user. IMPORTANT: Always query in the SAME language as the stored memory content. If the user speaks Vietnamese, search in Vietnamese. If memory was written in English, search in English. Matching the language dramatically improves search accuracy. If no relevant results found or confidence is low, tell the user you checked but found nothing — do not fabricate or guess memories."
}

func (t *MemorySearchTool) Parameters() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"query": map[string]any{
				"type":        "string",
				"description": "Natural language search query. Must be in the same language as the stored memory content (e.g., Vietnamese if memory is in Vietnamese).",
			},
			"maxResults": map[string]any{
				"type":        "number",
				"description": "Maximum number of results to return (default: 6)",
			},
			"minScore": map[string]any{
				"type":        "number",
				"description": "Minimum relevance score threshold (0-1)",
			},
			"depth": map[string]any{
				"type":        "string",
				"description": "Result depth: l0 (abstracts only), l1 (overview), l2 (full content). Default: l1. Only affects episodic memories.",
				"enum":        []string{"l0", "l1", "l2"},
			},
		},
		"required": []string{"query"},
	}
}

func (t *MemorySearchTool) Execute(ctx context.Context, args map[string]any) *Result {
	query, _ := args["query"].(string)
	if query == "" {
		return ErrorResult("query parameter is required")
	}

	var maxResults int
	var minScore float64
	if mr, ok := args["maxResults"].(float64); ok {
		maxResults = int(mr)
	}
	if ms, ok := args["minScore"].(float64); ok {
		minScore = ms
	}

	agentID := store.AgentIDFromContext(ctx)
	if t.memStore == nil || agentID == uuid.Nil {
		return ErrorResult("memory system not available")
	}

	userID := store.MemoryUserID(ctx)
	searchOpts := store.MemorySearchOptions{
		MaxResults: maxResults,
		MinScore:   minScore,
	}
	// Apply per-agent memory config overrides if set
	if mc := MemoryConfigFromCtx(ctx); mc != nil {
		if mc.MaxResults > 0 && searchOpts.MaxResults <= 0 {
			searchOpts.MaxResults = mc.MaxResults
		}
		if mc.VectorWeight > 0 {
			searchOpts.VectorWeight = mc.VectorWeight
		}
		if mc.TextWeight > 0 {
			searchOpts.TextWeight = mc.TextWeight
		}
		if mc.MinScore > 0 && searchOpts.MinScore <= 0 {
			searchOpts.MinScore = mc.MinScore
		}
	}
	agentStr := agentID.String()
	results, err := t.memStore.Search(ctx, query, agentStr, userID, searchOpts)
	if err != nil {
		return ErrorResult(fmt.Sprintf("memory search failed: %v", err))
	}
	// Fallback: also search leader's memory for team members and merge results.
	if leaderID := LeaderAgentIDFromCtx(ctx); leaderID != "" && leaderID != agentStr {
		leaderResults, lerr := t.memStore.Search(ctx, query, leaderID, userID, searchOpts)
		if lerr != nil && userID != "" {
			leaderResults, _ = t.memStore.Search(ctx, query, leaderID, "", searchOpts)
		}
		results = append(results, leaderResults...)
	}
	// V3 episodic memory search (merge with document results)
	var episodicResults []store.EpisodicSearchResult
	if t.episodicStore != nil {
		epOpts := store.EpisodicSearchOptions{
			MaxResults: maxResults, MinScore: minScore, VectorWeight: 0.3, TextWeight: 0.7,
		}
		epResults, epErr := t.episodicStore.Search(ctx, query, agentStr, userID, epOpts)
		if epErr == nil {
			episodicResults = epResults
		}
	}

	if len(results) == 0 && len(episodicResults) == 0 {
		return NewResult("No memory results found for query: " + query)
	}

	// Build combined output with tier labels
	type taggedResult struct {
		Tier string `json:"tier"`
		store.MemorySearchResult
		L0         string `json:"l0_abstract,omitempty"`
		EpisodicID string `json:"episodic_id,omitempty"`
	}
	var combined []taggedResult
	for _, r := range results {
		combined = append(combined, taggedResult{Tier: "document", MemorySearchResult: r})
	}
	for _, r := range episodicResults {
		combined = append(combined, taggedResult{
			Tier: "episodic", EpisodicID: r.EpisodicID, L0: r.L0Abstract,
			MemorySearchResult: store.MemorySearchResult{
				Path: "episodic:" + r.SessionKey, Score: r.Score, Snippet: r.L0Abstract, Source: "episodic",
			},
		})
	}

	output := map[string]any{
		"results": combined,
		"count":   len(combined),
	}
	if t.hasKG {
		output["hint"] = "Also run knowledge_graph_search if the query involves people, teams, projects, or connections between entities."
	}
	if len(episodicResults) > 0 {
		output["episodic_hint"] = "Use memory_expand(id) for full details on episodic memories."
	}
	data, _ := json.MarshalIndent(output, "", "  ")

	// Record retrieval metric non-blocking (best-effort).
	t.recordRetrievalMetric(ctx, len(combined), episodicResults)
	// Phase 10: update per-episode recall signals for dreaming weighted
	// scoring. Fire-and-forget — recall tracking must never block the hot
	// search path or surface errors to the agent loop.
	t.recordEpisodicRecall(ctx, episodicResults)

	return NewResult(string(data))
}

// recordEpisodicRecall schedules a best-effort RecordRecall update per
// episodic hit so DreamingWorker can prioritise useful entries. Runs in a
// background goroutine bounded by a 5s timeout so slow DBs can't leak.
func (t *MemorySearchTool) recordEpisodicRecall(ctx context.Context, episodic []store.EpisodicSearchResult) {
	if t.episodicStore == nil || len(episodic) == 0 {
		return
	}
	tenantID := store.TenantIDFromContext(ctx)
	if tenantID == uuid.Nil {
		return
	}
	// Snapshot hits so the goroutine doesn't observe caller mutations.
	hits := make([]store.EpisodicSearchResult, 0, len(episodic))
	for _, r := range episodic {
		if r.EpisodicID == "" {
			continue
		}
		hits = append(hits, r)
	}
	if len(hits) == 0 {
		return
	}
	go func() {
		bgCtx, cancel := context.WithTimeout(store.WithTenantID(context.Background(), tenantID), 5*time.Second)
		defer cancel()
		for _, r := range hits {
			if err := t.episodicStore.RecordRecall(bgCtx, r.EpisodicID, r.Score); err != nil {
				slog.Debug("memory.recall.record_failed", "episodic_id", r.EpisodicID, "error", err)
			}
		}
	}()
}

// recordRetrievalMetric records a memory_search retrieval metric in a background goroutine.
func (t *MemorySearchTool) recordRetrievalMetric(ctx context.Context, resultCount int, episodic []store.EpisodicSearchResult) {
	if t.metricsStore == nil {
		return
	}
	tenantID := store.TenantIDFromContext(ctx)
	if tenantID == uuid.Nil {
		return
	}
	agentID := store.AgentIDFromContext(ctx)
	var topScore float64
	for _, r := range episodic {
		if r.Score > topScore {
			topScore = r.Score
		}
	}
	go func() {
		bgCtx, cancel := context.WithTimeout(store.WithTenantID(context.Background(), tenantID), 5*time.Second)
		defer cancel()
		value, _ := json.Marshal(map[string]any{
			"result_count":  resultCount,
			"top_score":     topScore,
			"used_in_reply": resultCount > 0,
		})
		if err := t.metricsStore.RecordMetric(bgCtx, store.EvolutionMetric{
			ID:         uuid.New(),
			TenantID:   tenantID,
			AgentID:    agentID,
			MetricType: store.MetricRetrieval,
			MetricKey:  "memory_search",
			Value:      value,
		}); err != nil {
			slog.Debug("evolution.metric.memory_search_failed", "error", err)
		}
	}()
}

// MemoryGetTool implements the memory_get tool for reading specific memory files.
type MemoryGetTool struct {
	memStore store.MemoryStore // Postgres-backed
}

func NewMemoryGetTool() *MemoryGetTool {
	return &MemoryGetTool{}
}

// SetMemoryStore enables reading from Postgres memory_documents.
func (t *MemoryGetTool) SetMemoryStore(ms store.MemoryStore) {
	t.memStore = ms
}

func (t *MemoryGetTool) Name() string { return "memory_get" }

func (t *MemoryGetTool) Description() string {
	return "Safe snippet read from MEMORY.md or memory/*.md with optional from/lines; use after memory_search to pull only the needed lines and keep context small."
}

func (t *MemoryGetTool) Parameters() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"path": map[string]any{
				"type":        "string",
				"description": "Relative path to memory file (e.g., 'MEMORY.md' or 'memory/notes.md')",
			},
			"from": map[string]any{
				"type":        "number",
				"description": "Start line number (1-indexed). Omit to read from beginning.",
			},
			"lines": map[string]any{
				"type":        "number",
				"description": "Number of lines to read. Omit to read entire file.",
			},
		},
		"required": []string{"path"},
	}
}

func (t *MemoryGetTool) Execute(ctx context.Context, args map[string]any) *Result {
	path, _ := args["path"].(string)
	if path == "" {
		return ErrorResult("path parameter is required")
	}

	var fromLine, numLines int
	if from, ok := args["from"].(float64); ok {
		fromLine = int(from)
	}
	if lines, ok := args["lines"].(float64); ok {
		numLines = int(lines)
	}

	agentID := store.AgentIDFromContext(ctx)
	if t.memStore == nil || agentID == uuid.Nil {
		return ErrorResult("memory system not available")
	}

	userID := store.MemoryUserID(ctx)

	agentStr := agentID.String()

	// Try per-user first, then global
	content, err := t.memStore.GetDocument(ctx, agentStr, userID, path)
	if err != nil && userID != "" {
		content, err = t.memStore.GetDocument(ctx, agentStr, "", path)
	}
	// Fallback: try leader's memory for team members.
	if err != nil {
		if leaderID := LeaderAgentIDFromCtx(ctx); leaderID != "" && leaderID != agentStr {
			content, err = t.memStore.GetDocument(ctx, leaderID, userID, path)
			if err != nil && userID != "" {
				content, err = t.memStore.GetDocument(ctx, leaderID, "", path)
			}
		}
	}
	if err != nil {
		return ErrorResult(fmt.Sprintf("failed to read %s: %v", path, err))
	}

	text := extractLines(content, fromLine, numLines)
	if text == "" {
		return NewResult(fmt.Sprintf("File %s is empty or the specified range has no content.", path))
	}

	data, _ := json.MarshalIndent(map[string]any{
		"path": path,
		"text": text,
	}, "", "  ")
	return NewResult(string(data))
}

// extractLines extracts a range of lines from content.
// fromLine is 1-indexed. If 0, starts from beginning. If numLines is 0, returns all.
func extractLines(content string, fromLine, numLines int) string {
	if fromLine <= 0 && numLines <= 0 {
		return content
	}

	lines := strings.Split(content, "\n")
	start := 0
	if fromLine > 0 {
		start = fromLine - 1
	}
	if start >= len(lines) {
		return ""
	}

	end := len(lines)
	if numLines > 0 && start+numLines < end {
		end = start + numLines
	}

	return strings.Join(lines[start:end], "\n")
}
