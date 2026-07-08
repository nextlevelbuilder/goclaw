package config

// ChatBehaviorConfig controls optional human-like channel delivery behavior.
// Pointer fields allow per-channel overrides to inherit gateway defaults.
type ChatBehaviorConfig struct {
	Enabled             *bool                      `json:"enabled,omitempty"`
	IntermediateReplies *IntermediateRepliesConfig `json:"intermediate_replies,omitempty"`
	QuickAck            *QuickAckConfig            `json:"quick_ack,omitempty"`
	FinalSplit          *FinalSplitConfig          `json:"final_split,omitempty"`
}

// QuickAckConfig controls one short acknowledgement before longer non-streaming runs.
type QuickAckConfig struct {
	Enabled    *bool    `json:"enabled,omitempty"`
	Mode       *string  `json:"mode,omitempty"`
	MinDelayMs *int     `json:"min_delay_ms,omitempty"`
	Provider   string   `json:"provider,omitempty"`
	Model      string   `json:"model,omitempty"`
	TimeoutMs  *int     `json:"timeout_ms,omitempty"`
	MaxTokens  *int     `json:"max_tokens,omitempty"`
	MaxChars   *int     `json:"max_chars,omitempty"`
	Templates  []string `json:"templates,omitempty"`
}

// IntermediateRepliesConfig controls delivery-only progress messages during tool phases.
type IntermediateRepliesConfig struct {
	Enabled   *bool   `json:"enabled,omitempty"`
	Mode      *string `json:"mode,omitempty"`
	Provider  string  `json:"provider,omitempty"`
	Model     string  `json:"model,omitempty"`
	TimeoutMs *int    `json:"timeout_ms,omitempty"`
	MaxTokens *int    `json:"max_tokens,omitempty"`
	MaxChars  *int    `json:"max_chars,omitempty"`
}

// FinalSplitConfig controls semantic splitting of final channel replies.
type FinalSplitConfig struct {
	Enabled     *bool `json:"enabled,omitempty"`
	MinChars    *int  `json:"min_chars,omitempty"`
	MaxMessages *int  `json:"max_messages,omitempty"`
	DelayMs     *int  `json:"delay_ms,omitempty"`
}

// PendingCompactionConfig configures LLM-based compaction of pending group messages.
// When a group accumulates more than Threshold pending messages, older messages are
// summarized by an LLM and replaced with a compact summary, keeping KeepRecent raw messages.
type PendingCompactionConfig struct {
	Threshold  int    `json:"threshold,omitempty"`   // trigger compaction when entries exceed this (default 200)
	KeepRecent int    `json:"keep_recent,omitempty"` // keep this many recent raw messages after compaction (default 40)
	MaxTokens  int    `json:"max_tokens,omitempty"`  // max output tokens for LLM summarization (default 4096)
	Provider   string `json:"provider,omitempty"`    // LLM provider name (e.g. "openai"); empty = use agent's provider
	Model      string `json:"model,omitempty"`       // model for summarization; empty = use agent's model
}

// TelegramManagerConfig controls the hidden Telegram management tool for this channel.
// It is intentionally channel-scoped instead of part of the global builtin tool profiles.
type TelegramManagerConfig struct {
	Enabled        bool     `json:"enabled,omitempty"`
	AllowedActions []string `json:"allowed_actions,omitempty"`
}

// ChannelsConfig contains per-channel configuration.
type ChannelsConfig struct {
	Telegram          TelegramConfig           `json:"telegram"`
	Discord           DiscordConfig            `json:"discord"`
	Slack             SlackConfig              `json:"slack"`
	WhatsApp          WhatsAppConfig           `json:"whatsapp"`
	Zalo              ZaloConfig               `json:"zalo"`
	ZaloPersonal      ZaloPersonalConfig       `json:"zalo_personal"`
	Feishu            FeishuConfig             `json:"feishu"`
	PendingCompaction *PendingCompactionConfig `json:"pending_compaction,omitempty"` // global pending message compaction settings
}

