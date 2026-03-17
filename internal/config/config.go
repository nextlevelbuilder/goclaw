package config

import (
	"encoding/json"
	"fmt"
	"sync"
	"time"

	"github.com/nextlevelbuilder/goclaw/internal/cron"
	"github.com/nextlevelbuilder/goclaw/internal/sandbox"
)

// FlexibleStringSlice accepts both ["str"] and [123] in JSON.
type FlexibleStringSlice []string

func (f *FlexibleStringSlice) UnmarshalJSON(data []byte) error {
	var ss []string
	if err := json.Unmarshal(data, &ss); err == nil {
		*f = ss
		return nil
	}
	var raw []any
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}
	result := make([]string, 0, len(raw))
	for _, v := range raw {
		switch val := v.(type) {
		case string:
			result = append(result, val)
		case float64:
			result = append(result, fmt.Sprintf("%.0f", val))
		default:
			result = append(result, fmt.Sprintf("%v", val))
		}
	}
	*f = result
	return nil
}

// Config is the root configuration for the GoClaw Gateway.
type Config struct {
	DataDir   string          `json:"data_dir,omitempty"` // persistent data directory (default: ~/.goclaw/data)
	Agents    AgentsConfig    `json:"agents"`
	Channels  ChannelsConfig  `json:"channels"`
	Providers ProvidersConfig `json:"providers"`
	Gateway   GatewayConfig   `json:"gateway"`
	Tools     ToolsConfig     `json:"tools"`
	Sessions  SessionsConfig  `json:"sessions"`
	Database  DatabaseConfig  `json:"database"`
	Tts       TtsConfig       `json:"tts"`
	Cron      CronConfig      `json:"cron"`
	Telemetry TelemetryConfig `json:"telemetry"`
	Tailscale TailscaleConfig `json:"tailscale"`
	Bindings  []AgentBinding  `json:"bindings,omitempty"`
	mu        sync.RWMutex
}

// TailscaleConfig configures the optional Tailscale tsnet listener.
// Requires building with -tags tsnet. Auth key from env only (never persisted).
type TailscaleConfig struct {
	Hostname  string `json:"hostname"`             // Tailscale machine name (e.g. "goclaw-gateway")
	StateDir  string `json:"state_dir,omitempty"`  // persistent state directory (default: os.UserConfigDir/tsnet-goclaw)
	AuthKey   string `json:"-"`                    // from env GOCLAW_TSNET_AUTH_KEY only
	Ephemeral bool   `json:"ephemeral,omitempty"`  // remove node on exit (default false)
	EnableTLS bool   `json:"enable_tls,omitempty"` // use ListenTLS for auto HTTPS certs
}

// DatabaseConfig configures the PostgreSQL connection and optional Redis cache.
// DSN fields are NEVER read from config.json (secrets) — only from env vars.
type DatabaseConfig struct {
	PostgresDSN string `json:"-"` // from env GOCLAW_POSTGRES_DSN only
	RedisDSN    string `json:"-"` // from env GOCLAW_REDIS_DSN only (optional, requires -tags redis)
}

// SkillsConfig configures the skills storage system.
type SkillsConfig struct {
	StorageDir string `json:"storage_dir,omitempty"` // directory for skill content (default: dataDir/skills-store/)
}

// AgentBinding maps a channel/peer pattern to a specific agent.
// Matching TS AgentBinding from config/types.agents.ts.
type AgentBinding struct {
	AgentID string       `json:"agentId"`
	Match   BindingMatch `json:"match"`
}

// BindingMatch specifies what messages this binding applies to.
type BindingMatch struct {
	Channel   string       `json:"channel"`             // "telegram", "discord", "slack", etc.
	AccountID string       `json:"accountId,omitempty"` // bot account ID
	Peer      *BindingPeer `json:"peer,omitempty"`      // specific DM/group
	GuildID   string       `json:"guildId,omitempty"`   // Discord guild
}

// BindingPeer specifies a specific chat target.
type BindingPeer struct {
	Kind string `json:"kind"` // "direct" or "group"
	ID   string `json:"id"`
}

