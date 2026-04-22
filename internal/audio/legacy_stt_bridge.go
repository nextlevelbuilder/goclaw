package audio

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/nextlevelbuilder/goclaw/internal/channels/media"
	"github.com/nextlevelbuilder/goclaw/internal/config"
)

// bridgedChannels tracks channels already registered per manager for idempotency.
var (
	bridgedMu       sync.Mutex
	bridgedChannels = make(map[*Manager]map[string]bool)
)

// BridgeLegacySTT reads per-channel STTProxyURL from cfg and registers a
// channel-scoped proxy STT provider for each non-empty URL. Emits a deprecation
// warning once per channel per manager. Safe to call multiple times (idempotent).
//
// Channel override wins over tenant builtin_tools[stt] per Decision 2.
func BridgeLegacySTT(mgr *Manager, cfg *config.Config) {
	type chanEntry struct {
		Name string
		URL  string
		Key  string
		TID  string
		TOut int
	}

	channels := []chanEntry{
		{
			Name: "telegram",
			URL:  cfg.Channels.Telegram.STTProxyURL,
			Key:  cfg.Channels.Telegram.STTAPIKey,
			TID:  cfg.Channels.Telegram.STTTenantID,
			TOut: cfg.Channels.Telegram.STTTimeoutSeconds,
		},
		{
			Name: "feishu",
			URL:  cfg.Channels.Feishu.STTProxyURL,
			Key:  cfg.Channels.Feishu.STTAPIKey,
			TID:  cfg.Channels.Feishu.STTTenantID,
			TOut: cfg.Channels.Feishu.STTTimeoutSeconds,
		},
		{
			Name: "discord",
			URL:  cfg.Channels.Discord.STTProxyURL,
			Key:  cfg.Channels.Discord.STTAPIKey,
			TID:  cfg.Channels.Discord.STTTenantID,
			TOut: cfg.Channels.Discord.STTTimeoutSeconds,
		},
	}

	bridgedMu.Lock()
	defer bridgedMu.Unlock()

	if bridgedChannels[mgr] == nil {
		bridgedChannels[mgr] = make(map[string]bool)
	}
	already := bridgedChannels[mgr]

	for _, ch := range channels {
		if ch.URL == "" {
			continue
		}
		if already[ch.Name] {
			continue // idempotent — skip re-registration
		}

		slog.Warn("security.stt_legacy_config_deprecated",
			"channel", ch.Name,
			"url_suffix", lastN(ch.URL, 8),
		)

		p := &legacyBridgeSTTProvider{
			cfg:     media.STTConfig{ProxyURL: ch.URL, APIKey: ch.Key, TenantID: ch.TID, TimeoutSeconds: ch.TOut},
			channel: ch.Name,
		}
		mgr.RegisterChannelSTT(ch.Name, p)
		already[ch.Name] = true
	}
}

// legacyBridgeSTTProvider is a thin adapter that wraps media.TranscribeAudio as
// an STTProvider. Defined here (not in proxy_stt) to avoid a circular import:
// proxy_stt → audio → proxy_stt.
type legacyBridgeSTTProvider struct {
	cfg     media.STTConfig
	channel string
}

func (p *legacyBridgeSTTProvider) Name() string { return "proxy" }

func (p *legacyBridgeSTTProvider) Transcribe(ctx context.Context, in STTInput, _ STTOptions) (*TranscriptResult, error) {
	filePath := in.FilePath
	if filePath == "" && len(in.Bytes) > 0 {
		ext := legacyExtFromMime(in.MimeType)
		f, err := os.CreateTemp("", "stt-bridge-*"+ext)
		if err != nil {
			return nil, fmt.Errorf("legacy_bridge stt: create temp: %w", err)
		}
		if err := os.Chmod(f.Name(), 0600); err != nil {
			f.Close()
			os.Remove(f.Name())
			return nil, fmt.Errorf("legacy_bridge stt: chmod: %w", err)
		}
		if _, err := f.Write(in.Bytes); err != nil {
			f.Close()
			os.Remove(f.Name())
			return nil, fmt.Errorf("legacy_bridge stt: write: %w", err)
		}
		f.Close()
		defer os.Remove(f.Name())
		filePath = f.Name()
	}

	text, err := media.TranscribeAudio(ctx, p.cfg, filePath)
	if err != nil {
		return nil, err
	}
	return &TranscriptResult{Text: text, Provider: "proxy"}, nil
}

// lastN returns the last n characters of s (for safe URL logging).
func lastN(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return "..." + s[len(s)-n:]
}

// legacyExtFromMime returns a file extension for a MIME type.
func legacyExtFromMime(mime string) string {
	switch strings.Split(mime, ";")[0] {
	case "audio/ogg":
		return ".ogg"
	case "audio/mpeg", "audio/mp3":
		return ".mp3"
	case "audio/wav", "audio/wave":
		return ".wav"
	case "audio/mp4", "audio/m4a":
		return ".m4a"
	case "audio/webm":
		return ".webm"
	case "audio/flac":
		return ".flac"
	default:
		return filepath.Ext(mime)
	}
}
