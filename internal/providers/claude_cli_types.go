package providers

// Claude CLI JSON response types (internal).
// These map to the output of `claude -p --output-format json/stream-json`.

// cliJSONResponse is the final result envelope from `--output-format json`.
type cliJSONResponse struct {
	Type      string    `json:"type"`       // "result"
	Subtype   string    `json:"subtype"`    // "success", "error"
	Result    string    `json:"result"`     // text response
	SessionID string    `json:"session_id"` // CLI session UUID
	Model     string    `json:"model"`
	CostUSD   float64   `json:"cost_usd"`
	Usage     *cliUsage `json:"usage"`
}

// cliUsage maps Claude CLI usage counters.
type cliUsage struct {
	InputTokens  int `json:"input_tokens"`
	OutputTokens int `json:"output_tokens"`
}

// cliStreamEvent is a single line from `--output-format stream-json`.
type cliStreamEvent struct {
	Type    string        `json:"type"`              // "assistant", "result", "system"
	Subtype string        `json:"subtype,omitempty"` // "success", "error"
	Message *cliStreamMsg `json:"message,omitempty"` // for type="assistant"
	Result  string        `json:"result,omitempty"`  // for type="result"
	Model   string        `json:"model,omitempty"`
	CostUSD float64       `json:"cost_usd,omitempty"`
	Usage   *cliUsage     `json:"usage,omitempty"`
	IsError bool          `json:"is_error,omitempty"` // true when subtype="error"
	Error   string        `json:"error,omitempty"`    // error message (may be set when result is empty)
}

// cliStreamMsg wraps content blocks inside an assistant message event.
type cliStreamMsg struct {
	Content []cliContentBlock `json:"content"`
}

// cliContentBlock is a single content block (text, thinking, tool_use).
type cliContentBlock struct {
	Type     string `json:"type"`               // "text", "thinking", "tool_use", "tool_result"
	Text     string `json:"text,omitempty"`     // for type="text"
	Thinking string `json:"thinking,omitempty"` // for type="thinking"
}