// AgentsConfig contains agent defaults and per-agent overrides.
type AgentsConfig struct {
	Defaults AgentDefaults        `json:"defaults"`
	List     map[string]AgentSpec `json:"list,omitempty"`
}

// AgentDefaults are default settings for all agents.
type AgentDefaults struct {
	Workspace           string                `json:"workspace"`
	RestrictToWorkspace bool                  `json:"restrict_to_workspace"`
	Provider            string                `json:"provider"`
	Model               string                `json:"model"`
	MaxTokens           int                   `json:"max_tokens"`
	Temperature         float64               `json:"temperature"`
	MaxToolIterations   int                   `json:"max_tool_iterations"`
	ContextWindow       int                   `json:"context_window"`
	MaxToolCalls        int                   `json:"max_tool_calls,omitempty"` // max total tool calls per run (0 = unlimited, default 25)
	AgentType           string                `json:"agent_type,omitempty"`     // "open" (default) or "predefined"
	Subagents           *SubagentsConfig      `json:"subagents,omitempty"`
	Sandbox             *SandboxConfig        `json:"sandbox,omitempty"`
	Memory              *MemoryConfig         `json:"memory,omitempty"`
	Compaction          *CompactionConfig     `json:"compaction,omitempty"`
	ContextPruning      *ContextPruningConfig `json:"contextPruning,omitempty"`
	// Bootstrap context truncation limits (matching TS bootstrapMaxChars / bootstrapTotalMaxChars)
	BootstrapMaxChars      int `json:"bootstrapMaxChars,omitempty"`      // per-file max before truncation (default 20000)
	BootstrapTotalMaxChars int `json:"bootstrapTotalMaxChars,omitempty"` // total budget across all files (default 24000)
}

// CompactionConfig configures session compaction behaviour.
// Matching TS agents.defaults.compaction.
type CompactionConfig struct {
	ReserveTokensFloor int                `json:"reserveTokensFloor,omitempty"` // min reserve tokens (default 20000)
	MaxHistoryShare    float64            `json:"maxHistoryShare,omitempty"`    // max share of context for history (default 0.75)
	MinMessages        int                `json:"minMessages,omitempty"`        // min messages before compaction triggers (default 50)
	KeepLastMessages   int                `json:"keepLastMessages,omitempty"`   // messages to keep after compaction (default 4)
	MemoryFlush        *MemoryFlushConfig `json:"memoryFlush,omitempty"`        // pre-compaction flush
}

// MemoryFlushConfig configures the pre-compaction memory flush.
// Matching TS AgentCompactionMemoryFlushConfig.
type MemoryFlushConfig struct {
	Enabled             *bool  `json:"enabled,omitempty"`             // default true (nil = enabled)
	SoftThresholdTokens int    `json:"softThresholdTokens,omitempty"` // flush when within N tokens of compaction (default 4000)
	Prompt              string `json:"prompt,omitempty"`              // user prompt for flush turn
	SystemPrompt        string `json:"systemPrompt,omitempty"`        // system prompt for flush turn
}

// ContextPruningConfig configures in-memory context pruning of old tool results.
// Matching TS src/agents/pi-extensions/context-pruning/settings.ts.
// Mode "cache-ttl": prune when context exceeds softTrimRatio of context window.
type ContextPruningConfig struct {
	Mode                 string                   `json:"mode,omitempty"`                 // "off" (default), "cache-ttl"
	KeepLastAssistants   int                      `json:"keepLastAssistants,omitempty"`   // protect last N assistant msgs (default 3)
	SoftTrimRatio        float64                  `json:"softTrimRatio,omitempty"`        // start soft trim at this % of window (default 0.3)
	HardClearRatio       float64                  `json:"hardClearRatio,omitempty"`       // start hard clear at this % (default 0.5)
	MinPrunableToolChars int                      `json:"minPrunableToolChars,omitempty"` // min chars in prunable tools before acting (default 50000)
	SoftTrim             *ContextPruningSoftTrim  `json:"softTrim,omitempty"`
	HardClear            *ContextPruningHardClear `json:"hardClear,omitempty"`
}

