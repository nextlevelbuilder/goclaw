package proxyserver

import (
	"bytes"
	"encoding/json"
	"io"
	"log/slog"
	"math"
	"net/http"
	"strconv"
	"strings"
	"unicode"

	"github.com/nextlevelbuilder/goclaw/internal/proxyserver/validation"
)

// ResponseModifier modifies responses from upstream servers.
type ResponseModifier struct {
	logger         *slog.Logger
	audioExtractor *validation.AudioExtractor
}

// NewResponseModifier creates a new response modifier.
func NewResponseModifier(logger *slog.Logger, audioExtractor *validation.AudioExtractor) *ResponseModifier {
	return &ResponseModifier{
		logger:         logger.With("component", "response-modifier"),
		audioExtractor: audioExtractor,
	}
}

// ModifyResponseFunc creates a ModifyResponse function for httputil.ReverseProxy.
func (m *ResponseModifier) ModifyResponseFunc(model string, isAudioAPI, isTranscription, isTTS, isVideoGeneration bool) func(*http.Response) error {
	return func(resp *http.Response) error {
		resp.Header.Set("X-Model", model)

		contentType := resp.Header.Get("Content-Type")
		if strings.HasPrefix(contentType, "text/event-stream") {
			m.logger.Debug("streaming response, skipping body modification")
			return nil
		}

		if resp.StatusCode < 200 || resp.StatusCode >= 300 {
			return nil
		}

		if isTTS && strings.HasPrefix(contentType, "audio/") {
			return m.handleTTSAudioResponse(resp)
		}

		if !strings.HasPrefix(contentType, "application/json") {
			return nil
		}

		body, err := io.ReadAll(resp.Body)
		if err != nil {
			m.logger.Warn("failed to read response body", "error", err)
			return nil
		}
		resp.Body.Close()

		var payload map[string]interface{}
		if err := json.Unmarshal(body, &payload); err != nil {
			resp.Body = io.NopCloser(bytes.NewReader(body))
			return nil
		}

		if isVideoGeneration {
			m.handleVideoGenerationResponse(resp, payload)
		} else if usage, ok := payload["usage"].(map[string]interface{}); ok {
			m.addUsageHeaders(resp, usage, isAudioAPI, isTranscription, isTTS)
		} else if isAudioAPI {
			if isTranscription {
				m.synthesizeTranscriptionUsage(resp, payload)
			} else if isTTS {
				m.synthesizeTTSUsage(resp, payload)
			}
		}

		if modifiedBody, err := json.Marshal(payload); err == nil {
			body = modifiedBody
		}

		resp.Body = io.NopCloser(bytes.NewReader(body))
		resp.ContentLength = int64(len(body))
		resp.Header.Set("Content-Length", strconv.Itoa(len(body)))

		return nil
	}
}

func (m *ResponseModifier) addUsageHeaders(resp *http.Response, usage map[string]interface{}, isAudioAPI, isTranscription, isTTS bool) {
	if isAudioAPI {
		if isTranscription {
			if v, ok := usage["input_duration"]; ok {
				resp.Header.Set("X-Usage-Input-Duration", formatNumber(v))
			}
			if v, ok := usage["output_tokens"]; ok {
				resp.Header.Set("X-Usage-Output-Tokens", formatNumber(v))
			}
		} else if isTTS {
			if v, ok := usage["input_tokens"]; ok {
				resp.Header.Set("X-Usage-Input-Tokens", formatNumber(v))
			}
			if v, ok := usage["output_duration"]; ok {
				resp.Header.Set("X-Usage-Output-Duration", formatNumber(v))
			}
		}
	} else {
		if v := getUsageValue(usage, "prompt_tokens", "input_tokens"); v != "" {
			resp.Header.Set("X-Usage-Prompt-Tokens", v)
		}
		if v := getUsageValue(usage, "completion_tokens", "output_tokens"); v != "" {
			resp.Header.Set("X-Usage-Completion-Tokens", v)
		}
		if v, ok := usage["total_tokens"]; ok {
			resp.Header.Set("X-Usage-Total-Tokens", formatNumber(v))
		}
	}

	m.logger.Debug("added usage headers to response",
		"is_audio_api", isAudioAPI,
		"is_transcription", isTranscription,
		"is_tts", isTTS)
}

func getUsageValue(usage map[string]interface{}, keys ...string) string {
	for _, key := range keys {
		if v, ok := usage[key]; ok {
			return formatNumber(v)
		}
	}
	return ""
}

func formatNumber(v interface{}) string {
	switch val := v.(type) {
	case float64:
		if val == float64(int64(val)) {
			return strconv.FormatInt(int64(val), 10)
		}
		return strconv.FormatFloat(val, 'f', 2, 64)
	case int:
		return strconv.Itoa(val)
	case int64:
		return strconv.FormatInt(val, 10)
	case string:
		return val
	default:
		return ""
	}
}

// IsAudioTranscriptionPath checks if the path is for audio transcription.
func IsAudioTranscriptionPath(path string) bool {
	return strings.Contains(path, "audio/transcriptions")
}

// IsTextToSpeechPath checks if the path is for text-to-speech.
func IsTextToSpeechPath(path string) bool {
	return strings.Contains(path, "audio/speech") || strings.Contains(strings.ToLower(path), "tts")
}

// IsAudioAPIPath checks if the path is for any audio API.
func IsAudioAPIPath(path string) bool {
	return IsAudioTranscriptionPath(path) || IsTextToSpeechPath(path)
}

