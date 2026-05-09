package tools

import (
	"context"
	"fmt"
	"log/slog"
	"strings"

	"github.com/google/uuid"

	"github.com/nextlevelbuilder/goclaw/internal/store"
)

// SharedKnowledgeSearchTool orchestrates a two-phase search flow.
// Phase 1 (initial): searches raw message embeddings across all configured graph scopes.
// Phase 2 (drill-down via entity_id): traverses memory + knowledge graph.
type SharedKnowledgeSearchTool struct {
	chunkStore store.RawMessageChunkStore
	kgStore    store.KnowledgeGraphStore
	memStore   store.MemoryStore
}

func NewSharedKnowledgeSearchTool() *SharedKnowledgeSearchTool {
	return &SharedKnowledgeSearchTool{}
}

func (t *SharedKnowledgeSearchTool) SetChunkStore(s store.RawMessageChunkStore) {
	t.chunkStore = s
}

func (t *SharedKnowledgeSearchTool) SetKGStore(s store.KnowledgeGraphStore) {
	t.kgStore = s
}

func (t *SharedKnowledgeSearchTool) SetMemoryStore(s store.MemoryStore) {
	t.memStore = s
}

func (t *SharedKnowledgeSearchTool) IsWired() bool {
	return t.chunkStore != nil || t.kgStore != nil || t.memStore != nil
}

func (t *SharedKnowledgeSearchTool) Name() string { return "shared_knowledge_search" }

func (t *SharedKnowledgeSearchTool) Description() string {
	return "Search raw message embeddings across all configured graph scopes. " +
		"Use entity_id to drill down into memory context + knowledge graph relationships for a specific entity."
}

func (t *SharedKnowledgeSearchTool) Parameters() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"query": map[string]any{
				"type":        "string",
				"description": "Natural language search query. Use query='*' to list all entities.",
			},
			"scope": map[string]any{
				"type":        "string",
				"description": "Explicit graph scope ID override. If omitted, searches all configured shared knowledge scopes.",
			},
			"entity_id": map[string]any{
				"type":        "string",
				"description": "Entity UUID or name to traverse relationships from. Use to drill down into connections of a specific entity found in previous results.",
			},
			"max_depth": map[string]any{
				"type":        "number",
				"description": "Maximum traversal depth when entity_id is set (default 2, max 5).",
			},
			"max_results": map[string]any{
				"type":        "integer",
				"description": "Maximum results per scope (default 10).",
			},
		},
		"required": []string{"query"},
	}
}

