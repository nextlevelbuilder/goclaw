package tools

import (
	"context"

	"github.com/nextlevelbuilder/goclaw/internal/bus"
	"github.com/nextlevelbuilder/goclaw/internal/providers"
	"github.com/nextlevelbuilder/goclaw/internal/store"
)

// Tool is the interface all tools must implement.
type Tool interface {
	Name() string
	Description() string
	Parameters() map[string]any
	Execute(ctx context.Context, args map[string]any) *Result
}

// ContextualTool receives channel/chat context before execution.
type ContextualTool interface {
	Tool
	SetContext(channel, chatID string)
}

// PeerKindAware tools receive the peer kind (direct/group) before execution.
type PeerKindAware interface {
	SetPeerKind(peerKind string)
}

// SandboxAware tools receive sandbox scope key before execution.
// Used by exec tool to route commands through Docker containers.
type SandboxAware interface {
	SetSandboxKey(key string)
}

// AsyncCallback is invoked when an async tool completes.
type AsyncCallback func(ctx context.Context, result *Result)

// AsyncTool supports asynchronous execution with completion callbacks.
type AsyncTool interface {
	Tool
	SetCallback(cb AsyncCallback)
}

// --- Configuration interfaces for reducing type assertions in cmd/ wiring ---

// InterceptorAware tools can receive ContextFile and Memory interceptors.
type InterceptorAware interface {
	SetContextFileInterceptor(*ContextFileInterceptor)
	SetMemoryInterceptor(*MemoryInterceptor)
}

// GroupWriterAware tools receive a GroupWriterCache for group permission checks.
type GroupWriterAware interface {
	SetGroupWriterCache(*store.GroupWriterCache)
}

// WorkspaceInterceptorAware tools can receive a WorkspaceInterceptor for team workspace validation.
type WorkspaceInterceptorAware interface {
	SetWorkspaceInterceptor(*WorkspaceInterceptor)
}

// MemoryStoreAware tools can receive a MemoryStore for Postgres queries.
type MemoryStoreAware interface {
	SetMemoryStore(store.MemoryStore)
}

// ApprovalAware tools can receive an ExecApprovalManager.
type ApprovalAware interface {
	SetApprovalManager(*ExecApprovalManager, string)
}

// PathAllowable tools can allow extra path prefixes for read access.
type PathAllowable interface {
	AllowPaths(...string)
}

// PathDenyable tools can deny access to specific path prefixes within the workspace.
type PathDenyable interface {
	DenyPaths(...string)
}

// SessionStoreAware tools can receive a SessionStore for session queries.
type SessionStoreAware interface {
	SetSessionStore(store.SessionStore)
}

// BusAware tools can receive a MessageBus for publishing messages.
type BusAware interface {
	SetMessageBus(*bus.MessageBus)
}

// ChannelSender abstracts sending a message to a channel.
// Implemented by channels.Manager.SendToChannel.
type ChannelSender func(ctx context.Context, channel, chatID, content string) error

// ChannelSenderAware tools can receive a channel sender function.
type ChannelSenderAware interface {
	SetChannelSender(ChannelSender)
}

// ToProviderDef converts a Tool to a providers.ToolDefinition for LLM APIs.
func ToProviderDef(t Tool) providers.ToolDefinition {
	return providers.ToolDefinition{
		Type: "function",
		Function: providers.ToolFunctionSchema{
			Name:        t.Name(),
			Description: t.Description(),
			Parameters:  t.Parameters(),
		},
	}
}
