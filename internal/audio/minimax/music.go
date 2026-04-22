package minimax

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/nextlevelbuilder/goclaw/internal/audio"
)

// maxMusicDownloadBytes caps the audio download at 200 MB.
const maxMusicDownloadBytes = 200 * 1024 * 1024

// MusicConfig configures the MiniMax music provider.
type MusicConfig struct {
	APIKey  string
	APIBase string // default "https://api.minimaxi.chat/v1"
	Model   string // default "music-2.5+"
}

// MusicProvider generates music via the MiniMax music generation API.
// Endpoint: POST {apiBase}/music_generation
// Migrated from internal/tools/create_audio_minimax.go.
type MusicProvider struct {
	apiKey  string
	apiBase string
	model   string
}

// NewMusicProvider returns a MiniMax music provider.
func NewMusicProvider(cfg MusicConfig) *MusicProvider {
	if cfg.APIBase == "" {
		cfg.APIBase = "https://api.minimaxi.chat/v1"
	}
	if cfg.Model == "" {
		cfg.Model = "music-2.5+"
	}
	return &MusicProvider{
		apiKey:  cfg.APIKey,
		apiBase: cfg.APIBase,
		model:   cfg.Model,
	}
}

// Name returns the stable provider identifier used by the Manager.
func (p *MusicProvider) Name() string { return "minimax" }

// GenerateMusic calls the MiniMax music_generation endpoint.
// When lyrics is empty and instrumental is false, instrumental is forced true
// (MiniMax requires lyrics when is_instrumental=false).
func (p *MusicProvider) GenerateMusic(ctx context.Context, opts audio.MusicOptions) (*audio.AudioResult, error) {
	model := opts.Model
	if model == "" {
		model = p.model
	}

	lyrics := opts.Lyrics
	instrumental := opts.Instrumental
	if !instrumental && lyrics == "" {
		instrumental = true
	}

	body := map[string]any{
		"model":            model,
		"prompt":           opts.Prompt,
		"is_instrumental":  instrumental,
		"lyrics_optimizer": false,
		"output_format":    "url",
		"audio_setting": map[string]any{
			"sample_rate": 44100,
			"bitrate":     256000,
			"format":      "mp3",
		},
	}
	if lyrics != "" {
		body["lyrics"] = lyrics
	}

	jsonBody, err := json.Marshal(body)
	if err != nil {
		return nil, fmt.Errorf("marshal minimax music request: %w", err)
	}

	url := strings.TrimRight(p.apiBase, "/") + "/music_generation"
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(jsonBody))
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+p.apiKey)

	resp, err := (&http.Client{}).Do(req)
	if err != nil {
		return nil, fmt.Errorf("http request: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read response: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("MiniMax API error %d: %s", resp.StatusCode, truncate(respBody, 500))
	}

	var mmResp struct {
		Data *struct {
			Audio string `json:"audio"`
			Music string `json:"music"`
		} `json:"data"`
		BaseResp *struct {
			StatusCode int    `json:"status_code"`
			StatusMsg  string `json:"status_msg"`
		} `json:"base_resp"`
	}
	if err := json.Unmarshal(respBody, &mmResp); err != nil {
		return nil, fmt.Errorf("parse response: %w", err)
	}

	if mmResp.BaseResp != nil && mmResp.BaseResp.StatusCode != 0 {
		return nil, fmt.Errorf("MiniMax API error %d: %s",
			mmResp.BaseResp.StatusCode, mmResp.BaseResp.StatusMsg)
	}
	if mmResp.Data == nil {
		return nil, fmt.Errorf("no data in MiniMax music response")
	}

	audioURL := mmResp.Data.Audio
	if audioURL == "" {
		audioURL = mmResp.Data.Music
	}
	if audioURL == "" {
		return nil, fmt.Errorf("no audio URL in MiniMax music response")
	}

	// Download the audio file from the returned URL.
	dlReq, err := http.NewRequestWithContext(ctx, http.MethodGet, audioURL, nil)
	if err != nil {
		return nil, fmt.Errorf("create download request: %w", err)
	}
	dlResp, err := (&http.Client{}).Do(dlReq)
	if err != nil {
		return nil, fmt.Errorf("download audio: %w", err)
	}
	defer dlResp.Body.Close()

	if dlResp.StatusCode != http.StatusOK {
		dlBody, _ := io.ReadAll(dlResp.Body)
		return nil, fmt.Errorf("download error %d: %s", dlResp.StatusCode, truncate(dlBody, 300))
	}

	audioBytes, err := limitedRead(dlResp.Body, maxMusicDownloadBytes)
	if err != nil {
		return nil, fmt.Errorf("read audio data: %w", err)
	}

	return &audio.AudioResult{
		Audio:     audioBytes,
		Extension: "mp3",
		MimeType:  "audio/mpeg",
		Model:     model,
		Provider:  "minimax",
	}, nil
}

func truncate(b []byte, n int) string {
	if len(b) <= n {
		return string(b)
	}
	return string(b[:n]) + "...(truncated)"
}

func limitedRead(r io.Reader, maxBytes int64) ([]byte, error) {
	lr := io.LimitReader(r, maxBytes+1)
	data, err := io.ReadAll(lr)
	if err != nil {
		return nil, err
	}
	if int64(len(data)) > maxBytes {
		return nil, fmt.Errorf("audio response exceeds %d MB limit", maxBytes/(1024*1024))
	}
	return data, nil
}
