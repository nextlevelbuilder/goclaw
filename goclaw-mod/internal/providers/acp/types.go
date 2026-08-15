package acp

import "encoding/json"

// --- Client -> Agent Requests ---

type InitializeRequest struct {
	ProtocolVersion int        `json:"protocolVersion"`
	ClientInfo      ClientInfo `json:"clientInfo"`
	Capabilities    ClientCaps `json:"clientCapabilities"`
}

type ClientInfo struct {
	Name    string `json:"name"`
	Version string `json:"version"`
}

type ClientCaps struct {
	Fs       *FsCaps       `json:"fs,omitempty"`
	Terminal *TerminalCaps `json:"terminal,omitempty"`
}

type FsCaps struct {
	ReadTextFile  bool `json:"readTextFile"`
	WriteTextFile bool `json:"writeTextFile"`
}

type TerminalCaps struct {
	Enabled bool `json:"enabled"`
}

type InitializeResponse struct {
	AgentInfo    AgentInfo `json:"agentInfo"`
	Capabilities AgentCaps `json:"agentCapabilities"`
}

type AgentInfo struct {
	Name    string `json:"name"`
	Version string `json:"version"`
}

type AgentCaps struct {
	LoadSession         bool         `json:"loadSession"`
	PromptCapabilities  *PromptCaps  `json:"promptCapabilities,omitempty"`
	SessionCapabilities *SessionCaps `json:"sessionCapabilities,omitempty"`
	MCPCapabilities     *MCPCaps     `json:"mcpCapabilities,omitempty"`
}

type PromptCaps struct {
	Audio           bool `json:"audio"`
	Image           bool `json:"image"`
	EmbeddedContext bool `json:"embeddedContext"`
}

type SessionCaps struct{}

type MCPCaps struct {
	HTTP bool `json:"http"`
	SSE  bool `json:"sse"`
}

// --- Session Methods ---

type NewSessionRequest struct {
	Cwd        string   `json:"cwd"`
	McpServers []string `json:"mcpServers"`
}

type NewSessionResponse struct {
	SessionID string `json:"sessionId"`
}

type LoadSessionRequest struct {
	SessionID  string   `json:"sessionId"`
	Cwd        string   `json:"cwd,omitempty"`
	McpServers []string `json:"mcpServers"`
}

type LoadSessionResponse struct {
	SessionID string `json:"sessionId"`
}

type PromptRequest struct {
	SessionID string         `json:"sessionId"`
	Prompt    []ContentBlock `json:"prompt"`
}

type PromptResponse struct {
	StopReason string `json:"stopReason,omitempty"`
}

type CancelNotification struct {
	SessionID string `json:"sessionId"`
}

// --- Content Blocks ---

type ContentBlock struct {
	Type     string `json:"type"` // "text", "image", "audio"
	Text     string `json:"text,omitempty"`
	Data     string `json:"data,omitempty"`
	MimeType string `json:"mimeType,omitempty"`
}

// --- Agent -> Client Notifications ---

type SessionUpdate struct {
	SessionID  string `json:"sessionId"`
	StopReason string `json:"stopReason,omitempty"`
	
	Kind     string          `json:"kind,omitempty"`
	Message  *MessageUpdate  `json:"message,omitempty"`
	ToolCall *ToolCallUpdate `json:"toolCall,omitempty"`

	Update struct {
		SessionUpdate string `json:"sessionUpdate"`
		
		Content json.RawMessage `json:"content,omitempty"`

		Entries []struct {
			Content  string `json:"content"`
			Priority string `json:"priority"`
			Status   string `json:"status"`
		} `json:"entries,omitempty"`

		ToolCallID string `json:"toolCallId,omitempty"`
		Title      string `json:"title,omitempty"`
		Kind       string `json:"kind,omitempty"`
		Status     string `json:"status,omitempty"`
	} `json:"update"`
}

type MessageUpdate struct {
	Role    string         `json:"role"`
	Content []ContentBlock `json:"content"`
}

type ToolCallUpdate struct {
	ID      string         `json:"id"`
	Name    string         `json:"name"`
	Status  string         `json:"status"`
	Content []ContentBlock `json:"content,omitempty"`
}

// --- Agent -> Client Requests ---

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
	Outcome string `json:"outcome"` // "proceed_always", "approved", "denied"
}
