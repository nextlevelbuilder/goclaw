package cmd

import (
	"context"
	"errors"
	"path/filepath"
	"testing"

	"github.com/google/uuid"

	"github.com/nextlevelbuilder/goclaw/internal/agent"
	"github.com/nextlevelbuilder/goclaw/internal/sandbox"
	"github.com/nextlevelbuilder/goclaw/internal/tools"
)

type delegationReleaseManager struct {
	released string
	err      error
}

func (m *delegationReleaseManager) Get(context.Context, string, string, *sandbox.Config, ...sandbox.GetOption) (sandbox.Sandbox, error) {
	return nil, errors.New("not implemented")
}
func (m *delegationReleaseManager) Release(_ context.Context, key string) error {
	m.released = key
	return m.err
}
func (*delegationReleaseManager) ReleaseAll(context.Context) error { return nil }
func (*delegationReleaseManager) Stop()                            {}
func (*delegationReleaseManager) Stats() map[string]any            { return nil }

func TestBuildAgentLinkRunRequestPreservesGroupAuthorizationScope(t *testing.T) {
	req := tools.DelegateRequest{
		FromAgentKey:        "coordinator",
		Task:                "create the report",
		DelegationID:        uuid.NewString(),
		UserID:              "group:telegram:-100123",
		SenderID:            "386246614",
		Role:                "viewer",
		Channel:             "telegram-main",
		ChannelType:         "telegram",
		ChatID:              "-100123",
		PeerKind:            "group",
		DelegateInputsPath:  filepath.Join(t.TempDir(), "inputs"),
		DelegateOutputsPath: filepath.Join(t.TempDir(), "outputs"),
	}

	got := buildAgentLinkRunRequest(req, "delegate:session")

	if got.UserID != req.UserID || got.SenderID != req.SenderID || got.Role != req.Role {
		t.Fatalf("authorization scope = (%q, %q, %q), want (%q, %q, %q)",
			got.UserID, got.SenderID, got.Role, req.UserID, req.SenderID, req.Role)
	}
	if got.Channel != "delegate" || got.ChannelType != req.ChannelType {
		t.Fatalf("channel = (%q, %q), want (delegate, %q)", got.Channel, got.ChannelType, req.ChannelType)
	}
	if got.ChatID != req.ChatID || got.PeerKind != req.PeerKind {
		t.Fatalf("chat scope = (%q, %q), want (%q, %q)",
			got.ChatID, got.PeerKind, req.ChatID, req.PeerKind)
	}
	if got.WorkspaceChannel != req.Channel || got.WorkspaceChatID != req.ChatID {
		t.Fatalf("workspace scope = (%q, %q), want (%q, %q)",
			got.WorkspaceChannel, got.WorkspaceChatID, req.Channel, req.ChatID)
	}
	if got.RunID == "" || got.RunKind != "delegate" || got.DelegationID != req.DelegationID {
		t.Fatalf("delegate classification = %#v", got)
	}
	if got.DelegateInputsPath != req.DelegateInputsPath ||
		got.DelegateOutputsPath != req.DelegateOutputsPath {
		t.Fatalf("artifact runtime wiring = (%q, %q), want (%q, %q)",
			got.DelegateInputsPath, got.DelegateOutputsPath,
			req.DelegateInputsPath, req.DelegateOutputsPath)
	}
	if got.Media != nil {
		t.Fatalf("run media = %#v, want exchange-only input delivery", got.Media)
	}
}

func TestAgentMediaToBusFilesDiscardsEphemeralDelegateMedia(t *testing.T) {
	got := agentMediaToBusFiles([]agent.MediaResult{{
		Path:        filepath.Join(t.TempDir(), "ephemeral.png"),
		ContentType: "image/png",
	}})
	if got != nil {
		t.Fatalf("media = %#v, want raw delegate media discarded", got)
	}
}

func TestReleaseDelegationSandboxUsesExactSessionKey(t *testing.T) {
	manager := &delegationReleaseManager{}
	const sessionKey = "delegate:from:target:4fe8220f-07f1-4e64-a95c-b49ebc39db4a"
	if err := releaseDelegationSandbox(context.Background(), manager, sessionKey); err != nil {
		t.Fatalf("releaseDelegationSandbox: %v", err)
	}
	if manager.released != sessionKey {
		t.Fatalf("released key = %q, want %q", manager.released, sessionKey)
	}
}

func TestReleaseDelegationSandboxRedactsManagerFailure(t *testing.T) {
	manager := &delegationReleaseManager{err: errors.New("/private/exchange/inputs remained mounted")}
	err := releaseDelegationSandbox(context.Background(), manager, "delegate:session")
	if err == nil {
		t.Fatal("releaseDelegationSandbox unexpectedly succeeded")
	}
	if got := err.Error(); got != "delegation sandbox release failed" {
		t.Fatalf("error = %q, want redacted release failure", got)
	}
}
