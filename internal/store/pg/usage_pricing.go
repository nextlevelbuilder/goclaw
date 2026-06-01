package pg

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"math/big"
	"strconv"
	"strings"

	"github.com/google/uuid"
	"github.com/nextlevelbuilder/goclaw/internal/store"
)

type PGUsageCapStore struct {
	db *sql.DB
}

func NewPGUsageCapStore(db *sql.DB) *PGUsageCapStore {
	return &PGUsageCapStore{db: db}
}

func (s *PGUsageCapStore) UpsertPricingCatalog(ctx context.Context, entries []store.UsagePricingCatalogEntry) (int, error) {
	const q = `
INSERT INTO usage_pricing_catalog (
	model_id, canonical_model_id, raw_pricing, raw_model,
	input_price, output_price, cache_read_price, cache_write_price,
	reasoning_price, request_price, image_price, web_search_price, synced_at
) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13)
ON CONFLICT (model_id) DO UPDATE SET
	canonical_model_id = EXCLUDED.canonical_model_id,
	raw_pricing = EXCLUDED.raw_pricing,
	raw_model = EXCLUDED.raw_model,
	input_price = EXCLUDED.input_price,
	output_price = EXCLUDED.output_price,
	cache_read_price = EXCLUDED.cache_read_price,
	cache_write_price = EXCLUDED.cache_write_price,
	reasoning_price = EXCLUDED.reasoning_price,
	request_price = EXCLUDED.request_price,
	image_price = EXCLUDED.image_price,
	web_search_price = EXCLUDED.web_search_price,
	synced_at = EXCLUDED.synced_at,
	updated_at = now()`
	for _, e := range entries {
		if strings.TrimSpace(e.ModelID) == "" {
			continue
		}
		if err := validateUsagePricingFields(e.Pricing); err != nil {
			return 0, err
		}
		if len(e.RawPricing) == 0 {
			e.RawPricing = json.RawMessage(`{}`)
		}
		if len(e.RawModel) == 0 {
			e.RawModel = json.RawMessage(`{}`)
		}
		if _, err := s.db.ExecContext(ctx, q,
			e.ModelID, nullEmpty(e.CanonicalModelID), e.RawPricing, e.RawModel,
			priceVal(e.Pricing.Input), priceVal(e.Pricing.Output),
			priceVal(e.Pricing.CacheRead), priceVal(e.Pricing.CacheWrite),
			priceVal(e.Pricing.Reasoning), priceVal(e.Pricing.Request),
			priceVal(e.Pricing.Image), priceVal(e.Pricing.WebSearch), e.SyncedAt,
		); err != nil {
			return 0, err
		}
	}
	return len(entries), nil
}

