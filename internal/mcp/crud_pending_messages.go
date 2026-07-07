package mcp

import (
	"context"

	mcpgo "github.com/mark3labs/mcp-go/mcp"
	mcpserver "github.com/mark3labs/mcp-go/server"

	"github.com/nextlevelbuilder/goclaw/internal/store"
)

// registerPendingMessagesCRUDTools registers the goclaw_pending_messages_*
// MCP tools backed by store.PendingMessageStore — closes a CLI-vs-MCP
// coverage gap (`goclaw channels pending`/`pending-messages list/send`).
// "send" isn't included: pending messages are queued group-chat context
// awaiting a mention, not an outbound send path (see goclaw_send for that).
func registerPendingMessagesCRUDTools(srv *mcpserver.MCPServer, pending store.PendingMessageStore) {
	srv.AddTool(mcpgo.NewTool("goclaw_pending_messages_groups",
		mcpgo.WithDescription("List all pending-message groups (channel+historyKey) with counts."),
		mcpgo.WithReadOnlyHintAnnotation(true),
	), handlePendingMessagesGroups(pending))

	srv.AddTool(mcpgo.NewTool("goclaw_pending_messages_list",
		mcpgo.WithDescription("List queued pending messages for one channel+historyKey group."),
		mcpgo.WithString("channel_name", mcpgo.Required(), mcpgo.Description("Channel name.")),
		mcpgo.WithString("history_key", mcpgo.Required(), mcpgo.Description("History key (thread/group identifier).")),
		mcpgo.WithReadOnlyHintAnnotation(true),
	), handlePendingMessagesList(pending))

	srv.AddTool(mcpgo.NewTool("goclaw_pending_messages_delete",
		mcpgo.WithDescription("Delete all pending messages for one channel+historyKey group."),
		mcpgo.WithString("channel_name", mcpgo.Required(), mcpgo.Description("Channel name.")),
		mcpgo.WithString("history_key", mcpgo.Required(), mcpgo.Description("History key (thread/group identifier).")),
		mcpgo.WithDestructiveHintAnnotation(true),
	), handlePendingMessagesDelete(pending))
}

func handlePendingMessagesGroups(pending store.PendingMessageStore) mcpserver.ToolHandlerFunc {
	return func(ctx context.Context, _ mcpgo.CallToolRequest) (*mcpgo.CallToolResult, error) {
		groups, err := pending.ListGroups(ctx)
		if err != nil {
			return toolError("pending_messages.groups", err)
		}
		return jsonToolResult(groups)
	}
}

func handlePendingMessagesList(pending store.PendingMessageStore) mcpserver.ToolHandlerFunc {
	return func(ctx context.Context, req mcpgo.CallToolRequest) (*mcpgo.CallToolResult, error) {
		channelName, err := req.RequireString("channel_name")
		if err != nil {
			return toolError("pending_messages.list", err)
		}
		historyKey, err := req.RequireString("history_key")
		if err != nil {
			return toolError("pending_messages.list", err)
		}
		msgs, err := pending.ListByKey(ctx, channelName, historyKey)
		if err != nil {
			return toolError("pending_messages.list", err)
		}
		return jsonToolResult(msgs)
	}
}

func handlePendingMessagesDelete(pending store.PendingMessageStore) mcpserver.ToolHandlerFunc {
	return func(ctx context.Context, req mcpgo.CallToolRequest) (*mcpgo.CallToolResult, error) {
		channelName, err := req.RequireString("channel_name")
		if err != nil {
			return toolError("pending_messages.delete", err)
		}
		historyKey, err := req.RequireString("history_key")
		if err != nil {
			return toolError("pending_messages.delete", err)
		}
		if err := pending.DeleteByKey(ctx, channelName, historyKey); err != nil {
			return toolError("pending_messages.delete", err)
		}
		return jsonToolResult(map[string]string{"ok": "true"})
	}
}
