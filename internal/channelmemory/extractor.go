package channelmemory

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/nextlevelbuilder/goclaw/internal/providers"
	"github.com/nextlevelbuilder/goclaw/internal/store"
	usagecaps "github.com/nextlevelbuilder/goclaw/internal/usage/caps"
)

type ExtractedItem struct {
	Type       string   `json:"type"`
	Summary    string   `json:"summary"`
	Topics     []string `json:"topics"`
	Entities   []string `json:"entities"`
	Confidence float64  `json:"confidence"`
}

func Extract(ctx context.Context, provider providers.Provider, model string, caps *usagecaps.Service, messages []store.PendingMessage, allowed []string) ([]ExtractedItem, error) {
	if provider == nil {
		return nil, fmt.Errorf("background provider unavailable")
	}
	var sb strings.Builder
	for _, msg := range messages {
		sb.WriteString(msg.CreatedAt.Format(time.RFC3339))
		sb.WriteString(" ")
		if msg.Sender != "" {
			sb.WriteString(msg.Sender)
		} else {
			sb.WriteString(msg.SenderID)
		}
		sb.WriteString(": ")
		body := msg.Body
		if len([]rune(body)) > 800 {
			body = string([]rune(body)[:800]) + "..."
		}
		sb.WriteString(body)
		sb.WriteByte('\n')
		if sb.Len() > 12000 {
			sb.WriteString("...(truncated)\n")
			break
		}
	}
	req := providers.ChatRequest{
		Model: model,
		Messages: []providers.Message{
			{Role: "system", Content: extractionPrompt(allowed)},
			{Role: "user", Content: sb.String()},
		},
		Options: map[string]any{"max_tokens": 1200, "temperature": 0.1},
	}
	var resp *providers.ChatResponse
	var err error
	if caps != nil {
		resp, err = caps.Chat(ctx, provider, req, usagecaps.ChatOptions{
			ModelID:         model,
			Purpose:         "channel-memory-extraction",
			MaxOutputTokens: 1200,
		})
	} else {
		resp, err = provider.Chat(ctx, req)
	}
	if err != nil {
		return nil, err
	}
	return parseExtractionResponse(resp.Content)
}

func extractionPrompt(allowed []string) string {
	return `Extract only durable, reusable work context from channel messages.
Allowed item types: ` + strings.Join(allowed, ", ") + `.
Never include secrets, credentials, tokens, payment data, private addresses, phone numbers, health/legal/financial sensitive details, casual chatter, jokes, or low-confidence guesses.
Return strict JSON array only. Each item:
{"type":"people|projects|decisions|todos|preferences|events","summary":"one concise redacted fact","topics":["..."],"entities":["..."],"confidence":0.0-1.0}
If nothing durable remains, return [].`
}

func parseExtractionResponse(content string) ([]ExtractedItem, error) {
	raw := strings.TrimSpace(content)
	raw = strings.TrimPrefix(raw, "```json")
	raw = strings.TrimPrefix(raw, "```")
	raw = strings.TrimSuffix(raw, "```")
	raw = strings.TrimSpace(raw)
	var items []ExtractedItem
	if err := json.Unmarshal([]byte(raw), &items); err != nil {
		return nil, fmt.Errorf("parse extraction JSON: %w", err)
	}
	out := items[:0]
	for _, item := range items {
		item.Summary = strings.TrimSpace(item.Summary)
		if item.Summary == "" || item.Type == "" {
			continue
		}
		if item.Confidence < 0 {
			item.Confidence = 0
		}
		if item.Confidence > 1 {
			item.Confidence = 1
		}
		out = append(out, item)
	}
	return out, nil
}