type TelegramConfig struct {
	Enabled           bool                   `json:"enabled"`
	Token             string                 `json:"token"`
	Proxy             string                 `json:"proxy,omitempty"`
	APIServer         string                 `json:"api_server,omitempty"` // custom Telegram Bot API server URL (e.g. "http://localhost:8081")
	AllowFrom         FlexibleStringSlice    `json:"allow_from"`
	DMPolicy          string                 `json:"dm_policy,omitempty"`          // "pairing" (default), "allowlist", "open", "disabled"
	GroupPolicy       string                 `json:"group_policy,omitempty"`       // "open" (default), "allowlist", "disabled"
	RequireMention    *bool                  `json:"require_mention,omitempty"`    // require @bot mention in groups (default true)
	MentionMode       string                 `json:"mention_mode,omitempty"`       // "strict" (default) = only respond when mentioned; "yield" = respond unless another bot is mentioned
	HistoryLimit      int                    `json:"history_limit,omitempty"`      // max pending group messages for context (default 50, 0=disabled)
	DMStream          *bool                  `json:"dm_stream,omitempty"`          // enable streaming for DMs (default false) — edits placeholder progressively
	GroupStream       *bool                  `json:"group_stream,omitempty"`       // enable streaming for groups (default false) — sends new message, edits progressively
	DraftTransport    *bool                  `json:"draft_transport,omitempty"`    // use sendMessageDraft for DM streaming (default true) — stealth preview, no notifications per edit
	ReasoningDelivery string                 `json:"reasoning_delivery,omitempty"` // off, streaming_only, always_bubbles
	ReasoningStream   *bool                  `json:"reasoning_stream,omitempty"`   // show reasoning as separate message when provider emits thinking events (default true)
	ReactionLevel     string                 `json:"reaction_level,omitempty"`     // "off" (default), "minimal", "full" — status emoji reactions
	MediaMaxBytes     int64                  `json:"media_max_bytes,omitempty"`    // max media download size in bytes (default 20MB)
	LinkPreview       *bool                  `json:"link_preview,omitempty"`       // enable URL previews in messages (default true)
	BlockReply        *bool                  `json:"block_reply,omitempty"`        // override gateway block_reply (nil = inherit)
	ChatBehavior      *ChatBehaviorConfig    `json:"chat_behavior,omitempty"`      // override gateway chat behavior (nil = inherit)
	ForceIPv4         bool                   `json:"force_ipv4,omitempty"`         // force IPv4 for all Telegram API requests (use when IPv6 routing is broken)
	TelegramManager   *TelegramManagerConfig `json:"telegram_manager,omitempty"`   // hidden Telegram-only management tool permissions

	// Optional STT (Speech-to-Text) pipeline for voice/audio inbound messages.
	// When stt_proxy_url is set, audio/voice messages are transcribed before being forwarded to the agent.
	STTProxyURL       string `json:"stt_proxy_url,omitempty"`       // base URL of the STT proxy service (e.g. "https://stt.example.com")
	STTAPIKey         string `json:"stt_api_key,omitempty"`         // Bearer token for the STT proxy
	STTTenantID       string `json:"stt_tenant_id,omitempty"`       // optional tenant/org identifier forwarded to the STT proxy
	STTTimeoutSeconds int    `json:"stt_timeout_seconds,omitempty"` // per-request timeout for STT calls (default 30s)

	// Optional audio-aware routing: when set, voice/audio inbound messages are routed to this
	// agent instead of the default channel agent. Requires the named agent to exist in the config.
	VoiceAgentID string `json:"voice_agent_id,omitempty"` // agent ID to route voice inbound to (e.g. "speaking-agent")

	// Audio guard: intercept technical errors in voice agent replies and replace with friendly fallbacks.
	// Only active when VoiceAgentID is set. Custom error markers replace built-in defaults when provided.
	AudioGuardFallbackTranscript   string   `json:"audio_guard_fallback_transcript,omitempty"`    // fallback with %s for transcript (e.g. "I heard: \"%s\". Try again!")
	AudioGuardFallbackNoTranscript string   `json:"audio_guard_fallback_no_transcript,omitempty"` // fallback when no transcript available
	AudioGuardErrorMarkers         []string `json:"audio_guard_error_markers,omitempty"`          // custom error detection markers (replaces defaults)

	// Per-group (and per-topic) overrides. Key is chat ID string (e.g. "-100123456") or "*" for wildcard.
	// TS ref: channels.telegram.groups in src/config/types.telegram.ts.
	Groups map[string]*TelegramGroupConfig `json:"groups,omitempty"`
}

// TelegramGroupConfig defines per-group overrides for a Telegram channel.
// Matching TS TelegramGroupConfig in src/config/types.telegram.ts.
type TelegramGroupConfig struct {
	GroupPolicy    string                          `json:"group_policy,omitempty"`    // override group policy for this group
	RequireMention *bool                           `json:"require_mention,omitempty"` // override require_mention for this group
	MentionMode    string                          `json:"mention_mode,omitempty"`    // override mention_mode for this group
	AllowFrom      FlexibleStringSlice             `json:"allow_from,omitempty"`      // override allow_from for this group
	Enabled        *bool                           `json:"enabled,omitempty"`         // disable bot for this group (default: true)
	Skills         []string                        `json:"skills,omitempty"`          // skill whitelist (nil = all, [] = none)
	Tools          []string                        `json:"tools,omitempty"`           // tool allow list (nil = all, supports "group:xxx")
	SystemPrompt   string                          `json:"system_prompt,omitempty"`   // extra system prompt for this group
	Topics         map[string]*TelegramTopicConfig `json:"topics,omitempty"`          // per-topic overrides (key: thread ID string)
	Quota          *QuotaWindow                    `json:"quota,omitempty"`           // per-group quota override
}

// TelegramTopicConfig defines per-topic overrides within a Telegram group.
// Matching TS TelegramTopicConfig in src/config/types.telegram.ts.
type TelegramTopicConfig struct {
	RequireMention *bool               `json:"require_mention,omitempty"`
	MentionMode    string              `json:"mention_mode,omitempty"`
	GroupPolicy    string              `json:"group_policy,omitempty"`
	Skills         []string            `json:"skills,omitempty"`
	Tools          []string            `json:"tools,omitempty"` // tool allow list (nil = inherit, supports "group:xxx")
	Enabled        *bool               `json:"enabled,omitempty"`
	AllowFrom      FlexibleStringSlice `json:"allow_from,omitempty"`
	SystemPrompt   string              `json:"system_prompt,omitempty"`
}

