package tools

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/nextlevelbuilder/goclaw/internal/providers"
)

// geminiNativeDocumentCall sends a document to Gemini's native generateContent API.
// Uses inlineData with the document's MIME type (e.g. application/pdf) which Gemini
// supports natively, unlike the OpenAI-compat endpoint that only handles images.
func geminiNativeDocumentCall(ctx context.Context, apiKey, model, prompt string, docData []byte, docMime string) (*providers.ChatResponse, error) {
	b64 := base64.StdEncoding.EncodeToString(docData)

	// Gemini generateContent request body.
	body := map[string]any{
		"contents": []map[string]any{
			{
				"parts": []map[string]any{
					{
						"inline_data": map[string]any{
							"mime_type": docMime,
							"data":      b64,
						},
					},
					{
						"text": prompt,
					},
				},
			},
		},
		"generationConfig": map[string]any{
			"maxOutputTokens": 16384,
			"temperature":     0.2,
		},
	}

	bodyJSON, err := json.Marshal(body)
	if err != nil {
		return nil, fmt.Errorf("marshal request: %w", err)
	}

	url := fmt.Sprintf("https://generativelanguage.googleapis.com/v1beta/models/%s:generateContent?key=%s", model, apiKey)

	httpReq, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewReader(bodyJSON))
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")

	client := &http.Client{Timeout: 120 * time.Second}
	httpResp, err := client.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("HTTP request: %w", err)
	}
	defer httpResp.Body.Close()

	respBody, err := io.ReadAll(httpResp.Body)
	if err != nil {
		return nil, fmt.Errorf("read response: %w", err)
	}

	if httpResp.StatusCode != 200 {
		return nil, fmt.Errorf("HTTP %d: %s", httpResp.StatusCode, truncateStr(string(respBody), 500))
	}

	// Parse Gemini native response format.
	var geminiResp struct {
		Candidates []struct {
			Content struct {
				Parts []struct {
					Text string `json:"text"`
				} `json:"parts"`
			} `json:"content"`
		} `json:"candidates"`
		UsageMetadata struct {
			PromptTokenCount     int `json:"promptTokenCount"`
			CandidatesTokenCount int `json:"candidatesTokenCount"`
			TotalTokenCount      int `json:"totalTokenCount"`
		} `json:"usageMetadata"`
	}

	if err := json.Unmarshal(respBody, &geminiResp); err != nil {
		return nil, fmt.Errorf("parse response: %w", err)
	}

	// Extract text from response.
	var content string
	if len(geminiResp.Candidates) > 0 {
		for _, part := range geminiResp.Candidates[0].Content.Parts {
			if part.Text != "" {
				if content != "" {
					content += "\n"
				}
				content += part.Text
			}
		}
	}
	if content == "" {
		return nil, fmt.Errorf("empty response from Gemini")
	}

	return &providers.ChatResponse{
		Content:      content,
		FinishReason: "stop",
		Usage: &providers.Usage{
			PromptTokens:     geminiResp.UsageMetadata.PromptTokenCount,
			CompletionTokens: geminiResp.UsageMetadata.CandidatesTokenCount,
			TotalTokens:      geminiResp.UsageMetadata.TotalTokenCount,
		},
	}, nil
}
