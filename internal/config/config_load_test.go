package config

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

// --- Default ---

func TestDefault_SensibleDefaults(t *testing.T) {
	cfg := Default()

	if cfg.Gateway.Port != 18790 {
		t.Fatalf("default port: got %d, want 18790", cfg.Gateway.Port)
	}
	if cfg.Gateway.RateLimitRPM != 20 {
		t.Fatalf("default rate limit: got %d, want 20", cfg.Gateway.RateLimitRPM)
	}
	if cfg.Agents.Defaults.Provider != "anthropic" {
		t.Fatalf("default provider: got %q", cfg.Agents.Defaults.Provider)
	}
	if cfg.Agents.Defaults.MaxToolIterations != DefaultMaxIterations {
		t.Fatalf("default max iterations: got %d", cfg.Agents.Defaults.MaxToolIterations)
	}
	if cfg.Skills.EffectiveMaxUploadSizeMB() != DefaultSkillMaxUploadSizeMB {
		t.Fatalf("default skill upload max: got %d, want %d", cfg.Skills.EffectiveMaxUploadSizeMB(), DefaultSkillMaxUploadSizeMB)
	}
	if !cfg.Skills.SlashCommands.EffectiveEnabled() {
		t.Fatal("slash commands should default enabled")
	}
	if !cfg.Skills.SlashCommands.EffectiveSuggestNotFound() {
		t.Fatal("slash command suggestions should default enabled")
	}
	if cfg.Skills.SlashCommands.EffectivePartialMatching() {
		t.Fatal("slash command partial matching should default disabled")
	}
	if cfg.Skills.SlashCommands.EffectivePrefix() != "/" {
		t.Fatalf("slash command prefix = %q, want /", cfg.Skills.SlashCommands.EffectivePrefix())
	}

}

// --- Load with missing file → uses defaults ---

func TestLoad_MissingFile_UsesDefaults(t *testing.T) {
	cfg, err := Load("/nonexistent/path/config.json")
	if err != nil {
		t.Fatalf("missing file should not error: %v", err)
	}
	if cfg.Gateway.Port != 18790 {
		t.Fatalf("expected default port, got %d", cfg.Gateway.Port)
	}
}

// --- Load with valid JSON5 ---

func TestLoad_ValidJSON5(t *testing.T) {
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "config.json5")

	// JSON5: comments and trailing commas allowed
	content := `{
		// custom port
		"gateway": {
			"port": 9999,
			"rate_limit_rpm": 100,
		},
	}`
	os.WriteFile(cfgPath, []byte(content), 0644)

	cfg, err := Load(cfgPath)
	if err != nil {
		t.Fatalf("load error: %v", err)
	}
	if cfg.Gateway.Port != 9999 {
		t.Fatalf("port: got %d, want 9999", cfg.Gateway.Port)
	}
	if cfg.Gateway.RateLimitRPM != 100 {
		t.Fatalf("rate limit: got %d, want 100", cfg.Gateway.RateLimitRPM)
	}
	// Unset fields should retain defaults
	if cfg.Agents.Defaults.Provider != "anthropic" {
		t.Fatalf("default provider should be preserved: got %q", cfg.Agents.Defaults.Provider)
	}
}

// --- Load with invalid JSON5 → error ---

func TestLoad_InvalidJSON5(t *testing.T) {
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "config.json5")
	os.WriteFile(cfgPath, []byte(`{invalid json!!!`), 0644)

	_, err := Load(cfgPath)
	if err == nil {
		t.Fatal("expected error for invalid JSON5")
	}
}

// --- Env var override precedence ---

func TestLoad_EnvVarOverrides(t *testing.T) {
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "config.json5")
	os.WriteFile(cfgPath, []byte(`{"gateway":{"port":8080}}`), 0644)

	// Env override should win
	t.Setenv("GOCLAW_PORT", "7777")

	cfg, err := Load(cfgPath)
	if err != nil {
		t.Fatalf("load error: %v", err)
	}
	if cfg.Gateway.Port != 7777 {
		t.Fatalf("env override: got port %d, want 7777", cfg.Gateway.Port)
	}
}

