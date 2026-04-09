package providers

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
)

type gitHubCopilotAPIBaseSource interface {
	APIBase() string
}

const (
	gitHubCopilotUserAgent      = "GitHubCopilotChat/0.35.0"
	gitHubCopilotEditorVersion  = "vscode/1.107.0"
	gitHubCopilotPluginVersion  = "copilot-chat/0.35.0"
	gitHubCopilotIntegrationID  = "vscode-chat"
	defaultGitHubCopilotModel   = "gpt-5.4"
	defaultGitHubCopilotAPIBase = "https://api.individual.githubcopilot.com"
)

// GitHubCopilotProvider implements Provider for the GitHub Copilot Responses API.
// Initial support is intentionally scoped to GPT-family Copilot models that use the Responses transport.
type GitHubCopilotProvider struct {
	name         string
	apiBase      string
	defaultModel string
	client       *http.Client
	retryConfig  RetryConfig
	tokenSource  TokenSource
}

func NewGitHubCopilotProvider(name string, tokenSource TokenSource, apiBase, defaultModel string) *GitHubCopilotProvider {
	if apiBase == "" {
		apiBase = defaultGitHubCopilotAPIBase
	}
	if defaultModel == "" {
		defaultModel = defaultGitHubCopilotModel
	}
	return &GitHubCopilotProvider{
		name:         name,
		apiBase:      strings.TrimRight(apiBase, "/"),
		defaultModel: defaultModel,
		client:       &http.Client{Timeout: DefaultHTTPTimeout},
		retryConfig:  DefaultRetryConfig(),
		tokenSource:  tokenSource,
	}
}

func (p *GitHubCopilotProvider) Name() string           { return p.name }
func (p *GitHubCopilotProvider) DefaultModel() string   { return p.defaultModel }
func (p *GitHubCopilotProvider) SupportsThinking() bool { return true }

func (p *GitHubCopilotProvider) Chat(ctx context.Context, req ChatRequest) (*ChatResponse, error) {
	return p.ChatStream(ctx, req, nil)
}

func (p *GitHubCopilotProvider) ChatStream(ctx context.Context, req ChatRequest, onChunk func(StreamChunk)) (*ChatResponse, error) {
	body := p.buildRequestBody(req, true)
	respBody, err := RetryDo(ctx, p.retryConfig, func() (io.ReadCloser, error) {
		return p.doRequest(ctx, body)
	})
	if err != nil {
		return nil, err
	}
	defer respBody.Close()

	result := &ChatResponse{FinishReason: "stop"}
	toolCalls := make(map[string]*codexToolCallAcc)
	streamState := newCodexMessageStreamState()

	scanner := bufio.NewScanner(respBody)
	scanner.Buffer(make([]byte, 0, SSEScanBufInit), SSEScanBufMax)
	for scanner.Scan() {
		line := scanner.Text()
		if !strings.HasPrefix(line, "data:") {
			continue
		}
		data := strings.TrimPrefix(line, "data:")
		data = strings.TrimPrefix(data, " ")
		if data == "[DONE]" {
			break
		}

		var event codexSSEEvent
		if err := json.Unmarshal([]byte(data), &event); err != nil {
			continue
		}
		p.processSSEEvent(&event, result, toolCalls, streamState, onChunk)
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("%s: stream read error: %w", p.name, err)
	}
	for _, acc := range toolCalls {
		if acc.name == "" {
			continue
		}
		args := make(map[string]any)
		var parseErr string
		if err := json.Unmarshal([]byte(acc.rawArgs), &args); err != nil && acc.rawArgs != "" {
			parseErr = fmt.Sprintf("malformed JSON (%d chars): %v", len(acc.rawArgs), err)
		}
		result.ToolCalls = append(result.ToolCalls, ToolCall{ID: acc.callID, Name: acc.name, Arguments: args, ParseError: parseErr})
	}
	if len(result.ToolCalls) > 0 && result.FinishReason != "length" {
		result.FinishReason = "tool_calls"
	}
	if onChunk != nil {
		onChunk(StreamChunk{Done: true})
	}
	return result, nil
}

