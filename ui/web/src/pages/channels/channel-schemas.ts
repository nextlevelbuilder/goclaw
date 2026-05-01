// Per-channel-type field definitions for credentials and config.
// Used by the form dialog to render proper UI fields instead of raw JSON.

export interface FieldDef {
  key: string;
  label: string;
  type: "text" | "password" | "number" | "boolean" | "select" | "tags" | "tristate" | "textarea" | "tool-select" | "skill-select";
  placeholder?: string;
  required?: boolean;
  defaultValue?: string | number | boolean | string[];
  options?: { value: string; label: string }[];
  help?: string;
  /** Only show this field when another field has a specific value (or one of several values) */
  showWhen?: { key: string; value: string | string[] };
  /** Disable this field when another field has a specific value */
  disabledWhen?: { key: string; value: string; hint?: string };
  /** Hide in an "Advanced" collapsible section — for rarely-needed fields */
  advanced?: boolean;
}

// --- Shared option lists ---

const blockReplyOptions = [
  { value: "inherit", label: "Inherit from gateway" },
  { value: "true", label: "Enabled" },
  { value: "false", label: "Disabled" },
];

const dmPolicyOptions = [
  { value: "pairing", label: "Pairing (require code)" },
  { value: "open", label: "Open (accept all)" },
  { value: "allowlist", label: "Allowlist only" },
  { value: "disabled", label: "Disabled" },
];

export const groupPolicyOptions = [
  { value: "open", label: "Open (accept all)" },
  { value: "pairing", label: "Pairing (require approval)" },
  { value: "allowlist", label: "Allowlist only" },
  { value: "disabled", label: "Disabled" },
];

const mentionModeOptions = [
  { value: "strict", label: "Default (follow @mention setting)" },
  { value: "yield", label: "Multi-bot (respond unless another bot is @mentioned)" },
];

// --- Credentials schemas ---

export const credentialsSchema: Record<string, FieldDef[]> = {
  telegram: [
    { key: "token", label: "Bot Token", type: "password", required: true, placeholder: "123456:ABC-DEF...", help: "From @BotFather" },
  ],
  discord: [
    { key: "token", label: "Bot Token", type: "password", required: true, placeholder: "Discord bot token" },
  ],
  slack: [
    { key: "bot_token", label: "Bot Token", type: "password", required: true, placeholder: "xoxb-...", help: "Bot User OAuth Token from your Slack app's OAuth & Permissions page" },
    { key: "app_token", label: "App-Level Token", type: "password", required: true, placeholder: "xapp-...", help: "App-Level Token with connections:write scope (required for Socket Mode)" },
    { key: "user_token", label: "User Token (Optional)", type: "password", required: false, placeholder: "xoxp-...", help: "Optional: User OAuth Token for custom bot identity. Leave empty to use default bot identity." },
  ],
  feishu: [
    { key: "app_id", label: "App ID", type: "text", required: true, placeholder: "cli_xxxxx" },
    { key: "app_secret", label: "App Secret", type: "password", required: true },
    { key: "encrypt_key", label: "Encrypt Key", type: "password", help: "For webhook event decryption", showWhen: { key: "connection_mode", value: "webhook" } },
    { key: "verification_token", label: "Verification Token", type: "password", help: "For webhook event verification", showWhen: { key: "connection_mode", value: "webhook" } },
  ],
  zalo_oa: [
    { key: "token", label: "OA Access Token", type: "password", required: true },
    { key: "webhook_secret", label: "Webhook Secret", type: "password" },
  ],
  zalo_personal: [],
  whatsapp: [],
  facebook: [
    { key: "page_access_token", label: "Page Access Token", type: "password", required: true, help: "From Facebook Developer Console → Your App → Messenger → Page Access Token" },
    { key: "app_secret", label: "App Secret", type: "password", required: true, help: "From Facebook Developer Console → Your App → Settings → Basic" },
    { key: "verify_token", label: "Webhook Verify Token", type: "password", required: true, help: "A secret string you choose, used to verify the webhook URL" },
  ],
  pancake: [
    { key: "api_key", label: "API Key", type: "password", required: true, help: "Pancake user-level API key from pages.fm account settings" },
    { key: "page_access_token", label: "Page Access Token", type: "password", required: true, help: "Page-level token from Pancake dashboard → Page Settings" },
    { key: "webhook_secret", label: "Webhook Secret (Optional)", type: "password", help: "HMAC-SHA256 secret for webhook signature verification. Leave empty to skip verification." },
  ],
};

