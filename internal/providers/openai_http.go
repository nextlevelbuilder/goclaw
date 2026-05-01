package providers

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"regexp"
	"strconv"
	"strings"
)

func (p *OpenAIProvider) doRequest(ctx context.Context, body any) (io.ReadCloser, error) {
	data, err := json.Marshal(body)
	if err != nil {
		return nil, fmt.Errorf("%s: marshal request: %w", p.name, err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, "POST", p.apiBase+p.chatPath, bytes.NewReader(data))
	if err != nil {
		return nil, fmt.Errorf("%s: create request: %w", p.name, err)
	}

	httpReq.Header.Set("Content-Type", "application/json")
	// Azure OpenAI/Foundry support for now atleast
	if strings.Contains(strings.ToLower(p.apiBase), "azure.com") {
		httpReq.Header.Set("api-key", p.apiKey)
	} else {
		prefix := p.authPrefix
		if prefix == "" {
			prefix = "Bearer "
		}
		httpReq.Header.Set("Authorization", prefix+p.apiKey)
	}
	// OpenRouter identification headers for rankings/analytics
	if p.siteURL != "" {
		httpReq.Header.Set("HTTP-Referer", p.siteURL)
	}
	if p.siteTitle != "" {
		httpReq.Header.Set("X-Title", p.siteTitle)
	}

	resp, err := p.client.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("%s: request failed: %w", p.name, err)
	}

	if resp.StatusCode != http.StatusOK {
		respBody, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		retryAfter := ParseRetryAfter(resp.Header.Get("Retry-After"))
		return nil, &HTTPError{
			Status:     resp.StatusCode,
			Body:       fmt.Sprintf("%s: %s", p.name, string(respBody)),
			RetryAfter: retryAfter,
		}
	}

	return resp.Body, nil
}

func (p *OpenAIProvider) parseResponse(resp *openAIResponse) *ChatResponse {
	result := &ChatResponse{FinishReason: "stop"}

	if len(resp.Choices) > 0 {
		msg := resp.Choices[0].Message
		result.Content = msg.Content
		result.Thinking = msg.ReasoningContent
		if result.Thinking == "" {
			result.Thinking = msg.Reasoning
		}
		result.FinishReason = resp.Choices[0].FinishReason

		for _, tc := range msg.ToolCalls {
			args := make(map[string]any)
			var parseErr string
			if err := json.Unmarshal([]byte(tc.Function.Arguments), &args); err != nil && tc.Function.Arguments != "" {
				slog.Warn("openai: failed to parse tool call arguments",
					"tool", tc.Function.Name, "raw_len", len(tc.Function.Arguments), "error", err)
				parseErr = fmt.Sprintf("malformed JSON (%d chars): %v", len(tc.Function.Arguments), err)
			}
			call := ToolCall{
				ID:         tc.ID,
				Name:       strings.TrimSpace(tc.Function.Name),
				Arguments:  args,
				ParseError: parseErr,
			}
			if tc.Function.ThoughtSignature != "" {
				call.Metadata = map[string]string{"thought_signature": tc.Function.ThoughtSignature}
			}
			result.ToolCalls = append(result.ToolCalls, call)
		}

		// Only override finish_reason when response wasn't truncated.
		// Preserve "length" so agent loop can detect truncation and retry.
		if len(result.ToolCalls) > 0 && result.FinishReason != "length" {
			result.FinishReason = "tool_calls"
		}

		// Decode images[] from the response message into ChatResponse.Images.
		// Each entry carries a data URL (data:<mime>;base64,<b64>).
		// Malformed entries are skipped with a warning to avoid crashing on partial responses.
		for _, img := range msg.Images {
			mimeType, b64Data, err := parseDataURL(img.ImageURL.URL)
			if err != nil {
				slog.Warn("openai: skipping malformed image data URL",
					"type", img.Type, "url_len", len(img.ImageURL.URL), "error", err)
				continue
			}
			result.Images = append(result.Images, ImageContent{
				MimeType: mimeType,
				Data:     b64Data,
			})
		}
	}

	if resp.Usage != nil {
		result.Usage = &Usage{
			PromptTokens:     resp.Usage.PromptTokens,
			CompletionTokens: resp.Usage.CompletionTokens,
			TotalTokens:      resp.Usage.TotalTokens,
		}
		if resp.Usage.PromptTokensDetails != nil {
			result.Usage.CacheReadTokens = resp.Usage.PromptTokensDetails.CachedTokens
		}
		if resp.Usage.CompletionTokensDetails != nil && resp.Usage.CompletionTokensDetails.ReasoningTokens > 0 {
			result.Usage.ThinkingTokens = resp.Usage.CompletionTokensDetails.ReasoningTokens
		}
	}

	return result
}

// maxTokensLimitRe matches "supports at most N completion tokens" from OpenAI 400 errors.
var maxTokensLimitRe = regexp.MustCompile(`supports at most (\d+) completion tokens`)

// clampMaxTokensFromError checks if an error is a 400 "max_tokens is too large" rejection.
// If so, it parses the model's stated limit, clamps the body's max_tokens/max_completion_tokens,
// and returns true so the caller can retry.
func clampMaxTokensFromError(err error, body map[string]any) bool {
	var httpErr *HTTPError
	if !errors.As(err, &httpErr) || httpErr.Status != http.StatusBadRequest {
		return false
	}
	if !strings.Contains(httpErr.Body, "max_tokens") || !strings.Contains(httpErr.Body, "too large") {
		return false
	}

	matches := maxTokensLimitRe.FindStringSubmatch(httpErr.Body)
	if len(matches) < 2 {
		return false
	}
	limit, parseErr := strconv.Atoi(matches[1])
	if parseErr != nil || limit <= 0 {
		return false
	}

	// Clamp whichever key is present
	if _, ok := body["max_completion_tokens"]; ok {
		body["max_completion_tokens"] = limit
	} else {
		body["max_tokens"] = limit
	}
	return true
}

// clampedLimit returns the clamped max_tokens or max_completion_tokens value for logging.
func clampedLimit(body map[string]any) any {
	if v, ok := body["max_completion_tokens"]; ok {
		return v
	}
	return body["max_tokens"]
}