func TestLoad_SkillsMaxUploadSizeFromFileAndEnv(t *testing.T) {
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "config.json5")
	os.WriteFile(cfgPath, []byte(`{"skills":{"max_upload_size_mb":64}}`), 0644)

	cfg, err := Load(cfgPath)
	if err != nil {
		t.Fatalf("load error: %v", err)
	}
	if cfg.Skills.EffectiveMaxUploadSizeMB() != 64 {
		t.Fatalf("file skill upload max: got %d, want 64", cfg.Skills.EffectiveMaxUploadSizeMB())
	}

	t.Setenv("GOCLAW_SKILLS_MAX_UPLOAD_SIZE_MB", "128")
	cfg, err = Load(cfgPath)
	if err != nil {
		t.Fatalf("load with env error: %v", err)
	}
	if cfg.Skills.EffectiveMaxUploadSizeMB() != 128 {
		t.Fatalf("env skill upload max: got %d, want 128", cfg.Skills.EffectiveMaxUploadSizeMB())
	}
}

func TestLoad_SkillSlashCommandsFromFileEnvAndSystemConfig(t *testing.T) {
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "config.json5")
	os.WriteFile(cfgPath, []byte(`{
		"skills": {
			"slash_commands": {
				"enabled": false,
				"suggest_not_found": false,
				"partial_matching": true,
				"prefix": "!"
			}
		}
	}`), 0644)

	cfg, err := Load(cfgPath)
	if err != nil {
		t.Fatalf("load error: %v", err)
	}
	if cfg.Skills.SlashCommands.EffectiveEnabled() {
		t.Fatal("file enabled override should be false")
	}
	if cfg.Skills.SlashCommands.EffectiveSuggestNotFound() {
		t.Fatal("file suggestion override should be false")
	}
	if !cfg.Skills.SlashCommands.EffectivePartialMatching() {
		t.Fatal("file partial matching override should be true")
	}
	if cfg.Skills.SlashCommands.EffectivePrefix() != "!" {
		t.Fatalf("file prefix = %q, want !", cfg.Skills.SlashCommands.EffectivePrefix())
	}

	t.Setenv("GOCLAW_SKILLS_SLASH_COMMANDS_ENABLED", "true")
	t.Setenv("GOCLAW_SKILLS_SLASH_COMMANDS_SUGGEST_NOT_FOUND", "true")
	t.Setenv("GOCLAW_SKILLS_SLASH_COMMANDS_PARTIAL_MATCHING", "false")
	t.Setenv("GOCLAW_SKILLS_SLASH_COMMANDS_PREFIX", "#")
	cfg, err = Load(cfgPath)
	if err != nil {
		t.Fatalf("load with env error: %v", err)
	}
	if !cfg.Skills.SlashCommands.EffectiveEnabled() {
		t.Fatal("env enabled override should be true")
	}
	if !cfg.Skills.SlashCommands.EffectiveSuggestNotFound() {
		t.Fatal("env suggestion override should be true")
	}
	if cfg.Skills.SlashCommands.EffectivePartialMatching() {
		t.Fatal("env partial matching override should be false")
	}
	if cfg.Skills.SlashCommands.EffectivePrefix() != "#" {
		t.Fatalf("env prefix = %q, want #", cfg.Skills.SlashCommands.EffectivePrefix())
	}

	cfg.ApplySystemConfigs(map[string]string{
		"skills.slash_commands.enabled":           "false",
		"skills.slash_commands.suggest_not_found": "false",
		"skills.slash_commands.partial_matching":  "true",
		"skills.slash_commands.prefix":            "%",
	})
	if cfg.Skills.SlashCommands.EffectiveEnabled() {
		t.Fatal("system enabled override should be false")
	}
	if cfg.Skills.SlashCommands.EffectiveSuggestNotFound() {
		t.Fatal("system suggestion override should be false")
	}
	if !cfg.Skills.SlashCommands.EffectivePartialMatching() {
		t.Fatal("system partial matching override should be true")
	}
	if cfg.Skills.SlashCommands.EffectivePrefix() != "%" {
		t.Fatalf("system prefix = %q, want %%", cfg.Skills.SlashCommands.EffectivePrefix())
	}
}

