package protocol

// DelegationEventPayload is the typed payload for delegation lifecycle events
// (started, completed, failed, cancelled).
type DelegationEventPayload struct {
	DelegationID      string `json:"delegation_id"`
	SourceAgentID     string `json:"source_agent_id"`
	SourceAgentKey    string `json:"source_agent_key"`
	SourceDisplayName string `json:"source_display_name,omitempty"`
	TargetAgentID     string `json:"target_agent_id"`
	TargetAgentKey    string `json:"target_agent_key"`
	TargetDisplayName string `json:"target_display_name,omitempty"`
	UserID            string `json:"user_id"`
	Channel           string `json:"channel"`
	ChatID            string `json:"chat_id"`
	Mode              string `json:"mode"`
	Task              string `json:"task,omitempty"`
	TeamID            string `json:"team_id,omitempty"`
	TeamTaskID        string `json:"team_task_id,omitempty"`
	Status            string `json:"status,omitempty"`
	ElapsedMS         int    `json:"elapsed_ms,omitempty"`
	Error             string `json:"error,omitempty"`
	CreatedAt         string `json:"created_at"`
}

// DelegationProgressItem describes one active delegation in a progress report.
type DelegationProgressItem struct {
	DelegationID      string `json:"delegation_id"`
	TargetAgentKey    string `json:"target_agent_key"`
	TargetDisplayName string `json:"target_display_name,omitempty"`
	ElapsedMS         int    `json:"elapsed_ms"`
	TeamTaskID        string `json:"team_task_id,omitempty"`
	Activity          string `json:"activity,omitempty"` // "thinking", "tool_exec", "compacting"
	Tool              string `json:"tool,omitempty"`     // current tool name (when Activity == "tool_exec")
}

// DelegationProgressPayload is emitted periodically for async delegations.
type DelegationProgressPayload struct {
	SourceAgentID  string                   `json:"source_agent_id"`
	SourceAgentKey string                   `json:"source_agent_key"`
	UserID         string                   `json:"user_id"`
	Channel        string                   `json:"channel"`
	ChatID         string                   `json:"chat_id"`
	TeamID         string                   `json:"team_id,omitempty"`
	Active         []DelegationProgressItem `json:"active_delegations"`
}

// DelegationAccumulatedPayload is emitted when an async delegation completes
// but siblings are still running (result accumulated, not yet announced).
type DelegationAccumulatedPayload struct {
	DelegationID      string `json:"delegation_id"`
	SourceAgentID     string `json:"source_agent_id"`
	SourceAgentKey    string `json:"source_agent_key"`
	TargetAgentKey    string `json:"target_agent_key"`
	TargetDisplayName string `json:"target_display_name,omitempty"`
	UserID            string `json:"user_id"`
	Channel           string `json:"channel"`
	ChatID            string `json:"chat_id"`
	TeamID            string `json:"team_id,omitempty"`
	TeamTaskID        string `json:"team_task_id,omitempty"`
	SiblingsRemaining int    `json:"siblings_remaining"`
	ElapsedMS         int    `json:"elapsed_ms,omitempty"`
}

// DelegationAnnounceResultSummary describes one delegation result in the announce payload.
type DelegationAnnounceResultSummary struct {
	AgentKey       string `json:"agent_key"`
	DisplayName    string `json:"display_name,omitempty"`
	HasMedia       bool   `json:"has_media"`
	ContentPreview string `json:"content_preview,omitempty"`
}

// DelegationAnnouncePayload is emitted when the last sibling delegation completes
// and all accumulated results are announced back to the lead agent.
type DelegationAnnouncePayload struct {
	SourceAgentID     string                            `json:"source_agent_id"`
	SourceAgentKey    string                            `json:"source_agent_key"`
	SourceDisplayName string                            `json:"source_display_name,omitempty"`
	UserID            string                            `json:"user_id"`
	Channel           string                            `json:"channel"`
	ChatID            string                            `json:"chat_id"`
	TeamID            string                            `json:"team_id,omitempty"`
	Results           []DelegationAnnounceResultSummary `json:"results"`
	CompletedTaskIDs  []string                          `json:"completed_task_ids,omitempty"`
	TotalElapsedMS    int                               `json:"total_elapsed_ms"`
	HasMedia          bool                              `json:"has_media"`
}

// QualityGateRetryPayload is emitted when a quality gate rejects a delegation
// result and triggers a retry.
type QualityGateRetryPayload struct {
	DelegationID   string `json:"delegation_id"`
	TargetAgentKey string `json:"target_agent_key"`
	UserID         string `json:"user_id"`
	Channel        string `json:"channel"`
	ChatID         string `json:"chat_id"`
	TeamID         string `json:"team_id,omitempty"`
	TeamTaskID     string `json:"team_task_id,omitempty"`
	GateType       string `json:"gate_type"`
	Attempt        int    `json:"attempt"`
	MaxRetries     int    `json:"max_retries"`
	Feedback       string `json:"feedback,omitempty"`
}

