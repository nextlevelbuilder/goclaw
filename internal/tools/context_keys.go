package tools

import (
	"context"

	"github.com/nextlevelbuilder/goclaw/internal/config"
)

// Tool execution context keys.
// These replace mutable setter fields on tool instances, making tools thread-safe
// for concurrent execution. Values are injected into context by the registry
// and read by individual tools during Execute().

type toolContextKey string

const (
	ctxChannel    toolContextKey = "tool_channel"
	ctxChatID     toolContextKey = "tool_chat_id"
	ctxPeerKind   toolContextKey = "tool_peer_kind"
	ctxLocalKey   toolContextKey = "tool_local_key" // composite key with topic/thread suffix for routing
	ctxSandboxKey toolContextKey = "tool_sandbox_key"
	ctxAsyncCB    toolContextKey = "tool_async_cb"
	ctxWorkspace  toolContextKey = "tool_workspace"
	ctxAgentKey   toolContextKey = "tool_agent_key"
)

func WithToolChannel(ctx context.Context, channel string) context.Context {
	return context.WithValue(ctx, ctxChannel, channel)
}

func ToolChannelFromCtx(ctx context.Context) string {
	v, _ := ctx.Value(ctxChannel).(string)
	return v
}

func WithToolChatID(ctx context.Context, chatID string) context.Context {
	return context.WithValue(ctx, ctxChatID, chatID)
}

func ToolChatIDFromCtx(ctx context.Context) string {
	v, _ := ctx.Value(ctxChatID).(string)
	return v
}

func WithToolPeerKind(ctx context.Context, peerKind string) context.Context {
	return context.WithValue(ctx, ctxPeerKind, peerKind)
}

func ToolPeerKindFromCtx(ctx context.Context) string {
	v, _ := ctx.Value(ctxPeerKind).(string)
	return v
}

// WithToolLocalKey injects the composite local key (e.g. "-100123:topic:42") into context.
// Used by delegation/subagent to preserve topic routing info for announce-back.
func WithToolLocalKey(ctx context.Context, localKey string) context.Context {
	return context.WithValue(ctx, ctxLocalKey, localKey)
}

func ToolLocalKeyFromCtx(ctx context.Context) string {
	v, _ := ctx.Value(ctxLocalKey).(string)
	return v
}

func WithToolSandboxKey(ctx context.Context, key string) context.Context {
	return context.WithValue(ctx, ctxSandboxKey, key)
}

func ToolSandboxKeyFromCtx(ctx context.Context) string {
	v, _ := ctx.Value(ctxSandboxKey).(string)
	return v
}

func WithToolAsyncCB(ctx context.Context, cb AsyncCallback) context.Context {
	return context.WithValue(ctx, ctxAsyncCB, cb)
}

func ToolAsyncCBFromCtx(ctx context.Context) AsyncCallback {
	v, _ := ctx.Value(ctxAsyncCB).(AsyncCallback)
	return v
}

func WithToolWorkspace(ctx context.Context, ws string) context.Context {
	return context.WithValue(ctx, ctxWorkspace, ws)
}

func ToolWorkspaceFromCtx(ctx context.Context) string {
	v, _ := ctx.Value(ctxWorkspace).(string)
	return v
}

// WithToolAgentKey injects the calling agent's key into context.
// Multiple agents share a single tool registry; the agent key
// lets tools like spawn/subagent identify which agent is the parent.
func WithToolAgentKey(ctx context.Context, key string) context.Context {
	return context.WithValue(ctx, ctxAgentKey, key)
}

func ToolAgentKeyFromCtx(ctx context.Context) string {
	v, _ := ctx.Value(ctxAgentKey).(string)
	return v
}

// --- Vision / ImageGen config (per-agent overrides) ---

const (
	ctxVisionConfig   toolContextKey = "tool_vision_config"
	ctxImageGenConfig toolContextKey = "tool_imagegen_config"
)

func WithVisionConfig(ctx context.Context, cfg *config.VisionConfig) context.Context {
	return context.WithValue(ctx, ctxVisionConfig, cfg)
}

func VisionConfigFromCtx(ctx context.Context) *config.VisionConfig {
	v, _ := ctx.Value(ctxVisionConfig).(*config.VisionConfig)
	return v
}

func WithImageGenConfig(ctx context.Context, cfg *config.ImageGenConfig) context.Context {
	return context.WithValue(ctx, ctxImageGenConfig, cfg)
}

func ImageGenConfigFromCtx(ctx context.Context) *config.ImageGenConfig {
	v, _ := ctx.Value(ctxImageGenConfig).(*config.ImageGenConfig)
	return v
}

// --- Builtin tool settings (global DB overrides) ---

const ctxBuiltinToolSettings toolContextKey = "tool_builtin_settings"

// BuiltinToolSettings maps tool name → settings JSON bytes.
type BuiltinToolSettings map[string][]byte

func WithBuiltinToolSettings(ctx context.Context, settings BuiltinToolSettings) context.Context {
	return context.WithValue(ctx, ctxBuiltinToolSettings, settings)
}

func BuiltinToolSettingsFromCtx(ctx context.Context) BuiltinToolSettings {
	v, _ := ctx.Value(ctxBuiltinToolSettings).(BuiltinToolSettings)
	return v
}
