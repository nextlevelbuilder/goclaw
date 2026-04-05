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

// dashScopeImageEndpoint derives the DashScope multimodal generation endpoint from the
// stored api_base. The api_base in DB is typically an OpenAI-compat URL such as
// https://dashscope-intl.aliyuncs.com/compatible-mode/v1
// The real image generation endpoint lives at a different path on the same host.
func dashScopeImageEndpoint(apiBase string) string {
	base := strings.TrimRight(apiBase, "/")

	// Known patterns — strip compat suffix to get the host, then build the real path.
	for _, suffix := range []string{
		"/compatible-mode/v1",
		"/compatible-mode",
		"/openai/v1",
		"/openai",
		"/v1",
	} {
		if before, ok := strings.CutSuffix(base, suffix); ok {
			base = before
			break
		}
	}

	return base + "/api/v1/services/aigc/multimodal-generation/generation"
}

// dashScopeTaskEndpoint returns the task polling URL for a given task_id.
func dashScopeTaskEndpoint(apiBase, taskID string) string {
	base := strings.TrimRight(apiBase, "/")
	for _, suffix := range []string{
		"/compatible-mode/v1",
		"/compatible-mode",
		"/openai/v1",
		"/openai",
		"/v1",
	} {
		if before, ok := strings.CutSuffix(base, suffix); ok {
			base = before
			break
		}
	}
	return base + "/api/v1/tasks/" + taskID
}

// callDashScopeImageGen calls the DashScope (Alibaba/Bailian) multimodal image generation API.
// The API is async: an initial POST returns a task_id, which is then polled until done.
// On completion, output.results[].url contains the image URL to download.
// aspectRatioToDashScopeSize converts aspect_ratio to DashScope size format.
// Falls back to explicit "size" param if set, otherwise uses aspect_ratio mapping.
func aspectRatioToDashScopeSize(params map[string]any) string {
	if s := GetParamString(params, "size", ""); s != "" {
		return s
	}
	ar := GetParamString(params, "aspect_ratio", "1:1")
	switch ar {
	case "16:9":
		return "1280*720"
	case "9:16":
		return "720*1280"
	case "4:3":
		return "1024*768"
	case "3:4":
		return "768*1024"
	default:
		return "1024*1024"
	}
}

func callDashScopeImageGen(ctx context.Context, apiKey, apiBase, model, prompt string, params map[string]any) ([]byte, *providers.Usage, error) {
	size := aspectRatioToDashScopeSize(params)
	promptExtend := GetParamBool(params, "prompt_extend", true)

	endpoint := dashScopeImageEndpoint(apiBase)

	body := map[string]any{
		"model": model,
		"input": map[string]any{
			"messages": []map[string]any{
				{"role": "user", "content": prompt},
			},
		},
		"parameters": map[string]any{
			"n":             1,
			"size":          size,
			"prompt_extend": promptExtend,
		},
	}

	jsonBody, err := json.Marshal(body)
	if err != nil {
		return nil, nil, fmt.Errorf("marshal request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, "POST", endpoint, bytes.NewReader(jsonBody))
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

	// Parse initial response — may be synchronous (results present) or async (task_id present).
	var initResp struct {
		Output *struct {
			TaskID  string `json:"task_id"`
			Results []struct {
				URL string `json:"url"`
			} `json:"results"`
		} `json:"output"`
	}
	if err := json.Unmarshal(respBody, &initResp); err != nil {
		return nil, nil, fmt.Errorf("parse response: %w", err)
	}

	if initResp.Output == nil {
		return nil, nil, fmt.Errorf("no output in DashScope response: %s", truncateBytes(respBody, 300))
	}

	// Synchronous result already available
	if len(initResp.Output.Results) > 0 && initResp.Output.Results[0].URL != "" {
		return downloadImageURL(ctx, initResp.Output.Results[0].URL)
	}

	// Async: poll the task until done
	if initResp.Output.TaskID == "" {
		return nil, nil, fmt.Errorf("no task_id and no results in DashScope response")
	}

	return dashScopePollTask(ctx, apiKey, apiBase, initResp.Output.TaskID, client)
}

// dashScopePollTask polls the DashScope task API until the task completes, then downloads
// the result image. Max wait ~5 minutes (30 polls × 10s).
func dashScopePollTask(ctx context.Context, apiKey, apiBase, taskID string, client *http.Client) ([]byte, *providers.Usage, error) {
	pollURL := dashScopeTaskEndpoint(apiBase, taskID)
	slog.Info("create_image: DashScope task started, polling", "task_id", taskID)

	const maxPolls = 30
	const pollInterval = 10 * time.Second

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
			slog.Warn("create_image: DashScope poll error, retrying", "error", err, "attempt", i+1)
			continue
		}

		pollBody, _ := io.ReadAll(pollResp.Body)
		pollResp.Body.Close()

		if pollResp.StatusCode != http.StatusOK {
			return nil, nil, fmt.Errorf("poll API error %d: %s", pollResp.StatusCode, truncateBytes(pollBody, 500))
		}

		var taskResp struct {
			Output *struct {
				TaskStatus string `json:"task_status"`
				Results    []struct {
					URL string `json:"url"`
				} `json:"results"`
			} `json:"output"`
		}
		if err := json.Unmarshal(pollBody, &taskResp); err != nil {
			return nil, nil, fmt.Errorf("parse poll response: %w", err)
		}

		if taskResp.Output == nil {
			continue
		}

		switch taskResp.Output.TaskStatus {
		case "SUCCEEDED":
			if len(taskResp.Output.Results) == 0 || taskResp.Output.Results[0].URL == "" {
				return nil, nil, fmt.Errorf("task succeeded but no image URL in results")
			}
			return downloadImageURL(ctx, taskResp.Output.Results[0].URL)
		case "FAILED":
			return nil, nil, fmt.Errorf("DashScope task %s failed", taskID)
		default:
			slog.Info("create_image: DashScope task pending", "attempt", i+1, "status", taskResp.Output.TaskStatus)
		}
	}

	return nil, nil, fmt.Errorf("DashScope task %s timed out after %d polls", taskID, maxPolls)
}

// downloadImageURL downloads an image from a URL and returns the raw bytes.
func downloadImageURL(ctx context.Context, imageURL string) ([]byte, *providers.Usage, error) {
	req, err := http.NewRequestWithContext(ctx, "GET", imageURL, nil)
	if err != nil {
		return nil, nil, fmt.Errorf("create download request: %w", err)
	}

	client := &http.Client{} // timeout governed by chain context
	resp, err := client.Do(req)
	if err != nil {
		return nil, nil, fmt.Errorf("download image: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, nil, fmt.Errorf("download error %d: %s", resp.StatusCode, truncateBytes(body, 300))
	}

	imageBytes, err := limitedReadAll(resp.Body, maxMediaDownloadBytes)
	if err != nil {
		return nil, nil, fmt.Errorf("read image data: %w", err)
	}

	return imageBytes, nil, nil
}
