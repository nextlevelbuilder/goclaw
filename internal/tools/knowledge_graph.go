package tools

import (
	"context"
	"fmt"
	"log/slog"
	"strings"

	"github.com/google/uuid"

	"github.com/nextlevelbuilder/goclaw/internal/store"
)

// KnowledgeGraphSearchTool provides graph-based search for agents.
type KnowledgeGraphSearchTool struct {
	kgStore store.KnowledgeGraphStore
}

// NewKnowledgeGraphSearchTool creates a new KnowledgeGraphSearchTool.
func NewKnowledgeGraphSearchTool() *KnowledgeGraphSearchTool {
	return &KnowledgeGraphSearchTool{}
}

// SetKGStore sets the KnowledgeGraphStore for this tool.
func (t *KnowledgeGraphSearchTool) SetKGStore(ks store.KnowledgeGraphStore) {
	t.kgStore = ks
}

func (t *KnowledgeGraphSearchTool) Name() string { return "knowledge_graph_search" }

func (t *KnowledgeGraphSearchTool) Description() string {
	return "Search the knowledge graph to find people, projects, organizations, and how they connect. Better than memory_search when you need: who works with whom, what projects someone is involved in, dependencies between tasks, or any multi-hop relationship question. Use specific names (e.g. 'Minh', 'GoClaw') — not generic words. Use query='*' to list all known entities. Use entity_id to traverse connections from a specific entity."
}

func (t *KnowledgeGraphSearchTool) Parameters() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"query": map[string]any{
				"type":        "string",
				"description": "Search query for entity names or descriptions",
			},
			"entity_type": map[string]any{
				"type":        "string",
				"description": "Filter by entity type (person, organization, project, product, technology, task, event, document, concept, location)",
			},
			"entity_id": map[string]any{
				"type":        "string",
				"description": "Entity ID to traverse from (for relationship discovery)",
			},
			"max_depth": map[string]any{
				"type":        "number",
				"description": "Maximum traversal depth (default 2, max 5)",
			},
		},
		"required": []string{"query"},
	}
}

func (t *KnowledgeGraphSearchTool) Execute(ctx context.Context, args map[string]any) *Result {
	if t.kgStore == nil {
		return NewResult("Knowledge graph is not enabled for this agent.")
	}

	agentID := store.AgentIDFromContext(ctx)
	if agentID == uuid.Nil {
		return ErrorResult("agent context not available")
	}
	userID := store.KGUserID(ctx)

	query, _ := args["query"].(string)
	if query == "" {
		return ErrorResult("query parameter is required")
	}

	entityID, _ := args["entity_id"].(string)
	maxDepth := 2
	if md, ok := args["max_depth"].(float64); ok && md > 0 {
		maxDepth = min(int(md), 5)
	}

	// Traversal mode: entity_id provided
	if entityID != "" {
		return t.executeTraversal(ctx, agentID.String(), userID, entityID, maxDepth, query)
	}

	// List-all mode: query="*"
	if query == "*" {
		return t.executeListAll(ctx, agentID.String(), userID)
	}

	// Search mode
	return t.executeSearch(ctx, agentID.String(), userID, query, args)
}

