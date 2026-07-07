package gateway

import (
	"context"
	"fmt"

	"github.com/google/uuid"

	"github.com/nextlevelbuilder/goclaw/internal/agent"
	"github.com/nextlevelbuilder/goclaw/internal/bus"
	"github.com/nextlevelbuilder/goclaw/internal/channels/media"
	mcpbridge "github.com/nextlevelbuilder/goclaw/internal/mcp"
	"github.com/nextlevelbuilder/goclaw/internal/sessions"
)

// agentChatRunner implements mcpbridge.ChatRunner against the live
// *agent.Router, backing the CRUD MCP server's goclaw_chat_* tools
// (see internal/mcp/crud_chat.go). It intentionally omits the WS-only
// concerns handled by ChatMethods (internal/gateway/methods/chat.go): rate
// limiting, send debouncing, and per-WS-client session ownership checks. The
// MCP bearer token (gateway.mcp_server_token) is the sole security boundary
// for this surface, same as the rest of the CRUD MCP server.
type agentChatRunner struct {
	agents *agent.Router
}

// Send runs the agent synchronously and returns the final result. Always
// non-streaming: MCP tool calls are request/response, so there is no channel
// to forward incremental run events to the caller.
func (r *agentChatRunner) Send(ctx context.Context, agentID, sessionKey, message string, mediaItems []mcpbridge.ChatMediaItem) (*mcpbridge.ChatSendResult, error) {
	if agentID == "" {
		if sessionKey != "" {
			if parsedAgentID, _ := sessions.ParseSessionKey(sessionKey); parsedAgentID != "" {
				agentID = parsedAgentID
			}
		}
		if agentID == "" {
			agentID = "default"
		}
	}

	loop, err := r.agents.Get(ctx, agentID)
	if err != nil {
		return nil, fmt.Errorf("get agent %q: %w", agentID, err)
	}

	if sessionKey == "" {
		sessionKey = sessions.BuildWSSessionKey(agentID, uuid.NewString())
	}

	var mediaFiles []bus.MediaFile
	for _, item := range mediaItems {
		mediaFiles = append(mediaFiles, bus.MediaFile{
			Path:     item.Path,
			MimeType: media.DetectMIMEType(item.Path),
			Filename: item.Filename,
		})
	}

	runID := uuid.NewString()
	result, err := loop.Run(ctx, agent.RunRequest{
		SessionKey: sessionKey,
		Message:    message,
		Media:      mediaFiles,
		Channel:    "mcp",
		RunID:      runID,
	})
	if err != nil {
		if ctx.Err() != nil {
			return &mcpbridge.ChatSendResult{Cancelled: true, SessionKey: sessionKey}, nil
		}
		return nil, fmt.Errorf("run agent %q: %w", agentID, err)
	}

	return &mcpbridge.ChatSendResult{
		RunID:      result.RunID,
		SessionKey: sessionKey,
		Content:    result.Content,
		Usage:      result.Usage,
		Thinking:   result.Thinking,
		Media:      result.Media,
	}, nil
}

// Abort cancels the run(s) matching runID and/or sessionKey, mirroring
// ChatMethods.handleAbort's aggregation logic (minus the caller-identity
// unauthorized-vs-notfound collapsing, which requires a WS client role).
func (r *agentChatRunner) Abort(_ context.Context, runID, sessionKey string) (*mcpbridge.ChatAbortResult, error) {
	var results []agent.AbortResult
	if runID != "" {
		results = []agent.AbortResult{r.agents.AbortRun(runID, sessionKey)}
	} else {
		results = r.agents.AbortRunsForSession(sessionKey)
	}

	var runIDs []string
	stopped, forced, alreadyAborting, notFound := 0, 0, 0, 0
	for _, res := range results {
		runIDs = append(runIDs, res.RunID)
		switch {
		case res.Stopped:
			stopped++
		case res.Forced:
			forced++
		case res.AlreadyAborting:
			alreadyAborting++
		case res.NotFound, res.Unauthorized:
			notFound++
		}
	}

	return &mcpbridge.ChatAbortResult{
		OK:              true,
		Aborted:         stopped+forced > 0,
		Stopped:         stopped > 0,
		Forced:          forced > 0,
		AlreadyAborting: alreadyAborting > 0,
		NotFound:        notFound > 0 && stopped+forced+alreadyAborting == 0,
		RunIDs:          runIDs,
	}, nil
}

// SessionStatus reports the running state and activity for a session.
func (r *agentChatRunner) SessionStatus(_ context.Context, sessionKey string) (*mcpbridge.ChatSessionStatusResult, error) {
	result := &mcpbridge.ChatSessionStatusResult{
		IsRunning: r.agents.IsSessionBusy(sessionKey),
	}
	if runID, ok := r.agents.SessionRunID(sessionKey); ok {
		result.RunID = runID
	}
	if status := r.agents.GetActivity(sessionKey); status != nil {
		result.Activity = &mcpbridge.ChatActivity{
			Phase:     status.Phase,
			Tool:      status.Tool,
			Iteration: status.Iteration,
		}
	}
	return result, nil
}
