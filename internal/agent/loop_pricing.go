package agent

import (
	"context"
	"database/sql"
	"errors"
	"log/slog"
	"time"

	"github.com/google/uuid"
	"github.com/nextlevelbuilder/goclaw/internal/providers"
	"github.com/nextlevelbuilder/goclaw/internal/store"
	"github.com/nextlevelbuilder/goclaw/internal/tracing"
)

func (l *Loop) calculateLLMCost(ctx context.Context, providerName, model string, usage *providers.Usage) float64 {
	if usage == nil {
		return 0
	}
	tenantID := store.TenantIDFromContext(ctx)
	if tenantID == uuid.Nil {
		tenantID = l.tenantID
	}
	if tenantID == uuid.Nil {
		tenantID = store.MasterTenantID
	}
	if l.usageCaps != nil {
		resolveCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
		defer cancel()
		resolved, err := l.usageCaps.ResolvePricing(resolveCtx, tenantID, providerName, model)
		if err == nil && resolved != nil {
			cost, calcErr := tracing.CalculateCostFromUsagePricing(resolved.Pricing, usage)
			if calcErr == nil {
				return cost
			}
			slog.Warn("usage_pricing.trace_cost_calculation_failed", "provider", providerName, "model", model, "source", resolved.Source, "error", calcErr)
		} else if err != nil && !errors.Is(err, sql.ErrNoRows) {
			slog.Warn("usage_pricing.trace_pricing_resolve_failed", "provider", providerName, "model", model, "error", err)
		}
	}
	if pricing := tracing.LookupPricing(l.modelPricing, providerName, model); pricing != nil {
		return tracing.CalculateCost(pricing, usage)
	}
	return 0
}
