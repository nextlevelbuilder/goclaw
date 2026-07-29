package agent

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/nextlevelbuilder/goclaw/internal/providers"
	"github.com/nextlevelbuilder/goclaw/internal/store"
	"github.com/nextlevelbuilder/goclaw/internal/tools"
	"github.com/nextlevelbuilder/goclaw/internal/workspace"
)

func newArtifactRunRequest(t *testing.T, root string) *RunRequest {
	t.Helper()
	delegationID := uuid.NewString()
	exchange := filepath.Join(root, "collaboration", "delegations", delegationID)
	inputs := filepath.Join(exchange, "inputs")
	outputs := filepath.Join(exchange, "outputs")
	if err := os.MkdirAll(inputs, 0750); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(outputs, 0750); err != nil {
		t.Fatal(err)
	}
	return &RunRequest{
		SessionKey:          "agent:worker:delegate:test",
		RunKind:             "delegate",
		DelegationID:        delegationID,
		DelegateInputsPath:  inputs,
		DelegateOutputsPath: outputs,
	}
}

func newArtifactTestLoop(root string) *Loop {
	return NewLoop(LoopConfig{
		ID:        "worker",
		AgentUUID: uuid.New(),
		TenantID:  store.MasterTenantID,
		Workspace: filepath.Join(root, "agents", "worker"),
		DataDir:   root,
		Sessions:  &nopSessionStore{},
	})
}

func TestInjectContext_UsesOnlyDelegationArtifactWorkspace(t *testing.T) {
	root := t.TempDir()
	req := newArtifactRunRequest(t, root)
	parentCtx := tools.WithToolTeamWorkspace(context.Background(), filepath.Join(root, "teams", "stale"))
	parentCtx = tools.WithToolTeamID(parentCtx, uuid.NewString())
	parentCtx = tools.WithTenantAllowedPaths(parentCtx, []string{filepath.Join(root, "tenant-allowed")})
	parentCtx = tools.WithDelegationArtifactInputs(parentCtx, filepath.Join(root, "stale-inputs"))
	parentCtx = tools.WithRunMediaPaths(parentCtx, []string{"/stale/upload.txt"})

	setup, err := newArtifactTestLoop(root).injectContext(parentCtx, req)
	if err != nil {
		t.Fatalf("injectContext: %v", err)
	}
	if got := tools.ToolWorkspaceFromCtx(setup.ctx); got != req.DelegateOutputsPath {
		t.Fatalf("workspace = %q, want outputs %q", got, req.DelegateOutputsPath)
	}
	if got := tools.DelegationArtifactInputsFromCtx(setup.ctx); got != req.DelegateInputsPath {
		t.Fatalf("inputs = %q, want %q", got, req.DelegateInputsPath)
	}
	if got := tools.ToolTeamWorkspaceFromCtx(setup.ctx); got != "" {
		t.Fatalf("parent Team workspace survived: %q", got)
	}
	if got := tools.ToolTeamIDFromCtx(setup.ctx); got != "" {
		t.Fatalf("parent Team ID survived: %q", got)
	}
	if got := tools.TenantAllowedPathsFromCtx(setup.ctx); len(got) != 0 {
		t.Fatalf("parent tenant allowed paths survived: %v", got)
	}
	if got := tools.RunMediaPathsFromCtx(setup.ctx); len(got) != 0 {
		t.Fatalf("parent media paths survived: %v", got)
	}
	wc := workspace.FromContext(setup.ctx)
	if wc == nil || wc.Scope != workspace.ScopeDelegate || wc.ActivePath != req.DelegateOutputsPath {
		t.Fatalf("workspace context = %#v", wc)
	}
}

func TestInjectContext_DelegationSetupFailsClosed(t *testing.T) {
	root := t.TempDir()
	req := newArtifactRunRequest(t, root)
	req.DelegateOutputsPath = filepath.Join(root, "missing")
	if _, err := newArtifactTestLoop(root).injectContext(context.Background(), req); err == nil {
		t.Fatal("missing artifact workspace fell back to personal workspace")
	}

	outside := filepath.Join(root, "outside")
	if err := os.MkdirAll(outside, 0750); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(root, "output-link")
	if err := os.Symlink(outside, link); err != nil {
		t.Fatal(err)
	}
	req.DelegateOutputsPath = link
	if _, err := newArtifactTestLoop(root).injectContext(context.Background(), req); err == nil {
		t.Fatal("symlink artifact workspace fell back to personal workspace")
	}

	req = newArtifactRunRequest(t, root)
	req.DelegationID = uuid.NewString()
	if _, err := newArtifactTestLoop(root).injectContext(context.Background(), req); err == nil {
		t.Fatal("exchange path not bound to delegation ID was accepted")
	}
}