func (p *GitHubCopilotProvider) buildRequestBody(req ChatRequest, stream bool) map[string]any {
	model := req.Model
	if model == "" {
		model = p.defaultModel
	}
	var instructions string
	var input []any
	for _, m := range req.Messages {
		switch m.Role {
		case "system":
			if instructions == "" {
				instructions = m.Content
			} else {
				instructions += "\n\n" + m.Content
			}
		case "user":
			if len(m.Images) > 0 {
				var parts []map[string]any
				for _, img := range m.Images {
					parts = append(parts, map[string]any{"type": "input_image", "image_url": fmt.Sprintf("data:%s;base64,%s", img.MimeType, img.Data)})
				}
				if m.Content != "" {
					parts = append(parts, map[string]any{"type": "input_text", "text": m.Content})
				}
				input = append(input, map[string]any{"role": "user", "content": parts})
			} else {
				input = append(input, map[string]any{"role": "user", "content": m.Content})
			}
		case "assistant":
			if len(m.ToolCalls) > 0 {
				for _, tc := range m.ToolCalls {
					argsJSON, _ := json.Marshal(tc.Arguments)
					callID := toFcID(tc.ID)
					input = append(input, map[string]any{"type": "function_call", "id": callID, "call_id": callID, "name": tc.Name, "arguments": string(argsJSON)})
				}
			}
			if m.Content != "" {
				item := map[string]any{"type": "message", "role": "assistant", "content": []map[string]any{{"type": "output_text", "text": m.Content}}}
				if m.Phase != "" {
					item["phase"] = m.Phase
				}
				input = append(input, item)
			}
		case "tool":
			input = append(input, map[string]any{"type": "function_call_output", "call_id": toFcID(m.ToolCallID), "output": m.Content})
		}
	}
	if instructions == "" {
		instructions = "You are a helpful assistant."
	}
	body := map[string]any{"model": model, "input": input, "stream": stream, "store": false, "instructions": instructions}
	if len(req.Tools) > 0 {
		var tools []map[string]any
		for _, t := range req.Tools {
			tools = append(tools, map[string]any{"type": "function", "name": t.Function.Name, "description": t.Function.Description, "parameters": NormalizeSchema("github_copilot", t.Function.Parameters)})
		}
		body["tools"] = tools
	}
	if level, ok := req.Options[OptThinkingLevel].(string); ok && level != "" && level != "off" {
		body["reasoning"] = map[string]any{"effort": level}
	}
	return body
}

func (p *GitHubCopilotProvider) doRequest(ctx context.Context, body any) (io.ReadCloser, error) {
	data, err := json.Marshal(body)
	if err != nil {
		return nil, fmt.Errorf("%s: marshal request: %w", p.name, err)
	}
	apiBase := p.apiBase
	if baseSource, ok := p.tokenSource.(gitHubCopilotAPIBaseSource); ok {
		if dynamicBase := strings.TrimRight(baseSource.APIBase(), "/"); dynamicBase != "" {
			apiBase = dynamicBase
		}
	}
	target := apiBase + "/responses"
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, target, bytes.NewReader(data))
	if err != nil {
		return nil, fmt.Errorf("%s: create request: %w", p.name, err)
	}
	token, err := p.tokenSource.Token()
	if err != nil {
		return nil, fmt.Errorf("%s: get auth token: %w", p.name, err)
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Authorization", "Bearer "+token)
	httpReq.Header.Set("OpenAI-Beta", "responses=v1")
	httpReq.Header.Set("User-Agent", gitHubCopilotUserAgent)
	httpReq.Header.Set("Editor-Version", gitHubCopilotEditorVersion)
	httpReq.Header.Set("Editor-Plugin-Version", gitHubCopilotPluginVersion)
	httpReq.Header.Set("Copilot-Integration-Id", gitHubCopilotIntegrationID)
	for key, value := range buildGitHubCopilotDynamicHeaders(body) {
		httpReq.Header.Set(key, value)
	}
	resp, err := p.client.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("%s: request failed: %w", p.name, err)
	}
	if resp.StatusCode != http.StatusOK {
		respBody, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
		resp.Body.Close()
		retryAfter := ParseRetryAfter(resp.Header.Get("Retry-After"))
		return nil, &HTTPError{Status: resp.StatusCode, Body: fmt.Sprintf("%s: %s", p.name, string(respBody)), RetryAfter: retryAfter}
	}
	return resp.Body, nil
}