type DiscordConfig struct {
	Enabled           bool                `json:"enabled"`
	Token             string              `json:"token"`
	AllowFrom         FlexibleStringSlice `json:"allow_from"`
	DMPolicy          string              `json:"dm_policy,omitempty"`       // "open" (default), "allowlist", "disabled"
	GroupPolicy       string              `json:"group_policy,omitempty"`    // "open" (default), "allowlist", "disabled"
	RequireMention    *bool               `json:"require_mention,omitempty"` // require @bot mention in groups (default true)
	HistoryLimit      int                 `json:"history_limit,omitempty"`   // max pending group messages for context (default 50, 0=disabled)
	BlockReply        *bool               `json:"block_reply,omitempty"`     // override gateway block_reply (nil = inherit)
	ChatBehavior      *ChatBehaviorConfig `json:"chat_behavior,omitempty"`   // override gateway chat behavior (nil = inherit)
	MediaMaxBytes     int64               `json:"media_max_bytes,omitempty"` // max media download size (default 25MB)
	STTProxyURL       string              `json:"stt_proxy_url,omitempty"`
	STTAPIKey         string              `json:"stt_api_key,omitempty"`
	STTTenantID       string              `json:"stt_tenant_id,omitempty"`
	STTTimeoutSeconds int                 `json:"stt_timeout_seconds,omitempty"`
	VoiceAgentID      string              `json:"voice_agent_id,omitempty"`
}

type SlackConfig struct {
	Enabled        bool                `json:"enabled"`
	BotToken       string              `json:"bot_token"`            // xoxb-... (Bot User OAuth Token)
	AppToken       string              `json:"app_token"`            // xapp-... (App-Level Token for Socket Mode)
	UserToken      string              `json:"user_token,omitempty"` // xoxp-... (Optional: custom bot identity)
	AllowFrom      FlexibleStringSlice `json:"allow_from"`
	DMPolicy       string              `json:"dm_policy,omitempty"`       // "pairing" (default), "allowlist", "open", "disabled"
	GroupPolicy    string              `json:"group_policy,omitempty"`    // "open" (default), "pairing", "allowlist", "disabled"
	RequireMention *bool               `json:"require_mention,omitempty"` // require @bot mention in channels (default true)
	HistoryLimit   int                 `json:"history_limit,omitempty"`   // max pending group messages for context (default 50, 0=disabled)
	DMStream       *bool               `json:"dm_stream,omitempty"`       // enable streaming for DMs (default false)
	GroupStream    *bool               `json:"group_stream,omitempty"`    // enable streaming for groups (default false)
	NativeStream   *bool               `json:"native_stream,omitempty"`   // use Slack ChatStreamer API if available (default false)
	ReactionLevel  string              `json:"reaction_level,omitempty"`  // "off" (default), "minimal", "full"
	BlockReply     *bool               `json:"block_reply,omitempty"`     // override gateway block_reply (nil = inherit)
	ChatBehavior   *ChatBehaviorConfig `json:"chat_behavior,omitempty"`   // override gateway chat behavior (nil = inherit)
	DebounceDelay  *int                `json:"debounce_delay,omitempty"`  // ms delay before dispatching rapid messages (default 300, 0=disabled)
	ThreadTTL      *int                `json:"thread_ttl,omitempty"`      // hours before thread participation expires (default 24, 0=disabled — always require @mention)
	MediaMaxBytes  int64               `json:"media_max_bytes,omitempty"` // max file download size in bytes (default 20MB)
}

type WhatsAppConfig struct {
	Enabled        bool                `json:"enabled"`
	AuthDir        string              `json:"auth_dir,omitempty"` // optional: SQLite auth dir override (desktop)
	AllowFrom      FlexibleStringSlice `json:"allow_from"`
	DMPolicy       string              `json:"dm_policy,omitempty"`       // "pairing" (default for DB instances), "open", "allowlist", "disabled"
	GroupPolicy    string              `json:"group_policy,omitempty"`    // "pairing" (default for DB instances), "open" (default for config), "allowlist", "disabled"
	RequireMention *bool               `json:"require_mention,omitempty"` // only respond in groups when bot is @mentioned (default false)
	HistoryLimit   int                 `json:"history_limit,omitempty"`   // max pending group messages for context (default 200, 0=disabled)
	BlockReply     *bool               `json:"block_reply,omitempty"`     // override gateway block_reply (nil = inherit)
	ChatBehavior   *ChatBehaviorConfig `json:"chat_behavior,omitempty"`   // override gateway chat behavior (nil = inherit)
}

type ZaloConfig struct {
	Enabled       bool                `json:"enabled"`
	Token         string              `json:"token"`
	AllowFrom     FlexibleStringSlice `json:"allow_from"`
	DMPolicy      string              `json:"dm_policy,omitempty"` // "pairing" (default), "allowlist", "open", "disabled"
	WebhookURL    string              `json:"webhook_url,omitempty"`
	WebhookSecret string              `json:"webhook_secret,omitempty"`
	MediaMaxMB    int                 `json:"media_max_mb,omitempty"`  // default 5
	BlockReply    *bool               `json:"block_reply,omitempty"`   // override gateway block_reply (nil = inherit)
	ChatBehavior  *ChatBehaviorConfig `json:"chat_behavior,omitempty"` // override gateway chat behavior (nil = inherit)
}

