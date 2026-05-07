package tools

import (
	"context"
	"fmt"
	"log/slog"
	"slices"
	"strings"
	"time"

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
				"description": "Entity UUID to traverse relationships from. Get the UUID from a previous search result's (id: ...) field. Entity names are also accepted and will be auto-resolved.",
			},
			"max_depth": map[string]any{
				"type":        "number",
				"description": "Maximum traversal depth (default 2, max 5)",
			},
			"as_of": map[string]any{
				"type":        "string",
				"description": "ISO 8601 timestamp for point-in-time query (e.g. '2026-01-15T00:00:00Z'). Omit for current facts only.",
			},
			"from_time": map[string]any{
				"type":        "string",
				"description": "ISO 8601 start time for event-time range filter (e.g. '2026-04-01T00:00:00Z'). Returns only entities with event_time >= this value.",
			},
			"to_time": map[string]any{
				"type":        "string",
				"description": "ISO 8601 end time for event-time range filter. Returns only entities with event_time <= this value.",
			},
			"scope": map[string]any{
				"type":        "string",
				"description": "Graph scope ID to filter results (e.g. 'project-sovereign', 'project-gpu-elephant'). Lists entities within a specific knowledge graph scope.",
			},
		},
		"required": []string{},
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

	entityID, _ := args["entity_id"].(string)
	maxDepth := 2
	if md, ok := args["max_depth"].(float64); ok && md > 0 {
		maxDepth = min(int(md), 5)
	}

	// Traversal mode: entity_id provided
	if entityID != "" {
		return t.executeTraversal(ctx, agentID.String(), userID, entityID, maxDepth, query)
	}

	// Need query for all non-traversal modes
	if query == "" {
		return ErrorResult("query parameter is required (or use entity_id for traversal)")
	}

	// Parse temporal as_of parameter
	var temporal store.TemporalQueryOptions
	if asOfStr, ok := args["as_of"].(string); ok && asOfStr != "" {
		if asOf, err := time.Parse(time.RFC3339, asOfStr); err == nil {
			temporal.AsOf = &asOf
		}
	}

	// Parse event-time range parameters
	var fromTime, toTime *time.Time
	if ftStr, ok := args["from_time"].(string); ok && ftStr != "" {
		if ft, err := time.Parse(time.RFC3339, ftStr); err == nil {
			fromTime = &ft
		}
	}
	if ttStr, ok := args["to_time"].(string); ok && ttStr != "" {
		if tt, err := time.Parse(time.RFC3339, ttStr); err == nil {
			toTime = &tt
		}
	}

	// Event-time range mode: from_time or to_time provided
	if fromTime != nil || toTime != nil {
		return t.executeEventTimeSearch(ctx, agentID.String(), userID, query, fromTime, toTime)
	}

	// Scope mode: filter by graph scope ID
	scope, _ := args["scope"].(string)
	if scope != "" {
		return t.executeScopeSearch(ctx, agentID.String(), userID, scope, query, args, temporal)
	}

	// List-all mode: query="*"
	if query == "*" {
		return t.executeListAll(ctx, agentID.String(), userID, temporal)
	}

	// Search mode
	return t.executeSearch(ctx, agentID.String(), userID, query, args, temporal)
}

func (t *KnowledgeGraphSearchTool) executeTraversal(ctx context.Context, agentID, userID, entityID string, maxDepth int, query string) *Result {
	// Resolve entity_id: if not a valid UUID, search by name to find the actual UUID.
	if _, err := uuid.Parse(entityID); err != nil {
		resolved := t.resolveEntityIDByName(ctx, agentID, userID, entityID)
		if resolved != "" {
			entityID = resolved
		}
	}

	// Tier 1: outgoing deep traversal
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
		return t.executeSearch(ctx, agentID, userID, query, nil, store.TemporalQueryOptions{})
	}

	return NewResult(fmt.Sprintf("No connected entities found from entity_id=%q.", entityID))
}

func (t *KnowledgeGraphSearchTool) executeListAll(ctx context.Context, agentID, userID string, temporal store.TemporalQueryOptions) *Result {
	entities, err := t.kgStore.ListEntitiesTemporal(ctx, agentID, userID, store.EntityListOptions{Limit: 30}, temporal)
	if err != nil {
		return ErrorResult(fmt.Sprintf("list entities failed: %v", err))
	}
	if len(entities) == 0 {
		return NewResult("Knowledge graph is empty. No entities have been extracted yet.")
	}
	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("Knowledge graph has %d entities:\n\n", len(entities)))
	for _, e := range entities {
		sb.WriteString(fmt.Sprintf("- %s [%s] (id: %s)", e.Name, e.EntityType, e.ID))
		if e.EventTime != nil {
			sb.WriteString(fmt.Sprintf(" (event: %s)", e.EventTime.Format("2006-01-02 15:04")))
		}
		sb.WriteString("\n")
		if e.Description != "" {
			sb.WriteString(fmt.Sprintf("  %s\n", e.Description))
		}
		if p := formatProperties(e.Properties); p != "" {
			sb.WriteString(fmt.Sprintf("  [%s]\n", p))
		}
	}
	sb.WriteString("\nTip: Use entity_id parameter to traverse relationships from a specific entity.")
	return NewResult(sb.String())
}

