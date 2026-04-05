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

const maxChunkChars = 12000

// Extract calls the LLM to extract entities and relations from text.
// For long texts, it splits into chunks, extracts from each, and merges results.
func (e *Extractor) Extract(ctx context.Context, text string) (*ExtractionResult, error) {
	// Short text: single extraction
	if len(text) <= maxChunkChars {
		return e.extractChunk(ctx, text)
	}

	// Long text: split into chunks and merge
	chunks := splitChunks(text, maxChunkChars)
	slog.Info("kg extraction: splitting long input", "chunks", len(chunks), "total_len", len(text))

	merged := &ExtractionResult{}
	for i, chunk := range chunks {
		result, err := e.extractChunk(ctx, chunk)
		if err != nil {
			slog.Warn("kg extraction: chunk failed", "chunk", i+1, "total", len(chunks), "error", err)
			continue // skip failed chunk, extract what we can
		}
		merged = mergeResults(merged, result)
	}
	return merged, nil
}

// extractChunk extracts entities from a single chunk of text.
func (e *Extractor) extractChunk(ctx context.Context, text string) (*ExtractionResult, error) {
	req := providers.ChatRequest{
		Messages: []providers.Message{
			{Role: "system", Content: extractionSystemPrompt},
			{Role: "user", Content: text},
		},
		Model: e.model,
		Options: map[string]any{
			"max_tokens":  8192,
			"temperature": 0.2,
		},
	}

	resp, err := e.provider.Chat(ctx, req)
	if err != nil {
		return nil, fmt.Errorf("kg extraction LLM call: %w", err)
	}

	// If response was truncated, retry with shorter input
	if resp.FinishReason == "length" {
		slog.Warn("kg extraction: response truncated, retrying with shorter input")
		const retryMaxChars = 8000
		if len(text) > retryMaxChars {
			text = text[:retryMaxChars] + "\n\n[...truncated]"
		}
		req.Messages[1].Content = text
		resp, err = e.provider.Chat(ctx, req)
		if err != nil {
			return nil, fmt.Errorf("kg extraction LLM retry: %w", err)
		}
		if resp.FinishReason == "length" {
			return nil, fmt.Errorf("kg extraction: response still truncated after retry")
		}
	}

	// Parse JSON response
	var result ExtractionResult
	content := strings.TrimSpace(resp.Content)
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

	// Filter by confidence threshold and normalize
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

// splitChunks splits text into chunks at paragraph boundaries (\n\n).
func splitChunks(text string, maxChars int) []string {
	if len(text) <= maxChars {
		return []string{text}
	}

	var chunks []string
	for len(text) > 0 {
		if len(text) <= maxChars {
			chunks = append(chunks, text)
			break
		}
		// Find last paragraph break within limit
		cut := maxChars
		if idx := strings.LastIndex(text[:cut], "\n\n"); idx > cut/2 {
			cut = idx
		}
		chunks = append(chunks, strings.TrimSpace(text[:cut]))
		text = strings.TrimSpace(text[cut:])
	}
	return chunks
}

// mergeResults merges two extraction results, deduplicating entities by external_id
// (keeping higher confidence) and relations by source+type+target.
func mergeResults(a, b *ExtractionResult) *ExtractionResult {
	// Deduplicate entities — keep higher confidence
	entityMap := make(map[string]store.Entity, len(a.Entities)+len(b.Entities))
	for _, ent := range a.Entities {
		entityMap[ent.ExternalID] = ent
	}
	for _, ent := range b.Entities {
		if existing, ok := entityMap[ent.ExternalID]; !ok || ent.Confidence > existing.Confidence {
			entityMap[ent.ExternalID] = ent
		}
	}

	// Deduplicate relations
	type relKey struct{ src, rel, tgt string }
	relMap := make(map[relKey]store.Relation, len(a.Relations)+len(b.Relations))
	for _, rel := range a.Relations {
		relMap[relKey{rel.SourceEntityID, rel.RelationType, rel.TargetEntityID}] = rel
	}
	for _, rel := range b.Relations {
		k := relKey{rel.SourceEntityID, rel.RelationType, rel.TargetEntityID}
		if existing, ok := relMap[k]; !ok || rel.Confidence > existing.Confidence {
			relMap[k] = rel
		}
	}

	result := &ExtractionResult{
		Entities:  make([]store.Entity, 0, len(entityMap)),
		Relations: make([]store.Relation, 0, len(relMap)),
	}
	for _, ent := range entityMap {
		result.Entities = append(result.Entities, ent)
	}
	for _, rel := range relMap {
		result.Relations = append(result.Relations, rel)
	}
	return result
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

