package methods

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/google/uuid"

	"github.com/nextlevelbuilder/goclaw/internal/gateway"
	"github.com/nextlevelbuilder/goclaw/internal/i18n"
	"github.com/nextlevelbuilder/goclaw/internal/permissions"
	"github.com/nextlevelbuilder/goclaw/internal/providers"
	"github.com/nextlevelbuilder/goclaw/internal/store"
	"github.com/nextlevelbuilder/goclaw/pkg/protocol"
)

// LLMMethods exposes small provider-backed completion helpers for trusted
// operational scripts. It bypasses the agent loop so scripts can use the
// gateway's configured provider registry without writing provider-specific API
// code or storing provider keys in cron payloads.
type LLMMethods struct {
	providers *providers.Registry
	cfg       llmDefaults
}

type llmDefaults struct {
	Provider string
	Model    string
}

func NewLLMMethods(providerReg *providers.Registry, defaultProvider, defaultModel string) *LLMMethods {
	return &LLMMethods{
		providers: providerReg,
		cfg: llmDefaults{
			Provider: defaultProvider,
			Model:    defaultModel,
		},
	}
}

func (m *LLMMethods) Register(router *gateway.MethodRouter) {
	router.Register(protocol.MethodLLMComplete, m.handleComplete)
}

type llmCompleteParams struct {
	Provider    string              `json:"provider,omitempty"`
	Model       string              `json:"model,omitempty"`
	Messages    []providers.Message `json:"messages"`
	Temperature *float64            `json:"temperature,omitempty"`
	MaxTokens   int                 `json:"maxTokens,omitempty"`
}

func (m *LLMMethods) handleComplete(ctx context.Context, client *gateway.Client, req *protocol.RequestFrame) {
	locale := store.LocaleFromContext(ctx)
	if !permissions.HasMinRole(client.Role(), permissions.RoleOperator) {
		client.SendResponse(protocol.NewErrorResponse(req.ID, protocol.ErrUnauthorized, i18n.T(locale, i18n.MsgPermissionDenied, protocol.MethodLLMComplete)))
		return
	}
	if m.providers == nil {
		client.SendResponse(protocol.NewErrorResponse(req.ID, protocol.ErrInternal, "no providers configured"))
		return
	}

	var params llmCompleteParams
	if err := json.Unmarshal(req.Params, &params); err != nil {
		client.SendResponse(protocol.NewErrorResponse(req.ID, protocol.ErrInvalidRequest, i18n.T(locale, i18n.MsgInvalidJSON)))
		return
	}
	if len(params.Messages) == 0 {
		client.SendResponse(protocol.NewErrorResponse(req.ID, protocol.ErrInvalidRequest, i18n.T(locale, i18n.MsgMsgsRequired)))
		return
	}
	for i, msg := range params.Messages {
		role := strings.TrimSpace(msg.Role)
		if role == "" {
			client.SendResponse(protocol.NewErrorResponse(req.ID, protocol.ErrInvalidRequest, fmt.Sprintf("messages[%d].role is required", i)))
			return
		}
		if strings.TrimSpace(msg.Content) == "" {
			client.SendResponse(protocol.NewErrorResponse(req.ID, protocol.ErrInvalidRequest, fmt.Sprintf("messages[%d].content is required", i)))
			return
		}
	}

	providerName := strings.TrimSpace(params.Provider)
	if providerName == "" {
		providerName = strings.TrimSpace(m.cfg.Provider)
	}
	prov, model, err := m.resolveProvider(ctx, providerName, strings.TrimSpace(params.Model))
	if err != nil {
		client.SendResponse(protocol.NewErrorResponse(req.ID, protocol.ErrInvalidRequest, err.Error()))
		return
	}

	options := map[string]any{}
	if params.MaxTokens > 0 {
		options[providers.OptMaxTokens] = params.MaxTokens
	}
	if params.Temperature != nil {
		options[providers.OptTemperature] = *params.Temperature
	}

	resp, err := prov.Chat(ctx, providers.ChatRequest{
		Messages: params.Messages,
		Model:    model,
		Options:  options,
	})
	if err != nil {
		client.SendResponse(protocol.NewErrorResponse(req.ID, protocol.ErrInternal, err.Error()))
		return
	}

	result := map[string]any{
		"provider": prov.Name(),
		"model":    model,
		"content":  resp.Content,
	}
	if resp.Usage != nil {
		result["usage"] = resp.Usage
	}
	client.SendResponse(protocol.NewOKResponse(req.ID, result))
}

func (m *LLMMethods) resolveProvider(ctx context.Context, providerName, model string) (providers.Provider, string, error) {
	tenantID := store.TenantIDFromContext(ctx)
	if tenantID == uuid.Nil {
		tenantID = providers.MasterTenantID
	}

	try := func(name string) (providers.Provider, string, bool) {
		if name == "" {
			return nil, "", false
		}
		p, err := m.providers.GetForTenant(tenantID, name)
		if err != nil || p == nil {
			return nil, "", false
		}
		selectedModel := model
		if selectedModel == "" {
			selectedModel = strings.TrimSpace(m.cfg.Model)
		}
		if selectedModel == "" {
			selectedModel = p.DefaultModel()
		}
		return p, selectedModel, true
	}

	if p, selectedModel, ok := try(providerName); ok {
		return p, selectedModel, nil
	}
	if providerName != "" {
		return nil, "", fmt.Errorf("provider not found: %s", providerName)
	}
	for _, name := range m.providers.ListForTenant(tenantID) {
		if p, selectedModel, ok := try(name); ok {
			return p, selectedModel, nil
		}
	}
	return nil, "", fmt.Errorf("no providers configured")
}
