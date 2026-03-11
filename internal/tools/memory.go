package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/google/uuid"

	"github.com/nextlevelbuilder/goclaw/internal/store"
)

// MemorySearchTool implements the memory_search tool for hybrid semantic + FTS search.
type MemorySearchTool struct {
	memStore store.MemoryStore // Postgres-backed
}

func NewMemorySearchTool() *MemorySearchTool {
	return &MemorySearchTool{}
}

// SetMemoryStore enables Postgres queries with agentID/userID scoping.
func (t *MemorySearchTool) SetMemoryStore(ms store.MemoryStore) {
	t.memStore = ms
}

func (t *MemorySearchTool) Name() string { return "memory_search" }

func (t *MemorySearchTool) Description() string {
	return "Mandatory recall step: semantically search MEMORY.md + memory/*.md before answering questions about prior work, decisions, dates, people, preferences, or todos; returns top snippets with path + lines. If response has disabled=true, memory retrieval is unavailable and should be surfaced to the user. IMPORTANT: Always query in the SAME language as the stored memory content. If the user speaks Vietnamese, search in Vietnamese. If memory was written in English, search in English. Matching the language dramatically improves search accuracy."
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

	userID := store.UserIDFromContext(ctx)
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
	results, err := t.memStore.Search(ctx, query, agentID.String(), userID, searchOpts)
	if err != nil {
		return ErrorResult(fmt.Sprintf("memory search failed: %v", err))
	}
	if len(results) == 0 {
		return NewResult("No memory results found for query: " + query)
	}

	data, _ := json.MarshalIndent(map[string]any{
		"results": results,
		"count":   len(results),
	}, "", "  ")
	return NewResult(string(data))
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

	userID := store.UserIDFromContext(ctx)

	// Try per-user first, then global
	content, err := t.memStore.GetDocument(ctx, agentID.String(), userID, path)
	if err != nil && userID != "" {
		// Fallback to global
		content, err = t.memStore.GetDocument(ctx, agentID.String(), "", path)
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
