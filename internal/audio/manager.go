package audio

import (
	"context"
	"fmt"
	"log/slog"
)

// ctxKeyChannel is the context key for the current channel name (e.g. "telegram").
// Set via WithChannel; read by resolveSTTChain to select channel-scoped providers.
type ctxKeyChannelType struct{}

var ctxKeyChannel = ctxKeyChannelType{}

// WithChannel returns a context that carries the channel name for STT chain resolution.
func WithChannel(ctx context.Context, channel string) context.Context {
	return context.WithValue(ctx, ctxKeyChannel, channel)
}

// channelFromCtx extracts the channel name from ctx, or "" if not set.
func channelFromCtx(ctx context.Context) string {
	v, _ := ctx.Value(ctxKeyChannel).(string)
	return v
}

// TenantTTSResolver resolves per-tenant TTS provider and config.
// Returns (provider, providerName, autoMode, error). If error is non-nil,
// caller should fall back to global manager config.
type TenantTTSResolver func(ctx context.Context) (TTSProvider, string, AutoMode, error)

// Manager orchestrates audio providers across TTS, STT, Music, and SFX
// operations. Each op has its own provider map + primary/fallback chain.
//
// Phase 1 exercises the TTS path end-to-end. STT/Music/SFX maps and chains
// are present but empty (providers register in Phase 3/4).
type Manager struct {
	ttsProviders   map[string]TTSProvider
	sttProviders   map[string]STTProvider
	musicProviders map[string]MusicProvider
	sfxProviders   map[string]SFXProvider

	primary             string            // primary TTS provider
	sttChain            []string          // STT fallback order (Phase 4)
	musicChain          []string          // Music fallback order (Phase 3)
	channelSTTOverrides map[string][]string // channel → provider key list (Phase 4)

	auto      AutoMode
	mode      Mode
	maxLength int // max text length before truncation (default 1500)
	timeoutMs int // provider timeout (default 30000)

	tenantResolver TenantTTSResolver // per-tenant TTS config resolver (nil = use global)
}

// ManagerConfig configures the audio manager. Preserved from legacy TTS
// package — new STT/Music fields are set via RegisterSTT/RegisterMusic and
// (optionally) cfg.Audio in config_audio.go.
type ManagerConfig struct {
	Primary   string   // primary TTS provider name
	Auto      AutoMode // auto-apply mode (default "off")
	Mode      Mode     // "final" or "all" (default "final")
	MaxLength int      // default 1500
	TimeoutMs int      // default 30000
}

// NewManager creates an audio manager with empty provider maps.
func NewManager(cfg ManagerConfig) *Manager {
	m := &Manager{
		ttsProviders:        make(map[string]TTSProvider),
		sttProviders:        make(map[string]STTProvider),
		musicProviders:      make(map[string]MusicProvider),
		sfxProviders:        make(map[string]SFXProvider),
		channelSTTOverrides: make(map[string][]string),
		primary:             cfg.Primary,
		auto:                cfg.Auto,
		mode:                cfg.Mode,
		maxLength:           cfg.MaxLength,
		timeoutMs:           cfg.TimeoutMs,
	}
	if m.auto == "" {
		m.auto = AutoOff
	}
	if m.mode == "" {
		m.mode = ModeFinal
	}
	if m.maxLength <= 0 {
		m.maxLength = 1500
	}
	if m.timeoutMs <= 0 {
		m.timeoutMs = 30000
	}
	return m
}

// ---- Registration ----

// RegisterTTS adds a TTS provider. If no primary is set, the first registered
// provider becomes primary — matches legacy tts.Manager.RegisterProvider.
func (m *Manager) RegisterTTS(p TTSProvider) {
	m.ttsProviders[p.Name()] = p
	if m.primary == "" {
		m.primary = p.Name()
	}
}

// RegisterProvider is a backward-compat alias for RegisterTTS — lets pre-Phase-1
// callers that go through tts.Manager (= audio.Manager via alias) keep working.
func (m *Manager) RegisterProvider(p TTSProvider) { m.RegisterTTS(p) }

// RegisterSTT adds an STT provider (Phase 4).
func (m *Manager) RegisterSTT(p STTProvider) {
	m.sttProviders[p.Name()] = p
}

// RegisterMusic adds a music provider (Phase 3).
func (m *Manager) RegisterMusic(p MusicProvider) {
	m.musicProviders[p.Name()] = p
}

// RegisterSFX adds an SFX provider (Phase 3).
func (m *Manager) RegisterSFX(p SFXProvider) {
	m.sfxProviders[p.Name()] = p
}

// ---- Introspection ----

// GetProvider returns a TTS provider by name. Preserved from legacy API.
func (m *Manager) GetProvider(name string) (TTSProvider, bool) {
	p, ok := m.ttsProviders[name]
	return p, ok
}

// PrimaryProvider returns the primary TTS provider name.
func (m *Manager) PrimaryProvider() string { return m.primary }

// SetTenantResolver sets the per-tenant TTS config resolver.
// Channels use this to resolve tenant-specific TTS providers.
func (m *Manager) SetTenantResolver(r TenantTTSResolver) { m.tenantResolver = r }

// ResolveTenantProvider attempts to get tenant-specific TTS provider.
// Returns (provider, name, autoMode, ok). If ok=false, caller uses global config.
func (m *Manager) ResolveTenantProvider(ctx context.Context) (TTSProvider, string, AutoMode, bool) {
	if m.tenantResolver == nil {
		return nil, "", "", false
	}
	p, name, auto, err := m.tenantResolver(ctx)
	if err != nil {
		return nil, "", "", false
	}
	return p, name, auto, true
}

