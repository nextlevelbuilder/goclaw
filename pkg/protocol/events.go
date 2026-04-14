package protocol

// WebSocket event names pushed from server to client.
const (
	EventAgent              = "agent"
	EventChat               = "chat"
	EventHealth             = "health"
	EventCron               = "cron"
	EventHeartbeat          = "heartbeat"
	EventExecApprovalReq    = "exec.approval.requested"
	EventExecApprovalRes    = "exec.approval.resolved"
	EventPresence           = "presence"
	EventTick               = "tick"
	EventShutdown           = "shutdown"
	EventNodePairRequested  = "node.pair.requested"
	EventNodePairResolved   = "node.pair.resolved"
	EventDevicePairReq      = "device.pair.requested"
	EventDevicePairRes      = "device.pair.resolved"
	EventVoicewakeChanged   = "voicewake.changed"
	EventConnectChallenge   = "connect.challenge"
	EventTalkMode           = "talk.mode"

	// Agent summoning events (predefined agent setup via LLM).
	EventAgentSummoning = "agent.summoning"

	// Team activity events (real-time team workflow visibility).
	EventTeamTaskCreated     = "team.task.created"
	EventTeamTaskCompleted   = "team.task.completed"
	EventTeamMessageSent     = "team.message.sent"
	EventDelegationStarted   = "delegation.started"
	EventDelegationCompleted = "delegation.completed"

	// Delegation lifecycle events.
	EventDelegationFailed      = "delegation.failed"
	EventDelegationCancelled   = "delegation.cancelled"
	EventDelegationProgress    = "delegation.progress"
	EventDelegationAccumulated = "delegation.accumulated"
	EventDelegationAnnounce    = "delegation.announce"

	// Team task lifecycle events.
	EventTeamTaskClaimed   = "team.task.claimed"
	EventTeamTaskCancelled = "team.task.cancelled"
	EventTeamTaskFailed    = "team.task.failed"
	EventTeamTaskReviewed  = "team.task.reviewed"
	EventTeamTaskApproved  = "team.task.approved"
	EventTeamTaskRejected  = "team.task.rejected"
	EventTeamTaskProgress  = "team.task.progress"
	EventTeamTaskCommented = "team.task.commented"
	EventTeamTaskAssigned   = "team.task.assigned"
	EventTeamTaskDispatched = "team.task.dispatched"
	EventTeamTaskUpdated   = "team.task.updated"
	EventTeamTaskDeleted   = "team.task.deleted"
	EventTeamTaskStale          = "team.task.stale"
	EventTeamTaskAttachmentAdded = "team.task.attachment_added"

	// Emitted when leader starts processing completed team task results (before announce run).
	EventTeamLeaderProcessing = "team.leader.processing"

	// Team CRUD events (admin operations).
	EventTeamCreated       = "team.created"
	EventTeamUpdated       = "team.updated"
	EventTeamDeleted       = "team.deleted"
	EventTeamMemberAdded   = "team.member.added"
	EventTeamMemberRemoved = "team.member.removed"

	// Workspace events (team file changes).
	EventWorkspaceFileChanged = "workspace.file.changed"

	// Agent link events (admin operations).
	EventAgentLinkCreated = "agent_link.created"
	EventAgentLinkUpdated = "agent_link.updated"
	EventAgentLinkDeleted = "agent_link.deleted"

	// Trace lifecycle events (realtime trace/span updates).
	EventTraceUpdated = "trace.updated"

	// Skill dependency check events (realtime progress during startup/rescan).
	EventSkillDepsChecked  = "skill.deps.checked"
	EventSkillDepsComplete = "skill.deps.complete"

	// Skill dependency install events (triggered by POST /v1/skills/install-deps).
	EventSkillDepsInstalling = "skill.deps.installing"
	EventSkillDepsInstalled  = "skill.deps.installed"

	// Per-item install events (triggered by POST /v1/skills/install-dep).
	EventSkillDepItemInstalling = "skill.dep.item.installing" // payload: {dep: "pip:openpyxl"}
	EventSkillDepItemInstalled  = "skill.dep.item.installed"  // payload: {dep, ok: bool, error?: string}

	// Cache invalidation events (internal, not forwarded to WS clients).
	EventCacheInvalidate = "cache.invalidate"

	// Audit log event (internal, not forwarded to WS clients).
	EventAuditLog = "audit.log"

	// Session lifecycle events.
	EventSessionUpdated = "session.updated"

	// Zalo Personal QR login events (client-scoped, not broadcast).
	EventZaloPersonalQRCode = "zalo.personal.qr.code"
	EventZaloPersonalQRDone = "zalo.personal.qr.done"

	// WhatsApp QR login events (client-scoped, not broadcast).
	EventWhatsAppQRCode = "whatsapp.qr.code"
	EventWhatsAppQRDone = "whatsapp.qr.done"

	// Tenant access revocation — forces affected user's UI to logout.
	EventTenantAccessRevoked = "tenant.access.revoked"

	// Vault enrichment pipeline progress.
	EventVaultEnrichProgress = "vault.enrich.progress"

	// Background worker alerts (non-retryable LLM errors).
	EventBackgroundError = "background.error"
)

// Agent event subtypes (in payload.type)
const (
	AgentEventRunStarted   = "run.started"
	AgentEventRunCompleted = "run.completed"
	AgentEventRunFailed    = "run.failed"
	AgentEventRunCancelled = "run.cancelled"
	AgentEventRunRetrying  = "run.retrying"
	AgentEventToolCall     = "tool.call"
	AgentEventToolResult   = "tool.result"
	AgentEventBlockReply   = "block.reply"
	AgentEventActivity     = "activity" // agent phase transitions: thinking, tool_exec, compacting
)

// Chat event subtypes (in payload.type)
const (
	ChatEventChunk     = "chunk"
	ChatEventMessage   = "message"
	ChatEventThinking  = "thinking"
)
