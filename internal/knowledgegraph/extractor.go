package knowledgegraph

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"

	"github.com/nextlevelbuilder/goclaw/internal/providers"
	"github.com/nextlevelbuilder/goclaw/internal/store"
)

// ExtractionResult holds entities and relations extracted from text.
type ExtractionResult struct {
	Entities  []store.Entity   `json:"entities"`
	Relations []store.Relation `json:"relations"`
}

// Extractor extracts entities and relations from text using an LLM.
type Extractor struct {
	provider      providers.Provider
	model         string
	minConfidence float64
}

// NewExtractor creates a new Extractor with the given provider, model, and confidence threshold.
func NewExtractor(provider providers.Provider, model string, minConfidence float64) *Extractor {
	if minConfidence <= 0 {
		minConfidence = 0.75
	}
	return &Extractor{provider: provider, model: model, minConfidence: minConfidence}
}

// Extract calls the LLM to extract entities and relations from text.
func (e *Extractor) Extract(ctx context.Context, text string) (*ExtractionResult, error) {
	// Truncate very long texts to avoid overwhelming the LLM
	const maxInputChars = 6000
	if len(text) > maxInputChars {
		text = text[:maxInputChars] + "\n\n[...truncated]"
	}

	req := providers.ChatRequest{
		Messages: []providers.Message{
			{Role: "system", Content: extractionSystemPrompt},
			{Role: "user", Content: text},
		},
		Model: e.model,
		Options: map[string]interface{}{
			"max_tokens":  8192,
			"temperature": 0.0,
		},
	}

	resp, err := e.provider.Chat(ctx, req)
	if err != nil {
		return nil, fmt.Errorf("kg extraction LLM call: %w", err)
	}

	// Parse JSON response
	var result ExtractionResult
	content := strings.TrimSpace(resp.Content)
	// Handle markdown code blocks
	content = stripCodeBlock(content)
	if err := json.Unmarshal([]byte(content), &result); err != nil {
		preview := content
		if len(preview) > 300 {
			preview = preview[:300] + "..."
		}
		slog.Warn("kg extraction: failed to parse LLM response", "error", err, "content_len", len(content), "finish_reason", resp.FinishReason, "preview", preview)
		return nil, fmt.Errorf("parse extraction result: %w", err)
	}

	// Filter by confidence threshold
	filtered := &ExtractionResult{}
	for _, ent := range result.Entities {
		if ent.Confidence >= e.minConfidence {
			ent.ExternalID = strings.ToLower(strings.TrimSpace(ent.ExternalID))
			ent.Name = strings.TrimSpace(ent.Name)
			ent.EntityType = strings.ToLower(strings.TrimSpace(ent.EntityType))
			filtered.Entities = append(filtered.Entities, ent)
		}
	}
	for _, rel := range result.Relations {
		if rel.Confidence >= e.minConfidence {
			rel.SourceEntityID = strings.ToLower(strings.TrimSpace(rel.SourceEntityID))
			rel.TargetEntityID = strings.ToLower(strings.TrimSpace(rel.TargetEntityID))
			rel.RelationType = strings.ToLower(strings.TrimSpace(rel.RelationType))
			filtered.Relations = append(filtered.Relations, rel)
		}
	}

	return filtered, nil
}

// stripCodeBlock removes ```json ... ``` wrapper if present.
func stripCodeBlock(s string) string {
	s = strings.TrimSpace(s)
	if strings.HasPrefix(s, "```") {
		if idx := strings.Index(s, "\n"); idx >= 0 {
			s = s[idx+1:]
		}
		if idx := strings.LastIndex(s, "```"); idx >= 0 {
			s = s[:idx]
		}
	}
	return strings.TrimSpace(s)
}
