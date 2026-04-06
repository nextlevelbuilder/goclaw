package vault

import (
	"context"
	"fmt"
	"strings"

	"github.com/nextlevelbuilder/goclaw/internal/memory"
)

// VaultAutoInjector implements memory.AutoInjector using VaultSearchService.
// It injects relevant vault documents into the system prompt as L0 summaries.
type VaultAutoInjector struct {
	search     *VaultSearchService
	maxResults int
}

// NewVaultAutoInjector creates an injector backed by a VaultSearchService.
// maxResults controls how many docs are injected (default 5 if <= 0).
func NewVaultAutoInjector(search *VaultSearchService, maxResults int) *VaultAutoInjector {
	if maxResults <= 0 {
		maxResults = 5
	}
	return &VaultAutoInjector{search: search, maxResults: maxResults}
}

// Inject implements memory.AutoInjector. Returns "" section if nothing relevant.
func (v *VaultAutoInjector) Inject(ctx context.Context, params memory.InjectParams) (*memory.InjectResult, error) {
	// Skip trivial messages
	if len(strings.TrimSpace(params.UserMessage)) < 10 {
		return &memory.InjectResult{}, nil
	}

	maxEntries := params.MaxEntries
	if maxEntries <= 0 {
		maxEntries = v.maxResults
	}

	threshold := params.Threshold
	if threshold <= 0 {
		threshold = 0.3
	}

	results, err := v.search.Search(ctx, UnifiedSearchOptions{
		Query:      params.UserMessage,
		AgentID:    params.AgentID,
		UserID:     params.UserID,
		TenantID:   params.TenantID,
		MaxResults: maxEntries * 2, // fetch extra, filter by threshold
		MinScore:   threshold,
		Weights:    DefaultSearchWeights(),
	})
	if err != nil {
		return &memory.InjectResult{}, err
	}

	if len(results) == 0 {
		return &memory.InjectResult{}, nil
	}

	// Build L0 summaries from top results
	entries := make([]memory.L0Summary, 0, maxEntries)
	for i, r := range results {
		if i >= maxEntries {
			break
		}
		snippet := r.Snippet
		if snippet == "" {
			snippet = r.Title
		}
		entries = append(entries, memory.L0Summary{
			Topic:   r.Title,
			Summary: snippet,
			ID:      r.ID,
		})
	}

	if len(entries) == 0 {
		return &memory.InjectResult{}, nil
	}

	// Format section for system prompt
	var sb strings.Builder
	sb.WriteString("## Vault Context\n")
	for _, e := range entries {
		sb.WriteString(fmt.Sprintf("- %s: %s\n", e.Topic, e.Summary))
	}

	topScore := results[0].Score

	return &memory.InjectResult{
		Section:    sb.String(),
		MatchCount: len(results),
		Injected:   len(entries),
		TopScore:   topScore,
	}, nil
}
