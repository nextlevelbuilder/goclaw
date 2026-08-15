package methods

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/google/uuid"

	"github.com/nextlevelbuilder/goclaw/internal/gateway"
	"github.com/nextlevelbuilder/goclaw/internal/permissions"
	"github.com/nextlevelbuilder/goclaw/internal/providers"
	"github.com/nextlevelbuilder/goclaw/internal/store"
	"github.com/nextlevelbuilder/goclaw/pkg/protocol"
)

type stubLLMProvider struct {
	name  string
	model string
	req   providers.ChatRequest
}

func (p *stubLLMProvider) Chat(_ context.Context, req providers.ChatRequest) (*providers.ChatResponse, error) {
	p.req = req
	return &providers.ChatResponse{Content: `{"ok":true}`, Usage: &providers.Usage{PromptTokens: 1, CompletionTokens: 2, TotalTokens: 3}}, nil
}
func (p *stubLLMProvider) ChatStream(context.Context, providers.ChatRequest, func(providers.StreamChunk)) (*providers.ChatResponse, error) {
	return nil, nil
}
func (p *stubLLMProvider) DefaultModel() string { return p.model }
func (p *stubLLMProvider) Name() string         { return p.name }

func TestLLMCompleteUsesProviderRegistry(t *testing.T) {
	tid := uuid.New()
	prov := &stubLLMProvider{name: "local", model: "default-model"}
	reg := providers.NewRegistry(store.TenantIDFromContext)
	reg.RegisterForTenant(tid, prov)

	m := NewLLMMethods(reg, "local", "")
	client, out := gateway.NewCapturingTestClient(permissions.RoleOperator, tid, "user-1", 1)
	params := map[string]any{
		"messages": []map[string]string{
			{"role": "system", "content": "summarize"},
			{"role": "user", "content": "hello"},
		},
		"maxTokens": 123,
	}
	raw, _ := json.Marshal(params)
	ctx := store.WithTenantID(t.Context(), tid)
	m.handleComplete(ctx, client, &protocol.RequestFrame{ID: "r1", Params: raw})

	var frame protocol.ResponseFrame
	if err := json.Unmarshal(<-out, &frame); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if frame.Error != nil {
		t.Fatalf("unexpected error: %+v", frame.Error)
	}
	result, ok := frame.Payload.(map[string]any)
	if !ok {
		t.Fatalf("payload type = %T", frame.Payload)
	}
	if result["content"] != `{"ok":true}` {
		t.Fatalf("content = %v", result["content"])
	}
	if prov.req.Model != "default-model" {
		t.Fatalf("model = %q", prov.req.Model)
	}
	if got := prov.req.Options[providers.OptMaxTokens]; got != float64(123) && got != 123 {
		t.Fatalf("max tokens option = %#v", got)
	}
}

func TestLLMCompleteRequiresOperator(t *testing.T) {
	reg := providers.NewRegistry(store.TenantIDFromContext)
	m := NewLLMMethods(reg, "", "")
	client, out := gateway.NewCapturingTestClient(permissions.RoleViewer, uuid.New(), "user-1", 1)
	raw := json.RawMessage(`{"messages":[{"role":"user","content":"hi"}]}`)
	m.handleComplete(t.Context(), client, &protocol.RequestFrame{ID: "r1", Params: raw})

	var frame protocol.ResponseFrame
	if err := json.Unmarshal(<-out, &frame); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if frame.Error == nil {
		t.Fatal("expected authorization error")
	}
}
