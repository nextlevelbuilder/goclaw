package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"

	"github.com/google/uuid"

	mcpgo "github.com/mark3labs/mcp-go/mcp"
	mcpserver "github.com/mark3labs/mcp-go/server"

	"github.com/nextlevelbuilder/goclaw/internal/channels"
	"github.com/nextlevelbuilder/goclaw/internal/config"
	"github.com/nextlevelbuilder/goclaw/internal/store"
)

// registerChannelsCRUDTools registers the goclaw_channels_* MCP tools backed
// by the runtime *channels.Manager. Mirrors internal/gateway/methods/channels.go.
func registerChannelsCRUDTools(srv *mcpserver.MCPServer, mgr *channels.Manager) {
	srv.AddTool(mcpgo.NewTool("goclaw_channels_list",
		mcpgo.WithDescription("List enabled goclaw channels."),
		mcpgo.WithReadOnlyHintAnnotation(true),
	), handleChannelsList(mgr))

	srv.AddTool(mcpgo.NewTool("goclaw_channels_status",
		mcpgo.WithDescription("Return the connection status for all goclaw channels."),
		mcpgo.WithReadOnlyHintAnnotation(true),
	), handleChannelsStatus(mgr))

	srv.AddTool(mcpgo.NewTool("goclaw_channels_toggle",
		mcpgo.WithDescription("Enable or disable a channel. NOTE: not yet implemented server-side — always returns a \"not implemented\" error (matches the WS twin, channels.toggle, which requires a channel restart not yet supported)."),
		mcpgo.WithString("channel", mcpgo.Required(), mcpgo.Description("Channel name.")),
		mcpgo.WithBoolean("enabled", mcpgo.Required(), mcpgo.Description("Desired enabled state.")),
	), handleChannelsToggle())
}

func handleChannelsList(mgr *channels.Manager) mcpserver.ToolHandlerFunc {
	return func(_ context.Context, _ mcpgo.CallToolRequest) (*mcpgo.CallToolResult, error) {
		return jsonToolResult(map[string]any{"channels": mgr.GetEnabledChannels()})
	}
}

func handleChannelsStatus(mgr *channels.Manager) mcpserver.ToolHandlerFunc {
	return func(_ context.Context, _ mcpgo.CallToolRequest) (*mcpgo.CallToolResult, error) {
		return jsonToolResult(map[string]any{"channels": mgr.GetStatus()})
	}
}

func handleChannelsToggle() mcpserver.ToolHandlerFunc {
	return func(_ context.Context, _ mcpgo.CallToolRequest) (*mcpgo.CallToolResult, error) {
		return mcpgo.NewToolResultError("channels.toggle: not implemented"), nil
	}
}

// channelInstanceAllowed mirrors the HTTP/WS allowlist in
// internal/gateway/methods/channel_instances.go and internal/http/validate.go.
var channelInstanceAllowed = map[string]bool{
	"channel_type": true, "credentials": true, "agent_id": true,
	"enabled": true, "group_policy": true, "allow_from": true,
	"metadata": true, "webhook_secret": true, "config": true,
	"display_name": true,
}

// isValidChannelType mirrors internal/gateway/methods/channel_instances.go's
// unexported helper of the same name. Keep in sync with that switch and with
// CHANNEL_TYPES in ui/web/src/constants/channels.ts.
func isValidChannelType(ct string) bool {
	switch ct {
	case "telegram", "discord", "slack", "whatsapp", "zalo_oa", "zalo_personal", "feishu", "facebook", "pancake", "bitrix24":
		return true
	}
	return false
}

// maskChannelInstance mirrors internal/gateway/methods/channel_instances.go's
// unexported helper of the same name — never expose raw credentials via MCP.
func maskChannelInstance(inst store.ChannelInstanceData) map[string]any {
	result := map[string]any{
		"id": inst.ID, "name": inst.Name, "display_name": inst.DisplayName,
		"channel_type": inst.ChannelType, "agent_id": inst.AgentID, "config": inst.Config,
		"enabled": inst.Enabled, "is_default": store.IsDefaultChannelInstance(inst.Name),
		"has_credentials": len(inst.Credentials) > 0, "created_by": inst.CreatedBy,
		"created_at": inst.CreatedAt, "updated_at": inst.UpdatedAt,
	}
	if len(inst.Credentials) > 0 {
		var raw map[string]any
		if json.Unmarshal(inst.Credentials, &raw) == nil {
			masked := make(map[string]any, len(raw))
			for k := range raw {
				masked[k] = "***"
			}
			result["credentials"] = masked
		} else {
			result["credentials"] = map[string]string{}
		}
	} else {
		result["credentials"] = map[string]string{}
	}
	return result
}

