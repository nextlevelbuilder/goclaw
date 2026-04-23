package tools

import (
	"context"
	"fmt"
	"strings"

	"github.com/google/uuid"

	"github.com/nextlevelbuilder/goclaw/internal/store"
	"github.com/nextlevelbuilder/goclaw/internal/vault"
)

// VaultSearchTool provides unified search across all knowledge sources.
type VaultSearchTool struct {
	searchSvc *vault.VaultSearchService
}

func NewVaultSearchTool() *VaultSearchTool {
	return &VaultSearchTool{}
}

func (t *VaultSearchTool) SetSearchService(svc *vault.VaultSearchService) {
	t.searchSvc = svc
}

func (t *VaultSearchTool) Name() string { return "vault_search" }

func (t *VaultSearchTool) Description() string {
	return "Primary discovery tool: search across ALL knowledge sources (vault docs, memory, knowledge graph). Each result carries a source-specific id field (doc_id / entity_id / episodic_id) that matches the input param of its follow-up tool — pass doc_id to vault_read, entity_id to knowledge_graph_search, episodic_id to memory_expand. Narrow the search with types=\"context,note\" or types=\"kg\" when relevant."
}

// sourceIDField returns the (field-name, follow-up-hint) pair for a result
// source. Field names intentionally match the input param of the follow-up
// tool so the LLM cannot misroute an id: `doc_id` is vault_read's param,
// `entity_id` is knowledge_graph_search's param, `episodic_id` pairs with
// memory_expand's `id` param (kept distinct to signal namespace).
func sourceIDField(source string) (field, hint string) {
	switch source {
	case "vault":
		return "doc_id", " → vault_read(doc_id)"
	case "kg":
		return "entity_id", " → knowledge_graph_search(entity_id) — NOT vault_read"
	case "episodic":
		return "episodic_id", " → memory_expand(id=episodic_id) — NOT vault_read"
	default:
		return "id", ""
	}
}

func (t *VaultSearchTool) Parameters() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"query": map[string]any{
				"type":        "string",
				"description": "Natural language search query",
			},
			"scope": map[string]any{
				"type":        "string",
				"description": "Scope filter: personal, team, or shared (default: all)",
			},
			"types": map[string]any{
				"type":        "string",
				"description": "Comma-separated doc types: context, memory, note, skill, episodic, kg (default: all sources)",
			},
			"maxResults": map[string]any{
				"type":        "number",
				"description": "Maximum results (default: 10)",
			},
		},
		"required": []string{"query"},
	}
}

func (t *VaultSearchTool) Execute(ctx context.Context, args map[string]any) *Result {
	query, _ := args["query"].(string)
	if query == "" {
		return ErrorResult("query parameter is required")
	}

	agentID := store.AgentIDFromContext(ctx)
	tenantID := store.TenantIDFromContext(ctx)
	if t.searchSvc == nil || agentID == uuid.Nil {
		return ErrorResult("vault search not available")
	}

	userID := store.MemoryUserID(ctx)
	opts := vault.UnifiedSearchOptions{
		Query:    query,
		AgentID:  agentID.String(),
		UserID:   userID,
		TenantID: tenantID.String(),
	}
	// Team context from RunContext — cannot be spoofed via tool args.
	if rc := store.RunContextFromCtx(ctx); rc != nil && rc.TeamID != "" {
		opts.TeamID = &rc.TeamID
	}

	if scope, ok := args["scope"].(string); ok && scope != "" {
		opts.Scope = scope
	}
	if types, ok := args["types"].(string); ok && types != "" {
		for t := range strings.SplitSeq(types, ",") {
			opts.DocTypes = append(opts.DocTypes, strings.TrimSpace(t))
		}
	}
	if mr, ok := args["maxResults"].(float64); ok && mr > 0 {
		opts.MaxResults = int(mr)
	}

	results, err := t.searchSvc.Search(ctx, opts)
	if err != nil {
		return ErrorResult(fmt.Sprintf("vault search failed: %v", err))
	}

	if len(results) == 0 {
		return NewResult("No results found. Try memory_search for memory-specific queries or kg_search for relationship traversal.")
	}

	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("Found %d results:\n\n", len(results)))
	for i, r := range results {
		sb.WriteString(fmt.Sprintf("%d. [%s] %s", i+1, r.Source, r.Title))
		if r.Path != "" {
			sb.WriteString(fmt.Sprintf(" (%s)", r.Path))
		}
		sb.WriteString(fmt.Sprintf(" — score: %.2f", r.Score))
		field, hint := sourceIDField(r.Source)
		if r.ID != "" {
			sb.WriteString(fmt.Sprintf(" — %s: %s", field, r.ID))
		}
		if hint != "" {
			sb.WriteString(hint)
		}
		if r.Snippet != "" {
			sb.WriteString(fmt.Sprintf("\n   %s", r.Snippet))
		}
		sb.WriteByte('\n')
	}
	return NewResult(sb.String())
}
