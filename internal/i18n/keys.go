package i18n

// Message keys for gateway/HTTP error messages.
// Grouped by domain for easier maintenance.
const (
	// --- Common validation ---
	MsgRequired         = "error.required"          // "%s is required"
	MsgInvalidID        = "error.invalid_id"        // "invalid %s ID"
	MsgNotFound         = "error.not_found"         // "%s not found: %s"
	MsgAlreadyExists    = "error.already_exists"    // "%s already exists: %s"
	MsgInvalidRequest   = "error.invalid_request"   // "invalid request: %s"
	MsgInvalidJSON      = "error.invalid_json"      // "invalid JSON"
	MsgUnauthorized     = "error.unauthorized"      // "unauthorized"
	MsgPermissionDenied = "error.permission_denied" // "permission denied: %s"
	MsgInternalError    = "error.internal"          // "internal error: %s"
	MsgInvalidSlug      = "error.invalid_slug"      // "%s must be a valid slug (lowercase letters, numbers, hyphens only)"
	MsgFailedToList     = "error.failed_to_list"    // "failed to list %s"
	MsgFailedToCreate   = "error.failed_to_create"  // "failed to create %s: %s"
	MsgFailedToUpdate   = "error.failed_to_update"  // "failed to update %s: %s"
	MsgFailedToDelete   = "error.failed_to_delete"  // "failed to delete %s: %s"
	MsgFailedToSave     = "error.failed_to_save"    // "failed to save %s: %s"
	MsgInvalidUpdates   = "error.invalid_updates"   // "invalid updates"

	// --- Agent ---
	MsgAgentNotFound       = "error.agent_not_found"       // "agent not found: %s"
	MsgCannotDeleteDefault = "error.cannot_delete_default" // "cannot delete the default agent"
	MsgUserCtxRequired     = "error.user_ctx_required"     // "user context required"

	// --- Chat ---
	MsgRateLimitExceeded = "error.rate_limit"       // "rate limit exceeded — please wait"
	MsgNoUserMessage     = "error.no_user_message"  // "no user message found"
	MsgUserIDRequired    = "error.user_id_required" // "user_id is required"
	MsgMsgRequired       = "error.message_required" // "message is required"

	// --- Abort ---
	MsgAbortStopped         = "abort.stopped"          // "run stopped"
	MsgAbortForced          = "abort.forced"           // "run force-aborted (3s grace exceeded)"
	MsgAbortAlreadyAborting = "abort.already_aborting" // "abort already in progress"
	MsgAbortNotFound        = "abort.not_found"        // "run not found or already finished"
	MsgAbortUnauthorized    = "abort.unauthorized"     // "not authorized to abort this run"
	MsgAbortFailed          = "abort.failed"           // "failed to abort run: %s"

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
	MsgInvalidAuth           = "error.invalid_auth"            // "invalid authentication"
	MsgMsgsRequired          = "error.messages_required"       // "messages is required"
	MsgUserIDHeader          = "error.user_id_header"          // "X-GoClaw-User-Id header is required"
	MsgFileTooLarge          = "error.file_too_large"          // "file too large or invalid multipart form"
	MsgMissingFileField      = "error.missing_file_field"      // "missing 'file' field"
	MsgInvalidFilename       = "error.invalid_filename"        // "invalid filename"
	MsgChannelKeyReq         = "error.channel_key_required"    // "channel and key are required"
	MsgMethodNotAllowed      = "error.method_not_allowed"      // "method not allowed"
	MsgStreamingNotSupported = "error.streaming_not_supported" // "streaming not supported"
	MsgOwnerOnly             = "error.owner_only"              // "only owner can %s"
	MsgNoAccess              = "error.no_access"               // "no access to this %s"
	MsgAlreadySummoning      = "error.already_summoning"       // "agent is already being summoned"
	MsgSummoningUnavailable  = "error.summoning_unavailable"   // "summoning not available"
	MsgNoDescription         = "error.no_description"          // "agent has no description to resummon from"
	MsgSummonCancelled       = "info.summon_cancelled"         // "summon cancelled by user"
	MsgCannotCancel          = "error.cannot_cancel_summon"    // "agent is not being summoned"
	MsgInvalidPath           = "error.invalid_path"            // "invalid path"

	// --- Browser cookies ---
	MsgBrowserCookieTooMany            = "error.browser_cookie_too_many"            // "too many browser cookies in one sync request"
	MsgInvalidCookieURL                = "error.invalid_cookie_url"                 // "invalid cookie URL"
	MsgBrowserCookieValueTooLarge      = "error.browser_cookie_value_too_large"     // "cookie value too large"
	MsgBrowserCookieEncryptionRequired = "error.browser_cookie_encryption_required" // "browser cookie encryption is not configured"

	// --- Tenant backup / restore ---
	MsgRestoreNewModeRejectsTenantID = "error.restore_new_mode_rejects_tenant_id" // "mode=new uses tenant_slug; tenant_id is not accepted"

	// --- Scheduler ---
	MsgQueueFull    = "error.queue_full"    // "session queue is full"
	MsgShuttingDown = "error.shutting_down" // "gateway is shutting down, please retry shortly"

	// --- Provider ---
	MsgProviderReqFailed = "error.provider_request_failed" // "%s: request failed: %s"

	// --- Usage caps / pricing ---
	MsgUsageCapsListPoliciesFailed          = "usage_caps.list_policies_failed"
	MsgUsageCapPolicyValidationFailed       = "usage_caps.policy_validation_failed"
	MsgUsageCapPolicyManaged                = "usage_caps.policy_managed"
	MsgUsageCapsDeletePolicyFailed          = "usage_caps.delete_policy_failed"
	MsgUsageCapsUtilizationFailed           = "usage_caps.utilization_failed"
	MsgUsageCapsEventsFailed                = "usage_caps.events_failed"
	MsgUsagePricingSyncOpenRouterFailed     = "usage_pricing.sync_openrouter_failed"
	MsgUsagePricingStoreCatalogFailed       = "usage_pricing.store_catalog_failed"
	MsgUsagePricingListFailed               = "usage_pricing.list_failed"
	MsgUsagePricingProviderModelRequired    = "usage_pricing.provider_model_required"
	MsgUsagePricingOverrideValidationFailed = "usage_pricing.override_validation_failed"
	MsgUsagePricingListOverridesFailed      = "usage_pricing.list_overrides_failed"
	MsgUsagePricingDeleteOverrideFailed     = "usage_pricing.delete_override_failed"

	// --- Unknown method ---
	MsgUnknownMethod = "error.unknown_method" // "unknown method: %s"

	// --- Not implemented ---
	MsgNotImplemented = "error.not_implemented" // "%s not yet implemented"

	// --- Agent links ---
	MsgLinksNotConfigured = "error.links_not_configured" // "agent links not configured"
	MsgInvalidDirection   = "error.invalid_direction"    // "direction must be outbound, inbound, or bidirectional"
	MsgSourceTargetSame   = "error.source_target_same"   // "source and target must be different agents"
	MsgCannotDelegateOpen = "error.cannot_delegate_open" // "cannot delegate to open agents — only predefined agents can be delegation targets"
	MsgNoUpdatesProvided  = "error.no_updates_provided"  // "no updates provided"
	MsgInvalidLinkStatus  = "error.invalid_link_status"  // "status must be active or disabled"

	// --- Teams ---
	MsgTeamsNotConfigured   = "error.teams_not_configured"    // "teams not configured"
	MsgAgentIsTeamLead      = "error.agent_is_team_lead"      // "agent is already the team lead"
	MsgCannotRemoveTeamLead = "error.cannot_remove_team_lead" // "cannot remove the team lead"

	// --- Channels ---
	MsgCannotDeleteDefaultInst = "error.cannot_delete_default_inst" // "cannot delete default channel instance"
	MsgCannotRemoveLastWriter  = "error.cannot_remove_last_writer"  // "cannot remove the last file writer"

	// --- Skills ---
	MsgSkillsUpdateNotSupported = "error.skills_update_not_supported" // "skills.update not supported for file-based skills"
	MsgCannotResolveSkillID     = "error.cannot_resolve_skill_id"     // "cannot resolve skill ID for file-based skill"
	MsgInvalidVisibility        = "error.invalid_visibility"          // "invalid visibility %q: must be one of private, public"

	// --- Package updates (Phase 4+5) ---
	MsgPackageNotInstalled  = "packages.update.not_installed"     // "Package {name} is not installed"
	MsgPackageUpdateLocked  = "packages.update.locked"            // "Package {name} is being updated by another request"
	MsgReleaseNotFound      = "packages.update.release_not_found" // "Release {tag} not found for {repo}"
	MsgAssetNotFound        = "packages.update.asset_not_found"   // "No compatible asset for {os}/{arch}"
	MsgChecksumMismatch     = "packages.update.checksum_mismatch" // "Checksum mismatch for {name}"
	MsgUpdateSwapFailed     = "packages.update.swap_failed"       // "Failed to install {name}; previous version restored"
	MsgUpdateManifestDesync = "packages.update.manifest_desync"   // "Binary updated but manifest save failed — manual recovery required for {name}"
	MsgUpdateCacheStale     = "packages.update.cache_stale"       // "Updates cache stale; run refresh before applying an update"

	// Package update source labels
	MsgPackagesUpdatesSourceGithub = "packages.updates.source.github" // "GitHub"
	MsgPackagesUpdatesSourcePip    = "packages.updates.source.pip"    // "pip"
	MsgPackagesUpdatesSourceNpm    = "packages.updates.source.npm"    // "npm"

	// Package update availability messages
	MsgPackagesUpdatesUnavailablePip = "packages.updates.unavailable.pip" // "pip not installed on this system"
	MsgPackagesUpdatesUnavailableNpm = "packages.updates.unavailable.npm" // "npm not installed on this system"

	// Package update failure reasons
	MsgPackagesUpdatesReasonDependencyConflict = "packages.updates.reason.dependencyConflict" // "Dependency conflict"
	MsgPackagesUpdatesReasonPermission         = "packages.updates.reason.permission"         // "Permission denied"
	MsgPackagesUpdatesReasonNetwork            = "packages.updates.reason.network"            // "Network error"
	MsgPackagesUpdatesReasonNotFound           = "packages.updates.reason.notFound"           // "Package not found"
	MsgPackagesUpdatesReasonTargetMissing      = "packages.updates.reason.targetMissing"      // "Version not available"
	MsgPackagesUpdatesReasonExternallyManaged  = "packages.updates.reason.externallyManaged"  // "Environment externally managed"

	// Package update apk-specific labels (Phase 2b)
	MsgPackagesUpdatesSourceApk      = "packages.updates.source.apk"      // "apk"
	MsgPackagesUpdatesUnavailableApk = "packages.updates.unavailable.apk" // "apk not available on this system"

	// Package update apk-specific reasons (Phase 2b)
	MsgPackagesUpdatesReasonLocked            = "packages.updates.reason.locked"            // "Package database is locked"
	MsgPackagesUpdatesReasonDiskFull          = "packages.updates.reason.diskFull"          // "Disk full"
	MsgPackagesUpdatesReasonHelperUnavailable = "packages.updates.reason.helperUnavailable" // "Privileged helper unavailable"

	// --- Logs ---
	MsgInvalidLogAction = "error.invalid_log_action" // "action must be 'start' or 'stop'"

	// --- Config ---
	MsgRawConfigRequired     = "error.raw_config_required"      // "raw config is required"
	MsgRawPatchRequired      = "error.raw_patch_required"       // "raw patch is required"
	MsgConfigMasterScopeOnly = "error.config_master_scope_only" // "config.* methods are master-scope only"
	MsgMasterScopeRequired   = "error.master_scope_required"    // "this action requires master tenant scope"

	// --- Storage / File ---
	MsgCannotDeleteSkillsDir = "error.cannot_delete_skills_dir" // "cannot delete skills directories"
	MsgFailedToReadFile      = "error.failed_to_read_file"      // "failed to read file"
	MsgFileNotFound          = "error.file_not_found"           // "file not found"
	MsgInvalidVersion        = "error.invalid_version"          // "invalid version"
	MsgVersionNotFound       = "error.version_not_found"        // "version not found"
	MsgFailedToDeleteFile    = "error.failed_to_delete_file"    // "failed to delete"

	// --- OAuth ---
	MsgNoPendingOAuth    = "error.no_pending_oauth"     // "no pending OAuth flow"
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
	MsgEntityIDRequired       = "error.entity_id_required"        // "entity_id is required"
	MsgEntityFieldsRequired   = "error.entity_fields_required"    // "external_id, name, and entity_type are required"
	MsgTextRequired           = "error.text_required"             // "text is required"
	MsgProviderModelRequired  = "error.provider_model_required"   // "provider and model are required"
	MsgInvalidProviderOrModel = "error.invalid_provider_or_model" // "invalid provider or model"

	// --- Builtin tool descriptions (i18n key = core.tool.<name>) ---
	MsgToolReadFile             = "core.tool.read_file"
	MsgToolWriteFile            = "core.tool.write_file"
	MsgToolListFiles            = "core.tool.list_files"
	MsgToolEdit                 = "core.tool.edit"
	MsgToolExec                 = "core.tool.exec"
	MsgToolWebSearch            = "core.tool.web_search"
	MsgToolWebFetch             = "core.tool.web_fetch"
	MsgToolMemorySearch         = "core.tool.memory_search"
	MsgToolMemoryGet            = "core.tool.memory_get"
	MsgToolKGSearch             = "core.tool.knowledge_graph_search"
	MsgToolReadImage            = "core.tool.read_image"
	MsgToolReadDocument         = "core.tool.read_document"
	MsgToolCreateImage          = "core.tool.create_image"
	MsgToolReadAudio            = "core.tool.read_audio"
	MsgToolReadVideo            = "core.tool.read_video"
	MsgToolCreateVideo          = "core.tool.create_video"
	MsgToolCreateAudio          = "core.tool.create_audio"
	MsgToolTTS                  = "core.tool.tts"
	MsgToolBrowser              = "core.tool.browser"
	MsgToolSessionsList         = "core.tool.sessions_list"
	MsgToolSessionStatus        = "core.tool.session_status"
	MsgToolSessionsHistory      = "core.tool.sessions_history"
	MsgToolSessionsSend         = "core.tool.sessions_send"
	MsgToolMessage              = "core.tool.message"
	MessageCrossTargetForwarded = "tools.message.cross_target_forwarded"
	MsgToolCron                 = "core.tool.cron"
	MsgToolSpawn                = "core.tool.spawn"
	MsgToolSkillSearch          = "core.tool.skill_search"
	MsgToolUseSkill             = "core.tool.use_skill"
	MsgToolSkillManage          = "core.tool.skill_manage"
	MsgToolPublishSkill         = "core.tool.publish_skill"
	MsgToolTeamTasks            = "core.tool.team_tasks"

	// Skill evolution nudges (user-facing)
	MsgSkillNudgePostscript = "skill.nudge_postscript"
	MsgSkillNudge70Pct      = "skill.nudge_70_pct"
	MsgSkillNudge90Pct      = "skill.nudge_90_pct"

	// --- Tenants ---
	MsgInvalidRole = "error.invalid_role" // "invalid role: allowed values are owner, admin, operator, member, viewer"

	// --- TTS / Voices ---
	MsgTtsUnknownModel        = "error.tts_unknown_model"         // "unknown tts model: %s"
	MsgVoicesListFailed       = "error.voices_list_failed"        // "failed to list voices: %s"
	MsgTtsGeminiInvalidVoice  = "error.tts_gemini_invalid_voice"  // "invalid Gemini voice: %s"
	MsgTtsGeminiSpeakerLimit  = "error.tts_gemini_speaker_limit"  // "Gemini TTS supports at most 2 speakers"
	MsgTtsGeminiInvalidModel  = "error.tts_gemini_invalid_model"  // "invalid Gemini TTS model: %s"
	MsgTtsGeminiTextOnly      = "error.tts_gemini_text_only"      // "Gemini refused to generate audio; try simpler text without translation or commentary"
	MsgTtsParamOutOfRange     = "error.tts_param_out_of_range"    // "TTS param %q value %v is out of range [%v, %v]"
	MsgTtsParamUnknownKey     = "error.tts_param_unknown_key"     // "TTS param %q is not supported by this provider"
	MsgTtsMiniMaxVoicesFailed = "error.tts_minimax_voices_failed" // "failed to fetch MiniMax voices: %s"

	// --- STT ---
	MsgSTTAllProvidersFailed     = "error.stt_all_providers_failed"    // "All STT providers failed"
	MsgSTTLegacyConfigDeprecated = "warn.stt_legacy_config_deprecated" // "Legacy STT config deprecated; migrate to builtin_tools[stt]"
	MsgSTTWhatsappPrivacyWarning = "warn.stt_whatsapp_privacy"         // "Enabling STT for WhatsApp breaks end-to-end encryption for voice messages sent to this agent."
	MsgVoiceMessageFallback      = "channel.voice_message_fallback"    // "[Voice message]" — used when STT unavailable/disabled/timed-out

	// --- Contact merge ---
	MsgContactIDsRequired  = "error.contact_ids_required"  // "contact_ids is required"
	MsgMergeTargetRequired = "error.merge_target_required" // "exactly one of tenant_user_id or create_user is required"
	MsgTenantUserNotFound  = "error.tenant_user_not_found" // "tenant user not found"
	MsgTenantMismatch      = "error.tenant_mismatch"       // "tenant user does not belong to this tenant"
	MsgTenantScopeRequired = "error.tenant_scope_required" // "tenant scope is required for this operation"

	// --- Webhooks ---
	MsgWebhookAuthFailed              = "webhook.auth_failed"               // "webhook authentication failed"
	MsgWebhookHMACInvalid             = "webhook.hmac_invalid"              // "HMAC signature is invalid"
	MsgWebhookHMACTimestampSkew       = "webhook.hmac_timestamp_skew"       // "request timestamp outside acceptable window"
	MsgWebhookBearerRequiredHMAC      = "webhook.bearer_required_hmac"      // "this webhook requires HMAC authentication"
	MsgWebhookRevoked                 = "webhook.revoked"                   // "webhook has been revoked"
	MsgWebhookKindMismatch            = "webhook.kind_mismatch"             // "request kind does not match webhook configuration"
	MsgWebhookRateLimited             = "webhook.rate_limited"              // "webhook rate limit exceeded"
	MsgWebhookBodyTooLarge            = "webhook.body_too_large"            // "request body exceeds size limit"
	MsgWebhookIdempotencyConflict     = "webhook.idempotency_conflict"      // "idempotency key conflict: request body mismatch"
	MsgWebhookTenantMismatch          = "webhook.tenant_mismatch"           // "webhook tenant mismatch"
	MsgWebhookAgentNotFound           = "webhook.agent_not_found"           // "webhook agent not found"
	MsgWebhookChannelNotFound         = "webhook.channel_not_found"         // "webhook channel not found"
	MsgWebhookMediaSSRFBlocked        = "webhook.media_ssrf_blocked"        // "media URL blocked by SSRF policy"
	MsgWebhookMediaTooLarge           = "webhook.media_too_large"           // "media file exceeds size limit"
	MsgWebhookMediaMIMEDenied         = "webhook.media_mime_denied"         // "media MIME type is not allowed"
	MsgWebhookCallbackURLInvalid      = "webhook.callback_url_invalid"      // "callback URL is invalid or blocked"
	MsgWebhookLLMTimeout              = "webhook.llm_timeout"               // "LLM processing timed out"
	MsgWebhookLaneSaturated           = "webhook.lane_saturated"            // "webhook processing lane is at capacity"
	MsgWebhookLocalhostOnlyViolation  = "webhook.localhost_only_violation"  // "this webhook is restricted to localhost callers"
	MsgWebhookMediaChannelUnsupported = "webhook.media_channel_unsupported" // "channel does not support media attachments"
	MsgWebhookIPDenied                = "webhook.ip_denied"                 // "request origin is not in the IP allowlist"
	MsgWebhookEncryptionUnavailable   = "webhook.encryption_unavailable"    // "webhook encryption key not configured; set GOCLAW_ENCRYPTION_KEY to enable webhooks"

	// --- Workstation permissions ---
	MsgWorkstationCmdDenied    = "error.workstation_cmd_denied"     // "command denied by workstation policy: %s"
	MsgWorkstationEnvDenied    = "error.workstation_env_denied"     // "env var denied by policy: %s"
	MsgWorkstationInputInvalid = "error.workstation_input_invalid"  // "command contains invalid characters: %s"
	MsgWorkstationRateLimit    = "error.workstation_rate_limit"     // "workstation rate limit exceeded"
	MsgWorkstationPermNotFound = "error.workstation_perm_not_found" // "permission entry not found: %s"

	// --- Workstation activity (Phase 7) ---
	MsgWorkstationActivityTitle = "ui.workstations.activity.title"       // "Recent Activity"
	MsgWorkstationActionExec    = "ui.workstations.activity.action_exec" // "Exec"
	MsgWorkstationActionDeny    = "ui.workstations.activity.action_deny" // "Denied"

	// --- Workstation ---
	MsgWorkstationNotFound     = "error.workstation_not_found"     // "workstation not found: %s"
	MsgWorkstationKeyExists    = "error.workstation_key_exists"    // "workstation key already in use: %s"
	MsgInvalidBackend          = "error.invalid_backend"           // "invalid backend type: %s (must be ssh|docker)"
	MsgWorkstationInactive     = "error.workstation_inactive"      // "workstation is inactive: %s"
	MsgInvalidMetadataShape    = "error.invalid_metadata_shape"    // "invalid metadata for %s backend: %s"
	MsgWorkstationRequired     = "error.workstation_required"      // "no workstation bound to agent; pass workstation_id"
	MsgWorkstationAccessDenied = "error.workstation_access_denied" // "agent %s not authorized for workstation %s"
	MsgBackendNotReady         = "error.backend_not_ready"         // "workstation backend not ready: %s"

	// --- Hooks ---
	MsgHookInvalidMatcher          = "hook.invalid_matcher"           // "invalid matcher regex: %s"
	MsgHookCommandDisabledStandard = "hook.command_disabled_standard" // "command-type hooks are only available on Lite edition"
	MsgHookPromptRequiresMatcher   = "hook.prompt_requires_matcher"   // "prompt hooks require a matcher or if_expr (runaway-cost guard)"
	MsgHookCircuitBreakerTripped   = "hook.circuit_breaker_tripped"   // "hook auto-disabled after repeated failures"
	MsgHookBudgetExceeded          = "hook.budget_exceeded"           // "tenant hook token budget exceeded"
	MsgHookPerTurnCapReached       = "hook.per_turn_cap_reached"      // "hook invocation per-turn cap reached"
	MsgHookBuiltinReadOnly         = "hook.builtin_readonly"          // "builtin hooks are read-only except for the enabled toggle"

	// --- Grant env validation ---
	MsgGrantEnvDeniedKeys   = "error.grant_env_denied_keys"   // "env keys not allowed: %s"
	MsgGrantEnvValueInvalid = "error.grant_env_value_invalid" // "invalid env value: %s"
	MsgGrantEnvTooManyKeys  = "error.grant_env_too_many_keys" // "too many env keys: max 50"
	MsgGrantEnvRevealLimit  = "error.grant_env_reveal_limit"  // "rate limit exceeded for env reveal"

	// --- Git credential adapter (Phase 3+) ---
	MsgGitCredHostMismatch        = "error.git_cred_host_mismatch"         // "stored credential is for %s but command targets %s"
	MsgGitCredNoMatch             = "error.git_cred_no_match"              // "no git credential configured for host %s"
	MsgGitCredUnsupportedType     = "error.git_cred_unsupported_type"      // "git credential type %q is not supported"
	MsgGitCredTokenInvalid        = "error.git_cred_token_invalid"         // "stored git token is invalid or empty"
	MsgGitCredTokenControlChar    = "error.git_cred_token_control_char"    // "stored git token contains forbidden control characters"
	MsgGitCredHostUserinfoRejected = "error.git_cred_host_userinfo_rejected" // "git URL with embedded userinfo is rejected as ambiguous"
	MsgGitCredSSHPassphraseUnsupported = "error.git_cred_ssh_passphrase_unsupported" // "passphrase-protected SSH keys not supported in v1"
	MsgGitCredSSHKeyInvalid            = "error.git_cred_ssh_key_invalid"             // "SSH private key invalid: %s"
	MsgGitCredHostScopeRequired        = "error.git_cred_host_scope_required"         // "host_scope required for credential_type %s"
	MsgGitCredHostScopeInvalid         = "error.git_cred_host_scope_invalid"          // "host_scope %q is not a valid hostname"
	MsgGitCredBlobMissingField         = "error.git_cred_blob_missing_field"          // "blob missing required field %q"
	MsgGitCredUnsupportedCredType      = "error.git_cred_unsupported_cred_type"       // "credential_type %q is not supported"
)
