package acp

// ACP protocol types — client-side subset for GoClaw as ACP client.
// Covers: initialize, session lifecycle, content blocks, and agent→client requests.

// --- Client → Agent Requests ---

// InitializeRequest starts the ACP handshake.
type InitializeRequest struct {
	ClientInfo   ClientInfo `json:"clientInfo"`
	Capabilities ClientCaps `json:"capabilities"`
}

// ClientInfo identifies the ACP client.
type ClientInfo struct {
	Name    string `json:"name"`
	Version string `json:"version"`
}

// ClientCaps declares what the client can handle (fs, terminal, etc.).
type ClientCaps struct {
	Fs       *FsCaps       `json:"fs,omitempty"`
	Terminal *TerminalCaps `json:"terminal,omitempty"`
}

// FsCaps declares filesystem capabilities.
type FsCaps struct {
	ReadTextFile  bool `json:"readTextFile"`
	WriteTextFile bool `json:"writeTextFile"`
}

// TerminalCaps declares terminal capabilities.
type TerminalCaps struct {
	Enabled bool `json:"enabled"`
}

// InitializeResponse carries the agent's identity and capabilities.
type InitializeResponse struct {
	AgentInfo    AgentInfo `json:"agentInfo"`
	Capabilities AgentCaps `json:"capabilities"`
}

// AgentInfo identifies the ACP agent.
type AgentInfo struct {
	Name    string `json:"name"`
	Version string `json:"version"`
}

// AgentCaps declares agent capabilities.
type AgentCaps struct {
	LoadSession         bool         `json:"loadSession"`
	PromptCapabilities  *PromptCaps  `json:"promptCapabilities,omitempty"`
	SessionCapabilities *SessionCaps `json:"sessionCapabilities,omitempty"`
}

// PromptCaps describes what content types the agent accepts.
type PromptCaps struct {
	Audio           bool `json:"audio"`
	Image           bool `json:"image"`
	EmbeddedContext bool `json:"embeddedContext"`
}

// SessionCaps describes session-level capabilities.
type SessionCaps struct{}

// --- Session Methods ---

// NewSessionRequest creates a new ACP session.
type NewSessionRequest struct{}

// NewSessionResponse carries the new session ID.
type NewSessionResponse struct {
	SessionID string `json:"sessionId"`
}

// PromptRequest sends user content to the agent.
type PromptRequest struct {
	SessionID string         `json:"sessionId"`
	Content   []ContentBlock `json:"content"`
}

// PromptResponse is the final response after the agent completes.
type PromptResponse struct {
	StopReason string `json:"stopReason,omitempty"`
}

// CancelNotification requests cooperative cancellation.
type CancelNotification struct {
	SessionID string `json:"sessionId"`
}

// --- Content Blocks ---

// ContentBlock represents a piece of content (text, image, audio).
type ContentBlock struct {
	Type     string `json:"type"` // "text", "image", "audio"
	Text     string `json:"text,omitempty"`
	Data     string `json:"data,omitempty"` // base64 for image/audio
	MimeType string `json:"mimeType,omitempty"`
}

// --- Agent → Client Notifications ---

// SessionUpdate carries incremental updates during prompt execution.
type SessionUpdate struct {
	Kind       string          `json:"kind"`                 // "message", "toolCall", "plan"
	StopReason string          `json:"stopReason,omitempty"` // "endTurn", "cancelled"
	Message    *MessageUpdate  `json:"message,omitempty"`
	ToolCall   *ToolCallUpdate `json:"toolCall,omitempty"`
}

// MessageUpdate carries an assistant text delta.
type MessageUpdate struct {
	Role    string         `json:"role"`
	Content []ContentBlock `json:"content"`
}

// ToolCallUpdate carries tool call progress.
type ToolCallUpdate struct {
	ID      string         `json:"id"`
	Name    string         `json:"name"`
	Status  string         `json:"status"` // "running", "completed"
	Content []ContentBlock `json:"content,omitempty"`
}

// --- Agent → Client Requests (fs/terminal/permission) ---

type ReadTextFileRequest struct {
	Path string `json:"path"`
}

type ReadTextFileResponse struct {
	Content string `json:"content"`
}

type WriteTextFileRequest struct {
	Path    string `json:"path"`
	Content string `json:"content"`
}

type WriteTextFileResponse struct{}

type CreateTerminalRequest struct {
	Command string   `json:"command"`
	Args    []string `json:"args,omitempty"`
	Cwd     string   `json:"cwd,omitempty"`
}

type CreateTerminalResponse struct {
	TerminalID string `json:"terminalId"`
}

type TerminalOutputRequest struct {
	TerminalID string `json:"terminalId"`
}

type TerminalOutputResponse struct {
	Output     string `json:"output"`
	ExitStatus *int   `json:"exitStatus,omitempty"`
}

type ReleaseTerminalRequest struct {
	TerminalID string `json:"terminalId"`
}

type ReleaseTerminalResponse struct{}

type WaitForTerminalExitRequest struct {
	TerminalID string `json:"terminalId"`
}

type WaitForTerminalExitResponse struct {
	ExitStatus int `json:"exitStatus"`
}

type KillTerminalRequest struct {
	TerminalID string `json:"terminalId"`
}

type KillTerminalResponse struct{}

type RequestPermissionRequest struct {
	ToolName    string `json:"toolName"`
	Description string `json:"description"`
}

type RequestPermissionResponse struct {
	Outcome string `json:"outcome"` // "approved", "denied", "cancelled"
}