func TestInjectContext_DelegatePreservesAuthorizationScope(t *testing.T) {
	root := t.TempDir()
	req := newArtifactRunRequest(t, root)
	req.UserID = "group:telegram:-100123"
	req.SenderID = "386246614"
	req.Role = "viewer"
	req.Channel = "delegate"
	req.ChannelType = "telegram"
	req.ChatID = "-100123"
	req.PeerKind = "group"
	req.WorkspaceChannel = "telegram-main"
	req.WorkspaceChatID = "-100123"

	setup, err := newArtifactTestLoop(root).injectContext(context.Background(), req)
	if err != nil {
		t.Fatalf("injectContext: %v", err)
	}
	if got := store.UserIDFromContext(setup.ctx); got != req.UserID {
		t.Fatalf("UserID = %q, want %q", got, req.UserID)
	}
	if got := store.SenderIDFromContext(setup.ctx); got != req.SenderID {
		t.Fatalf("SenderID = %q, want %q", got, req.SenderID)
	}
	if got := store.RoleFromContext(setup.ctx); got != req.Role {
		t.Fatalf("Role = %q, want %q", got, req.Role)
	}
}

func TestInjectContext_TeamDispatchKeepsSharedWorkspaceContract(t *testing.T) {
	root := t.TempDir()
	teamWorkspace := filepath.Join(root, "teams", "team-a", "shared")
	if err := os.MkdirAll(teamWorkspace, 0750); err != nil {
		t.Fatal(err)
	}
	req := &RunRequest{
		SessionKey:    "agent:worker:team:test",
		TeamWorkspace: teamWorkspace,
		TeamID:        uuid.NewString(),
		TeamTaskID:    uuid.NewString(),
		LeaderAgentID: uuid.NewString(),
	}

	setup, err := newArtifactTestLoop(root).injectContext(context.Background(), req)
	if err != nil {
		t.Fatalf("injectContext: %v", err)
	}
	if got := tools.ToolWorkspaceFromCtx(setup.ctx); got != teamWorkspace {
		t.Fatalf("active workspace = %q, want Team workspace %q", got, teamWorkspace)
	}
	if got := tools.ToolTeamWorkspaceFromCtx(setup.ctx); got != teamWorkspace {
		t.Fatalf("Team workspace = %q, want %q", got, teamWorkspace)
	}
	if got := tools.ToolTeamIDFromCtx(setup.ctx); got != req.TeamID {
		t.Fatalf("Team ID = %q, want %q", got, req.TeamID)
	}
	if got := tools.TeamTaskIDFromCtx(setup.ctx); got != req.TeamTaskID {
		t.Fatalf("Team task ID = %q, want %q", got, req.TeamTaskID)
	}
	if got := tools.DelegationArtifactInputsFromCtx(setup.ctx); got != "" {
		t.Fatalf("Team dispatch gained artifact inputs: %q", got)
	}
}

func TestDelegationArtifactRedactionCoversEventsSessionsAndResults(t *testing.T) {
	req := newArtifactRunRequest(t, t.TempDir())
	hostResult := filepath.Join(req.DelegateOutputsPath, "report.txt")
	hostInput := filepath.Join(req.DelegateInputsPath, "source.txt")

	event := redactDelegationAgentEvent(req, AgentEvent{
		Payload: map[string]any{
			"content": "created " + hostResult,
			"media":   []any{hostResult},
		},
	})
	message := redactDelegationMessage(req, providers.Message{
		Content:   "read " + hostInput,
		Thinking:  "write " + hostResult,
		MediaRefs: []providers.MediaRef{{Path: hostResult}},
		ToolCalls: []providers.ToolCall{{
			Arguments: map[string]any{"path": hostResult},
		}},
		RawAssistantContent: json.RawMessage(`{"path":` + mustJSONQuote(t, hostResult) + `}`),
	})
	result := redactDelegationRunResult(req, &RunResult{
		Content:      "done at " + hostResult,
		Thinking:     hostInput,
		Deliverables: []string{hostResult},
		Media:        []MediaResult{{Path: hostResult}},
	})

	encoded, err := json.Marshal([]any{event, message, result})
	if err != nil {
		t.Fatal(err)
	}
	for _, hostRoot := range []string{
		filepath.Dir(req.DelegateInputsPath),
		req.DelegateInputsPath,
		req.DelegateOutputsPath,
	} {
		if strings.Contains(string(encoded), hostRoot) {
			t.Fatalf("redacted boundary leaked %q: %s", hostRoot, encoded)
		}
	}
	if len(message.MediaRefs) != 0 || len(result.Media) != 0 {
		t.Fatalf("ephemeral media survived: message=%#v result=%#v", message.MediaRefs, result.Media)
	}
	if !strings.Contains(string(encoded), "inputs/source.txt") ||
		!strings.Contains(string(encoded), "outputs/report.txt") {
		t.Fatalf("logical aliases missing: %s", encoded)
	}
}

func mustJSONQuote(t *testing.T, value string) string {
	t.Helper()
	encoded, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	return string(encoded)
}
