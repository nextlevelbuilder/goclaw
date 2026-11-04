package agent

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/nextlevelbuilder/goclaw/internal/providers"
	"github.com/nextlevelbuilder/goclaw/internal/secretvault"
	"github.com/nextlevelbuilder/goclaw/internal/store"
	"github.com/nextlevelbuilder/goclaw/internal/tools"
	"github.com/nextlevelbuilder/goclaw/internal/tracing"
)

const vaultTestSecret = "ghp_SuperSecretToken_123"

type vaultFake struct {
	store.SecretVaultStore
	secrets []store.SecretVaultResolved
	err     error
}

func (f *vaultFake) ListGrantedSecrets(context.Context, uuid.UUID) ([]store.SecretVaultResolved, error) {
	return f.secrets, f.err
}

func vaultLoop(f store.SecretVaultStore) *Loop {
	return &Loop{id: "agent-x", agentUUID: uuid.New(), secretVault: f}
}

func tenantContext() context.Context {
	return store.WithTenantID(context.Background(), uuid.New())
}

func grantedFake() *vaultFake {
	return &vaultFake{secrets: []store.SecretVaultResolved{{
		Entry:   store.SecretVaultEntry{Name: "gh", Kind: store.SecretKindAPIKey},
		Payload: store.SecretVaultPayload{Secret: vaultTestSecret},
	}}}
}

func TestWithSecretVault_InstallsRuntimeAndRedactor(t *testing.T) {
	l := vaultLoop(grantedFake())
	req := &RunRequest{UserID: "u1"}
	ctx := l.withSecretVault(tenantContext(), req)

	if secretvault.RuntimeFrom(ctx) == nil {
		t.Fatal("runtime missing")
	}
	if req.SecretRedactor == nil {
		t.Fatal("request redactor missing")
	}
	if got := req.SecretRedactor("x " + vaultTestSecret + " y"); strings.Contains(got, vaultTestSecret) {
		t.Fatalf("secret not redacted: %q", got)
	}
}

func TestWithSecretVault_NoRuntimeWhenUnavailable(t *testing.T) {
	parent := secretvault.WithRuntime(tenantContext(), secretvault.NewRuntime(grantedFake(), uuid.New(), "parent"))
	cases := map[string]*Loop{
		"no store":    vaultLoop(nil),
		"no agent":    {id: "a", secretVault: grantedFake()},
		"preload err": vaultLoop(&vaultFake{err: errors.New("db down")}),
	}
	for name, l := range cases {
		req := &RunRequest{}
		ctx := l.withSecretVault(parent, req)
		if secretvault.RuntimeFrom(ctx) != nil {
			t.Errorf("%s: parent's runtime must be cleared, not inherited", name)
		}
		if req.SecretRedactor != nil {
			t.Errorf("%s: redactor must not be set", name)
		}
	}
	// No tenant in ctx: vault stays off.
	req := &RunRequest{}
	if secretvault.RuntimeFrom(vaultLoop(grantedFake()).withSecretVault(context.Background(), req)) != nil {
		t.Error("no tenant: runtime must be nil")
	}
}

func TestRedactToolResult_AllTextFields(t *testing.T) {
	ctx := tracing.WithTextRedactor(context.Background(), func(s string) string {
		return strings.ReplaceAll(s, vaultTestSecret, "[R]")
	})
	res := &tools.Result{
		ForLLM:      "for llm " + vaultTestSecret,
		ForUser:     "for user " + vaultTestSecret,
		Deliverable: "deliverable " + vaultTestSecret,
		Err:         errors.New("boom " + vaultTestSecret),
	}
	got := redactToolResult(ctx, res)
	for name, v := range map[string]string{"ForLLM": got.ForLLM, "ForUser": got.ForUser, "Deliverable": got.Deliverable, "Err": got.Err.Error()} {
		if strings.Contains(v, vaultTestSecret) {
			t.Errorf("%s leaks the secret: %q", name, v)
		}
	}
	if redactToolResult(ctx, nil) != nil {
		t.Error("nil result must stay nil")
	}
}