// IsVideoGenerationPath checks if the path is for a video generation API.
func IsVideoGenerationPath(path string) bool {
	return strings.Contains(path, "contents/generations/tasks")
}

// IsVideoGenerationCreatePath checks if the path is specifically for creating a video generation task.
func IsVideoGenerationCreatePath(path string) bool {
	p := strings.TrimRight(path, "/")
	return strings.HasSuffix(p, "contents/generations/tasks")
}

func estimateTokenCount(text string) int {
	if text == "" {
		return 0
	}

	runes := []rune(text)
	cjkCount := 0
	latinCount := 0

	for _, r := range runes {
		if isCJKOrVietnamese(r) {
			cjkCount++
		} else if !unicode.IsSpace(r) {
			latinCount++
		}
	}

	tokens := float64(cjkCount) + float64(latinCount)/4.0
	if tokens < 1 {
		tokens = 1
	}

	return int(math.Ceil(tokens))
}

func isCJKOrVietnamese(r rune) bool {
	if r >= 0x4E00 && r <= 0x9FFF {
		return true
	}
	if r >= 0x3400 && r <= 0x4DBF {
		return true
	}
	if r >= 0xAC00 && r <= 0xD7AF {
		return true
	}
	if r >= 0x30A0 && r <= 0x30FF {
		return true
	}
	if r >= 0x3040 && r <= 0x309F {
		return true
	}
	if r >= 0x00C0 && r <= 0x024F {
		return true
	}
	if r >= 0x1EA0 && r <= 0x1EFF {
		return true
	}
	return false
}

func (m *ResponseModifier) synthesizeTranscriptionUsage(resp *http.Response, payload map[string]interface{}) {
	var inputDuration float64
	if resp.Request != nil {
		if dur, ok := resp.Request.Context().Value(ctxKeyAudioDuration).(float64); ok && dur > 0 {
			inputDuration = dur
		}
	}

	var outputTokens int
	if text, ok := payload["text"].(string); ok && text != "" {
		outputTokens = estimateTokenCount(text)
	}

	m.logger.Info("synthesizing transcription usage",
		"input_duration", inputDuration,
		"output_tokens", outputTokens)

	resp.Header.Set("X-Usage-Input-Duration", strconv.FormatFloat(inputDuration, 'f', 2, 64))
	resp.Header.Set("X-Usage-Output-Tokens", strconv.Itoa(outputTokens))

	payload["usage"] = map[string]interface{}{
		"input_duration": inputDuration,
		"output_tokens":  float64(outputTokens),
	}
}

func (m *ResponseModifier) synthesizeTTSUsage(resp *http.Response, payload map[string]interface{}) {
	var inputTokens int
	if resp.Request != nil {
		if inputText, ok := resp.Request.Context().Value(ctxKeyInputText).(string); ok && inputText != "" {
			inputTokens = estimateTokenCount(inputText)
		}
	}

	var outputDuration float64
	if dur, ok := payload["duration"].(float64); ok {
		outputDuration = dur
	}

	m.logger.Info("synthesizing TTS usage from JSON response",
		"input_tokens", inputTokens,
		"output_duration", outputDuration)

	resp.Header.Set("X-Usage-Input-Tokens", strconv.Itoa(inputTokens))
	resp.Header.Set("X-Usage-Output-Duration", strconv.FormatFloat(outputDuration, 'f', 2, 64))

	payload["usage"] = map[string]interface{}{
		"input_tokens":    float64(inputTokens),
		"output_duration": outputDuration,
	}
}

func (m *ResponseModifier) handleTTSAudioResponse(resp *http.Response) error {
	var outputDuration float64
	if m.audioExtractor != nil {
		audioData, err := io.ReadAll(resp.Body)
		if err != nil {
			m.logger.Warn("failed to read TTS audio response body", "error", err)
			return nil
		}
		resp.Body.Close()

		outputDuration = m.audioExtractor.GetDuration(audioData, "tts_output")

		resp.Body = io.NopCloser(bytes.NewReader(audioData))
		resp.ContentLength = int64(len(audioData))
	}

	var inputTokens int
	if resp.Request != nil {
		if inputText, ok := resp.Request.Context().Value(ctxKeyInputText).(string); ok && inputText != "" {
			inputTokens = estimateTokenCount(inputText)
		}
	}

	m.logger.Info("synthesizing TTS usage from audio response",
		"input_tokens", inputTokens,
		"output_duration", outputDuration)

	resp.Header.Set("X-Usage-Input-Tokens", strconv.Itoa(inputTokens))
	resp.Header.Set("X-Usage-Output-Duration", strconv.FormatFloat(outputDuration, 'f', 2, 64))

	return nil
}

func (m *ResponseModifier) handleVideoGenerationResponse(resp *http.Response, payload map[string]interface{}) {
	status, _ := payload["status"].(string)
	if status != "succeeded" {
		m.logger.Debug("video generation task not succeeded, skipping usage extraction",
			"status", status)
		return
	}

	usage, ok := payload["usage"].(map[string]interface{})
	if !ok {
		m.logger.Debug("no usage block in succeeded video generation response")
		return
	}

	if v, exists := usage["completion_tokens"]; exists {
		resp.Header.Set("X-Usage-Completion-Tokens", formatNumber(v))
	}
	if v, exists := usage["total_tokens"]; exists {
		resp.Header.Set("X-Usage-Total-Tokens", formatNumber(v))
	}

	m.logger.Info("extracted video generation usage",
		"status", status)
}
