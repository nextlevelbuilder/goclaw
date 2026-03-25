package googlechat

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"

	"golang.org/x/oauth2"
	"golang.org/x/oauth2/google"

	"github.com/nextlevelbuilder/goclaw/internal/providers"
)

const (
	chatAPIBase = "https://chat.googleapis.com/v1/"
	chatScope   = "https://www.googleapis.com/auth/chat.bot"
)

// ChatClient is a lightweight Google Chat REST API client.
type ChatClient struct {
	httpClient  *http.Client
	tokenSource oauth2.TokenSource
}

// NewChatClient creates a client authenticated via Service Account JSON content.
func NewChatClient(saJSON []byte) (*ChatClient, error) {
	conf, err := google.JWTConfigFromJSON(saJSON, chatScope)
	if err != nil {
		return nil, fmt.Errorf("parse service account: %w", err)
	}

	ts := conf.TokenSource(context.Background())

	return &ChatClient{
		httpClient:  &http.Client{Timeout: 30 * time.Second},
		tokenSource: ts,
	}, nil
}

// doJSON performs an authenticated JSON request to the Google Chat API.
func (c *ChatClient) doJSON(ctx context.Context, method, url string, body interface{}) ([]byte, error) {
	var bodyReader io.Reader
	if body != nil {
		data, err := json.Marshal(body)
		if err != nil {
			return nil, fmt.Errorf("marshal body: %w", err)
		}
		bodyReader = bytes.NewReader(data)
	}

	req, err := http.NewRequestWithContext(ctx, method, url, bodyReader)
	if err != nil {
		return nil, err
	}

	token, err := c.tokenSource.Token()
	if err != nil {
		return nil, fmt.Errorf("get token: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+token.AccessToken)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("google chat api %s: %w", method, err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20)) // limit to 1MB
	if err != nil {
		return nil, fmt.Errorf("read response body: %w", err)
	}
	if resp.StatusCode >= 400 {
		retryAfter := providers.ParseRetryAfter(resp.Header.Get("Retry-After"))
		return nil, &providers.HTTPError{
			Status:     resp.StatusCode,
			Body:       string(respBody),
			RetryAfter: retryAfter,
		}
	}
	return respBody, nil
}

// googleChatSendRetryConfig is used only for SendMessage — not reactions (fire-and-forget).
// GChat rate limit ~10 msg/s/space → MinDelay 1s instead of 300ms used by LLM providers.
var googleChatSendRetryConfig = providers.RetryConfig{
	Attempts: 5,
	MinDelay: 1 * time.Second,
	MaxDelay: 30 * time.Second,
	Jitter:   0.1,
}

// SendMessage sends a text message to a Google Chat space with retry on 429/5xx.
// spaceName format: "spaces/SPACE_ID"
// AddReaction/DeleteReaction are fire-and-forget — they call doJSON directly without retry.
func (c *ChatClient) SendMessage(ctx context.Context, spaceName, text string) error {
	url := chatAPIBase + spaceName + "/messages"
	msg := gcSendMessage{Text: text}
	// doJSON marshals body fresh each call, so retrying is safe — no consumed reader.
	_, err := providers.RetryDo[[]byte](ctx, googleChatSendRetryConfig, func() ([]byte, error) {
		return c.doJSON(ctx, "POST", url, msg)
	})
	return err
}

// AddReaction adds an emoji reaction to a message.
// messageName format: "spaces/SPACE_ID/messages/MSG_ID"
// Returns the reaction resource name for deletion.
func (c *ChatClient) AddReaction(ctx context.Context, messageName, unicode string) (string, error) {
	url := chatAPIBase + messageName + "/reactions"
	body := gcReaction{Emoji: &gcEmoji{Unicode: unicode}}
	respBody, err := c.doJSON(ctx, "POST", url, body)
	if err != nil {
		return "", err
	}
	var result struct {
		Name string `json:"name"`
	}
	if err := json.Unmarshal(respBody, &result); err != nil {
		return "", fmt.Errorf("parse reaction response: %w", err)
	}
	return result.Name, nil
}

// DeleteReaction removes a reaction from a message.
// reactionName format: "spaces/SPACE_ID/messages/MSG_ID/reactions/REACTION_ID"
func (c *ChatClient) DeleteReaction(ctx context.Context, reactionName string) error {
	url := chatAPIBase + reactionName
	_, err := c.doJSON(ctx, "DELETE", url, nil)
	return err
}
