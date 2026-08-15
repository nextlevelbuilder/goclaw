package mcp

import (
	"context"

	mcpgo "github.com/mark3labs/mcp-go/mcp"
	mcpserver "github.com/mark3labs/mcp-go/server"

	"github.com/nextlevelbuilder/goclaw/internal/store"
)

// registerSessionCRUDTools registers the goclaw_sessions_* MCP tools backed by store.SessionStore.
func registerSessionCRUDTools(srv *mcpserver.MCPServer, sessions store.SessionStore) {
	srv.AddTool(mcpgo.NewTool("goclaw_sessions_list",
		mcpgo.WithDescription("List session keys for a given agent."),
		mcpgo.WithString("agent_id", mcpgo.Required(), mcpgo.Description("Agent ID to list sessions for.")),
		mcpgo.WithReadOnlyHintAnnotation(true),
	), handleSessionsList(sessions))

	srv.AddTool(mcpgo.NewTool("goclaw_sessions_get",
		mcpgo.WithDescription("Get a session's current state (label, summary, message count) by key."),
		mcpgo.WithString("key", mcpgo.Required(), mcpgo.Description("Session key.")),
		mcpgo.WithReadOnlyHintAnnotation(true),
	), handleSessionsGet(sessions))

	srv.AddTool(mcpgo.NewTool("goclaw_sessions_preview",
		mcpgo.WithDescription("Return the message history and summary for a goclaw session."),
		mcpgo.WithString("key", mcpgo.Required(), mcpgo.Description("Session key.")),
		mcpgo.WithReadOnlyHintAnnotation(true),
	), handleSessionsPreview(sessions))

	srv.AddTool(mcpgo.NewTool("goclaw_sessions_patch",
		mcpgo.WithDescription("Update label, model, and/or metadata on a goclaw session."),
		mcpgo.WithString("key", mcpgo.Required(), mcpgo.Description("Session key.")),
		mcpgo.WithString("label", mcpgo.Description("New session label.")),
		mcpgo.WithString("model", mcpgo.Description("New model name.")),
		mcpgo.WithObject("metadata", mcpgo.Description("Metadata key/value pairs to set (replaces existing metadata).")),
	), handleSessionsPatch(sessions))

	srv.AddTool(mcpgo.NewTool("goclaw_sessions_delete",
		mcpgo.WithDescription("Delete a goclaw session."),
		mcpgo.WithString("key", mcpgo.Required(), mcpgo.Description("Session key.")),
		mcpgo.WithDestructiveHintAnnotation(true),
	), handleSessionsDelete(sessions))

	srv.AddTool(mcpgo.NewTool("goclaw_sessions_reset",
		mcpgo.WithDescription("Reset a goclaw session's transcript, clearing its message history."),
		mcpgo.WithString("key", mcpgo.Required(), mcpgo.Description("Session key.")),
		mcpgo.WithDestructiveHintAnnotation(true),
	), handleSessionsReset(sessions))

	srv.AddTool(mcpgo.NewTool("goclaw_sessions_compact",
		mcpgo.WithDescription("Compact a goclaw session's history, keeping only the most recent messages."),
		mcpgo.WithString("key", mcpgo.Required(), mcpgo.Description("Session key.")),
		mcpgo.WithNumber("keep_last", mcpgo.Description("Number of most-recent messages to keep.")),
		mcpgo.WithDestructiveHintAnnotation(true),
	), handleSessionsCompact(sessions))
}

func handleSessionsList(sessions store.SessionStore) mcpserver.ToolHandlerFunc {
	return func(ctx context.Context, req mcpgo.CallToolRequest) (*mcpgo.CallToolResult, error) {
		agentID, err := req.RequireString("agent_id")
		if err != nil {
			return toolError("sessions.list", err)
		}
		list := sessions.List(ctx, agentID)
		return jsonToolResult(list)
	}
}

