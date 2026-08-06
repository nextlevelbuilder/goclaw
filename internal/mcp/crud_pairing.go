package mcp

import (
	"context"
	"log/slog"
	"regexp"
	"strings"

	mcpgo "github.com/mark3labs/mcp-go/mcp"
	mcpserver "github.com/mark3labs/mcp-go/server"

	"github.com/nextlevelbuilder/goclaw/internal/bus"
	"github.com/nextlevelbuilder/goclaw/internal/channels"
	"github.com/nextlevelbuilder/goclaw/internal/config"
	"github.com/nextlevelbuilder/goclaw/internal/store"
	"github.com/nextlevelbuilder/goclaw/internal/systemmessages"
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

// pairingCRUDDeps bundles the dependencies pairing CRUD tools need, including
// the channel notification side effects on approve (mirrors the WS
// PairingMethods onApprove callback in cmd/gateway_channels_setup.go).
type pairingCRUDDeps struct {
	pairing    store.PairingStore
	channelMgr *channels.Manager
	msgBus     *bus.MessageBus
	cfg        *config.Config
}

// notifyPairingApproved mirrors the WS onApprove callback
// (cmd/gateway_channels_setup.go) so MCP-triggered approvals also notify the
// paired channel. Browser/internal channels are skipped — UI polls directly.
// Logs the send result so MCP operators can verify delivery.
func notifyPairingApproved(ctx context.Context, deps pairingCRUDDeps, paired *store.PairedDeviceData) {
	if deps.channelMgr == nil || deps.msgBus == nil || deps.cfg == nil || paired == nil {
		return
	}
	if channels.IsInternalChannel(paired.Channel) {
		slog.Debug("pairing approved for internal channel, skipping notification", "channel", paired.Channel)
		return
	}
	botName := deps.cfg.ResolveDisplayName("default")
	msg := systemmessages.NewResolver(deps.cfg).Render("", systemmessages.KeyPairingApproved, systemmessages.Vars{
		"app_name": botName,
	})
	// Group pairings need group_id metadata so channels (e.g. Zalo) route to group API.
	if strings.HasPrefix(paired.SenderID, "group:") {
		deps.msgBus.PublishOutbound(bus.OutboundMessage{
			Channel:  paired.Channel,
			ChatID:   paired.ChatID,
			Content:  msg,
			Metadata: map[string]string{"group_id": paired.ChatID},
		})
		slog.Info("pairing approval notification published", "channel", paired.Channel, "chat_id", paired.ChatID)
		return
	}
	if err := deps.channelMgr.SendToChannel(ctx, paired.Channel, paired.ChatID, msg); err != nil {
		slog.Warn("failed to send pairing approval notification", "channel", paired.Channel, "chat_id", paired.ChatID, "error", err)
		return
	}
	slog.Info("pairing approval notification sent", "channel", paired.Channel, "chat_id", paired.ChatID)
}

// registerPairingCRUDTools registers the goclaw_pairing_device_* and
// goclaw_pairing_browser_status MCP tools backed by store.PairingStore.
// Mirrors internal/gateway/methods/pairing.go, including the approve-callback
// (channel notification) and event-broadcast side effects.
func registerPairingCRUDTools(srv *mcpserver.MCPServer, deps pairingCRUDDeps) {
	srv.AddTool(mcpgo.NewTool("goclaw_pairing_device_request",
		mcpgo.WithDescription("Request a device pairing code."),
		mcpgo.WithString("sender_id", mcpgo.Required(), mcpgo.Description("Sender identifier.")),
		mcpgo.WithString("channel", mcpgo.Required(), mcpgo.Description("Channel name.")),
		mcpgo.WithString("chat_id", mcpgo.Description("Chat ID.")),
		mcpgo.WithString("account_id", mcpgo.Description("Account ID; defaults to \"default\".")),
	), handlePairingDeviceRequest(deps.pairing))

	srv.AddTool(mcpgo.NewTool("goclaw_pairing_device_approve",
		mcpgo.WithDescription("Approve a pending pairing code."),
		mcpgo.WithString("code", mcpgo.Required(), mcpgo.Description("Pairing code.")),
		mcpgo.WithString("approved_by", mcpgo.Description("Approver identifier; defaults to \"operator\".")),
	), handlePairingDeviceApprove(deps))

	srv.AddTool(mcpgo.NewTool("goclaw_pairing_device_deny",
		mcpgo.WithDescription("Deny a pending pairing code."),
		mcpgo.WithString("code", mcpgo.Required(), mcpgo.Description("Pairing code.")),
	), handlePairingDeviceDeny(deps.pairing))

	srv.AddTool(mcpgo.NewTool("goclaw_pairing_device_list",
		mcpgo.WithDescription("List pending and paired devices."),
		mcpgo.WithReadOnlyHintAnnotation(true),
	), handlePairingDeviceList(deps.pairing))

	srv.AddTool(mcpgo.NewTool("goclaw_pairing_device_revoke",
		mcpgo.WithDescription("Revoke an approved device pairing."),
		mcpgo.WithString("sender_id", mcpgo.Required(), mcpgo.Description("Sender identifier.")),
		mcpgo.WithString("channel", mcpgo.Required(), mcpgo.Description("Channel name.")),
		mcpgo.WithDestructiveHintAnnotation(true),
	), handlePairingDeviceRevoke(deps))

	srv.AddTool(mcpgo.NewTool("goclaw_pairing_browser_status",
		mcpgo.WithDescription("Check the pairing status for a pending browser client."),
		mcpgo.WithString("sender_id", mcpgo.Required(), mcpgo.Description("Sender identifier.")),
		mcpgo.WithReadOnlyHintAnnotation(true),
	), handlePairingBrowserStatus(deps.pairing))
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

func handlePairingDeviceApprove(deps pairingCRUDDeps) mcpserver.ToolHandlerFunc {
	return func(ctx context.Context, req mcpgo.CallToolRequest) (*mcpgo.CallToolResult, error) {
		code, err := req.RequireString("code")
		if err != nil {
			return toolError("pairing.approve", err)
		}
		approvedBy := req.GetString("approved_by", "operator")
		paired, err := deps.pairing.ApprovePairing(ctx, code, approvedBy)
		if err != nil {
			return toolError("pairing.approve", err)
		}
		notifyPairingApproved(ctx, deps, paired)
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

func handlePairingDeviceRevoke(deps pairingCRUDDeps) mcpserver.ToolHandlerFunc {
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
		if err := deps.pairing.RevokePairing(ctx, senderID, channel); err != nil {
			return toolError("pairing.revoke", err)
		}
		// Broadcast revocation so the gateway force-disconnects active WebSocket
		// sessions and clears the in-memory group approval cache (parity with
		// internal/gateway/methods/pairing.go's handleRevoke).
		if deps.msgBus != nil {
			deps.msgBus.Broadcast(bus.Event{
				Name: bus.EventPairingRevoked,
				Payload: bus.PairingRevokedPayload{
					SenderID: senderID,
					Channel:  channel,
				},
			})
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
