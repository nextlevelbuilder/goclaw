package cmd

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/google/uuid"

	"github.com/nextlevelbuilder/goclaw/internal/agent"
	"github.com/nextlevelbuilder/goclaw/internal/bus"
	"github.com/nextlevelbuilder/goclaw/internal/channels"
	"github.com/nextlevelbuilder/goclaw/internal/providers"
	"github.com/nextlevelbuilder/goclaw/internal/store"
)

func TestBuildDeliveryRuntimeUsesResolvedDeliveryProviderModels(t *testing.T) {
	tenantID := uuid.New()
	ctx := store.WithTenantID(context.Background(), tenantID)
	reg := providers.NewRegistry(store.TenantIDFromContext)
	reg.RegisterForTenant(tenantID, deliveryRuntimeTestProvider{name: "channel-provider", model: "channel-default"})
	reg.RegisterForTenant(tenantID, deliveryRuntimeTestProvider{name: "progress-provider", model: "progress-default"})

	runtime := buildDeliveryRuntime(ctx, &ConsumerDeps{ProviderReg: reg}, deliveryRuntimeTestAgent{
		id:           "agent",
		uuid:         uuid.New(),
		model:        "agent-model",
		providerName: "agent-provider",
		provider:     deliveryRuntimeTestProvider{name: "agent-provider", model: "agent-default"},
	}, channels.ResolvedChatBehavior{
		Enabled: true,
		QuickAck: channels.ResolvedQuickAckConfig{
			Enabled:  true,
			Mode:     channels.QuickAckModeSidecar,
			Provider: "channel-provider",
			Model:    "channel-model",
		},
		IntermediateReplies: channels.ResolvedIntermediateRepliesConfig{
			Enabled:  true,
			Mode:     channels.IntermediateModeSidecar,
			Provider: "progress-provider",
			Model:    "progress-model",
		},
	}, bus.InboundMessage{
		TenantID: tenantID,
		Content:  "kiểm tra giúp tôi",
		Metadata: map[string]string{"locale": "vi"},
	}, "user-1", "dm", "telegram", "agent")

	quick, ok := runtime.QuickAckGenerator.(channels.ProviderDeliveryMessageGenerator)
	if !ok {
		t.Fatalf("QuickAckGenerator = %T, want ProviderDeliveryMessageGenerator", runtime.QuickAckGenerator)
	}
	if quick.ProviderName != "channel-provider" || quick.Model != "channel-model" {
		t.Fatalf("quick provider/model = %q/%q, want channel-provider/channel-model", quick.ProviderName, quick.Model)
	}
	progress, ok := runtime.ProgressGenerator.(channels.ProviderDeliveryMessageGenerator)
	if !ok {
		t.Fatalf("ProgressGenerator = %T, want ProviderDeliveryMessageGenerator", runtime.ProgressGenerator)
	}
	if progress.ProviderName != "progress-provider" || progress.Model != "progress-model" {
		t.Fatalf("progress provider/model = %q/%q, want progress-provider/progress-model", progress.ProviderName, progress.Model)
	}
}

func TestBuildDeliveryRuntimeFallsBackToAgentProviderModelWhenUnset(t *testing.T) {
	tenantID := uuid.New()
	agentProvider := deliveryRuntimeTestProvider{name: "agent-provider", model: "agent-default"}
	runtime := buildDeliveryRuntime(context.Background(), &ConsumerDeps{}, deliveryRuntimeTestAgent{
		id:           "agent",
		uuid:         uuid.New(),
		model:        "agent-model",
		providerName: "agent-provider",
		provider:     agentProvider,
	}, channels.ResolvedChatBehavior{
		Enabled: true,
		QuickAck: channels.ResolvedQuickAckConfig{
			Enabled: true,
			Mode:    channels.QuickAckModeSidecar,
		},
		IntermediateReplies: channels.ResolvedIntermediateRepliesConfig{
			Enabled: true,
			Mode:    channels.IntermediateModeSidecar,
		},
	}, bus.InboundMessage{TenantID: tenantID}, "user-1", "dm", "telegram", "agent")

	quick, ok := runtime.QuickAckGenerator.(channels.ProviderDeliveryMessageGenerator)
	if !ok {
		t.Fatalf("QuickAckGenerator = %T, want ProviderDeliveryMessageGenerator", runtime.QuickAckGenerator)
	}
	if quick.ProviderName != "agent-provider" || quick.Model != "agent-model" {
		t.Fatalf("quick provider/model = %q/%q, want agent-provider/agent-model", quick.ProviderName, quick.Model)
	}
	progress, ok := runtime.ProgressGenerator.(channels.ProviderDeliveryMessageGenerator)
	if !ok {
		t.Fatalf("ProgressGenerator = %T, want ProviderDeliveryMessageGenerator", runtime.ProgressGenerator)
	}
	if progress.ProviderName != "agent-provider" || progress.Model != "agent-model" {
		t.Fatalf("progress provider/model = %q/%q, want agent-provider/agent-model", progress.ProviderName, progress.Model)
	}
}

