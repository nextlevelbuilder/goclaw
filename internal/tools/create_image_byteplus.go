package tools

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"

	"github.com/nextlevelbuilder/goclaw/internal/providers"
)

// bytePlusImageEndpoint derives the Seedream image generation endpoint from the stored api_base.
// Media endpoints always use the standard /api/v3 path (not /api/coding/v3).
func bytePlusImageEndpoint(apiBase string) string {
	return bytePlusMediaBase(apiBase) + "/images/generations"
}

// bytePlusMediaBase extracts the host and returns the standard /api/v3 media base.
// Strips /chat/completions, /coding/v3, etc. to always use /api/v3 for media endpoints.
func bytePlusMediaBase(apiBase string) string {
	base := strings.TrimRight(apiBase, "/")
	base = strings.TrimSuffix(base, "/chat/completions")
	// Strip any versioned path to rebuild consistently
	for _, suffix := range []string{"/api/coding/v3", "/api/v3", "/v3"} {
		if before, ok := strings.CutSuffix(base, suffix); ok {
			return before + "/api/v3"
		}
	}
	return base + "/api/v3"
}

// callBytePlusImageGen calls the BytePlus Seedream image generation API.
// Seedream returns results synchronously (no async polling needed).
// Endpoint: POST /api/v3/images/generations
func callBytePlusImageGen(ctx context.Context, apiKey, apiBase, model, prompt string, params map[string]any) ([]byte, *providers.Usage, error) {
	size := aspectRatioToBytePlusSize(params)
	endpoint := bytePlusImageEndpoint(apiBase)

	body := map[string]any{
		"model":           model,
		"prompt":          prompt,
		"size":            size,
		"response_format": "url",
	}

	jsonBody, err := json.Marshal(body)
	if err != nil {
		return nil, nil, fmt.Errorf("marshal request: %w", err)
	}

	slog.Info("create_image: calling BytePlus Seedream API", "model", model, "size", size)

	req, err := http.NewRequestWithContext(ctx, "POST", endpoint, bytes.NewReader(jsonBody))
	if err != nil {
		return nil, nil, fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+apiKey)

	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		return nil, nil, fmt.Errorf("http request: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, nil, fmt.Errorf("read response: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		return nil, nil, fmt.Errorf("API error %d: %s", resp.StatusCode, truncateBytes(respBody, 500))
	}

	var result struct {
		Data []struct {
			URL string `json:"url"`
		} `json:"data"`
	}
	if err := json.Unmarshal(respBody, &result); err != nil {
		return nil, nil, fmt.Errorf("parse response: %w", err)
	}

	if len(result.Data) == 0 || result.Data[0].URL == "" {
		return nil, nil, fmt.Errorf("no image URL in BytePlus response: %s", truncateBytes(respBody, 300))
	}

	return downloadImageURL(ctx, result.Data[0].URL)
}

// aspectRatioToBytePlusSize converts aspect_ratio to BytePlus size format.
// Seedream supports "1k", "2K", "4K" or "WIDTHxHEIGHT".
func aspectRatioToBytePlusSize(params map[string]any) string {
	if s := GetParamString(params, "size", ""); s != "" {
		return s
	}
	ar := GetParamString(params, "aspect_ratio", "1:1")
	switch ar {
	case "16:9":
		return "1280x720"
	case "9:16":
		return "720x1280"
	case "4:3":
		return "1024x768"
	case "3:4":
		return "768x1024"
	default:
		return "1024x1024"
	}
}
