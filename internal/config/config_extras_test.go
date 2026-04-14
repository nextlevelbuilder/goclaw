package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// --- NormalizeAgentID ---

func TestNormalizeAgentID(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"default", "default"},
		{"my-agent", "my-agent"},
		{"MY_AGENT", "my_agent"},
		{"Hello World", "hello-world"},
		{"agent@123", "agent-123"},
		{"  trimmed  ", "trimmed"},
		{"", "default"},
		{"   ", "default"},
		// leading dashes stripped (falls through best-effort path)
		{"---leading", "leading"},
		// trailing dashes stripped via best-effort path (already valid regex allows trailing dashes
		// only when input passes validIDRe; raw "trailing---" DOES match so it's returned as-is)
		{"trailing---", "trailing---"},
		// special chars become dashes, then trailing dash stripped
		{"agent!@#$%", "agent"},
	}
	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got := NormalizeAgentID(tt.input)
			if got != tt.want {
				t.Errorf("NormalizeAgentID(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

func TestNormalizeAgentID_Long(t *testing.T) {
	long := strings.Repeat("a", 100)
	got := NormalizeAgentID(long)
	if len(got) > 64 {
		t.Errorf("NormalizeAgentID truncation failed: len=%d", len(got))
	}
}

// --- ExpandHome / ContractHome ---

func TestExpandHome(t *testing.T) {
	home, _ := os.UserHomeDir()

	got := ExpandHome("~/foo/bar")
	if !strings.HasPrefix(got, home) {
		t.Errorf("ExpandHome(~/foo/bar) = %q, expected to start with %q", got, home)
	}

	// Non-tilde path is unchanged
	abs := "/absolute/path"
	if ExpandHome(abs) != abs {
		t.Errorf("ExpandHome(%q) should be unchanged", abs)
	}

	// Empty string
	if ExpandHome("") != "" {
		t.Error("ExpandHome('') should return ''")
	}
}

func TestContractHome(t *testing.T) {
	home, _ := os.UserHomeDir()

	contracted := ContractHome(filepath.Join(home, "projects", "goclaw"))
	if !strings.HasPrefix(contracted, "~") {
		t.Errorf("ContractHome should start with ~, got %q", contracted)
	}

	// Non-home path is unchanged
	abs := "/some/other/path"
	if ContractHome(abs) != abs {
		t.Errorf("ContractHome(%q) should be unchanged", abs)
	}

	// Empty
	if ContractHome("") != "" {
		t.Error("ContractHome('') should return ''")
	}
}

// --- ResolveAgent ---

func TestResolveAgent_Default(t *testing.T) {
	cfg := Default()
	d := cfg.ResolveAgent("nonexistent")

	if d.Provider != "anthropic" {
		t.Errorf("expected default provider, got %q", d.Provider)
	}
	if d.MaxTokens != DefaultMaxTokens {
		t.Errorf("expected default max tokens, got %d", d.MaxTokens)
	}
}

func TestResolveAgent_Override(t *testing.T) {
	cfg := Default()
	cfg.Agents.List = map[string]AgentSpec{
		"myagent": {
			Provider:  "openai",
			Model:     "gpt-4o",
			MaxTokens: 4096,
			AgentType: "predefined",
		},
	}

	d := cfg.ResolveAgent("myagent")
	if d.Provider != "openai" {
		t.Errorf("expected openai, got %q", d.Provider)
	}
	if d.Model != "gpt-4o" {
		t.Errorf("expected gpt-4o, got %q", d.Model)
	}
	if d.MaxTokens != 4096 {
		t.Errorf("expected 4096, got %d", d.MaxTokens)
	}
	if d.AgentType != "predefined" {
		t.Errorf("expected predefined, got %q", d.AgentType)
	}
	// Temperature should still be from defaults (not overridden)
	if d.Temperature != DefaultTemperature {
		t.Errorf("unset Temperature should inherit default, got %v", d.Temperature)
	}
}

// --- ResolveDefaultAgentID ---

func TestResolveDefaultAgentID_NoDefault(t *testing.T) {
	cfg := Default()
	got := cfg.ResolveDefaultAgentID()
	if got != DefaultAgentID {
		t.Errorf("expected %q, got %q", DefaultAgentID, got)
	}
}

func TestResolveDefaultAgentID_Explicit(t *testing.T) {
	cfg := Default()
	cfg.Agents.List = map[string]AgentSpec{
		"agent-a": {Default: false},
		"agent-b": {Default: true},
	}
	got := cfg.ResolveDefaultAgentID()
	if got != "agent-b" {
		t.Errorf("expected agent-b, got %q", got)
	}
}

// --- ResolveDisplayName ---

func TestResolveDisplayName_Default(t *testing.T) {
	cfg := Default()
	got := cfg.ResolveDisplayName("nonexistent")
	if got != "GoClaw" {
		t.Errorf("expected GoClaw fallback, got %q", got)
	}
}

func TestResolveDisplayName_Set(t *testing.T) {
	cfg := Default()
	cfg.Agents.List = map[string]AgentSpec{
		"myagent": {DisplayName: "My Custom Agent"},
	}
	got := cfg.ResolveDisplayName("myagent")
	if got != "My Custom Agent" {
		t.Errorf("expected 'My Custom Agent', got %q", got)
	}
}

// --- Hash ---

func TestConfigHash(t *testing.T) {
	cfg1 := Default()
	cfg2 := Default()

	h1 := cfg1.Hash()
	h2 := cfg2.Hash()

	if h1 != h2 {
		t.Errorf("identical configs should have same hash: %q vs %q", h1, h2)
	}

	// Changing a field should change the hash
	cfg2.Gateway.Port = 9999
	h3 := cfg2.Hash()
	if h1 == h3 {
		t.Error("modified config should have different hash")
	}
}

// --- ReplaceFrom ---

func TestReplaceFrom(t *testing.T) {
	dst := Default()
	src := Default()
	src.Gateway.Port = 12345
	src.DataDir = "/custom/data"

	dst.ReplaceFrom(src)

	if dst.Gateway.Port != 12345 {
		t.Errorf("ReplaceFrom should copy port, got %d", dst.Gateway.Port)
	}
	if dst.DataDir != "/custom/data" {
		t.Errorf("ReplaceFrom should copy DataDir, got %q", dst.DataDir)
	}
}

// --- CronConfig helpers ---

func TestCronConfig_JobTimeoutDuration_Default(t *testing.T) {
	cc := CronConfig{}
	got := cc.JobTimeoutDuration()
	if got != DefaultJobTimeout {
		t.Errorf("expected default timeout %v, got %v", DefaultJobTimeout, got)
	}
}

func TestCronConfig_JobTimeoutDuration_Custom(t *testing.T) {
	cc := CronConfig{JobTimeout: "5m"}
	got := cc.JobTimeoutDuration()
	if got != 5*time.Minute {
		t.Errorf("expected 5m, got %v", got)
	}
}

func TestCronConfig_JobTimeoutDuration_Invalid(t *testing.T) {
	cc := CronConfig{JobTimeout: "not-a-duration"}
	got := cc.JobTimeoutDuration()
	if got != DefaultJobTimeout {
		t.Errorf("invalid duration should fall back to default, got %v", got)
	}
}

func TestCronConfig_ToRetryConfig_Defaults(t *testing.T) {
	cc := CronConfig{}
	got := cc.ToRetryConfig()
	// Should apply cron default retry config
	if got.MaxRetries <= 0 {
		t.Errorf("default MaxRetries should be positive, got %d", got.MaxRetries)
	}
}

func TestCronConfig_ToRetryConfig_Custom(t *testing.T) {
	cc := CronConfig{
		MaxRetries:     5,
		RetryBaseDelay: "1s",
		RetryMaxDelay:  "60s",
	}
	got := cc.ToRetryConfig()
	if got.MaxRetries != 5 {
		t.Errorf("expected MaxRetries=5, got %d", got.MaxRetries)
	}
	if got.BaseDelay != time.Second {
		t.Errorf("expected BaseDelay=1s, got %v", got.BaseDelay)
	}
	if got.MaxDelay != 60*time.Second {
		t.Errorf("expected MaxDelay=60s, got %v", got.MaxDelay)
	}
}

// --- ApplySystemConfigs ---

func TestApplySystemConfigs(t *testing.T) {
	cfg := Default()
	cfg.ApplySystemConfigs(map[string]string{
		"agent.default_provider":   "openai",
		"agent.default_model":      "gpt-4o",
		"agent.context_window":     "100000",
		"gateway.rate_limit_rpm":   "60",
		"gateway.max_message_chars": "50000",
	})

	if cfg.Agents.Defaults.Provider != "openai" {
		t.Errorf("provider: got %q", cfg.Agents.Defaults.Provider)
	}
	if cfg.Agents.Defaults.Model != "gpt-4o" {
		t.Errorf("model: got %q", cfg.Agents.Defaults.Model)
	}
	if cfg.Agents.Defaults.ContextWindow != 100000 {
		t.Errorf("context_window: got %d", cfg.Agents.Defaults.ContextWindow)
	}
	if cfg.Gateway.RateLimitRPM != 60 {
		t.Errorf("rate_limit_rpm: got %d", cfg.Gateway.RateLimitRPM)
	}
}

// --- Save / Load round-trip ---

func TestSaveLoad_RoundTrip(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.json")

	cfg := Default()
	cfg.Gateway.Port = 7654
	cfg.DataDir = "/test/data"

	if err := Save(path, cfg); err != nil {
		t.Fatalf("Save error: %v", err)
	}

	loaded, err := Load(path)
	if err != nil {
		t.Fatalf("Load error: %v", err)
	}

	if loaded.Gateway.Port != 7654 {
		t.Errorf("port round-trip: got %d", loaded.Gateway.Port)
	}
	if loaded.DataDir != "/test/data" {
		t.Errorf("DataDir round-trip: got %q", loaded.DataDir)
	}
}

// --- Env fallback: GOCLAW_PROVIDER / GOCLAW_MODEL only used when config is empty ---

func TestLoad_EnvFallback_OnlyWhenEmpty(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "cfg.json5")
	os.WriteFile(path, []byte(`{"agents":{"defaults":{"provider":"anthropic","model":"claude-3-5"}}}`), 0644)

	t.Setenv("GOCLAW_PROVIDER", "openai")
	t.Setenv("GOCLAW_MODEL", "gpt-4o")

	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("load error: %v", err)
	}
	// File has provider=anthropic → env fallback should NOT override it
	if cfg.Agents.Defaults.Provider != "anthropic" {
		t.Errorf("file provider should win over env fallback: got %q", cfg.Agents.Defaults.Provider)
	}
}

