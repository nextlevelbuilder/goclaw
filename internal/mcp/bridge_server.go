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
	"github.com/nextlevelbuilder/goclaw/internal/store"
	"github.com/nextlevelbuilder/goclaw/internal/tools"
)

// bridgeAlwaysExcluded lists tools that must never be exposed through the
// MCP bridge regardless of UI toggle state, because they are internal to the
// agent runtime rather than externally-callable capabilities.
var bridgeAlwaysExcluded = map[string]bool{
	"spawn":              true, // starts a nested agent loop
	"create_forum_topic": true, // channel-internal
	"heartbeat":          true, // internal health signal
}

// NewBridgeServer creates a StreamableHTTPServer that exposes the GoClaw
// tools enabled in the BuiltinToolStore as MCP tools over streamable-http
// transport (stateless mode).
//
// Tool selection sources:
//  1. BuiltinToolStore.ListEnabled — UI/seed-managed canonical list; any tool
//     toggled off in the builtin-tools page is immediately removed on next
//     startup (or cache rebuild).
//  2. bridgeAlwaysExcluded — safety whitelist (spawn, heartbeat, etc.).
//
// When btStore is nil (e.g. very early boot before stores are wired), the
// bridge falls back to an empty tool set rather than exposing everything.
//
// msgBus is optional; when non-nil, tools that produce media (deliver:true)
// will publish file attachments directly to the outbound bus.
func NewBridgeServer(reg *tools.Registry, btStore store.BuiltinToolStore, version string, msgBus *bus.MessageBus) *mcpserver.StreamableHTTPServer {
	srv := mcpserver.NewMCPServer("goclaw-bridge", version,
		mcpserver.WithToolCapabilities(false),
	)

	var registered int
	var skippedDisabled, skippedMissing, skippedExcluded int

	if btStore != nil {
		enabled, err := btStore.ListEnabled(context.Background())
		if err != nil {
			slog.Error("mcp.bridge: failed to list enabled builtin tools", "error", err)
		} else {
			for _, def := range enabled {
				if bridgeAlwaysExcluded[def.Name] {
					skippedExcluded++
					continue
				}
				t, ok := reg.Get(def.Name)
				if !ok {
					skippedMissing++
					continue
				}
				mcpTool := convertToMCPTool(t)
				handler := makeToolHandler(reg, def.Name, msgBus)
				srv.AddTool(mcpTool, handler)
				registered++
			}
		}
	}

	slog.Info("mcp.bridge: tools registered",
		"count", registered,
		"skipped_disabled", skippedDisabled,
		"skipped_missing", skippedMissing,
		"skipped_excluded", skippedExcluded,
	)

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

		// Pass routing context (channel, chatID, peerKind, sessionKey) so native
		// tools can access local_key, session_key etc. for forum topic routing.
		result := reg.ExecuteWithContext(ctx, toolName, args,
			tools.ToolChannelFromCtx(ctx),
			tools.ToolChatIDFromCtx(ctx),
			tools.ToolPeerKindFromCtx(ctx),
			tools.ToolSessionKeyFromCtx(ctx),
			nil,
		)

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
