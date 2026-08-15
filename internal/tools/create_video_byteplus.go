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

// bytePlusVideoRunEndpoint derives the Seedance video generation submit endpoint.
// Media endpoints always use the standard /api/v3 path (not /api/coding/v3).
func bytePlusVideoRunEndpoint(apiBase string) string {
	return bytePlusMediaBase(apiBase) + "/text-to-video-pro/run"
}

// bytePlusVideoStatusEndpoint derives the Seedance video generation poll endpoint.
func bytePlusVideoStatusEndpoint(apiBase, taskID string) string {
	return bytePlusMediaBase(apiBase) + "/text-to-video-pro/status/" + taskID
}

// callBytePlusVideoGen calls the BytePlus Seedance video generation API.
// The API is async: POST /text-to-video-pro/run → task id → poll /status/{id}.
func callBytePlusVideoGen(ctx context.Context, apiKey, apiBase, model, prompt string, duration int, aspectRatio string, params map[string]any) ([]byte, *providers.Usage, error) {
	endpoint := bytePlusVideoRunEndpoint(apiBase)

	input := map[string]any{
		"prompt": prompt,
	}
	if aspectRatio != "" {
		input["ratio"] = aspectRatio
	}
	if duration > 0 {
		input["duration"] = fmt.Sprintf("%d", duration)
	}
	resolution := GetParamString(params, "resolution", "480p")
	input["resolution"] = resolution

	body := map[string]any{
		"input": input,
	}

	jsonBody, err := json.Marshal(body)
	if err != nil {
		return nil, nil, fmt.Errorf("marshal request: %w", err)
	}

	slog.Info("create_video: calling BytePlus Seedance API",
		"model", model, "duration", duration, "ratio", aspectRatio, "resolution", resolution)

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

	var initResp struct {
		ID     string `json:"id"`
		Status string `json:"status"`
	}
	if err := json.Unmarshal(respBody, &initResp); err != nil {
		return nil, nil, fmt.Errorf("parse response: %w", err)
	}

	if initResp.ID == "" {
		return nil, nil, fmt.Errorf("no task id in BytePlus video response: %s", truncateBytes(respBody, 300))
	}

	return bytePlusVideoPollTask(ctx, apiKey, apiBase, initResp.ID, client)
}

// bytePlusVideoPollTask polls GET /text-to-video-pro/status/{id} until done.
// Video gen typically takes 45s-3min. Allow up to ~5 minutes (300 polls × 1s).
func bytePlusVideoPollTask(ctx context.Context, apiKey, apiBase, taskID string, client *http.Client) ([]byte, *providers.Usage, error) {
	statusURL := bytePlusVideoStatusEndpoint(apiBase, taskID)
	slog.Info("create_video: BytePlus Seedance task started, polling", "task_id", taskID)

	const maxPolls = 300
	const pollInterval = 1 * time.Second

	for i := range maxPolls {
		select {
		case <-ctx.Done():
			return nil, nil, ctx.Err()
		case <-time.After(pollInterval):
		}

		pollReq, err := http.NewRequestWithContext(ctx, "GET", statusURL, nil)
		if err != nil {
			return nil, nil, fmt.Errorf("create poll request: %w", err)
		}
		pollReq.Header.Set("Authorization", "Bearer "+apiKey)

		pollResp, err := client.Do(pollReq)
		if err != nil {
			slog.Warn("create_video: BytePlus poll error, retrying", "error", err, "attempt", i+1)
			continue
		}

		respBytes, _ := io.ReadAll(pollResp.Body)
		pollResp.Body.Close()

		if pollResp.StatusCode != http.StatusOK {
			return nil, nil, fmt.Errorf("poll API error %d: %s", pollResp.StatusCode, truncateBytes(respBytes, 500))
		}

		var taskResp struct {
			ID     string `json:"id"`
			Status string `json:"status"`
			Output *struct {
				VideoURL string `json:"video_url"`
			} `json:"output"`
		}
		if err := json.Unmarshal(respBytes, &taskResp); err != nil {
			return nil, nil, fmt.Errorf("parse poll response: %w", err)
		}

		switch taskResp.Status {
		case "succeeded":
			if taskResp.Output == nil || taskResp.Output.VideoURL == "" {
				return nil, nil, fmt.Errorf("task succeeded but no video URL")
			}
			return bytePlusDownloadVideo(ctx, taskResp.Output.VideoURL)
		case "failed":
			return nil, nil, fmt.Errorf("BytePlus video task %s failed", taskID)
		default:
			// Log progress every 10 polls to avoid spam
			if (i+1)%10 == 0 {
				slog.Info("create_video: BytePlus task pending", "attempt", i+1, "status", taskResp.Status)
			}
		}
	}

	return nil, nil, fmt.Errorf("BytePlus video task %s timed out after %d polls", taskID, maxPolls)
}

// bytePlusDownloadVideo downloads a video from a URL and returns the raw bytes.
func bytePlusDownloadVideo(ctx context.Context, videoURL string) ([]byte, *providers.Usage, error) {
	req, err := http.NewRequestWithContext(ctx, "GET", videoURL, nil)
	if err != nil {
		return nil, nil, fmt.Errorf("create download request: %w", err)
	}

	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		return nil, nil, fmt.Errorf("download video: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, nil, fmt.Errorf("download error %d: %s", resp.StatusCode, truncateBytes(body, 300))
	}

	videoBytes, err := limitedReadAll(resp.Body, maxMediaDownloadBytes)
	if err != nil {
		return nil, nil, fmt.Errorf("read video data: %w", err)
	}

	return videoBytes, nil, nil
}
