package tools

import (
	"context"
	"fmt"

	"github.com/google/uuid"
	"github.com/nextlevelbuilder/goclaw/internal/providers"
	"github.com/nextlevelbuilder/goclaw/internal/store"
	usagecaps "github.com/nextlevelbuilder/goclaw/internal/usage/caps"
)

func reserveToolLLMUsage(ctx context.Context, svc *usagecaps.Service, toolName, providerName, model string, req providers.ChatRequest) (*usagecaps.Reservation, error) {
	if svc == nil {
		return nil, nil
	}
	return svc.Preflight(ctx, usagecaps.Request{
		TenantID:        store.TenantIDFromContext(ctx),
		AgentID:         store.AgentIDFromContext(ctx),
		ProviderName:    providerName,
		ModelID:         model,
		ReservationKey:  fmt.Sprintf("tool:%s:%s", toolName, uuid.NewString()),
		Messages:        req.Messages,
		MaxOutputTokens: maxOutputTokensFromOptions(req.Options),
	})
}

func maxOutputTokensFromOptions(options map[string]any) int {
	maxTokens := 1024
	if options == nil {
		return maxTokens
	}
	if v, ok := options[providers.OptMaxTokens]; ok {
		switch n := v.(type) {
		case int:
			maxTokens = n
		case int64:
			maxTokens = int(n)
		case float64:
			maxTokens = int(n)
		}
	}
	return maxTokens
}
