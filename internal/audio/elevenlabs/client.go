package elevenlabs

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// client is the shared HTTP client used by the TTS and SFX providers. It
// encapsulates the ElevenLabs `xi-api-key` auth scheme + common error handling.
type client struct {
	apiKey    string
	baseURL   string
	timeoutMs int
}

func newClient(apiKey, baseURL string, timeoutMs int) *client {
	if baseURL == "" {
		baseURL = "https://api.elevenlabs.io"
	}
	if timeoutMs <= 0 {
		timeoutMs = 30000
	}
	return &client{apiKey: apiKey, baseURL: baseURL, timeoutMs: timeoutMs}
}

// postJSON performs a POST with JSON body to {baseURL}/path and returns the
// raw response bytes on 200 OK. Non-200 responses surface as errors with the
// upstream body appended — matches legacy behavior for debuggability.
func (c *client) postJSON(ctx context.Context, path string, body []byte, customTimeout time.Duration) ([]byte, error) {
	url := strings.TrimRight(c.baseURL, "/") + path
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("xi-api-key", c.apiKey)

	timeout := customTimeout
	if timeout <= 0 {
		timeout = time.Duration(c.timeoutMs) * time.Millisecond
	}
	hc := &http.Client{Timeout: timeout}
	resp, err := hc.Do(req)
	if err != nil {
		return nil, fmt.Errorf("http request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		errBody, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("ElevenLabs API error %d: %s", resp.StatusCode, truncate(errBody, 500))
	}
	out, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read response: %w", err)
	}
	return out, nil
}

// getJSON performs a GET request to {baseURL}/path and returns the raw response
// bytes on 200 OK. Non-200 responses surface as errors with a truncated body —
// mirrors the postJSON error handling pattern.
func (c *client) getJSON(ctx context.Context, path string) ([]byte, error) {
	url := strings.TrimRight(c.baseURL, "/") + path
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("xi-api-key", c.apiKey)

	hc := &http.Client{Timeout: time.Duration(c.timeoutMs) * time.Millisecond}
	resp, err := hc.Do(req)
	if err != nil {
		return nil, fmt.Errorf("http request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		errBody, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("ElevenLabs API error %d: %s", resp.StatusCode, truncate(errBody, 500))
	}
	out, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read response: %w", err)
	}
	return out, nil
}

func truncate(b []byte, n int) string {
	if len(b) <= n {
		return string(b)
	}
	return string(b[:n]) + "...(truncated)"
}
