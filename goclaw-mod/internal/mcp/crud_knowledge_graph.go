package mcp

import (
	"context"
	"encoding/json"
	"fmt"

	mcpgo "github.com/mark3labs/mcp-go/mcp"
	mcpserver "github.com/mark3labs/mcp-go/server"

	"github.com/nextlevelbuilder/goclaw/internal/store"
)

// registerKnowledgeGraphCRUDTools registers the goclaw_kg_* MCP tools backed
// by store.KnowledgeGraphStore — closes the `goclaw kg` CLI-vs-MCP coverage
// gap: read/inspect (entities/search/traverse/relations/stats), direct
// writes (link = upsert relation, entity upsert/delete), and the dedup
// family (scan/list/merge/dismiss). "extract" itself (running the LLM-driven
// extraction pipeline over free text) is intentionally not wrapped here —
// internal/knowledgegraph's extractor needs a resolved LLM provider/model
// and produces the same Entity/Relation shapes this surface already accepts
// via goclaw_kg_ingest, so a caller can run extraction itself (e.g. via
// goclaw_llm_complete) and hand the result to goclaw_kg_ingest.
func registerKnowledgeGraphCRUDTools(srv *mcpserver.MCPServer, kg store.KnowledgeGraphStore) {
	srv.AddTool(mcpgo.NewTool("goclaw_kg_entities_list",
		mcpgo.WithDescription("List knowledge graph entities for an agent/user scope."),
		mcpgo.WithString("agent_id", mcpgo.Required(), mcpgo.Description("Agent ID scope.")),
		mcpgo.WithString("user_id", mcpgo.Description("User ID scope; empty for agent-global entities.")),
		mcpgo.WithString("entity_type", mcpgo.Description("Filter by entity type.")),
		mcpgo.WithNumber("limit", mcpgo.Description("Maximum entities to return.")),
		mcpgo.WithNumber("offset", mcpgo.Description("Pagination offset.")),
		mcpgo.WithReadOnlyHintAnnotation(true),
	), handleKGEntitiesList(kg))

	srv.AddTool(mcpgo.NewTool("goclaw_kg_entity_get",
		mcpgo.WithDescription("Get a single knowledge graph entity by ID."),
		mcpgo.WithString("agent_id", mcpgo.Required(), mcpgo.Description("Agent ID scope.")),
		mcpgo.WithString("user_id", mcpgo.Description("User ID scope; empty for agent-global entities.")),
		mcpgo.WithString("entity_id", mcpgo.Required(), mcpgo.Description("Entity ID.")),
		mcpgo.WithReadOnlyHintAnnotation(true),
	), handleKGEntityGet(kg))

	srv.AddTool(mcpgo.NewTool("goclaw_kg_search",
		mcpgo.WithDescription("Search knowledge graph entities by name/description."),
		mcpgo.WithString("agent_id", mcpgo.Required(), mcpgo.Description("Agent ID scope.")),
		mcpgo.WithString("user_id", mcpgo.Description("User ID scope; empty for agent-global entities.")),
		mcpgo.WithString("query", mcpgo.Required(), mcpgo.Description("Search query.")),
		mcpgo.WithNumber("limit", mcpgo.Description("Maximum results to return.")),
		mcpgo.WithReadOnlyHintAnnotation(true),
	), handleKGSearch(kg))

	srv.AddTool(mcpgo.NewTool("goclaw_kg_traverse",
		mcpgo.WithDescription("Traverse the knowledge graph outward from a starting entity."),
		mcpgo.WithString("agent_id", mcpgo.Required(), mcpgo.Description("Agent ID scope.")),
		mcpgo.WithString("user_id", mcpgo.Description("User ID scope; empty for agent-global entities.")),
		mcpgo.WithString("entity_id", mcpgo.Required(), mcpgo.Description("Starting entity ID.")),
		mcpgo.WithNumber("max_depth", mcpgo.Description("Maximum traversal depth; defaults to 2.")),
		mcpgo.WithReadOnlyHintAnnotation(true),
	), handleKGTraverse(kg))

	srv.AddTool(mcpgo.NewTool("goclaw_kg_relations_list",
		mcpgo.WithDescription("List relations for an entity, or all relations for the scope if entity_id is omitted."),
		mcpgo.WithString("agent_id", mcpgo.Required(), mcpgo.Description("Agent ID scope.")),
		mcpgo.WithString("user_id", mcpgo.Description("User ID scope; empty for agent-global entities.")),
		mcpgo.WithString("entity_id", mcpgo.Description("Entity ID; omit to list all relations in scope.")),
		mcpgo.WithNumber("limit", mcpgo.Description("Maximum results when entity_id is omitted.")),
		mcpgo.WithReadOnlyHintAnnotation(true),
	), handleKGRelationsList(kg))

	srv.AddTool(mcpgo.NewTool("goclaw_kg_stats",
		mcpgo.WithDescription("Get aggregate knowledge graph stats (entity/relation counts by type) for an agent/user scope."),
		mcpgo.WithString("agent_id", mcpgo.Required(), mcpgo.Description("Agent ID scope.")),
		mcpgo.WithString("user_id", mcpgo.Description("User ID scope; empty for agent-global entities.")),
		mcpgo.WithReadOnlyHintAnnotation(true),
	), handleKGStats(kg))

	srv.AddTool(mcpgo.NewTool("goclaw_kg_ingest",
		mcpgo.WithDescription("Upsert a batch of entities and relations (e.g. output of an LLM extraction pass) into the knowledge graph."),
		mcpgo.WithString("agent_id", mcpgo.Required(), mcpgo.Description("Agent ID scope.")),
		mcpgo.WithString("user_id", mcpgo.Description("User ID scope; empty for agent-global entities.")),
		mcpgo.WithArray("entities", mcpgo.Description("Entities to upsert (objects matching the Entity shape from goclaw_kg_entities_list).")),
		mcpgo.WithArray("relations", mcpgo.Description("Relations to upsert (objects matching the Relation shape from goclaw_kg_relations_list).")),
	), handleKGIngest(kg))

	srv.AddTool(mcpgo.NewTool("goclaw_kg_entity_delete",
		mcpgo.WithDescription("Delete a single knowledge graph entity."),
		mcpgo.WithString("agent_id", mcpgo.Required(), mcpgo.Description("Agent ID scope.")),
		mcpgo.WithString("user_id", mcpgo.Description("User ID scope; empty for agent-global entities.")),
		mcpgo.WithString("entity_id", mcpgo.Required(), mcpgo.Description("Entity ID to delete.")),
		mcpgo.WithDestructiveHintAnnotation(true),
	), handleKGEntityDelete(kg))

	srv.AddTool(mcpgo.NewTool("goclaw_kg_relation_delete",
		mcpgo.WithDescription("Delete a single knowledge graph relation."),
		mcpgo.WithString("agent_id", mcpgo.Required(), mcpgo.Description("Agent ID scope.")),
		mcpgo.WithString("user_id", mcpgo.Description("User ID scope; empty for agent-global entities.")),
		mcpgo.WithString("relation_id", mcpgo.Required(), mcpgo.Description("Relation ID to delete.")),
		mcpgo.WithDestructiveHintAnnotation(true),
	), handleKGRelationDelete(kg))

	srv.AddTool(mcpgo.NewTool("goclaw_kg_prune",
		mcpgo.WithDescription("Delete entities below a confidence threshold."),
		mcpgo.WithString("agent_id", mcpgo.Required(), mcpgo.Description("Agent ID scope.")),
		mcpgo.WithString("user_id", mcpgo.Description("User ID scope; empty for agent-global entities.")),
		mcpgo.WithNumber("min_confidence", mcpgo.Required(), mcpgo.Description("Entities with confidence below this value are deleted.")),
		mcpgo.WithDestructiveHintAnnotation(true),
	), handleKGPrune(kg))

	srv.AddTool(mcpgo.NewTool("goclaw_kg_dedup_scan",
		mcpgo.WithDescription("Scan all entities with embeddings for near-duplicates, flagging candidates above a similarity threshold for review."),
		mcpgo.WithString("agent_id", mcpgo.Required(), mcpgo.Description("Agent ID scope.")),
		mcpgo.WithString("user_id", mcpgo.Description("User ID scope; empty for agent-global entities.")),
		mcpgo.WithNumber("threshold", mcpgo.Description("Similarity threshold (0-1); defaults to 0.90.")),
		mcpgo.WithNumber("limit", mcpgo.Description("Maximum candidates to flag; defaults to 100.")),
	), handleKGDedupScan(kg))

	srv.AddTool(mcpgo.NewTool("goclaw_kg_dedup_candidates",
		mcpgo.WithDescription("List pending dedup candidates for review."),
		mcpgo.WithString("agent_id", mcpgo.Required(), mcpgo.Description("Agent ID scope.")),
		mcpgo.WithString("user_id", mcpgo.Description("User ID scope; empty for agent-global entities.")),
		mcpgo.WithNumber("limit", mcpgo.Description("Maximum candidates to return; defaults to 50.")),
		mcpgo.WithReadOnlyHintAnnotation(true),
	), handleKGDedupCandidates(kg))

	srv.AddTool(mcpgo.NewTool("goclaw_kg_merge_entities",
		mcpgo.WithDescription("Merge one entity into another: relations are re-pointed to the target and the source entity is deleted."),
		mcpgo.WithString("agent_id", mcpgo.Required(), mcpgo.Description("Agent ID scope.")),
		mcpgo.WithString("user_id", mcpgo.Description("User ID scope; empty for agent-global entities.")),
		mcpgo.WithString("target_id", mcpgo.Required(), mcpgo.Description("Entity ID to keep.")),
		mcpgo.WithString("source_id", mcpgo.Required(), mcpgo.Description("Entity ID to merge into target and delete.")),
		mcpgo.WithDestructiveHintAnnotation(true),
	), handleKGMergeEntities(kg))

	srv.AddTool(mcpgo.NewTool("goclaw_kg_dismiss_candidate",
		mcpgo.WithDescription("Dismiss a dedup candidate as not a duplicate."),
		mcpgo.WithString("agent_id", mcpgo.Required(), mcpgo.Description("Agent ID scope.")),
		mcpgo.WithString("candidate_id", mcpgo.Required(), mcpgo.Description("Dedup candidate ID.")),
	), handleKGDismissCandidate(kg))
}