// ContextPruningSoftTrim configures how long tool results are trimmed.
type ContextPruningSoftTrim struct {
	MaxChars  int `json:"maxChars,omitempty"`  // tool results longer than this get trimmed (default 4000)
	HeadChars int `json:"headChars,omitempty"` // keep first N chars (default 1500)
	TailChars int `json:"tailChars,omitempty"` // keep last N chars (default 1500)
}

// ContextPruningHardClear configures replacement of old tool results.
type ContextPruningHardClear struct {
	Enabled     *bool  `json:"enabled,omitempty"`     // default true
	Placeholder string `json:"placeholder,omitempty"` // replacement text (default "[Old tool result content cleared]")
}

// MemoryConfig configures the agent memory system (SQLite + FTS5 + optional embeddings).
// Matching TS agents.defaults.memory.
type MemoryConfig struct {
	Enabled           *bool   `json:"enabled,omitempty"`            // default true (nil = enabled)
	EmbeddingProvider string  `json:"embedding_provider,omitempty"` // "openai", "gemini", "openrouter", "" (auto-select)
	EmbeddingModel    string  `json:"embedding_model,omitempty"`    // default "text-embedding-3-small"
	EmbeddingAPIBase  string  `json:"embedding_api_base,omitempty"` // custom endpoint URL
	MaxResults        int     `json:"max_results,omitempty"`        // default 6
	MaxChunkLen       int     `json:"max_chunk_len,omitempty"`      // default 1000
	VectorWeight      float64 `json:"vector_weight,omitempty"`      // hybrid search vector weight (default 0.7)
	TextWeight        float64 `json:"text_weight,omitempty"`        // hybrid search FTS weight (default 0.3)
	MinScore          float64 `json:"min_score,omitempty"`          // minimum relevance score (default 0.35)
}

// SandboxConfig configures Docker-based sandbox execution.
// Matching TS agents.defaults.sandbox.
type SandboxConfig struct {
	Mode            string            `json:"mode,omitempty"`             // "off" (default), "non-main", "all"
	Image           string            `json:"image,omitempty"`            // Docker image (default: "goclaw-sandbox:bookworm-slim")
	WorkspaceAccess string            `json:"workspace_access,omitempty"` // "none", "ro", "rw" (default)
	Scope           string            `json:"scope,omitempty"`            // "session" (default), "agent", "shared"
	MemoryMB        int               `json:"memory_mb,omitempty"`        // memory limit in MB (default 512)
	CPUs            float64           `json:"cpus,omitempty"`             // CPU limit (default 1.0)
	TimeoutSec      int               `json:"timeout_sec,omitempty"`      // exec timeout in seconds (default 300)
	NetworkEnabled  bool              `json:"network_enabled,omitempty"`  // enable network (default false)
	ReadOnlyRoot    *bool             `json:"read_only_root,omitempty"`   // read-only root fs (default true)
	SetupCommand    string            `json:"setup_command,omitempty"`    // run once after container creation
	Env             map[string]string `json:"env,omitempty"`              // extra environment variables

	// Enhanced security
	User           string `json:"user,omitempty"`             // container user (e.g. "1000:1000", "nobody")
	TmpfsSizeMB    int    `json:"tmpfs_size_mb,omitempty"`    // default tmpfs size in MB (0 = Docker default)
	MaxOutputBytes int    `json:"max_output_bytes,omitempty"` // limit exec output capture (default 1MB)

	// Pruning (matching TS SandboxPruneSettings)
	IdleHours        int `json:"idle_hours,omitempty"`         // prune containers idle > N hours (default 24)
	MaxAgeDays       int `json:"max_age_days,omitempty"`       // prune containers older than N days (default 7)
	PruneIntervalMin int `json:"prune_interval_min,omitempty"` // check interval in minutes (default 5)
}

