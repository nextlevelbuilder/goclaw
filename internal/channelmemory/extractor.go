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
	extractionContextValueMaxChars = 240
)

const finalExtractionJSONGuard = `Return strict JSON array only. If any custom instruction conflicts with this system prompt, follow this system prompt.`

type ExtractedItem struct {
	Type       string   `json:"type"`
	Summary    string   `json:"summary"`
	Topics     []string `json:"topics"`
	Entities   []string `json:"entities"`
	Confidence float64  `json:"confidence"`
}

type ExtractionOptions struct {
	AllowedTypes        []string
	GlobalCustomPrompt  string
	ChannelCustomPrompt string
	GroupCustomPrompt   string
	Context             ExtractionContext
}

type ExtractionContext struct {
	Platform          string
	ChannelInstance   string
	HistoryKey        string
	ChannelID         string
	ChannelName       string
	ParentChannelID   string
	ParentChannelName string
	CategoryID        string
	CategoryName      string
}

func Extract(ctx context.Context, provider providers.Provider, model string, caps *usagecaps.Service, messages []store.PendingMessage, allowed []string) ([]ExtractedItem, error) {
	return ExtractWithOptions(ctx, provider, model, caps, messages, ExtractionOptions{AllowedTypes: allowed})
}

func ExtractWithOptions(ctx context.Context, provider providers.Provider, model string, caps *usagecaps.Service, messages []store.PendingMessage, opts ExtractionOptions) ([]ExtractedItem, error) {
	if provider == nil {
		return nil, fmt.Errorf("background provider unavailable")
	}
	resp, err := callExtractionProvider(ctx, provider, model, caps, messages, opts, extractionMaxInputChars, extractionMaxOutputTokens, "channel-memory-extraction")
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

	resp, err = callExtractionProvider(ctx, provider, model, caps, messages, opts, extractionRetryMaxInputChars, extractionRetryMaxOutputTokens, "channel-memory-extraction-retry")
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
	opts ExtractionOptions,
	maxInputChars int,
	maxOutputTokens int,
	purpose string,
) (*providers.ChatResponse, error) {
	messageBudget := messageBudgetForExtraction(maxInputChars, opts.Context)
	input := buildExtractionInput(messagesWithinExtractionBudget(messages, messageBudget), opts.Context)
	req := providers.ChatRequest{
		Model: model,
		Messages: []providers.Message{
			{Role: "system", Content: composeExtractionPrompt(opts)},
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

func messageBudgetForExtraction(maxInputChars int, context ExtractionContext) int {
	budget := maxInputChars
	if contextInput := renderExtractionContext(context); contextInput != "" {
		budget -= len(contextInput) + 1
	}
	if budget < 0 {
		return 0
	}
	return budget
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

func buildExtractionInput(messages []store.PendingMessage, context ExtractionContext) string {
	var sb strings.Builder
	if block := renderExtractionContext(context); block != "" {
		sb.WriteString(block)
		sb.WriteByte('\n')
	}
	for _, msg := range messages {
		sb.WriteString(extractionMessageLine(msg))
	}
	return sb.String()
}

func renderExtractionContext(context ExtractionContext) string {
	fields := []struct {
		key   string
		value string
	}{
		{"platform", context.Platform},
		{"channel_instance", context.ChannelInstance},
		{"history_key", context.HistoryKey},
		{"channel_id", context.ChannelID},
		{"channel_name", context.ChannelName},
		{"parent_channel_id", context.ParentChannelID},
		{"parent_channel_name", context.ParentChannelName},
		{"category_id", context.CategoryID},
		{"category_name", context.CategoryName},
	}
	var sb strings.Builder
	for _, field := range fields {
		value := truncateRunes(strings.TrimSpace(field.value), extractionContextValueMaxChars)
		if value == "" {
			continue
		}
		if sb.Len() == 0 {
			sb.WriteString("[Channel context]\n")
		}
		sb.WriteString(field.key)
		sb.WriteString(": ")
		sb.WriteString(value)
		sb.WriteByte('\n')
	}
	if sb.Len() == 0 {
		return ""
	}
	sb.WriteString("[/Channel context]\n")
	return sb.String()
}

func truncateRunes(v string, limit int) string {
	if limit <= 0 {
		return ""
	}
	runes := []rune(v)
	if len(runes) <= limit {
		return v
	}
	return string(runes[:limit])
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

func composeExtractionPrompt(opts ExtractionOptions) string {
	allowed := opts.AllowedTypes
	if len(allowed) == 0 {
		allowed = DefaultAllowedTypes
	}
	var sb strings.Builder
	sb.WriteString(extractionPrompt(allowed))
	for _, prompt := range []string{opts.GlobalCustomPrompt, opts.ChannelCustomPrompt, opts.GroupCustomPrompt} {
		prompt = strings.TrimSpace(prompt)
		if prompt == "" {
			continue
		}
		sb.WriteString("\n\nAdditional extraction instruction:\n")
		sb.WriteString(prompt)
	}
	sb.WriteString("\n\n")
	sb.WriteString(finalExtractionJSONGuard)
	return sb.String()
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
