package mcp

import (
	"context"
	"log/slog"
	"time"

	mcpgo "github.com/mark3labs/mcp-go/mcp"
	mcpserver "github.com/mark3labs/mcp-go/server"

	"github.com/nextlevelbuilder/goclaw/internal/channels"
	"github.com/nextlevelbuilder/goclaw/internal/config"
	"github.com/nextlevelbuilder/goclaw/internal/providers"
	"github.com/nextlevelbuilder/goclaw/internal/store"
)

// registerChatCRUDTools registers the goclaw_chat_* MCP tools backed by the
// live agent runtime (via ChatRunner) and store.SessionStore.
func registerChatCRUDTools(srv *mcpserver.MCPServer, runner ChatRunner, sessions store.SessionStore) {
	srv.AddTool(mcpgo.NewTool("goclaw_chat_send",
		mcpgo.WithDescription("Send a chat message to a goclaw agent and receive the assistant's reply. Always synchronous — the underlying run's incremental events (if any) are not forwarded, only the final result."),
		mcpgo.WithString("message", mcpgo.Required(), mcpgo.Description("The user message to send.")),
		mcpgo.WithString("agent_id", mcpgo.Description("Agent key/slug (defaults to \"default\", or is inferred from session_key when provided).")),
		mcpgo.WithString("session_key", mcpgo.Description("Existing session key to resume; a new one is created when omitted.")),
	), handleChatSend(runner))

	srv.AddTool(mcpgo.NewTool("goclaw_chat_history",
		mcpgo.WithDescription("Fetch the message history for a goclaw chat session."),
		mcpgo.WithString("session_key", mcpgo.Required(), mcpgo.Description("Session key.")),
		mcpgo.WithReadOnlyHintAnnotation(true),
	), handleChatHistory(sessions))

	srv.AddTool(mcpgo.NewTool("goclaw_chat_inject",
		mcpgo.WithDescription("Inject a message into a goclaw session's transcript without triggering an agent run."),
		mcpgo.WithString("session_key", mcpgo.Required(), mcpgo.Description("Session key.")),
		mcpgo.WithString("message", mcpgo.Required(), mcpgo.Description("Message text to inject.")),
		mcpgo.WithString("label", mcpgo.Description("Optional label prefix (e.g. \"note\"), truncated to 100 chars.")),
	), handleChatInject(sessions))

	srv.AddTool(mcpgo.NewTool("goclaw_chat_abort",
		mcpgo.WithDescription("Cancel a running goclaw agent invocation for a session or a specific run ID."),
		mcpgo.WithString("run_id", mcpgo.Description("Specific run ID to abort.")),
		mcpgo.WithString("session_key", mcpgo.Description("Session key whose active run(s) should be aborted.")),
		mcpgo.WithDestructiveHintAnnotation(true),
	), handleChatAbort(runner))

	srv.AddTool(mcpgo.NewTool("goclaw_chat_session_status",
		mcpgo.WithDescription("Return the running state and current activity (phase, tool, iteration) for a goclaw chat session."),
		mcpgo.WithString("session_key", mcpgo.Required(), mcpgo.Description("Session key.")),
		mcpgo.WithReadOnlyHintAnnotation(true),
	), handleChatSessionStatus(runner))
}

const maxInjectLabelLen = 100

func handleChatSend(runner ChatRunner) mcpserver.ToolHandlerFunc {
	return func(ctx context.Context, req mcpgo.CallToolRequest) (*mcpgo.CallToolResult, error) {
		if runner == nil {
			return mcpgo.NewToolResultError("chat.send: chat runtime not available"), nil
		}
		message, err := req.RequireString("message")
		if err != nil {
			return toolError("chat.send", err)
		}
		agentID := req.GetString("agent_id", "")
		sessionKey := req.GetString("session_key", "")

		startTime := time.Now()
		slog.Info("mcp.chat_send.start", "agent_id", agentID, "session_key", sessionKey, "message_len", len(message))

		result, err := runner.Send(ctx, agentID, sessionKey, message, nil)

		duration := time.Since(startTime)
		if err != nil {
			slog.Warn("mcp.chat_send.error", "agent_id", agentID, "session_key", sessionKey, "duration_ms", duration.Milliseconds(), "error", err)
			return toolError("chat.send", err)
		}

		slog.Info("mcp.chat_send.done", "agent_id", agentID, "session_key", sessionKey, "run_id", result.RunID, "duration_ms", duration.Milliseconds(), "content_len", len(result.Content))
		return jsonToolResult(result)
	}
}

func handleChatHistory(sessions store.SessionStore) mcpserver.ToolHandlerFunc {
	return func(ctx context.Context, req mcpgo.CallToolRequest) (*mcpgo.CallToolResult, error) {
		sessionKey, err := req.RequireString("session_key")
		if err != nil {
			return toolError("chat.history", err)
		}
		history := sessions.GetHistory(ctx, sessionKey)
		return jsonToolResult(map[string]any{"messages": history})
	}
}

