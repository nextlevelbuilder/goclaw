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