// registerChannelInstancesCRUDTools registers the goclaw_channel_instances_*
// MCP tools backed by store.ChannelInstanceStore.
func registerChannelInstancesCRUDTools(srv *mcpserver.MCPServer, insts store.ChannelInstanceStore, agents store.AgentStore) {
	srv.AddTool(mcpgo.NewTool("goclaw_channel_instances_list",
		mcpgo.WithDescription("List all channel instances (credentials masked)."),
		mcpgo.WithReadOnlyHintAnnotation(true),
	), handleChannelInstancesList(insts))

	srv.AddTool(mcpgo.NewTool("goclaw_channel_instances_get",
		mcpgo.WithDescription("Get a single channel instance by UUID (credentials masked)."),
		mcpgo.WithString("id", mcpgo.Required(), mcpgo.Description("Instance UUID.")),
		mcpgo.WithReadOnlyHintAnnotation(true),
	), handleChannelInstancesGet(insts))

	srv.AddTool(mcpgo.NewTool("goclaw_channel_instances_create",
		mcpgo.WithDescription("Create a new channel instance."),
		mcpgo.WithString("name", mcpgo.Required(), mcpgo.Description("Instance name.")),
		mcpgo.WithString("display_name", mcpgo.Description("Human-readable display name.")),
		mcpgo.WithString("channel_type", mcpgo.Required(), mcpgo.Description("Channel type (telegram, discord, slack, whatsapp, zalo_oa, zalo_personal, feishu, facebook, pancake, bitrix24).")),
		mcpgo.WithString("agent_id", mcpgo.Required(), mcpgo.Description("Owning agent key or UUID.")),
		mcpgo.WithObject("credentials", mcpgo.Description("Channel credentials object.")),
		mcpgo.WithObject("config", mcpgo.Description("Channel config object.")),
		mcpgo.WithBoolean("enabled", mcpgo.Description("Enabled state; defaults to true.")),
	), handleChannelInstancesCreate(insts, agents))

	srv.AddTool(mcpgo.NewTool("goclaw_channel_instances_update",
		mcpgo.WithDescription("Apply a partial update to a channel instance."),
		mcpgo.WithString("id", mcpgo.Required(), mcpgo.Description("Instance UUID.")),
		mcpgo.WithObject("updates", mcpgo.Required(), mcpgo.Description("Column→value patch (allowlisted keys only).")),
	), handleChannelInstancesUpdate(insts))

	srv.AddTool(mcpgo.NewTool("goclaw_channel_instances_delete",
		mcpgo.WithDescription("Delete a channel instance. Refuses to delete default (seeded) instances."),
		mcpgo.WithString("id", mcpgo.Required(), mcpgo.Description("Instance UUID.")),
		mcpgo.WithDestructiveHintAnnotation(true),
	), handleChannelInstancesDelete(insts))
}

func handleChannelInstancesList(insts store.ChannelInstanceStore) mcpserver.ToolHandlerFunc {
	return func(ctx context.Context, _ mcpgo.CallToolRequest) (*mcpgo.CallToolResult, error) {
		list, err := insts.ListAll(ctx)
		if err != nil {
			return toolError("channels.instances.list", err)
		}
		result := make([]map[string]any, 0, len(list))
		for _, inst := range list {
			result = append(result, maskChannelInstance(inst))
		}
		return jsonToolResult(map[string]any{"instances": result})
	}
}

func handleChannelInstancesGet(insts store.ChannelInstanceStore) mcpserver.ToolHandlerFunc {
	return func(ctx context.Context, req mcpgo.CallToolRequest) (*mcpgo.CallToolResult, error) {
		idStr, err := req.RequireString("id")
		if err != nil {
			return toolError("channels.instances.get", err)
		}
		id, err := uuid.Parse(idStr)
		if err != nil {
			return toolError("channels.instances.get", fmt.Errorf("invalid id: %w", err))
		}
		inst, err := insts.Get(ctx, id)
		if err != nil {
			return toolError("channels.instances.get", err)
		}
		return jsonToolResult(maskChannelInstance(*inst))
	}
}