type ZaloPersonalConfig struct {
	Enabled         bool                `json:"enabled"`
	AllowFrom       FlexibleStringSlice `json:"allow_from"`
	DMPolicy        string              `json:"dm_policy,omitempty"`        // "pairing" (default), "allowlist", "open", "disabled"
	GroupPolicy     string              `json:"group_policy,omitempty"`     // "open" (default), "allowlist", "disabled"
	RequireMention  *bool               `json:"require_mention,omitempty"`  // require @bot mention in groups (default true)
	HistoryLimit    int                 `json:"history_limit,omitempty"`    // max pending group messages for context (default 50, 0=disabled)
	CredentialsPath string              `json:"credentials_path,omitempty"` // path to saved cookies JSON
	BlockReply      *bool               `json:"block_reply,omitempty"`      // override gateway block_reply (nil = inherit)
	ChatBehavior    *ChatBehaviorConfig `json:"chat_behavior,omitempty"`    // override gateway chat behavior (nil = inherit)
}

type FeishuConfig struct {
	Enabled           bool                `json:"enabled"`
	AppID             string              `json:"app_id"`
	AppSecret         string              `json:"app_secret"`
	EncryptKey        string              `json:"encrypt_key,omitempty"`
	VerificationToken string              `json:"verification_token,omitempty"`
	Domain            string              `json:"domain,omitempty"`          // "lark" (default/global), "feishu" (China), or custom URL
	ConnectionMode    string              `json:"connection_mode,omitempty"` // "websocket" (default), "webhook"
	WebhookPort       int                 `json:"webhook_port,omitempty"`    // default 3000
	WebhookPath       string              `json:"webhook_path,omitempty"`    // default "/feishu/events"
	AllowFrom         FlexibleStringSlice `json:"allow_from"`
	DMPolicy          string              `json:"dm_policy,omitempty"`    // "pairing" (default)
	GroupPolicy       string              `json:"group_policy,omitempty"` // "open" (default)
	GroupAllowFrom    FlexibleStringSlice `json:"group_allow_from,omitempty"`
	RequireMention    *bool               `json:"require_mention,omitempty"`    // default true (groups)
	TopicSessionMode  string              `json:"topic_session_mode,omitempty"` // "disabled" (default)
	TextChunkLimit    int                 `json:"text_chunk_limit,omitempty"`   // default 4000
	MediaMaxMB        int                 `json:"media_max_mb,omitempty"`       // default 30
	RenderMode        string              `json:"render_mode,omitempty"`        // "auto", "raw", "card"
	Streaming         *bool               `json:"streaming,omitempty"`          // default true
	ReactionLevel     string              `json:"reaction_level,omitempty"`     // "off" (default), "minimal", "full" — typing emoji reactions
	HistoryLimit      int                 `json:"history_limit,omitempty"`
	BlockReply        *bool               `json:"block_reply,omitempty"`   // override gateway block_reply (nil = inherit)
	ChatBehavior      *ChatBehaviorConfig `json:"chat_behavior,omitempty"` // override gateway chat behavior (nil = inherit)
	STTProxyURL       string              `json:"stt_proxy_url,omitempty"`
	STTAPIKey         string              `json:"stt_api_key,omitempty"`
	STTTenantID       string              `json:"stt_tenant_id,omitempty"`
	STTTimeoutSeconds int                 `json:"stt_timeout_seconds,omitempty"`
	VoiceAgentID      string              `json:"voice_agent_id,omitempty"`
}

// ProvidersConfig maps provider name to its config.
type ProvidersConfig struct {
	Anthropic      ProviderConfig  `json:"anthropic"`
	OpenAI         ProviderConfig  `json:"openai"`
	OpenRouter     ProviderConfig  `json:"openrouter"`
	Groq           ProviderConfig  `json:"groq"`
	Gemini         ProviderConfig  `json:"gemini"`
	DeepSeek       ProviderConfig  `json:"deepseek"`
	Mistral        ProviderConfig  `json:"mistral"`
	XAI            ProviderConfig  `json:"xai"`
	MiniMax        ProviderConfig  `json:"minimax"`
	Cohere         ProviderConfig  `json:"cohere"`
	Perplexity     ProviderConfig  `json:"perplexity"`
	DashScope      ProviderConfig  `json:"dashscope"`
	Bailian        ProviderConfig  `json:"bailian"`
	Zai            ProviderConfig  `json:"zai"`
	ZaiCoding      ProviderConfig  `json:"zai_coding"`
	Ollama         OllamaConfig    `json:"ollama"`       // local Ollama instance (no API key needed)
	OllamaCloud    ProviderConfig  `json:"ollama_cloud"` // Ollama Cloud (API key required)
	ClaudeCLI      ClaudeCLIConfig `json:"claude_cli"`
	ACP            ACPConfig       `json:"acp"`
	Novita         ProviderConfig  `json:"novita"`          // Novita AI (OpenAI-compatible endpoint)
	BytePlus       ProviderConfig  `json:"byteplus"`        // BytePlus ModelArk (Seed 2.0)
	BytePlusCoding ProviderConfig  `json:"byteplus_coding"` // BytePlus ModelArk Coding Plan
	Vertex         VertexConfig    `json:"vertex"`          // Google Cloud Vertex AI (OAuth2 service account + ADC)

	// RequestTimeoutSec bounds provider verify and models-list HTTP calls.
	// Tenant-scoped, overridable via the "providers.request_timeout_sec" system config.
	RequestTimeoutSec int `json:"request_timeout_sec,omitempty"`
}

