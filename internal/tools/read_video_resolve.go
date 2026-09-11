package tools

import (
	"context"
	"encoding/base64"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/nextlevelbuilder/goclaw/internal/mediabudget"
	"github.com/nextlevelbuilder/goclaw/internal/providers"
	"github.com/nextlevelbuilder/goclaw/internal/security"
	usagecaps "github.com/nextlevelbuilder/goclaw/internal/usage/caps"
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
	loadedLegacy := false
	if p == "" {
		var err error
		if t.mediaLoader == nil {
			return "", "", fmt.Errorf("no media storage configured")
		}
		p, err = t.mediaLoader.LoadPath(ref.ID)
		if err != nil {
			return "", "", fmt.Errorf("video file not found: %v", err)
		}
		loadedLegacy = true
	}

	if loadedLegacy {
		p, err = resolveLoadedMediaRefPath(ctx, t.mediaLoader, p, "video")
	} else {
		p, err = resolveStructuredMediaRefPath(ctx, p, "video")
	}
	if err != nil {
		return "", "", err
	}

	mime = ref.MimeType
	if mime == "" || mime == "application/octet-stream" {
		mime = mimeFromVideoExt(filepath.Ext(p))
	}

	return p, mime, nil
}

// probeVideoURL learns a remote video's size, and where possible a head and
// tail sample, from two small ranged reads instead of buffering the whole
// file. Head and tail are requested independently so a server that fails one
// range can still answer the other. A prefix alone is not enough: it
// under-reports duration by 98% for mpeg and 79% for ogg, because the index
// several formats need (moov, the ogg/opus final granule, the AVI index)
// sits at the end of the file, not the start.
func probeVideoURL(ctx context.Context, pinnedIP net.IP, rawURL, mime string) (mediabudget.Payload, bool) {
	head, headTotal, _ := probeVideoRange(ctx, pinnedIP, rawURL,
		fmt.Sprintf("bytes=0-%d", mediabudget.HeadBytes-1), mediabudget.HeadBytes)
	tail, tailTotal, tailIsHead := probeVideoRange(ctx, pinnedIP, rawURL,
		fmt.Sprintf("bytes=-%d", mediabudget.TailBytes), mediabudget.TailBytes)
	if tailIsHead {
		// A 200 to a suffix range is the start of the file, not its end;
		// written at the tail offset it can only mislead ffprobe.
		tail = nil
	}

	total := headTotal
	if tailTotal > total {
		total = tailTotal
	}
	if total <= 0 {
		// Both ranged reads came back empty-handed, which is what an origin
		// that rejects Range outright looks like. A plain HEAD still reports
		// the size on most of them; that is not a duration, so such a payload
		// is refused rather than charged, but reporting it as known keeps the
		// caller from taking a second guess at the same unknowable number.
		if size, ok := headVideoURLSize(ctx, pinnedIP, rawURL); ok {
			return mediabudget.Payload{Kind: mediabudget.KindVideo, MIME: mime, Size: size}, true
		}
		return mediabudget.Payload{}, false
	}
	// Accepted risk: a crafted container can make ffprobe under-report duration; Reservation.Reconcile corrects it afterward.
	return mediabudget.Payload{Kind: mediabudget.KindVideo, MIME: mime, Size: total, Head: head, Tail: tail}, true
}

// probeRangeTimeout bounds a probe read (at most one window); the streaming GET below has no deadline since it legitimately transfers a large body.
const probeRangeTimeout = 10 * time.Second

// probeVideoRange issues one ranged GET and returns whatever bytes and total
// size it could learn. It goes through the same pinned-IP context and
// SSRF-safe client as the streaming GET below: this is a second network trip
// to a caller-supplied URL, and it must not open an unpinned path to it.
//
// Any failure, at any stage, is reported as total == 0 rather than an error: a
// probe is best-effort, and its caller has a HEAD fallback behind it, then
// refuses. isHead marks a body that is the start of the file whatever range
// was asked for, so a caller cannot mistake it for the end.
func probeVideoRange(ctx context.Context, pinnedIP net.IP, rawURL, rangeHeader string, capBytes int64) (data []byte, total int64, isHead bool) {
	reqCtx, cancel := context.WithTimeout(security.WithPinnedIP(ctx, pinnedIP), probeRangeTimeout)
	defer cancel()

	req, err := http.NewRequestWithContext(reqCtx, "GET", rawURL, nil)
	if err != nil {
		return nil, 0, false
	}
	req.Header.Set("Range", rangeHeader)

	resp, err := security.NewSafeClient(0).Do(req)
	if err != nil {
		return nil, 0, false
	}
	defer resp.Body.Close()

	switch resp.StatusCode {
	case http.StatusPartialContent:
		total, _ = parseContentRangeTotal(resp.Header.Get("Content-Range"))
		body, _ := io.ReadAll(io.LimitReader(resp.Body, capBytes))
		return body, total, false
	case http.StatusOK:
		// No range support: cap the read to the head window and close
		// immediately, so a server that ignores Range never streams its
		// whole file into a probe.
		body, _ := io.ReadAll(io.LimitReader(resp.Body, mediabudget.HeadBytes))
		return body, resp.ContentLength, true
	default:
		return nil, 0, false
	}
}