func handleKGEntitiesList(kg store.KnowledgeGraphStore) mcpserver.ToolHandlerFunc {
	return func(ctx context.Context, req mcpgo.CallToolRequest) (*mcpgo.CallToolResult, error) {
		agentID, err := req.RequireString("agent_id")
		if err != nil {
			return toolError("kg.entities_list", err)
		}
		userID := req.GetString("user_id", "")
		opts := store.EntityListOptions{
			EntityType: req.GetString("entity_type", ""),
			Limit:      intArg(req, "limit", 0),
			Offset:     intArg(req, "offset", 0),
		}
		entities, err := kg.ListEntities(ctx, agentID, userID, opts)
		if err != nil {
			return toolError("kg.entities_list", err)
		}
		return jsonToolResult(entities)
	}
}

func handleKGEntityGet(kg store.KnowledgeGraphStore) mcpserver.ToolHandlerFunc {
	return func(ctx context.Context, req mcpgo.CallToolRequest) (*mcpgo.CallToolResult, error) {
		agentID, err := req.RequireString("agent_id")
		if err != nil {
			return toolError("kg.entity_get", err)
		}
		entityID, err := req.RequireString("entity_id")
		if err != nil {
			return toolError("kg.entity_get", err)
		}
		userID := req.GetString("user_id", "")
		entity, err := kg.GetEntity(ctx, agentID, userID, entityID)
		if err != nil {
			return toolError("kg.entity_get", err)
		}
		return jsonToolResult(entity)
	}
}

