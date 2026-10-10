package zalobot

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

const (
	DefaultBaseURL = "https://bot-api.zaloplatforms.com"
)

type BotUser struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	Username string `json:"username"`
}

type User struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

type Chat struct {
	ID       string `json:"id"`
	ChatType string `json:"chat_type"` // "DIRECT" or "GROUP"
}

type PhotoSize struct {
	FileID   string `json:"file_id"`
	URL      string `json:"url"`
	Width    int    `json:"width,omitempty"`
	Height   int    `json:"height,omitempty"`
	FileSize int64  `json:"file_size,omitempty"`
}

type Message struct {
	MessageID string      `json:"message_id"`
	From      User        `json:"from"`
	Chat      Chat        `json:"chat"`
	Date      int64       `json:"date"`
	Text      string      `json:"text"`
	Photo     []PhotoSize `json:"photo,omitempty"`
	Caption   string      `json:"caption,omitempty"`
}

type Update struct {
	UpdateID int64    `json:"update_id"`
	Message  *Message `json:"message,omitempty"`
}

type APIResponse[T any] struct {
	Ok          bool   `json:"ok"`
	Result      T      `json:"result"`
	Description string `json:"description,omitempty"`
	ErrorCode   int    `json:"error_code,omitempty"`
}

type SentMessageResult struct {
	MessageID string `json:"message_id"`
}

type getUpdatesRequest struct {
	Offset  int64 `json:"offset"`
	Limit   int   `json:"limit"`
	Timeout int   `json:"timeout"`
}

type sendMessageRequest struct {
	ChatID string `json:"chat_id"`
	Text   string `json:"text"`
}

type sendPhotoRequest struct {
	ChatID  string `json:"chat_id"`
	Photo   string `json:"photo"`
	Caption string `json:"caption,omitempty"`
}

type Client struct {
	token      string
	baseURL    string
	httpClient *http.Client
}

type ClientOption func(*Client)

func WithBaseURL(baseURL string) ClientOption {
	return func(c *Client) {
		if baseURL != "" {
			c.baseURL = baseURL
		}
	}
}

func WithHTTPClient(httpClient *http.Client) ClientOption {
	return func(c *Client) {
		if httpClient != nil {
			c.httpClient = httpClient
		}
	}
}

func NewClient(token string, opts ...ClientOption) *Client {
	c := &Client{
		token:   token,
		baseURL: DefaultBaseURL,
		httpClient: &http.Client{
			Timeout: 60 * time.Second,
		},
	}
	for _, opt := range opts {
		opt(c)
	}
	return c
}

func (c *Client) doRequest(ctx context.Context, method, endpoint string, body any, respOut any) error {
	var bodyReader io.Reader
	if body != nil {
		data, err := json.Marshal(body)
		if err != nil {
			return fmt.Errorf("marshal request: %w", err)
		}
		bodyReader = bytes.NewReader(data)
	}

	req, err := http.NewRequestWithContext(ctx, method, c.baseURL+endpoint, bodyReader)
	if err != nil {
		return fmt.Errorf("create request: %w", err)
	}

	req.Header.Set("bot-token", c.token)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("execute request: %w", err)
	}
	defer resp.Body.Close()

	respBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("read response body: %w", err)
	}

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("api error (status %d): %s", resp.StatusCode, string(respBytes))
	}

	if respOut != nil {
		if err := json.Unmarshal(respBytes, respOut); err != nil {
			return fmt.Errorf("unmarshal response (%s): %w", string(respBytes), err)
		}
	}

	return nil
}

func (c *Client) GetMe(ctx context.Context) (*BotUser, error) {
	var resp APIResponse[BotUser]
	if err := c.doRequest(ctx, http.MethodGet, "/bot/getMe", nil, &resp); err != nil {
		return nil, err
	}
	if !resp.Ok {
		return nil, fmt.Errorf("getMe returned ok=false: %s", resp.Description)
	}
	return &resp.Result, nil
}

func (c *Client) GetUpdates(ctx context.Context, offset int64, limit int, timeoutSec int) ([]Update, error) {
	req := getUpdatesRequest{
		Offset:  offset,
		Limit:   limit,
		Timeout: timeoutSec,
	}
	var resp APIResponse[[]Update]
	if err := c.doRequest(ctx, http.MethodPost, "/bot/getUpdates", req, &resp); err != nil {
		return nil, err
	}
	if !resp.Ok {
		return nil, fmt.Errorf("getUpdates returned ok=false: %s", resp.Description)
	}
	return resp.Result, nil
}

func (c *Client) SendMessage(ctx context.Context, chatID, text string) error {
	req := sendMessageRequest{
		ChatID: chatID,
		Text:   text,
	}
	var resp APIResponse[SentMessageResult]
	if err := c.doRequest(ctx, http.MethodPost, "/bot/sendMessage", req, &resp); err != nil {
		return err
	}
	if !resp.Ok {
		return fmt.Errorf("sendMessage returned ok=false: %s", resp.Description)
	}
	return nil
}

func (c *Client) SendPhoto(ctx context.Context, chatID, photoURL, caption string) error {
	req := sendPhotoRequest{
		ChatID:  chatID,
		Photo:   photoURL,
		Caption: caption,
	}
	var resp APIResponse[SentMessageResult]
	if err := c.doRequest(ctx, http.MethodPost, "/bot/sendPhoto", req, &resp); err != nil {
		return err
	}
	if !resp.Ok {
		return fmt.Errorf("sendPhoto returned ok=false: %s", resp.Description)
	}
	return nil
}

func (c *Client) DownloadMedia(ctx context.Context, mediaURL string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, mediaURL, nil)
	if err != nil {
		return nil, fmt.Errorf("create media request: %w", err)
	}
	req.Header.Set("bot-token", c.token)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("fetch media: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("download media status %d", resp.StatusCode)
	}
	return io.ReadAll(resp.Body)
}
