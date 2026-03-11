package config

// PendingCompactionConfig configures LLM-based compaction of pending group messages.
// When a group accumulates more than Threshold pending messages, older messages are
// summarized by an LLM and replaced with a compact summary, keeping KeepRecent raw messages.
type PendingCompactionConfig struct {
	Threshold  int    `json:"threshold,omitempty"`   // trigger compaction when entries exceed this (default 50)
	KeepRecent int    `json:"keep_recent,omitempty"` // keep this many recent raw messages after compaction (default 15)
	MaxTokens  int    `json:"max_tokens,omitempty"`  // max output tokens for LLM summarization (default 4096)
	Provider   string `json:"provider,omitempty"`    // LLM provider name (e.g. "openai"); empty = use agent's provider
	Model      string `json:"model,omitempty"`       // model for summarization; empty = use agent's model
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
	Enabled        bool                `json:"enabled"`
	Token          string              `json:"token"`
	Proxy          string              `json:"proxy,omitempty"`
	AllowFrom      FlexibleStringSlice `json:"allow_from"`
	DMPolicy       string              `json:"dm_policy,omitempty"`       // "pairing" (default), "allowlist", "open", "disabled"
	GroupPolicy    string              `json:"group_policy,omitempty"`    // "open" (default), "allowlist", "disabled"
	RequireMention *bool               `json:"require_mention,omitempty"` // require @bot mention in groups (default true)
	HistoryLimit   int                 `json:"history_limit,omitempty"`   // max pending group messages for context (default 50, 0=disabled)
	DMStream       *bool               `json:"dm_stream,omitempty"`       // enable streaming for DMs (default false) — edits placeholder progressively; disabled pending Telegram draft API fixes (tdesktop#10315)
	GroupStream    *bool               `json:"group_stream,omitempty"`    // enable streaming for groups (default false) — sends new message, edits progressively
	ReactionLevel  string              `json:"reaction_level,omitempty"`  // "off" (default), "minimal", "full" — status emoji reactions
	MediaMaxBytes  int64               `json:"media_max_bytes,omitempty"` // max media download size in bytes (default 20MB)
	LinkPreview    *bool               `json:"link_preview,omitempty"`    // enable URL previews in messages (default true)
	BlockReply     *bool               `json:"block_reply,omitempty"`     // override gateway block_reply (nil = inherit)

	// Optional STT (Speech-to-Text) pipeline for voice/audio inbound messages.
	// When stt_proxy_url is set, audio/voice messages are transcribed before being forwarded to the agent.
	STTProxyURL       string `json:"stt_proxy_url,omitempty"`       // base URL of the STT proxy service (e.g. "https://stt.example.com")
	STTAPIKey         string `json:"stt_api_key,omitempty"`         // Bearer token for the STT proxy
	STTTenantID       string `json:"stt_tenant_id,omitempty"`       // optional tenant/org identifier forwarded to the STT proxy
	STTTimeoutSeconds int    `json:"stt_timeout_seconds,omitempty"` // per-request timeout for STT calls (default 30s)

	// Optional audio-aware routing: when set, voice/audio inbound messages are routed to this
	// agent instead of the default channel agent. Requires the named agent to exist in the config.
	VoiceAgentID string `json:"voice_agent_id,omitempty"` // agent ID to route voice inbound to (e.g. "speaking-agent")

	// Per-group (and per-topic) overrides. Key is chat ID string (e.g. "-100123456") or "*" for wildcard.
	// TS ref: channels.telegram.groups in src/config/types.telegram.ts.
	Groups map[string]*TelegramGroupConfig `json:"groups,omitempty"`
}

// TelegramGroupConfig defines per-group overrides for a Telegram channel.
// Matching TS TelegramGroupConfig in src/config/types.telegram.ts.
type TelegramGroupConfig struct {
	GroupPolicy    string                          `json:"group_policy,omitempty"`    // override group policy for this group
	RequireMention *bool                           `json:"require_mention,omitempty"` // override require_mention for this group
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
	DebounceDelay  int                 `json:"debounce_delay,omitempty"`  // ms delay before dispatching rapid messages (default 300, 0=disabled)
	ThreadTTL      *int                `json:"thread_ttl,omitempty"`      // hours before thread participation expires (default 24, 0=disabled — always require @mention)
	MediaMaxBytes  int64               `json:"media_max_bytes,omitempty"` // max file download size in bytes (default 20MB)
}