// AutoMode returns the current auto-apply mode.
func (m *Manager) AutoMode() AutoMode { return m.auto }

// HasProviders reports whether any TTS provider is registered.
func (m *Manager) HasProviders() bool { return len(m.ttsProviders) > 0 }

// ---- TTS dispatch ----

// Synthesize uses the primary provider.
func (m *Manager) Synthesize(ctx context.Context, text string, opts TTSOptions) (*SynthResult, error) {
	p, ok := m.ttsProviders[m.primary]
	if !ok {
		return nil, fmt.Errorf("tts provider not found: %s", m.primary)
	}
	return p.Synthesize(ctx, text, opts)
}

// SynthesizeStream dispatches streaming TTS to the primary provider. Returns
// ErrStreamingNotSupported if the primary does not implement
// StreamingTTSProvider, letting callers fall back to buffered Synthesize.
func (m *Manager) SynthesizeStream(ctx context.Context, text string, opts TTSOptions) (*StreamResult, error) {
	p, ok := m.ttsProviders[m.primary]
	if !ok {
		return nil, fmt.Errorf("tts provider not found: %s", m.primary)
	}
	sp, ok := p.(StreamingTTSProvider)
	if !ok {
		return nil, ErrStreamingNotSupported
	}
	return sp.SynthesizeStream(ctx, text, opts)
}

// ---- Music dispatch ----

// GenerateMusic tries registered music providers in chain order until one succeeds.
// Chain order: elevenlabs first (if registered), then remaining providers.
// Override order by setting m.musicChain before the first call.
func (m *Manager) GenerateMusic(ctx context.Context, opts MusicOptions) (*AudioResult, error) {
	chain := m.resolveMusicChain()
	if len(chain) == 0 {
		return nil, fmt.Errorf("no music providers registered")
	}
	var lastErr error
	for _, name := range chain {
		p, ok := m.musicProviders[name]
		if !ok {
			slog.Info("audio.music provider not registered, skipping", "provider", name)
			continue
		}
		if res, err := p.GenerateMusic(ctx, opts); err == nil {
			return res, nil
		} else {
			slog.Warn("audio.music provider failed", "provider", name, "error", err)
			lastErr = err
		}
	}
	if lastErr != nil {
		return nil, fmt.Errorf("all music providers failed: %w", lastErr)
	}
	return nil, fmt.Errorf("no music providers registered")
}

// GenerateSFX tries SFX providers in order: elevenlabs first, then any other registered.
func (m *Manager) GenerateSFX(ctx context.Context, opts SFXOptions) (*AudioResult, error) {
	order := m.resolveSFXChain()
	if len(order) == 0 {
		return nil, fmt.Errorf("no sfx providers registered")
	}
	var lastErr error
	for _, name := range order {
		p, ok := m.sfxProviders[name]
		if !ok {
			continue
		}
		if res, err := p.GenerateSFX(ctx, opts); err == nil {
			return res, nil
		} else {
			slog.Warn("audio.sfx provider failed", "provider", name, "error", err)
			lastErr = err
		}
	}
	if lastErr != nil {
		return nil, fmt.Errorf("all sfx providers failed: %w", lastErr)
	}
	return nil, fmt.Errorf("no sfx providers registered")
}

// resolveMusicChain returns the ordered provider names for music generation.
// If m.musicChain is set explicitly it is used as-is; otherwise elevenlabs is
// preferred and remaining providers follow in registration order.
func (m *Manager) resolveMusicChain() []string {
	if len(m.musicChain) > 0 {
		return m.musicChain
	}
	out := make([]string, 0, len(m.musicProviders))
	if _, ok := m.musicProviders["elevenlabs"]; ok {
		out = append(out, "elevenlabs")
	}
	for name := range m.musicProviders {
		if name != "elevenlabs" {
			out = append(out, name)
		}
	}
	return out
}

// resolveSFXChain returns the ordered provider names for SFX generation.
func (m *Manager) resolveSFXChain() []string {
	out := make([]string, 0, len(m.sfxProviders))
	if _, ok := m.sfxProviders["elevenlabs"]; ok {
		out = append(out, "elevenlabs")
	}
	for name := range m.sfxProviders {
		if name != "elevenlabs" {
			out = append(out, name)
		}
	}
	return out
}

// SynthesizeWithFallback tries primary first, then any other registered
// provider on error. Returns first success or aggregate failure.
func (m *Manager) SynthesizeWithFallback(ctx context.Context, text string, opts TTSOptions) (*SynthResult, error) {
	if p, ok := m.ttsProviders[m.primary]; ok {
		if result, err := p.Synthesize(ctx, text, opts); err == nil {
			return result, nil
		} else {
			slog.Warn("tts primary provider failed, trying fallback", "provider", m.primary, "error", err)
		}
	}
	for name, p := range m.ttsProviders {
		if name == m.primary {
			continue
		}
		result, err := p.Synthesize(ctx, text, opts)
		if err == nil {
			slog.Info("tts fallback succeeded", "provider", name)
			return result, nil
		}
		slog.Warn("tts fallback provider failed", "provider", name, "error", err)
	}
	return nil, fmt.Errorf("all tts providers failed")
}
