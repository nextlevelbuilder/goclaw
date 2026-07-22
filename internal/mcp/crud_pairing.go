package mcp

import (
	"context"
	"log/slog"
	"regexp"

	mcpgo "github.com/mark3labs/mcp-go/mcp"
	mcpserver "github.com/mark3labs/mcp-go/server"

	"github.com/nextlevelbuilder/goclaw/internal/store"
)

// validMCPSenderIDRe mirrors internal/gateway/methods/pairing.go's
// validSenderIDRe — safe characters only, prevents log injection.
var validMCPSenderIDRe = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9._:@-]*$`)

const maxSenderIDLen = 128

// isValidMCPSenderID mirrors internal/gateway/methods/pairing.go's
// isValidSenderID helper.
func isValidMCPSenderID(id string) bool {
	return len(id) <= maxSenderIDLen && validMCPSenderIDRe.MatchString(id)
}

// registerPairingCRUDTools registers the goclaw_pairing_device_* and
// goclaw_pairing_browser_status MCP tools backed by store.PairingStore.
// Mirrors internal/gateway/methods/pairing.go minus the approve-callback
// (channel notification) and event-broadcast side effects, which are WS/bus
// concerns not applicable to this standalone MCP surface.
func registerPairingCRUDTools(srv *mcpserver.MCPServer, pairing store.PairingStore) {
	srv.AddTool(mcpgo.NewTool("goclaw_pairing_device_request",
		mcpgo.WithDescription("Request a device pairing code."),
		mcpgo.WithString("sender_id", mcpgo.Required(), mcpgo.Description("Sender identifier.")),
		mcpgo.WithString("channel", mcpgo.Required(), mcpgo.Description("Channel name.")),
		mcpgo.WithString("chat_id", mcpgo.Description("Chat ID.")),
		mcpgo.WithString("account_id", mcpgo.Description("Account ID; defaults to \"default\".")),
	), handlePairingDeviceRequest(pairing))

	srv.AddTool(mcpgo.NewTool("goclaw_pairing_device_approve",
		mcpgo.WithDescription("Approve a pending pairing code."),
		mcpgo.WithString("code", mcpgo.Required(), mcpgo.Description("Pairing code.")),
		mcpgo.WithString("approved_by", mcpgo.Description("Approver identifier; defaults to \"operator\".")),
	), handlePairingDeviceApprove(pairing))

	srv.AddTool(mcpgo.NewTool("goclaw_pairing_device_deny",
		mcpgo.WithDescription("Deny a pending pairing code."),
		mcpgo.WithString("code", mcpgo.Required(), mcpgo.Description("Pairing code.")),
	), handlePairingDeviceDeny(pairing))

	srv.AddTool(mcpgo.NewTool("goclaw_pairing_device_list",
		mcpgo.WithDescription("List pending and paired devices."),
		mcpgo.WithReadOnlyHintAnnotation(true),
	), handlePairingDeviceList(pairing))

	srv.AddTool(mcpgo.NewTool("goclaw_pairing_device_revoke",
		mcpgo.WithDescription("Revoke an approved device pairing."),
		mcpgo.WithString("sender_id", mcpgo.Required(), mcpgo.Description("Sender identifier.")),
		mcpgo.WithString("channel", mcpgo.Required(), mcpgo.Description("Channel name.")),
		mcpgo.WithDestructiveHintAnnotation(true),
	), handlePairingDeviceRevoke(pairing))

	srv.AddTool(mcpgo.NewTool("goclaw_pairing_browser_status",
		mcpgo.WithDescription("Check the pairing status for a pending browser client."),
		mcpgo.WithString("sender_id", mcpgo.Required(), mcpgo.Description("Sender identifier.")),
		mcpgo.WithReadOnlyHintAnnotation(true),
	), handlePairingBrowserStatus(pairing))
}

func handlePairingDeviceRequest(pairing store.PairingStore) mcpserver.ToolHandlerFunc {
	return func(ctx context.Context, req mcpgo.CallToolRequest) (*mcpgo.CallToolResult, error) {
		senderID, err := req.RequireString("sender_id")
		if err != nil {
			return toolError("pairing.request", err)
		}
		channel, err := req.RequireString("channel")
		if err != nil {
			return toolError("pairing.request", err)
		}
		if !isValidMCPSenderID(senderID) {
			slog.Warn("security.invalid_sender_id_format", "handler", "mcp.pairing.request")
			return mcpgo.NewToolResultError("pairing.request: invalid sender_id format"), nil
		}
		accountID := req.GetString("account_id", "default")
		code, err := pairing.RequestPairing(ctx, senderID, channel, req.GetString("chat_id", ""), accountID, nil)
		if err != nil {
			return toolError("pairing.request", err)
		}
		return jsonToolResult(map[string]string{"code": code})
	}
}

func handlePairingDeviceApprove(pairing store.PairingStore) mcpserver.ToolHandlerFunc {
	return func(ctx context.Context, req mcpgo.CallToolRequest) (*mcpgo.CallToolResult, error) {
		code, err := req.RequireString("code")
		if err != nil {
			return toolError("pairing.approve", err)
		}
		approvedBy := req.GetString("approved_by", "operator")
		paired, err := pairing.ApprovePairing(ctx, code, approvedBy)
		if err != nil {
			return toolError("pairing.approve", err)
		}
		return jsonToolResult(map[string]any{"paired": paired})
	}
}

func handlePairingDeviceDeny(pairing store.PairingStore) mcpserver.ToolHandlerFunc {
	return func(ctx context.Context, req mcpgo.CallToolRequest) (*mcpgo.CallToolResult, error) {
		code, err := req.RequireString("code")
		if err != nil {
			return toolError("pairing.deny", err)
		}
		if err := pairing.DenyPairing(ctx, code); err != nil {
			return toolError("pairing.deny", err)
		}
		return jsonToolResult(map[string]bool{"denied": true})
	}
}

func handlePairingDeviceList(pairing store.PairingStore) mcpserver.ToolHandlerFunc {
	return func(ctx context.Context, _ mcpgo.CallToolRequest) (*mcpgo.CallToolResult, error) {
		return jsonToolResult(map[string]any{
			"pending": pairing.ListPending(ctx),
			"paired":  pairing.ListPaired(ctx),
		})
	}
}

func handlePairingDeviceRevoke(pairing store.PairingStore) mcpserver.ToolHandlerFunc {
	return func(ctx context.Context, req mcpgo.CallToolRequest) (*mcpgo.CallToolResult, error) {
		senderID, err := req.RequireString("sender_id")
		if err != nil {
			return toolError("pairing.revoke", err)
		}
		channel, err := req.RequireString("channel")
		if err != nil {
			return toolError("pairing.revoke", err)
		}
		if !isValidMCPSenderID(senderID) {
			slog.Warn("security.invalid_sender_id_format", "handler", "mcp.pairing.revoke")
			return mcpgo.NewToolResultError("pairing.revoke: invalid sender_id format"), nil
		}
		if err := pairing.RevokePairing(ctx, senderID, channel); err != nil {
			return toolError("pairing.revoke", err)
		}
		return jsonToolResult(map[string]bool{"revoked": true})
	}
}

func handlePairingBrowserStatus(pairing store.PairingStore) mcpserver.ToolHandlerFunc {
	return func(ctx context.Context, req mcpgo.CallToolRequest) (*mcpgo.CallToolResult, error) {
		senderID, err := req.RequireString("sender_id")
		if err != nil {
			return toolError("pairing.browser.status", err)
		}
		if !isValidMCPSenderID(senderID) {
			slog.Warn("security.invalid_sender_id_format", "handler", "mcp.pairing.browser_status")
			return mcpgo.NewToolResultError("pairing.browser.status: invalid sender_id format"), nil
		}
		paired, pairErr := pairing.IsPaired(ctx, senderID, "browser")
		if pairErr != nil {
			slog.Warn("security.pairing_check_failed", "error", pairErr)
		}
		if paired {
			return jsonToolResult(map[string]string{"status": "approved"})
		}
		for _, p := range pairing.ListPending(ctx) {
			if p.SenderID == senderID && p.Channel == "browser" {
				return jsonToolResult(map[string]string{"status": "pending"})
			}
		}
		return jsonToolResult(map[string]string{"status": "expired"})
	}
}
