package providers

// cursorStreamEvent is a single line from Cursor's `--output-format stream-json`.
// Mirrors cliStreamEvent but adds SessionID for server-assigned chat ID tracking.
type cursorStreamEvent struct {
	Type      string        `json:"type"`                 // "assistant", "result", "system", "tool_call"
	Subtype   string        `json:"subtype,omitempty"`    // "success", "error"
	Message   *cliStreamMsg `json:"message,omitempty"`    // for type="assistant" (reuse shared type)
	Result    string        `json:"result,omitempty"`     // for type="result"
	SessionID string        `json:"session_id,omitempty"` // server-assigned chat ID on result event
	Model     string        `json:"model,omitempty"`
	Usage     *cliUsage     `json:"usage,omitempty"` // reuse shared type
}

// extractCursorChatID returns the server-assigned chat ID from a result event.
// Returns "" if no chat ID is present — stateless fallback applies.
func extractCursorChatID(ev *cursorStreamEvent) string {
	if ev.Type != "result" {
		return ""
	}
	return ev.SessionID
}
