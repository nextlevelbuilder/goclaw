package mcp

import (
	"context"
	"encoding/json"

	"github.com/google/uuid"

	mcpgo "github.com/mark3labs/mcp-go/mcp"

	"github.com/nextlevelbuilder/goclaw/internal/store"
)

// jsonToolResult marshals v as an MCP text tool result. Marshal failures are
// reported as tool errors rather than propagated as transport errors, since
// they indicate a bug in the value being returned, not a request problem.
func jsonToolResult(v any) (*mcpgo.CallToolResult, error) {
	data, err := json.Marshal(v)
	if err != nil {
		return mcpgo.NewToolResultError("marshal result: " + err.Error()), nil
	}
	return mcpgo.NewToolResultText(string(data)), nil
}

// toolError wraps an error as an MCP tool-level error result (not a
// transport-level error), so the calling LLM sees the failure reason in the
// tool response instead of the call silently failing.
func toolError(prefix string, err error) (*mcpgo.CallToolResult, error) {
	return mcpgo.NewToolResultError(prefix + ": " + err.Error()), nil
}

// resolveAgentUUID resolves an agent identifier (either UUID or agent_key) to
// its canonical UUID via a DB lookup. Mirrors
// internal/gateway/methods/agent_links.go's unexported helper of the same
// name — duplicated rather than imported since crud_*.go is a standalone MCP
// surface that does not depend on internal/gateway/methods (which itself
// depends on internal/gateway, which imports this package — importing
// methods here would create an import cycle).
func resolveAgentUUID(ctx context.Context, agents store.AgentStore, keyOrID string) (uuid.UUID, error) {
	if id, err := uuid.Parse(keyOrID); err == nil {
		ag, err := agents.GetByID(ctx, id)
		if err != nil {
			return uuid.Nil, err
		}
		return ag.ID, nil
	}
	ag, err := agents.GetByKey(ctx, keyOrID)
	if err != nil {
		return uuid.Nil, err
	}
	return ag.ID, nil
}

// resolveAgentInfo returns full agent data for an agent identifier (UUID or
// agent_key). See resolveAgentUUID for why this is duplicated here.
func resolveAgentInfo(ctx context.Context, agents store.AgentStore, keyOrID string) (*store.AgentData, error) {
	if id, err := uuid.Parse(keyOrID); err == nil {
		return agents.GetByID(ctx, id)
	}
	return agents.GetByKey(ctx, keyOrID)
}

// mcpTenantHeader is the HTTP header a CRUD MCP caller may use to scope a
// request to a specific tenant, mirroring internal/http/auth.go's
// "X-GoClaw-Tenant-Id" header used by the gateway-token (owner) and
// system-level API key auth paths. The MCP bearer token
// (gateway.mcp_server_token) is a single shared secret with no per-caller
// identity, so it is treated the same way the owner branch of
// resolveAuthWithBearer treats the gateway token: any tenant may be
// requested, with no membership check, falling back to store.MasterTenantID
// when absent or unresolvable.
const mcpTenantHeader = "X-GoClaw-Tenant-Id"

// resolveMCPTenantID resolves the caller-supplied tenant header (UUID or
// slug) to a concrete tenant UUID using tenants, falling back to
// store.MasterTenantID when the header is empty, unresolvable, or tenants is
// nil. This is the sole place tenant scope is established for the CRUD MCP
// surface (see NewCRUDServer's mcpserver.WithHTTPContextFunc wiring) — every
// tool handler in this package relies on the incoming ctx already carrying a
// concrete tenant ID via store.WithTenantID.
func resolveMCPTenantID(ctx context.Context, tenants store.TenantStore, headerVal string) uuid.UUID {
	if headerVal == "" || tenants == nil {
		return store.MasterTenantID
	}
	if id, err := uuid.Parse(headerVal); err == nil {
		if t, err := tenants.GetTenant(ctx, id); err == nil && t != nil {
			return t.ID
		}
		return store.MasterTenantID
	}
	if t, err := tenants.GetTenantBySlug(ctx, headerVal); err == nil && t != nil {
		return t.ID
	}
	return store.MasterTenantID
}

// ChatMediaItem represents a media file attached to a goclaw_chat_send call,
// mirroring internal/gateway/methods/chat.go's chatMediaItem.
type ChatMediaItem struct {
	Path     string
	Filename string
}

// ChatSendResult is the outcome of a goclaw_chat_send call.
type ChatSendResult struct {
	RunID        string `json:"runId"`
	SessionKey   string `json:"sessionKey"`
	Content      string `json:"content"`
	Usage        any    `json:"usage,omitempty"`
	Thinking     string `json:"thinking,omitempty"`
	Media        any    `json:"media,omitempty"`
	Cancelled    bool   `json:"cancelled,omitempty"`
}

// ChatAbortResult is the outcome of a goclaw_chat_abort call, mirroring
// internal/gateway/methods/chat.go's handleAbort response shape.
type ChatAbortResult struct {
	OK              bool     `json:"ok"`
	Aborted         bool     `json:"aborted"`
	Stopped         bool     `json:"stopped"`
	Forced          bool     `json:"forced"`
	AlreadyAborting bool     `json:"alreadyAborting"`
	NotFound        bool     `json:"notFound"`
	RunIDs          []string `json:"runIds"`
}

// ChatActivity describes the current in-flight agent activity for a session.
type ChatActivity struct {
	Phase     string `json:"phase"`
	Tool      string `json:"tool"`
	Iteration int    `json:"iteration"`
}

// ChatSessionStatusResult is the outcome of a goclaw_chat_session_status call.
type ChatSessionStatusResult struct {
	IsRunning bool          `json:"isRunning"`
	RunID     string        `json:"runId"`
	Activity  *ChatActivity `json:"activity,omitempty"`
}

// ChatRunner executes and controls agent chat runs on behalf of the CRUD MCP
// server. It is implemented by internal/gateway.Server (which holds the live
// *agent.Router) and injected here as an interface — internal/agent already
// imports internal/mcp (loop_mcp_user.go), so importing agent.Router directly
// in this package would create an import cycle. Same workaround as
// AgentRuntimeLookup in crud_agents.go.
//
// Unlike the WS chat.send/chat.abort/chat.session.status RPC methods, calls
// through this interface have no per-WS-client concept: no rate limiting, no
// send debouncing, and no session-ownership check against a caller identity.
// The MCP bearer token (gateway.mcp_server_token) is the sole security
// boundary here, matching the rest of this CRUD MCP surface (e.g.
// goclaw_sessions_* has no ownership checks either).
type ChatRunner interface {
	// Send runs the agent for sessionKey (creating a new session key when
	// empty) and returns the final result. Always synchronous/non-streaming:
	// MCP tool calls are request/response, so there is no channel to forward
	// incremental run events to the caller.
	Send(ctx context.Context, agentID, sessionKey, message string, media []ChatMediaItem) (*ChatSendResult, error)
	// Abort cancels the run(s) matching runID and/or sessionKey (at least one
	// must be non-empty).
	Abort(ctx context.Context, runID, sessionKey string) (*ChatAbortResult, error)
	// SessionStatus reports whether sessionKey currently has an in-flight run.
	SessionStatus(ctx context.Context, sessionKey string) (*ChatSessionStatusResult, error)
}
