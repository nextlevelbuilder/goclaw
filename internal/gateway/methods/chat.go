package methods

import (
	"context"
	"encoding/json"

	"github.com/google/uuid"

	"github.com/nextlevelbuilder/goclaw/internal/agent"
	"github.com/nextlevelbuilder/goclaw/internal/gateway"
	"github.com/nextlevelbuilder/goclaw/internal/providers"
	"github.com/nextlevelbuilder/goclaw/internal/sessions"
	"github.com/nextlevelbuilder/goclaw/internal/store"
	"github.com/nextlevelbuilder/goclaw/pkg/protocol"
)

// ChatMethods handles chat.send, chat.history, chat.abort, chat.inject.
type ChatMethods struct {
	agents      *agent.Router
	sessions    store.SessionStore
	rateLimiter *gateway.RateLimiter
}

func NewChatMethods(agents *agent.Router, sess store.SessionStore, rl *gateway.RateLimiter) *ChatMethods {
	return &ChatMethods{agents: agents, sessions: sess, rateLimiter: rl}
}

// Register adds chat methods to the router.
func (m *ChatMethods) Register(router *gateway.MethodRouter) {
	router.Register(protocol.MethodChatSend, m.handleSend)
	router.Register(protocol.MethodChatHistory, m.handleHistory)
	router.Register(protocol.MethodChatAbort, m.handleAbort)
	router.Register(protocol.MethodChatInject, m.handleInject)
}

type chatSendParams struct {
	Message    string `json:"message"`
	AgentID    string `json:"agentId"`
	SessionKey string `json:"sessionKey"`
	Stream     bool   `json:"stream"`
}

func (m *ChatMethods) handleSend(ctx context.Context, client *gateway.Client, req *protocol.RequestFrame) {
	// Rate limit check per user/client
	if m.rateLimiter != nil && m.rateLimiter.Enabled() {
		key := client.UserID()
		if key == "" {
			key = client.ID()
		}
		if !m.rateLimiter.Allow(key) {
			client.SendResponse(protocol.NewErrorResponse(req.ID, protocol.ErrInvalidRequest, "rate limit exceeded — please wait before sending more messages"))
			return
		}
	}

	var params chatSendParams
	if err := json.Unmarshal(req.Params, &params); err != nil {
		client.SendResponse(protocol.NewErrorResponse(req.ID, protocol.ErrInvalidRequest, "invalid params: "+err.Error()))
		return
	}

	if params.AgentID == "" {
		params.AgentID = "default"
	}

	loop, err := m.agents.Get(params.AgentID)
	if err != nil {
		client.SendResponse(protocol.NewErrorResponse(req.ID, protocol.ErrNotFound, err.Error()))
		return
	}

	userID := client.UserID()
	if userID == "" {
		client.SendResponse(protocol.NewErrorResponse(req.ID, protocol.ErrInvalidRequest, "user_id is required in managed mode — provide it in the connect handshake"))
		return
	}

	runID := uuid.NewString()
	sessionKey := params.SessionKey
	if sessionKey == "" {
		sessionKey = sessions.SessionKey(params.AgentID, "ws-"+client.ID())
	}

	// Inject user_id into context for downstream stores/tools
	runCtxBase := ctx
	if userID != "" {
		runCtxBase = store.WithUserID(runCtxBase, userID)
	}

	// Create cancellable context for abort support (matching TS AbortController pattern).
	runCtx, cancel := context.WithCancel(runCtxBase)
	m.agents.RegisterRun(runID, sessionKey, params.AgentID, cancel)

	// Run agent asynchronously - events are broadcast via the event system
	go func() {
		defer m.agents.UnregisterRun(runID)
		defer cancel()

		result, err := loop.Run(runCtx, agent.RunRequest{
			SessionKey: sessionKey,
			Message:    params.Message,
			Channel:    "ws",
			ChatID:     client.ID(),
			RunID:      runID,
			UserID:     userID,
			Stream:     params.Stream,
		})

		if err != nil {
			// Don't send error if context was cancelled (abort)
			if runCtx.Err() != nil {
				return
			}
			client.SendResponse(protocol.NewErrorResponse(req.ID, protocol.ErrInternal, err.Error()))
			return
		}

		client.SendResponse(protocol.NewOKResponse(req.ID, map[string]interface{}{
			"runId":   result.RunID,
			"content": result.Content,
			"usage":   result.Usage,
		}))
	}()
}