// VertexConfig configures Google Cloud Vertex AI.
// Credentials precedence: APIKey (inline JSON) > CredentialsFile (path) > ADC (both empty).
// ProjectID and Region are required; Model optional (defaults to google/gemini-2.0-flash-001).
type VertexConfig struct {
	APIKey          string `json:"api_key,omitempty"`          // service account JSON inline (secret — never persist in config.json)
	CredentialsFile string `json:"credentials_file,omitempty"` // path to service account JSON file
	ProjectID       string `json:"project_id,omitempty"`
	Region          string `json:"region,omitempty"`
	Model           string `json:"model,omitempty"`
}

// OllamaConfig configures a local (or self-hosted) Ollama instance.
// No API key is required — Ollama accepts any Bearer token value.
type OllamaConfig struct {
	Host string `json:"host"` // Ollama server base URL, e.g. http://localhost:11434
}

// ClaudeCLIConfig configures the Claude CLI provider (uses subscription, not API key).
type ClaudeCLIConfig struct {
	CLIPath     string `json:"cli_path" yaml:"cli_path"`           // path to claude binary (default: "claude")
	Model       string `json:"model" yaml:"model"`                 // default model alias (default: "sonnet")
	BaseWorkDir string `json:"base_work_dir" yaml:"base_work_dir"` // base dir for agent workspaces
	PermMode    string `json:"perm_mode" yaml:"perm_mode"`         // permission mode (default: "bypassPermissions")
}

// ACPConfig configures the ACP (Agent Client Protocol) provider.
// Orchestrates any ACP-compatible coding agent (Claude Code, Codex CLI, Gemini CLI) as a subprocess.
type ACPConfig struct {
	Binary   string   `json:"binary"`    // agent binary name or path (e.g. "claude", "codex")
	Args     []string `json:"args"`      // extra spawn args
	Model    string   `json:"model"`     // default model/agent name
	WorkDir  string   `json:"work_dir"`  // base workspace dir
	IdleTTL  string   `json:"idle_ttl"`  // process idle TTL (e.g. "5m")
	PermMode string   `json:"perm_mode"` // "approve-all" (default), "approve-reads", "deny-all"
}

type ProviderConfig struct {
	APIKey  string `json:"api_key"`
	APIBase string `json:"api_base,omitempty"`
}

// APIBaseForType returns the config-level api_base for a given provider type.
// Used as a fallback when DB providers have no api_base set.
func (p *ProvidersConfig) APIBaseForType(providerType string) string {
	switch providerType {
	case "anthropic_native":
		return p.Anthropic.APIBase
	case "openai", "openai_compat":
		return p.OpenAI.APIBase
	case "openrouter":
		return p.OpenRouter.APIBase
	case "groq":
		return p.Groq.APIBase
	case "deepseek":
		return p.DeepSeek.APIBase
	case "gemini_native":
		return p.Gemini.APIBase
	case "mistral":
		return p.Mistral.APIBase
	case "xai":
		return p.XAI.APIBase
	case "minimax_native":
		return p.MiniMax.APIBase
	case "cohere":
		return p.Cohere.APIBase
	case "perplexity":
		return p.Perplexity.APIBase
	case "dashscope":
		return p.DashScope.APIBase
	case "bailian":
		return p.Bailian.APIBase
	case "zai":
		return p.Zai.APIBase
	case "zai_coding":
		return p.ZaiCoding.APIBase
	case "ollama_cloud":
		return p.OllamaCloud.APIBase
	case "novita":
		return p.Novita.APIBase
	case "byteplus":
		return p.BytePlus.APIBase
	case "byteplus_coding":
		return p.BytePlusCoding.APIBase
	case "vertex":
		// Computed from project+region at registration time; no config-level static base.
		return ""
	default:
		return ""
	}
}

// HasAnyProvider returns true if at least one provider has an API key or CLI configured.
func (c *Config) HasAnyProvider() bool {
	p := c.Providers
	return p.Anthropic.APIKey != "" ||
		p.OpenAI.APIKey != "" ||
		p.OpenRouter.APIKey != "" ||
		p.Groq.APIKey != "" ||
		p.Gemini.APIKey != "" ||
		p.DeepSeek.APIKey != "" ||
		p.Mistral.APIKey != "" ||
		p.XAI.APIKey != "" ||
		p.MiniMax.APIKey != "" ||
		p.Cohere.APIKey != "" ||
		p.Perplexity.APIKey != "" ||
		p.DashScope.APIKey != "" ||
		p.Bailian.APIKey != "" ||
		p.Zai.APIKey != "" ||
		p.ZaiCoding.APIKey != "" ||
		p.Ollama.Host != "" ||
		p.OllamaCloud.APIKey != "" ||
		p.ClaudeCLI.CLIPath != "" ||
		p.ACP.Binary != "" ||
		p.Novita.APIKey != "" ||
		p.BytePlus.APIKey != "" ||
		p.BytePlusCoding.APIKey != "" ||
		(p.Vertex.ProjectID != "" && p.Vertex.Region != "")
}

// QuotaWindow defines request limits per time window. Zero means unlimited.
type QuotaWindow struct {
	Hour int `json:"hour,omitempty"` // max requests per hour (0 = unlimited)
	Day  int `json:"day,omitempty"`  // max requests per day (0 = unlimited)
	Week int `json:"week,omitempty"` // max requests per week (0 = unlimited)
}

// IsZero returns true if no limits are set.
func (w QuotaWindow) IsZero() bool { return w.Hour == 0 && w.Day == 0 && w.Week == 0 }

