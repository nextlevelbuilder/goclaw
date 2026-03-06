package cmd

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"

	"github.com/spf13/cobra"

	"github.com/nextlevelbuilder/goclaw/internal/config"
)

func onboardCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "onboard",
		Short: "Interactive setup wizard — configure provider, model, gateway, channels",
		Run: func(cmd *cobra.Command, args []string) {
			runOnboard()
		},
	}
}

type providerInfo struct {
	name      string
	envKey    string
	modelHint string
}

var providerMap = map[string]providerInfo{
	"openrouter": {"openrouter", "GOCLAW_OPENROUTER_API_KEY", "anthropic/claude-sonnet-4-5-20250929"},
	"anthropic":  {"anthropic", "GOCLAW_ANTHROPIC_API_KEY", "claude-sonnet-4-5-20250929"},
	"openai":     {"openai", "GOCLAW_OPENAI_API_KEY", "gpt-4o"},
	"groq":       {"groq", "GOCLAW_GROQ_API_KEY", "llama-3.3-70b-versatile"},
	"deepseek":   {"deepseek", "GOCLAW_DEEPSEEK_API_KEY", "deepseek-chat"},
	"gemini":     {"gemini", "GOCLAW_GEMINI_API_KEY", "gemini-2.0-flash"},
	"mistral":    {"mistral", "GOCLAW_MISTRAL_API_KEY", "mistral-large-latest"},
	"xai":        {"xai", "GOCLAW_XAI_API_KEY", "grok-3-mini"},
	"minimax":    {"minimax", "GOCLAW_MINIMAX_API_KEY", "MiniMax-M2.5"},
	"cohere":     {"cohere", "GOCLAW_COHERE_API_KEY", "command-a"},
	"perplexity": {"perplexity", "GOCLAW_PERPLEXITY_API_KEY", "sonar-pro"},
	"custom":     {"custom", "", ""},
}

