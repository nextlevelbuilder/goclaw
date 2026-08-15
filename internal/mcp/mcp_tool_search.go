package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"

	"github.com/nextlevelbuilder/goclaw/internal/tools"
)

// MCPToolSearchTool searches deferred MCP tools by keyword using BM25.
// Registered instead of individual MCP tools when the total count exceeds
// the inline threshold (search mode). Discovered tools are activated in
// the registry and become available on the next agent loop iteration.
type MCPToolSearchTool struct {
	manager *Manager
	index   *mcpBM25Index
}

// NewMCPToolSearchTool creates an mcp_tool_search tool backed by BM25.
func NewMCPToolSearchTool(mgr *Manager) *MCPToolSearchTool {
	t := &MCPToolSearchTool{
		manager: mgr,
		index:   newMCPBM25Index(),
	}
	t.rebuildIndex()
	return t
}

func (t *MCPToolSearchTool) rebuildIndex() {
	deferred := t.manager.DeferredToolInfos()
	t.index.build(deferred)
	slog.Debug("mcp_tool_search.index_built", "tools", len(deferred))
}

func (t *MCPToolSearchTool) Name() string { return "mcp_tool_search" }

func (t *MCPToolSearchTool) Description() string {
	return "Search for available external integration tools (MCP) by keyword. " +
		"IMPORTANT: You have access to external service integrations " +
		"(databases, APIs, file systems, messaging, etc.) through MCP tools " +
		"that are NOT loaded by default. Before performing any external service " +
		"operation, you MUST search here first to discover available tools. " +
		"Use English keywords describing what you need " +
		"(e.g. 'database query', 'create issue', 'send email'). " +
		"Discovered tools become immediately available for use."
}

func (t *MCPToolSearchTool) Parameters() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"query": map[string]any{
				"type":        "string",
				"description": "English keywords describing the operation you need (e.g. 'create github issue', 'query postgres', 'send slack message')",
			},
			"max_results": map[string]any{
				"type":        "integer",
				"description": "Maximum number of tools to return (default: 5)",
			},
		},
		"required": []string{"query"},
	}
}

func (t *MCPToolSearchTool) Execute(ctx context.Context, args map[string]any) *tools.Result {
	query, _ := args["query"].(string)
	if query == "" {
		return tools.ErrorResult("query parameter is required")
	}

	maxResults := 5
	if mr, ok := args["max_results"].(float64); ok && int(mr) > 0 {
		maxResults = int(mr)
	}

	results := t.index.search(query, maxResults)

	slog.Info("mcp_tool_search", "query", query, "results", len(results))

	if len(results) == 0 {
		return tools.NewResult(fmt.Sprintf(
			"No MCP tools found matching: %q\nProceed with other available tools.", query))
	}

	// Activate matched tools in the registry
	names := make([]string, len(results))
	for i, r := range results {
		names[i] = r.RegisteredName
	}
	t.manager.ActivateTools(names)

	data, _ := json.MarshalIndent(map[string]any{
		"tools": results,
		"count": len(results),
	}, "", "  ")

	return tools.NewResult(string(data) +
		"\n\nThe above tools are now activated and available for use. " +
		"Call them directly by name to perform your operation.")
}
