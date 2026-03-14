// Per-channel-type field definitions for credentials and config.
// Used by the form dialog to render proper UI fields instead of raw JSON.

export interface FieldDef {
  key: string;
  label: string;
  type: "text" | "password" | "number" | "boolean" | "select" | "tags";
  placeholder?: string;
  required?: boolean;
  defaultValue?: string | number | boolean | string[];
  options?: { value: string; label: string }[];
  help?: string;
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
    { key: "encrypt_key", label: "Encrypt Key", type: "password", help: "For webhook mode" },
    { key: "verification_token", label: "Verification Token", type: "password", help: "For webhook mode" },
  ],
  zalo_oa: [
    { key: "token", label: "OA Access Token", type: "password", required: true },
    { key: "webhook_secret", label: "Webhook Secret", type: "password" },
  ],
  zalo_personal: [],
  whatsapp: [
    { key: "bridge_url", label: "Bridge URL", type: "text", required: true, placeholder: "http://bridge:3000" },
  ],
};

// --- Config schemas ---

export const configSchema: Record<string, FieldDef[]> = {
  telegram: [
    { key: "api_server", label: "API Server URL", type: "text", placeholder: "http://127.0.0.1:8081", help: "Custom Telegram Bot API server for large file uploads (up to 2GB). Leave empty for default." },
    { key: "proxy", label: "HTTP Proxy", type: "text", placeholder: "http://proxy:8080", help: "Route bot traffic through an HTTP proxy" },
    { key: "dm_policy", label: "DM Policy", type: "select", options: dmPolicyOptions, defaultValue: "pairing" },
    { key: "group_policy", label: "Group Policy", type: "select", options: groupPolicyOptions, defaultValue: "pairing" },
    { key: "require_mention", label: "Require @mention in groups", type: "boolean", defaultValue: true },
    { key: "history_limit", label: "Group History Limit", type: "number", defaultValue: 50, help: "Max pending group messages for context (0 = disabled)" },
    { key: "dm_stream", label: "DM Streaming", type: "boolean", defaultValue: true, help: "Stream response progressively in DMs" },
    { key: "group_stream", label: "Group Streaming", type: "boolean", defaultValue: false, help: "Stream response progressively in groups" },
    { key: "draft_transport", label: "Draft Preview", type: "boolean", defaultValue: true, help: "Use stealth draft preview for answer stream in DMs — no notification per edit (requires DM Streaming)" },
    { key: "reasoning_stream", label: "Show Reasoning", type: "boolean", defaultValue: true, help: "Display AI thinking as a separate message before the answer (requires streaming)" },
    { key: "reaction_level", label: "Reaction Level", type: "select", options: [{ value: "off", label: "Off" }, { value: "minimal", label: "Minimal" }, { value: "full", label: "Full" }], defaultValue: "full" },
    { key: "media_max_mb", label: "Max Media Size (MB)", type: "number", defaultValue: 20, help: "Default: 20 MB (cloud API). Increase when using local Bot API server." },
    { key: "link_preview", label: "Link Preview", type: "boolean", defaultValue: true },
    { key: "allow_from", label: "Allowed Users", type: "tags", help: "User IDs or @usernames, one per line" },
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
    { key: "domain", label: "Domain", type: "select", options: [{ value: "lark", label: "Lark (global) — webhook only" }, { value: "feishu", label: "Feishu (China)" }], defaultValue: "lark" },
    { key: "connection_mode", label: "Connection Mode", type: "select", options: [{ value: "webhook", label: "Webhook" }, { value: "websocket", label: "WebSocket (Feishu only)" }], defaultValue: "webhook", help: "Lark Global only supports Webhook" },
    { key: "webhook_port", label: "Webhook Port", type: "number", defaultValue: 0, help: "0 = share main gateway port (recommended)" },
    { key: "webhook_path", label: "Webhook Path", type: "text", defaultValue: "/feishu/events", help: "Path on main server for Lark events" },
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
    { key: "allow_from", label: "Allowed Users", type: "tags", help: "WhatsApp user IDs" },
    { key: "block_reply", label: "Block Reply", type: "select", options: blockReplyOptions, defaultValue: "inherit", help: "Deliver intermediate text during tool iterations" },
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
    createLabel: "Create & Authenticate",
    formBanner: "After creating, you'll authenticate via QR code and configure allowed users.",
    excludeConfigFields: ["allow_from"],
  },
};