// --- Pancake platform options ---

const pancakePlatformOptions = [
  { value: "facebook",    label: "Facebook" },
  { value: "instagram",   label: "Instagram" },
  { value: "threads",     label: "Threads (Beta)" },
  { value: "tiktok",      label: "TikTok" },
  { value: "youtube",     label: "YouTube (Beta)" },
  { value: "shopee",      label: "Shopee" },
  { value: "line",        label: "Line" },
  { value: "google",      label: "Google" },
  { value: "chat_plugin", label: "Chat Plugin" },
  { value: "lazada",      label: "Lazada" },
  { value: "tokopedia",   label: "Tokopedia" },
];

const tiktokTypeOptions = [
  { value: "livestream", label: "Livestream AIO" },
  { value: "messaging",  label: "Business Messaging" },
  { value: "shop",       label: "TikTok Shop" },
];

// --- Config schemas ---

export const configSchema: Record<string, FieldDef[]> = {
  telegram: [
    { key: "api_server", label: "API Server URL", type: "text", placeholder: "http://127.0.0.1:8081", help: "Custom Telegram Bot API server for large file uploads (up to 2GB). Leave empty for default." },
    { key: "proxy", label: "HTTP Proxy", type: "text", placeholder: "http://proxy:8080", help: "Route bot traffic through an HTTP proxy" },
    { key: "dm_policy", label: "DM Policy", type: "select", options: dmPolicyOptions, defaultValue: "pairing" },
    { key: "group_policy", label: "Group Policy", type: "select", options: groupPolicyOptions, defaultValue: "pairing" },
    { key: "mention_mode", label: "Group Response Behavior", type: "select", options: mentionModeOptions, defaultValue: "strict", help: "How the bot decides when to respond in groups with multiple bots." },
    { key: "require_mention", label: "Require @mention in groups", type: "boolean", defaultValue: true, disabledWhen: { key: "mention_mode", value: "yield", hint: "fieldConfig.require_mention.disabledHint" } },
    { key: "history_limit", label: "Group History Limit", type: "number", defaultValue: 50, help: "Max pending group messages for context (0 = disabled)" },
    { key: "dm_stream", label: "DM Streaming", type: "boolean", defaultValue: true, help: "Stream response progressively in DMs" },
    { key: "group_stream", label: "Group Streaming", type: "boolean", defaultValue: false, help: "Stream response progressively in groups" },
    { key: "draft_transport", label: "Draft Preview", type: "boolean", defaultValue: true, help: "Use stealth draft preview for answer stream in DMs — no notification per edit (requires DM Streaming)" },
    { key: "reasoning_stream", label: "Show Reasoning", type: "boolean", defaultValue: true, help: "Display AI thinking as a separate message before the answer (requires streaming)" },
    { key: "reaction_level", label: "Reaction Level", type: "select", options: [{ value: "off", label: "Off" }, { value: "minimal", label: "Minimal" }, { value: "full", label: "Full" }], defaultValue: "full" },
    { key: "media_max_mb", label: "Max Media Size (MB)", type: "number", defaultValue: 20, help: "Default: 20 MB (cloud API). Increase when using local Bot API server." },
    { key: "link_preview", label: "Link Preview", type: "boolean", defaultValue: true },
    { key: "allow_from", label: "Allowed Users", type: "tags", help: "User IDs or @usernames, one per line or comma-separated" },
    { key: "block_reply", label: "Block Reply", type: "select", options: blockReplyOptions, defaultValue: "inherit", help: "Deliver intermediate text during tool iterations" },
  ],
  discord: [
    { key: "dm_policy", label: "DM Policy", type: "select", options: dmPolicyOptions, defaultValue: "pairing" },
    { key: "group_policy", label: "Group Policy", type: "select", options: groupPolicyOptions, defaultValue: "pairing" },
    { key: "require_mention", label: "Require @mention in groups", type: "boolean", defaultValue: true },
    { key: "history_limit", label: "Group History Limit", type: "number", defaultValue: 50, help: "Max pending group messages for context (0 = disabled)" },
    { key: "allow_from", label: "Allowed Users", type: "tags", help: "Discord user IDs" },
    { key: "block_reply", label: "Block Reply", type: "select", options: blockReplyOptions, defaultValue: "inherit", help: "Deliver intermediate text during tool iterations" },
  ],
  slack: [
    { key: "dm_policy", label: "DM Policy", type: "select", options: dmPolicyOptions, defaultValue: "pairing", help: "How to handle direct messages from unknown users" },
    { key: "group_policy", label: "Group Policy", type: "select", options: groupPolicyOptions, defaultValue: "pairing", help: "How to handle messages from channels/groups" },
    { key: "require_mention", label: "Require @mention in channels", type: "boolean", defaultValue: true, help: "Bot only responds when explicitly @mentioned in channels (recommended)" },
    { key: "history_limit", label: "Group History Limit", type: "number", defaultValue: 50, help: "Max pending group messages for context (0 = disabled)" },
    { key: "dm_stream", label: "DM Streaming", type: "boolean", defaultValue: true, help: "Progressively edit placeholder message as LLM generates (DMs)" },
    { key: "group_stream", label: "Group Streaming", type: "boolean", defaultValue: false, help: "Progressively edit placeholder message as LLM generates (channels)" },
    { key: "native_stream", label: "Native Streaming (Agents & AI Apps)", type: "boolean", defaultValue: false, help: "Use Slack's ChatStreamer API for native streaming. Falls back to edit-in-place if unavailable." },
    { key: "debounce_delay", label: "Debounce Delay (ms)", type: "number", defaultValue: 300, help: "Milliseconds to wait before dispatching rapid messages. Set 0 to disable." },
    { key: "thread_ttl", label: "Thread Participation TTL (hours)", type: "number", defaultValue: 24, help: "Hours before bot stops auto-replying in threads it participated in. 0 = always require @mention." },
    { key: "reaction_level", label: "Reaction Level", type: "select", options: [{ value: "off", label: "Off" }, { value: "minimal", label: "Minimal (thinking + done)" }, { value: "full", label: "Full (all status emoji)" }], defaultValue: "off", help: "Show emoji reactions on user messages during agent processing" },
    { key: "allow_from", label: "Allowed Users", type: "tags", help: "Slack user IDs (U...) allowed to interact; empty = no allowlist filter" },
    { key: "block_reply", label: "Block Reply", type: "select", options: blockReplyOptions, defaultValue: "inherit", help: "Deliver intermediate text during tool iterations" },
  ],
  feishu: [
    { key: "domain", label: "Domain", type: "select", options: [{ value: "lark", label: "Lark (Global)" }, { value: "feishu", label: "Feishu (China)" }], defaultValue: "lark" },
    { key: "connection_mode", label: "Connection Mode", type: "select", options: [{ value: "websocket", label: "WebSocket (recommended)" }, { value: "webhook", label: "Webhook (requires public endpoint)" }], defaultValue: "websocket", help: "WebSocket needs no public IP — outbound connection only" },
    { key: "webhook_port", label: "Webhook Port", type: "number", defaultValue: 0, help: "0 = share main gateway port (recommended)", showWhen: { key: "connection_mode", value: "webhook" } },
    { key: "webhook_path", label: "Webhook Path", type: "text", defaultValue: "/feishu/events", help: "Path on main server for Lark events", showWhen: { key: "connection_mode", value: "webhook" } },
    { key: "dm_policy", label: "DM Policy", type: "select", options: dmPolicyOptions, defaultValue: "pairing" },
    { key: "group_policy", label: "Group Policy", type: "select", options: groupPolicyOptions, defaultValue: "pairing" },
    { key: "require_mention", label: "Require @mention in groups", type: "boolean", defaultValue: true },
    { key: "topic_session_mode", label: "Topic Session Mode", type: "select", options: [{ value: "disabled", label: "Disabled" }, { value: "enabled", label: "Enabled" }], defaultValue: "disabled", help: "Use thread root_id for session isolation" },
    { key: "history_limit", label: "Group History Limit", type: "number", help: "Max pending group messages for context (0 = disabled)" },
    { key: "render_mode", label: "Render Mode", type: "select", options: [{ value: "auto", label: "Auto" }, { value: "raw", label: "Raw" }, { value: "card", label: "Card" }], defaultValue: "auto" },
    { key: "text_chunk_limit", label: "Text Chunk Limit", type: "number", defaultValue: 4000, help: "Max characters per message" },
    { key: "media_max_mb", label: "Max Media Size (MB)", type: "number", defaultValue: 30, help: "Max inbound media download size" },
    { key: "reaction_level", label: "Reaction Level", type: "select", options: [{ value: "off", label: "Off" }, { value: "minimal", label: "Minimal" }, { value: "full", label: "Full" }], defaultValue: "off", help: "Typing emoji reaction on user messages while bot is processing" },
    { key: "allow_from", label: "Allowed Users", type: "tags", help: "Lark open_ids (ou_...)" },
    { key: "group_allow_from", label: "Group Allowed Users", type: "tags", help: "Separate allowlist for group senders" },
    { key: "block_reply", label: "Block Reply", type: "select", options: blockReplyOptions, defaultValue: "inherit", help: "Deliver intermediate text during tool iterations" },
  ],
  zalo_oa: [
    { key: "dm_policy", label: "DM Policy", type: "select", options: dmPolicyOptions, defaultValue: "pairing" },
    { key: "webhook_url", label: "Webhook URL", type: "text", placeholder: "https://..." },
    { key: "media_max_mb", label: "Max Media Size (MB)", type: "number", defaultValue: 5 },
    { key: "allow_from", label: "Allowed Users", type: "tags", help: "Zalo user IDs" },
    { key: "block_reply", label: "Block Reply", type: "select", options: blockReplyOptions, defaultValue: "inherit", help: "Deliver intermediate text during tool iterations" },
  ],
  zalo_personal: [
    { key: "dm_policy", label: "DM Policy", type: "select", options: dmPolicyOptions, defaultValue: "allowlist" },
    { key: "group_policy", label: "Group Policy", type: "select", options: groupPolicyOptions, defaultValue: "allowlist" },
    { key: "require_mention", label: "Require @mention in groups", type: "boolean", defaultValue: true },
    { key: "allow_from", label: "Allowed Users", type: "tags", help: "Zalo user IDs or group IDs" },
    { key: "block_reply", label: "Block Reply", type: "select", options: blockReplyOptions, defaultValue: "inherit", help: "Deliver intermediate text during tool iterations" },
  ],
  whatsapp: [
    { key: "dm_policy", label: "DM Policy", type: "select", options: dmPolicyOptions, defaultValue: "pairing" },
    { key: "group_policy", label: "Group Policy", type: "select", options: groupPolicyOptions, defaultValue: "pairing" },
    { key: "require_mention", label: "Require @Mention in Groups", type: "boolean", help: "Only respond in group chats when the bot is explicitly @mentioned" },
    { key: "allow_from", label: "Allowed Users", type: "tags", help: "WhatsApp user IDs" },
    { key: "block_reply", label: "Block Reply", type: "select", options: blockReplyOptions, defaultValue: "inherit", help: "Deliver intermediate text during tool iterations" },
  ],
  facebook: [
    { key: "page_id", label: "Page ID", type: "text", required: true, help: "Facebook Page numeric ID" },
    { key: "features.comment_reply", label: "Comment Auto-Reply", type: "boolean", defaultValue: false },
    { key: "features.messenger_auto_reply", label: "Messenger Auto-Reply", type: "boolean", defaultValue: false },
    { key: "features.first_inbox", label: "First Inbox DM", type: "boolean", defaultValue: false, help: "Send a one-time DM to commenters after their first comment reply" },
    { key: "comment_reply_options.include_post_context", label: "Include Post Context", type: "boolean", defaultValue: false, help: "Fetch original post content for comment context" },
    { key: "comment_reply_options.max_thread_depth", label: "Max Thread Depth", type: "number", defaultValue: 10 },
    { key: "messenger_options.session_timeout", label: "Messenger Session Timeout", type: "text", placeholder: "e.g. 30m" },
    { key: "post_context_cache_ttl", label: "Post Cache TTL", type: "text", placeholder: "e.g. 15m" },
    { key: "first_inbox_message", label: "First Inbox DM Text", type: "textarea", help: "Custom DM sent to first-time commenters. Defaults to Vietnamese if empty." },
    { key: "allow_from", label: "Allowed Users", type: "tags", help: "Facebook user IDs" },
    { key: "block_reply", label: "Block Reply", type: "select", options: blockReplyOptions, defaultValue: "inherit" },
  ],
  pancake: [
    { key: "page_id", label: "Page ID", type: "text", required: true, help: "Pancake internal page ID (numeric, from Pancake dashboard)" },
    { key: "webhook_page_id", label: "Webhook Page ID", type: "text", help: "Only needed when the native platform page ID in webhooks differs from the Pancake page ID above (rare). Leave empty if both are the same.", advanced: true },
    { key: "platform", label: "Platform", type: "select", required: true, defaultValue: "", options: pancakePlatformOptions, help: "Select the platform this Pancake page serves." },
    { key: "tiktok_type", label: "TikTok Type", type: "select", options: tiktokTypeOptions, showWhen: { key: "platform", value: "tiktok" }, help: "Select the TikTok account type for this page" },
    { key: "features.inbox_reply", label: "Inbox Auto-Reply", type: "boolean", defaultValue: true },
    { key: "features.comment_reply", label: "Comment Reply", type: "boolean", defaultValue: false,
      showWhen: { key: "platform", value: ["facebook", "instagram", "threads", "tiktok", "youtube"] } },
    { key: "features.private_reply", label: "Private Reply (Comment → DM)", type: "boolean", defaultValue: false,
      help: "Send a one-time DM to commenters after the public reply. Facebook/Instagram only. Meta allows DM within 7 days of the comment.",
      showWhen: { key: "platform", value: ["facebook", "instagram"] } },
    { key: "private_reply_message", label: "DM Message", type: "textarea",
      help: "Supports {{commenter_name}} and {{post_title}}. Empty = default English text.",
      placeholder: "Hi {{commenter_name}}! Thanks for commenting on \"{{post_title}}\". How can I help?",
      showWhen: { key: "features.private_reply", value: "true" } },
    { key: "features.auto_react", label: "Auto-React (Like) Comments", type: "boolean",
      defaultValue: false,
      showWhen: { key: "platform", value: "facebook" },
      help: "Automatically like Facebook comments. Set webhook_secret for security." },
    { key: "auto_react_options.allow_post_ids", label: "Auto-React: Allow Post IDs", type: "tags",
      showWhen: { key: "features.auto_react", value: "true" },
      help: "Only react on these post IDs. Empty = all posts. Deny list overrides." },
    { key: "auto_react_options.deny_post_ids", label: "Auto-React: Deny Post IDs", type: "tags",
      showWhen: { key: "features.auto_react", value: "true" },
      help: "Never react on these post IDs." },
    { key: "auto_react_options.allow_user_ids", label: "Auto-React: Allow User IDs", type: "tags",
      showWhen: { key: "features.auto_react", value: "true" },
      help: "Only react to comments from these user IDs. Empty = all users. Deny list overrides." },
    { key: "auto_react_options.deny_user_ids", label: "Auto-React: Deny User IDs", type: "tags",
      showWhen: { key: "features.auto_react", value: "true" },
      help: "Never react to comments from these user IDs." },
    { key: "allow_from", label: "Allowed Users", type: "tags", help: "Sender IDs to whitelist. Empty = accept all." },
    { key: "block_reply", label: "Block Reply", type: "select", options: blockReplyOptions, defaultValue: "inherit" },
  ],
};