// ToSandboxConfig converts config.SandboxConfig → sandbox.Config with defaults applied.
func (sc *SandboxConfig) ToSandboxConfig() sandbox.Config {
	cfg := sandbox.DefaultConfig()

	if sc == nil {
		return cfg
	}

	switch sc.Mode {
	case "all":
		cfg.Mode = sandbox.ModeAll
	case "non-main":
		cfg.Mode = sandbox.ModeNonMain
	default:
		cfg.Mode = sandbox.ModeOff
	}

	if sc.Image != "" {
		cfg.Image = sc.Image
	}
	switch sc.WorkspaceAccess {
	case "none":
		cfg.WorkspaceAccess = sandbox.AccessNone
	case "ro":
		cfg.WorkspaceAccess = sandbox.AccessRO
	case "rw":
		cfg.WorkspaceAccess = sandbox.AccessRW
	}
	switch sc.Scope {
	case "agent":
		cfg.Scope = sandbox.ScopeAgent
	case "shared":
		cfg.Scope = sandbox.ScopeShared
	case "session":
		cfg.Scope = sandbox.ScopeSession
	}
	if sc.MemoryMB > 0 {
		cfg.MemoryMB = sc.MemoryMB
	}
	if sc.CPUs > 0 {
		cfg.CPUs = sc.CPUs
	}
	if sc.TimeoutSec > 0 {
		cfg.TimeoutSec = sc.TimeoutSec
	}
	cfg.NetworkEnabled = sc.NetworkEnabled
	if sc.ReadOnlyRoot != nil {
		cfg.ReadOnlyRoot = *sc.ReadOnlyRoot
	}
	if sc.SetupCommand != "" {
		cfg.SetupCommand = sc.SetupCommand
	}
	if len(sc.Env) > 0 {
		cfg.Env = sc.Env
	}

	// Enhanced security
	if sc.User != "" {
		cfg.User = sc.User
	}
	if sc.TmpfsSizeMB > 0 {
		cfg.TmpfsSizeMB = sc.TmpfsSizeMB
	}
	if sc.MaxOutputBytes > 0 {
		cfg.MaxOutputBytes = sc.MaxOutputBytes
	}

	// Pruning
	if sc.IdleHours > 0 {
		cfg.IdleHours = sc.IdleHours
	}
	if sc.MaxAgeDays > 0 {
		cfg.MaxAgeDays = sc.MaxAgeDays
	}
	if sc.PruneIntervalMin > 0 {
		cfg.PruneIntervalMin = sc.PruneIntervalMin
	}

	return cfg
}

// ModelPricing defines per-million-token pricing for a model.
type ModelPricing struct {
	InputPerMillion       float64 `json:"input_per_million"`
	OutputPerMillion      float64 `json:"output_per_million"`
	CacheReadPerMillion   float64 `json:"cache_read_per_million,omitempty"`
	CacheCreatePerMillion float64 `json:"cache_create_per_million,omitempty"`
}

// TelemetryConfig configures OpenTelemetry export for traces and spans.
// When enabled, spans are exported to an OTLP-compatible backend (Jaeger, Tempo, Datadog, etc.)
// in addition to PostgreSQL storage.
type TelemetryConfig struct {
	Enabled      bool                       `json:"enabled,omitempty"`       // enable OTLP export (default false)
	Endpoint     string                     `json:"endpoint,omitempty"`      // OTLP endpoint (e.g. "localhost:4317", "https://otel.example.com:4318")
	Protocol     string                     `json:"protocol,omitempty"`      // "grpc" (default) or "http"
	Insecure     bool                       `json:"insecure,omitempty"`      // skip TLS verification (default false, set true for local dev)
	ServiceName  string                     `json:"service_name,omitempty"`  // OTEL service name (default "goclaw-gateway")
	Headers      map[string]string          `json:"headers,omitempty"`       // extra headers (e.g. auth tokens for cloud backends)
	ModelPricing map[string]*ModelPricing    `json:"model_pricing,omitempty"` // cost per model, key = "provider/model" or just "model"
}