// QuotaConfig configures per-user/group request quotas.
// Config merge priority: Groups > Channels > Providers > Default.
type QuotaConfig struct {
	Enabled   bool                   `json:"enabled"`
	Default   QuotaWindow            `json:"default"`
	Providers map[string]QuotaWindow `json:"providers,omitempty"` // key = provider name (e.g. "anthropic")
	Channels  map[string]QuotaWindow `json:"channels,omitempty"`  // key = channel name (e.g. "telegram")
	Groups    map[string]QuotaWindow `json:"groups,omitempty"`    // key = userID (e.g. "group:telegram:-100123")
}

// GatewayConfig controls the gateway server.
type GatewayConfig struct {
	Host                    string              `json:"host"`
	Port                    int                 `json:"port"`
	Token                   string              `json:"token,omitempty"`                      // bearer token for WS/HTTP auth
	OwnerIDs                []string            `json:"owner_ids,omitempty"`                  // sender IDs considered "owner"
	AllowedOrigins          []string            `json:"allowed_origins,omitempty"`            // WebSocket CORS whitelist (empty = allow all)
	MCPAllowedHosts         []string            `json:"mcp_allowed_hosts,omitempty"`          // trusted MCP server hostnames exempt from the private-IP SSRF block during config validation (empty = none)
	MaxMessageChars         int                 `json:"max_message_chars,omitempty"`          // max user message characters (default 32000)
	RateLimitRPM            int                 `json:"rate_limit_rpm,omitempty"`             // rate limit: requests per minute per user (default 20, 0 = disabled)
	InjectionAction         string              `json:"injection_action,omitempty"`           // prompt injection action: "log", "warn" (default), "block", "off"
	InboundDebounceMs       int                 `json:"inbound_debounce_ms,omitempty"`        // silence-window in ms that merges rapid channel/Web Chat messages from the same sender/session; 0 disables for text but media-bearing messages still honor a built-in media floor so multi-attachment bursts (#63) coalesce into a single agent run. Agents may override via per-agent agent_config.inbound_debounce_ms.
	Quota                   *QuotaConfig        `json:"quota,omitempty"`                      // per-user/group request quotas
	BlockReply              *bool               `json:"block_reply,omitempty"`                // deliver intermediate text during tool iterations (default false)
	ChatBehavior            *ChatBehaviorConfig `json:"chat_behavior,omitempty"`              // human-like channel delivery behavior (default disabled)
	ToolStatus              *bool               `json:"tool_status,omitempty"`                // show tool name in streaming preview during tool execution (default true)
	TeamWorkClassify        *bool               `json:"team_work_classify,omitempty"`         // classify new requests as direct handling or team workflow (default false)
	TaskRecoveryIntervalSec int                 `json:"task_recovery_interval_sec,omitempty"` // team task recovery ticker interval in seconds (default 300 = 5min)
	WebhookAsyncTimeoutSec  int                 `json:"webhook_async_timeout_sec,omitempty"`  // async webhook worker agent-run deadline in seconds (default 600, cap 3600)
	WebhookSyncTimeoutSec   int                 `json:"webhook_sync_timeout_sec,omitempty"`   // sync + test webhook handler agent-run deadline in seconds (default 600, cap 3600). NOTE: sync holds the HTTP connection open for this duration — a value above an upstream proxy/LB read timeout may be cut.
	WebhookStream           *bool               `json:"webhook_stream,omitempty"`             // stream provider responses for server-side webhook agent runs (sync/async/test) so the upstream can populate/serve its prompt cache (default true). Response to the caller is unchanged (still assembled JSON).
	BackgroundProvider      string              `json:"background_provider,omitempty"`        // LLM provider for background workers (vault enrichment, consolidation)
	BackgroundModel         string              `json:"background_model,omitempty"`           // LLM model for background workers
	PublicURL               string              `json:"public_url,omitempty"`                 // public base URL for OAuth callbacks (e.g. "https://goclaw.example.com")
}

// ToolsConfig controls tool availability, policy, and web search.
type ToolsConfig struct {
	Profile                 string                        `json:"profile,omitempty"`                 // global profile: "minimal", "coding", "messaging", "full"
	Allow                   []string                      `json:"allow,omitempty"`                   // global allow list (tool names or "group:xxx")
	Deny                    []string                      `json:"deny,omitempty"`                    // global deny list
	AlsoAllow               []string                      `json:"alsoAllow,omitempty"`               // additive: adds without removing existing
	ByProvider              map[string]*ToolPolicySpec    `json:"byProvider,omitempty"`              // per-provider overrides
	ShellDenyGroups         map[string]bool               `json:"shellDenyGroups,omitempty"`         // global shell deny-group toggles (group name -> denied); per-agent overrides win per-key
	CommandKeywordAllowlist []CommandKeywordAllowlistRule `json:"commandKeywordAllowlist,omitempty"` // scoped bypass for credentialed CLI content keywords
	ExecApproval            ExecApprovalCfg               `json:"execApproval"`                      // exec command approval settings
	WebFetch                WebFetchPolicyConfig          `json:"web_fetch"`                         // domain policy for URL fetching
	Browser                 BrowserToolConfig             `json:"browser"`
	RateLimitPerHour        int                           `json:"rate_limit_per_hour,omitempty"` // max tool executions per hour per session (0 = disabled)
	ScrubCredentials        *bool                         `json:"scrub_credentials,omitempty"`   // auto-redact API keys/tokens in tool output (default true)
	McpServers              map[string]*MCPServerConfig   `json:"mcp_servers,omitempty"`         // external MCP server connections
	DocumentParser          DocumentParserConfig          `json:"document_parser"`               // local-first document text extraction
}