// --- Env override: GOCLAW_DATA_DIR ---

func TestLoad_DataDirEnvOverride(t *testing.T) {
	t.Setenv("GOCLAW_DATA_DIR", "/env/data")
	cfg, err := Load("/nonexistent/path")
	if err != nil {
		t.Fatalf("load error: %v", err)
	}
	if cfg.DataDir != "/env/data" {
		t.Errorf("GOCLAW_DATA_DIR not applied: got %q", cfg.DataDir)
	}
}

// --- Telemetry env overrides ---

func TestLoad_TelemetryEnvOverrides(t *testing.T) {
	t.Setenv("GOCLAW_TELEMETRY_ENABLED", "true")
	t.Setenv("GOCLAW_TELEMETRY_ENDPOINT", "localhost:4317")
	t.Setenv("GOCLAW_TELEMETRY_PROTOCOL", "grpc")

	cfg, err := Load("/nonexistent/path")
	if err != nil {
		t.Fatalf("load error: %v", err)
	}
	if !cfg.Telemetry.Enabled {
		t.Error("GOCLAW_TELEMETRY_ENABLED=true should enable telemetry")
	}
	if cfg.Telemetry.Endpoint != "localhost:4317" {
		t.Errorf("telemetry endpoint: got %q", cfg.Telemetry.Endpoint)
	}
}