func (t *SharedKnowledgeSearchTool) Execute(ctx context.Context, args map[string]any) *Result {
	query, _ := args["query"].(string)
	if query == "" {
		return ErrorResult("query parameter is required")
	}

	agentID := store.AgentIDFromContext(ctx)
	if agentID == uuid.Nil {
		return ErrorResult("agent context not available")
	}
	agentStr := agentID.String()
	userID := store.KGUserID(ctx)

	maxResults := 10
	if mr, ok := toInt(args["max_results"]); ok && mr > 0 {
		maxResults = min(mr, 50)
	}

	// Resolve graph scopes — search ALL configured graphs, not just the first.
	explicitScope, _ := args["scope"].(string)
	var scopes []string
	if explicitScope != "" {
		scopes = []string{explicitScope}
	} else {
		scopes = store.SharedKGIDsFromCtx(ctx)
	}

	if len(scopes) == 0 && t.chunkStore == nil && t.memStore == nil && t.kgStore == nil {
		return NewResult("Shared knowledge search is not configured for this agent (no scopes and no stores available).")
	}

	// Drill-down mode: entity traversal via memory + KG
	entityID, _ := args["entity_id"].(string)
	if entityID != "" {
		if t.kgStore != nil {
			return t.executeTraversal(ctx, agentStr, userID, entityID, query)
		}
		if t.memStore != nil {
			return t.searchMemory(ctx, query, agentStr, maxResults)
		}
		return ErrorResult("no memory or KG store available for drill-down")
	}

	var b strings.Builder

	// Phase 1: Raw message search across all configured graph scopes
	rmCount := 0
	var senders map[string]bool
	if t.chunkStore != nil && len(scopes) > 0 {
		searchQuery := query
		searchOpts := store.RawMessageChunkSearchOptions{
			MaxResults: maxResults,
		}
		if dateRange := ExtractDateRange(query); dateRange != nil {
			searchOpts.FromTime = &dateRange.From
			searchOpts.ToTime = &dateRange.To
			searchQuery = StripDateTokens(query)
			if searchQuery == "" {
				searchQuery = query
			}
		}

		for _, scope := range scopes {
			opts := searchOpts
			opts.GraphID = scope

			rmResults, err := t.chunkStore.Search(ctx, searchQuery, agentStr, opts)
			if err != nil {
				slog.Warn("shared_knowledge.raw_message_search_failed", "scope", scope, "error", err)
				continue
			}
			if len(rmResults) == 0 {
				continue
			}

			if senders == nil {
				senders = make(map[string]bool)
			}
			rmCount += len(rmResults)
			fmt.Fprintf(&b, "## Raw Messages from %q (%d results)\n\n", scope, len(rmResults))
			for i, r := range rmResults {
				if i >= maxResults {
					break
				}
				c := r.Chunk
				fmt.Fprintf(&b, "%d. [%s → %s] %s in \"%s\"\n",
					i+1, c.MsgTimeFrom.Format("2006-01-02 15:04"), c.MsgTimeTo.Format("2006-01-02 15:04"),
					c.Sender, c.ChatName)
				fmt.Fprintf(&b, "   \"%s\"\n\n", c.Text)

				if c.Sender != "" {
					senders[c.Sender] = true
				}
			}
		}
	}

	// Sender entity hints for drill-down
	if t.kgStore != nil && len(senders) > 0 {
		var entityLines []string
		seen := make(map[string]bool)
		for sender := range senders {
			entities, err := t.kgStore.SearchEntities(ctx, agentStr, userID, sender, 3)
			if err != nil {
				continue
			}
			for _, e := range entities {
				if seen[e.ID] {
					continue
				}
				seen[e.ID] = true
				line := fmt.Sprintf("- %s [%s] (id: %s)", e.Name, e.EntityType, e.ID)
				if e.Description != "" {
					desc := e.Description
					if len(desc) > 150 {
						desc = desc[:147] + "..."
					}
					line += "\n  " + desc
				}
				entityLines = append(entityLines, line)
			}
		}
		if len(entityLines) > 0 {
			fmt.Fprintf(&b, "## Related Entities (%d)\n\n", len(entityLines))
			for _, line := range entityLines {
				fmt.Fprintf(&b, "%s\n", line)
			}
			b.WriteString("\n")
		}
	}

	if b.Len() == 0 {
		return NewResult(fmt.Sprintf("No results found for %q across %d graph scope(s).", query, len(scopes)))
	}

	var header strings.Builder
	fmt.Fprintf(&header, "Raw message search for %q across %d scope(s)", query, len(scopes))
	if len(scopes) <= 3 {
		header.WriteString(" [" + strings.Join(scopes, ", ") + "]")
	}
	fmt.Fprintf(&header, ":\n\n")

	return NewResult(header.String() + b.String())
}

// searchMemory searches memory store only (used for drill-down).
func (t *SharedKnowledgeSearchTool) searchMemory(ctx context.Context, query, agentID string, maxResults int) *Result {
	memUserID := store.MemoryUserID(ctx)
	memResults, err := t.memStore.Search(ctx, query, agentID, memUserID, store.MemorySearchOptions{
		MaxResults:   maxResults,
		VectorWeight: 0.7,
		TextWeight:   0.3,
	})
	if err != nil {
		return ErrorResult(fmt.Sprintf("memory search failed: %v", err))
	}
	if len(memResults) == 0 {
		return NewResult("No memory results found.")
	}
	var b strings.Builder
	fmt.Fprintf(&b, "## Memory Context (%d results)\n\n", len(memResults))
	for i, r := range memResults {
		if i >= maxResults {
			break
		}
		snippet := r.Snippet
		if len(snippet) > 300 {
			snippet = snippet[:297] + "..."
		}
		fmt.Fprintf(&b, "- [%s] %s (score: %.3f)\n", r.Scope, r.Path, r.Score)
		fmt.Fprintf(&b, "  %s\n", snippet)
	}
	return NewResult(b.String())
}