// DocumentParserConfig controls local-first document text extraction in the
// read_document tool. Local-first is OPT-IN (disabled by default): plain
// pdftotext/pandoc output mis-orders tables/multi-column layouts and ignores
// the analysis prompt, so the cloud vision path remains the default. When
// enabled, PDF/DOCX are extracted via local binaries before any cloud LLM
// call; missing binaries or empty output fall back to the provider chain.
//
// Changing these values requires a gateway restart — they are captured at tool
// construction and not picked up by config hot-reload. (Binary availability,
// however, is re-checked per call, so runtime installs are seen without a
// restart.)
type DocumentParserConfig struct {
	LocalFirst *bool `json:"local_first,omitempty"`  // nil => false (default OFF)
	MaxPages   int   `json:"max_pages,omitempty"`    // 0 => 200
	TimeoutSec int   `json:"timeout_sec,omitempty"`  // 0 => 30s
	MinTextLen int   `json:"min_text_len,omitempty"` // 0 => 16
}

// LocalFirstEnabled reports whether local-first extraction is turned on.
func (c DocumentParserConfig) LocalFirstEnabled() bool {
	return c.LocalFirst != nil && *c.LocalFirst
}

// CommandKeywordAllowlistRule scopes product/security vocabulary that may appear
// in credentialed CLI content args without disabling command-path deny patterns.
type CommandKeywordAllowlistRule struct {
	ID           string   `json:"id,omitempty"`
	Command      string   `json:"command"`
	Subcommands  []string `json:"subcommands,omitempty"`
	Args         []string `json:"args,omitempty"`
	ArgPositions []int    `json:"argPositions,omitempty"` // 0-based after matched subcommand
	Keywords     []string `json:"keywords,omitempty"`
	Reason       string   `json:"reason,omitempty"`
	Enabled      *bool    `json:"enabled,omitempty"` // nil means enabled
}

// MCPServerConfig configures a single external MCP server connection.
type MCPServerConfig struct {
	Transport  string            `json:"transport"`             // "stdio", "sse", "streamable-http"
	Command    string            `json:"command,omitempty"`     // stdio: command to spawn
	Args       []string          `json:"args,omitempty"`        // stdio: command arguments
	Env        map[string]string `json:"env,omitempty"`         // stdio: extra environment variables
	URL        string            `json:"url,omitempty"`         // sse/http: server URL
	Headers    map[string]string `json:"headers,omitempty"`     // sse/http: extra HTTP headers
	Enabled    *bool             `json:"enabled,omitempty"`     // default true
	ToolPrefix string            `json:"tool_prefix,omitempty"` // prefix for tool names (avoids collisions)
	TimeoutSec int               `json:"timeout_sec,omitempty"` // per-tool-call timeout in seconds (default 60)
}

// IsEnabled returns whether this MCP server is enabled (default true).
func (c *MCPServerConfig) IsEnabled() bool {
	return c.Enabled == nil || *c.Enabled
}

// ExecApprovalCfg configures command execution approval (matching TS exec-approval.ts).
type ExecApprovalCfg struct {
	Security  string   `json:"security,omitempty"`  // "deny", "allowlist", "full" (default "full")
	Ask       string   `json:"ask,omitempty"`       // "off", "on-miss", "always" (default "off")
	Allowlist []string `json:"allowlist,omitempty"` // glob patterns for allowed commands
}

// WebFetchPolicyConfig controls domain filtering for the web_fetch tool.
type WebFetchPolicyConfig struct {
	Policy         string   `json:"policy,omitempty"`          // "allow_all" (default), "allowlist"
	AllowedDomains []string `json:"allowed_domains,omitempty"` // e.g. ["github.com", "*.example.com"]
	BlockedDomains []string `json:"blocked_domains,omitempty"` // always checked regardless of policy
}

// BrowserToolConfig controls the browser automation tool.
type BrowserToolConfig struct {
	Enabled           bool   `json:"enabled"`                     // enable the browser tool (default false)
	Headless          bool   `json:"headless,omitempty"`          // run Chrome in headless mode (ignored when RemoteURL is set)
	RemoteURL         string `json:"remote_url,omitempty"`        // CDP endpoint for remote Chrome sidecar, e.g. "ws://chrome:9222"
	ActionTimeoutMs   int    `json:"action_timeout_ms,omitempty"` // per-action timeout in ms (default 30000)
	IdleTimeoutMs     int    `json:"idle_timeout_ms,omitempty"`   // idle page auto-close in ms (default 600000, 0=disabled)
	MaxPages          int    `json:"max_pages,omitempty"`         // max open pages per tenant (default 5)
	CookieSyncEnabled bool   `json:"cookie_sync_enabled"`         // apply selected synced cookies to scoped browser sessions
}

