package mattermost

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"

	"github.com/nextlevelbuilder/goclaw/internal/bus"
)

// Send delivers an outbound message to a Mattermost channel.
func (c *Channel) Send(ctx context.Context, msg bus.OutboundMessage) error {
	if !c.IsRunning() {
		return fmt.Errorf("mattermost channel not running")
	}

	if msg.Content == "" && len(msg.Media) == 0 {
		slog.Debug("mattermost send: skipping empty message")
		return nil
	}

	chatID := msg.ChatID
	if chatID == "" {
		return fmt.Errorf("mattermost send: chatID is empty")
	}

	// Content may be markdown — Mattermost supports markdown natively
	content := msg.Content

	// Chunk if necessary (Mattermost limit: ~16383 chars)
	chunks := chunkText(content, mattermostMaxMessageLen)
	for i, chunk := range chunks {
		// For multi-chunk messages, prefix with part number
		if len(chunks) > 1 {
			chunk = fmt.Sprintf("**[%d/%d]**\n%s", i+1, len(chunks), chunk)
		}

		// Handle media attachments (only on first chunk)
		var fileIDs []string
		if i == 0 {
			for _, media := range msg.Media {
				fileID, err := c.uploadFile(ctx, chatID, media.URL, media.ContentType)
				if err != nil {
					slog.Warn("mattermost: failed to upload media", "url", media.URL, "error", err)
				} else if fileID != "" {
					fileIDs = append(fileIDs, fileID)
				}
			}
		}

		if err := c.createPost(ctx, chatID, chunk, fileIDs); err != nil {
			return fmt.Errorf("mattermost create post: %w", err)
		}
	}

	slog.Info("mattermost outbound message sent",
		"channel_id", chatID,
		"content_len", len(content),
		"chunks", len(chunks),
	)
	return nil
}

// createPost creates a new post in a Mattermost channel.
func (c *Channel) createPost(ctx context.Context, channelID, message string, fileIDs []string) error {
	payload := map[string]any{
		"channel_id": channelID,
		"message":    message,
	}
	if len(fileIDs) > 0 {
		payload["file_ids"] = fileIDs
	}

	_, err := c.apiPost(ctx, "/api/v4/posts", payload)
	return err
}

// uploadFile uploads a file to Mattermost and returns the file ID.
func (c *Channel) uploadFile(ctx context.Context, channelID, filePath, contentType string) (string, error) {
	// Read file from local path
	if filePath == "" {
		return "", nil
	}

	// Check if it's a URL or local path
	if strings.HasPrefix(filePath, "http://") || strings.HasPrefix(filePath, "https://") {
		// Download first
		resp, err := c.httpClient.Get(filePath)
		if err != nil {
			return "", fmt.Errorf("download media: %w", err)
		}
		defer resp.Body.Close()

		return c.uploadFromReader(ctx, channelID, resp.Body, contentType)
	}

	// Local file — open and upload
	return c.uploadLocalFile(ctx, channelID, filePath)
}

// apiGet performs an authenticated GET request to the Mattermost API.
func (c *Channel) apiGet(ctx context.Context, path string) ([]byte, error) {
	url := c.serverURL + path

	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return nil, fmt.Errorf("create GET request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+c.botToken)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("GET %s: %w", path, err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read response: %w", err)
	}

	if resp.StatusCode >= 400 {
		return nil, fmt.Errorf("GET %s: HTTP %d: %s", path, resp.StatusCode, string(body))
	}

	return body, nil
}

// apiPost performs an authenticated POST request with JSON body.
func (c *Channel) apiPost(ctx context.Context, path string, payload any) ([]byte, error) {
	url := c.serverURL + path

	jsonBody, err := json.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("marshal payload: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, "POST", url, strings.NewReader(string(jsonBody)))
	if err != nil {
		return nil, fmt.Errorf("create POST request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+c.botToken)
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("POST %s: %w", path, err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read response: %w", err)
	}

	if resp.StatusCode >= 400 {
		return nil, fmt.Errorf("POST %s: HTTP %d: %s", path, resp.StatusCode, string(body))
	}

	return body, nil
}

// uploadLocalFile uploads a local file to Mattermost.
func (c *Channel) uploadLocalFile(ctx context.Context, channelID, filePath string) (string, error) {
	// Use multipart form upload via http
	fileReader, err := openFile(filePath)
	if err != nil {
		return "", fmt.Errorf("open file %s: %w", filePath, err)
	}
	defer fileReader.Close()

	return c.uploadFromReader(ctx, channelID, fileReader, "")
}

// chunkText splits text into chunks of maxLen, trying to break at paragraph/newline boundaries.
func chunkText(text string, maxLen int) []string {
	if len(text) <= maxLen {
		return []string{text}
	}

	var chunks []string
	for len(text) > maxLen {
		// Try to find a newline near the limit
		breakAt := maxLen
		for i := maxLen; i > maxLen-500 && i > 0; i-- {
			if text[i] == '\n' {
				breakAt = i + 1
				break
			}
		}
		chunks = append(chunks, text[:breakAt])
		text = text[breakAt:]
	}
	if text != "" {
		chunks = append(chunks, text)
	}
	return chunks
}
