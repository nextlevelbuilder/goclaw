package i18n

// Message keys for gateway/HTTP error messages.
// Grouped by domain for easier maintenance.
const (
	// --- Common validation ---
	MsgRequired          = "error.required"           // "%s is required"
	MsgInvalidID         = "error.invalid_id"         // "invalid %s ID"
	MsgNotFound          = "error.not_found"          // "%s not found: %s"
	MsgAlreadyExists     = "error.already_exists"     // "%s already exists: %s"
	MsgInvalidRequest    = "error.invalid_request"    // "invalid request: %s"
	MsgInvalidJSON       = "error.invalid_json"       // "invalid JSON"
	MsgUnauthorized      = "error.unauthorized"       // "unauthorized"
	MsgPermissionDenied  = "error.permission_denied"  // "permission denied: insufficient role for %s"
	MsgInternalError     = "error.internal"           // "internal error: %s"
	MsgInvalidSlug       = "error.invalid_slug"       // "%s must be a valid slug (lowercase letters, numbers, hyphens only)"
	MsgFailedToList      = "error.failed_to_list"     // "failed to list %s"
	MsgFailedToCreate    = "error.failed_to_create"   // "failed to create %s: %s"
	MsgFailedToUpdate    = "error.failed_to_update"   // "failed to update %s: %s"
	MsgFailedToDelete    = "error.failed_to_delete"   // "failed to delete %s: %s"
	MsgFailedToSave      = "error.failed_to_save"     // "failed to save %s: %s"
	MsgInvalidUpdates    = "error.invalid_updates"    // "invalid updates"

	// --- Agent ---
	MsgAgentNotFound       = "error.agent_not_found"        // "agent not found: %s"
	MsgCannotDeleteDefault = "error.cannot_delete_default"   // "cannot delete the default agent"
	MsgUserCtxRequired     = "error.user_ctx_required"       // "user context required"

	// --- Chat ---
	MsgRateLimitExceeded = "error.rate_limit"          // "rate limit exceeded — please wait"
	MsgNoUserMessage     = "error.no_user_message"     // "no user message found"
	MsgUserIDRequired    = "error.user_id_required"    // "user_id is required"
	MsgMsgRequired       = "error.message_required"    // "message is required"

	// --- Channel instances ---
	MsgInvalidChannelType = "error.invalid_channel_type" // "invalid channel_type"
	MsgInstanceNotFound   = "error.instance_not_found"   // "instance not found"

	// --- Cron ---
	MsgJobNotFound     = "error.job_not_found"     // "job not found"
	MsgInvalidCronExpr = "error.invalid_cron_expr" // "invalid cron expression: %s"

	// --- Config ---
	MsgConfigHashMismatch = "error.config_hash_mismatch" // "config has changed (hash mismatch)"

	// --- Exec approval ---
	MsgExecApprovalDisabled = "error.exec_approval_disabled" // "exec approval is not enabled"

	// --- Pairing ---
	MsgSenderChannelRequired = "error.sender_channel_required" // "senderId and channel are required"
	MsgCodeRequired          = "error.code_required"           // "code is required"
	MsgSenderIDRequired      = "error.sender_id_required"      // "sender_id is required"

	// --- HTTP API ---
	MsgInvalidAuth          = "error.invalid_auth"              // "invalid authentication"
	MsgMsgsRequired         = "error.messages_required"         // "messages is required"
	MsgUserIDHeader         = "error.user_id_header"            // "X-GoClaw-User-Id header is required"
	MsgFileTooLarge         = "error.file_too_large"            // "file too large or invalid multipart form"
	MsgMissingFileField     = "error.missing_file_field"        // "missing 'file' field"
	MsgInvalidFilename      = "error.invalid_filename"          // "invalid filename"
	MsgChannelKeyReq        = "error.channel_key_required"      // "channel and key are required"
	MsgMethodNotAllowed     = "error.method_not_allowed"        // "method not allowed"
	MsgStreamingNotSupported = "error.streaming_not_supported"  // "streaming not supported"
	MsgOwnerOnly            = "error.owner_only"                // "only owner can %s"
	MsgNoAccess             = "error.no_access"                 // "no access to this %s"
	MsgAlreadySummoning     = "error.already_summoning"         // "agent is already being summoned"
	MsgSummoningUnavailable = "error.summoning_unavailable"     // "summoning not available"
	MsgNoDescription        = "error.no_description"            // "agent has no description to resummon from"
	MsgInvalidPath          = "error.invalid_path"              // "invalid path"

	// --- Scheduler ---
	MsgQueueFull       = "error.queue_full"       // "session queue is full"
	MsgShuttingDown    = "error.shutting_down"     // "gateway is shutting down, please retry shortly"

	// --- Provider ---
	MsgProviderReqFailed = "error.provider_request_failed" // "%s: request failed: %s"

	// --- Unknown method ---
	MsgUnknownMethod = "error.unknown_method" // "unknown method: %s"

	// --- Not implemented ---
	MsgNotImplemented = "error.not_implemented" // "%s not yet implemented"

	// --- Agent links ---
	MsgLinksNotConfigured   = "error.links_not_configured"    // "agent links not configured"
	MsgInvalidDirection     = "error.invalid_direction"       // "direction must be outbound, inbound, or bidirectional"
	MsgSourceTargetSame     = "error.source_target_same"      // "source and target must be different agents"
	MsgCannotDelegateOpen   = "error.cannot_delegate_open"    // "cannot delegate to open agents — only predefined agents can be delegation targets"
	MsgNoUpdatesProvided    = "error.no_updates_provided"     // "no updates provided"
	MsgInvalidLinkStatus    = "error.invalid_link_status"     // "status must be active or disabled"

	// --- Teams ---
	MsgTeamsNotConfigured   = "error.teams_not_configured"    // "teams not configured"
	MsgAgentIsTeamLead      = "error.agent_is_team_lead"      // "agent is already the team lead"
	MsgCannotRemoveTeamLead = "error.cannot_remove_team_lead" // "cannot remove the team lead"

	// --- Delegations ---
	MsgDelegationsUnavailable = "error.delegations_not_available" // "delegations not available"

	// --- Channels ---
	MsgCannotDeleteDefaultInst  = "error.cannot_delete_default_inst"  // "cannot delete default channel instance"

	// --- Skills ---
	MsgSkillsUpdateNotSupported = "error.skills_update_not_supported" // "skills.update not supported for file-based skills"
	MsgCannotResolveSkillID     = "error.cannot_resolve_skill_id"     // "cannot resolve skill ID for file-based skill"

	// --- Logs ---
	MsgInvalidLogAction = "error.invalid_log_action" // "action must be 'start' or 'stop'"

	// --- Config ---
	MsgRawConfigRequired = "error.raw_config_required" // "raw config is required"
	MsgRawPatchRequired  = "error.raw_patch_required"  // "raw patch is required"

	// --- Storage / File ---
	MsgCannotDeleteSkillsDir = "error.cannot_delete_skills_dir" // "cannot delete skills directories"
	MsgFailedToReadFile      = "error.failed_to_read_file"      // "failed to read file"
	MsgFileNotFound          = "error.file_not_found"           // "file not found"
	MsgInvalidVersion        = "error.invalid_version"          // "invalid version"
	MsgVersionNotFound       = "error.version_not_found"        // "version not found"
	MsgFailedToDeleteFile    = "error.failed_to_delete_file"    // "failed to delete"

	// --- OAuth ---
	MsgNoPendingOAuth    = "error.no_pending_oauth"    // "no pending OAuth flow"
	MsgFailedToSaveToken = "error.failed_to_save_token" // "failed to save token"

	// --- Intent Classify (channel-facing status replies) ---
	MsgStatusWorking       = "status.working"         // "🔄 I'm working on your request... Please wait."
	MsgStatusDetailed      = "status.detailed"        // "🔄 I'm currently working on your request...\n%s (iteration %d)\nRunning for: %s\n\nPlease wait — I'll respond when done."
	MsgStatusPhaseThinking = "status.phase_thinking"  // "Phase: Thinking..."
	MsgStatusPhaseToolExec = "status.phase_tool_exec" // "Phase: Running %s"
	MsgStatusPhaseTools    = "status.phase_tools"     // "Phase: Executing tools..."
	MsgStatusPhaseCompact  = "status.phase_compact"   // "Phase: Compacting context..."
	MsgStatusPhaseDefault  = "status.phase_default"   // "Phase: Processing..."
	MsgCancelledReply      = "status.cancelled"       // "✋ Cancelled. What would you like to do next?"
	MsgInjectedAck         = "status.injected_ack"    // "Got it, I'll incorporate that into what I'm working on."

	// --- Knowledge Graph ---
	MsgEntityIDRequired           = "error.entity_id_required"            // "entity_id is required"
	MsgEntityFieldsRequired       = "error.entity_fields_required"        // "external_id, name, and entity_type are required"
	MsgTextRequired               = "error.text_required"                 // "text is required"
	MsgProviderModelRequired      = "error.provider_model_required"       // "provider and model are required"
	MsgInvalidProviderOrModel     = "error.invalid_provider_or_model"     // "invalid provider or model"

	// --- Builtin tool descriptions (i18n key = core.tool.<name>) ---
	MsgToolReadFile          = "core.tool.read_file"
	MsgToolWriteFile         = "core.tool.write_file"
	MsgToolListFiles         = "core.tool.list_files"
	MsgToolEdit              = "core.tool.edit"
	MsgToolExec              = "core.tool.exec"
	MsgToolWebSearch         = "core.tool.web_search"
	MsgToolWebFetch          = "core.tool.web_fetch"
	MsgToolMemorySearch      = "core.tool.memory_search"
	MsgToolMemoryGet         = "core.tool.memory_get"
	MsgToolKGSearch          = "core.tool.knowledge_graph_search"
	MsgToolReadImage         = "core.tool.read_image"
	MsgToolReadDocument      = "core.tool.read_document"
	MsgToolCreateImage       = "core.tool.create_image"
	MsgToolReadAudio         = "core.tool.read_audio"
	MsgToolReadVideo         = "core.tool.read_video"
	MsgToolCreateVideo       = "core.tool.create_video"
	MsgToolCreateAudio       = "core.tool.create_audio"
	MsgToolTTS               = "core.tool.tts"
	MsgToolBrowser           = "core.tool.browser"
	MsgToolSessionsList      = "core.tool.sessions_list"
	MsgToolSessionStatus     = "core.tool.session_status"
	MsgToolSessionsHistory   = "core.tool.sessions_history"
	MsgToolSessionsSend      = "core.tool.sessions_send"
	MsgToolMessage           = "core.tool.message"
	MsgToolCron              = "core.tool.cron"
	MsgToolSpawn             = "core.tool.spawn"
	MsgToolSkillSearch       = "core.tool.skill_search"
	MsgToolUseSkill          = "core.tool.use_skill"
	MsgToolSkillManage       = "core.tool.skill_manage"
	MsgToolPublishSkill      = "core.tool.publish_skill"
	MsgToolTeamTasks         = "core.tool.team_tasks"
	MsgToolTeamMessage       = "core.tool.team_message"

	// Skill evolution nudges (user-facing)
	MsgSkillNudgePostscript = "skill.nudge_postscript"
	MsgSkillNudge70Pct      = "skill.nudge_70_pct"
	MsgSkillNudge90Pct      = "skill.nudge_90_pct"
)
