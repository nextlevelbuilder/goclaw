package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"
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
}

// NewExtractorChain creates a chain from ordered extractors.
func NewExtractorChain(extractors ...ContentExtractor) *ExtractorChain {
	return &ExtractorChain{extractors: extractors}
}

// Extract runs each extractor in order, returning the first quality result.
func (c *ExtractorChain) Extract(ctx context.Context, rawURL string) (ExtractResult, error) {
	var lastErr error
	for _, ext := range c.extractors {
		content, err := ext.Extract(ctx, rawURL)
		if err != nil {
			slog.Debug("extractor failed", "extractor", ext.Name(), "url", rawURL, "error", err)
			lastErr = err
			continue
		}
		if !isQualityContent(content) {
			slog.Debug("extractor returned low quality content", "extractor", ext.Name(), "url", rawURL, "chars", len(content))
			lastErr = fmt.Errorf("%s: content below quality threshold (%d chars)", ext.Name(), len(content))
			continue
		}
		return ExtractResult{Content: content, Extractor: ext.Name()}, nil
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
	Name    string `json:"name"`
	Enabled bool   `json:"enabled"`
	Timeout int    `json:"timeout,omitempty"`  // seconds, 0 = use extractor default
	BaseURL string `json:"base_url,omitempty"` // for defuddle: CF Worker URL
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
		}
	}
	if len(extractors) == 0 {
		return nil
	}
	return NewExtractorChain(extractors...)
}
