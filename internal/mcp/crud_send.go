package mcp

import (
	"context"

	mcpgo "github.com/mark3labs/mcp-go/mcp"
	mcpserver "github.com/mark3labs/mcp-go/server"

	"github.com/nextlevelbuilder/goclaw/internal/bus"
)

// registerSendCRUDTool registers goclaw_send, backed by the same
// bus.MessageBus outbound publish path used by the gateway's own "send" WS
// RPC method (internal/gateway/methods/send.go).
func registerSendCRUDTool(srv *mcpserver.MCPServer, msgBus *bus.MessageBus) {
	srv.AddTool(mcpgo.NewTool("goclaw_send",
		mcpgo.WithDescription("Route an outbound message to a channel."),
		mcpgo.WithString("channel", mcpgo.Required(), mcpgo.Description("Channel instance name.")),
		mcpgo.WithString("to", mcpgo.Required(), mcpgo.Description("Destination chat/peer ID on that channel.")),
		mcpgo.WithString("message", mcpgo.Required(), mcpgo.Description("Message text to send.")),
	), handleSendCRUD(msgBus))
}

func handleSendCRUD(msgBus *bus.MessageBus) mcpserver.ToolHandlerFunc {
	return func(_ context.Context, req mcpgo.CallToolRequest) (*mcpgo.CallToolResult, error) {
		if msgBus == nil {
			return mcpgo.NewToolResultError("send: message bus not available"), nil
		}
		channel, err := req.RequireString("channel")
		if err != nil {
			return toolError("send", err)
		}
		to, err := req.RequireString("to")
		if err != nil {
			return toolError("send", err)
		}
		message, err := req.RequireString("message")
		if err != nil {
			return toolError("send", err)
		}

		msgBus.PublishOutbound(bus.OutboundMessage{
			Channel: channel,
			ChatID:  to,
			Content: message,
		})

		return jsonToolResult(map[string]any{
			"ok":      true,
			"channel": channel,
			"to":      to,
		})
	}
}
