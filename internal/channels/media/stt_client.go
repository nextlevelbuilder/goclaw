package media

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"mime/multipart"
	"net/http"
	"os"
	"path/filepath"
	"sync"
	"time"
)

const (
	// DefaultSTTTimeout is the default timeout for STT proxy requests.
	DefaultSTTTimeout = 30

	// sttTranscribeEndpoint is the path appended to STTProxyURL.
	sttTranscribeEndpoint = "/transcribe_audio"
)

var (
	sttClient     *http.Client
	sttClientOnce sync.Once
	sttSem        = make(chan struct{}, 4) // max 4 concurrent STT calls
)

// getSTTClient returns a shared HTTP client with connection pooling for STT requests.
func getSTTClient() *http.Client {
	sttClientOnce.Do(func() {
		sttClient = &http.Client{
			Timeout: 60 * time.Second, // defensive cap in case caller context has no deadline
			Transport: &http.Transport{
				MaxIdleConnsPerHost: 4,
				IdleConnTimeout:     90 * time.Second,
			},
		}
	})
	return sttClient
}

// STTConfig holds configuration for the Speech-to-Text proxy service.
type STTConfig struct {
	ProxyURL       string // base URL of the STT proxy (e.g. "http://localhost:8080")
	APIKey         string // optional Bearer token
	TenantID       string // optional tenant identifier
	TimeoutSeconds int    // request timeout (defaults to DefaultSTTTimeout)
}

// sttResponse is the expected JSON response from the STT proxy.
type sttResponse struct {
	Transcript string `json:"transcript"`
}

// TranscribeAudio calls the configured STT proxy service with the given audio file
// and returns the transcribed text. Returns ("", nil) silently when:
//   - cfg.ProxyURL is empty (STT not configured), or
//   - filePath is empty (download failed earlier in the pipeline).
//
// Any HTTP or parse error is returned so the caller can log and fall back gracefully.
func TranscribeAudio(ctx context.Context, cfg STTConfig, filePath string) (string, error) {
	if cfg.ProxyURL == "" || filePath == "" {
		return "", nil
	}

	timeoutSec := cfg.TimeoutSeconds
	if timeoutSec <= 0 {
		timeoutSec = DefaultSTTTimeout
	}

	f, err := os.Open(filePath)
	if err != nil {
		return "", fmt.Errorf("stt: open audio file %q: %w", filePath, err)
	}
	defer f.Close()

	// Build multipart/form-data body.
	var body bytes.Buffer
	w := multipart.NewWriter(&body)

	fw, err := w.CreateFormFile("file", filepath.Base(filePath))
	if err != nil {
		return "", fmt.Errorf("stt: create form file field: %w", err)
	}
	if _, err := io.Copy(fw, f); err != nil {
		return "", fmt.Errorf("stt: write audio bytes to form: %w", err)
	}

	if cfg.TenantID != "" {
		if err := w.WriteField("tenant_id", cfg.TenantID); err != nil {
			return "", fmt.Errorf("stt: write tenant_id field: %w", err)
		}
	}

	if err := w.Close(); err != nil {
		return "", fmt.Errorf("stt: close multipart writer: %w", err)
	}

	// Build HTTP request with a deadline.
	reqCtx, cancel := context.WithTimeout(ctx, time.Duration(timeoutSec)*time.Second)
	defer cancel()

	url := cfg.ProxyURL + sttTranscribeEndpoint
	req, err := http.NewRequestWithContext(reqCtx, http.MethodPost, url, &body)
	if err != nil {
		return "", fmt.Errorf("stt: build request to %q: %w", url, err)
	}
	req.Header.Set("Content-Type", w.FormDataContentType())
	if cfg.APIKey != "" {
		req.Header.Set("Authorization", "Bearer "+cfg.APIKey)
	}

	slog.Debug("stt: calling proxy", "url", url, "file", filepath.Base(filePath))

	// Acquire concurrency slot (blocks if 4 calls already in-flight).
	select {
	case sttSem <- struct{}{}:
		defer func() { <-sttSem }()
	case <-reqCtx.Done():
		return "", fmt.Errorf("stt: context cancelled waiting for concurrency slot: %w", reqCtx.Err())
	}

	resp, err := getSTTClient().Do(req)
	if err != nil {
		return "", fmt.Errorf("stt: request to %q failed: %w", url, err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20)) // 1 MB cap
	if err != nil {
		return "", fmt.Errorf("stt: read response body: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("stt: upstream returned %d: %s", resp.StatusCode, string(respBody))
	}

	var result sttResponse
	if err := json.Unmarshal(respBody, &result); err != nil {
		return "", fmt.Errorf("stt: parse response JSON: %w", err)
	}

	slog.Debug("stt: transcript received",
		"length", len(result.Transcript),
		"preview", truncatePreview(result.Transcript, 80),
	)

	return result.Transcript, nil
}

// truncatePreview returns s truncated to maxLen with "..." suffix if needed.
func truncatePreview(s string, maxLen int) string {
	if len(s) > maxLen {
		return s[:maxLen] + "..."
	}
	return s
}
