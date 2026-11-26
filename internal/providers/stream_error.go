package providers

import "encoding/json"

// streamErrorMessage describes an "error" value found in a streamed event.
// It returns "" when the value is absent or empty (some gateways send
// "error": null on every chunk).
func streamErrorMessage(raw json.RawMessage) string {
	switch string(raw) {
	case "", "null", "false", `""`, "{}":
		return ""
	}
	var text string
	if json.Unmarshal(raw, &text) == nil {
		return text
	}
	var obj struct {
		Message string `json:"message"`
	}
	if json.Unmarshal(raw, &obj) == nil && obj.Message != "" {
		return obj.Message
	}
	return string(raw)
}
