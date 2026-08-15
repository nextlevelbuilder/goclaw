package protocol

// MCPOAuthCompletePayload is the payload for EventMCPOAuthComplete.
// Fired after the OAuth callback is processed (success or error).
type MCPOAuthCompletePayload struct {
	ServerID         string `json:"serverId"`
	UserID           string `json:"userId"`           // per-user token's userID; empty = global token
	InitiatingUserID string `json:"initiatingUserId"` // admin who started the flow
	Status           string `json:"status"`           // "success" | "error"
	Error            string `json:"error,omitempty"`
}