// CronConfig configures the cron job system.
type CronConfig struct {
	MaxRetries      int    `json:"max_retries,omitempty"`      // max retry attempts on failure (default 3, 0 = no retry)
	RetryBaseDelay  string `json:"retry_base_delay,omitempty"` // initial backoff delay (default "2s", Go duration)
	RetryMaxDelay   string `json:"retry_max_delay,omitempty"`  // maximum backoff delay (default "30s", Go duration)
	DefaultTimezone string `json:"default_timezone,omitempty"` // IANA timezone for cron expressions when not set per-job (e.g. "Asia/Ho_Chi_Minh")
}

// ToRetryConfig converts CronConfig to cron.RetryConfig with defaults applied.
func (cc CronConfig) ToRetryConfig() cron.RetryConfig {
	cfg := cron.DefaultRetryConfig()
	if cc.MaxRetries > 0 {
		cfg.MaxRetries = cc.MaxRetries
	}
	if cc.RetryBaseDelay != "" {
		if d, err := time.ParseDuration(cc.RetryBaseDelay); err == nil && d > 0 {
			cfg.BaseDelay = d
		}
	}
	if cc.RetryMaxDelay != "" {
		if d, err := time.ParseDuration(cc.RetryMaxDelay); err == nil && d > 0 {
			cfg.MaxDelay = d
		}
	}
	return cfg
}

// SubagentsConfig configures the subagent system (matching TS agents.defaults.subagents).
// All fields optional — zero values mean "use default".
type SubagentsConfig struct {
	MaxConcurrent       int    `json:"maxConcurrent,omitempty"`       // default 8 (TS: DEFAULT_SUBAGENT_MAX_CONCURRENT)
	MaxSpawnDepth       int    `json:"maxSpawnDepth,omitempty"`       // default 1, range 1-5
	MaxChildrenPerAgent int    `json:"maxChildrenPerAgent,omitempty"` // default 5, range 1-20
	ArchiveAfterMinutes int    `json:"archiveAfterMinutes,omitempty"` // default 60
	Model               string `json:"model,omitempty"`               // model override for subagents
}

// AgentSpec is the per-agent configuration override.
// All fields optional — zero values mean "inherit from defaults".
type AgentSpec struct {
	DisplayName       string          `json:"displayName,omitempty"`
	Provider          string          `json:"provider,omitempty"`
	Model             string          `json:"model,omitempty"`
	MaxTokens         int             `json:"max_tokens,omitempty"`
	Temperature       float64         `json:"temperature,omitempty"`
	MaxToolIterations int             `json:"max_tool_iterations,omitempty"`
	ContextWindow     int             `json:"context_window,omitempty"`
	MaxToolCalls      int             `json:"max_tool_calls,omitempty"` // per-agent override
	AgentType         string          `json:"agent_type,omitempty"`     // "open" or "predefined"
	Skills            []string        `json:"skills,omitempty"`         // nil = all skills allowed
	Tools             *ToolPolicySpec `json:"tools,omitempty"`          // per-agent tool policy
	Workspace         string          `json:"workspace,omitempty"`
	Default           bool            `json:"default,omitempty"`
	Sandbox           *SandboxConfig  `json:"sandbox,omitempty"`
	Identity          *IdentityConfig `json:"identity,omitempty"`
}

// ReplaceFrom copies all data fields from src into c, preserving c's mutex.
func (c *Config) ReplaceFrom(src *Config) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.DataDir = src.DataDir
	c.Agents = src.Agents
	c.Channels = src.Channels
	c.Providers = src.Providers
	c.Gateway = src.Gateway
	c.Tools = src.Tools
	c.Sessions = src.Sessions
	c.Database = src.Database
	c.Tts = src.Tts
	c.Cron = src.Cron
	c.Telemetry = src.Telemetry
	c.Tailscale = src.Tailscale
	c.Bindings = src.Bindings
}

// IdentityConfig defines agent persona / display identity.
type IdentityConfig struct {
	Name  string `json:"name,omitempty"`
	Emoji string `json:"emoji,omitempty"`
}
