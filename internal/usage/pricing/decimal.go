package pricing

import (
	"errors"
	"fmt"
	"math/big"
	"strings"

	"github.com/nextlevelbuilder/goclaw/internal/providers"
	"github.com/nextlevelbuilder/goclaw/internal/store"
)

var ErrUnknownPricing = errors.New("usage pricing unknown")

type BillableUsage struct {
	InputTokens      int64
	OutputTokens     int64
	CacheReadTokens  int64
	CacheWriteTokens int64
	ReasoningTokens  int64
	RequestCount     int64
	ImageCount       int64
	WebSearchCount   int64
}

func FromProviderUsage(u *providers.Usage) BillableUsage {
	if u == nil {
		return BillableUsage{}
	}
	inputTokens := int64(u.PromptTokens)
	cacheReadTokens := int64(u.CacheReadTokens)
	cacheWriteTokens := int64(u.CacheCreationTokens)
	if u.PromptTokensIncludeCachedSegments {
		inputTokens -= cacheReadTokens + cacheWriteTokens
		if inputTokens < 0 {
			inputTokens = 0
		}
	}
	return BillableUsage{
		InputTokens:      inputTokens,
		OutputTokens:     int64(u.CompletionTokens),
		CacheReadTokens:  cacheReadTokens,
		CacheWriteTokens: cacheWriteTokens,
		ReasoningTokens:  int64(u.ThinkingTokens),
		RequestCount:     int64(u.RequestCount),
		ImageCount:       int64(u.ImageCount),
		WebSearchCount:   int64(u.WebSearchCount),
	}
}

func (u BillableUsage) TotalTokens() int64 {
	return u.InputTokens + u.OutputTokens + u.CacheReadTokens + u.CacheWriteTokens
}

func CostMicros(fields store.UsagePricingFields, usage BillableUsage) (int64, error) {
	var total int64
	add := func(name string, price *string, units int64) error {
		if units <= 0 {
			return nil
		}
		if price == nil || strings.TrimSpace(*price) == "" {
			return fmt.Errorf("%w: %s", ErrUnknownPricing, name)
		}
		micros, err := microsForUnits(*price, units)
		if err != nil {
			return fmt.Errorf("%s pricing: %w", name, err)
		}
		total += micros
		return nil
	}
	if err := add("input", fields.Input, usage.InputTokens); err != nil {
		return 0, err
	}
	if usage.ReasoningTokens > 0 && fields.Reasoning != nil {
		visible := max(usage.OutputTokens-usage.ReasoningTokens, 0)
		if err := add("output", fields.Output, visible); err != nil {
			return 0, err
		}
		if err := add("reasoning", fields.Reasoning, usage.ReasoningTokens); err != nil {
			return 0, err
		}
	} else if err := add("output", fields.Output, usage.OutputTokens); err != nil {
		return 0, err
	}
	if err := add("cache_read", fields.CacheRead, usage.CacheReadTokens); err != nil {
		return 0, err
	}
	if err := add("cache_write", fields.CacheWrite, usage.CacheWriteTokens); err != nil {
		return 0, err
	}
	if err := add("request", fields.Request, usage.RequestCount); err != nil {
		return 0, err
	}
	if err := add("image", fields.Image, usage.ImageCount); err != nil {
		return 0, err
	}
	if err := add("web_search", fields.WebSearch, usage.WebSearchCount); err != nil {
		return 0, err
	}
	return total, nil
}

func microsForUnits(price string, units int64) (int64, error) {
	r, ok := new(big.Rat).SetString(strings.TrimSpace(price))
	if !ok {
		return 0, fmt.Errorf("invalid decimal %q", price)
	}
	r.Mul(r, big.NewRat(units, 1))
	r.Mul(r, big.NewRat(1_000_000, 1))
	q := new(big.Int).Quo(r.Num(), r.Denom())
	rem := new(big.Int).Rem(r.Num(), r.Denom())
	if rem.Sign() > 0 {
		q.Add(q, big.NewInt(1))
	}
	if !q.IsInt64() {
		return 0, errors.New("cost overflows int64 micros")
	}
	return q.Int64(), nil
}