func (t *KnowledgeGraphSearchTool) executeTraversal(ctx context.Context, agentID, userID, entityID string, maxDepth int, query string) *Result {
	// Tier 1: outgoing deep traversal
	results, err := t.kgStore.Traverse(ctx, agentID, userID, entityID, maxDepth)
	if err != nil {
		return ErrorResult(fmt.Sprintf("graph traversal failed: %v", err))
	}
	if len(results) > 0 {
		const maxTraversalResults = 20
		totalResults := len(results)
		if totalResults > maxTraversalResults {
			results = results[:maxTraversalResults]
		}
		var sb strings.Builder
		sb.WriteString(fmt.Sprintf("Graph traversal from %q (max depth %d):\n\n", entityID, maxDepth))
		for _, r := range results {
			sb.WriteString(fmt.Sprintf("- [depth %d] %s (%s)", r.Depth, r.Entity.Name, r.Entity.EntityType))
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

	// Tier 2: direct connections (bidirectional, 1-hop, cap 10)
	relations, relErr := t.kgStore.ListRelations(ctx, agentID, userID, entityID)
	if relErr != nil {
		slog.Warn("kg.listRelations failed", "entity_id", entityID, "error", relErr)
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

	// Tier 3: fallback to search if query provided
	if query != "" && query != "*" {
		return t.executeSearch(ctx, agentID, userID, query, nil)
	}

	return NewResult(fmt.Sprintf("No connected entities found from entity_id=%q.", entityID))
}

func (t *KnowledgeGraphSearchTool) executeListAll(ctx context.Context, agentID, userID string) *Result {
	entities, err := t.kgStore.ListEntities(ctx, agentID, userID, store.EntityListOptions{Limit: 30})
	if err != nil {
		return ErrorResult(fmt.Sprintf("list entities failed: %v", err))
	}
	if len(entities) == 0 {
		return NewResult("Knowledge graph is empty. No entities have been extracted yet.")
	}
	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("Knowledge graph has %d entities:\n\n", len(entities)))
	for _, e := range entities {
		sb.WriteString(fmt.Sprintf("- %s [%s] (id: %s)\n", e.Name, e.EntityType, e.ID))
		if e.Description != "" {
			sb.WriteString(fmt.Sprintf("  %s\n", e.Description))
		}
	}
	sb.WriteString("\nTip: Use entity_id parameter to traverse relationships from a specific entity.")
	return NewResult(sb.String())
}

func (t *KnowledgeGraphSearchTool) executeSearch(ctx context.Context, agentID, userID, query string, args map[string]any) *Result {
	entities, err := t.kgStore.SearchEntities(ctx, agentID, userID, query, 10)
	if err != nil {
		return ErrorResult(fmt.Sprintf("entity search failed: %v", err))
	}

	// No results: show available entities as hints
	if len(entities) == 0 {
		return t.noResultsHint(ctx, agentID, userID, query)
	}

	// Optional type filter (post-search)
	entityType, _ := args["entity_type"].(string)
	if entityType != "" {
		filtered := entities[:0]
		for _, e := range entities {
			if e.EntityType == entityType {
				filtered = append(filtered, e)
			}
		}
		entities = filtered
		if len(entities) == 0 {
			return NewResult(fmt.Sprintf("No entities of type %q found matching %q.", entityType, query))
		}
	}

	// Build entity name lookup for relation display
	entityNames := make(map[string]string, len(entities))
	for _, e := range entities {
		entityNames[e.ID] = e.Name
	}

	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("Found %d entities matching %q:\n\n", len(entities), query))
	for _, e := range entities {
		sb.WriteString(fmt.Sprintf("- %s [%s] (id: %s)\n", e.Name, e.EntityType, e.ID))
		if e.Description != "" {
			sb.WriteString(fmt.Sprintf("  %s\n", e.Description))
		}

		// Fetch relations to show connections with names (cap 5 per entity)
		relations, err := t.kgStore.ListRelations(ctx, agentID, userID, e.ID)
		if err == nil && len(relations) > 0 {
			const maxRelationsPerEntity = 5
			sb.WriteString("  Relations:\n")
			shown := min(len(relations), maxRelationsPerEntity)
			for _, rel := range relations[:shown] {
				srcName := t.resolveEntityName(ctx, agentID, userID, rel.SourceEntityID, entityNames)
				tgtName := t.resolveEntityName(ctx, agentID, userID, rel.TargetEntityID, entityNames)
				sb.WriteString(fmt.Sprintf("    %s —[%s]→ %s\n", srcName, rel.RelationType, tgtName))
			}
			if len(relations) > maxRelationsPerEntity {
				sb.WriteString(fmt.Sprintf("    (+%d more, use entity_id=%q to see all)\n", len(relations)-maxRelationsPerEntity, e.ID))
			}
		}
	}
	return NewResult(sb.String())
}

// resolveEntityName returns a human-readable name for an entity ID, using cache or DB lookup.
func (t *KnowledgeGraphSearchTool) resolveEntityName(ctx context.Context, agentID, userID, entityID string, cache map[string]string) string {
	if name, ok := cache[entityID]; ok {
		return name
	}
	e, err := t.kgStore.GetEntity(ctx, agentID, userID, entityID)
	if err == nil && e != nil {
		cache[entityID] = e.Name
		return e.Name
	}
	return entityID[:8] // fallback: short UUID
}

// noResultsHint returns top entities so the model knows what's available.
func (t *KnowledgeGraphSearchTool) noResultsHint(ctx context.Context, agentID, userID, query string) *Result {
	top, _ := t.kgStore.ListEntities(ctx, agentID, userID, store.EntityListOptions{Limit: 10})
	if len(top) == 0 {
		return NewResult("Knowledge graph is empty. No entities have been extracted yet.")
	}
	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("No entities found matching %q. ", query))
	sb.WriteString(fmt.Sprintf("The knowledge graph has %d entities. Here are some available ones:\n\n", len(top)))
	for _, e := range top {
		sb.WriteString(fmt.Sprintf("- %s [%s] (id: %s)\n", e.Name, e.EntityType, e.ID))
	}
	sb.WriteString("\nTry searching with a specific name from the list above, or use query='*' to see all.")
	return NewResult(sb.String())
}