func handleChatInject(sessions store.SessionStore) mcpserver.ToolHandlerFunc {
	return func(ctx context.Context, req mcpgo.CallToolRequest) (*mcpgo.CallToolResult, error) {
		sessionKey, err := req.RequireString("session_key")
		if err != nil {
			return toolError("chat.inject", err)
		}
		message, err := req.RequireString("message")
		if err != nil {
			return toolError("chat.inject", err)
		}
		label := req.GetString("label", "")
		if len(label) > maxInjectLabelLen {
			label = label[:maxInjectLabelLen]
		}
		text := message
		if label != "" {
			text = "[" + label + "]\n\n" + message
		}
		sessions.AddMessage(ctx, sessionKey, providers.Message{Role: "assistant", Content: text})
		return jsonToolResult(map[string]any{"ok": true})
	}
}

func handleChatAbort(runner ChatRunner) mcpserver.ToolHandlerFunc {
	return func(ctx context.Context, req mcpgo.CallToolRequest) (*mcpgo.CallToolResult, error) {
		if runner == nil {
			return mcpgo.NewToolResultError("chat.abort: chat runtime not available"), nil
		}
		runID := req.GetString("run_id", "")
		sessionKey := req.GetString("session_key", "")
		if runID == "" && sessionKey == "" {
			return mcpgo.NewToolResultError("chat.abort: one of run_id or session_key is required"), nil
		}
		result, err := runner.Abort(ctx, runID, sessionKey)
		if err != nil {
			return toolError("chat.abort", err)
		}
		return jsonToolResult(result)
	}
}

func handleChatSessionStatus(runner ChatRunner) mcpserver.ToolHandlerFunc {
	return func(ctx context.Context, req mcpgo.CallToolRequest) (*mcpgo.CallToolResult, error) {
		if runner == nil {
			return mcpgo.NewToolResultError("chat.session.status: chat runtime not available"), nil
		}
		sessionKey, err := req.RequireString("session_key")
		if err != nil {
			return toolError("chat.session.status", err)
		}
		result, err := runner.SessionStatus(ctx, sessionKey)
		if err != nil {
			return toolError("chat.session.status", err)
		}
		return jsonToolResult(result)
	}
}

// registerChatBehaviorCRUDTool registers goclaw_chat_behavior_preview, backed
// by the same channels.ResolveChatBehavior/PreviewResolvedChatBehavior logic
// used by the WS chat_behavior.preview method
// (internal/gateway/methods/chat_behavior.go). The WS method additionally
// requires master scope + owner role, enforced via the WS client's resolved
// role/tenant; this MCP surface has no such per-caller identity (the bearer
// token is the sole boundary), matching the rest of this CRUD MCP server.
func registerChatBehaviorCRUDTool(srv *mcpserver.MCPServer, cfg *config.Config, channelMgr *channels.Manager) {
	srv.AddTool(mcpgo.NewTool("goclaw_chat_behavior_preview",
		mcpgo.WithDescription("Preview resolved channel delivery behavior (streaming/quick-ack/final-split) for a channel or an ad-hoc config."),
		mcpgo.WithString("channel", mcpgo.Description("Channel instance name to resolve behavior for; empty uses the global default.")),
		mcpgo.WithString("content", mcpgo.Description("Sample content to preview delivery for.")),
		mcpgo.WithBoolean("is_streaming", mcpgo.Description("Whether the sample response is streaming.")),
		mcpgo.WithBoolean("has_tool_calls", mcpgo.Description("Whether the sample response includes tool calls.")),
		mcpgo.WithReadOnlyHintAnnotation(true),
	), handleChatBehaviorPreview(cfg, channelMgr))
}

func handleChatBehaviorPreview(cfg *config.Config, channelMgr *channels.Manager) mcpserver.ToolHandlerFunc {
	return func(_ context.Context, req mcpgo.CallToolRequest) (*mcpgo.CallToolResult, error) {
		channel := req.GetString("channel", "")

		var resolved channels.ResolvedChatBehavior
		if channelMgr != nil {
			resolved = channelMgr.ResolveChatBehavior(channel, cfg.Gateway.ChatBehavior)
		} else {
			resolved = channels.ResolveChatBehavior(cfg.Gateway.ChatBehavior, nil)
		}
		preview := channels.PreviewResolvedChatBehavior(resolved, channels.ChatBehaviorPreviewOptions{
			Content:      req.GetString("content", ""),
			IsStreaming:  req.GetBool("is_streaming", false),
			HasToolCalls: req.GetBool("has_tool_calls", false),
		})
		return jsonToolResult(preview)
	}
}
