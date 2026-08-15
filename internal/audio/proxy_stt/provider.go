// Package proxy_stt wraps internal/channels/media.TranscribeAudio as an
// audio.STTProvider, preserving all existing proxy behavior for backward compat.
package proxy_stt

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/nextlevelbuilder/goclaw/internal/audio"
	"github.com/nextlevelbuilder/goclaw/internal/channels/media"
)

// Provider wraps media.TranscribeAudio as an audio.STTProvider.
// Preserves all proxy behaviors (Bearer auth, tenant_id field, semaphore, etc.)
// so channels can fall back to the proxy without any behavior change.
type Provider struct {
	cfg media.STTConfig
}

// NewProvider returns a proxy STT provider backed by media.TranscribeAudio.
func NewProvider(cfg media.STTConfig) *Provider {
	return &Provider{cfg: cfg}
}

// Name returns the stable provider identifier.
func (p *Provider) Name() string { return "proxy" }

// Transcribe delegates to media.TranscribeAudio. When in.FilePath is empty but
// in.Bytes is present, writes a 0600 temp file and defers cleanup.
// Returns ("", nil) when proxy URL is empty or filePath is empty (proxy no-op).
func (p *Provider) Transcribe(ctx context.Context, in audio.STTInput, _ audio.STTOptions) (*audio.TranscriptResult, error) {
	filePath := in.FilePath

	if filePath == "" && len(in.Bytes) > 0 {
		ext := extFromMime(in.MimeType)
		f, err := os.CreateTemp("", "stt-proxy-*"+ext)
		if err != nil {
			return nil, fmt.Errorf("proxy_stt: create temp file: %w", err)
		}
		if err := os.Chmod(f.Name(), 0600); err != nil {
			f.Close()
			os.Remove(f.Name())
			return nil, fmt.Errorf("proxy_stt: chmod temp file: %w", err)
		}
		if _, err := f.Write(in.Bytes); err != nil {
			f.Close()
			os.Remove(f.Name())
			return nil, fmt.Errorf("proxy_stt: write temp file: %w", err)
		}
		f.Close()
		defer os.Remove(f.Name())
		filePath = f.Name()
	}

	text, err := media.TranscribeAudio(ctx, p.cfg, filePath)
	if err != nil {
		return nil, err
	}
	return &audio.TranscriptResult{
		Text:     text,
		Provider: "proxy",
	}, nil
}

// extFromMime returns a file extension for a MIME type.
func extFromMime(mime string) string {
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
		return filepath.Ext(mime) // fallback; usually empty
	}
}