// --- Sandbox env overrides ---

func TestLoad_SandboxEnvOverrides(t *testing.T) {
	t.Setenv("GOCLAW_SANDBOX_MODE", "all")
	t.Setenv("GOCLAW_SANDBOX_IMAGE", "my-sandbox:latest")
	t.Setenv("GOCLAW_SANDBOX_MEMORY_MB", "1024")

	cfg, err := Load("/nonexistent/path")
	if err != nil {
		t.Fatalf("load error: %v", err)
	}
	if cfg.Agents.Defaults.Sandbox == nil {
		t.Fatal("sandbox config should not be nil after env override")
	}
	if cfg.Agents.Defaults.Sandbox.Mode != "all" {
		t.Errorf("sandbox mode: got %q", cfg.Agents.Defaults.Sandbox.Mode)
	}
	if cfg.Agents.Defaults.Sandbox.Image != "my-sandbox:latest" {
		t.Errorf("sandbox image: got %q", cfg.Agents.Defaults.Sandbox.Image)
	}
	if cfg.Agents.Defaults.Sandbox.MemoryMB != 1024 {
		t.Errorf("sandbox memory: got %d", cfg.Agents.Defaults.Sandbox.MemoryMB)
	}
}

// --- SandboxConfig.ToSandboxConfig ---