func handleKGSearch(kg store.KnowledgeGraphStore) mcpserver.ToolHandlerFunc {
	return func(ctx context.Context, req mcpgo.CallToolRequest) (*mcpgo.CallToolResult, error) {
		agentID, err := req.RequireString("agent_id")
		if err != nil {
			return toolError("kg.search", err)
		}
		query, err := req.RequireString("query")
		if err != nil {
			return toolError("kg.search", err)
		}
		userID := req.GetString("user_id", "")
		limit := intArg(req, "limit", 20)
		entities, err := kg.SearchEntities(ctx, agentID, userID, query, limit)
		if err != nil {
			return toolError("kg.search", err)
		}
		return jsonToolResult(entities)
	}
}

func handleKGTraverse(kg store.KnowledgeGraphStore) mcpserver.ToolHandlerFunc {
	return func(ctx context.Context, req mcpgo.CallToolRequest) (*mcpgo.CallToolResult, error) {
		agentID, err := req.RequireString("agent_id")
		if err != nil {
			return toolError("kg.traverse", err)
		}
		entityID, err := req.RequireString("entity_id")
		if err != nil {
			return toolError("kg.traverse", err)
		}
		userID := req.GetString("user_id", "")
		maxDepth := intArg(req, "max_depth", 2)
		results, err := kg.Traverse(ctx, agentID, userID, entityID, maxDepth)
		if err != nil {
			return toolError("kg.traverse", err)
		}
		return jsonToolResult(results)
	}
}

