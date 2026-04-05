package mcp

import (
	"context"
	"encoding/json"
	"log/slog"
	"mime"
	"path/filepath"
	"strings"
	"time"

	"github.com/google/uuid"
	mcpgo "github.com/mark3labs/mcp-go/mcp"
	mcpserver "github.com/mark3labs/mcp-go/server"

	"github.com/nextlevelbuilder/goclaw/internal/bus"
	"github.com/nextlevelbuilder/goclaw/internal/store"
	"github.com/nextlevelbuilder/goclaw/internal/tools"
	"github.com/nextlevelbuilder/goclaw/internal/tracing"
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

	// Register ALL bridge tools regardless of enabled/disabled state.
	// This ensures tools enabled at runtime (e.g. read_image via UI) are
	// discoverable by CLI agents. Execution checks enabled state at call time.
	var registered int
	for name := range BridgeToolNames {
		t, ok := reg.GetAny(name)
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
		start := time.Now().UTC()

		slog.Info("mcp.bridge: tool call", "tool", toolName, "args_keys", mapKeys(args))

		// Emit running tool span if trace context is available (injected by bridge middleware).
		spanID := emitBridgeToolSpanStart(ctx, start, toolName, args)

		result := reg.Execute(ctx, toolName, args)

		// Finalize the tool span with results.
		emitBridgeToolSpanEnd(ctx, spanID, start, toolName, result)

		if result.IsError {
			slog.Warn("mcp.bridge: tool error", "tool", toolName, "error", truncateStr(result.ForLLM, 200))
			return mcpgo.NewToolResultError(result.ForLLM), nil
		}

		slog.Info("mcp.bridge: tool result", "tool", toolName, "result_len", len(result.ForLLM))

		// Forward media files to the outbound bus so they reach the user as attachments.
		// This is necessary because Claude CLI processes tool results internally —
		// GoClaw's agent loop never sees result.Media from bridge tool calls.
		forwardMediaToOutbound(ctx, msgBus, toolName, result)

		return mcpgo.NewToolResultText(result.ForLLM), nil
	}
}

// emitBridgeToolSpanStart emits a "running" tool span for a bridge tool call.
// Returns uuid.Nil if tracing is not available in context.
func emitBridgeToolSpanStart(ctx context.Context, start time.Time, toolName string, args map[string]any) uuid.UUID {
	collector := tracing.CollectorFromContext(ctx)
	traceID := tracing.TraceIDFromContext(ctx)
	if collector == nil || traceID == uuid.Nil {
		return uuid.Nil
	}

	inputJSON, _ := json.Marshal(args)
	previewLimit := 40_000
	if collector.Verbose() {
		previewLimit = 200_000
	}

	spanID := store.GenNewID()
	span := store.SpanData{
		ID:           spanID,
		TraceID:      traceID,
		SpanType:     store.SpanTypeToolCall,
		Name:         toolName,
		StartTime:    start,
		ToolName:     toolName,
		InputPreview: tracing.TruncateJSON(string(inputJSON), previewLimit),
		Status:       store.SpanStatusRunning,
		Level:        store.SpanLevelDefault,
		CreatedAt:    start,
	}
	if parentID := tracing.ParentSpanIDFromContext(ctx); parentID != uuid.Nil {
		span.ParentSpanID = &parentID
	}
	if agentID := bridgeAgentIDFromContext(ctx); agentID != uuid.Nil {
		span.AgentID = &agentID
	}
	span.TenantID = store.TenantIDFromContext(ctx)
	if span.TenantID == uuid.Nil {
		span.TenantID = store.MasterTenantID
	}

	collector.EmitSpan(span)
	return spanID
}

// emitBridgeToolSpanEnd finalizes a running bridge tool span with results.
func emitBridgeToolSpanEnd(ctx context.Context, spanID uuid.UUID, start time.Time, toolName string, result *tools.Result) {
	if spanID == uuid.Nil {
		return
	}
	collector := tracing.CollectorFromContext(ctx)
	traceID := tracing.TraceIDFromContext(ctx)
	if collector == nil || traceID == uuid.Nil {
		return
	}

	now := time.Now().UTC()
	previewLimit := 40_000
	if collector.Verbose() {
		previewLimit = 200_000
	}

	updates := map[string]any{
		"end_time":       now,
		"duration_ms":    int(now.Sub(start).Milliseconds()),
		"status":         store.SpanStatusCompleted,
		"output_preview": tracing.TruncateMid(result.ForLLM, previewLimit),
	}
	if result.IsError {
		updates["status"] = store.SpanStatusError
		updates["error"] = truncateStr(result.ForLLM, 200)
	}
	// Record token usage from tools that make internal LLM calls (e.g. read_image).
	if result.Usage != nil {
		updates["input_tokens"] = result.Usage.PromptTokens
		updates["output_tokens"] = result.Usage.CompletionTokens
		updates["provider"] = result.Provider
		updates["model"] = result.Model
	}

	collector.EmitSpanUpdate(spanID, traceID, updates)
}

// mapKeys returns the keys of a map for logging (avoids logging full args).
func mapKeys(m map[string]any) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	return keys
}

// truncateStr truncates a string to maxLen for logging.
func truncateStr(s string, maxLen int) string {
	if len(s) > maxLen {
		return s[:maxLen] + "..."
	}
	return s
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