// executeEventTimeSearch searches entities by event_time range, optionally filtered by query text.
func (t *KnowledgeGraphSearchTool) executeEventTimeSearch(ctx context.Context, agentID, userID, query string, fromTime, toTime *time.Time) *Result {
	entities, err := t.kgStore.SearchEntitiesByEventTime(ctx, agentID, userID, fromTime, toTime, 20)
	if err != nil {
		return ErrorResult(fmt.Sprintf("event-time search failed: %v", err))
	}
	if len(entities) == 0 {
		return NewResult("No entities with event_time found in the specified time range.")
	}

	// Optional text filter (post-search)
	if query != "" && query != "*" {
		filtered := entities[:0]
		for _, e := range entities {
			if matchesQuery(e, query) {
				filtered = append(filtered, e)
			}
		}
		entities = filtered
		if len(entities) == 0 {
			return NewResult(fmt.Sprintf("No entities matching %q found in the specified time range.", query))
		}
	}

	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("Found %d entities with event_time in range", len(entities)))
	if fromTime != nil {
		sb.WriteString(fmt.Sprintf(" from %s", fromTime.Format("2006-01-02 15:04")))
	}
	if toTime != nil {
		sb.WriteString(fmt.Sprintf(" to %s", toTime.Format("2006-01-02 15:04")))
	}
	sb.WriteString(":\n\n")
	for _, e := range entities {
		sb.WriteString(fmt.Sprintf("- %s [%s]", e.Name, e.EntityType))
		if e.EventTime != nil {
			sb.WriteString(fmt.Sprintf(" (event: %s)", e.EventTime.Format("2006-01-02 15:04")))
		}
		sb.WriteString("\n")
		if e.Description != "" {
			sb.WriteString(fmt.Sprintf("  %s\n", e.Description))
		}
		if p := formatProperties(e.Properties); p != "" {
			sb.WriteString(fmt.Sprintf("  [%s]\n", p))
		}
	}
	return NewResult(sb.String())
}

