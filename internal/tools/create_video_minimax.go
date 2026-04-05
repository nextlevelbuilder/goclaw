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
	"time"

	"github.com/nextlevelbuilder/goclaw/internal/providers"
)

// callMinimaxVideoGen calls the MiniMax video generation API (async with task polling).
// Image-to-video is not supported by MiniMax — image data is ignored.
// Flow: POST /video_generation → poll /query/video_generation → download from file retrieve.
func callMinimaxVideoGen(ctx context.Context, apiKey, apiBase, model string, params map[string]any) ([]byte, *providers.Usage, error) {
	if GetParamString(params, "image_base64", "") != "" {
		slog.Warn("create_video: image-to-video not supported by MiniMax, falling back to text-to-video")
	}
	prompt := GetParamString(params, "prompt", "")
	duration := GetParamInt(params, "duration", 6)
	resolution := GetParamString(params, "resolution", "720P")
	promptOptimizer := GetParamBool(params, "prompt_optimizer", true)
	fastPretreatment := GetParamBool(params, "fast_pretreatment", false)

	base := strings.TrimRight(apiBase, "/")

	// 1. Submit video generation task.
	submitBody := map[string]any{
		"model":            model,
		"prompt":           prompt,
		"duration":         duration,
		"resolution":       resolution,
		"prompt_optimizer": promptOptimizer,
	}
	if fastPretreatment {
		submitBody["fast_pretreatment"] = true
	}

	jsonBody, err := json.Marshal(submitBody)
	if err != nil {
		return nil, nil, fmt.Errorf("marshal request: %w", err)
	}

	submitURL := base + "/video_generation"
	req, err := http.NewRequestWithContext(ctx, "POST", submitURL, bytes.NewReader(jsonBody))
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

	var submitResp struct {
		TaskID   string `json:"task_id"`
		BaseResp *struct {
			StatusCode int    `json:"status_code"`
			StatusMsg  string `json:"status_msg"`
		} `json:"base_resp"`
	}
	if err := json.Unmarshal(respBody, &submitResp); err != nil {
		return nil, nil, fmt.Errorf("parse submit response: %w", err)
	}
	if submitResp.BaseResp != nil && submitResp.BaseResp.StatusCode != 0 {
		return nil, nil, fmt.Errorf("MiniMax API error %d: %s",
			submitResp.BaseResp.StatusCode, submitResp.BaseResp.StatusMsg)
	}
	if submitResp.TaskID == "" {
		return nil, nil, fmt.Errorf("no task_id in MiniMax response: %s", truncateBytes(respBody, 300))
	}

	slog.Info("create_video: MiniMax task submitted", "task_id", submitResp.TaskID)

	// 2. Poll until done (max ~6 minutes, poll every 10s).
	pollURL := base + "/query/video_generation?task_id=" + submitResp.TaskID
	const maxPolls = 40
	const pollInterval = 10 * time.Second

	var fileID string
	for i := range maxPolls {
		select {
		case <-ctx.Done():
			return nil, nil, ctx.Err()
		case <-time.After(pollInterval):
		}

		pollReq, err := http.NewRequestWithContext(ctx, "GET", pollURL, nil)
		if err != nil {
			return nil, nil, fmt.Errorf("create poll request: %w", err)
		}
		pollReq.Header.Set("Authorization", "Bearer "+apiKey)

		pollResp, err := client.Do(pollReq)
		if err != nil {
			slog.Warn("create_video: MiniMax poll error, retrying", "error", err, "attempt", i+1)
			continue
		}

		pollBody, _ := io.ReadAll(pollResp.Body)
		pollResp.Body.Close()

		if pollResp.StatusCode != http.StatusOK {
			return nil, nil, fmt.Errorf("poll API error %d: %s", pollResp.StatusCode, truncateBytes(pollBody, 500))
		}

		var pollResult struct {
			Status   string `json:"status"`
			FileID   string `json:"file_id"`
			BaseResp *struct {
				StatusCode int    `json:"status_code"`
				StatusMsg  string `json:"status_msg"`
			} `json:"base_resp"`
		}
		if err := json.Unmarshal(pollBody, &pollResult); err != nil {
			return nil, nil, fmt.Errorf("parse poll response: %w", err)
		}

		if pollResult.BaseResp != nil && pollResult.BaseResp.StatusCode != 0 {
			return nil, nil, fmt.Errorf("MiniMax poll error %d: %s",
				pollResult.BaseResp.StatusCode, pollResult.BaseResp.StatusMsg)
		}

		slog.Info("create_video: MiniMax polling", "attempt", i+1, "status", pollResult.Status)

		switch pollResult.Status {
		case "Success":
			fileID = pollResult.FileID
		case "Failed":
			return nil, nil, fmt.Errorf("MiniMax video generation failed")
		}

		if fileID != "" {
			break
		}
	}

	if fileID == "" {
		return nil, nil, fmt.Errorf("MiniMax video generation timed out after %d polls", maxPolls)
	}

	// 3. Retrieve download URL.
	retrieveURL := base + "/files/retrieve?file_id=" + fileID
	retrieveReq, err := http.NewRequestWithContext(ctx, "GET", retrieveURL, nil)
	if err != nil {
		return nil, nil, fmt.Errorf("create retrieve request: %w", err)
	}
	retrieveReq.Header.Set("Authorization", "Bearer "+apiKey)

	retrieveResp, err := client.Do(retrieveReq)
	if err != nil {
		return nil, nil, fmt.Errorf("retrieve file: %w", err)
	}
	defer retrieveResp.Body.Close()

	retrieveBody, err := io.ReadAll(retrieveResp.Body)
	if err != nil {
		return nil, nil, fmt.Errorf("read retrieve response: %w", err)
	}
	if retrieveResp.StatusCode != http.StatusOK {
		return nil, nil, fmt.Errorf("retrieve API error %d: %s", retrieveResp.StatusCode, truncateBytes(retrieveBody, 500))
	}

	var fileResp struct {
		File *struct {
			DownloadURL string `json:"download_url"`
		} `json:"file"`
	}
	if err := json.Unmarshal(retrieveBody, &fileResp); err != nil {
		return nil, nil, fmt.Errorf("parse retrieve response: %w", err)
	}
	if fileResp.File == nil || fileResp.File.DownloadURL == "" {
		return nil, nil, fmt.Errorf("no download_url in MiniMax file response: %s", truncateBytes(retrieveBody, 300))
	}

	downloadURL := fileResp.File.DownloadURL
	slog.Info("create_video: MiniMax downloading video", "url", downloadURL)

	// 4. Download the video.
	dlReq, err := http.NewRequestWithContext(ctx, "GET", downloadURL, nil)
	if err != nil {
		return nil, nil, fmt.Errorf("create download request: %w", err)
	}

	dlClient := &http.Client{} // timeout governed by chain context
	dlResp, err := dlClient.Do(dlReq)
	if err != nil {
		return nil, nil, fmt.Errorf("download video: %w", err)
	}
	defer dlResp.Body.Close()

	if dlResp.StatusCode != http.StatusOK {
		dlBody, _ := io.ReadAll(dlResp.Body)
		return nil, nil, fmt.Errorf("download error %d: %s", dlResp.StatusCode, truncateBytes(dlBody, 300))
	}

	videoBytes, err := limitedReadAll(dlResp.Body, maxMediaDownloadBytes)
	if err != nil {
		return nil, nil, fmt.Errorf("read video data: %w", err)
	}

	return videoBytes, nil, nil
}
