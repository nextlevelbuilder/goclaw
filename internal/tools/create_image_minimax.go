package tools

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/nextlevelbuilder/goclaw/internal/providers"
)

// minimaxImageAspectRatio returns the aspect_ratio string for MiniMax image_generation.
// See: https://platform.minimax.io/docs/guides/image-generation
// Legacy chain settings may still pass "size" as "WIDTH*HEIGHT"; map those to ratios.
func minimaxImageAspectRatio(params map[string]any) string {
	if s := GetParamString(params, "size", ""); s != "" {
		switch strings.ReplaceAll(s, " ", "") {
		case "1280*720":
			return "16:9"
		case "720*1280":
			return "9:16"
		case "1024*768":
			return "4:3"
		case "768*1024":
			return "3:4"
		case "1024*1024":
			return "1:1"
		}
	}
	ar := GetParamString(params, "aspect_ratio", "")
	switch ar {
	case "1:1", "3:4", "4:3", "9:16", "16:9":
		return ar
	case "":
		return "1:1"
	default:
		return "1:1"
	}
}

// callMinimaxImageGen calls the MiniMax image generation API.
// Endpoint: POST {apiBase}/image_generation
// Response: base64 strings in data.image_base64 (per official guide).
func callMinimaxImageGen(ctx context.Context, apiKey, apiBase, model, prompt string, params map[string]any) ([]byte, *providers.Usage, error) {
	aspectRatio := minimaxImageAspectRatio(params)

	body := map[string]any{
		"model":           model,
		"prompt":          prompt,
		"aspect_ratio":    aspectRatio,
		"response_format": "base64",
	}

	jsonBody, err := json.Marshal(body)
	if err != nil {
		return nil, nil, fmt.Errorf("marshal request: %w", err)
	}

	url := strings.TrimRight(apiBase, "/") + "/image_generation"
	req, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewReader(jsonBody))
	if err != nil {
		return nil, nil, fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+apiKey)

	client := &http.Client{} // timeout governed by chain context
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

	var minimaxResp struct {
		Data *struct {
			ImageBase64 []string `json:"image_base64"`
			ImageList   []struct {
				Base64Image string `json:"base64_image"`
			} `json:"image_list"`
		} `json:"data"`
		BaseResp *struct {
			StatusCode int    `json:"status_code"`
			StatusMsg  string `json:"status_msg"`
		} `json:"base_resp"`
	}
	if err := json.Unmarshal(respBody, &minimaxResp); err != nil {
		return nil, nil, fmt.Errorf("parse response: %w", err)
	}

	if minimaxResp.BaseResp != nil && minimaxResp.BaseResp.StatusCode != 0 {
		return nil, nil, fmt.Errorf("MiniMax API error %d: %s",
			minimaxResp.BaseResp.StatusCode, minimaxResp.BaseResp.StatusMsg)
	}

	if minimaxResp.Data == nil {
		return nil, nil, fmt.Errorf("no image data in MiniMax response")
	}

	var b64 string
	if len(minimaxResp.Data.ImageBase64) > 0 {
		b64 = minimaxResp.Data.ImageBase64[0]
	} else if len(minimaxResp.Data.ImageList) > 0 {
		b64 = minimaxResp.Data.ImageList[0].Base64Image
	}
	if b64 == "" {
		return nil, nil, fmt.Errorf("no image data in MiniMax response")
	}

	imageBytes, err := base64.StdEncoding.DecodeString(b64)
	if err != nil {
		return nil, nil, fmt.Errorf("decode base64: %w", err)
	}

	return imageBytes, nil, nil
}
