package i18n

func init() {
	register(LocaleEN, map[string]string{
		// Common validation
		MsgRequired:         "%s is required",
		MsgInvalidID:        "invalid %s ID",
		MsgNotFound:         "%s not found: %s",
		MsgAlreadyExists:    "%s already exists: %s",
		MsgInvalidRequest:   "invalid request: %s",
		MsgInvalidJSON:      "invalid JSON",
		MsgUnauthorized:     "unauthorized",
		MsgPermissionDenied: "permission denied: insufficient role for %s",
		MsgInternalError:    "internal error: %s",
		MsgInvalidSlug:      "%s must be a valid slug (lowercase letters, numbers, hyphens only)",
		MsgFailedToList:     "failed to list %s",
		MsgFailedToCreate:   "failed to create %s: %s",
		MsgFailedToUpdate:   "failed to update %s: %s",
		MsgFailedToDelete:   "failed to delete %s: %s",
		MsgFailedToSave:     "failed to save %s: %s",
		MsgInvalidUpdates:   "invalid updates",

		// Agent
		MsgAgentNotFound:       "agent not found: %s",
		MsgCannotDeleteDefault: "cannot delete the default agent",
		MsgUserCtxRequired:     "user context required",

		// Chat
		MsgRateLimitExceeded: "rate limit exceeded — please wait",
		MsgNoUserMessage:     "no user message found",
		MsgUserIDRequired:    "user_id is required",
		MsgMsgRequired:       "message is required",

		// Channel instances
		MsgInvalidChannelType: "invalid channel_type",
		MsgInstanceNotFound:   "instance not found",

		// Cron
		MsgJobNotFound:     "job not found",
		MsgInvalidCronExpr: "invalid cron expression: %s",

		// Config
		MsgConfigHashMismatch: "config has changed (hash mismatch)",

		// Exec approval
		MsgExecApprovalDisabled: "exec approval is not enabled",

		// Pairing
		MsgSenderChannelRequired: "senderId and channel are required",
		MsgCodeRequired:          "code is required",
		MsgSenderIDRequired:      "sender_id is required",

		// HTTP API
		MsgInvalidAuth:           "invalid authentication",
		MsgMsgsRequired:          "messages is required",
		MsgUserIDHeader:          "X-GoClaw-User-Id header is required",
		MsgFileTooLarge:          "file too large or invalid multipart form",
		MsgMissingFileField:      "missing 'file' field",
		MsgInvalidFilename:       "invalid filename",
		MsgChannelKeyReq:         "channel and key are required",
		MsgMethodNotAllowed:      "method not allowed",
		MsgStreamingNotSupported: "streaming not supported",
		MsgOwnerOnly:             "only owner can %s",
		MsgNoAccess:              "no access to this %s",
		MsgAlreadySummoning:      "agent is already being summoned",
		MsgSummoningUnavailable:  "summoning not available",
		MsgNoDescription:         "agent has no description to resummon from",
		MsgInvalidPath:           "invalid path",

		// Scheduler
		MsgQueueFull:    "session queue is full",
		MsgShuttingDown: "gateway is shutting down, please retry shortly",

		// Provider
		MsgProviderReqFailed: "%s: request failed: %s",

		// Unknown method
		MsgUnknownMethod: "unknown method: %s",

		// Not implemented
		MsgNotImplemented: "%s not yet implemented",

		// Agent links
		MsgLinksNotConfigured:   "agent links not configured",
		MsgInvalidDirection:     "direction must be outbound, inbound, or bidirectional",
		MsgSourceTargetSame:     "source and target must be different agents",
		MsgCannotDelegateOpen:   "cannot delegate to open agents — only predefined agents can be delegation targets",
		MsgNoUpdatesProvided:    "no updates provided",
		MsgInvalidLinkStatus:    "status must be active or disabled",

		// Teams
		MsgTeamsNotConfigured:   "teams not configured",
		MsgAgentIsTeamLead:      "agent is already the team lead",
		MsgCannotRemoveTeamLead: "cannot remove the team lead",

		// Delegations
		MsgDelegationsUnavailable: "delegations not available",

		// Channels
		MsgCannotDeleteDefaultInst: "cannot delete default channel instance",

		// Skills
		MsgSkillsUpdateNotSupported: "skills.update not supported for file-based skills",
		MsgCannotResolveSkillID:     "cannot resolve skill ID for file-based skill",

		// Logs
		MsgInvalidLogAction: "action must be 'start' or 'stop'",

		// Config
		MsgRawConfigRequired: "raw config is required",
		MsgRawPatchRequired:  "raw patch is required",

		// Storage / File
		MsgCannotDeleteSkillsDir: "cannot delete skills directories",
		MsgFailedToReadFile:      "failed to read file",
		MsgFileNotFound:          "file not found",
		MsgInvalidVersion:        "invalid version",
		MsgVersionNotFound:       "version not found",
		MsgFailedToDeleteFile:    "failed to delete",

		// OAuth
		MsgNoPendingOAuth:    "no pending OAuth flow",
		MsgFailedToSaveToken: "failed to save token",

		// Intent Classify
		MsgStatusWorking:       "🔄 I'm working on your request... Please wait.",
		MsgStatusDetailed:      "🔄 I'm currently working on your request...\n%s (iteration %d)\nRunning for: %s\n\nPlease wait — I'll respond when done.",
		MsgStatusPhaseThinking: "Phase: Thinking...",
		MsgStatusPhaseToolExec: "Phase: Running %s",
		MsgStatusPhaseTools:    "Phase: Executing tools...",
		MsgStatusPhaseCompact:  "Phase: Compacting context...",
		MsgStatusPhaseDefault:  "Phase: Processing...",
		MsgCancelledReply:      "✋ Cancelled. What would you like to do next?",
		MsgInjectedAck:         "Got it, I'll incorporate that into what I'm working on.",

		// Knowledge Graph
		MsgEntityIDRequired:       "entity_id is required",
		MsgEntityFieldsRequired:   "external_id, name, and entity_type are required",
		MsgTextRequired:           "text is required",
		MsgProviderModelRequired:  "provider and model are required",
		MsgInvalidProviderOrModel: "invalid provider or model",

		// Builtin tool descriptions
		MsgToolReadFile:        "Read the contents of a file from the agent's workspace by path",
		MsgToolWriteFile:       "Write content to a file in the workspace, creating directories as needed",
		MsgToolListFiles:       "List files and directories in a given path within the workspace",
		MsgToolEdit:            "Apply targeted search-and-replace edits to existing files without rewriting the entire file",
		MsgToolExec:            "Execute a shell command in the workspace and return stdout/stderr",
		MsgToolWebSearch:       "Search the web for information using a search engine (Brave or DuckDuckGo)",
		MsgToolWebFetch:        "Fetch a web page or API endpoint and extract its text content",
		MsgToolMemorySearch:    "Search through the agent's long-term memory using semantic similarity",
		MsgToolMemoryGet:       "Retrieve a specific memory document by its file path",
		MsgToolKGSearch:        "Search entities, relationships, and observations in the agent's knowledge graph",
		MsgToolReadImage:       "Analyze images using a vision-capable LLM provider",
		MsgToolReadDocument:    "Analyze documents (PDF, Word, Excel, PowerPoint, CSV, etc.) using a document-capable LLM provider",
		MsgToolCreateImage:     "Generate images from text prompts using an image generation provider",
		MsgToolReadAudio:       "Analyze audio files (speech, music, sounds) using an audio-capable LLM provider",
		MsgToolReadVideo:       "Analyze video files using a video-capable LLM provider",
		MsgToolCreateVideo:     "Generate videos from text descriptions using AI",
		MsgToolCreateAudio:     "Generate music or sound effects from text descriptions using AI",
		MsgToolTTS:             "Convert text to natural-sounding speech audio",
		MsgToolBrowser:         "Automate browser interactions: navigate pages, click elements, fill forms, take screenshots",
		MsgToolSessionsList:    "List active chat sessions across all channels",
		MsgToolSessionStatus:   "Get the current status and metadata of a specific chat session",
		MsgToolSessionsHistory: "Retrieve the message history of a specific chat session",
		MsgToolSessionsSend:    "Send a message to an active chat session on behalf of the agent",
		MsgToolMessage:         "Send a proactive message to a user on a connected channel (Telegram, Discord, etc.)",
		MsgToolCron:            "Schedule or manage recurring tasks using cron expressions, at-times, or intervals",
		MsgToolSpawn:           "Spawn a subagent for background work or delegate a task to a linked agent",
		MsgToolSkillSearch:     "Search for available skills by keyword or description to find relevant capabilities",
		MsgToolUseSkill:        "Activate a skill to use its specialized capabilities (tracing marker)",
		MsgToolSkillManage:     "Create, patch, or delete skills from conversation experience",
		MsgToolPublishSkill:    "Register a skill directory in the system database, making it discoverable",
		MsgToolTeamTasks:       "View, create, update, and complete tasks on the team task board",
		MsgToolTeamMessage:     "Send a direct message or broadcast to teammates in the agent team",

		MsgSkillNudgePostscript: "This task involved several steps. Want me to save the process as a reusable skill? Reply **\"save as skill\"** or **\"skip\"**.",
		MsgSkillNudge70Pct:      "[System] You are at 70% of your iteration budget. Consider whether any patterns from this session would make a good skill.",
		MsgSkillNudge90Pct:      "[System] You are at 90% of your iteration budget. If this session involved reusable patterns, consider saving them as a skill before completing.",
	})
}