func TestSkillsMaxUploadSizeClampAndSystemConfigOverlay(t *testing.T) {
	cfg := Default()
	cfg.Skills.MaxUploadSizeMB = 0
	if got := cfg.Skills.EffectiveMaxUploadSizeMB(); got != DefaultSkillMaxUploadSizeMB {
		t.Fatalf("zero upload max: got %d, want %d", got, DefaultSkillMaxUploadSizeMB)
	}

	cfg.Skills.MaxUploadSizeMB = -10
	if got := cfg.Skills.EffectiveMaxUploadSizeMB(); got != MinSkillMaxUploadSizeMB {
		t.Fatalf("negative upload max: got %d, want %d", got, MinSkillMaxUploadSizeMB)
	}

	cfg.Skills.MaxUploadSizeMB = 999
	if got := cfg.Skills.EffectiveMaxUploadSizeMB(); got != MaxSkillMaxUploadSizeMB {
		t.Fatalf("high upload max: got %d, want %d", got, MaxSkillMaxUploadSizeMB)
	}

	cfg.ApplySystemConfigs(map[string]string{"skills.max_upload_size_mb": "77"})
	if got := cfg.Skills.EffectiveMaxUploadSizeMB(); got != 77 {
		t.Fatalf("system config upload max: got %d, want 77", got)
	}
}

func TestLoad_EnvVarOverrides_InvalidPort(t *testing.T) {
	t.Setenv("GOCLAW_PORT", "not-a-number")

	cfg, err := Load("/nonexistent/path")
	if err != nil {
		t.Fatalf("load error: %v", err)
	}
	// Invalid port should keep default
	if cfg.Gateway.Port != 18790 {
		t.Fatalf("invalid port env should keep default: got %d", cfg.Gateway.Port)
	}
}

func TestValidateGatewayAuthRejectsExternalNoToken(t *testing.T) {
	cfg := Default()
	cfg.Gateway.Host = "0.0.0.0"
	cfg.Gateway.Token = ""
	t.Setenv(GatewayAllowInsecureNoAuthEnv, "")

	if err := ValidateGatewayAuth(cfg.Gateway); err == nil {
		t.Fatal("expected external bind with empty gateway token to fail")
	}
}

func TestValidateGatewayAuthAllowsLoopbackNoToken(t *testing.T) {
	cfg := Default()
	cfg.Gateway.Host = "127.0.0.1"
	cfg.Gateway.Token = ""
	t.Setenv(GatewayAllowInsecureNoAuthEnv, "")

	if err := ValidateGatewayAuth(cfg.Gateway); err != nil {
		t.Fatalf("loopback no-token mode should be allowed: %v", err)
	}
}

func TestValidateGatewayAuthAllowsExplicitInsecureOptIn(t *testing.T) {
	cfg := Default()
	cfg.Gateway.Host = "0.0.0.0"
	cfg.Gateway.Token = ""
	t.Setenv(GatewayAllowInsecureNoAuthEnv, "1")

	if err := ValidateGatewayAuth(cfg.Gateway); err != nil {
		t.Fatalf("explicit insecure opt-in should allow no-token mode: %v", err)
	}
}

// --- Env var for API keys ---

func TestLoad_EnvVarAPIKeys(t *testing.T) {
	t.Setenv("GOCLAW_ANTHROPIC_API_KEY", "sk-test-key")

	cfg, err := Load("/nonexistent/path")
	if err != nil {
		t.Fatalf("load error: %v", err)
	}
	if cfg.Providers.Anthropic.APIKey != "sk-test-key" {
		t.Fatalf("anthropic key: got %q", cfg.Providers.Anthropic.APIKey)
	}
}

// --- Allowed origins from JSON5 ---

func TestLoad_AllowedOrigins_JSON5(t *testing.T) {
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "config.json5")

	content := `{
		"gateway": {
			"allowed_origins": [
				"https://app.example.com",
				"https://admin.example.com",
				"http://localhost:3002",
			],
		},
	}`
	os.WriteFile(cfgPath, []byte(content), 0644)

	cfg, err := Load(cfgPath)
	if err != nil {
		t.Fatalf("load error: %v", err)
	}
	if len(cfg.Gateway.AllowedOrigins) != 3 {
		t.Fatalf("expected 3 origins, got %d: %v", len(cfg.Gateway.AllowedOrigins), cfg.Gateway.AllowedOrigins)
	}
	if cfg.Gateway.AllowedOrigins[0] != "https://app.example.com" {
		t.Fatalf("first origin: got %q", cfg.Gateway.AllowedOrigins[0])
	}
	if cfg.Gateway.AllowedOrigins[2] != "http://localhost:3002" {
		t.Fatalf("third origin: got %q", cfg.Gateway.AllowedOrigins[2])
	}
}

