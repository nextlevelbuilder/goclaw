package providers

// Qwen CLI JSON response types (internal).

type qwenJSONResponse struct {
	Type      string     `json:"type"`
	Subtype   string     `json:"subtype"`
	Result    string     `json:"result"`
	SessionID string     `json:"session_id"`
	Model     string     `json:"model"`
	Usage     *qwenUsage `json:"usage"`
	IsError   bool       `json:"is_error"`
}

type qwenUsage struct {
	InputTokens          int `json:"input_tokens"`
	OutputTokens         int `json:"output_tokens"`
	TotalTokens          int `json:"total_tokens"`
	CacheReadInputTokens int `json:"cache_read_input_tokens"`
}

type qwenStreamEvent struct {
	Type      string         `json:"type"`
	Subtype   string         `json:"subtype,omitempty"`
	UUID      string         `json:"uuid,omitempty"`
	SessionID string         `json:"session_id"`
	Message   *qwenStreamMsg `json:"message,omitempty"`
	Result    string         `json:"result,omitempty"`
	Model     string         `json:"model,omitempty"`
	Usage     *qwenUsage     `json:"usage,omitempty"`
	IsError   bool           `json:"is_error,omitempty"`
}

type qwenStreamMsg struct {
	ID         string             `json:"id"`
	Type       string             `json:"type"`
	Role       string             `json:"role"`
	Model      string             `json:"model"`
	Content    []qwenContentBlock `json:"content"`
	StopReason *string            `json:"stop_reason"`
	Usage      *qwenUsage         `json:"usage,omitempty"`
}

type qwenContentBlock struct {
	Type      string `json:"type"`
	Text      string `json:"text,omitempty"`
	Thinking  string `json:"thinking,omitempty"`
	Signature string `json:"signature,omitempty"`
}
