package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"sort"
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
	return "Search the knowledge graph to find people, projects, organizations, and how they connect. Better than memory_search when you need: who works with whom, what projects someone is involved in, dependencies between tasks, or any multi-hop relationship question. Use specific names (e.g. 'Minh', 'GoClaw') — not generic words. Use query='*' to list all known entities, or combine query='*' with entity_type='project' to list known projects. Use entity_id to traverse connections from a specific entity."
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

	entityType, _ := args["entity_type"].(string)

	// List-all mode: query="*"
	if query == "*" {
		return t.executeListAll(ctx, agentID.String(), userID, entityType)
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

func (t *KnowledgeGraphSearchTool) executeListAll(ctx context.Context, agentID, userID, entityType string) *Result {
	limit := 30
	if entityType != "" {
		limit = 100
	}
	if entityType == "project" {
		limit = 200
	}

	entities, err := t.kgStore.ListEntities(ctx, agentID, userID, store.EntityListOptions{
		EntityType: entityType,
		Limit:      limit,
	})
	if err != nil {
		return ErrorResult(fmt.Sprintf("list entities failed: %v", err))
	}

	projectNames := make([]string, 0, len(entities))
	for _, e := range entities {
		if e.EntityType == "project" {
			projectNames = append(projectNames, e.Name)
		}
	}

	// Enrich entities with top relations when filtered and count is manageable
	type enrichedEntity struct {
		store.Entity
		TopRelations []string `json:"top_relations,omitempty"`
	}
	enriched := make([]enrichedEntity, len(entities))
	nameCache := make(map[string]string, len(entities)*2)
	for _, e := range entities {
		nameCache[e.ID] = e.Name
	}
	for idx, e := range entities {
		enriched[idx] = enrichedEntity{Entity: e}
		if entityType != "" && len(entities) <= 50 {
			rels, err := t.kgStore.ListRelations(ctx, agentID, userID, e.ID)
			if err == nil && len(rels) > 0 {
				cap := min(len(rels), 3)
				relStrs := make([]string, 0, cap)
				for _, rel := range rels[:cap] {
					srcName := t.resolveEntityName(ctx, agentID, userID, rel.SourceEntityID, nameCache)
					tgtName := t.resolveEntityName(ctx, agentID, userID, rel.TargetEntityID, nameCache)
					relStrs = append(relStrs, fmt.Sprintf("%s —[%s]→ %s", srcName, rel.RelationType, tgtName))
				}
				enriched[idx].TopRelations = relStrs
			}
		}
	}

	output := map[string]any{
		"mode":              "list_all",
		"entity_type":       entityType,
		"count":             len(entities),
		"has_results":       len(entities) > 0,
		"entities":          enriched,
		"matched_projects":  []store.Entity{},
		"project_names":     []string{},
		"has_project_match": false,
	}
	if len(projectNames) > 0 {
		output["matched_projects"] = entities
		output["project_names"] = projectNames
		output["has_project_match"] = true
	}

	// Compute status breakdown from properties
	statusBreakdown := make(map[string]int)
	ownersSet := make(map[string]bool)
	for _, e := range entities {
		status := ""
		if e.Properties != nil {
			status = e.Properties["status"]
			if owner := e.Properties["owner"]; owner != "" {
				ownersSet[owner] = true
			}
		}
		if status == "" {
			status = "unknown"
		}
		statusBreakdown[status]++
	}
	if len(statusBreakdown) > 0 {
		output["status_breakdown"] = statusBreakdown
	}
	output["total_with_owner"] = len(ownersSet)

	// Count global canonical entities for annotation
	globalCount := 0
	if userID != "" {
		for _, e := range entities {
			if e.UserID == "" {
				globalCount++
			}
		}
	}

	switch {
	case len(entities) == 0 && entityType != "":
		output["summary"] = fmt.Sprintf("Knowledge graph has no entities of type %q.", entityType)
	case len(entities) == 0:
		output["summary"] = "Knowledge graph is empty. No entities have been extracted yet."
	case entityType != "":
		summary := fmt.Sprintf("Knowledge graph has %d entities of type %q.", len(entities), entityType)
		if globalCount > 0 {
			summary += fmt.Sprintf(" (%d from global canonical)", globalCount)
		}
		if entityType == "project" && len(statusBreakdown) > 0 {
			parts := make([]string, 0, len(statusBreakdown))
			for status, count := range statusBreakdown {
				parts = append(parts, fmt.Sprintf("%s: %d", status, count))
			}
			sort.Strings(parts)
			summary += fmt.Sprintf(" Status: %s.", strings.Join(parts, ", "))
		}
		output["summary"] = summary
	default:
		summary := fmt.Sprintf("Knowledge graph has %d entities.", len(entities))
		if globalCount > 0 {
			summary += fmt.Sprintf(" (%d from global canonical)", globalCount)
		}
		output["summary"] = summary
		output["tip"] = "Use entity_id parameter to traverse relationships from a specific entity."
	}

	data, _ := json.MarshalIndent(output, "", "  ")
	return NewResult(string(data))
}

func (t *KnowledgeGraphSearchTool) executeSearch(ctx context.Context, agentID, userID, query string, args map[string]any) *Result {
	// Fetch extra results so name-based dedup still yields enough unique entities.
	const searchLimit = 10
	entities, err := t.kgStore.SearchEntities(ctx, agentID, userID, query, searchLimit*3)
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

	// Deduplicate entities with the same name+type, keeping the first (highest-scored).
	// Prevents duplicate entities from dominating the top results.
	entities = deduplicateEntitiesByName(entities, searchLimit)

	// Build entity name lookup for relation display
	entityNames := make(map[string]string, len(entities))
	for _, e := range entities {
		entityNames[e.ID] = e.Name
	}

	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("Found %d entities matching %q:\n\n", len(entities), query))
	for _, e := range entities {
		scope := ""
		if userID != "" && e.UserID == "" {
			scope = " [global]"
		}
		sb.WriteString(fmt.Sprintf("- %s [%s]%s (id: %s)\n", e.Name, e.EntityType, scope, e.ID))
		if e.Description != "" {
			sb.WriteString(fmt.Sprintf("  %s\n", e.Description))
		}

		// Show properties if non-empty
		if len(e.Properties) > 0 {
			propParts := make([]string, 0, len(e.Properties))
			for k, v := range e.Properties {
				if v != "" {
					propParts = append(propParts, fmt.Sprintf("%s: %s", k, v))
				}
			}
			if len(propParts) > 0 {
				sort.Strings(propParts)
				sb.WriteString(fmt.Sprintf("  [%s]\n", strings.Join(propParts, " | ")))
			}
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

// deduplicateEntitiesByName keeps only the first (highest-scored) entity per name+type pair.
// This prevents duplicate KG entities from monopolizing the top results.
func deduplicateEntitiesByName(entities []store.Entity, limit int) []store.Entity {
	type key struct{ name, etype string }
	seen := make(map[key]bool, len(entities))
	deduped := make([]store.Entity, 0, limit)
	for _, e := range entities {
		k := key{strings.ToLower(e.Name), e.EntityType}
		if seen[k] {
			continue
		}
		seen[k] = true
		deduped = append(deduped, e)
		if len(deduped) >= limit {
			break
		}
	}
	return deduped
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