// --- Group override schema (Telegram per-group/topic overrides) ---
// Uses tristate fields: undefined = inherit from parent, value = override.
// tristate without options → Inherit/Yes/No (boolean).
// tristate with options → Inherit + custom options (string).

export const groupOverrideSchema: FieldDef[] = [
  { key: "group_policy", label: "Group Policy", type: "tristate", options: groupPolicyOptions },
  { key: "mention_mode", label: "Mention Mode", type: "tristate", options: mentionModeOptions },
  { key: "require_mention", label: "Require @mention", type: "tristate", disabledWhen: { key: "mention_mode", value: "yield", hint: "fieldConfig.require_mention.disabledHint" } },
  { key: "enabled", label: "Enabled", type: "tristate" },
  { key: "allow_from", label: "Allowed Users", type: "tags", placeholder: "User IDs, one per line", help: "Restrict which users can interact in this group" },
  { key: "skills", label: "Skills Filter", type: "skill-select", help: "Limit available skills for this group" },
  { key: "tools", label: "Tool Allowlist", type: "tool-select", help: "Restrict which tools the agent can use in this group" },
  { key: "system_prompt", label: "System Prompt", type: "textarea", placeholder: "Additional system prompt for this group..." },
];

// --- Required API scopes per channel type ---
// Displayed as a help reference when creating/configuring a channel.

