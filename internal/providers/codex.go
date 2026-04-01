package providers

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"sort"
	"strings"
)

type CodexRoutingDefaults struct {
	Strategy           string
	ExtraProviderNames []string
}

// CodexProvider implements Provider for the OpenAI Responses API,
// used with ChatGPT subscription via OAuth (Codex flow).
// Wire format: POST /codex/responses on chatgpt.com backend.
type CodexProvider struct {
	name            string
	apiBase         string // e.g. "https://api.openai.com/v1" or "https://chatgpt.com/backend-api"
	defaultModel    string
	client          *http.Client
	retryConfig     RetryConfig
	tokenSource     TokenSource
	routingDefaults *CodexRoutingDefaults
}

// NewCodexProvider creates a provider for the OpenAI Responses API with OAuth token.
func NewCodexProvider(name string, tokenSource TokenSource, apiBase, defaultModel string) *CodexProvider {
	if apiBase == "" {
		apiBase = "https://chatgpt.com/backend-api"
	}
	apiBase = strings.TrimRight(apiBase, "/")

	if defaultModel == "" {
		defaultModel = "gpt-5.4"
	}

	return &CodexProvider{
		name:         name,
		apiBase:      apiBase,
		defaultModel: defaultModel,
		client:       &http.Client{Timeout: DefaultHTTPTimeout},
		retryConfig:  DefaultRetryConfig(),
		tokenSource:  tokenSource,
	}
}

func (p *CodexProvider) Name() string           { return p.name }
func (p *CodexProvider) DefaultModel() string   { return p.defaultModel }
func (p *CodexProvider) SupportsThinking() bool { return true }
func (p *CodexProvider) WithRoutingDefaults(strategy string, extraProviderNames []string) *CodexProvider {
	p.routingDefaults = &CodexRoutingDefaults{
		Strategy:           strategy,
		ExtraProviderNames: append([]string(nil), extraProviderNames...),
	}
	return p
}
func (p *CodexProvider) RoutingDefaults() *CodexRoutingDefaults {
	if p.routingDefaults == nil {
		return nil
	}
	return &CodexRoutingDefaults{
		Strategy:           p.routingDefaults.Strategy,
		ExtraProviderNames: append([]string(nil), p.routingDefaults.ExtraProviderNames...),
	}
}
func (p *CodexProvider) RouteEligibility(ctx context.Context) RouteEligibility {
	if aware, ok := p.tokenSource.(RouteEligibilityAware); ok {
		return aware.RouteEligibility(ctx)
	}
	return RouteEligibility{Class: RouteEligibilityHealthy}
}

func (p *CodexProvider) Chat(ctx context.Context, req ChatRequest) (*ChatResponse, error) {
	// Codex Responses API requires stream=true; delegate to ChatStream with no chunk handler.
	return p.ChatStream(ctx, req, nil)
}

func (p *CodexProvider) ChatStream(ctx context.Context, req ChatRequest, onChunk func(StreamChunk)) (*ChatResponse, error) {
	body := p.buildRequestBody(req, true)

	respBody, err := RetryDo(ctx, p.retryConfig, func() (io.ReadCloser, error) {
		return p.doRequest(ctx, body)
	})
	if err != nil {
		return nil, err
	}
	defer respBody.Close()

	result := &ChatResponse{FinishReason: "stop"}
	toolCalls := make(map[string]*codexToolCallAcc) // keyed by item_id
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

	// Build tool calls from accumulators
	for _, acc := range toolCalls {
		if acc.name == "" {
			continue
		}
		args := make(map[string]any)
		var parseErr string
		if err := json.Unmarshal([]byte(acc.rawArgs), &args); err != nil && acc.rawArgs != "" {
			parseErr = fmt.Sprintf("malformed JSON (%d chars): %v", len(acc.rawArgs), err)
		}
		result.ToolCalls = append(result.ToolCalls, ToolCall{
			ID:         acc.callID,
			Name:       acc.name,
			Arguments:  args,
			ParseError: parseErr,
		})
	}

	// Only override finish_reason when response wasn't truncated.
	// Preserve "length" so agent loop can detect truncation and retry.
	if len(result.ToolCalls) > 0 && result.FinishReason != "length" {
		result.FinishReason = "tool_calls"
	}

	if onChunk != nil {
		onChunk(StreamChunk{Done: true})
	}

	return result, nil
}

