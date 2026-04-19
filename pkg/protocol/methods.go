package protocol

// RPC method name constants.
// Organized by priority: CRITICAL (Phase 1) → NEEDED (Phase 2) → NICE TO HAVE (Phase 3+).

// Phase 1 - CRITICAL methods
const (
	// Agent
	MethodAgent            = "agent"
	MethodAgentWait        = "agent.wait"
	MethodAgentIdentityGet = "agent.identity.get"

	// Chat
	MethodChatSend          = "chat.send"
	MethodChatHistory       = "chat.history"
	MethodChatAbort         = "chat.abort"
	MethodChatInject        = "chat.inject"
	MethodChatSessionStatus = "chat.session.status"

	// Agents management
	MethodAgentsList     = "agents.list"
	MethodAgentsCreate   = "agents.create"
	MethodAgentsUpdate   = "agents.update"
	MethodAgentsDelete   = "agents.delete"
	MethodAgentsFileList = "agents.files.list"
	MethodAgentsFileGet  = "agents.files.get"
	MethodAgentsFileSet  = "agents.files.set"

	// Config
	MethodConfigGet      = "config.get"
	MethodConfigApply    = "config.apply"
	MethodConfigPatch    = "config.patch"
	MethodConfigSchema   = "config.schema"
	MethodConfigDefaults = "config.defaults"

	// Sessions
	MethodSessionsList    = "sessions.list"
	MethodSessionsPreview = "sessions.preview"
	MethodSessionsPatch   = "sessions.patch"
	MethodSessionsDelete  = "sessions.delete"
	MethodSessionsReset   = "sessions.reset"

	// System
	MethodConnect = "connect"
	MethodHealth  = "health"
	MethodStatus  = "status"
)

// Phase 2 - NEEDED methods
const (
	MethodSkillsList  = "skills.list"
	MethodSkillsGet   = "skills.get"
	MethodSkillsUpdate = "skills.update"

	MethodCronList   = "cron.list"
	MethodCronCreate = "cron.create"
	MethodCronUpdate = "cron.update"
	MethodCronDelete = "cron.delete"
	MethodCronToggle = "cron.toggle"
	MethodCronStatus = "cron.status"
	MethodCronRun    = "cron.run"
	MethodCronRuns   = "cron.runs"

	MethodChannelsList   = "channels.list"
	MethodChannelsStatus = "channels.status"
	MethodChannelsToggle = "channels.toggle"

	MethodPairingRequest = "device.pair.request"
	MethodPairingApprove = "device.pair.approve"
	MethodPairingDeny    = "device.pair.deny"
	MethodPairingList    = "device.pair.list"
	MethodPairingRevoke  = "device.pair.revoke"

	MethodBrowserPairingStatus = "browser.pairing.status"

	MethodApprovalsList    = "exec.approval.list"
	MethodApprovalsApprove = "exec.approval.approve"
	MethodApprovalsDeny    = "exec.approval.deny"

	MethodUsageGet     = "usage.get"
	MethodUsageSummary = "usage.summary"

	MethodQuotaUsage = "quota.usage"

	MethodSend = "send"
)

// Agent heartbeat
const (
	MethodHeartbeatGet          = "heartbeat.get"
	MethodHeartbeatSet          = "heartbeat.set"
	MethodHeartbeatToggle       = "heartbeat.toggle"
	MethodHeartbeatTest         = "heartbeat.test"
	MethodHeartbeatLogs         = "heartbeat.logs"
	MethodHeartbeatChecklistGet = "heartbeat.checklist.get"
	MethodHeartbeatChecklistSet = "heartbeat.checklist.set"
	MethodHeartbeatTargets      = "heartbeat.targets"
)

// Config permissions
const (
	MethodConfigPermissionsList   = "config.permissions.list"
	MethodConfigPermissionsGrant  = "config.permissions.grant"
	MethodConfigPermissionsRevoke = "config.permissions.revoke"
)