type chatHistoryParams struct {
	AgentID    string `json:"agentId"`
	SessionKey string `json:"sessionKey"`
}

func (m *ChatMethods) handleHistory(ctx context.Context, client *gateway.Client, req *protocol.RequestFrame) {
	var params chatHistoryParams
	if err := json.Unmarshal(req.Params, &params); err != nil {
		client.SendResponse(protocol.NewErrorResponse(req.ID, protocol.ErrInvalidRequest, "invalid params: "+err.Error()))
		return
	}

	if params.AgentID == "" {
		params.AgentID = "default"
	}

	sessionKey := params.SessionKey
	if sessionKey == "" {
		sessionKey = sessions.SessionKey(params.AgentID, "ws-"+client.ID())
	}

	history := m.sessions.GetHistory(sessionKey)

	client.SendResponse(protocol.NewOKResponse(req.ID, map[string]interface{}{
		"messages": history,
	}))
}

// handleInject injects a message into a session transcript without running the agent.
// Matching TS chat.inject (src/gateway/server-methods/chat.ts:686-746).
func (m *ChatMethods) handleInject(_ context.Context, client *gateway.Client, req *protocol.RequestFrame) {
	var params struct {
		SessionKey string `json:"sessionKey"`
		Message    string `json:"message"`
		Label      string `json:"label"`
	}
	if err := json.Unmarshal(req.Params, &params); err != nil {
		client.SendResponse(protocol.NewErrorResponse(req.ID, protocol.ErrInvalidRequest, "invalid params: "+err.Error()))
		return
	}

	if params.SessionKey == "" {
		client.SendResponse(protocol.NewErrorResponse(req.ID, protocol.ErrInvalidRequest, "sessionKey is required"))
		return
	}
	if params.Message == "" {
		client.SendResponse(protocol.NewErrorResponse(req.ID, protocol.ErrInvalidRequest, "message is required"))
		return
	}

	// Truncate label
	if len(params.Label) > 100 {
		params.Label = params.Label[:100]
	}

	// Build content text
	text := params.Message
	if params.Label != "" {
		text = "[" + params.Label + "]\n\n" + params.Message
	}

	// Create an assistant message with gateway-injected metadata
	messageID := uuid.NewString()
	m.sessions.AddMessage(params.SessionKey, providers.Message{
		Role:    "assistant",
		Content: text,
	})

	client.SendResponse(protocol.NewOKResponse(req.ID, map[string]interface{}{
		"ok":        true,
		"messageId": messageID,
	}))
}

// handleAbort cancels running agent invocations.
// Matching TS chat-abort.ts: validates sessionKey, supports per-runId or per-session abort.
//
// Params:
//
//	{ sessionKey: string, runId?: string }
//
// Response:
//
//	{ ok: true, aborted: bool, runIds: []string }
func (m *ChatMethods) handleAbort(_ context.Context, client *gateway.Client, req *protocol.RequestFrame) {
	var params struct {
		RunID      string `json:"runId"`
		SessionKey string `json:"sessionKey"`
	}
	if err := json.Unmarshal(req.Params, &params); err != nil {
		client.SendResponse(protocol.NewErrorResponse(req.ID, protocol.ErrInvalidRequest, "invalid params: "+err.Error()))
		return
	}

	if params.SessionKey == "" && params.RunID == "" {
		client.SendResponse(protocol.NewErrorResponse(req.ID, protocol.ErrInvalidRequest, "sessionKey or runId is required"))
		return
	}

	var abortedIDs []string

	if params.RunID != "" {
		// Abort specific run (with sessionKey authorization)
		if m.agents.AbortRun(params.RunID, params.SessionKey) {
			abortedIDs = append(abortedIDs, params.RunID)
		}
	} else {
		// Abort all runs for session
		abortedIDs = m.agents.AbortRunsForSession(params.SessionKey)
	}

	client.SendResponse(protocol.NewOKResponse(req.ID, map[string]interface{}{
		"ok":      true,
		"aborted": len(abortedIDs) > 0,
		"runIds":  abortedIDs,
	}))
}