func TestSandboxConfig_ToSandboxConfig_Nil(t *testing.T) {
	var sc *SandboxConfig
	got := sc.ToSandboxConfig()
	// Should return defaults without panic
	if got.TimeoutSec <= 0 {
		t.Errorf("expected positive timeout, got %d", got.TimeoutSec)
	}
}

func TestSandboxConfig_ToSandboxConfig_AllModes(t *testing.T) {
	tests := []struct {
		input string
	}{
		{"all"},
		{"non-main"},
		{"off"},
		{"unknown"},
	}
	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			sc := &SandboxConfig{Mode: tt.input, Image: "test:latest", MemoryMB: 256, CPUs: 2.0}
			got := sc.ToSandboxConfig()
			if tt.input == "all" || tt.input == "non-main" {
				// just ensure no panic
				_ = got
			}
			if got.Image != "test:latest" {
				t.Errorf("image not propagated: got %q", got.Image)
			}
			if got.MemoryMB != 256 {
				t.Errorf("memory not propagated: got %d", got.MemoryMB)
			}
			if got.CPUs != 2.0 {
				t.Errorf("cpus not propagated: got %v", got.CPUs)
			}
		})
	}
}

// --- Channel auto-enable ---

func TestLoad_ChannelAutoEnable_Telegram(t *testing.T) {
	t.Setenv("GOCLAW_TELEGRAM_TOKEN", "bot123:abc")

	cfg, err := Load("/nonexistent/path")
	if err != nil {
		t.Fatalf("load error: %v", err)
	}
	if !cfg.Channels.Telegram.Enabled {
		t.Error("Telegram should be auto-enabled when token is set")
	}
	if cfg.Channels.Telegram.Token != "bot123:abc" {
		t.Errorf("telegram token: got %q", cfg.Channels.Telegram.Token)
	}
}

func TestLoad_ChannelAutoEnable_Slack(t *testing.T) {
	t.Setenv("GOCLAW_SLACK_BOT_TOKEN", "xoxb-bot")
	t.Setenv("GOCLAW_SLACK_APP_TOKEN", "xapp-app")

	cfg, err := Load("/nonexistent/path")
	if err != nil {
		t.Fatalf("load error: %v", err)
	}
	if !cfg.Channels.Slack.Enabled {
		t.Error("Slack should be auto-enabled when both tokens are set")
	}
}