type WhatsAppConfig struct {
	Enabled     bool                `json:"enabled"`
	BridgeURL   string              `json:"bridge_url"`
	AllowFrom   FlexibleStringSlice `json:"allow_from"`
	DMPolicy    string              `json:"dm_policy,omitempty"`    // "open" (default), "allowlist", "disabled"
	GroupPolicy string              `json:"group_policy,omitempty"` // "open" (default), "allowlist", "disabled"
	BlockReply  *bool               `json:"block_reply,omitempty"`  // override gateway block_reply (nil = inherit)
}

type ZaloConfig struct {
	Enabled       bool                `json:"enabled"`
	Token         string              `json:"token"`
	AllowFrom     FlexibleStringSlice `json:"allow_from"`
	DMPolicy      string              `json:"dm_policy,omitempty"` // "pairing" (default), "allowlist", "open", "disabled"
	WebhookURL    string              `json:"webhook_url,omitempty"`
	WebhookSecret string              `json:"webhook_secret,omitempty"`
	MediaMaxMB    int                 `json:"media_max_mb,omitempty"` // default 5
	BlockReply    *bool               `json:"block_reply,omitempty"`  // override gateway block_reply (nil = inherit)
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
	BlockReply        *bool               `json:"block_reply,omitempty"` // override gateway block_reply (nil = inherit)
	STTProxyURL       string              `json:"stt_proxy_url,omitempty"`
	STTAPIKey         string              `json:"stt_api_key,omitempty"`
	STTTenantID       string              `json:"stt_tenant_id,omitempty"`
	STTTimeoutSeconds int                 `json:"stt_timeout_seconds,omitempty"`
	VoiceAgentID      string              `json:"voice_agent_id,omitempty"`
}