func runOnboard() {
	// If env vars provide API keys, skip interactive wizard entirely.
	if canAutoOnboard() {
		cfgPath := resolveConfigPath()
		fmt.Println("Environment variables detected. Running non-interactive setup...")
		if runAutoOnboard(cfgPath) {
			return
		}
		fmt.Println("Auto-onboard failed, falling through to interactive wizard...")
		fmt.Println()
	}

	fmt.Println("╔══════════════════════════════════════════════╗")
	fmt.Println("║        GoClaw — Setup Wizard            ║")
	fmt.Println("╚══════════════════════════════════════════════╝")
	fmt.Println()

	// Determine config path
	cfgPath := resolveConfigPath()

	// Check existing config
	var cfg *config.Config
	isNewConfig := true
	if _, err := os.Stat(cfgPath); err == nil {
		fmt.Printf("Found existing config at %s\n", cfgPath)
		useExisting, err := promptConfirm("Use existing config as base?", true)
		if err != nil {
			fmt.Println("Cancelled.")
			return
		}
		if useExisting {
			loaded, err := config.Load(cfgPath)
			if err != nil {
				fmt.Printf("Warning: could not load existing config: %v\n", err)
				cfg = config.Default()
			} else {
				cfg = loaded
				isNewConfig = false
			}
		} else {
			cfg = config.Default()
		}
	} else {
		cfg = config.Default()
	}

	// --- Declare all wizard variables (pre-filled from existing config) ---
	var (
		providerChoice = cfg.Agents.Defaults.Provider
		apiKey         string
		customAPIBase  string
		customModel    string
		modelChoice    = cfg.Agents.Defaults.Model
		orModelChoice  = cfg.Agents.Defaults.Model

		portStr = strconv.Itoa(cfg.Gateway.Port)

		selectedChannels []string
		telegramToken    string
		zaloToken        string
		zaloDMPolicy     = "pairing"
		feishuAppID      string
		feishuSecret     string
		feishuDomain     = "lark"
		feishuConnMode   = "websocket"

		selectedFeatures []string
		embProvider      string

		ttsProvider = "none"
		ttsAPIKey   string
		ttsGroupID  string
		ttsAutoMode = "off"

		postgresDSN  string
		traceVerbose bool
	)

	if isNewConfig {
		providerChoice = "openrouter"
		selectedFeatures = []string{"memory", "browser"}
	}
	if portStr == "0" {
		portStr = "18790"
	}

	// Pre-fill from existing config
	if cfg.Channels.Telegram.Enabled {
		selectedChannels = append(selectedChannels, "telegram")
		telegramToken = cfg.Channels.Telegram.Token
	}
	if cfg.Channels.Zalo.Enabled {
		selectedChannels = append(selectedChannels, "zalo")
		zaloToken = cfg.Channels.Zalo.Token
		zaloDMPolicy = cfg.Channels.Zalo.DMPolicy
	}
	if cfg.Channels.Feishu.Enabled {
		selectedChannels = append(selectedChannels, "feishu")
		feishuAppID = cfg.Channels.Feishu.AppID
		feishuSecret = cfg.Channels.Feishu.AppSecret
		feishuDomain = cfg.Channels.Feishu.Domain
		feishuConnMode = cfg.Channels.Feishu.ConnectionMode
	}
	if cfg.Agents.Defaults.Memory != nil && (cfg.Agents.Defaults.Memory.Enabled == nil || *cfg.Agents.Defaults.Memory.Enabled) {
		selectedFeatures = append(selectedFeatures, "memory")
		embProvider = cfg.Agents.Defaults.Memory.EmbeddingProvider
	}
	if cfg.Tools.Browser.Enabled {
		selectedFeatures = append(selectedFeatures, "browser")
	}
	if cfg.Tts.Provider != "" {
		ttsProvider = cfg.Tts.Provider
		ttsAutoMode = cfg.Tts.Auto
		ttsAPIKey = cfg.Tts.OpenAI.APIKey
		if ttsAPIKey == "" {
			ttsAPIKey = cfg.Tts.ElevenLabs.APIKey
		}
		if ttsAPIKey == "" {
			ttsAPIKey = cfg.Tts.MiniMax.APIKey
		}
		ttsGroupID = cfg.Tts.MiniMax.GroupID
	}
	postgresDSN = cfg.Database.PostgresDSN

	// Pre-fill API key from env or config
	apiKey = resolveExistingAPIKey(cfg, providerChoice)
	if cfg.Providers.OpenAI.APIBase != "" {
		customAPIBase = cfg.Providers.OpenAI.APIBase
	}

	// ── Step 1: Provider ──
	providerOptions := []SelectOption[string]{
		{"OpenRouter  (recommended — access to many models)", "openrouter"},
		{"Anthropic   (Claude models directly)", "anthropic"},
		{"OpenAI      (GPT models)", "openai"},
		{"Groq        (fast inference)", "groq"},
		{"DeepSeek    (DeepSeek models)", "deepseek"},
		{"Gemini      (Google Gemini)", "gemini"},
		{"Mistral     (Mistral AI models)", "mistral"},
		{"xAI         (Grok models)", "xai"},
		{"MiniMax     (MiniMax models)", "minimax"},
		{"Cohere      (Command models)", "cohere"},
		{"Perplexity  (Sonar search models)", "perplexity"},
		{"Custom      (any OpenAI-compatible endpoint)", "custom"},
	}

	var err error
	providerChoice, err = promptSelect("Step 1 · AI Provider — Choose your LLM provider", providerOptions, 0)
	if err != nil {
		fmt.Println("Cancelled.")
		return
	}

	// Provider-specific prompts
	switch providerChoice {
	case "custom":
		customAPIBase, err = promptString("API Base URL", "OpenAI-compatible endpoint (e.g. Ollama, vLLM, LiteLLM)", customAPIBase)
		if err != nil {
			fmt.Println("Cancelled.")
			return
		}
		apiKey, err = promptPassword("API Key", "Leave empty if not required")
		if err != nil {
			fmt.Println("Cancelled.")
			return
		}
		customModel, err = promptString("Model ID", "The model to use on this endpoint", customModel)
		if err != nil {
			fmt.Println("Cancelled.")
			return
		}

	case "openrouter":
		apiKey, err = promptPassword("OpenRouter API Key", "Get yours at https://openrouter.ai/keys")
		if err != nil {
			fmt.Println("Cancelled.")
			return
		}

		// Fetch and select model
		fmt.Println("  Fetching OpenRouter models...")
		orModelOptions := buildOpenRouterModelOptions()
		orModelChoice, err = promptSelect("OpenRouter Model", orModelOptions, 0)
		if err != nil {
			fmt.Println("Cancelled.")
			return
		}

	default:
		// Standard provider
		apiKey, err = promptPassword("API Key", "Your provider API key (check env vars or dashboard)")
		if err != nil {
			fmt.Println("Cancelled.")
			return
		}
		modelChoice, err = promptString("Default Model", "Model ID to use (leave empty for provider default)", modelChoice)
		if err != nil {
			fmt.Println("Cancelled.")
			return
		}
	}

	// ── Gateway Port ──
	portStr, err = promptString("Gateway Port", "WebSocket server port", portStr)
	if err != nil {
		fmt.Println("Cancelled.")
		return
	}

	// ── Step 2: Channels ──
	selectedChannels, err = promptMultiSelect("Step 2 · Channels (select at least 1)", "Enter numbers to toggle channels", []SelectOption[string]{
		{"Telegram", "telegram"},
		{"Zalo OA", "zalo"},
		{"Feishu / Lark", "feishu"},
	}, selectedChannels)
	if err != nil {
		fmt.Println("Cancelled.")
		return
	}

	// Helper closures
	hasChannel := func(ch string) bool {
		for _, c := range selectedChannels {
			if c == ch {
				return true
			}
		}
		return false
	}
	hasFeature := func(f string) bool {
		for _, feat := range selectedFeatures {
			if feat == f {
				return true
			}
		}
		return false
	}

	// Channel-specific configs
	if hasChannel("telegram") {
		telegramToken, err = promptPassword("Telegram Bot Token", "Get from @BotFather on Telegram")
		if err != nil {
			fmt.Println("Cancelled.")
			return
		}
		if telegramToken == "" {
			telegramToken = cfg.Channels.Telegram.Token // keep existing
		}
	}

	if hasChannel("zalo") {
		if err := promptZaloConfig(&zaloToken, &zaloDMPolicy); err != nil {
			fmt.Println("Cancelled.")
			return
		}
	}

	if hasChannel("feishu") {
		if err := promptFeishuConfig(&feishuAppID, &feishuSecret, &feishuDomain, &feishuConnMode); err != nil {
			fmt.Println("Cancelled.")
			return
		}
	}

	// ── Features ──
	selectedFeatures, err = promptMultiSelect("Features (recommended: keep both)", "Enter numbers to toggle features", []SelectOption[string]{
		{"Memory (vector search over agent notes)", "memory"},
		{"Browser automation (agent can browse the web)", "browser"},
	}, selectedFeatures)
	if err != nil {
		fmt.Println("Cancelled.")
		return
	}

	// Memory embedding provider
	if hasFeature("memory") {
		embProvider, err = promptSelect("Memory Embedding Provider", []SelectOption[string]{
			{"Auto-detect (use chat provider's API key)", ""},
			{"OpenAI (text-embedding-3-small)", "openai"},
			{"OpenRouter (openai/text-embedding-3-small)", "openrouter"},
			{"Gemini (text-embedding-004)", "gemini"},
		}, 0)
		if err != nil {
			fmt.Println("Cancelled.")
			return
		}
	}

	// ── TTS ──
	if err := promptTTSConfig(&ttsProvider, &ttsAPIKey, &ttsGroupID, &ttsAutoMode); err != nil {
		fmt.Println("Cancelled.")
		return
	}

	// ── Verbose Tracing ──
	traceVerbose, err = promptConfirm("Enable verbose tracing? (Logs full LLM input in trace spans)", false)
	if err != nil {
		fmt.Println("Cancelled.")
		return
	}

	// ── Step 3: Database ──
	postgresDSN, err = promptString("Step 3 · Postgres DSN", "Connection string (e.g. postgres://user:pass@host:5432/dbname)", postgresDSN)
	if err != nil {
		fmt.Println("Cancelled.")
		return
	}

	// --- Post-form validation ---
	var errors []string

	if providerChoice == "custom" {
		if customAPIBase == "" {
			errors = append(errors, "API base URL is required for custom provider")
		}
		if customModel == "" {
			errors = append(errors, "Model ID is required for custom provider")
		}
	} else if apiKey == "" {
		errors = append(errors, fmt.Sprintf("API key is required for %s", providerChoice))
	}

	if _, err := strconv.Atoi(portStr); err != nil {
		errors = append(errors, fmt.Sprintf("Invalid gateway port: %s", portStr))
	}

	if len(selectedChannels) == 0 {
		errors = append(errors, "At least one channel must be selected (Telegram, Zalo, or Feishu)")
	}

	if hasChannel("telegram") && telegramToken == "" {
		errors = append(errors, "Telegram bot token is required")
	}
	if hasChannel("zalo") && zaloToken == "" {
		errors = append(errors, "Zalo bot token is required")
	}
	if hasChannel("feishu") && (feishuAppID == "" || feishuSecret == "") {
		errors = append(errors, "Feishu App ID and App Secret are required")
	}

	if postgresDSN == "" {
		errors = append(errors, "Postgres DSN is required (set GOCLAW_POSTGRES_DSN or enter above)")
	}

	if len(errors) > 0 {
		fmt.Println()
		fmt.Println("  Validation errors:")
		for _, e := range errors {
			fmt.Printf("    • %s\n", e)
		}
		fmt.Println()
		fmt.Println("  Please re-run: ./goclaw onboard")
		return
	}

	// --- Apply collected values to config ---

	// Provider & model
	if providerChoice == "custom" {
		cfg.Agents.Defaults.Provider = "openai"
		cfg.Providers.OpenAI.APIBase = customAPIBase
		cfg.Providers.OpenAI.APIKey = apiKey
		cfg.Agents.Defaults.Model = customModel
	} else {
		pi := providerMap[providerChoice]
		cfg.Agents.Defaults.Provider = pi.name
		applyProviderAPIKey(cfg, pi.name, apiKey)

		if providerChoice == "openrouter" {
			if orModelChoice == "__custom__" {
				cfg.Agents.Defaults.Model = pi.modelHint
			} else {
				cfg.Agents.Defaults.Model = orModelChoice
			}
		} else {
			if modelChoice == "" {
				modelChoice = pi.modelHint
			}
			cfg.Agents.Defaults.Model = modelChoice
		}
	}

	// Gateway
	cfg.Gateway.Port, _ = strconv.Atoi(portStr)
	if cfg.Gateway.Host == "" {
		cfg.Gateway.Host = "0.0.0.0"
	}
	if cfg.Gateway.Token == "" {
		cfg.Gateway.Token = onboardGenerateToken(16)
		fmt.Printf("  Generated gateway token: %s\n", cfg.Gateway.Token)
	}

	// Workspace (use default, no prompt)
	cfg.Agents.Defaults.Workspace = "~/.goclaw/workspace"
	expandedWS := config.ExpandHome(cfg.Agents.Defaults.Workspace)
	if err := os.MkdirAll(expandedWS, 0755); err != nil {
		fmt.Printf("Warning: could not create workspace: %v\n", err)
	}

	// Channels
	cfg.Channels.Telegram.Enabled = hasChannel("telegram")
	if cfg.Channels.Telegram.Enabled {
		cfg.Channels.Telegram.Token = telegramToken
		cfg.Channels.Telegram.DMPolicy = "pairing"
	}

	cfg.Channels.Zalo.Enabled = hasChannel("zalo")
	if cfg.Channels.Zalo.Enabled {
		cfg.Channels.Zalo.Token = zaloToken
		cfg.Channels.Zalo.DMPolicy = zaloDMPolicy
	}

	cfg.Channels.Feishu.Enabled = hasChannel("feishu")
	if cfg.Channels.Feishu.Enabled {
		cfg.Channels.Feishu.AppID = feishuAppID
		cfg.Channels.Feishu.AppSecret = feishuSecret
		cfg.Channels.Feishu.Domain = feishuDomain
		cfg.Channels.Feishu.ConnectionMode = feishuConnMode
	}

	// Features
	if hasFeature("memory") {
		enabled := true
		if cfg.Agents.Defaults.Memory == nil {
			cfg.Agents.Defaults.Memory = &config.MemoryConfig{}
		}
		cfg.Agents.Defaults.Memory.Enabled = &enabled
		cfg.Agents.Defaults.Memory.EmbeddingProvider = embProvider
	} else {
		disabled := false
		cfg.Agents.Defaults.Memory = &config.MemoryConfig{Enabled: &disabled}
	}
	cfg.Tools.Browser.Enabled = hasFeature("browser")
	if cfg.Tools.Browser.Enabled {
		cfg.Tools.Browser.Headless = true
	}

	// TTS
	if ttsProvider != "none" {
		cfg.Tts.Provider = ttsProvider
		cfg.Tts.Auto = ttsAutoMode
		switch ttsProvider {
		case "openai":
			cfg.Tts.OpenAI.APIKey = ttsAPIKey
		case "elevenlabs":
			cfg.Tts.ElevenLabs.APIKey = ttsAPIKey
		case "minimax":
			cfg.Tts.MiniMax.APIKey = ttsAPIKey
			cfg.Tts.MiniMax.GroupID = ttsGroupID
		case "edge":
			cfg.Tts.Edge.Enabled = true
		}
	}

	// Database
	cfg.Database.PostgresDSN = postgresDSN

	// Auto-generate encryption key for API keys in DB (if not already set).
	if os.Getenv("GOCLAW_ENCRYPTION_KEY") == "" {
		encKey := onboardGenerateToken(32)
		os.Setenv("GOCLAW_ENCRYPTION_KEY", encKey)
		fmt.Printf("  Generated encryption key for API keys (AES-256-GCM)\n")
	} else {
		fmt.Println("  Using existing GOCLAW_ENCRYPTION_KEY from environment")
	}

	fmt.Print("  Testing Postgres connection... ")
	if err := testPostgresConnection(postgresDSN); err != nil {
		fmt.Println("FAILED")
		fmt.Printf("  Error: %v\n", err)
		fmt.Println("  Please check your DSN and try again: ./goclaw onboard")
		return
	}
	fmt.Println("OK")

	runMigrate, err := promptConfirm("Run database migration now?", true)
	if err != nil {
		fmt.Println("Cancelled.")
		return
	}
	if runMigrate {
		fmt.Println("  Running migration...")
		m, err := newMigrator(postgresDSN)
		if err != nil {
			fmt.Printf("  Migration error: %v\n", err)
			fmt.Println("  You can run it manually later: ./goclaw migrate up")
		} else {
			if err := m.Up(); err != nil && err.Error() != "no change" {
				fmt.Printf("  Migration error: %v\n", err)
				fmt.Println("  You can run it manually later: ./goclaw migrate up")
			} else {
				v, _, _ := m.Version()
				fmt.Printf("  Migration complete (version: %d)\n", v)
			}
			m.Close()
		}

		fmt.Println("  Seeding default agent and provider...")
		if err := seedManagedData(postgresDSN, cfg); err != nil {
			fmt.Printf("  Seed warning: %v\n", err)
			fmt.Println("  You can seed manually via the API after starting the gateway.")
		} else {
			fmt.Println("  Default agent and provider seeded.")
		}
	}

	// --- Save config ---
	fmt.Println()
	fmt.Println("── Saving Config ──")
	fmt.Println()

	savedProviders := cfg.Providers
	savedGwToken := cfg.Gateway.Token
	savedTgToken := cfg.Channels.Telegram.Token
	savedZaloToken := cfg.Channels.Zalo.Token
	savedFeishuAppSecret := cfg.Channels.Feishu.AppSecret
	savedTtsOpenAIKey := cfg.Tts.OpenAI.APIKey
	savedTtsElevenLabsKey := cfg.Tts.ElevenLabs.APIKey
	savedTtsMiniMaxKey := cfg.Tts.MiniMax.APIKey

	cfg.Providers = config.ProvidersConfig{}
	cfg.Gateway.Token = ""
	cfg.Channels.Telegram.Token = ""
	cfg.Channels.Zalo.Token = ""
	cfg.Channels.Feishu.AppSecret = ""
	cfg.Tts.OpenAI.APIKey = ""
	cfg.Tts.ElevenLabs.APIKey = ""
	cfg.Tts.MiniMax.APIKey = ""

	saveErr := config.Save(cfgPath, cfg)

	cfg.Providers = savedProviders
	cfg.Gateway.Token = savedGwToken
	cfg.Channels.Telegram.Token = savedTgToken
	cfg.Channels.Zalo.Token = savedZaloToken
	cfg.Channels.Feishu.AppSecret = savedFeishuAppSecret
	cfg.Tts.OpenAI.APIKey = savedTtsOpenAIKey
	cfg.Tts.ElevenLabs.APIKey = savedTtsElevenLabsKey
	cfg.Tts.MiniMax.APIKey = savedTtsMiniMaxKey

	if err := saveErr; err != nil {
		fmt.Printf("Error saving config: %v\n", err)
		os.Exit(1)
	}
	fmt.Printf("Config saved to %s (no secrets)\n", cfgPath)

	envPath := filepath.Join(filepath.Dir(cfgPath), ".env.local")
	pi := providerMap[providerChoice]
	onboardWriteEnvFile(envPath, cfg, apiKey, pi.envKey, traceVerbose)
	fmt.Printf("Secrets saved to %s\n", envPath)

	// Summary
	fmt.Println()
	fmt.Println("╔══════════════════════════════════════════════╗")
	fmt.Println("║           Setup Complete!                     ║")
	fmt.Println("╚══════════════════════════════════════════════╝")
	fmt.Println()
	fmt.Printf("  Provider:  %s\n", cfg.Agents.Defaults.Provider)
	fmt.Printf("  Model:     %s\n", cfg.Agents.Defaults.Model)
	fmt.Printf("  Gateway:   ws://%s:%d\n", cfg.Gateway.Host, cfg.Gateway.Port)
	fmt.Printf("  Token:     %s\n", cfg.Gateway.Token)
	fmt.Printf("  Workspace: %s\n", cfg.Agents.Defaults.Workspace)
	fmt.Println("  Database:  PostgreSQL")
	if cfg.Channels.Telegram.Enabled {
		fmt.Println("  Telegram:  enabled")
	}
	if cfg.Channels.Zalo.Enabled {
		fmt.Println("  Zalo:      enabled")
	}
	if cfg.Channels.Feishu.Enabled {
		fmt.Printf("  Feishu:    enabled (%s, %s)\n", cfg.Channels.Feishu.Domain, cfg.Channels.Feishu.ConnectionMode)
	}
	if cfg.Agents.Defaults.Memory != nil && (cfg.Agents.Defaults.Memory.Enabled == nil || *cfg.Agents.Defaults.Memory.Enabled) {
		embProv := cfg.Agents.Defaults.Memory.EmbeddingProvider
		if embProv == "" {
			embProv = "auto-detect"
		}
		fmt.Printf("  Memory:    enabled (embedding: %s)\n", embProv)
	} else {
		fmt.Println("  Memory:    disabled")
	}
	if cfg.Tools.Browser.RemoteURL != "" {
		fmt.Printf("  Browser:   enabled (remote: %s)\n", cfg.Tools.Browser.RemoteURL)
	} else if cfg.Tools.Browser.Enabled {
		fmt.Println("  Browser:   enabled (headless)")
	} else {
		fmt.Println("  Browser:   disabled")
	}
	if cfg.Tts.Provider != "" {
		autoMode := cfg.Tts.Auto
		if autoMode == "" {
			autoMode = "off"
		}
		fmt.Printf("  TTS:       %s (auto: %s)\n", cfg.Tts.Provider, autoMode)
	} else {
		fmt.Println("  TTS:       disabled")
	}
	fmt.Println()
	fmt.Println("To start the gateway:")
	fmt.Println()
	fmt.Printf("  source %s && ./goclaw\n", envPath)
	fmt.Println()
}