func handleChannelInstancesCreate(insts store.ChannelInstanceStore, agents store.AgentStore) mcpserver.ToolHandlerFunc {
	return func(ctx context.Context, req mcpgo.CallToolRequest) (*mcpgo.CallToolResult, error) {
		name, err := req.RequireString("name")
		if err != nil {
			return toolError("channels.instances.create", err)
		}
		channelType, err := req.RequireString("channel_type")
		if err != nil {
			return toolError("channels.instances.create", err)
		}
		agentRef, err := req.RequireString("agent_id")
		if err != nil {
			return toolError("channels.instances.create", err)
		}
		if !isValidChannelType(channelType) {
			return mcpgo.NewToolResultError("channels.instances.create: invalid channel_type"), nil
		}
		agentID, err := resolveAgentUUID(ctx, agents, agentRef)
		if err != nil {
			return toolError("channels.instances.create", fmt.Errorf("invalid agent_id: %w", err))
		}

		args := req.GetArguments()
		var credentials, cfgRaw json.RawMessage
		if v, ok := args["credentials"]; ok {
			credentials, _ = json.Marshal(v)
		}
		if v, ok := args["config"]; ok {
			cfgRaw, _ = json.Marshal(v)
		}

		inst := &store.ChannelInstanceData{
			Name: name, DisplayName: req.GetString("display_name", ""), ChannelType: channelType,
			AgentID: agentID, Credentials: credentials,
			Config:  config.NormalizeChannelInstanceConfigRaw(channelType, cfgRaw),
			Enabled: req.GetBool("enabled", true),
		}
		if err := insts.Create(ctx, inst); err != nil {
			return toolError("channels.instances.create", err)
		}
		return jsonToolResult(maskChannelInstance(*inst))
	}
}

func handleChannelInstancesUpdate(insts store.ChannelInstanceStore) mcpserver.ToolHandlerFunc {
	return func(ctx context.Context, req mcpgo.CallToolRequest) (*mcpgo.CallToolResult, error) {
		idStr, err := req.RequireString("id")
		if err != nil {
			return toolError("channels.instances.update", err)
		}
		id, err := uuid.Parse(idStr)
		if err != nil {
			return toolError("channels.instances.update", fmt.Errorf("invalid id: %w", err))
		}
		args := req.GetArguments()
		raw, _ := args["updates"].(map[string]any)
		if len(raw) == 0 {
			return mcpgo.NewToolResultError("channels.instances.update: updates is required"), nil
		}

		updates := make(map[string]any, len(raw))
		for k, v := range raw {
			if channelInstanceAllowed[k] {
				updates[k] = v
			} else {
				slog.Warn("security.filtered_unknown_field", "field", k, "handler", "mcp.channels.instances.update")
			}
		}
		if value, ok := updates["config"]; ok {
			channelType, _ := updates["channel_type"].(string)
			if channelType == "" {
				if inst, err := insts.Get(ctx, id); err == nil {
					channelType = inst.ChannelType
				}
			}
			updates["config"] = config.NormalizeChannelInstanceConfigValue(channelType, value)
		}

		if err := insts.Update(ctx, id, updates); err != nil {
			return toolError("channels.instances.update", err)
		}
		return jsonToolResult(map[string]string{"status": "updated"})
	}
}

func handleChannelInstancesDelete(insts store.ChannelInstanceStore) mcpserver.ToolHandlerFunc {
	return func(ctx context.Context, req mcpgo.CallToolRequest) (*mcpgo.CallToolResult, error) {
		idStr, err := req.RequireString("id")
		if err != nil {
			return toolError("channels.instances.delete", err)
		}
		id, err := uuid.Parse(idStr)
		if err != nil {
			return toolError("channels.instances.delete", fmt.Errorf("invalid id: %w", err))
		}
		inst, err := insts.Get(ctx, id)
		if err != nil {
			return toolError("channels.instances.delete", err)
		}
		if store.IsDefaultChannelInstance(inst.Name) {
			return mcpgo.NewToolResultError("channels.instances.delete: cannot delete a default instance"), nil
		}
		if err := insts.Delete(ctx, id); err != nil {
			return toolError("channels.instances.delete", err)
		}
		return jsonToolResult(map[string]string{"status": "deleted"})
	}
}