func (s *PGUsageCapStore) ListPricingCatalog(ctx context.Context, q store.UsagePricingQuery) ([]store.UsagePricingCatalogEntry, error) {
	limit := q.Limit
	if limit <= 0 || limit > 500 {
		limit = 100
	}
	args := []any{}
	where := "TRUE"
	if q.ModelID != "" {
		args = append(args, "%"+q.ModelID+"%")
		where = "model_id ILIKE $1"
	}
	args = append(args, limit)
	rows, err := s.db.QueryContext(ctx, `
SELECT id, model_id, COALESCE(canonical_model_id,''), raw_pricing, raw_model,
	input_price::text, output_price::text, cache_read_price::text, cache_write_price::text,
	reasoning_price::text, request_price::text, image_price::text, web_search_price::text,
	synced_at, created_at, updated_at
FROM usage_pricing_catalog
WHERE `+where+`
ORDER BY model_id
LIMIT $`+strconv.Itoa(len(args))+``, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []store.UsagePricingCatalogEntry
	for rows.Next() {
		e, err := scanCatalog(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, e)
	}
	return out, rows.Err()
}

func (s *PGUsageCapStore) PutPricingOverride(ctx context.Context, o *store.UsagePricingOverride) error {
	if o.ID == uuid.Nil {
		o.ID = uuid.New()
	}
	if o.ProviderID == uuid.Nil || o.TenantID == uuid.Nil || o.ModelID == "" {
		return errors.New("tenant_id, provider_id, and model_id are required")
	}
	if err := validateUsagePricingFields(o.Pricing); err != nil {
		return err
	}
	if err := s.validateUsageCapRefs(ctx, o.TenantID, nil, &o.ProviderID); err != nil {
		return err
	}
	const q = `
INSERT INTO usage_pricing_overrides (
	id, tenant_id, provider_id, provider_type, model_id,
	input_price, output_price, cache_read_price, cache_write_price,
	reasoning_price, request_price, image_price, web_search_price, enabled
) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14)
ON CONFLICT (tenant_id, provider_id, model_id) DO UPDATE SET
	provider_type = EXCLUDED.provider_type,
	input_price = EXCLUDED.input_price,
	output_price = EXCLUDED.output_price,
	cache_read_price = EXCLUDED.cache_read_price,
	cache_write_price = EXCLUDED.cache_write_price,
	reasoning_price = EXCLUDED.reasoning_price,
	request_price = EXCLUDED.request_price,
	image_price = EXCLUDED.image_price,
	web_search_price = EXCLUDED.web_search_price,
	enabled = EXCLUDED.enabled,
	updated_at = now()
RETURNING id, created_at, updated_at`
	return s.db.QueryRowContext(ctx, q,
		o.ID, o.TenantID, o.ProviderID, o.ProviderType, o.ModelID,
		priceVal(o.Pricing.Input), priceVal(o.Pricing.Output),
		priceVal(o.Pricing.CacheRead), priceVal(o.Pricing.CacheWrite),
		priceVal(o.Pricing.Reasoning), priceVal(o.Pricing.Request),
		priceVal(o.Pricing.Image), priceVal(o.Pricing.WebSearch), o.Enabled,
	).Scan(&o.ID, &o.CreatedAt, &o.UpdatedAt)
}

func (s *PGUsageCapStore) ListPricingOverrides(ctx context.Context, q store.UsagePricingQuery) ([]store.UsagePricingOverride, error) {
	args := []any{q.TenantID}
	where := "tenant_id = $1"
	if q.ProviderID != uuid.Nil {
		args = append(args, q.ProviderID)
		where += " AND provider_id = $" + itoa(len(args))
	}
	rows, err := s.db.QueryContext(ctx, overrideSelectSQL+" WHERE "+where+" ORDER BY updated_at DESC", args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []store.UsagePricingOverride
	for rows.Next() {
		o, err := scanOverride(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, o)
	}
	return out, rows.Err()
}

func (s *PGUsageCapStore) DeletePricingOverride(ctx context.Context, tenantID, id uuid.UUID) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM usage_pricing_overrides WHERE tenant_id=$1 AND id=$2`, tenantID, id)
	return err
}

func (s *PGUsageCapStore) ResolvePricing(ctx context.Context, tenantID, providerID uuid.UUID, providerName, providerType, modelID string) (*store.ResolvedUsagePricing, error) {
	candidates := usagePricingModelCandidates(providerName, providerType, modelID)
	if tenantID != uuid.Nil && providerID != uuid.Nil {
		for _, candidate := range candidates {
			row := s.db.QueryRowContext(ctx, overrideSelectSQL+` WHERE tenant_id=$1 AND provider_id=$2 AND model_id=$3 AND enabled=true`, tenantID, providerID, candidate)
			if o, err := scanOverride(row); err == nil {
				return &store.ResolvedUsagePricing{ModelID: o.ModelID, ProviderID: providerID, ProviderType: providerType, Source: "override", Pricing: o.Pricing, OverrideID: o.ID}, nil
			} else if !errors.Is(err, sql.ErrNoRows) {
				return nil, err
			}
		}
	}
	for _, candidate := range candidates {
		row := s.db.QueryRowContext(ctx, `
SELECT id, model_id, COALESCE(canonical_model_id,''), raw_pricing, raw_model,
	input_price::text, output_price::text, cache_read_price::text, cache_write_price::text,
	reasoning_price::text, request_price::text, image_price::text, web_search_price::text,
	synced_at, created_at, updated_at
FROM usage_pricing_catalog WHERE model_id=$1 OR canonical_model_id=$1 LIMIT 1`, candidate)
		e, err := scanCatalog(row)
		if err == nil {
			return &store.ResolvedUsagePricing{ModelID: e.ModelID, ProviderID: providerID, ProviderType: providerType, Source: "catalog", Pricing: e.Pricing, CatalogSynced: &e.SyncedAt}, nil
		}
		if !errors.Is(err, sql.ErrNoRows) {
			return nil, err
		}
	}
	return nil, sql.ErrNoRows
}

func usagePricingModelCandidates(providerName, providerType, modelID string) []string {
	modelID = strings.TrimSpace(modelID)
	if modelID == "" {
		return nil
	}
	out := []string{modelID}
	if strings.Contains(modelID, "/") {
		return out
	}
	for _, prefix := range openRouterProviderPrefixes(providerName, providerType) {
		out = appendUniqueString(out, prefix+"/"+modelID)
	}
	return out
}

func openRouterProviderPrefixes(providerName, providerType string) []string {
	switch providerType {
	case store.ProviderAnthropicNative:
		return []string{"anthropic"}
	case store.ProviderGeminiNative, store.ProviderVertex:
		return []string{"google"}
	case store.ProviderOpenAICompat:
		switch normalizeProviderAlias(providerName) {
		case "openai", "azure", "azure-openai", "azure_openai":
			return []string{"openai"}
		case "anthropic":
			return []string{"anthropic"}
		case "gemini", "google", "vertex":
			return []string{"google"}
		}
		return nil
	case store.ProviderOpenRouter:
		return nil
	case store.ProviderGroq:
		return []string{"groq"}
	case store.ProviderDeepSeek:
		return []string{"deepseek"}
	case store.ProviderMistral:
		return []string{"mistralai"}
	case store.ProviderXAI:
		return []string{"x-ai"}
	case store.ProviderMiniMax:
		return []string{"minimax"}
	case store.ProviderCohere:
		return []string{"cohere"}
	case store.ProviderPerplexity:
		return []string{"perplexity"}
	case store.ProviderDashScope:
		return []string{"qwen"}
	default:
		return nil
	}
}

func normalizeProviderAlias(s string) string {
	s = strings.ToLower(strings.TrimSpace(s))
	s = strings.ReplaceAll(s, " ", "-")
	return s
}

func appendUniqueString(values []string, next string) []string {
	next = strings.TrimSpace(next)
	if next == "" {
		return values
	}
	for _, existing := range values {
		if existing == next {
			return values
		}
	}
	return append(values, next)
}

const overrideSelectSQL = `SELECT id, tenant_id, provider_id, provider_type, model_id,
	input_price::text, output_price::text, cache_read_price::text, cache_write_price::text,
	reasoning_price::text, request_price::text, image_price::text, web_search_price::text,
	enabled, created_at, updated_at FROM usage_pricing_overrides`

type scanner interface{ Scan(dest ...any) error }

func scanOverride(row scanner) (store.UsagePricingOverride, error) {
	var o store.UsagePricingOverride
	var prices [8]sql.NullString
	err := row.Scan(&o.ID, &o.TenantID, &o.ProviderID, &o.ProviderType, &o.ModelID,
		&prices[0], &prices[1], &prices[2], &prices[3], &prices[4], &prices[5], &prices[6], &prices[7],
		&o.Enabled, &o.CreatedAt, &o.UpdatedAt)
	o.Pricing = pricingFromNulls(prices)
	return o, err
}

func scanCatalog(row scanner) (store.UsagePricingCatalogEntry, error) {
	var e store.UsagePricingCatalogEntry
	var prices [8]sql.NullString
	err := row.Scan(&e.ID, &e.ModelID, &e.CanonicalModelID, &e.RawPricing, &e.RawModel,
		&prices[0], &prices[1], &prices[2], &prices[3], &prices[4], &prices[5], &prices[6], &prices[7],
		&e.SyncedAt, &e.CreatedAt, &e.UpdatedAt)
	e.Pricing = pricingFromNulls(prices)
	return e, err
}

func pricingFromNulls(p [8]sql.NullString) store.UsagePricingFields {
	return store.UsagePricingFields{
		Input: pricePtr(p[0]), Output: pricePtr(p[1]),
		CacheRead: pricePtr(p[2]), CacheWrite: pricePtr(p[3]),
		Reasoning: pricePtr(p[4]), Request: pricePtr(p[5]),
		Image: pricePtr(p[6]), WebSearch: pricePtr(p[7]),
	}
}

func pricePtr(v sql.NullString) *string {
	if !v.Valid {
		return nil
	}
	s := v.String
	return &s
}

func priceVal(v *string) any {
	if v == nil || strings.TrimSpace(*v) == "" {
		return nil
	}
	return strings.TrimSpace(*v)
}

func validateUsagePricingFields(fields store.UsagePricingFields) error {
	values := map[string]*string{
		"input":       fields.Input,
		"output":      fields.Output,
		"cache_read":  fields.CacheRead,
		"cache_write": fields.CacheWrite,
		"reasoning":   fields.Reasoning,
		"request":     fields.Request,
		"image":       fields.Image,
		"web_search":  fields.WebSearch,
	}
	for name, raw := range values {
		if raw == nil || strings.TrimSpace(*raw) == "" {
			continue
		}
		rat, ok := new(big.Rat).SetString(strings.TrimSpace(*raw))
		if !ok {
			return fmt.Errorf("invalid %s price", name)
		}
		if rat.Sign() < 0 {
			return fmt.Errorf("%s price must be non-negative", name)
		}
	}
	return nil
}

func nullEmpty(s string) any {
	if strings.TrimSpace(s) == "" {
		return nil
	}
	return s
}