// processSSEEvent handles a single SSE event during streaming.
func (p *CodexProvider) processSSEEvent(event *codexSSEEvent, result *ChatResponse, toolCalls map[string]*codexToolCallAcc, streamState *codexMessageStreamState, onChunk func(StreamChunk)) {
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
				result.Usage = &Usage{
					PromptTokens:     u.InputTokens,
					CompletionTokens: u.OutputTokens,
					TotalTokens:      u.TotalTokens,
				}
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

type codexMessageStreamState struct {
	messages map[string]*codexMessageState
}

type codexMessageState struct {
	id          string
	outputIndex int
	phase       string
	parts       map[int]*codexTextPartState
}

type codexTextPartState struct {
	text         string
	emittedBytes int
}

func newCodexMessageStreamState() *codexMessageStreamState {
	return &codexMessageStreamState{messages: make(map[string]*codexMessageState)}
}

func (s *codexMessageStreamState) registerMessageItem(itemID string, outputIndex int, item *codexItem) {
	if item == nil || item.Type != "message" {
		return
	}
	msg := s.ensureMessage(itemID, item.ID, outputIndex)
	if item.Phase != "" {
		msg.phase = item.Phase
	}
	if msg.outputIndex == 0 && outputIndex != 0 {
		msg.outputIndex = outputIndex
	}
	for idx, part := range item.Content {
		if part.Type != "output_text" || part.Text == "" {
			continue
		}
		textPart := msg.ensurePart(idx)
		if part.Text != "" {
			textPart.text = part.Text
		}
	}
}

func (s *codexMessageStreamState) recordTextDelta(itemID string, outputIndex, contentIndex int, delta string, result *ChatResponse, onChunk func(StreamChunk)) {
	if delta == "" {
		return
	}
	msg := s.ensureMessage(itemID, "", outputIndex)
	part := msg.ensurePart(contentIndex)
	part.text += delta
	if !shouldEmitCodexPhase(msg.phase) {
		return
	}
	msg.flushContiguous(result, onChunk)
}

func (s *codexMessageStreamState) recordFinalText(itemID string, outputIndex, contentIndex int, text string, result *ChatResponse, onChunk func(StreamChunk)) {
	if text == "" {
		return
	}
	msg := s.ensureMessage(itemID, "", outputIndex)
	part := msg.ensurePart(contentIndex)
	prev := part.text
	part.text = text
	if !shouldEmitCodexPhase(msg.phase) {
		return
	}
	part.reconcileCompleted(prev)
	msg.flushContiguous(result, onChunk)
}

func (s *codexMessageStreamState) flushMessage(itemID string, result *ChatResponse, onChunk func(StreamChunk)) {
	msg, ok := s.messages[itemID]
	if !ok || !shouldEmitCodexPhase(msg.phase) {
		return
	}
	msg.flushContiguous(result, onChunk)
}

func (s *codexMessageStreamState) ingestCompletedResponse(resp *codexAPIResponse) {
	if resp == nil {
		return
	}
	for i := range resp.Output {
		item := &resp.Output[i]
		if item.Type != "message" {
			continue
		}
		s.registerMessageItem(item.ID, i, item)
	}
}

func (s *codexMessageStreamState) flushCompletedResponse(result *ChatResponse, onChunk func(StreamChunk)) {
	ordered := s.preferredMessages()
	for _, msg := range ordered {
		msg.flushContiguous(result, onChunk)
	}
}

func (s *codexMessageStreamState) updateResultPhase(result *ChatResponse) {
	if result == nil || result.Phase == "final_answer" {
		return
	}
	for _, msg := range s.messages {
		if msg.phase == "final_answer" {
			result.Phase = "final_answer"
			return
		}
	}
}

func (s *codexMessageStreamState) preferredMessages() []*codexMessageState {
	if len(s.messages) == 0 {
		return nil
	}
	ordered := make([]*codexMessageState, 0, len(s.messages))
	for _, msg := range s.messages {
		if msg.phase == "final_answer" {
			ordered = append(ordered, msg)
		}
	}
	if len(ordered) == 0 {
		for _, msg := range s.messages {
			if msg.phase != "commentary" {
				ordered = append(ordered, msg)
			}
		}
	}
	sort.SliceStable(ordered, func(i, j int) bool {
		return ordered[i].outputIndex < ordered[j].outputIndex
	})
	return ordered
}

func codexEventItemKey(eventItemID string, item *codexItem) string {
	if eventItemID != "" {
		return eventItemID
	}
	if item != nil {
		return item.ID
	}
	return ""
}

func (s *codexMessageStreamState) ensureMessage(itemID, fallbackID string, outputIndex int) *codexMessageState {
	key := itemID
	if key == "" {
		key = fallbackID
	}
	if key == "" {
		key = fmt.Sprintf("output:%d", outputIndex)
	}
	if msg, ok := s.messages[key]; ok {
		if msg.outputIndex == 0 && outputIndex != 0 {
			msg.outputIndex = outputIndex
		}
		return msg
	}
	msg := &codexMessageState{
		id:          key,
		outputIndex: outputIndex,
		parts:       make(map[int]*codexTextPartState),
	}
	s.messages[key] = msg
	return msg
}

func (m *codexMessageState) ensurePart(contentIndex int) *codexTextPartState {
	if part, ok := m.parts[contentIndex]; ok {
		return part
	}
	part := &codexTextPartState{}
	m.parts[contentIndex] = part
	return part
}

func (m *codexMessageState) flushContiguous(result *ChatResponse, onChunk func(StreamChunk)) {
	for nextIndex := 0; ; nextIndex++ {
		part, ok := m.parts[nextIndex]
		if !ok {
			return
		}
		part.emitMissing(result, onChunk)
	}
}

func (p *codexTextPartState) emitMissing(result *ChatResponse, onChunk func(StreamChunk)) {
	if p.text == "" || p.emittedBytes >= len(p.text) {
		return
	}
	suffix := p.text[p.emittedBytes:]
	appendCodexContent(result, suffix, onChunk)
	p.emittedBytes = len(p.text)
}

func (p *codexTextPartState) reconcileCompleted(previous string) {
	if p.text == "" {
		return
	}
	if len(previous) >= p.emittedBytes && strings.HasPrefix(p.text, previous[:p.emittedBytes]) {
		return
	}
	if p.emittedBytes > len(p.text) {
		p.emittedBytes = len(p.text)
	}
}

func shouldEmitCodexPhase(phase string) bool {
	return phase == "" || phase == "final_answer"
}

func appendCodexContent(result *ChatResponse, text string, onChunk func(StreamChunk)) {
	if text == "" {
		return
	}
	result.Content += text
	if onChunk != nil {
		onChunk(StreamChunk{Content: text})
	}
}
