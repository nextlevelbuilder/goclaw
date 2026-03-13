package tools

import (
	"context"

	"github.com/nextlevelbuilder/goclaw/internal/config"
	"github.com/nextlevelbuilder/goclaw/internal/sandbox"
)

// Tool execution context keys.
// These replace mutable setter fields on tool instances, making tools thread-safe
// for concurrent execution. Values are injected into context by the registry
// and read by individual tools during Execute().

type toolContextKey string

const (
	ctxChannel     toolContextKey = "tool_channel"
	ctxChannelType toolContextKey = "tool_channel_type"
	ctxChatID      toolContextKey = "tool_chat_id"
	ctxPeerKind    toolContextKey = "tool_peer_kind"
	ctxLocalKey    toolContextKey = "tool_local_key" // composite key with topic/thread suffix for routing
	ctxSandboxKey  toolContextKey = "tool_sandbox_key"
	ctxAsyncCB     toolContextKey = "tool_async_cb"
	ctxWorkspace   toolContextKey = "tool_workspace"
	ctxAgentKey    toolContextKey = "tool_agent_key"
	ctxSessionKey  toolContextKey = "tool_session_key" // origin session key for announce routing
)

// Well-known channel names used for routing and access control.
const (
	ChannelSystem    = "system"
	ChannelDashboard = "dashboard"
	ChannelDelegate  = "delegate"
)

func WithToolChannel(ctx context.Context, channel string) context.Context {
	return context.WithValue(ctx, ctxChannel, channel)
}

func ToolChannelFromCtx(ctx context.Context) string {
	v, _ := ctx.Value(ctxChannel).(string)
	return v
}

func WithToolChannelType(ctx context.Context, channelType string) context.Context {
	return context.WithValue(ctx, ctxChannelType, channelType)
}

func ToolChannelTypeFromCtx(ctx context.Context) string {
	v, _ := ctx.Value(ctxChannelType).(string)
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

// WithToolSessionKey injects the parent's session key so subagent announce
// can route results back to the exact same session (required for WS where
// session keys don't follow BuildScopedSessionKey format).
func WithToolSessionKey(ctx context.Context, key string) context.Context {
	return context.WithValue(ctx, ctxSessionKey, key)
}

func ToolSessionKeyFromCtx(ctx context.Context) string {
	v, _ := ctx.Value(ctxSessionKey).(string)
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

// --- Per-agent restrict_to_workspace override ---

const ctxRestrictWs toolContextKey = "tool_restrict_to_workspace"

// WithRestrictToWorkspace injects a per-agent restrict_to_workspace override into context.
func WithRestrictToWorkspace(ctx context.Context, restrict bool) context.Context {
	return context.WithValue(ctx, ctxRestrictWs, restrict)
}

// RestrictFromCtx returns the per-agent restrict_to_workspace override.
func RestrictFromCtx(ctx context.Context) (bool, bool) {
	v, ok := ctx.Value(ctxRestrictWs).(bool)
	return v, ok
}

func effectiveRestrict(ctx context.Context, toolDefault bool) bool {
	if v, ok := RestrictFromCtx(ctx); ok {
		return v
	}
	return toolDefault
}

// --- Per-agent subagent config override ---

const ctxSubagentCfg toolContextKey = "tool_subagent_config"

func WithSubagentConfig(ctx context.Context, cfg *config.SubagentsConfig) context.Context {
	return context.WithValue(ctx, ctxSubagentCfg, cfg)
}

func SubagentConfigFromCtx(ctx context.Context) *config.SubagentsConfig {
	v, _ := ctx.Value(ctxSubagentCfg).(*config.SubagentsConfig)
	return v
}

// --- Per-agent memory config override ---

const ctxMemoryCfg toolContextKey = "tool_memory_config"

func WithMemoryConfig(ctx context.Context, cfg *config.MemoryConfig) context.Context {
	return context.WithValue(ctx, ctxMemoryCfg, cfg)
}

func MemoryConfigFromCtx(ctx context.Context) *config.MemoryConfig {
	v, _ := ctx.Value(ctxMemoryCfg).(*config.MemoryConfig)
	return v
}

// --- Workspace scope propagation (delegation origin) ---

const (
	ctxWsChannel toolContextKey = "tool_workspace_channel"
	ctxWsChatID  toolContextKey = "tool_workspace_chat_id"
)

func WithWorkspaceChannel(ctx context.Context, channel string) context.Context {
	return context.WithValue(ctx, ctxWsChannel, channel)
}

func WorkspaceChannelFromCtx(ctx context.Context) string {
	v, _ := ctx.Value(ctxWsChannel).(string)
	return v
}

func WithWorkspaceChatID(ctx context.Context, chatID string) context.Context {
	return context.WithValue(ctx, ctxWsChatID, chatID)
}

func WorkspaceChatIDFromCtx(ctx context.Context) string {
	v, _ := ctx.Value(ctxWsChatID).(string)
	return v
}

// --- Per-agent sandbox config override ---

const ctxSandboxCfg toolContextKey = "tool_sandbox_config"

func WithSandboxConfig(ctx context.Context, cfg *sandbox.Config) context.Context {
	return context.WithValue(ctx, ctxSandboxCfg, cfg)
}

func SandboxConfigFromCtx(ctx context.Context) *sandbox.Config {
	v, _ := ctx.Value(ctxSandboxCfg).(*sandbox.Config)
	return v
}
