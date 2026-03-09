package tools

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/nextlevelbuilder/goclaw/internal/providers"
)

// callMinimaxMusicGen calls the MiniMax music generation API.
// Endpoint: POST {apiBase}/music_generation
// Response: data.audio or data.music (URL to audio file, downloaded and returned as bytes).
func callMinimaxMusicGen(ctx context.Context, apiKey, apiBase, model, prompt string, params map[string]any) ([]byte, *providers.Usage, error) {
	lyrics := GetParamString(params, "lyrics", "")
	instrumental := GetParamBool(params, "instrumental", false)
	lyricsOptimizer := GetParamBool(params, "lyrics_optimizer", false)
	sampleRate := GetParamInt(params, "sample_rate", 44100)
	bitrate := GetParamInt(params, "bitrate", 256000)

	// lyrics is required when is_instrumental is false
	if !instrumental && lyrics == "" {
		instrumental = true
	}

	// NOTE: MiniMax music API does NOT support a duration parameter.
	// Audio length is determined by lyrics length (vocal) or the model's default (~3-4 min for instrumental).

	body := map[string]interface{}{
		"model":            model,
		"prompt":           prompt,
		"is_instrumental":  instrumental,
		"lyrics_optimizer": lyricsOptimizer,
		"output_format":    "url",
		"audio_setting": map[string]interface{}{
			"sample_rate": sampleRate,
			"bitrate":     bitrate,
			"format":      "mp3",
		},
	}
	if lyrics != "" {
		body["lyrics"] = lyrics
	}

	jsonBody, err := json.Marshal(body)
	if err != nil {
		return nil, nil, fmt.Errorf("marshal request: %w", err)
	}

	url := strings.TrimRight(apiBase, "/") + "/music_generation"
	req, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewReader(jsonBody))
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

	var minimaxResp struct {
		Data *struct {
			Audio string `json:"audio"`
			Music string `json:"music"`
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
		return nil, nil, fmt.Errorf("no data in MiniMax music response")
	}

	audioURL := minimaxResp.Data.Audio
	if audioURL == "" {
		audioURL = minimaxResp.Data.Music
	}
	if audioURL == "" {
		return nil, nil, fmt.Errorf("no audio URL in MiniMax music response")
	}

	// Download the audio file from the returned URL.
	dlReq, err := http.NewRequestWithContext(ctx, "GET", audioURL, nil)
	if err != nil {
		return nil, nil, fmt.Errorf("create download request: %w", err)
	}

	dlClient := &http.Client{}
	dlResp, err := dlClient.Do(dlReq)
	if err != nil {
		return nil, nil, fmt.Errorf("download audio: %w", err)
	}
	defer dlResp.Body.Close()

	if dlResp.StatusCode != http.StatusOK {
		dlBody, _ := io.ReadAll(dlResp.Body)
		return nil, nil, fmt.Errorf("download error %d: %s", dlResp.StatusCode, truncateBytes(dlBody, 300))
	}

	audioBytes, err := limitedReadAll(dlResp.Body, maxMediaDownloadBytes)
	if err != nil {
		return nil, nil, fmt.Errorf("read audio data: %w", err)
	}

	return audioBytes, nil, nil
}
