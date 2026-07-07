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

const (
	extractionMaxInputChars        = 12000
	extractionRetryMaxInputChars   = 6000
	extractionMaxOutputTokens      = 4096
	extractionRetryMaxOutputTokens = 4096
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
	resp, err := callExtractionProvider(ctx, provider, model, caps, messages, allowed, extractionMaxInputChars, extractionMaxOutputTokens, "channel-memory-extraction")
	if err != nil {
		return nil, err
	}
	if resp == nil {
		return nil, fmt.Errorf("background provider returned nil response")
	}
	if resp.FinishReason != "length" {
		items, err := parseExtractionResponse(resp.Content)
		if !shouldRetryExtractionParse(resp, err) {
			return items, err
		}
	}

	resp, err = callExtractionProvider(ctx, provider, model, caps, messages, allowed, extractionRetryMaxInputChars, extractionRetryMaxOutputTokens, "channel-memory-extraction-retry")
	if err != nil {
		return nil, err
	}
	if resp == nil {
		return nil, fmt.Errorf("background provider returned nil response")
	}
	if resp.FinishReason == "length" {
		return nil, fmt.Errorf("extraction response truncated after retry")
	}
	return parseExtractionResponse(resp.Content)
}

func callExtractionProvider(
	ctx context.Context,
	provider providers.Provider,
	model string,
	caps *usagecaps.Service,
	messages []store.PendingMessage,
	allowed []string,
	maxInputChars int,
	maxOutputTokens int,
	purpose string,
) (*providers.ChatResponse, error) {
	input := buildExtractionInput(messagesWithinExtractionBudget(messages, maxInputChars))
	req := providers.ChatRequest{
		Model: model,
		Messages: []providers.Message{
			{Role: "system", Content: extractionPrompt(allowed)},
			{Role: "user", Content: input},
		},
		Options: map[string]any{"max_tokens": maxOutputTokens, "temperature": 0.1},
	}
	if caps != nil {
		return caps.Chat(ctx, provider, req, usagecaps.ChatOptions{
			ModelID:         model,
			Purpose:         purpose,
			MaxOutputTokens: maxOutputTokens,
		})
	}
	return provider.Chat(ctx, req)
}

func messagesWithinExtractionBudget(messages []store.PendingMessage, maxInputChars int) []store.PendingMessage {
	if maxInputChars <= 0 {
		return nil
	}
	var sb strings.Builder
	out := make([]store.PendingMessage, 0, len(messages))
	for _, msg := range messages {
		line := extractionMessageLine(msg)
		if len(out) > 0 && sb.Len()+len(line) > maxInputChars {
			break
		}
		if len(out) == 0 && len(line) > maxInputChars {
			break
		}
		sb.WriteString(line)
		out = append(out, msg)
	}
	return out
}

func buildExtractionInput(messages []store.PendingMessage) string {
	var sb strings.Builder
	for _, msg := range messages {
		sb.WriteString(extractionMessageLine(msg))
	}
	return sb.String()
}

func extractionMessageLine(msg store.PendingMessage) string {
	var sb strings.Builder
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
	return sb.String()
}

func extractionPrompt(allowed []string) string {
	return `Extract only durable, reusable work context from channel messages.
Allowed item types: ` + strings.Join(allowed, ", ") + `.
Never include secrets, credentials, tokens, payment data, private addresses, phone numbers, health/legal/financial sensitive details, casual chatter, jokes, or low-confidence guesses.
Return at most 20 items, highest-confidence first.
Return strict JSON array only. Each item:
{"type":"people|projects|decisions|todos|preferences|events","summary":"one concise redacted fact","topics":["..."],"entities":["..."],"confidence":0.0-1.0}
If nothing durable remains, return [].`
}

func shouldRetryExtractionParse(resp *providers.ChatResponse, err error) bool {
	if err == nil || resp == nil {
		return false
	}
	raw := strings.TrimSpace(resp.Content)
	return raw == "" || strings.Contains(err.Error(), "unexpected end of JSON input")
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
