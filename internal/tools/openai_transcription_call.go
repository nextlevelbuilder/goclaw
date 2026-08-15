package tools

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"strings"
	"time"

	"github.com/nextlevelbuilder/goclaw/internal/providers"
)

// isTranscriptionModel returns true for OpenAI models that require the
// /v1/audio/transcriptions endpoint instead of /chat/completions.
// Covers whisper and the gpt-4o-(mini-)transcribe family.
func isTranscriptionModel(model string) bool {
	m := strings.ToLower(strings.TrimSpace(model))
	if m == "" {
		return false
	}
	if strings.HasPrefix(m, "whisper") {
		return true
	}
	// gpt-4o-transcribe, gpt-4o-mini-transcribe, and future variants.
	return strings.Contains(m, "transcribe")
}

// extFromMime maps an audio MIME type to a file extension accepted by
// OpenAI's transcription endpoint. Falls back to .mp3 for unknown types.
func extFromMime(mime string) string {
	m := strings.ToLower(mime)
	switch {
	case strings.Contains(m, "wav"):
		return ".wav"
	case strings.Contains(m, "mp3"), strings.Contains(m, "mpeg"):
		return ".mp3"
	case strings.Contains(m, "m4a"), strings.Contains(m, "mp4"):
		return ".m4a"
	case strings.Contains(m, "ogg"), strings.Contains(m, "opus"):
		return ".ogg"
	case strings.Contains(m, "flac"):
		return ".flac"
	case strings.Contains(m, "webm"):
		return ".webm"
	default:
		return ".mp3"
	}
}

// openaiTranscriptionCall sends audio to OpenAI's /v1/audio/transcriptions
// endpoint using multipart/form-data. The endpoint returns only the
// transcribed text and does not provide token usage counters.
func openaiTranscriptionCall(ctx context.Context, apiKey, baseURL, model string, data []byte, mime string) (*providers.ChatResponse, error) {
	var buf bytes.Buffer
	w := multipart.NewWriter(&buf)

	filePart, err := w.CreateFormFile("file", "audio"+extFromMime(mime))
	if err != nil {
		return nil, fmt.Errorf("create form file: %w", err)
	}
	if _, err := filePart.Write(data); err != nil {
		return nil, fmt.Errorf("write audio payload: %w", err)
	}
	if err := w.WriteField("model", model); err != nil {
		return nil, fmt.Errorf("write model field: %w", err)
	}
	if err := w.Close(); err != nil {
		return nil, fmt.Errorf("close multipart: %w", err)
	}

	url := strings.TrimRight(baseURL, "/") + "/audio/transcriptions"
	req, err := http.NewRequestWithContext(ctx, "POST", url, &buf)
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("Content-Type", w.FormDataContentType())
	req.Header.Set("Authorization", "Bearer "+apiKey)

	client := &http.Client{Timeout: 120 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("HTTP request: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read response: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("HTTP %d: %s", resp.StatusCode, truncateStr(string(respBody), 500))
	}

	var out struct {
		Text string `json:"text"`
	}
	if err := json.Unmarshal(respBody, &out); err != nil {
		return nil, fmt.Errorf("parse response: %w", err)
	}
	if out.Text == "" {
		return nil, fmt.Errorf("empty transcription")
	}
	return &providers.ChatResponse{
		Content:      out.Text,
		FinishReason: "stop",
	}, nil
}