// TeamTaskEventPayload is the typed payload for team task lifecycle events
// (created, claimed, completed, cancelled, approved, rejected).
type TeamTaskEventPayload struct {
	TeamID           string `json:"team_id"`
	TaskID           string `json:"task_id"`
	Subject          string `json:"subject,omitempty"`
	Status           string `json:"status"`
	OwnerAgentKey    string `json:"owner_agent_key,omitempty"`
	OwnerDisplayName string `json:"owner_display_name,omitempty"`
	Reason           string `json:"reason,omitempty"`
	UserID           string `json:"user_id"`
	Channel          string `json:"channel"`
	ChatID           string `json:"chat_id"`
	Timestamp        string `json:"timestamp"`

	// Actor info for audit trail (recorded to team_task_events by subscriber).
	ActorType string `json:"actor_type,omitempty"` // "agent", "human", "system"
	ActorID   string `json:"actor_id,omitempty"`   // agent key, user ID, or system identifier
}

// TeamMessageEventPayload is the typed payload for team.message.sent events.
type TeamMessageEventPayload struct {
	TeamID          string `json:"team_id"`
	FromAgentKey    string `json:"from_agent_key"`
	FromDisplayName string `json:"from_display_name,omitempty"`
	ToAgentKey      string `json:"to_agent_key"`
	ToDisplayName   string `json:"to_display_name,omitempty"`
	MessageType     string `json:"message_type"`
	Preview         string `json:"preview"`
	TaskID          string `json:"task_id,omitempty"`
	UserID          string `json:"user_id"`
	Channel         string `json:"channel"`
	ChatID          string `json:"chat_id"`
}

// --- Team CRUD event payloads ---

// TeamCreatedPayload is emitted when a new team is created via RPC.
type TeamCreatedPayload struct {
	TeamID          string `json:"team_id"`
	TeamName        string `json:"team_name"`
	LeadAgentKey    string `json:"lead_agent_key"`
	LeadDisplayName string `json:"lead_display_name,omitempty"`
	MemberCount     int    `json:"member_count"`
}

// TeamUpdatedPayload is emitted when team settings are updated via RPC.
type TeamUpdatedPayload struct {
	TeamID   string   `json:"team_id"`
	TeamName string   `json:"team_name"`
	Changes  []string `json:"changes"`
}

// TeamDeletedPayload is emitted when a team is deleted via RPC.
type TeamDeletedPayload struct {
	TeamID   string `json:"team_id"`
	TeamName string `json:"team_name"`
}

// TeamMemberAddedPayload is emitted when a member is added to a team.
type TeamMemberAddedPayload struct {
	TeamID      string `json:"team_id"`
	TeamName    string `json:"team_name"`
	AgentID     string `json:"agent_id"`
	AgentKey    string `json:"agent_key"`
	DisplayName string `json:"display_name,omitempty"`
	Role        string `json:"role"`
}

// TeamMemberRemovedPayload is emitted when a member is removed from a team.
type TeamMemberRemovedPayload struct {
	TeamID      string `json:"team_id"`
	TeamName    string `json:"team_name"`
	AgentID     string `json:"agent_id"`
	AgentKey    string `json:"agent_key"`
	DisplayName string `json:"display_name,omitempty"`
}

// --- Agent link event payloads ---

// AgentLinkCreatedPayload is emitted when a new agent link is created.
type AgentLinkCreatedPayload struct {
	LinkID         string `json:"link_id"`
	SourceAgentID  string `json:"source_agent_id"`
	SourceAgentKey string `json:"source_agent_key"`
	TargetAgentID  string `json:"target_agent_id"`
	TargetAgentKey string `json:"target_agent_key"`
	Direction      string `json:"direction"`
	TeamID         string `json:"team_id,omitempty"`
	Status         string `json:"status"`
}

// AgentLinkUpdatedPayload is emitted when an agent link is updated.
type AgentLinkUpdatedPayload struct {
	LinkID         string   `json:"link_id"`
	SourceAgentKey string   `json:"source_agent_key"`
	TargetAgentKey string   `json:"target_agent_key"`
	Direction      string   `json:"direction,omitempty"`
	Status         string   `json:"status,omitempty"`
	Changes        []string `json:"changes"`
}

// AgentLinkDeletedPayload is emitted when an agent link is deleted.
type AgentLinkDeletedPayload struct {
	LinkID         string `json:"link_id"`
	SourceAgentKey string `json:"source_agent_key"`
	TargetAgentKey string `json:"target_agent_key"`
}
