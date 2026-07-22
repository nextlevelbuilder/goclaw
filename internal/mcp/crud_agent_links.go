package mcp

import (
	"context"
	"fmt"

	"github.com/google/uuid"

	mcpgo "github.com/mark3labs/mcp-go/mcp"
	mcpserver "github.com/mark3labs/mcp-go/server"

	"github.com/nextlevelbuilder/goclaw/internal/store"
)

// registerAgentLinkCRUDTools registers the goclaw_agent_links_* MCP tools
// backed by store.AgentLinkStore.
func registerAgentLinkCRUDTools(srv *mcpserver.MCPServer, links store.AgentLinkStore) {
	srv.AddTool(mcpgo.NewTool("goclaw_agent_links_list",
		mcpgo.WithDescription("List inter-agent delegation links for an agent."),
		mcpgo.WithString("agent_id", mcpgo.Required(), mcpgo.Description("Agent UUID to list links for.")),
		mcpgo.WithString("direction", mcpgo.Enum("from", "to", "all"), mcpgo.Description("Direction to list: \"from\" (default), \"to\", or \"all\".")),
		mcpgo.WithReadOnlyHintAnnotation(true),
	), handleAgentLinksList(links))

	srv.AddTool(mcpgo.NewTool("goclaw_agent_links_create",
		mcpgo.WithDescription("Create a new inter-agent delegation link."),
		mcpgo.WithString("source_agent", mcpgo.Required(), mcpgo.Description("Source agent UUID.")),
		mcpgo.WithString("target_agent", mcpgo.Required(), mcpgo.Description("Target agent UUID.")),
		mcpgo.WithString("direction", mcpgo.Enum("outbound", "inbound", "bidirectional"), mcpgo.Description("Link direction; defaults to \"outbound\".")),
		mcpgo.WithString("description", mcpgo.Description("Human-readable description of the link.")),
		mcpgo.WithNumber("max_concurrent", mcpgo.Description("Maximum concurrent delegated tasks.")),
	), handleAgentLinksCreate(links))

	srv.AddTool(mcpgo.NewTool("goclaw_agent_links_update",
		mcpgo.WithDescription("Apply a partial update to an agent link."),
		mcpgo.WithString("link_id", mcpgo.Required(), mcpgo.Description("Link UUID.")),
		mcpgo.WithString("direction", mcpgo.Enum("outbound", "inbound", "bidirectional"), mcpgo.Description("New direction.")),
		mcpgo.WithString("description", mcpgo.Description("New description.")),
		mcpgo.WithNumber("max_concurrent", mcpgo.Description("New max concurrent delegated tasks.")),
		mcpgo.WithString("status", mcpgo.Enum("active", "disabled"), mcpgo.Description("New status.")),
	), handleAgentLinksUpdate(links))

	srv.AddTool(mcpgo.NewTool("goclaw_agent_links_delete",
		mcpgo.WithDescription("Delete an agent link."),
		mcpgo.WithString("link_id", mcpgo.Required(), mcpgo.Description("Link UUID.")),
		mcpgo.WithDestructiveHintAnnotation(true),
	), handleAgentLinksDelete(links))
}

func handleAgentLinksList(links store.AgentLinkStore) mcpserver.ToolHandlerFunc {
	return func(ctx context.Context, req mcpgo.CallToolRequest) (*mcpgo.CallToolResult, error) {
		agentIDStr, err := req.RequireString("agent_id")
		if err != nil {
			return toolError("agent_links.list", err)
		}
		agentID, err := uuid.Parse(agentIDStr)
		if err != nil {
			return toolError("agent_links.list", fmt.Errorf("invalid agent_id: %w", err))
		}

		direction := req.GetString("direction", "from")
		var list []store.AgentLinkData
		switch direction {
		case "to":
			list, err = links.ListLinksTo(ctx, agentID)
		case "all":
			var fromList, toList []store.AgentLinkData
			fromList, err = links.ListLinksFrom(ctx, agentID)
			if err == nil {
				toList, err = links.ListLinksTo(ctx, agentID)
			}
			list = append(fromList, toList...) //nolint:gocritic // append(dst, src...) is intentional aggregation
		default:
			list, err = links.ListLinksFrom(ctx, agentID)
		}
		if err != nil {
			return toolError("agent_links.list", err)
		}
		return jsonToolResult(map[string]any{"links": list, "count": len(list)})
	}
}

func handleAgentLinksCreate(links store.AgentLinkStore) mcpserver.ToolHandlerFunc {
	return func(ctx context.Context, req mcpgo.CallToolRequest) (*mcpgo.CallToolResult, error) {
		sourceStr, err := req.RequireString("source_agent")
		if err != nil {
			return toolError("agent_links.create", err)
		}
		targetStr, err := req.RequireString("target_agent")
		if err != nil {
			return toolError("agent_links.create", err)
		}
		sourceID, err := uuid.Parse(sourceStr)
		if err != nil {
			return toolError("agent_links.create", fmt.Errorf("invalid source_agent: %w", err))
		}
		targetID, err := uuid.Parse(targetStr)
		if err != nil {
			return toolError("agent_links.create", fmt.Errorf("invalid target_agent: %w", err))
		}

		link := &store.AgentLinkData{
			BaseModel:     store.BaseModel{ID: store.GenNewID()},
			SourceAgentID: sourceID,
			TargetAgentID: targetID,
			Direction:     req.GetString("direction", store.LinkDirectionOutbound),
			Description:   req.GetString("description", ""),
			MaxConcurrent: int(req.GetFloat("max_concurrent", 0)),
			Status:        store.LinkStatusActive,
		}
		if err := links.CreateLink(ctx, link); err != nil {
			return toolError("agent_links.create", err)
		}
		return jsonToolResult(map[string]any{"link": link})
	}
}

func handleAgentLinksUpdate(links store.AgentLinkStore) mcpserver.ToolHandlerFunc {
	return func(ctx context.Context, req mcpgo.CallToolRequest) (*mcpgo.CallToolResult, error) {
		linkIDStr, err := req.RequireString("link_id")
		if err != nil {
			return toolError("agent_links.update", err)
		}
		linkID, err := uuid.Parse(linkIDStr)
		if err != nil {
			return toolError("agent_links.update", fmt.Errorf("invalid link_id: %w", err))
		}

		updates := map[string]any{}
		args := req.GetArguments()
		for _, key := range []string{"direction", "description", "status"} {
			if v, ok := args[key]; ok {
				updates[key] = v
			}
		}
		if v, ok := args["max_concurrent"]; ok {
			updates["max_concurrent"] = v
		}
		if len(updates) == 0 {
			return mcpgo.NewToolResultError("agent_links.update: no fields to update"), nil
		}
		if err := links.UpdateLink(ctx, linkID, updates); err != nil {
			return toolError("agent_links.update", err)
		}
		return jsonToolResult(map[string]bool{"ok": true})
	}
}

func handleAgentLinksDelete(links store.AgentLinkStore) mcpserver.ToolHandlerFunc {
	return func(ctx context.Context, req mcpgo.CallToolRequest) (*mcpgo.CallToolResult, error) {
		linkIDStr, err := req.RequireString("link_id")
		if err != nil {
			return toolError("agent_links.delete", err)
		}
		linkID, err := uuid.Parse(linkIDStr)
		if err != nil {
			return toolError("agent_links.delete", fmt.Errorf("invalid link_id: %w", err))
		}
		if err := links.DeleteLink(ctx, linkID); err != nil {
			return toolError("agent_links.delete", err)
		}
		return jsonToolResult(map[string]bool{"ok": true})
	}
}
