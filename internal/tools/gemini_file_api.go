package tools

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"time"

	"github.com/nextlevelbuilder/goclaw/internal/providers"
)

const (
	geminiUploadBase = "https://generativelanguage.googleapis.com/upload/v1beta/files"
	geminiFilesBase  = "https://generativelanguage.googleapis.com/v1beta"

	geminiFilePollInterval = 2 * time.Second
	geminiFilePollMax      = 30
)

// geminiFileUpload uploads a file to Gemini File API using resumable upload protocol.
// Returns the file name (e.g. "files/abc123") and file URI for use in generateContent.
func geminiFileUpload(ctx context.Context, apiKey, displayName string, data []byte, mime string) (fileName, fileURI string, err error) {
	// Step 1: Initiate resumable upload.
	initBody, _ := json.Marshal(map[string]any{
		"file": map[string]string{"display_name": displayName},
	})
	initReq, err := http.NewRequestWithContext(ctx, "POST", geminiUploadBase+"?key="+apiKey, bytes.NewReader(initBody))
	if err != nil {
		return "", "", fmt.Errorf("create init request: %w", err)
	}
	initReq.Header.Set("Content-Type", "application/json")
	initReq.Header.Set("X-Goog-Upload-Protocol", "resumable")
	initReq.Header.Set("X-Goog-Upload-Command", "start")
	initReq.Header.Set("X-Goog-Upload-Header-Content-Length", fmt.Sprintf("%d", len(data)))
	initReq.Header.Set("X-Goog-Upload-Header-Content-Type", mime)

	client := &http.Client{Timeout: 60 * time.Second}
	initResp, err := client.Do(initReq)
	if err != nil {
		return "", "", fmt.Errorf("init upload: %w", err)
	}
	defer initResp.Body.Close()
	io.ReadAll(initResp.Body) // drain

	if initResp.StatusCode != 200 {
		return "", "", fmt.Errorf("init upload HTTP %d", initResp.StatusCode)
	}

	uploadURL := initResp.Header.Get("X-Goog-Upload-URL")
	if uploadURL == "" {
		return "", "", fmt.Errorf("no upload URL in response headers")
	}

	// Step 2: Upload file bytes.
	uploadReq, err := http.NewRequestWithContext(ctx, "POST", uploadURL, bytes.NewReader(data))
	if err != nil {
		return "", "", fmt.Errorf("create upload request: %w", err)
	}
	uploadReq.Header.Set("Content-Length", fmt.Sprintf("%d", len(data)))
	uploadReq.Header.Set("X-Goog-Upload-Offset", "0")
	uploadReq.Header.Set("X-Goog-Upload-Command", "upload, finalize")

	uploadClient := &http.Client{Timeout: 120 * time.Second}
	uploadResp, err := uploadClient.Do(uploadReq)
	if err != nil {
		return "", "", fmt.Errorf("upload bytes: %w", err)
	}
	defer uploadResp.Body.Close()

	respBody, err := io.ReadAll(uploadResp.Body)
	if err != nil {
		return "", "", fmt.Errorf("read upload response: %w", err)
	}
	if uploadResp.StatusCode != 200 {
		return "", "", fmt.Errorf("upload HTTP %d: %s", uploadResp.StatusCode, truncateStr(string(respBody), 500))
	}

	var uploadResult struct {
		File struct {
			Name  string `json:"name"`
			URI   string `json:"uri"`
			State string `json:"state"`
		} `json:"file"`
	}
	if err := json.Unmarshal(respBody, &uploadResult); err != nil {
		return "", "", fmt.Errorf("parse upload response: %w", err)
	}

	// Only return URI if file is already ACTIVE; otherwise caller must poll.
	if uploadResult.File.State == "ACTIVE" {
		return uploadResult.File.Name, uploadResult.File.URI, nil
	}
	return uploadResult.File.Name, "", nil
}

// geminiFilePoll polls the Gemini File API until the file reaches ACTIVE state.
// Returns the file URI once active, or error on FAILED/timeout.
func geminiFilePoll(ctx context.Context, apiKey, fileName string) (fileURI string, err error) {
	url := fmt.Sprintf("%s/%s?key=%s", geminiFilesBase, fileName, apiKey)

	for i := range geminiFilePollMax {
		select {
		case <-ctx.Done():
			return "", ctx.Err()
		case <-time.After(geminiFilePollInterval):
		}

		req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
		if err != nil {
			return "", fmt.Errorf("create poll request: %w", err)
		}

		client := &http.Client{Timeout: 10 * time.Second}
		resp, err := client.Do(req)
		if err != nil {
			slog.Warn("gemini file poll error, retrying", "attempt", i, "error", err)
			continue
		}

		body, _ := io.ReadAll(resp.Body)
		resp.Body.Close()

		var status struct {
			State string `json:"state"`
			URI   string `json:"uri"`
		}
		if err := json.Unmarshal(body, &status); err != nil {
			slog.Warn("gemini file poll parse error, retrying", "attempt", i, "error", err)
			continue
		}

		switch status.State {
		case "ACTIVE":
			return status.URI, nil
		case "FAILED":
			return "", fmt.Errorf("file processing failed")
		default:
			slog.Debug("gemini file poll", "state", status.State, "attempt", i)
		}
	}

	return "", fmt.Errorf("file processing timeout after %d polls", geminiFilePollMax)
}