// ToolPolicySpec defines a tool policy at any level (global, per-agent, per-provider).
type ToolPolicySpec struct {
	Profile        string                     `json:"profile,omitempty"`
	Allow          []string                   `json:"allow,omitempty"`
	Deny           []string                   `json:"deny,omitempty"`
	AlsoAllow      []string                   `json:"alsoAllow,omitempty"`
	ByProvider     map[string]*ToolPolicySpec `json:"byProvider,omitempty"`
	Wait           *WaitToolPolicy            `json:"wait,omitempty"`
	ToolCallPrefix string                     `json:"toolCallPrefix,omitempty"` // prefix to strip from model's tool call names before registry lookup
	// RateLimitPerHour overrides the global tools.rate_limit_per_hour for this
	// agent (applied per session key). 0 = inherit the global limit.
	RateLimitPerHour int `json:"rate_limit_per_hour,omitempty"`
}

// WaitToolPolicy configures per-agent safety bounds for the wait tool.
type WaitToolPolicy struct {
	MinMs int `json:"min_ms,omitempty"`
	MaxMs int `json:"max_ms,omitempty"`
}

// SessionsConfig controls session behavior.
// Matching TS src/config/sessions/types.ts + src/config/types.base.ts.
type SessionsConfig struct {
	Scope   string `json:"scope,omitempty"`    // "per-sender" (default), "global"
	DmScope string `json:"dm_scope,omitempty"` // "main", "per-peer", "per-channel-peer" (default), "per-account-channel-peer"
	MainKey string `json:"main_key,omitempty"` // main session key suffix (default "main", used when dm_scope="main")
}

// TtsConfig configures text-to-speech.
// Matching TS src/config/types.tts.ts.
type TtsConfig struct {
	Provider   string              `json:"provider,omitempty"`   // "openai", "elevenlabs", "edge", "minimax", "gemini"
	Auto       string              `json:"auto,omitempty"`       // "off" (default), "always", "inbound", "tagged"
	Mode       string              `json:"mode,omitempty"`       // "final" (default), "all"
	MaxLength  int                 `json:"max_length,omitempty"` // max text length before truncation (default 1500)
	TimeoutMs  int                 `json:"timeout_ms,omitempty"` // API timeout in ms (default 30000)
	OpenAI     TtsOpenAIConfig     `json:"openai"`
	ElevenLabs TtsElevenLabsConfig `json:"elevenlabs"`
	Edge       TtsEdgeConfig       `json:"edge"`
	MiniMax    TtsMiniMaxConfig    `json:"minimax"`
	Gemini     TtsGeminiConfig     `json:"gemini"`
}

// TtsGeminiConfig configures the Google Gemini TTS provider.
type TtsGeminiConfig struct {
	APIKey   string `json:"api_key,omitempty"`  // required; encrypted at rest
	APIBase  string `json:"api_base,omitempty"` // custom endpoint (optional; SSRF-gated)
	Voice    string `json:"voice,omitempty"`    // default "Kore"
	Model    string `json:"model,omitempty"`    // default "gemini-2.5-flash-preview-tts"
	Speakers string `json:"speakers,omitempty"` // JSON-encoded []SpeakerVoice for multi-speaker mode
}

// TtsOpenAIConfig configures the OpenAI TTS provider.
type TtsOpenAIConfig struct {
	APIKey  string `json:"api_key,omitempty"`
	APIBase string `json:"api_base,omitempty"` // custom endpoint URL
	Model   string `json:"model,omitempty"`    // default "gpt-4o-mini-tts"
	Voice   string `json:"voice,omitempty"`    // default "alloy"
}

// TtsElevenLabsConfig configures the ElevenLabs TTS provider.
type TtsElevenLabsConfig struct {
	APIKey  string `json:"api_key,omitempty"`
	BaseURL string `json:"base_url,omitempty"`
	VoiceID string `json:"voice_id,omitempty"` // default "pMsXgVXv3BLzUgSXRplE"
	ModelID string `json:"model_id,omitempty"` // default "eleven_multilingual_v2"
}

// TtsEdgeConfig configures the Microsoft Edge TTS provider (free, no API key).
type TtsEdgeConfig struct {
	Enabled bool   `json:"enabled,omitempty"`
	Voice   string `json:"voice,omitempty"` // default "en-US-MichelleNeural"
	Rate    string `json:"rate,omitempty"`  // speech rate, e.g. "+0%"
}

// TtsMiniMaxConfig configures the MiniMax TTS provider.
type TtsMiniMaxConfig struct {
	APIKey  string `json:"api_key,omitempty"`
	GroupID string `json:"group_id,omitempty"` // MiniMax GroupId (required)
	APIBase string `json:"api_base,omitempty"` // default "https://api.minimax.io/v1"
	Model   string `json:"model,omitempty"`    // default "speech-02-hd"
	VoiceID string `json:"voice_id,omitempty"` // default "Wise_Woman"
}

// MergeChannelGroupQuotas merges per-group quota overrides from channel configs
// (e.g., channels.telegram.groups[chatID].quota) into gateway.quota.groups.
// This allows per-group quotas to be set at the channel level and picked up
// by the QuotaChecker at runtime.
func MergeChannelGroupQuotas(cfg *Config) {
	if cfg.Gateway.Quota == nil {
		return
	}
	if cfg.Gateway.Quota.Groups == nil {
		cfg.Gateway.Quota.Groups = make(map[string]QuotaWindow)
	}

	// Telegram per-group quotas
	for chatID, groupCfg := range cfg.Channels.Telegram.Groups {
		if groupCfg != nil && groupCfg.Quota != nil && !groupCfg.Quota.IsZero() {
			key := "group:telegram:" + chatID
			cfg.Gateway.Quota.Groups[key] = *groupCfg.Quota
		}
	}
}
