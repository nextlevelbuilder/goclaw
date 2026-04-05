// Per-channel-type field definitions for credentials and config.
// Simplified from web UI — only telegram and discord supported in Lite edition.

export interface FieldDef {
  key: string
  label: string
  type: 'text' | 'password' | 'number' | 'boolean' | 'select' | 'tags'
  placeholder?: string
  required?: boolean
  defaultValue?: string | number | boolean | string[]
  options?: { value: string; label: string }[]
  help?: string
  showWhen?: { key: string; value: string }
  disabledWhen?: { key: string; value: string; hint?: string }
}

// --- Shared option lists ---

const dmPolicyOptions = [
  { value: 'pairing', label: 'Pairing (require code)' },
  { value: 'open', label: 'Open (accept all)' },
  { value: 'allowlist', label: 'Allowlist only' },
  { value: 'disabled', label: 'Disabled' },
]

const groupPolicyOptions = [
  { value: 'open', label: 'Open (accept all)' },
  { value: 'pairing', label: 'Pairing (require approval)' },
  { value: 'allowlist', label: 'Allowlist only' },
  { value: 'disabled', label: 'Disabled' },
]

const mentionModeOptions = [
  { value: 'strict', label: 'Default (follow @mention setting)' },
  { value: 'yield', label: 'Multi-bot (respond unless another bot is @mentioned)' },
]

const blockReplyOptions = [
  { value: 'inherit', label: 'Inherit from gateway' },
  { value: 'true', label: 'Enabled' },
  { value: 'false', label: 'Disabled' },
]

const reactionLevelOptions = [
  { value: 'off', label: 'Off' },
  { value: 'minimal', label: 'Minimal' },
  { value: 'full', label: 'Full' },
]

// --- Credentials schemas ---

export const credentialsSchema: Record<string, FieldDef[]> = {
  telegram: [
    { key: 'token', label: 'Bot Token', type: 'password', required: true, placeholder: '123456:ABC-DEF...', help: 'From @BotFather' },
  ],
  discord: [
    { key: 'token', label: 'Bot Token', type: 'password', required: true, placeholder: 'Discord bot token' },
  ],
}

// --- Config schemas ---

export const configSchema: Record<string, FieldDef[]> = {
  telegram: [
    { key: 'api_server', label: 'API Server URL', type: 'text', placeholder: 'http://127.0.0.1:8081', help: 'Custom Bot API server for large file uploads. Leave empty for default.' },
    { key: 'proxy', label: 'HTTP Proxy', type: 'text', placeholder: 'http://proxy:8080', help: 'Route bot traffic through an HTTP proxy' },
    { key: 'dm_policy', label: 'DM Policy', type: 'select', options: dmPolicyOptions, defaultValue: 'pairing' },
    { key: 'group_policy', label: 'Group Policy', type: 'select', options: groupPolicyOptions, defaultValue: 'pairing' },
    { key: 'mention_mode', label: 'Group Response Behavior', type: 'select', options: mentionModeOptions, defaultValue: 'strict', help: 'How the bot decides when to respond in groups with multiple bots.' },
    { key: 'require_mention', label: 'Require @mention in groups', type: 'boolean', defaultValue: true, disabledWhen: { key: 'mention_mode', value: 'yield', hint: 'Disabled in multi-bot mode' } },
    { key: 'history_limit', label: 'Group History Limit', type: 'number', defaultValue: 50, help: 'Max pending group messages for context (0 = disabled)' },
    { key: 'dm_stream', label: 'DM Streaming', type: 'boolean', defaultValue: true, help: 'Stream response progressively in DMs' },
    { key: 'group_stream', label: 'Group Streaming', type: 'boolean', defaultValue: false, help: 'Stream response progressively in groups' },
    { key: 'draft_transport', label: 'Draft Preview', type: 'boolean', defaultValue: true, help: 'Stealth draft preview for streaming in DMs (requires DM Streaming)' },
    { key: 'reasoning_stream', label: 'Show Reasoning', type: 'boolean', defaultValue: true, help: 'Display AI thinking before the answer (requires streaming)' },
    { key: 'reaction_level', label: 'Reaction Level', type: 'select', options: reactionLevelOptions, defaultValue: 'full' },
    { key: 'media_max_mb', label: 'Max Media Size (MB)', type: 'number', defaultValue: 20, help: 'Default: 20 MB. Increase when using local Bot API server.' },
    { key: 'link_preview', label: 'Link Preview', type: 'boolean', defaultValue: true },
    { key: 'allow_from', label: 'Allowed Users', type: 'tags', help: 'User IDs or @usernames, one per line or comma-separated' },
    { key: 'block_reply', label: 'Block Reply', type: 'select', options: blockReplyOptions, defaultValue: 'inherit', help: 'Deliver intermediate text during tool iterations' },
  ],
  discord: [
    { key: 'dm_policy', label: 'DM Policy', type: 'select', options: dmPolicyOptions, defaultValue: 'pairing' },
    { key: 'group_policy', label: 'Group Policy', type: 'select', options: groupPolicyOptions, defaultValue: 'pairing' },
    { key: 'require_mention', label: 'Require @mention in groups', type: 'boolean', defaultValue: true },
    { key: 'history_limit', label: 'Group History Limit', type: 'number', defaultValue: 50, help: 'Max pending group messages for context (0 = disabled)' },
    { key: 'allow_from', label: 'Allowed Users', type: 'tags', help: 'Discord user IDs' },
    { key: 'block_reply', label: 'Block Reply', type: 'select', options: blockReplyOptions, defaultValue: 'inherit', help: 'Deliver intermediate text during tool iterations' },
  ],
}

// Essential config keys shown in the General tab (policies section)
export const ESSENTIAL_CONFIG_KEYS: Record<string, string[]> = {
  _default: ['dm_policy', 'group_policy', 'require_mention'],
  telegram: ['dm_policy', 'group_policy', 'mention_mode', 'require_mention'],
}

// Advanced config grouping keys (for ChannelAdvancedDialog)
export const NETWORK_KEYS = new Set(['api_server', 'proxy'])
export const LIMITS_KEYS = new Set(['history_limit', 'media_max_mb'])
export const STREAMING_KEYS = new Set(['dm_stream', 'group_stream', 'draft_transport', 'reasoning_stream'])
export const BEHAVIOR_KEYS = new Set(['reaction_level', 'link_preview', 'block_reply'])
export const ACCESS_KEYS = new Set(['allow_from'])
