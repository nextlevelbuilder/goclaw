package config

// AudioConfig is the optional cfg.Audio block for STT/Music static defaults.
// Pointer-typed field on Config so absent/empty JSON silently decodes as nil —
// existing config.json files that predate Phase 1 continue to load unchanged.
//
// Phase 1 ships the shape only (no consumers yet). Phase 3 wires Music; Phase 4
// wires STT. In both phases the Manager continues to honor per-tenant
// builtin_tools settings regardless of whether cfg.Audio is set.
type AudioConfig struct {
	Stt   *AudioSTTConfig   `json:"stt,omitempty"`
	Music *AudioMusicConfig `json:"music,omitempty"`
}

// AudioSTTConfig configures optional static STT defaults. Empty = "no global
// default; rely on tenant builtin_tools[stt] or per-channel STTProxyURL".
type AudioSTTConfig struct {
	Provider   string `json:"provider,omitempty"`    // primary provider name (e.g. "scribe")
	APIKey     string `json:"api_key,omitempty"`     // may be overridden by env / secrets
	BaseURL    string `json:"base_url,omitempty"`    // override for enterprise deploys
	Model      string `json:"model,omitempty"`       // provider-specific
	Language   string `json:"language,omitempty"`    // BCP-47 hint
	Fallback   string `json:"fallback,omitempty"`    // provider name to try on primary failure
	TimeoutMs  int    `json:"timeout_ms,omitempty"`  // default 30000
}

// AudioMusicConfig configures optional static Music defaults.
type AudioMusicConfig struct {
	Provider  string `json:"provider,omitempty"`  // primary provider name (e.g. "elevenlabs")
	APIKey    string `json:"api_key,omitempty"`
	BaseURL   string `json:"base_url,omitempty"`
	Model     string `json:"model,omitempty"`
	Fallback  string `json:"fallback,omitempty"`
	TimeoutMs int    `json:"timeout_ms,omitempty"`
}