// headVideoURLSize asks for the size alone, for an origin that refuses ranged
// reads but still answers a HEAD. It uses the same pinned-IP, SSRF-safe client
// as every other trip to this URL.
func headVideoURLSize(ctx context.Context, pinnedIP net.IP, rawURL string) (int64, bool) {
	reqCtx, cancel := context.WithTimeout(security.WithPinnedIP(ctx, pinnedIP), probeRangeTimeout)
	defer cancel()

	req, err := http.NewRequestWithContext(reqCtx, http.MethodHead, rawURL, nil)
	if err != nil {
		return 0, false
	}

	resp, err := security.NewSafeClient(0).Do(req)
	if err != nil {
		return 0, false
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 || resp.ContentLength <= 0 {
		return 0, false
	}
	return resp.ContentLength, true
}

// parseContentRangeTotal extracts total from a "bytes a-b/total" Content-Range
// header. An unparsable header or an unknown total ("bytes a-b/*") both
// report as an error, which the caller treats as size-unknown.
func parseContentRangeTotal(headerValue string) (int64, error) {
	const prefix = "bytes "
	if !strings.HasPrefix(headerValue, prefix) {
		return 0, fmt.Errorf("range probe: missing Content-Range")
	}
	idx := strings.IndexByte(headerValue, '/')
	if idx < 0 {
		return 0, fmt.Errorf("range probe: malformed Content-Range %q", headerValue)
	}
	totalStr := headerValue[idx+1:]
	if totalStr == "*" {
		return 0, fmt.Errorf("range probe: unknown total in Content-Range %q", headerValue)
	}
	return strconv.ParseInt(totalStr, 10, 64)
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
		var reservation *usagecaps.Reservation
		var reserveErr error
		if videoURL != "" {
			// An origin that answers neither a ranged read nor a HEAD leaves
			// nothing to measure and nothing a later gate could measure
			// either, so refuse before issuing a GET to a caller-supplied URL.
			payload, ok := probeVideoURL(ctx, pinnedIP, videoURL, mime)
			if !ok {
				return nil, nil, refuseUnpriceableMedia(mediabudget.KindVideo, mediabudget.ErrNoProbeableBytes)
			}
			reservation, reserveErr = reserveToolLLMUsageWithMedia(ctx, t.usageCaps, t.Name(), providerName, model, chatReq, payload)
		} else {
			localPath, _ := params[videoLocalPathParam].(string)
			reservation, reserveErr = reserveToolLLMUsageWithMedia(ctx, t.usageCaps, t.Name(), providerName, model, chatReq, mediabudget.Payload{Kind: mediabudget.KindVideo, MIME: mime, Size: int64(len(data)), Path: localPath})
		}
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

			// No re-gate here: the front gate refused anything it could not
			// measure, and Content-Length says nothing about a duration that
			// could revise the charge already taken.
			resp, err = t.streamToGemini(ctx, cp.APIKey(), model, prompt, httpResp.Body, contentLength, mime, 300*time.Second)
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

	// The payload is charged here too. This branch sends it base64-inlined or
	// by URL, either way out of sight of the token counter, so without its own
	// media charge it would be the cheap way around every gate above.
	var vidContent providers.VideoContent
	var payload mediabudget.Payload
	if videoURL != "" {
		slog.Info("read_video: using chat API with direct video URL", "provider", providerName, "model", model, "url", videoURL)
		vidContent = providers.VideoContent{MimeType: mime, URL: videoURL}
		probed, ok := probeVideoURL(ctx, pinnedIP, videoURL, mime)
		if !ok {
			return nil, nil, refuseUnpriceableMedia(mediabudget.KindVideo, mediabudget.ErrNoProbeableBytes)
		}
		payload = probed
	} else {
		slog.Info("read_video: using chat API fallback with base64", "provider", providerName, "model", model, "size", len(data))
		vidContent = providers.VideoContent{MimeType: mime, Data: base64.StdEncoding.EncodeToString(data)}
		localPath, _ := params[videoLocalPathParam].(string)
		payload = mediabudget.Payload{Kind: mediabudget.KindVideo, MIME: mime, Size: int64(len(data)), Path: localPath}
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
	reservation, reserveErr := reserveToolLLMUsageWithMedia(ctx, t.usageCaps, t.Name(), providerName, model, chatReq, payload)
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