// ProvidersConfig maps provider name to its config.
type ProvidersConfig struct {
	Anthropic  ProviderConfig  `json:"anthropic"`
	OpenAI     ProviderConfig  `json:"openai"`
	OpenRouter ProviderConfig  `json:"openrouter"`
	Groq       ProviderConfig  `json:"groq"`
	Gemini     ProviderConfig  `json:"gemini"`
	DeepSeek   ProviderConfig  `json:"deepseek"`
	Mistral    ProviderConfig  `json:"mistral"`
	XAI        ProviderConfig  `json:"xai"`
	MiniMax    ProviderConfig  `json:"minimax"`
	Cohere     ProviderConfig  `json:"cohere"`
	Perplexity ProviderConfig  `json:"perplexity"`
	DashScope  ProviderConfig  `json:"dashscope"`
	Bailian    ProviderConfig  `json:"bailian"`
	Zai         ProviderConfig  `json:"zai"`
	ZaiCoding   ProviderConfig  `json:"zai_coding"`
	Ollama      OllamaConfig    `json:"ollama"`       // local Ollama instance (no API key needed)
	OllamaCloud ProviderConfig  `json:"ollama_cloud"` // Ollama Cloud (API key required)
	ClaudeCLI   ClaudeCLIConfig `json:"claude_cli"`
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

type ProviderConfig struct {
	APIKey  string `json:"api_key"`
	APIBase string `json:"api_base,omitempty"`
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
		p.ClaudeCLI.CLIPath != ""
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
	Host              string       `json:"host"`
	Port              int          `json:"port"`
	Token             string       `json:"token,omitempty"`               // bearer token for WS/HTTP auth
	OwnerIDs          []string     `json:"owner_ids,omitempty"`           // sender IDs considered "owner"
	AllowedOrigins    []string     `json:"allowed_origins,omitempty"`     // WebSocket CORS whitelist (empty = allow all)
	MaxMessageChars   int          `json:"max_message_chars,omitempty"`   // max user message characters (default 32000)
	RateLimitRPM      int          `json:"rate_limit_rpm,omitempty"`      // rate limit: requests per minute per user (default 20, 0 = disabled)
	InjectionAction   string       `json:"injection_action,omitempty"`    // prompt injection action: "log", "warn" (default), "block", "off"
	InboundDebounceMs int          `json:"inbound_debounce_ms,omitempty"` // merge rapid messages from same sender (default 1000ms, -1 = disabled)
	Quota             *QuotaConfig `json:"quota,omitempty"`               // per-user/group request quotas
	BlockReply        *bool        `json:"block_reply,omitempty"`         // deliver intermediate text during tool iterations (default false)
	ToolStatus        *bool        `json:"tool_status,omitempty"`         // show tool name in streaming preview during tool execution (default true)
}

// ToolsConfig controls tool availability, policy, and web search.
type ToolsConfig struct {
	Profile          string                      `json:"profile,omitempty"`    // global profile: "minimal", "coding", "messaging", "full"
	Allow            []string                    `json:"allow,omitempty"`      // global allow list (tool names or "group:xxx")
	Deny             []string                    `json:"deny,omitempty"`       // global deny list
	AlsoAllow        []string                    `json:"alsoAllow,omitempty"`  // additive: adds without removing existing
	ByProvider       map[string]*ToolPolicySpec  `json:"byProvider,omitempty"` // per-provider overrides
	ExecApproval     ExecApprovalCfg             `json:"execApproval"`         // exec command approval settings
	WebFetch         WebFetchPolicyConfig        `json:"web_fetch"`            // domain policy for URL fetching
	Web              WebToolsConfig              `json:"web"`
	Browser          BrowserToolConfig           `json:"browser"`
	RateLimitPerHour int                         `json:"rate_limit_per_hour,omitempty"` // max tool executions per hour per session (0 = disabled)
	ScrubCredentials *bool                       `json:"scrub_credentials,omitempty"`   // auto-redact API keys/tokens in tool output (default true)
	McpServers       map[string]*MCPServerConfig `json:"mcp_servers,omitempty"`         // external MCP server connections
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
	Enabled   bool   `json:"enabled"`              // enable the browser tool (default false)
	Headless  bool   `json:"headless,omitempty"`   // run Chrome in headless mode (ignored when RemoteURL is set)
	RemoteURL string `json:"remote_url,omitempty"` // CDP endpoint for remote Chrome sidecar, e.g. "ws://chrome:9222"
}

// ToolPolicySpec defines a tool policy at any level (global, per-agent, per-provider).
type ToolPolicySpec struct {
	Profile    string                     `json:"profile,omitempty"`
	Allow      []string                   `json:"allow,omitempty"`
	Deny       []string                   `json:"deny,omitempty"`
	AlsoAllow  []string                   `json:"alsoAllow,omitempty"`
	ByProvider map[string]*ToolPolicySpec `json:"byProvider,omitempty"`
}

type WebToolsConfig struct {
	Brave      BraveConfig      `json:"brave"`
	DuckDuckGo DuckDuckGoConfig `json:"duckduckgo"`
}

type BraveConfig struct {
	Enabled    bool   `json:"enabled"`
	APIKey     string `json:"api_key"`
	MaxResults int    `json:"max_results"`
}

type DuckDuckGoConfig struct {
	Enabled    bool `json:"enabled"`
	MaxResults int  `json:"max_results"`
}

// SessionsConfig controls session behavior.
// Matching TS src/config/sessions/types.ts + src/config/types.base.ts.
type SessionsConfig struct {
	Storage string `json:"storage"`            // directory for session files
	Scope   string `json:"scope,omitempty"`    // "per-sender" (default), "global"
	DmScope string `json:"dm_scope,omitempty"` // "main", "per-peer", "per-channel-peer" (default), "per-account-channel-peer"
	MainKey string `json:"main_key,omitempty"` // main session key suffix (default "main", used when dm_scope="main")
}

// TtsConfig configures text-to-speech.
// Matching TS src/config/types.tts.ts.
type TtsConfig struct {
	Provider   string              `json:"provider,omitempty"`   // "openai", "elevenlabs", "edge", "minimax"
	Auto       string              `json:"auto,omitempty"`       // "off" (default), "always", "inbound", "tagged"
	Mode       string              `json:"mode,omitempty"`       // "final" (default), "all"
	MaxLength  int                 `json:"max_length,omitempty"` // max text length before truncation (default 1500)
	TimeoutMs  int                 `json:"timeout_ms,omitempty"` // API timeout in ms (default 30000)
	OpenAI     TtsOpenAIConfig     `json:"openai"`
	ElevenLabs TtsElevenLabsConfig `json:"elevenlabs"`
	Edge       TtsEdgeConfig       `json:"edge"`
	MiniMax    TtsMiniMaxConfig    `json:"minimax"`
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