// Channel instances management
const (
	MethodChannelInstancesList   = "channels.instances.list"
	MethodChannelInstancesGet    = "channels.instances.get"
	MethodChannelInstancesCreate = "channels.instances.create"
	MethodChannelInstancesUpdate = "channels.instances.update"
	MethodChannelInstancesDelete = "channels.instances.delete"
)

// Agent links (inter-agent delegation)
const (
	MethodAgentsLinksList   = "agents.links.list"
	MethodAgentsLinksCreate = "agents.links.create"
	MethodAgentsLinksUpdate = "agents.links.update"
	MethodAgentsLinksDelete = "agents.links.delete"
)

// Agent teams
const (
	MethodTeamsList     = "teams.list"
	MethodTeamsCreate   = "teams.create"
	MethodTeamsGet      = "teams.get"
	MethodTeamsDelete   = "teams.delete"
	MethodTeamsTaskList      = "teams.tasks.list"
	MethodTeamsTaskGet       = "teams.tasks.get"
	MethodTeamsTaskGetLight  = "teams.tasks.get-light"
	MethodTeamsTaskApprove   = "teams.tasks.approve"
	MethodTeamsTaskReject    = "teams.tasks.reject"
	MethodTeamsTaskComment   = "teams.tasks.comment"
	MethodTeamsTaskComments  = "teams.tasks.comments"
	MethodTeamsTaskEvents    = "teams.tasks.events"
	MethodTeamsTaskCreate    = "teams.tasks.create"
	MethodTeamsTaskDelete     = "teams.tasks.delete"
	MethodTeamsTaskDeleteBulk = "teams.tasks.delete-bulk"
	MethodTeamsTaskAssign            = "teams.tasks.assign"
	MethodTeamsTaskActiveBySession   = "teams.tasks.active-by-session"
	MethodTeamsMembersAdd    = "teams.members.add"
	MethodTeamsMembersRemove = "teams.members.remove"
	MethodTeamsUpdate        = "teams.update"
	MethodTeamsKnownUsers    = "teams.known_users"
	MethodTeamsScopes        = "teams.scopes"
)

// Team workspace
const (
	MethodTeamsWorkspaceList   = "teams.workspace.list"
	MethodTeamsWorkspaceRead   = "teams.workspace.read"
	MethodTeamsWorkspaceDelete = "teams.workspace.delete"
)

// Team events
const (
	MethodTeamsEventsList = "teams.events.list"
)

// API key management
const (
	MethodAPIKeysList   = "api_keys.list"
	MethodAPIKeysCreate = "api_keys.create"
	MethodAPIKeysRevoke = "api_keys.revoke"
)

// Voices (ElevenLabs voice picker)
const (
	MethodVoicesList    = "voices.list"
	MethodVoicesRefresh = "voices.refresh"
)

// Phase 3+ - NICE TO HAVE methods
const (
	MethodLogsTail = "logs.tail"

	MethodTTSStatus      = "tts.status"
	MethodTTSEnable      = "tts.enable"
	MethodTTSDisable     = "tts.disable"
	MethodTTSConvert     = "tts.convert"
	MethodTTSSetProvider = "tts.setProvider"
	MethodTTSProviders   = "tts.providers"

	MethodBrowserAct        = "browser.act"
	MethodBrowserSnapshot   = "browser.snapshot"
	MethodBrowserScreenshot = "browser.screenshot"

	// Zalo Personal
	MethodZaloPersonalQRStart   = "zalo.personal.qr.start"
	MethodZaloPersonalContacts  = "zalo.personal.contacts"

	// WhatsApp
	MethodWhatsAppQRStart = "whatsapp.qr.start"
)

// Agent hooks (Phase 3)
const (
	MethodHooksList    = "hooks.list"
	MethodHooksCreate  = "hooks.create"
	MethodHooksUpdate  = "hooks.update"
	MethodHooksDelete  = "hooks.delete"
	MethodHooksToggle  = "hooks.toggle"
	MethodHooksTest    = "hooks.test"
	MethodHooksHistory = "hooks.history"
)
