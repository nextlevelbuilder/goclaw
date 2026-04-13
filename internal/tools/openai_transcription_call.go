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

// isTranscriptionModel returns true for OpenAI models that use the dedicated
// /v1/audio/transcriptions endpoint rather than /chat/completions.
func isTranscriptionModel(model string) bool {
	if model == "whisper-1" {
		return true
	}
	// gpt-4o-transcribe, gpt-4o-mini-transcribe, gpt-4o-transcribe-diarize, etc.
	if strings.HasSuffix(model, "-transcribe") || strings.Contains(model, "-transcribe-") {
		return true
	}
	return false
}

// extFromMime maps a MIME type to a file extension for the multipart filename.
func extFromMime(mime string) string {
	switch {
	case strings.Contains(mime, "wav"):
		return ".wav"
	case strings.Contains(mime, "mp4"), strings.Contains(mime, "m4a"):
		return ".m4a"
	case strings.Contains(mime, "ogg"), strings.Contains(mime, "opus"):
		return ".ogg"
	case strings.Contains(mime, "flac"):
		return ".flac"
	case strings.Contains(mime, "webm"):
		return ".webm"
	default:
		return ".mp3"
	}
}

// openaiTranscriptionCall sends audio to OpenAI's /v1/audio/transcriptions endpoint
// using multipart/form-data. Used for dedicated transcription models like
// gpt-4o-transcribe, gpt-4o-mini-transcribe, and whisper-1.
func openaiTranscriptionCall(ctx context.Context, apiKey, baseURL, model, prompt string, data []byte, mime string) (*providers.ChatResponse, error) {
	var buf bytes.Buffer
	w := multipart.NewWriter(&buf)

	// File field.
	filename := "audio" + extFromMime(mime)
	fw, err := w.CreateFormFile("file", filename)
	if err != nil {
		return nil, fmt.Errorf("create form file: %w", err)
	}
	if _, err := fw.Write(data); err != nil {
		return nil, fmt.Errorf("write audio data: %w", err)
	}

	// Model field.
	if err := w.WriteField("model", model); err != nil {
		return nil, fmt.Errorf("write model field: %w", err)
	}

	// Request JSON response for parseable output.
	if err := w.WriteField("response_format", "json"); err != nil {
		return nil, fmt.Errorf("write response_format field: %w", err)
	}

	// Prompt field (optional transcription guidance).
	if prompt != "" {
		if err := w.WriteField("prompt", prompt); err != nil {
			return nil, fmt.Errorf("write prompt field: %w", err)
		}
	}

	if err := w.Close(); err != nil {
		return nil, fmt.Errorf("close multipart writer: %w", err)
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

	var tr struct {
		Text  string `json:"text"`
		Usage *struct {
			PromptTokens     int `json:"prompt_tokens"`
			CompletionTokens int `json:"completion_tokens"`
			TotalTokens      int `json:"total_tokens"`
		} `json:"usage"`
	}
	if err := json.Unmarshal(respBody, &tr); err != nil {
		return nil, fmt.Errorf("parse response: %w", err)
	}
	if tr.Text == "" {
		return nil, fmt.Errorf("empty transcription response")
	}

	chatResp := &providers.ChatResponse{
		Content:      tr.Text,
		FinishReason: "stop",
	}
	if tr.Usage != nil {
		chatResp.Usage = &providers.Usage{
			PromptTokens:     tr.Usage.PromptTokens,
			CompletionTokens: tr.Usage.CompletionTokens,
			TotalTokens:      tr.Usage.TotalTokens,
		}
	}
	return chatResp, nil
}
