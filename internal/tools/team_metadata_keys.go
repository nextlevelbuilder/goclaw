package tools

// Message metadata keys used in dispatch → consumer communication.
// These keys appear in bus.InboundMessage.Metadata for teammate and
// subagent messages routed through the gateway consumer.
const (
	MetaOriginChannel    = "origin_channel"
	MetaOriginPeerKind   = "origin_peer_kind"
	MetaOriginChatID     = "origin_chat_id"
	MetaOriginUserID     = "origin_user_id"
	// MetaOriginSenderID carries the real acting sender through announce re-ingress
	// so permission checks (e.g. CheckFileWriterPermission) attribute to the
	// original user rather than a synthetic "subagent:<id>" / "notification:system" string.
	MetaOriginSenderID   = "origin_sender_id"
	// MetaOriginRole carries the caller's RBAC role through dispatch + re-ingress
	// so permission checks can bypass per-user grants for authenticated admins
	// (e.g. dashboard user dispatches a task that writes files in a group chat).
	MetaOriginRole       = "origin_role"
	MetaOriginLocalKey   = "origin_local_key"
	MetaOriginSessionKey = "origin_session_key"
	MetaOriginTraceID    = "origin_trace_id"
	MetaOriginRootSpanID = "origin_root_span_id"
	MetaFromAgent        = "from_agent"
	MetaToAgent          = "to_agent"
	MetaToAgentDisplay   = "to_agent_display"
	MetaTeamTaskID       = "team_task_id"
	MetaTeamID           = "team_id"
	MetaTeamWorkspace    = "team_workspace"
	MetaLeaderAgentID    = "leader_agent_id"
	MetaParentAgent      = "parent_agent"
	MetaSubagentLabel      = "subagent_label"
	MetaSubagentStatus     = "subagent_status"
	MetaSubagentResult     = "subagent_result"
	MetaSubagentRuntime    = "subagent_runtime_ms"
	MetaSubagentIterations = "subagent_iterations"
	MetaSubagentInputToks  = "subagent_input_tokens"
	MetaSubagentOutputToks = "subagent_output_tokens"
	MetaCommand          = "command"
	MetaIsForum          = "is_forum"
	MetaMessageThreadID  = "message_thread_id"
	MetaDMThreadID       = "dm_thread_id"
	MetaChatTitle        = "chat_title"
	MetaUsername         = "username"
	MetaUserName         = "user_name"
	MetaTopicSystemPrompt = "topic_system_prompt"
	MetaTopicSkills      = "topic_skills"
)

// Task metadata keys stored in store.TeamTaskData.Metadata.
// Written during task creation/dispatch, read during deferred dispatch
// and trace context restoration.
const (
	TaskMetaPeerKind       = "peer_kind"
	TaskMetaLocalKey       = "local_key"
	TaskMetaOriginSession  = "origin_session_key"
	TaskMetaOriginTrace    = "origin_trace_id"
	TaskMetaOriginRootSpan = "origin_root_span_id"
	TaskMetaTeamWorkspace  = "team_workspace"
)
