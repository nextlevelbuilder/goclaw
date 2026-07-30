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
	MethodChatSend           = "chat.send"
	MethodChatHistory        = "chat.history"
	MethodChatAbort          = "chat.abort"
	MethodChatInject         = "chat.inject"
	MethodChatSessionStatus  = "chat.session.status"
	MethodChatActiveSessions = "chat.activeSessions"
	MethodChatToolResult     = "chat.toolResult"

	// MethodRunsSubscribe is the resumable-stream entry point. Client
	// sends `{runId, sinceSeq}` and receives all buffered events with
	// Seq > sinceSeq (in a single response), then continues to receive
	// live events through the normal broadcast path. Replaces the
	// activeSessions + sessions.preview hybrid for in-flight recovery.
	MethodRunsSubscribe = "runs.subscribe"

	// MethodWorkflowRunState returns the full per-cell snapshot for a
	// sheet-workflow run. Used by the SPA's Paradigm-style split-view
	// canvas on WS (re)connect to rehydrate the grid without waiting on
	// `workflow.event` traffic (events are at-least-once but not
	// durable — anything emitted before this client (re)connected is
	// gone). Server-side auth: caller's tenant must match the run's
	// tenant; otherwise not_found. Schema docs:
	// goclaw/docs/SHEET_WORKFLOWS_EVENTS.md.
	MethodWorkflowRunState = "workflow.runState"

	// MethodWorkflowEnqueue kicks off a new sheet-workflow run on
	// behalf of the calling user (SPA "Enrich" wizard entry point).
	// Mirrors the HTTP /v1/internal/workflows/enqueue contract but
	// reads tenant + user from the WS session — never trusts
	// client-supplied identity. Same orchestrator and store under the
	// hood; same RunEvent stream surfaces after queue.
	MethodWorkflowEnqueue = "workflow.enqueue"

	// MethodWorkflowPeekSheet reads a range of values straight from
	// the user's Google Sheet via composio-mcp's GOOGLESHEETS_VALUES_GET.
	// The SPA bubble uses this to display the actual cell contents
	// (what's IN the sheet right now) instead of the orchestrator's
	// per-cell status DB cache. Composio authenticates via the user's
	// own OAuth — X-Proxy-User from the WS session identity — so
	// Google's ACL is the source of truth for access, not goclaw's
	// tenant filter.
	MethodWorkflowPeekSheet = "workflow.peekSheet"

	// MethodWorkflowRunsSubscribe is the resumable-stream replay
	// endpoint for sheet-workflow runs — same pattern as runs.subscribe
	// for chat. Client sends (run_id, since_seq); server returns every
	// buffered workflow.event whose Seq > since_seq, so a WS reconnect
	// after a brief drop replays the gap without losing per-cell value
	// updates that arrived while the socket was down.
	MethodWorkflowRunsSubscribe = "workflow.runsSubscribe"

	// MethodSheetPreview parses a delivered spreadsheet file (.xlsx/.csv) in
	// the user's workspace/data dir into a compact JSON grid {sheet, columns,
	// rows} the chat UI renders as an interactive, editable table inline next
	// to the download link. Read path for the interactive-spreadsheet feature.
	MethodSheetPreview = "sheet.preview"

	// MethodSheetSave rewrites the spreadsheet file in place from an edited grid
	// (cell edits / rename / delete-column / add-row) with no LLM — so simple
	// edits update the same document instead of starting a new chat turn.
	MethodSheetSave = "sheet.save"

	// MethodSheetEnrich fills newly-added empty columns (e.g. "Year Founded")
	// for every row via one LLM call, writes the file in place, and returns the
	// enriched grid so the same inline view updates — no new chat message.
	MethodSheetEnrich = "sheet.enrich"

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
	MethodSkillsList   = "skills.list"
	MethodSkillsGet    = "skills.get"
	MethodSkillsUpdate = "skills.update"

	MethodCronList   = "cron.list"
	MethodCronCreate = "cron.create"
	MethodCronUpdate = "cron.update"
	MethodCronDelete = "cron.delete"
	MethodCronToggle = "cron.toggle"
	MethodCronStatus = "cron.status"
	MethodCronRun    = "cron.run"
	MethodCronRuns   = "cron.runs"

	MethodRemindersList        = "reminders.list"
	MethodRemindersMarkRead    = "reminders.markRead"
	MethodRemindersMarkAllRead = "reminders.markAllRead"
	MethodRemindersDelete      = "reminders.delete"

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

	// Tenant-level CLI connections (migration 000082): the connected-CLI
	// catalogue shared by every agent in a tenant. connections.list is a read
	// (viewer+); the rest are writes and require operator+ — see
	// permissions.isWriteMethod. credential.* writes a per-USER BYOK secret and
	// never returns one.
	MethodConnectionsList             = "connections.list"
	MethodConnectionsCreate           = "connections.create"
	MethodConnectionsUpdate           = "connections.update"
	MethodConnectionsDelete           = "connections.delete"
	MethodConnectionsCredentialSet    = "connections.credential.set"
	MethodConnectionsCredentialDelete = "connections.credential.delete"

	// MethodConnectionsChatOpen opens an interactive conversation with a
	// connected coding CLI: it validates that the connection is visible,
	// enabled and able to hold a conversation at all, then returns the
	// `sessionKey` the client sends chat.send to. Nothing is started here —
	// the first chat.send is what launches the CLI process. Requires
	// operator+ like the other connections.* actions (it can start a
	// process and spend the user's CLI credential).
	MethodConnectionsChatOpen = "connections.chat.open"

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
	MethodTeamsList                = "teams.list"
	MethodTeamsCreate              = "teams.create"
	MethodTeamsGet                 = "teams.get"
	MethodTeamsDelete              = "teams.delete"
	MethodTeamsTaskList            = "teams.tasks.list"
	MethodTeamsTaskGet             = "teams.tasks.get"
	MethodTeamsTaskGetLight        = "teams.tasks.get-light"
	MethodTeamsTaskApprove         = "teams.tasks.approve"
	MethodTeamsTaskReject          = "teams.tasks.reject"
	MethodTeamsTaskComment         = "teams.tasks.comment"
	MethodTeamsTaskComments        = "teams.tasks.comments"
	MethodTeamsTaskEvents          = "teams.tasks.events"
	MethodTeamsTaskCreate          = "teams.tasks.create"
	MethodTeamsTaskDelete          = "teams.tasks.delete"
	MethodTeamsTaskDeleteBulk      = "teams.tasks.delete-bulk"
	MethodTeamsTaskAssign          = "teams.tasks.assign"
	MethodTeamsTaskActiveBySession = "teams.tasks.active-by-session"
	MethodTeamsMembersAdd          = "teams.members.add"
	MethodTeamsMembersRemove       = "teams.members.remove"
	MethodTeamsUpdate              = "teams.update"
	MethodTeamsKnownUsers          = "teams.known_users"
	MethodTeamsScopes              = "teams.scopes"
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
	MethodZaloPersonalQRStart  = "zalo.personal.qr.start"
	MethodZaloPersonalContacts = "zalo.personal.contacts"

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