func handleSessionsGet(sessions store.SessionStore) mcpserver.ToolHandlerFunc {
	return func(ctx context.Context, req mcpgo.CallToolRequest) (*mcpgo.CallToolResult, error) {
		key, err := req.RequireString("key")
		if err != nil {
			return toolError("sessions.get", err)
		}
		sess := sessions.Get(ctx, key)
		if sess == nil {
			return mcpgo.NewToolResultError("sessions.get: session not found: " + key), nil
		}
		return jsonToolResult(sess)
	}
}

func handleSessionsPreview(sessions store.SessionStore) mcpserver.ToolHandlerFunc {
	return func(ctx context.Context, req mcpgo.CallToolRequest) (*mcpgo.CallToolResult, error) {
		key, err := req.RequireString("key")
		if err != nil {
			return toolError("sessions.preview", err)
		}
		sess := sessions.Get(ctx, key)
		if sess == nil {
			return mcpgo.NewToolResultError("sessions.preview: session not found: " + key), nil
		}
		return jsonToolResult(map[string]any{
			"key":      key,
			"messages": sess.Messages,
			"summary":  sess.Summary,
		})
	}
}

func handleSessionsPatch(sessions store.SessionStore) mcpserver.ToolHandlerFunc {
	return func(ctx context.Context, req mcpgo.CallToolRequest) (*mcpgo.CallToolResult, error) {
		key, err := req.RequireString("key")
		if err != nil {
			return toolError("sessions.patch", err)
		}
		sess := sessions.Get(ctx, key)
		if sess == nil {
			return mcpgo.NewToolResultError("sessions.patch: session not found: " + key), nil
		}

		if label := req.GetString("label", ""); label != "" {
			sessions.SetLabel(ctx, key, label)
		}
		if model := req.GetString("model", ""); model != "" {
			sessions.UpdateMetadata(ctx, key, model, sess.Provider, sess.Channel)
		}
		if args := req.GetArguments(); args != nil {
			if raw, ok := args["metadata"]; ok {
				if metaMap, ok := raw.(map[string]any); ok {
					metadata := make(map[string]string, len(metaMap))
					for k, v := range metaMap {
						if s, ok := v.(string); ok {
							metadata[k] = s
						}
					}
					sessions.SetSessionMetadata(ctx, key, metadata)
				}
			}
		}
		return jsonToolResult(map[string]any{"ok": true, "key": key})
	}
}

func handleSessionsDelete(sessions store.SessionStore) mcpserver.ToolHandlerFunc {
	return func(ctx context.Context, req mcpgo.CallToolRequest) (*mcpgo.CallToolResult, error) {
		key, err := req.RequireString("key")
		if err != nil {
			return toolError("sessions.delete", err)
		}
		if err := sessions.Delete(ctx, key); err != nil {
			return toolError("sessions.delete", err)
		}
		return jsonToolResult(map[string]bool{"ok": true})
	}
}

func handleSessionsReset(sessions store.SessionStore) mcpserver.ToolHandlerFunc {
	return func(ctx context.Context, req mcpgo.CallToolRequest) (*mcpgo.CallToolResult, error) {
		key, err := req.RequireString("key")
		if err != nil {
			return toolError("sessions.reset", err)
		}
		sessions.Reset(ctx, key)
		return jsonToolResult(map[string]bool{"ok": true})
	}
}

const defaultSessionCompactKeepLast = 20

func handleSessionsCompact(sessions store.SessionStore) mcpserver.ToolHandlerFunc {
	return func(ctx context.Context, req mcpgo.CallToolRequest) (*mcpgo.CallToolResult, error) {
		key, err := req.RequireString("key")
		if err != nil {
			return toolError("sessions.compact", err)
		}
		keepLast := int(req.GetFloat("keep_last", defaultSessionCompactKeepLast))
		history := sessions.GetHistory(ctx, key)
		original := len(history)
		sessions.TruncateHistory(ctx, key, keepLast)
		kept := min(original, keepLast)
		return jsonToolResult(map[string]any{"ok": true, "original": original, "kept": kept})
	}
}
