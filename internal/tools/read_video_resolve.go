package tools

import (
	"context"
	"encoding/base64"
	"fmt"
	"log/slog"
	"path/filepath"
	"time"

	"github.com/nextlevelbuilder/goclaw/internal/providers"
)

// resolveVideoFile finds the video file path from context MediaRefs.
func (t *ReadVideoTool) resolveVideoFile(ctx context.Context, mediaID string) (path, mime string, err error) {
	if t.mediaLoader == nil {
		return "", "", fmt.Errorf("no media storage configured — cannot access video files")
	}

	refs := MediaVideoRefsFromCtx(ctx)
	if len(refs) == 0 {
		return "", "", fmt.Errorf("no video files available in this conversation. The user may not have sent a video file.")
	}

	var ref *providers.MediaRef
	if mediaID != "" {
		for i := range refs {
			if refs[i].ID == mediaID {
				ref = &refs[i]
				break
			}
		}
		if ref == nil {
			return "", "", fmt.Errorf("video with media_id %q not found in conversation", mediaID)
		}
	} else {
		ref = &refs[len(refs)-1]
	}

	// Prefer persisted workspace path; fall back to legacy .media/ lookup.
	p := ref.Path
	if p == "" {
		var err error
		if t.mediaLoader == nil {
			return "", "", fmt.Errorf("no media storage configured")
		}
		p, err = t.mediaLoader.LoadPath(ref.ID)
		if err != nil {
			return "", "", fmt.Errorf("video file not found: %v", err)
		}
	}

	mime = ref.MimeType
	if mime == "" || mime == "application/octet-stream" {
		mime = mimeFromVideoExt(filepath.Ext(p))
	}

	return p, mime, nil
}

// callProvider dispatches video analysis to the appropriate provider API.
// Gemini: uses File API (upload → poll → file_data in generateContent).
// Others: falls back to base64 in image_url (OpenRouter routes to Gemini which handles video).
func (t *ReadVideoTool) callProvider(ctx context.Context, cp credentialProvider, providerName, model string, params map[string]any) ([]byte, *providers.Usage, error) {
	prompt := GetParamString(params, "prompt", "Analyze this video and describe its contents.")
	data, _ := params["data"].([]byte)
	mime := GetParamString(params, "mime", "video/mp4")

	// Gemini: use File API (requires credentials).
	ptype := GetParamString(params, "_provider_type", providerTypeFromName(providerName))
	if cp != nil && ptype == "gemini" {
		slog.Info("read_video: using gemini file API", "provider", providerName, "model", model, "size", len(data), "mime", mime)
		resp, err := geminiFileAPICall(ctx, cp.APIKey(), model, prompt, data, mime, 180*time.Second)
		if err != nil {
			return nil, nil, fmt.Errorf("gemini file API: %w", err)
		}
		return []byte(resp.Content), resp.Usage, nil
	}

	// Other providers: try standard Chat API with base64 as image_url (best effort).
	p, err := t.registry.Get(ctx, providerName)
	if err != nil {
		return nil, nil, fmt.Errorf("provider %q not available: %w", providerName, err)
	}

	slog.Info("read_video: using chat API fallback", "provider", providerName, "model", model, "size", len(data))
	resp, err := p.Chat(ctx, providers.ChatRequest{
		Messages: []providers.Message{
			{
				Role:    "user",
				Content: prompt,
				Images:  []providers.ImageContent{{MimeType: mime, Data: base64.StdEncoding.EncodeToString(data)}},
			},
		},
		Model: model,
		Options: map[string]any{
			"max_tokens":  16384,
			"temperature": 0.2,
		},
	})
	if err != nil {
		return nil, nil, fmt.Errorf("chat API: %w", err)
	}
	return []byte(resp.Content), resp.Usage, nil
}