func TestBuildDeliveryRuntimeIncludesAgentPersonaBrief(t *testing.T) {
	agent := deliveryRuntimePersonaTestAgent{
		deliveryRuntimeTestAgent: deliveryRuntimeTestAgent{
			id:           "agent",
			uuid:         uuid.New(),
			model:        "agent-model",
			providerName: "agent-provider",
			provider:     deliveryRuntimeTestProvider{name: "agent-provider", model: "agent-default"},
		},
		personaByUserID: map[string]string{
			"raw-user":      "wrong persona",
			"resolved-user": "Style: concise, warm",
		},
	}
	runtime := buildDeliveryRuntime(context.Background(), &ConsumerDeps{}, deliveryRuntimePersonaTestAgent{
		deliveryRuntimeTestAgent: agent.deliveryRuntimeTestAgent,
		personaByUserID:          agent.personaByUserID,
	}, channels.ResolvedChatBehavior{
		Enabled: true,
		QuickAck: channels.ResolvedQuickAckConfig{
			Enabled: true,
			Mode:    channels.QuickAckModeSidecar,
		},
		IntermediateReplies: channels.ResolvedIntermediateRepliesConfig{
			Enabled: true,
			Mode:    channels.IntermediateModeSidecar,
		},
	}, bus.InboundMessage{UserID: "raw-user"}, "resolved-user", "dm", "telegram", "agent")

	if runtime.PersonaBrief != "Style: concise, warm" {
		t.Fatalf("runtime persona brief = %q, want agent persona", runtime.PersonaBrief)
	}
}

type deliveryRuntimeTestAgent struct {
	id           string
	uuid         uuid.UUID
	otherConfig  json.RawMessage
	model        string
	providerName string
	provider     providers.Provider
}

func (a deliveryRuntimeTestAgent) ID() string                   { return a.id }
func (a deliveryRuntimeTestAgent) UUID() uuid.UUID              { return a.uuid }
func (a deliveryRuntimeTestAgent) OtherConfig() json.RawMessage { return a.otherConfig }
func (a deliveryRuntimeTestAgent) Run(context.Context, agent.RunRequest) (*agent.RunResult, error) {
	return nil, nil
}
func (a deliveryRuntimeTestAgent) IsRunning() bool              { return true }
func (a deliveryRuntimeTestAgent) Model() string                { return a.model }
func (a deliveryRuntimeTestAgent) ProviderName() string         { return a.providerName }
func (a deliveryRuntimeTestAgent) Provider() providers.Provider { return a.provider }

type deliveryRuntimePersonaTestAgent struct {
	deliveryRuntimeTestAgent
	personaByUserID map[string]string
}

func (a deliveryRuntimePersonaTestAgent) DeliveryPersonaBrief(_ context.Context, userID string) string {
	return a.personaByUserID[userID]
}

type deliveryRuntimeTestProvider struct {
	name  string
	model string
}

func (p deliveryRuntimeTestProvider) Chat(context.Context, providers.ChatRequest) (*providers.ChatResponse, error) {
	return &providers.ChatResponse{}, nil
}

func (p deliveryRuntimeTestProvider) ChatStream(context.Context, providers.ChatRequest, func(providers.StreamChunk)) (*providers.ChatResponse, error) {
	return &providers.ChatResponse{}, nil
}

func (p deliveryRuntimeTestProvider) DefaultModel() string { return p.model }
func (p deliveryRuntimeTestProvider) Name() string         { return p.name }
