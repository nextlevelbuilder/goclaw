package zalobot

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

const (
	DefaultBaseURL = "https://bot-api.zaloplatforms.com"
)

type BotUser struct {
	ID          string `json:"id"`
	AccountName string `json:"account_name"`
	AccountType string `json:"account_type,omitempty"`
	Name        string `json:"name,omitempty"`
	DisplayName string `json:"display_name,omitempty"`
	Username    string `json:"username,omitempty"`
}

func (u *BotUser) GetName() string {
	if u == nil {
		return ""
	}
	if u.DisplayName != "" {
		return u.DisplayName
	}
	if u.AccountName != "" {
		return u.AccountName
	}
	if u.Name != "" {
		return u.Name
	}
	return u.Username
}

type User struct {
	ID          string `json:"id"`
	DisplayName string `json:"display_name"`
	Name        string `json:"name,omitempty"`
	Username    string `json:"username,omitempty"`
	IsBot       bool   `json:"is_bot,omitempty"`
}

func (u *User) GetName() string {
	if u.DisplayName != "" {
		return u.DisplayName
	}
	if u.Name != "" {
		return u.Name
	}
	return u.Username
}

type Chat struct {
	ID       string `json:"id"`
	ChatType string `json:"chat_type"` // "PRIVATE" or "GROUP"
}

type Message struct {
	MessageID string `json:"message_id"`
	From      User   `json:"from"`
	Chat      Chat   `json:"chat"`
	Date      int64  `json:"date"`
	Text      string `json:"text"`
	Photo     string `json:"photo,omitempty"`
	PhotoURL  string `json:"photo_url,omitempty"`
	Caption   string `json:"caption,omitempty"`
	Sticker   string `json:"sticker,omitempty"`
	VoiceURL  string `json:"voice_url,omitempty"`
}

type Update struct {
	UpdateID  int64    `json:"update_id,omitempty"`
	EventName string   `json:"event_name,omitempty"`
	Message   *Message `json:"message,omitempty"`
}

func (u *Update) MessageID() string {
	if u.Message != nil {
		return u.Message.MessageID
	}
	return ""
}

type APIResponse[T any] struct {
	Ok          bool   `json:"ok"`
	Result      T      `json:"result"`
	Description string `json:"description,omitempty"`
	ErrorCode   int    `json:"error_code,omitempty"`
}

type rawAPIResponse struct {
	Ok          bool            `json:"ok"`
	Result      json.RawMessage `json:"result,omitempty"`
	Description string          `json:"description,omitempty"`
	ErrorCode   int             `json:"error_code,omitempty"`
}

type SentMessageResult struct {
	MessageID string `json:"message_id"`
}

type getUpdatesRequest struct {
	Timeout int `json:"timeout"`
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

func (c *Client) methodURL(method string) string {
	base := strings.TrimRight(c.baseURL, "/")
	method = strings.TrimLeft(method, "/")
	return fmt.Sprintf("%s/bot%s/%s", base, c.token, method)
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

	url := c.methodURL(endpoint)
	req, err := http.NewRequestWithContext(ctx, method, url, bodyReader)
	if err != nil {
		return fmt.Errorf("create request: %w", err)
	}

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

	if resp.StatusCode == 408 {
		return fmt.Errorf("api error (status 408): timeout")
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
	if err := c.doRequest(ctx, http.MethodPost, "getMe", nil, &resp); err != nil {
		return nil, err
	}
	if !resp.Ok {
		return nil, fmt.Errorf("getMe returned ok=false: %s", resp.Description)
	}
	return &resp.Result, nil
}

func (c *Client) GetUpdates(ctx context.Context, timeoutSec int) ([]Update, error) {
	req := getUpdatesRequest{
		Timeout: timeoutSec,
	}
	var raw rawAPIResponse
	if err := c.doRequest(ctx, http.MethodPost, "getUpdates", req, &raw); err != nil {
		if strings.Contains(err.Error(), "408") || strings.Contains(strings.ToLower(err.Error()), "timeout") {
			return nil, nil
		}
		return nil, err
	}
	if !raw.Ok {
		if raw.ErrorCode == 408 || strings.Contains(strings.ToLower(raw.Description), "timeout") {
			return nil, nil
		}
		return nil, fmt.Errorf("getUpdates returned ok=false: %s", raw.Description)
	}

	trimmed := bytes.TrimSpace(raw.Result)
	if len(trimmed) == 0 || string(trimmed) == "null" || string(trimmed) == "{}" || string(trimmed) == "[]" {
		return nil, nil
	}

	if trimmed[0] == '[' {
		var updates []Update
		if err := json.Unmarshal(raw.Result, &updates); err != nil {
			return nil, fmt.Errorf("unmarshal updates: %w", err)
		}
		return updates, nil
	}

	var single Update
	if err := json.Unmarshal(raw.Result, &single); err != nil {
		return nil, fmt.Errorf("unmarshal single update: %w", err)
	}
	if single.Message == nil && single.EventName == "" {
		return nil, nil
	}
	return []Update{single}, nil
}

func (c *Client) SendMessage(ctx context.Context, chatID, text string) error {
	req := sendMessageRequest{
		ChatID: chatID,
		Text:   text,
	}
	var resp APIResponse[SentMessageResult]
	if err := c.doRequest(ctx, http.MethodPost, "sendMessage", req, &resp); err != nil {
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
	if err := c.doRequest(ctx, http.MethodPost, "sendPhoto", req, &resp); err != nil {
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