func handleKGRelationsList(kg store.KnowledgeGraphStore) mcpserver.ToolHandlerFunc {
	return func(ctx context.Context, req mcpgo.CallToolRequest) (*mcpgo.CallToolResult, error) {
		agentID, err := req.RequireString("agent_id")
		if err != nil {
			return toolError("kg.relations_list", err)
		}
		userID := req.GetString("user_id", "")
		entityID := req.GetString("entity_id", "")
		if entityID != "" {
			relations, err := kg.ListRelations(ctx, agentID, userID, entityID)
			if err != nil {
				return toolError("kg.relations_list", err)
			}
			return jsonToolResult(relations)
		}
		limit := intArg(req, "limit", 100)
		relations, err := kg.ListAllRelations(ctx, agentID, userID, limit)
		if err != nil {
			return toolError("kg.relations_list", err)
		}
		return jsonToolResult(relations)
	}
}

func handleKGStats(kg store.KnowledgeGraphStore) mcpserver.ToolHandlerFunc {
	return func(ctx context.Context, req mcpgo.CallToolRequest) (*mcpgo.CallToolResult, error) {
		agentID, err := req.RequireString("agent_id")
		if err != nil {
			return toolError("kg.stats", err)
		}
		userID := req.GetString("user_id", "")
		stats, err := kg.Stats(ctx, agentID, userID)
		if err != nil {
			return toolError("kg.stats", err)
		}
		return jsonToolResult(stats)
	}
}

// intArg reads an integer-valued argument (MCP numbers decode as float64),
// returning fallback when absent.
func intArg(req mcpgo.CallToolRequest, key string, fallback int) int {
	if v, ok := req.GetArguments()[key]; ok {
		if f, ok := v.(float64); ok {
			return int(f)
		}
	}
	return fallback
}

// remarshalEntities/remarshalRelations parse a generic JSON array argument
// into typed slices via a JSON round-trip (same technique as
// remarshalInto in crud_agents_export.go).
func remarshalEntities(raw any) ([]store.Entity, error) {
	if raw == nil {
		return nil, nil
	}
	var out []store.Entity
	data, err := json.Marshal(raw)
	if err != nil {
		return nil, err
	}
	if err := json.Unmarshal(data, &out); err != nil {
		return nil, err
	}
	return out, nil
}

func remarshalRelations(raw any) ([]store.Relation, error) {
	if raw == nil {
		return nil, nil
	}
	var out []store.Relation
	data, err := json.Marshal(raw)
	if err != nil {
		return nil, err
	}
	if err := json.Unmarshal(data, &out); err != nil {
		return nil, err
	}
	return out, nil
}

func handleKGIngest(kg store.KnowledgeGraphStore) mcpserver.ToolHandlerFunc {
	return func(ctx context.Context, req mcpgo.CallToolRequest) (*mcpgo.CallToolResult, error) {
		agentID, err := req.RequireString("agent_id")
		if err != nil {
			return toolError("kg.ingest", err)
		}
		userID := req.GetString("user_id", "")
		args := req.GetArguments()

		entities, err := remarshalEntities(args["entities"])
		if err != nil {
			return toolError("kg.ingest", fmt.Errorf("invalid entities: %w", err))
		}
		relations, err := remarshalRelations(args["relations"])
		if err != nil {
			return toolError("kg.ingest", fmt.Errorf("invalid relations: %w", err))
		}
		if len(entities) == 0 && len(relations) == 0 {
			return mcpgo.NewToolResultError("kg.ingest: at least one of entities or relations is required"), nil
		}

		ids, err := kg.IngestExtraction(ctx, agentID, userID, entities, relations)
		if err != nil {
			return toolError("kg.ingest", err)
		}
		return jsonToolResult(map[string]any{"entity_ids": ids})
	}
}

func handleKGEntityDelete(kg store.KnowledgeGraphStore) mcpserver.ToolHandlerFunc {
	return func(ctx context.Context, req mcpgo.CallToolRequest) (*mcpgo.CallToolResult, error) {
		agentID, err := req.RequireString("agent_id")
		if err != nil {
			return toolError("kg.entity_delete", err)
		}
		entityID, err := req.RequireString("entity_id")
		if err != nil {
			return toolError("kg.entity_delete", err)
		}
		userID := req.GetString("user_id", "")
		if err := kg.DeleteEntity(ctx, agentID, userID, entityID); err != nil {
			return toolError("kg.entity_delete", err)
		}
		return jsonToolResult(map[string]string{"ok": "true"})
	}
}

