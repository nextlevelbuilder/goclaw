package mcp

import (
	"context"
	"encoding/json"
	"log/slog"
	"mime"
	"path/filepath"
	"strings"

	mcpgo "github.com/mark3labs/mcp-go/mcp"
	mcpserver "github.com/mark3labs/mcp-go/server"

	"github.com/nextlevelbuilder/goclaw/internal/bus"
	"github.com/nextlevelbuilder/goclaw/internal/tools"
)

// BridgeToolNames is the subset of GoClaw tools exposed via the MCP bridge.
// Excluded: spawn (agent loop), create_forum_topic (channels).
var BridgeToolNames = map[string]bool{
	// Filesystem
	"read_file":  true,
	"write_file": true,
	"list_files": true,
	"edit":       true,
	"exec":       true,
	// Web
	"web_search": true,
	"web_fetch":  true,
	// Memory & knowledge
	"memory_search": true,
	"memory_get":    true,
	"skill_search":  true,
	// Media
	"read_image":   true,
	"create_image": true,
	"tts":          true,
	// Browser automation
	"browser": true,
	// Scheduler
	"cron": true,
	// Messaging (send text/files to channels)
	"message": true,
	// Sessions (read + send)
	"sessions_list":    true,
	"session_status":   true,
	"sessions_history": true,
	"sessions_send":    true,
	// Team tools (context from X-Agent-ID/X-Channel/X-Chat-ID headers)
	"team_tasks": true,
}

// NewBridgeServer creates a StreamableHTTPServer that exposes GoClaw tools as MCP tools.
// It reads tools from the registry, filters to BridgeToolNames, and serves them
// over streamable-http transport (stateless mode).
// msgBus is optional; when non-nil, tools that produce media (deliver:true) will
// publish file attachments directly to the outbound bus.
func NewBridgeServer(reg *tools.Registry, version string, msgBus *bus.MessageBus) *mcpserver.StreamableHTTPServer {
	srv := mcpserver.NewMCPServer("goclaw-bridge", version,
		mcpserver.WithToolCapabilities(false),
	)

	// Register each safe tool from the GoClaw registry
	var registered int
	for name := range BridgeToolNames {
		t, ok := reg.Get(name)
		if !ok {
			continue
		}

		mcpTool := convertToMCPTool(t)
		handler := makeToolHandler(reg, name, msgBus)
		srv.AddTool(mcpTool, handler)
		registered++
	}

	slog.Info("mcp.bridge: tools registered", "count", registered)

	return mcpserver.NewStreamableHTTPServer(srv,
		mcpserver.WithStateLess(true),
	)
}

// convertToMCPTool converts a GoClaw tools.Tool into an mcp-go Tool.
func convertToMCPTool(t tools.Tool) mcpgo.Tool {
	schema, err := json.Marshal(t.Parameters())
	if err != nil {
		// Fallback: empty object schema
		schema = []byte(`{"type":"object"}`)
	}
	return mcpgo.NewToolWithRawSchema(t.Name(), t.Description(), schema)
}

// makeToolHandler creates a ToolHandlerFunc that delegates to the GoClaw tool registry.
// When msgBus is non-nil and a tool result contains Media paths, the handler publishes
// them as outbound media attachments so files reach the user (e.g. Telegram document).
func makeToolHandler(reg *tools.Registry, toolName string, msgBus *bus.MessageBus) mcpserver.ToolHandlerFunc {
	return func(ctx context.Context, req mcpgo.CallToolRequest) (*mcpgo.CallToolResult, error) {
		args := req.GetArguments()

		result := reg.Execute(ctx, toolName, args)

		if result.IsError {
			return mcpgo.NewToolResultError(result.ForLLM), nil
		}

		// Forward media files to the outbound bus so they reach the user as attachments.
		// This is necessary because Claude CLI processes tool results internally —
		// GoClaw's agent loop never sees result.Media from bridge tool calls.
		forwardMediaToOutbound(ctx, msgBus, toolName, result)

		return mcpgo.NewToolResultText(result.ForLLM), nil
	}
}

// forwardMediaToOutbound publishes media files from a tool result to the outbound bus.
func forwardMediaToOutbound(ctx context.Context, msgBus *bus.MessageBus, toolName string, result *tools.Result) {
	if msgBus == nil || len(result.Media) == 0 {
		return
	}
	channel := tools.ToolChannelFromCtx(ctx)
	chatID := tools.ToolChatIDFromCtx(ctx)
	if channel == "" || chatID == "" {
		slog.Debug("mcp.bridge: skipping media forward, missing channel context",
			"tool", toolName, "channel", channel, "chat_id", chatID)
		return
	}

	var attachments []bus.MediaAttachment
	for _, mf := range result.Media {
		ct := mf.MimeType
		if ct == "" {
			ct = mimeFromExt(filepath.Ext(mf.Path))
		}
		attachments = append(attachments, bus.MediaAttachment{
			URL:         mf.Path,
			ContentType: ct,
		})
	}

	peerKind := tools.ToolPeerKindFromCtx(ctx)
	var meta map[string]string
	if peerKind == "group" {
		meta = map[string]string{"group_id": chatID}
	}
	msgBus.PublishOutbound(bus.OutboundMessage{
		Channel:  channel,
		ChatID:   chatID,
		Media:    attachments,
		Metadata: meta,
	})
	slog.Debug("mcp.bridge: forwarded media to outbound bus",
		"tool", toolName, "channel", channel, "files", len(attachments))
}

// mimeFromExt returns a MIME type for a file extension.
// Uses Go stdlib first, falls back to a small map for types not reliably
// handled by mime.TypeByExtension on all platforms (e.g. .opus, .webp).
func mimeFromExt(ext string) string {
	if ct := mime.TypeByExtension(ext); ct != "" {
		return ct
	}
	switch strings.ToLower(ext) {
	case ".webp":
		return "image/webp"
	case ".opus":
		return "audio/ogg"
	case ".md":
		return "text/markdown"
	default:
		return "application/octet-stream"
	}
}
