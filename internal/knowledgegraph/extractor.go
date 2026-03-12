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
		Options: map[string]any{
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

	originalContent := content
	content = sanitizeJSON(content)
	if content != originalContent {
		slog.Debug("kg extraction: sanitized JSON output",
			"original_len", len(originalContent),
			"sanitized_len", len(content),
		)
	}

	if err := json.Unmarshal([]byte(content), &result); err != nil {
		preview := originalContent
		if len(preview) > 300 {
			preview = preview[:300] + "..."
		}
		slog.Warn("kg extraction: failed to parse LLM response",
			"error", err,
			"content_len", len(originalContent),
			"finish_reason", resp.FinishReason,
			"preview", preview,
		)
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

// sanitizeJSON fixes common LLM JSON issues while preserving string values.
// It walks the JSON character-by-character, only applying fixes outside quoted strings:
//   - Malformed decimals: "0. 85" → "0.85"
//   - Trailing commas: [1, 2,] → [1, 2]
func sanitizeJSON(s string) string {
	var b strings.Builder
	b.Grow(len(s))

	inString := false
	escaped := false

	for i := 0; i < len(s); i++ {
		ch := s[i]

		if escaped {
			b.WriteByte(ch)
			escaped = false
			continue
		}

		if ch == '\\' && inString {
			b.WriteByte(ch)
			escaped = true
			continue
		}

		if ch == '"' {
			inString = !inString
			b.WriteByte(ch)
			continue
		}

		if inString {
			b.WriteByte(ch)
			continue
		}

		// Fix malformed decimals: "0. 85" → "0.85"
		if ch == '.' && i > 0 && isDigit(s[i-1]) {
			b.WriteByte('.')
			for i+1 < len(s) && s[i+1] == ' ' {
				i++
			}
			continue
		}

		// Fix trailing commas: skip comma if next non-whitespace is } or ]
		if ch == ',' {
			j := i + 1
			for j < len(s) && (s[j] == ' ' || s[j] == '\t' || s[j] == '\n' || s[j] == '\r') {
				j++
			}
			if j < len(s) && (s[j] == '}' || s[j] == ']') {
				continue
			}
		}

		b.WriteByte(ch)
	}

	return b.String()
}

func isDigit(c byte) bool {
	return c >= '0' && c <= '9'
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

