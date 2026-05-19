package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"time"

	mcpgo "github.com/mark3labs/mcp-go/mcp"
	mcpserver "github.com/mark3labs/mcp-go/server"

	"github.com/nextlevelbuilder/goclaw/internal/tools"
)

// NewSandboxAgentServer creates a StreamableHTTPServer that exposes the
// goclaw sub-agent tools as a standalone MCP service.
//
// Deploy this server independently and register its URL in the GoClaw UI
// as an MCP server — the task management agent can then call it via MCP protocol.
func NewSandboxAgentServer(version string) *mcpserver.StreamableHTTPServer {
	sessionStore := NewFormFillSessionStore()
	registeredTools := []tools.Tool{
		NewFeishuFormFillRunTool(sessionStore),
		NewFeishuFormFillInputTool(sessionStore),
		NewAgentFieldExecuteTool(),
		NewOpencodeRunTool(),
		NewOpencodeWorkspaceOpenTool(NewOpencodeWorkspaceStore()),
	}

	srv := mcpserver.NewMCPServer("goclaw-sandbox-agent", version,
		mcpserver.WithToolCapabilities(false),
	)

	for _, tool := range registeredTools {
		registerStandaloneTool(srv, tool)
	}
	registerProgressProbeTool(srv)

	return mcpserver.NewStreamableHTTPServer(srv,
		mcpserver.WithStateLess(true),
	)
}

func registerStandaloneTool(srv *mcpserver.MCPServer, tool tools.Tool) {
	schema, err := json.Marshal(tool.Parameters())
	if err != nil {
		schema = []byte(`{"type":"object"}`)
	}
	mcpTool := mcpgo.NewToolWithRawSchema(tool.Name(), tool.Description(), schema)

	srv.AddTool(mcpTool, func(ctx context.Context, req mcpgo.CallToolRequest) (*mcpgo.CallToolResult, error) {
		args := req.GetArguments()
		progressToken := any(nil)
		if req.Params.Meta != nil {
			progressToken = req.Params.Meta.ProgressToken
		}
		if progressToken != nil {
			ctx = tools.WithToolProgressCallback(ctx, func(ctx context.Context, ev tools.ProgressEvent) {
				payload := map[string]any{
					"progressToken": progressToken,
					"progress":      ev.Progress,
					"total":         ev.Total,
					"message":       ev.Message,
					"event":         ev.Event,
					"run_id":        ev.RunID,
					"timestamp":     ev.Timestamp,
					"event_data":    ev.EventData,
				}
				if err := srv.SendNotificationToClient(ctx, "notifications/progress", payload); err != nil {
					slog.Warn("mcp.sandbox_agent_server: progress notify failed",
						"tool", tool.Name(),
						"message", ev.Message,
						"error", err,
					)
				}
			})
		}
		result := tool.Execute(ctx, args)
		if result.IsError {
			slog.Warn("mcp.sandbox_agent_server: tool error", "error", result.ForLLM)
			return mcpgo.NewToolResultError(result.ForLLM), nil
		}
		return mcpgo.NewToolResultText(result.ForLLM), nil
	})

	slog.Info("mcp.sandbox_agent_server: tool registered", "tool", tool.Name())
}

func registerProgressProbeTool(srv *mcpserver.MCPServer) {
	schema := []byte(`{
		"type": "object",
		"properties": {
			"count": {
				"type": "integer",
				"description": "How many progress notifications to send. Defaults to 5."
			},
			"interval_sec": {
				"type": "integer",
				"description": "Seconds between notifications. Defaults to 2."
			}
		}
	}`)
	mcpTool := mcpgo.NewToolWithRawSchema(
		"mcp_progress_probe",
		"Test MCP progress streaming by sending numbered notifications while the tool is running.",
		schema,
	)

	srv.AddTool(mcpTool, func(ctx context.Context, req mcpgo.CallToolRequest) (*mcpgo.CallToolResult, error) {
		args := req.GetArguments()
		count := intArg(args["count"], 5)
		if count > 30 {
			count = 30
		}
		intervalSec := intArg(args["interval_sec"], 2)
		if intervalSec > 10 {
			intervalSec = 10
		}

		progressToken := any(nil)
		if req.Params.Meta != nil {
			progressToken = req.Params.Meta.ProgressToken
		}

		slog.Info("mcp.progress_probe.started",
			"count", count,
			"interval_sec", intervalSec,
			"has_progress_token", progressToken != nil,
		)

		for i := 1; i <= count; i++ {
			select {
			case <-ctx.Done():
				return mcpgo.NewToolResultError(ctx.Err().Error()), nil
			case <-time.After(time.Duration(intervalSec) * time.Second):
			}

			message := fmt.Sprintf("progress probe tick %d/%d", i, count)
			err := srv.SendNotificationToClient(ctx, "notifications/progress", map[string]any{
				"progressToken": progressToken,
				"progress":      i,
				"total":         count,
				"message":       message,
			})
			if err != nil {
				slog.Warn("mcp.progress_probe.notify_failed", "tick", i, "error", err)
			} else {
				slog.Info("mcp.progress_probe.notify_sent", "tick", i, "total", count, "message", message)
			}
			if i == count {
				time.Sleep(150 * time.Millisecond)
			}
		}

		return mcpgo.NewToolResultText(fmt.Sprintf("progress probe done: %d ticks", count)), nil
	})

	slog.Info("mcp.sandbox_agent_server: tool registered", "tool", "mcp_progress_probe")
}