export interface ScopeEntry {
  scope: string;
  note?: string; // e.g. "Range: All members"
}

export const requiredScopes: Partial<Record<string, ScopeEntry[]>> = {
  feishu: [
    { scope: "application:application:self_manage" },
    { scope: "application:bot.menu:write" },
    { scope: "cardkit:card:read" },
    { scope: "cardkit:card:write" },
    { scope: "contact:contact.base:readonly", note: "Range: All members" },
    { scope: "contact:user.base:readonly", note: "Range: All members" },
    { scope: "contact:user.employee_id:readonly", note: "Range: All members" },
    { scope: "event:ip_list" },
    { scope: "im:chat.members:bot_access" },
    { scope: "im:chat.members:read", note: "Required for list_group_members tool" },
    { scope: "im:message" },
    { scope: "im:message.group_at_msg:readonly" },
    { scope: "im:message.p2p_msg:readonly" },
    { scope: "im:message:readonly" },
    { scope: "im:message:send_as_bot" },
    { scope: "im:resource" },
  ],
};

// --- Post-create wizard configuration ---
// Channels with multi-step create flows (e.g. auth then config).
// Channels not listed here use the default single-step create.

export interface WizardConfig {
  /** Post-create step sequence */
  steps: ("auth" | "config")[];
  /** Custom label for the create button */
  createLabel?: string;
  /** Info banner shown on the form step during create */
  formBanner?: string;
  /** Config field keys excluded from form step (handled in wizard config step) */
  excludeConfigFields?: string[];
}

export const wizardConfig: Partial<Record<string, WizardConfig>> = {
  zalo_personal: {
    steps: ["auth", "config"],
    createLabel: "wizard.zaloPersonal.createLabel",
    formBanner: "wizard.zaloPersonal.formBanner",
    excludeConfigFields: ["allow_from"],
  },
  whatsapp: {
    steps: ["auth"],
    createLabel: "wizard.whatsapp.createLabel",
    formBanner: "wizard.whatsapp.formBanner",
  },
};
