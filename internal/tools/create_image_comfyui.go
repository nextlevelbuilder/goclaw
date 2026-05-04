package tools

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/nextlevelbuilder/goclaw/internal/providers"
)

func comfyUIAspectSize(aspectRatio string) (int, int) {
	size := providers.SizeFromAspect(aspectRatio)
	parts := strings.Split(size, "x")
	if len(parts) != 2 {
		return 1024, 1024
	}
	var w, h int
	_, _ = fmt.Sscanf(size, "%dx%d", &w, &h)
	if w <= 0 || h <= 0 {
		return 1024, 1024
	}
	return w, h
}

func callComfyUIImageGen(ctx context.Context, apiBase, model, prompt string, params map[string]any) ([]byte, *providers.Usage, error) {
	if apiBase == "" {
		apiBase = "http://127.0.0.1:8188"
	}
	aspectRatio := GetParamString(params, "aspect_ratio", "1:1")
	width, height := comfyUIAspectSize(aspectRatio)
	if model == "" {
		model = "sd_xl_turbo_1.0_fp16.safetensors"
	}
	if !strings.HasSuffix(strings.ToLower(model), ".safetensors") {
		model += ".safetensors"
	}

	workflow := map[string]any{
		"1": map[string]any{"class_type": "CheckpointLoaderSimple", "inputs": map[string]any{"ckpt_name": model}},
		"2": map[string]any{"class_type": "CLIPTextEncode", "inputs": map[string]any{"text": prompt, "clip": []any{"1", 1}}},
		"3": map[string]any{"class_type": "CLIPTextEncode", "inputs": map[string]any{"text": "blurry, low quality, distorted, extra limbs", "clip": []any{"1", 1}}},
		"4": map[string]any{"class_type": "EmptyLatentImage", "inputs": map[string]any{"width": width, "height": height, "batch_size": 1}},
		"5": map[string]any{"class_type": "KSampler", "inputs": map[string]any{
			"model":        []any{"1", 0},
			"seed":         time.Now().UnixNano() % 1844674407370955161,
			"steps":        8,
			"cfg":          2,
			"sampler_name": "euler",
			"scheduler":    "simple",
			"denoise":      1.0,
			"positive":     []any{"2", 0},
			"negative":     []any{"3", 0},
			"latent_image": []any{"4", 0},
		}},
		"6": map[string]any{"class_type": "VAEDecode", "inputs": map[string]any{"samples": []any{"5", 0}, "vae": []any{"1", 2}}},
		"7": map[string]any{"class_type": "UnloadAllModels", "inputs": map[string]any{"value": []any{"6", 0}}},
		"8": map[string]any{"class_type": "SaveImage", "inputs": map[string]any{"images": []any{"7", 0}, "filename_prefix": "openclaw_comfyui"}},
	}

	payload := map[string]any{
		"prompt":    workflow,
		"client_id": fmt.Sprintf("openclaw-%d", time.Now().UnixNano()),
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return nil, nil, fmt.Errorf("marshal workflow: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, strings.TrimRight(apiBase, "/")+"/prompt", bytes.NewReader(body))
	if err != nil {
		return nil, nil, fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := (&http.Client{Timeout: 30 * time.Second}).Do(req)
	if err != nil {
		return nil, nil, fmt.Errorf("submit prompt: %w", err)
	}
	defer resp.Body.Close()
	respBody, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		return nil, nil, fmt.Errorf("comfyui prompt error %d: %s", resp.StatusCode, truncateBytes(respBody, 500))
	}
	var submit struct {
		PromptID string `json:"prompt_id"`
	}
	if err := json.Unmarshal(respBody, &submit); err != nil {
		return nil, nil, fmt.Errorf("parse submit response: %w", err)
	}
	if submit.PromptID == "" {
		return nil, nil, fmt.Errorf("comfyui: missing prompt_id")
	}

	deadline := time.NewTimer(5 * time.Minute)
	defer deadline.Stop()
	client := &http.Client{Timeout: 30 * time.Second}
	for {
		select {
		case <-ctx.Done():
			return nil, nil, ctx.Err()
		case <-deadline.C:
			return nil, nil, fmt.Errorf("comfyui: timed out waiting for result")
		case <-time.After(2 * time.Second):
		}

		histReq, err := http.NewRequestWithContext(ctx, http.MethodGet, strings.TrimRight(apiBase, "/")+"/history/"+submit.PromptID, nil)
		if err != nil {
			return nil, nil, fmt.Errorf("create history request: %w", err)
		}
		histResp, err := client.Do(histReq)
		if err != nil {
			continue
		}
		histBody, _ := io.ReadAll(histResp.Body)
		histResp.Body.Close()
		if histResp.StatusCode != http.StatusOK {
			continue
		}
		var history map[string]struct {
			Outputs map[string]struct {
				Images []struct {
					Filename  string `json:"filename"`
					Subfolder string `json:"subfolder"`
					Type      string `json:"type"`
				} `json:"images"`
			} `json:"outputs"`
		}
		if err := json.Unmarshal(histBody, &history); err != nil {
			continue
		}
		entry, ok := history[submit.PromptID]
		if !ok {
			continue
		}
		for _, out := range entry.Outputs {
			for _, img := range out.Images {
				if img.Filename == "" {
					continue
				}
				viewURL := strings.TrimRight(apiBase, "/") + "/view?filename=" + url.QueryEscape(img.Filename) + "&subfolder=" + url.QueryEscape(img.Subfolder) + "&type=" + url.QueryEscape(img.Type)
				viewReq, err := http.NewRequestWithContext(ctx, http.MethodGet, viewURL, nil)
				if err != nil {
					return nil, nil, fmt.Errorf("create view request: %w", err)
				}
				viewResp, err := client.Do(viewReq)
				if err != nil {
					continue
				}
				imgBytes, err := io.ReadAll(viewResp.Body)
				viewResp.Body.Close()
				if err != nil || viewResp.StatusCode != http.StatusOK {
					continue
				}
				return imgBytes, nil, nil
			}
		}
	}
}
