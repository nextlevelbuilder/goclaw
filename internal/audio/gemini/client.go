package gemini

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

const defaultAPIBase = "https://generativelanguage.googleapis.com"

// client handles HTTP communication with the Gemini generateContent API.
type client struct {
	apiKey    string
	apiBase   string
	timeoutMs int
}

func newClient(apiKey, apiBase string, timeoutMs int) *client {
	base := strings.TrimRight(apiBase, "/")
	if base == "" {
		base = defaultAPIBase
	}
	if timeoutMs <= 0 {
		timeoutMs = 30000
	}
	return &client{apiKey: apiKey, apiBase: base, timeoutMs: timeoutMs}
}

// buildURL constructs the generateContent endpoint URL for a given model.
func buildURL(base, model string) string {
	base = strings.TrimRight(base, "/")
	return base + "/v1beta/models/" + model + ":generateContent"
}

// post sends a JSON body to the Gemini API and returns the raw response bytes.
func (c *client) post(ctx context.Context, model string, body []byte) ([]byte, int, error) {
	url := buildURL(c.apiBase, model)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return nil, 0, fmt.Errorf("gemini: create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("x-goog-api-key", c.apiKey)

	hc := &http.Client{Timeout: time.Duration(c.timeoutMs) * time.Millisecond}
	resp, err := hc.Do(req)
	if err != nil {
		return nil, 0, fmt.Errorf("gemini: request failed: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, resp.StatusCode, fmt.Errorf("gemini: read response: %w", err)
	}
	return respBody, resp.StatusCode, nil
}