// echoTool returns its Authorization argument, as a vault-aware tool would if
// it mishandled a response that reflects the credential.
type echoTool struct{}

func (echoTool) Name() string               { return "echo_secret" }
func (echoTool) Description() string        { return "echo" }
func (echoTool) Parameters() map[string]any { return map[string]any{"type": "object"} }
func (echoTool) Execute(_ context.Context, args map[string]any) *tools.Result {
	v, _ := args["v"].(string)
	return &tools.Result{ForLLM: "echo:" + v, ForUser: "user:" + v, Deliverable: "d:" + v}
}

func TestExecuteToolForActor_RedactsBeforeAnySink(t *testing.T) {
	reg := tools.NewRegistry()
	reg.Register(echoTool{})
	l := vaultLoop(grantedFake())
	l.tools = reg

	req := &RunRequest{}
	ctx := l.withSecretVault(tenantContext(), req)
	ctx = withDelegationArtifactTextRedactor(ctx, req)

	res := l.executeToolForActor(ctx, "echo_secret", map[string]any{"v": vaultTestSecret}, "ch", "chat", "direct", "sk", "")
	for name, v := range map[string]string{"ForLLM": res.ForLLM, "ForUser": res.ForUser, "Deliverable": res.Deliverable} {
		if strings.Contains(v, vaultTestSecret) {
			t.Errorf("%s leaks: %q", name, v)
		}
	}
	if !strings.Contains(res.ForLLM, secretvault.Mask) {
		t.Errorf("expected mask in %q", res.ForLLM)
	}
}

func TestRunSinks_UseVaultRedactorWithoutDroppingMedia(t *testing.T) {
	l := vaultLoop(grantedFake())
	req := &RunRequest{} // ordinary run, not a delegate
	_ = l.withSecretVault(tenantContext(), req)

	// Event payload (tool.call / tool.result arguments and result).
	ev := redactDelegationAgentEvent(req, AgentEvent{Payload: map[string]any{
		"arguments": map[string]any{"h": "Bearer " + vaultTestSecret},
		"result":    "got " + vaultTestSecret,
	}})
	if s := ev.Payload.(map[string]any)["result"].(string); strings.Contains(s, vaultTestSecret) {
		t.Errorf("event result leaks: %q", s)
	}
	if s := ev.Payload.(map[string]any)["arguments"].(map[string]any)["h"].(string); strings.Contains(s, vaultTestSecret) {
		t.Errorf("event arguments leak: %q", s)
	}

	// Session message: content, tool-call args; media refs survive.
	msg := redactDelegationMessage(req, providers.Message{
		Role:      "assistant",
		Content:   "token " + vaultTestSecret,
		ToolCalls: []providers.ToolCall{{Arguments: map[string]any{"v": vaultTestSecret}}},
		MediaRefs: []providers.MediaRef{{ID: "m1"}},
	})
	if strings.Contains(msg.Content, vaultTestSecret) || strings.Contains(msg.ToolCalls[0].Arguments["v"].(string), vaultTestSecret) {
		t.Errorf("message leaks: %+v", msg)
	}
	if len(msg.MediaRefs) != 1 {
		t.Error("vault redaction must not drop media refs of a normal run")
	}

	// Run result: content, thinking, deliverables; media survives.
	res := redactDelegationRunResult(req, &RunResult{
		Content:      "a " + vaultTestSecret,
		Thinking:     "b " + vaultTestSecret,
		Deliverables: []string{"c " + vaultTestSecret},
		Media:        []MediaResult{{Path: "/w/x.png"}},
	})
	if strings.Contains(res.Content, vaultTestSecret) || strings.Contains(res.Thinking, vaultTestSecret) || strings.Contains(res.Deliverables[0], vaultTestSecret) {
		t.Errorf("result leaks: %+v", res)
	}
	if len(res.Media) != 1 {
		t.Error("vault redaction must not drop media of a normal run")
	}
}