// resolveExistingAPIKey tries to find an existing API key from config or env vars.
func resolveExistingAPIKey(cfg *config.Config, provider string) string {
	key := resolveProviderAPIKey(cfg, provider)
	if key != "" {
		return key
	}
	if pi, ok := providerMap[provider]; ok && pi.envKey != "" {
		if envKey := os.Getenv(pi.envKey); envKey != "" {
			return envKey
		}
	}
	return ""
}

// applyProviderAPIKey stores the API key in the correct config field.
func applyProviderAPIKey(cfg *config.Config, provider, key string) {
	switch provider {
	case "openrouter":
		cfg.Providers.OpenRouter.APIKey = key
	case "anthropic":
		cfg.Providers.Anthropic.APIKey = key
	case "openai":
		cfg.Providers.OpenAI.APIKey = key
	case "groq":
		cfg.Providers.Groq.APIKey = key
	case "deepseek":
		cfg.Providers.DeepSeek.APIKey = key
	case "gemini":
		cfg.Providers.Gemini.APIKey = key
	case "mistral":
		cfg.Providers.Mistral.APIKey = key
	case "xai":
		cfg.Providers.XAI.APIKey = key
	case "minimax":
		cfg.Providers.MiniMax.APIKey = key
	case "cohere":
		cfg.Providers.Cohere.APIKey = key
	case "perplexity":
		cfg.Providers.Perplexity.APIKey = key
	}
}
