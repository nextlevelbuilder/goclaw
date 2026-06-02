package agent

import (
	"context"
	"fmt"

	"github.com/google/uuid"
	"github.com/nextlevelbuilder/goclaw/internal/providers"
	"github.com/nextlevelbuilder/goclaw/internal/store"
	usagecaps "github.com/nextlevelbuilder/goclaw/internal/usage/caps"
)

func (l *Loop) reserveInternalLLMUsage(ctx context.Context, chatReq providers.ChatRequest, purpose string) (*usagecaps.Reservation, error) {
	if l.usageCaps == nil || l.provider == nil {
		return nil, nil
	}
	return l.reserveInternalLLMUsageFor(ctx, chatReq, purpose, l.provider.Name(), chatReq.Model)
}

func (l *Loop) reserveInternalLLMUsageFor(ctx context.Context, chatReq providers.ChatRequest, purpose, providerName, model string) (*usagecaps.Reservation, error) {
	if l.usageCaps == nil {
		return nil, nil
	}
	if model == "" {
		model = chatReq.Model
	}
	if model == "" {
		model = l.model
	}
	tenantID := store.TenantIDFromContext(ctx)
	if tenantID == uuid.Nil {
		tenantID = l.tenantID
	}
	return l.usageCaps.Preflight(ctx, usagecaps.Request{
		TenantID:        tenantID,
		AgentID:         l.agentUUID,
		ProviderName:    providerName,
		ModelID:         model,
		ReservationKey:  fmt.Sprintf("%s:%s:%s", purpose, l.agentUUID.String(), uuid.NewString()),
		Messages:        chatReq.Messages,
		MaxOutputTokens: l.maxOutputTokensFromRequest(chatReq),
	})
}

func (l *Loop) callInternalLLMWithUsage(ctx context.Context, chatReq providers.ChatRequest, purpose string) (*providers.ChatResponse, error) {
	if fallbackProvider, ok := l.provider.(*providers.ModelFallbackProvider); ok {
		before := func(callCtx context.Context, entry providers.FallbackCandidate, actualReq providers.ChatRequest) (providers.FallbackAfterCall, error) {
			candidatePurpose := fmt.Sprintf("%s:%s:%s", purpose, entry.ProviderName, actualReq.Model)
			reservation, err := l.reserveInternalLLMUsageFor(callCtx, actualReq, candidatePurpose, entry.ProviderName, actualReq.Model)
			if err != nil {
				return nil, err
			}
			return func(resp *providers.ChatResponse, callErr error, _ providers.FallbackCallInfo) {
				if reservation != nil {
					reservation.Reconcile(callCtx, resp, callErr)
				}
			}, nil
		}
		return fallbackProvider.ChatWithHook(ctx, chatReq, before)
	}
	reservation, reserveErr := l.reserveInternalLLMUsage(ctx, chatReq, purpose)
	if reserveErr != nil {
		return nil, reserveErr
	}
	resp, err := l.provider.Chat(ctx, chatReq)
	if reservation != nil {
		reservation.Reconcile(ctx, resp, err)
	}
	return resp, err
}

func (l *Loop) maxOutputTokensFromRequest(chatReq providers.ChatRequest) int {
	maxTokens := l.effectiveMaxTokens()
	if chatReq.Options == nil {
		return maxTokens
	}
	if v, ok := chatReq.Options[providers.OptMaxTokens]; ok {
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
