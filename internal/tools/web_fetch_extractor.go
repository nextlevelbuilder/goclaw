package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"
	"time"
)

// ContentExtractor extracts readable content from a URL.
type ContentExtractor interface {
	Extract(ctx context.Context, rawURL string) (string, error)
	Name() string
}

// ExtractResult holds the output from a successful extraction.
type ExtractResult struct {
	Content   string
	Extractor string // name of the extractor that succeeded
}

// ExtractorChain tries extractors in order until one returns quality content.
type ExtractorChain struct {
	extractors []ContentExtractor
	maxRetries []int           // per-extractor max attempts (default 1)
	timeouts   []time.Duration // per-extractor chain-level timeout (0 = no chain timeout)
}

// NewExtractorChain creates a chain from ordered extractors with default settings (1 attempt, no chain timeout).
func NewExtractorChain(extractors ...ContentExtractor) *ExtractorChain {
	maxRetries := make([]int, len(extractors))
	timeouts := make([]time.Duration, len(extractors))
	for i := range extractors {
		maxRetries[i] = 1
	}
	return &ExtractorChain{extractors: extractors, maxRetries: maxRetries, timeouts: timeouts}
}

// Extract runs each extractor in order with per-entry retry and optional timeout.
// Returns the first quality result or cascades to the next extractor.
func (c *ExtractorChain) Extract(ctx context.Context, rawURL string) (ExtractResult, error) {
	var lastErr error
	for i, ext := range c.extractors {
		maxRetries := c.maxRetries[i]

		for attempt := 1; attempt <= maxRetries; attempt++ {
			// Apply chain-level timeout if configured.
			callCtx, cancel := ctx, context.CancelFunc(nil)
			if timeout := c.timeouts[i]; timeout > 0 {
				callCtx, cancel = context.WithTimeout(ctx, timeout)
			}

			content, err := ext.Extract(callCtx, rawURL)
			if cancel != nil {
				cancel()
			}
			if err != nil {
				lastErr = err
				if ctx.Err() != nil {
					return ExtractResult{}, fmt.Errorf("context cancelled: %w", lastErr)
				}
				if attempt < maxRetries {
					slog.Warn("extractor_chain: attempt failed, retrying",
						"extractor", ext.Name(), "url", rawURL,
						"attempt", attempt, "max_retries", maxRetries,
						"error", err)
				} else {
					slog.Debug("extractor failed", "extractor", ext.Name(), "url", rawURL, "error", err)
				}
				continue
			}
			if !isQualityContent(content) {
				slog.Debug("extractor returned low quality content", "extractor", ext.Name(), "url", rawURL, "chars", len(content))
				lastErr = fmt.Errorf("%s: content below quality threshold (%d chars)", ext.Name(), len(content))
				break // low quality is not transient — don't retry, cascade to next
			}
			return ExtractResult{Content: content, Extractor: ext.Name()}, nil
		}

		slog.Debug("extractor_chain: extractor exhausted, moving to next",
			"extractor", ext.Name(), "max_retries", maxRetries)
	}
	if lastErr != nil {
		return ExtractResult{}, fmt.Errorf("all extractors failed for %s: %w", rawURL, lastErr)
	}
	return ExtractResult{}, fmt.Errorf("no extractors configured")
}

// isQualityContent checks if extracted content meets minimum quality thresholds.
// Returns false for empty, very short (<100 chars), or low word count (<10 words) content.
func isQualityContent(content string) bool {
	trimmed := strings.TrimSpace(content)
	if len(trimmed) < 100 {
		return false
	}
	return len(strings.Fields(trimmed)) >= 10
}

// ---------------------------------------------------------------------------
// Extractor chain settings — stored in builtin_tools.settings for web_fetch
// ---------------------------------------------------------------------------

// ExtractorEntry represents a single extractor in the chain settings JSON.
type ExtractorEntry struct {
	Name       string `json:"name"`
	Enabled    bool   `json:"enabled"`
	Timeout    int    `json:"timeout,omitempty"`     // seconds, 0 = use extractor default
	MaxRetries int    `json:"max_retries,omitempty"` // default 1 (no retry)
	BaseURL    string `json:"base_url,omitempty"`    // for defuddle: CF Worker URL
}

// extractorChainSettings is the JSON schema for web_fetch builtin_tools.settings.
type extractorChainSettings struct {
	Extractors []ExtractorEntry `json:"extractors,omitempty"`
}

// ResolveExtractorChain parses builtin_tools.settings from context and builds
// an ordered ExtractorChain. Returns nil if no extractors are enabled.
func ResolveExtractorChain(ctx context.Context, tool *WebFetchTool) *ExtractorChain {
	if settings := BuiltinToolSettingsFromCtx(ctx); settings != nil {
		if raw, ok := settings["web_fetch"]; ok && len(raw) > 0 {
			chain := parseExtractorChainSettings(raw, tool)
			if chain != nil {
				return chain
			}
		}
	}
	// Default fallback: InProcess only (no external extractors).
	return NewExtractorChain(&InProcessExtractor{tool: tool})
}

// parseExtractorChainSettings parses the settings JSON and builds a chain.
func parseExtractorChainSettings(raw []byte, tool *WebFetchTool) *ExtractorChain {
	var settings extractorChainSettings
	if err := json.Unmarshal(raw, &settings); err != nil {
		slog.Warn("web_fetch: failed to parse extractor chain settings", "error", err)
		return nil
	}

	var extractors []ContentExtractor
	var maxRetries []int
	var timeouts []time.Duration
	for _, entry := range settings.Extractors {
		if !entry.Enabled || entry.Name == "" {
			continue
		}
		switch entry.Name {
		case "defuddle":
			extractors = append(extractors, NewDefuddleExtractorFromEntry(entry))
		case "html-to-markdown":
			extractors = append(extractors, &InProcessExtractor{tool: tool})
		default:
			slog.Warn("web_fetch: unknown extractor in chain, skipping", "name", entry.Name)
			continue
		}
		retries := entry.MaxRetries
		if retries <= 0 {
			retries = 1
		}
		maxRetries = append(maxRetries, retries)
		timeouts = append(timeouts, time.Duration(entry.Timeout)*time.Second)
	}
	if len(extractors) == 0 {
		return nil
	}
	return &ExtractorChain{extractors: extractors, maxRetries: maxRetries, timeouts: timeouts}
}