// geminiFileAPICall uploads a file via Gemini File API, polls until ready,
// then calls generateContent with file_data reference.
// Used for audio/video files where inlineData doesn't work.
func geminiFileAPICall(ctx context.Context, apiKey, model, prompt string, data []byte, mime string, httpTimeout time.Duration) (*providers.ChatResponse, error) {
	displayName := fmt.Sprintf("goclaw_%d", time.Now().UnixNano())

	slog.Info("gemini file api: uploading", "size", len(data), "mime", mime)
	fileName, fileURI, err := geminiFileUpload(ctx, apiKey, displayName, data, mime)
	if err != nil {
		return nil, fmt.Errorf("upload: %w", err)
	}
	slog.Info("gemini file api: uploaded", "name", fileName)

	// If file URI not returned directly, poll for it.
	if fileURI == "" {
		slog.Info("gemini file api: polling for active state", "name", fileName)
		fileURI, err = geminiFilePoll(ctx, apiKey, fileName)
		if err != nil {
			return nil, fmt.Errorf("poll: %w", err)
		}
	}
	slog.Info("gemini file api: file active", "uri", fileURI)

	// Call generateContent with file_data reference.
	body := map[string]any{
		"contents": []map[string]any{
			{
				"parts": []map[string]any{
					{"file_data": map[string]any{"mime_type": mime, "file_uri": fileURI}},
					{"text": prompt},
				},
			},
		},
		"generationConfig": map[string]any{
			"maxOutputTokens": 16384,
			"temperature":     0.2,
		},
	}

	bodyJSON, err := json.Marshal(body)
	if err != nil {
		return nil, fmt.Errorf("marshal request: %w", err)
	}

	url := fmt.Sprintf("https://generativelanguage.googleapis.com/v1beta/models/%s:generateContent?key=%s", model, apiKey)
	httpReq, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewReader(bodyJSON))
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")

	if httpTimeout == 0 {
		httpTimeout = 120 * time.Second
	}
	client := &http.Client{Timeout: httpTimeout}
	httpResp, err := client.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("HTTP request: %w", err)
	}
	defer httpResp.Body.Close()

	respBody, err := io.ReadAll(httpResp.Body)
	if err != nil {
		return nil, fmt.Errorf("read response: %w", err)
	}
	if httpResp.StatusCode != 200 {
		return nil, fmt.Errorf("HTTP %d: %s", httpResp.StatusCode, truncateStr(string(respBody), 500))
	}

	// Parse — same response format as geminiNativeDocumentCall.
	return parseGeminiResponse(respBody)
}

// parseGeminiResponse extracts text content and usage from a Gemini generateContent response.
func parseGeminiResponse(respBody []byte) (*providers.ChatResponse, error) {
	var geminiResp struct {
		Candidates []struct {
			Content struct {
				Parts []struct {
					Text string `json:"text"`
				} `json:"parts"`
			} `json:"content"`
		} `json:"candidates"`
		UsageMetadata struct {
			PromptTokenCount     int `json:"promptTokenCount"`
			CandidatesTokenCount int `json:"candidatesTokenCount"`
			TotalTokenCount      int `json:"totalTokenCount"`
		} `json:"usageMetadata"`
	}
	if err := json.Unmarshal(respBody, &geminiResp); err != nil {
		return nil, fmt.Errorf("parse response: %w", err)
	}

	var content string
	if len(geminiResp.Candidates) > 0 {
		for _, part := range geminiResp.Candidates[0].Content.Parts {
			if part.Text != "" {
				if content != "" {
					content += "\n"
				}
				content += part.Text
			}
		}
	}
	if content == "" {
		return nil, fmt.Errorf("empty response from Gemini")
	}

	return &providers.ChatResponse{
		Content:      content,
		FinishReason: "stop",
		Usage: &providers.Usage{
			PromptTokens:     geminiResp.UsageMetadata.PromptTokenCount,
			CompletionTokens: geminiResp.UsageMetadata.CandidatesTokenCount,
			TotalTokens:      geminiResp.UsageMetadata.TotalTokenCount,
		},
	}, nil
}
