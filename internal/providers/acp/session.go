package acp

import (
	"context"
	"fmt"
	"time"
)

// Initialize sends the ACP initialize request to establish capabilities.
func (p *ACPProcess) Initialize(ctx context.Context) error {
	req := InitializeRequest{
		ClientInfo: ClientInfo{Name: "goclaw", Version: "1.0"},
		Capabilities: ClientCaps{
			Fs:       &FsCaps{ReadTextFile: true, WriteTextFile: true},
			Terminal: &TerminalCaps{Enabled: true},
		},
	}
	var resp InitializeResponse
	if err := p.conn.Call(ctx, "initialize", req, &resp); err != nil {
		return fmt.Errorf("acp initialize: %w", err)
	}
	p.agentCaps = resp.Capabilities
	return nil
}

// NewSession creates a new ACP session on this process.
func (p *ACPProcess) NewSession(ctx context.Context) error {
	var resp NewSessionResponse
	if err := p.conn.Call(ctx, "session/new", NewSessionRequest{}, &resp); err != nil {
		return fmt.Errorf("acp session/new: %w", err)
	}
	p.sessionID = resp.SessionID
	return nil
}

// Prompt sends user content and blocks until the agent responds.
// onUpdate is called for each session/update notification (streaming).
func (p *ACPProcess) Prompt(ctx context.Context, content []ContentBlock, onUpdate func(SessionUpdate)) (*PromptResponse, error) {
	p.inUse.Add(1)
	defer p.inUse.Add(-1)

	p.mu.Lock()
	p.lastActive = time.Now()
	p.mu.Unlock()

	// Install the update callback for this prompt
	p.setUpdateFn(onUpdate)
	defer p.setUpdateFn(nil)

	req := PromptRequest{
		SessionID: p.sessionID,
		Content:   content,
	}
	var resp PromptResponse
	if err := p.conn.Call(ctx, "session/prompt", req, &resp); err != nil {
		return nil, fmt.Errorf("acp session/prompt: %w", err)
	}

	p.mu.Lock()
	p.lastActive = time.Now()
	p.mu.Unlock()

	return &resp, nil
}

// Cancel sends a session/cancel notification for cooperative cancellation.
func (p *ACPProcess) Cancel() error {
	return p.conn.Notify("session/cancel", CancelNotification{
		SessionID: p.sessionID,
	})
}
