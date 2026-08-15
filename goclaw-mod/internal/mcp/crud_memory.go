package mcp

import (
	"context"

	mcpgo "github.com/mark3labs/mcp-go/mcp"
	mcpserver "github.com/mark3labs/mcp-go/server"

	"github.com/nextlevelbuilder/goclaw/internal/store"
)

// registerMemoryCRUDTools registers the goclaw_memory_* MCP tools backed by
// store.MemoryStore — closes a CLI-vs-MCP coverage gap (the `goclaw memory
// get/list/search/store/delete` commands had no MCP equivalent).
func registerMemoryCRUDTools(srv *mcpserver.MCPServer, memory store.MemoryStore) {
	srv.AddTool(mcpgo.NewTool("goclaw_memory_list",
		mcpgo.WithDescription("List memory documents for an agent/user scope."),
		mcpgo.WithString("agent_id", mcpgo.Required(), mcpgo.Description("Agent ID scope.")),
		mcpgo.WithString("user_id", mcpgo.Description("User ID scope; empty for agent-global documents.")),
		mcpgo.WithReadOnlyHintAnnotation(true),
	), handleMemoryList(memory))

	srv.AddTool(mcpgo.NewTool("goclaw_memory_get",
		mcpgo.WithDescription("Get a single memory document's content."),
		mcpgo.WithString("agent_id", mcpgo.Required(), mcpgo.Description("Agent ID scope.")),
		mcpgo.WithString("user_id", mcpgo.Description("User ID scope; empty for agent-global documents.")),
		mcpgo.WithString("path", mcpgo.Required(), mcpgo.Description("Document path.")),
		mcpgo.WithReadOnlyHintAnnotation(true),
	), handleMemoryGet(memory))

	srv.AddTool(mcpgo.NewTool("goclaw_memory_search",
		mcpgo.WithDescription("Search memory documents (hybrid vector+text search) for an agent/user scope."),
		mcpgo.WithString("query", mcpgo.Required(), mcpgo.Description("Search query.")),
		mcpgo.WithString("agent_id", mcpgo.Required(), mcpgo.Description("Agent ID scope.")),
		mcpgo.WithString("user_id", mcpgo.Description("User ID scope; empty for agent-global documents.")),
		mcpgo.WithNumber("max_results", mcpgo.Description("Maximum results to return; server default if omitted.")),
		mcpgo.WithReadOnlyHintAnnotation(true),
	), handleMemorySearch(memory))

	srv.AddTool(mcpgo.NewTool("goclaw_memory_store",
		mcpgo.WithDescription("Create or overwrite a memory document's content, then re-index it."),
		mcpgo.WithString("agent_id", mcpgo.Required(), mcpgo.Description("Agent ID scope.")),
		mcpgo.WithString("user_id", mcpgo.Description("User ID scope; empty for agent-global documents.")),
		mcpgo.WithString("path", mcpgo.Required(), mcpgo.Description("Document path.")),
		mcpgo.WithString("content", mcpgo.Required(), mcpgo.Description("Document content.")),
	), handleMemoryStore(memory))

	srv.AddTool(mcpgo.NewTool("goclaw_memory_delete",
		mcpgo.WithDescription("Delete a memory document."),
		mcpgo.WithString("agent_id", mcpgo.Required(), mcpgo.Description("Agent ID scope.")),
		mcpgo.WithString("user_id", mcpgo.Description("User ID scope; empty for agent-global documents.")),
		mcpgo.WithString("path", mcpgo.Required(), mcpgo.Description("Document path.")),
		mcpgo.WithDestructiveHintAnnotation(true),
	), handleMemoryDelete(memory))
}

func handleMemoryList(memory store.MemoryStore) mcpserver.ToolHandlerFunc {
	return func(ctx context.Context, req mcpgo.CallToolRequest) (*mcpgo.CallToolResult, error) {
		agentID, err := req.RequireString("agent_id")
		if err != nil {
			return toolError("memory.list", err)
		}
		userID := req.GetString("user_id", "")
		docs, err := memory.ListDocuments(ctx, agentID, userID)
		if err != nil {
			return toolError("memory.list", err)
		}
		return jsonToolResult(docs)
	}
}

func handleMemoryGet(memory store.MemoryStore) mcpserver.ToolHandlerFunc {
	return func(ctx context.Context, req mcpgo.CallToolRequest) (*mcpgo.CallToolResult, error) {
		agentID, err := req.RequireString("agent_id")
		if err != nil {
			return toolError("memory.get", err)
		}
		path, err := req.RequireString("path")
		if err != nil {
			return toolError("memory.get", err)
		}
		userID := req.GetString("user_id", "")
		content, err := memory.GetDocument(ctx, agentID, userID, path)
		if err != nil {
			return toolError("memory.get", err)
		}
		return jsonToolResult(map[string]string{"path": path, "content": content})
	}
}

func handleMemorySearch(memory store.MemoryStore) mcpserver.ToolHandlerFunc {
	return func(ctx context.Context, req mcpgo.CallToolRequest) (*mcpgo.CallToolResult, error) {
		query, err := req.RequireString("query")
		if err != nil {
			return toolError("memory.search", err)
		}
		agentID, err := req.RequireString("agent_id")
		if err != nil {
			return toolError("memory.search", err)
		}
		userID := req.GetString("user_id", "")
		opts := store.MemorySearchOptions{}
		if v, ok := req.GetArguments()["max_results"]; ok {
			if f, ok := v.(float64); ok {
				opts.MaxResults = int(f)
			}
		}
		results, err := memory.Search(ctx, query, agentID, userID, opts)
		if err != nil {
			return toolError("memory.search", err)
		}
		return jsonToolResult(results)
	}
}

func handleMemoryStore(memory store.MemoryStore) mcpserver.ToolHandlerFunc {
	return func(ctx context.Context, req mcpgo.CallToolRequest) (*mcpgo.CallToolResult, error) {
		agentID, err := req.RequireString("agent_id")
		if err != nil {
			return toolError("memory.store", err)
		}
		path, err := req.RequireString("path")
		if err != nil {
			return toolError("memory.store", err)
		}
		content, err := req.RequireString("content")
		if err != nil {
			return toolError("memory.store", err)
		}
		userID := req.GetString("user_id", "")
		if err := memory.PutDocument(ctx, agentID, userID, path, content); err != nil {
			return toolError("memory.store", err)
		}
		if err := memory.IndexDocument(ctx, agentID, userID, path); err != nil {
			return toolError("memory.store", err)
		}
		return jsonToolResult(map[string]string{"ok": "true", "path": path})
	}
}

func handleMemoryDelete(memory store.MemoryStore) mcpserver.ToolHandlerFunc {
	return func(ctx context.Context, req mcpgo.CallToolRequest) (*mcpgo.CallToolResult, error) {
		agentID, err := req.RequireString("agent_id")
		if err != nil {
			return toolError("memory.delete", err)
		}
		path, err := req.RequireString("path")
		if err != nil {
			return toolError("memory.delete", err)
		}
		userID := req.GetString("user_id", "")
		if err := memory.DeleteDocument(ctx, agentID, userID, path); err != nil {
			return toolError("memory.delete", err)
		}
		return jsonToolResult(map[string]string{"ok": "true"})
	}
}
