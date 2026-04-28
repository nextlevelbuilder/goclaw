package providers

// Wire types for the OpenAI Responses API (Codex flow).

type codexAPIResponse struct {
	ID     string            `json:"id"`
	Object string            `json:"object"`
	Model  string            `json:"model"`
	Output []codexItem       `json:"output"`
	Usage  *codexUsage       `json:"usage,omitempty"`
	Status string            `json:"status"`
	Error  *codexErrorDetail `json:"error,omitempty"` // set when status="failed"
}

type codexErrorDetail struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

type codexItem struct {
	ID           string         `json:"id"`
	Type         string         `json:"type"` // "message", "function_call", "reasoning", "image_generation_call"
	Role         string         `json:"role,omitempty"`
	Phase        string         `json:"phase,omitempty"` // gpt-5.3-codex: "commentary" or "final_answer"
	Content      []codexContent `json:"content,omitempty"`
	CallID       string         `json:"call_id,omitempty"`
	Name         string         `json:"name,omitempty"`
	Arguments    string         `json:"arguments,omitempty"`
	Summary      []codexSummary `json:"summary,omitempty"`
	OutputFormat string         `json:"output_format,omitempty"` // image_generation_call: "png", "jpeg", "webp"
	Result       string         `json:"result,omitempty"`        // image_generation_call: base64 final image
}

type codexContent struct {
	Type string `json:"type"` // "output_text"
	Text string `json:"text"`
}

type codexSummary struct {
	Type string `json:"type"`
	Text string `json:"text"`
}

type codexUsage struct {
	InputTokens         int                 `json:"input_tokens"`
	OutputTokens        int                 `json:"output_tokens"`
	TotalTokens         int                 `json:"total_tokens"`
	OutputTokensDetails *codexTokensDetails `json:"output_tokens_details,omitempty"`
}

type codexTokensDetails struct {
	ReasoningTokens int `json:"reasoning_tokens"`
}

// SSE streaming types

type codexSSEEvent struct {
	Type               string            `json:"type"`
	Delta              string            `json:"delta,omitempty"`
	Text               string            `json:"text,omitempty"`
	ItemID             string            `json:"item_id,omitempty"`
	OutputIndex        int               `json:"output_index,omitempty"`
	ContentIndex       int               `json:"content_index,omitempty"`
	Item               *codexItem        `json:"item,omitempty"`
	Part               *codexContentPart `json:"part,omitempty"`
	Response           *codexAPIResponse `json:"response,omitempty"`
	OutputFormat       string            `json:"output_format,omitempty"`        // response.image_generation_call.partial_image
	PartialImageB64    string            `json:"partial_image_b64,omitempty"`    // response.image_generation_call.partial_image
	PartialImageIndex  int               `json:"partial_image_index,omitempty"`  // response.image_generation_call.partial_image
}

type codexToolCallAcc struct {
	callID  string
	name    string
	rawArgs string
}

type codexContentPart struct {
	Type string `json:"type"`
	Text string `json:"text,omitempty"`
}
