package consolidation

import (
	"context"
	"encoding/json"
	"strings"
	"time"

	"github.com/nextlevelbuilder/goclaw/internal/providers"
	"github.com/nextlevelbuilder/goclaw/internal/store"
)

// metadataExtractionPrompt instructs the LLM to extract structured data from a summary.
const metadataExtractionPrompt = `Extract structured information from the conversation summary below.

Return a JSON object with these fields (all optional, omit if not present):
{
  "decisions": [{"content": "decision text", "status": "confirmed|pending|rejected"}],
  "action_items": [{"content": "task description", "assignee": "person or role", "due_date": "YYYY-MM-DD or empty", "status": "pending|in_progress|done"}],
  "entities": ["key entity names mentioned"]
}

Rules:
- decisions: explicit choices or determinations made during the conversation
- action_items: tasks, follow-ups, or commitments mentioned
- entities: important people, projects, tools, or concepts discussed
- Keep content concise (1-2 sentences max)
- Only include clear, actionable items, not general discussion
- If no structured data found, return empty arrays

Summary:`

// ExtractMetadata uses LLM to extract structured data from an episodic summary.
// Returns nil if extraction fails or yields no results.
func ExtractMetadata(ctx context.Context, provider providers.Provider, model, summary string) *store.EpisodicMetadata {
	if summary == "" || provider == nil {
		return nil
	}

	sctx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()

	resp, err := provider.Chat(sctx, providers.ChatRequest{
		Messages: []providers.Message{
			{Role: "system", Content: metadataExtractionPrompt},
			{Role: "user", Content: summary},
		},
		Model:   model,
		Options: map[string]any{"max_tokens": 1024, "temperature": 0.1},
	})
	if err != nil {
		return nil
	}

	return parseMetadataJSON(resp.Content)
}

// parseMetadataJSON extracts JSON from LLM response and parses into EpisodicMetadata.
func parseMetadataJSON(content string) *store.EpisodicMetadata {
	// Find JSON in response (may be wrapped in markdown code blocks)
	jsonStr := extractJSON(content)
	if jsonStr == "" {
		return nil
	}

	var result struct {
		Decisions   []store.EpisodicDecision   `json:"decisions"`
		ActionItems []store.EpisodicActionItem `json:"action_items"`
		Entities    []string                   `json:"entities"`
	}
	if err := json.Unmarshal([]byte(jsonStr), &result); err != nil {
		return nil
	}

	// Return nil if everything is empty
	if len(result.Decisions) == 0 && len(result.ActionItems) == 0 && len(result.Entities) == 0 {
		return nil
	}

	return &store.EpisodicMetadata{
		Decisions:   result.Decisions,
		ActionItems: result.ActionItems,
		Entities:    result.Entities,
	}
}

// extractJSON finds JSON object in text, handling markdown code blocks.
func extractJSON(text string) string {
	// Try to find JSON in code block first
	if idx := strings.Index(text, "```json"); idx >= 0 {
		start := idx + 7
		if end := strings.Index(text[start:], "```"); end >= 0 {
			return strings.TrimSpace(text[start : start+end])
		}
	}
	if idx := strings.Index(text, "```"); idx >= 0 {
		start := idx + 3
		// Skip language identifier if present
		if nlIdx := strings.Index(text[start:], "\n"); nlIdx >= 0 {
			start += nlIdx + 1
		}
		if end := strings.Index(text[start:], "```"); end >= 0 {
			candidate := strings.TrimSpace(text[start : start+end])
			if strings.HasPrefix(candidate, "{") {
				return candidate
			}
		}
	}

	// Try to find raw JSON object
	start := strings.Index(text, "{")
	if start < 0 {
		return ""
	}
	// Find matching closing brace
	depth := 0
	for i := start; i < len(text); i++ {
		switch text[i] {
		case '{':
			depth++
		case '}':
			depth--
			if depth == 0 {
				return text[start : i+1]
			}
		}
	}
	return ""
}
