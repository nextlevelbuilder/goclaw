package providers

// OpenAI API response types (internal)

type openAIResponse struct {
	Choices []openAIChoice `json:"choices"`
	Usage   *openAIUsage   `json:"usage,omitempty"`
}

type openAIChoice struct {
	Message      openAIMessage `json:"message"`
	FinishReason string        `json:"finish_reason"`
}

type openAIMessage struct {
	Role             string            `json:"role"`
	Content          string            `json:"content"`
	ReasoningContent string            `json:"reasoning_content,omitempty"`
	Reasoning        string            `json:"reasoning,omitempty"` // Ollama alias for reasoning_content
	ToolCalls        []openAIToolCall  `json:"tool_calls,omitempty"`
	Images           []openAIImagePart `json:"images,omitempty"`
}

// openAIImagePart represents an image entry in the images[] array returned by
// OpenAI-compat endpoints that generate images (e.g. gpt-image-1 via chat completions).
type openAIImagePart struct {
	Type     string               `json:"type"`
	ImageURL openAIImageURLObject `json:"image_url"`
}

// openAIImageURLObject holds the data URL (data:<mime>;base64,<b64>) for an image.
type openAIImageURLObject struct {
	URL string `json:"url"`
}

type openAIToolCall struct {
	ID       string             `json:"id"`
	Type     string             `json:"type"`
	Function openAIFunctionCall `json:"function"`
}

type openAIFunctionCall struct {
	Name             string `json:"name"`
	Arguments        string `json:"arguments"`
	ThoughtSignature string `json:"thought_signature,omitempty"` // Gemini 2.5/3: must echo back
}

type openAIUsage struct {
	PromptTokens            int                      `json:"prompt_tokens"`
	CompletionTokens        int                      `json:"completion_tokens"`
	TotalTokens             int                      `json:"total_tokens"`
	Cost                    float64                  `json:"cost,omitempty"`
	PromptTokensDetails     *openAIPromptDetails     `json:"prompt_tokens_details,omitempty"`
	CompletionTokensDetails *openAICompletionDetails `json:"completion_tokens_details,omitempty"`
	ServerToolUse           *openAIServerToolUse     `json:"server_tool_use,omitempty"`
}

type openAIPromptDetails struct {
	CachedTokens     int `json:"cached_tokens"`
	CacheWriteTokens int `json:"cache_write_tokens,omitempty"`
}

type openAICompletionDetails struct {
	ReasoningTokens int `json:"reasoning_tokens"`
}

type openAIServerToolUse struct {
	WebSearchRequests int `json:"web_search_requests,omitempty"`
}

// Streaming types

type openAIStreamChunk struct {
	Choices []openAIStreamChoice `json:"choices"`
	Usage   *openAIUsage         `json:"usage,omitempty"`
}

type openAIStreamChoice struct {
	Delta        openAIStreamDelta `json:"delta"`
	FinishReason string            `json:"finish_reason,omitempty"`
}

type openAIStreamDelta struct {
	Content          string                 `json:"content,omitempty"`
	ReasoningContent string                 `json:"reasoning_content,omitempty"`
	Reasoning        string                 `json:"reasoning,omitempty"` // Ollama alias for reasoning_content
	ToolCalls        []openAIStreamToolCall `json:"tool_calls,omitempty"`
	Images           []openAIImagePart      `json:"images,omitempty"`
}

type openAIStreamToolCall struct {
	Index    int                `json:"index"`
	ID       string             `json:"id,omitempty"`
	Function openAIFunctionCall `json:"function"`
}

// toolCallAccumulator extends ToolCall with temporary fields for accumulating
// streamed arguments and thought_signature during SSE streaming.
type toolCallAccumulator struct {
	ToolCall
	rawArgs    string
	thoughtSig string
}