// executeTraversal drills down into a specific entity's relationships.
func (t *SharedKnowledgeSearchTool) executeTraversal(ctx context.Context, agentID, userID, entityID, query string) *Result {
	// Resolve name to UUID if needed
	if _, err := uuid.Parse(entityID); err != nil {
		entities, err := t.kgStore.SearchEntities(ctx, agentID, userID, entityID, 5)
		if err == nil && len(entities) > 0 {
			lower := strings.ToLower(entityID)
			for _, e := range entities {
				if strings.ToLower(e.Name) == lower {
					entityID = e.ID
					break
				}
			}
			if _, err := uuid.Parse(entityID); err != nil {
				entityID = entities[0].ID
			}
		}
	}

	maxDepth := 2

	// Tier 1: deep traversal
	results, err := t.kgStore.Traverse(ctx, agentID, userID, entityID, maxDepth)
	if err != nil {
		return ErrorResult(fmt.Sprintf("graph traversal failed: %v", err))
	}
	if len(results) > 0 {
		const maxTraversalResults = 30
		totalResults := len(results)
		if totalResults > maxTraversalResults {
			results = results[:maxTraversalResults]
		}
		var sb strings.Builder
		sb.WriteString(fmt.Sprintf("Graph traversal from %q (max depth %d):\n\n", entityID, maxDepth))
		for _, r := range results {
			sb.WriteString(fmt.Sprintf("- [depth %d] %s (%s)", r.Depth, r.Entity.Name, r.Entity.EntityType))
			if r.Entity.EventTime != nil {
				sb.WriteString(fmt.Sprintf(" (event: %s)", r.Entity.EventTime.Format("2006-01-02 15:04")))
			}
			if r.Via != "" {
				if strings.HasPrefix(r.Via, "~") {
					sb.WriteString(fmt.Sprintf(" ←[%s]—", r.Via[1:]))
				} else {
					sb.WriteString(fmt.Sprintf(" —[%s]→", r.Via))
				}
			}
			if r.Entity.Description != "" {
				sb.WriteString(fmt.Sprintf("\n  %s", r.Entity.Description))
			}
			if p := formatProperties(r.Entity.Properties); p != "" {
				sb.WriteString(fmt.Sprintf("\n  [%s]", p))
			}
			if len(r.Path) > 0 {
				sb.WriteString(fmt.Sprintf("\n  path: %s", strings.Join(r.Path, " → ")))
			}
			sb.WriteString("\n")
		}
		if totalResults > maxTraversalResults {
			sb.WriteString(fmt.Sprintf("\n(+%d more entities reachable, use query to narrow or adjust max_depth)\n", totalResults-maxTraversalResults))
		}
		return NewResult(sb.String())
	}

	// Tier 2: direct connections
	relations, relErr := t.kgStore.ListRelations(ctx, agentID, userID, entityID)
	if relErr != nil {
		slog.Warn("shared_knowledge.listRelations_failed", "entity_id", entityID, "error", relErr)
	}
	if len(relations) > 0 {
		const maxDirectConnections = 10
		totalCount := len(relations)
		if totalCount > maxDirectConnections {
			relations = relations[:maxDirectConnections]
		}
		nameCache := make(map[string]string)
		entityName := t.resolveEntityName(ctx, agentID, userID, entityID, nameCache)
		var sb strings.Builder
		sb.WriteString(fmt.Sprintf("Direct connections of %q:\n\n", entityName))
		for _, rel := range relations {
			srcName := t.resolveEntityName(ctx, agentID, userID, rel.SourceEntityID, nameCache)
			tgtName := t.resolveEntityName(ctx, agentID, userID, rel.TargetEntityID, nameCache)
			sb.WriteString(fmt.Sprintf("  %s —[%s]→ %s\n", srcName, rel.RelationType, tgtName))
		}
		if totalCount > maxDirectConnections {
			sb.WriteString(fmt.Sprintf("\n(%d more connections not shown)\n", totalCount-maxDirectConnections))
		}
		return NewResult(sb.String())
	}

	return NewResult(fmt.Sprintf("No connected entities found from entity_id=%q.", entityID))
}

// resolveEntityName returns a human-readable name for an entity ID.
func (t *SharedKnowledgeSearchTool) resolveEntityName(ctx context.Context, agentID, userID, entityID string, cache map[string]string) string {
	if name, ok := cache[entityID]; ok {
		return name
	}
	e, err := t.kgStore.GetEntity(ctx, agentID, userID, entityID)
	if err == nil && e != nil {
		cache[entityID] = e.Name
		return e.Name
	}
	return entityID[:8]
}