// --- Allowed origins from env var ---

func TestLoad_AllowedOrigins_EnvVar(t *testing.T) {
	t.Setenv("GOCLAW_ALLOWED_ORIGINS", " https://a.com , https://b.com ")

	cfg, err := Load("/nonexistent/path")
	if err != nil {
		t.Fatalf("load error: %v", err)
	}
	if len(cfg.Gateway.AllowedOrigins) != 2 {
		t.Fatalf("expected 2 origins, got %d: %v", len(cfg.Gateway.AllowedOrigins), cfg.Gateway.AllowedOrigins)
	}
	if cfg.Gateway.AllowedOrigins[0] != "https://a.com" || cfg.Gateway.AllowedOrigins[1] != "https://b.com" {
		t.Fatalf("origins not parsed correctly: %v", cfg.Gateway.AllowedOrigins)
	}
}

func TestLoad_AllowedOrigins_EnvVar_OverridesFile(t *testing.T) {
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "config.json5")
	os.WriteFile(cfgPath, []byte(`{"gateway":{"allowed_origins":["https://file.com"]}}`), 0644)

	// Env var should override file value
	t.Setenv("GOCLAW_ALLOWED_ORIGINS", "https://env.com")

	cfg, err := Load(cfgPath)
	if err != nil {
		t.Fatalf("load error: %v", err)
	}
	if len(cfg.Gateway.AllowedOrigins) != 1 || cfg.Gateway.AllowedOrigins[0] != "https://env.com" {
		t.Fatalf("env should override file: got %v", cfg.Gateway.AllowedOrigins)
	}
}

// --- FlexibleStringSlice ---

func TestFlexibleStringSlice_StringArray(t *testing.T) {
	var f FlexibleStringSlice
	err := json.Unmarshal([]byte(`["a","b","c"]`), &f)
	if err != nil {
		t.Fatalf("unmarshal error: %v", err)
	}
	if len(f) != 3 || f[0] != "a" || f[1] != "b" || f[2] != "c" {
		t.Fatalf("got %v", f)
	}
}

func TestFlexibleStringSlice_MixedArray(t *testing.T) {
	var f FlexibleStringSlice
	// Numbers and strings mixed
	err := json.Unmarshal([]byte(`["user1", 12345, "user2"]`), &f)
	if err != nil {
		t.Fatalf("unmarshal error: %v", err)
	}
	if len(f) != 3 || f[0] != "user1" || f[1] != "12345" || f[2] != "user2" {
		t.Fatalf("got %v", f)
	}
}

func TestFlexibleStringSlice_EmptyArray(t *testing.T) {
	var f FlexibleStringSlice
	err := json.Unmarshal([]byte(`[]`), &f)
	if err != nil {
		t.Fatalf("unmarshal error: %v", err)
	}
	if len(f) != 0 {
		t.Fatalf("expected empty, got %v", f)
	}
}

// --- Owner IDs parsing ---

func TestLoad_OwnerIDsParsing(t *testing.T) {
	t.Setenv("GOCLAW_OWNER_IDS", " alice , bob , charlie ")

	cfg, err := Load("/nonexistent/path")
	if err != nil {
		t.Fatalf("load error: %v", err)
	}
	if len(cfg.Gateway.OwnerIDs) != 3 {
		t.Fatalf("expected 3 owner IDs, got %d: %v", len(cfg.Gateway.OwnerIDs), cfg.Gateway.OwnerIDs)
	}
	if cfg.Gateway.OwnerIDs[0] != "alice" || cfg.Gateway.OwnerIDs[1] != "bob" || cfg.Gateway.OwnerIDs[2] != "charlie" {
		t.Fatalf("owner IDs not trimmed: %v", cfg.Gateway.OwnerIDs)
	}
}

func TestLoad_OwnerIDsEmpty(t *testing.T) {
	t.Setenv("GOCLAW_OWNER_IDS", "")

	cfg, err := Load("/nonexistent/path")
	if err != nil {
		t.Fatalf("load error: %v", err)
	}
	// Empty string should not produce [""]
	for _, id := range cfg.Gateway.OwnerIDs {
		if id == "" {
			t.Fatal("empty owner ID should not be included")
		}
	}
}