func buildGitHubCopilotDynamicHeaders(body any) map[string]string {
	headers := map[string]string{
		"Openai-Intent": "conversation-edits",
		"X-Initiator":   "user",
	}
	payload, ok := body.(map[string]any)
	if !ok {
		return headers
	}
	input, _ := payload["input"].([]any)
	if len(input) > 0 {
		if last, ok := input[len(input)-1].(map[string]any); ok {
			if role, _ := last["role"].(string); role != "" && role != "user" {
				headers["X-Initiator"] = "agent"
			}
		}
	}
	for _, item := range input {
		msg, ok := item.(map[string]any)
		if !ok {
			continue
		}
		content := msg["content"]
		parts, ok := content.([]map[string]any)
		if ok {
			for _, part := range parts {
				if part["type"] == "input_image" {
					headers["Copilot-Vision-Request"] = "true"
					return headers
				}
			}
		}
	}
	return headers
}

func (p *GitHubCopilotProvider) processSSEEvent(event *codexSSEEvent, result *ChatResponse, toolCalls map[string]*codexToolCallAcc, streamState *codexMessageStreamState, onChunk func(StreamChunk)) {
	switch event.Type {
	case "response.output_item.added":
		if event.Item != nil {
			streamState.registerMessageItem(event.ItemID, event.OutputIndex, event.Item)
		}
	case "response.output_text.delta":
		streamState.recordTextDelta(event.ItemID, event.OutputIndex, event.ContentIndex, event.Delta, result, onChunk)
	case "response.output_text.done":
		streamState.recordFinalText(event.ItemID, event.OutputIndex, event.ContentIndex, event.Text, result, onChunk)
	case "response.content_part.done":
		if event.Part != nil && event.Part.Type == "output_text" {
			streamState.recordFinalText(event.ItemID, event.OutputIndex, event.ContentIndex, event.Part.Text, result, onChunk)
		}
	case "response.function_call_arguments.delta":
		if event.ItemID != "" {
			acc := toolCalls[event.ItemID]
			if acc == nil {
				acc = &codexToolCallAcc{}
				toolCalls[event.ItemID] = acc
			}
			acc.rawArgs += event.Delta
		}
	case "response.output_item.done":
		if event.Item != nil {
			switch event.Item.Type {
			case "message":
				streamState.registerMessageItem(event.ItemID, event.OutputIndex, event.Item)
				streamState.flushMessage(codexEventItemKey(event.ItemID, event.Item), result, onChunk)
				streamState.updateResultPhase(result)
			case "function_call":
				acc := toolCalls[event.Item.ID]
				if acc == nil {
					acc = &codexToolCallAcc{}
				}
				acc.callID = event.Item.CallID
				acc.name = event.Item.Name
				if event.Item.Arguments != "" {
					acc.rawArgs = event.Item.Arguments
				}
				toolCalls[event.Item.ID] = acc
			case "reasoning":
				for _, s := range event.Item.Summary {
					if s.Text != "" {
						result.Thinking += s.Text
						if onChunk != nil {
							onChunk(StreamChunk{Thinking: s.Text})
						}
					}
				}
			}
		}
	case "response.completed", "response.incomplete", "response.failed":
		if event.Response != nil {
			if result.Content == "" {
				streamState.ingestCompletedResponse(event.Response)
				streamState.flushCompletedResponse(result, onChunk)
				streamState.updateResultPhase(result)
			}
			if event.Response.Usage != nil {
				u := event.Response.Usage
				result.Usage = &Usage{PromptTokens: u.InputTokens, CompletionTokens: u.OutputTokens, TotalTokens: u.TotalTokens}
				if u.OutputTokensDetails != nil {
					result.Usage.ThinkingTokens = u.OutputTokensDetails.ReasoningTokens
				}
			}
			if event.Response.Status == "incomplete" {
				result.FinishReason = "length"
			}
		}
	}
}