func handleKGRelationDelete(kg store.KnowledgeGraphStore) mcpserver.ToolHandlerFunc {
	return func(ctx context.Context, req mcpgo.CallToolRequest) (*mcpgo.CallToolResult, error) {
		agentID, err := req.RequireString("agent_id")
		if err != nil {
			return toolError("kg.relation_delete", err)
		}
		relationID, err := req.RequireString("relation_id")
		if err != nil {
			return toolError("kg.relation_delete", err)
		}
		userID := req.GetString("user_id", "")
		if err := kg.DeleteRelation(ctx, agentID, userID, relationID); err != nil {
			return toolError("kg.relation_delete", err)
		}
		return jsonToolResult(map[string]string{"ok": "true"})
	}
}

func handleKGPrune(kg store.KnowledgeGraphStore) mcpserver.ToolHandlerFunc {
	return func(ctx context.Context, req mcpgo.CallToolRequest) (*mcpgo.CallToolResult, error) {
		agentID, err := req.RequireString("agent_id")
		if err != nil {
			return toolError("kg.prune", err)
		}
		userID := req.GetString("user_id", "")
		var minConfidence float64
		if v, ok := req.GetArguments()["min_confidence"]; ok {
			if f, ok := v.(float64); ok {
				minConfidence = f
			}
		} else {
			return mcpgo.NewToolResultError("kg.prune: min_confidence is required"), nil
		}
		n, err := kg.PruneByConfidence(ctx, agentID, userID, minConfidence)
		if err != nil {
			return toolError("kg.prune", err)
		}
		return jsonToolResult(map[string]int{"deleted": n})
	}
}

func handleKGDedupScan(kg store.KnowledgeGraphStore) mcpserver.ToolHandlerFunc {
	return func(ctx context.Context, req mcpgo.CallToolRequest) (*mcpgo.CallToolResult, error) {
		agentID, err := req.RequireString("agent_id")
		if err != nil {
			return toolError("kg.dedup_scan", err)
		}
		userID := req.GetString("user_id", "")
		threshold := 0.90
		if v, ok := req.GetArguments()["threshold"]; ok {
			if f, ok := v.(float64); ok {
				threshold = f
			}
		}
		limit := intArg(req, "limit", 100)
		n, err := kg.ScanDuplicates(ctx, agentID, userID, threshold, limit)
		if err != nil {
			return toolError("kg.dedup_scan", err)
		}
		return jsonToolResult(map[string]int{"flagged": n})
	}
}

func handleKGDedupCandidates(kg store.KnowledgeGraphStore) mcpserver.ToolHandlerFunc {
	return func(ctx context.Context, req mcpgo.CallToolRequest) (*mcpgo.CallToolResult, error) {
		agentID, err := req.RequireString("agent_id")
		if err != nil {
			return toolError("kg.dedup_candidates", err)
		}
		userID := req.GetString("user_id", "")
		limit := intArg(req, "limit", 50)
		candidates, err := kg.ListDedupCandidates(ctx, agentID, userID, limit)
		if err != nil {
			return toolError("kg.dedup_candidates", err)
		}
		return jsonToolResult(candidates)
	}
}

func handleKGMergeEntities(kg store.KnowledgeGraphStore) mcpserver.ToolHandlerFunc {
	return func(ctx context.Context, req mcpgo.CallToolRequest) (*mcpgo.CallToolResult, error) {
		agentID, err := req.RequireString("agent_id")
		if err != nil {
			return toolError("kg.merge_entities", err)
		}
		targetID, err := req.RequireString("target_id")
		if err != nil {
			return toolError("kg.merge_entities", err)
		}
		sourceID, err := req.RequireString("source_id")
		if err != nil {
			return toolError("kg.merge_entities", err)
		}
		userID := req.GetString("user_id", "")
		if err := kg.MergeEntities(ctx, agentID, userID, targetID, sourceID); err != nil {
			return toolError("kg.merge_entities", err)
		}
		return jsonToolResult(map[string]string{"ok": "true"})
	}
}

func handleKGDismissCandidate(kg store.KnowledgeGraphStore) mcpserver.ToolHandlerFunc {
	return func(ctx context.Context, req mcpgo.CallToolRequest) (*mcpgo.CallToolResult, error) {
		agentID, err := req.RequireString("agent_id")
		if err != nil {
			return toolError("kg.dismiss_candidate", err)
		}
		candidateID, err := req.RequireString("candidate_id")
		if err != nil {
			return toolError("kg.dismiss_candidate", err)
		}
		if err := kg.DismissCandidate(ctx, agentID, candidateID); err != nil {
			return toolError("kg.dismiss_candidate", err)
		}
		return jsonToolResult(map[string]string{"ok": "true"})
	}
}
