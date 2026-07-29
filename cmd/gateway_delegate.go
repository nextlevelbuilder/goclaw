package cmd

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"

	"github.com/nextlevelbuilder/goclaw/internal/agent"
	"github.com/nextlevelbuilder/goclaw/internal/bus"
	"github.com/nextlevelbuilder/goclaw/internal/sandbox"
	"github.com/nextlevelbuilder/goclaw/internal/tools"
)

// buildAgentLinkRunRequest preserves the origin's authorization-bearing
// identity while keeping delegation on its internal delivery channel.
func buildAgentLinkRunRequest(req tools.DelegateRequest, sessionKey string) agent.RunRequest {
	return agent.RunRequest{
		RunID:               uuid.New().String(),
		SessionKey:          sessionKey,
		Message:             req.Task,
		UserID:              req.UserID,
		SenderID:            req.SenderID,
		Role:                req.Role,
		Channel:             "delegate",
		ChannelType:         req.ChannelType,
		ChatID:              req.ChatID,
		PeerKind:            req.PeerKind,
		RunKind:             "delegate",
		DelegationID:        req.DelegationID,
		ParentAgentID:       req.FromAgentKey,
		WorkspaceChannel:    req.Channel,
		WorkspaceChatID:     req.ChatID,
		DelegateInputsPath:  req.DelegateInputsPath,
		DelegateOutputsPath: req.DelegateOutputsPath,
	}
}

// Agent Link artifacts are published from the exchange manifest. Raw delegate
// media paths point into B's ephemeral workspace and must never cross back to A.
func agentMediaToBusFiles(_ []agent.MediaResult) []bus.MediaFile { return nil }

func releaseDelegationSandbox(ctx context.Context, manager sandbox.Manager, sessionKey string) error {
	if manager == nil {
		return nil
	}
	cleanupCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 10*time.Second)
	defer cancel()
	if err := manager.Release(cleanupCtx, sessionKey); err != nil {
		return fmt.Errorf("delegation sandbox release failed")
	}
	return nil
}