func (t *KnowledgeGraphSearchTool) executeSearch(ctx context.Context, agentID, userID, query string, args map[string]any, temporal store.TemporalQueryOptions) *Result {
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
		sb.WriteString(fmt.Sprintf("- %s [%s] (id: %s)", e.Name, e.EntityType, e.ID))
		if e.EventTime != nil {
			sb.WriteString(fmt.Sprintf(" (event: %s)", e.EventTime.Format("2006-01-02 15:04")))
		}
		sb.WriteString("\n")
		if e.Description != "" {
			sb.WriteString(fmt.Sprintf("  %s\n", e.Description))
		}
		if p := formatProperties(e.Properties); p != "" {
			sb.WriteString(fmt.Sprintf("  [%s]\n", p))
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

// resolveEntityIDByName searches for an entity by name and returns its UUID.
// Returns empty string if no unique match found.
func (t *KnowledgeGraphSearchTool) resolveEntityIDByName(ctx context.Context, agentID, userID, name string) string {
	entities, err := t.kgStore.SearchEntities(ctx, agentID, userID, name, 5)
	if err != nil || len(entities) == 0 {
		return ""
	}
	// Prefer exact name match
	lower := strings.ToLower(name)
	for _, e := range entities {
		if strings.ToLower(e.Name) == lower {
			return e.ID
		}
	}
	// Fall back to first result if only one found
	if len(entities) == 1 {
		return entities[0].ID
	}
	return ""
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

// formatProperties returns a compact "key: value" line from entity properties.
func formatProperties(props map[string]string) string {
	if len(props) == 0 {
		return ""
	}
	keys := make([]string, 0, len(props))
	for k := range props {
		keys = append(keys, k)
	}
	slices.Sort(keys)
	parts := make([]string, len(keys))
	for i, k := range keys {
		parts[i] = k + ": " + props[k]
	}
	return strings.Join(parts, ", ")
}

// matchesQuery checks if an entity matches a multi-word query.
// Each word must appear in the entity's name, description, or properties.
func matchesQuery(e store.Entity, query string) bool {
	words := strings.Fields(strings.ToLower(query))
	if len(words) == 0 {
		return true
	}
	var text strings.Builder
	text.WriteString(strings.ToLower(e.Name + " " + e.Description))
	for k, v := range e.Properties {
		text.WriteString(" " + k + " " + v)
	}
	for _, w := range words {
		if !strings.Contains(text.String(), w) {
			return false
		}
	}
	return true
}

// noResultsHint returns top entities so the model knows what's available.
// Falls back to scope-based listing if the query matches a graph scope ID.
func (t *KnowledgeGraphSearchTool) noResultsHint(ctx context.Context, agentID, userID, query string) *Result {
	// Fallback: try matching query as a graph scope ID (user_id)
	if query != "" && query != "*" {
		scopeEntities, scopeErr := t.kgStore.ListEntities(ctx, agentID, userID, store.EntityListOptions{
			ScopeID: query,
			Limit:   15,
		})
		if scopeErr == nil && len(scopeEntities) > 0 {
			var sb strings.Builder
			sb.WriteString(fmt.Sprintf("Found %d entities in scope %q:\n\n", len(scopeEntities), query))
			for _, e := range scopeEntities {
				sb.WriteString(fmt.Sprintf("- %s [%s]", e.Name, e.EntityType))
				if e.EventTime != nil {
					sb.WriteString(fmt.Sprintf(" (event: %s)", e.EventTime.Format("2006-01-02 15:04")))
				}
				sb.WriteString("\n")
				if e.Description != "" {
					sb.WriteString(fmt.Sprintf("  %s\n", e.Description))
				}
				if p := formatProperties(e.Properties); p != "" {
					sb.WriteString(fmt.Sprintf("  [%s]\n", p))
				}
			}
			sb.WriteString(fmt.Sprintf("\nTip: Use entity_id parameter to traverse relationships from a specific entity."))
			return NewResult(sb.String())
		}
	}

	top, _ := t.kgStore.ListEntities(ctx, agentID, userID, store.EntityListOptions{Limit: 10})
	if len(top) == 0 {
		return NewResult("Knowledge graph is empty. No entities have been extracted yet.")
	}
	total := len(top)
	if stats, serr := t.kgStore.Stats(ctx, agentID, userID); serr == nil && stats.EntityCount > 0 {
		total = stats.EntityCount
	}
	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("No entities found matching %q. ", query))
	sb.WriteString(fmt.Sprintf("The knowledge graph has %d entities. Here are some available ones:\n\n", total))
	for _, e := range top {
		sb.WriteString(fmt.Sprintf("- %s [%s] (id: %s)", e.Name, e.EntityType, e.ID))
		if e.EventTime != nil {
			sb.WriteString(fmt.Sprintf(" (event: %s)", e.EventTime.Format("2006-01-02 15:04")))
		}
		sb.WriteString("\n")
	}
	sb.WriteString("\nTry searching with a specific name from the list above, or use query='*' to see all.")
	return NewResult(sb.String())
}

// executeScopeSearch lists entities within a specific graph scope, optionally filtered by query text.
func (t *KnowledgeGraphSearchTool) executeScopeSearch(ctx context.Context, agentID, userID, scope, query string, args map[string]any, temporal store.TemporalQueryOptions) *Result {
	entityType, _ := args["entity_type"].(string)
	opts := store.EntityListOptions{
		ScopeID:    scope,
		EntityType: entityType,
		Limit:      30,
	}

	var entities []store.Entity
	var err error
	if temporal.AsOf != nil {
		entities, err = t.kgStore.ListEntitiesTemporal(ctx, agentID, userID, opts, temporal)
	} else {
		entities, err = t.kgStore.ListEntities(ctx, agentID, userID, opts)
	}
	if err != nil {
		return ErrorResult(fmt.Sprintf("scope search failed: %v", err))
	}
	if len(entities) == 0 {
		return NewResult(fmt.Sprintf("No entities found in scope %q.", scope))
	}

	// Optional text filter (post-search)
	if query != "" && query != "*" {
		filtered := entities[:0]
		for _, e := range entities {
			if matchesQuery(e, query) {
				filtered = append(filtered, e)
			}
		}
		entities = filtered
		if len(entities) == 0 {
			return NewResult(fmt.Sprintf("No entities matching %q found in scope %q.", query, scope))
		}
	}

	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("Found %d entities in scope %q:\n\n", len(entities), scope))
	for _, e := range entities {
		sb.WriteString(fmt.Sprintf("- %s [%s] (id: %s)", e.Name, e.EntityType, e.ID))
		if e.EventTime != nil {
			sb.WriteString(fmt.Sprintf(" (event: %s)", e.EventTime.Format("2006-01-02 15:04")))
		}
		sb.WriteString("\n")
		if e.Description != "" {
			sb.WriteString(fmt.Sprintf("  %s\n", e.Description))
		}
		if p := formatProperties(e.Properties); p != "" {
			sb.WriteString(fmt.Sprintf("  [%s]\n", p))
		}
	}
	sb.WriteString("\nTip: Use entity_id parameter to traverse relationships from a specific entity.")
	return NewResult(sb.String())
}
