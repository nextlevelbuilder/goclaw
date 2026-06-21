package tools

import (
	"context"
	"encoding/base64"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"path/filepath"
	"strings"
	"time"

	"github.com/nextlevelbuilder/goclaw/internal/providers"
	"github.com/nextlevelbuilder/goclaw/internal/security"
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
// Others: falls back to base64 or URL in video_url (OpenRouter routes to Gemini which handles video).
func (t *ReadVideoTool) callProvider(ctx context.Context, cp credentialProvider, providerName, model string, params map[string]any) ([]byte, *providers.Usage, error) {
	prompt := GetParamString(params, "prompt", "Analyze this video and describe its contents.")
	data, _ := params["data"].([]byte)
	videoURL, _ := params["url"].(string)
	mime := GetParamString(params, "mime", "video/mp4")
	pinnedIP, _ := params[videoURLPinnedIPParam].(net.IP)
	if videoURL != "" && pinnedIP == nil {
		var err error
		if _, pinnedIP, err = security.Validate(videoURL); err != nil {
			return nil, nil, fmt.Errorf("invalid video URL: %w", err)
		}
	}

	// Gemini: use File API (requires credentials).
	ptype := GetParamString(params, "_provider_type", providerTypeFromName(providerName))
	if cp != nil && ptype == "gemini" {
		var resp *providers.ChatResponse
		var err error

		chatReq := providers.ChatRequest{
			Messages: []providers.Message{{Role: "user", Content: prompt}},
			Model:    model,
			Options:  map[string]any{"max_tokens": 16384},
		}
		reservation, reserveErr := reserveToolLLMUsage(ctx, t.usageCaps, t.Name(), providerName, model, chatReq)
		if reserveErr != nil {
			return nil, nil, reserveErr
		}

		if videoURL != "" {
			slog.Info("read_video: streaming URL directly to Gemini File API", "provider", providerName, "model", model, "url", videoURL)

			// Send GET request to fetch the stream.
			reqCtx := security.WithPinnedIP(ctx, pinnedIP)
			req, getErr := http.NewRequestWithContext(reqCtx, "GET", videoURL, nil)
			if getErr != nil {
				if reservation != nil {
					reservation.Reconcile(ctx, nil, getErr)
				}
				return nil, nil, fmt.Errorf("failed to create GET request for video URL: %w", getErr)
			}

			// Use the shared SSRF-safe client so DNS stays pinned during streaming.
			client := security.NewSafeClient(0)
			httpResp, getErr := client.Do(req)
			if getErr != nil {
				if reservation != nil {
					reservation.Reconcile(ctx, nil, getErr)
				}
				return nil, nil, fmt.Errorf("failed to fetch video URL: %w", getErr)
			}
			defer httpResp.Body.Close()

			if httpResp.StatusCode < 200 || httpResp.StatusCode >= 300 {
				statusErr := fmt.Errorf("video URL returned status code %d", httpResp.StatusCode)
				if reservation != nil {
					reservation.Reconcile(ctx, nil, statusErr)
				}
				return nil, nil, statusErr
			}

			// Validate Content-Length
			contentLength := httpResp.ContentLength
			if contentLength <= 0 {
				invalidLenErr := fmt.Errorf("URL does not support static streaming (missing or invalid Content-Length: %d)", contentLength)
				if reservation != nil {
					reservation.Reconcile(ctx, nil, invalidLenErr)
				}
				return nil, nil, invalidLenErr
			}

			// Check limits: maximum 2 GB
			const videoMaxStreamBytes = 2 * 1024 * 1024 * 1024 // 2 GB
			if contentLength > videoMaxStreamBytes {
				limitErr := fmt.Errorf("video stream size (%d bytes) exceeds the maximum limit of 2 GB", contentLength)
				if reservation != nil {
					reservation.Reconcile(ctx, nil, limitErr)
				}
				return nil, nil, limitErr
			}

			// Extract MIME type from Content-Type header if valid video type, otherwise use the inferred/passed one
			contentType := httpResp.Header.Get("Content-Type")
			if contentType != "" && strings.HasPrefix(contentType, "video/") {
				mime = contentType
			}

			resp, err = geminiFileAPICallStream(ctx, cp.APIKey(), model, prompt, httpResp.Body, contentLength, mime, 300*time.Second)
		} else {
			slog.Info("read_video: using gemini file API", "provider", providerName, "model", model, "size", len(data), "mime", mime)
			resp, err = geminiFileAPICall(ctx, cp.APIKey(), model, prompt, data, mime, 180*time.Second)
		}

		if reservation != nil {
			reservation.Reconcile(ctx, resp, err)
		}
		if err != nil {
			return nil, nil, fmt.Errorf("gemini file API: %w", err)
		}
		return []byte(resp.Content), resp.Usage, nil
	}

	// Other providers: try standard Chat API with base64 or URL as video_url (best effort).
	p, err := t.registry.Get(ctx, providerName)
	if err != nil {
		return nil, nil, fmt.Errorf("provider %q not available: %w", providerName, err)
	}

	var vidContent providers.VideoContent
	if videoURL != "" {
		slog.Info("read_video: using chat API with direct video URL", "provider", providerName, "model", model, "url", videoURL)
		vidContent = providers.VideoContent{MimeType: mime, URL: videoURL}
	} else {
		slog.Info("read_video: using chat API fallback with base64", "provider", providerName, "model", model, "size", len(data))
		vidContent = providers.VideoContent{MimeType: mime, Data: base64.StdEncoding.EncodeToString(data)}
	}

	chatReq := providers.ChatRequest{
		Messages: []providers.Message{
			{
				Role:    "user",
				Content: prompt,
				Videos:  []providers.VideoContent{vidContent},
			},
		},
		Model: model,
		Options: map[string]any{
			"max_tokens":  16384,
			"temperature": 0.2,
		},
	}
	reservation, reserveErr := reserveToolLLMUsage(ctx, t.usageCaps, t.Name(), providerName, model, chatReq)
	if reserveErr != nil {
		return nil, nil, reserveErr
	}
	resp, err := p.Chat(ctx, chatReq)
	if reservation != nil {
		reservation.Reconcile(ctx, resp, err)
	}
	if err != nil {
		return nil, nil, fmt.Errorf("chat API: %w", err)
	}
	return []byte(resp.Content), resp.Usage, nil
}
