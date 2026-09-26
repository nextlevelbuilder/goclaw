package mattermost

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"strings"

	"github.com/nextlevelbuilder/goclaw/internal/bus"
	"github.com/nextlevelbuilder/goclaw/internal/channels"
	"github.com/nextlevelbuilder/goclaw/internal/config"
	"github.com/nextlevelbuilder/goclaw/internal/store"
)

// --- Factory types for DB instance loading ---

// mattermostCreds holds decrypted credentials from channel_instances table.
type mattermostCreds struct {
	ServerURL    string `json:"server_url"`              // canonical
	BotToken     string `json:"bot_token"`               // canonical
	BotUserID    string `json:"bot_user_id,omitempty"`   // auto-detected if empty
	TeamID       string `json:"team_id,omitempty"`
	TeamName     string `json:"team_name,omitempty"`
	// Legacy/alternative field names (from pre-existing DB rows)
	MattermostURL string `json:"mattermost_url,omitempty"` // maps to ServerURL
	APIToken      string `json:"api_token,omitempty"`      // maps to BotToken
	BotUsername   string `json:"bot_username,omitempty"`
}

// mattermostInstanceConfig holds non-secret config from channel_instances table.
type mattermostInstanceConfig struct {
	DMPolicy    string   `json:"dm_policy,omitempty"`
	GroupPolicy string   `json:"group_policy,omitempty"`
	AllowFrom   []string `json:"allow_from,omitempty"`
	TeamName    string   `json:"team_name,omitempty"`
}

// Factory creates a Mattermost channel from DB instance credentials.
func Factory(name string, creds json.RawMessage, cfg json.RawMessage,
	msgBus *bus.MessageBus, pairingSvc store.PairingStore) (channels.Channel, error) {
	return buildChannel(name, creds, cfg, msgBus, pairingSvc)
}

// FactoryWithStores is the full factory with optional store dependencies.
func FactoryWithStores(agentStore store.AgentStore) channels.ChannelFactory {
	return func(name string, creds json.RawMessage, cfg json.RawMessage,
		msgBus *bus.MessageBus, pairingSvc store.PairingStore) (channels.Channel, error) {
		return buildChannel(name, creds, cfg, msgBus, pairingSvc, WithAgentStore(agentStore))
	}
}

func buildChannel(name string, creds json.RawMessage, cfg json.RawMessage,
	msgBus *bus.MessageBus, _ store.PairingStore, opts ...Option) (channels.Channel, error) {

	// Parse credentials
	var c mattermostCreds
	if err := json.Unmarshal(creds, &c); err != nil {
		return nil, fmt.Errorf("parse mattermost creds: %w", err)
	}
	// Normalize: map legacy field names to canonical ones
	if c.ServerURL == "" {
		c.ServerURL = c.MattermostURL
	}
	if c.BotToken == "" {
		c.BotToken = c.APIToken
	}

	if c.ServerURL == "" {
		return nil, fmt.Errorf("mattermost server_url is required")
	}
	if c.BotToken == "" {
		return nil, fmt.Errorf("mattermost bot_token is required")
	}

	// Parse non-secret config
	var ic mattermostInstanceConfig
	if cfg != nil {
		json.Unmarshal(cfg, &ic)
	}

	// Build config.MattermostConfig
	mmCfg := config.MattermostConfig{
		Enabled:     true,
		ServerURL:   c.ServerURL,
		BotToken:    c.BotToken,
		BotUserID:   c.BotUserID,
		TeamID:      c.TeamID,
		TeamName:    c.TeamName,
		DMPolicy:    ic.DMPolicy,
		GroupPolicy: ic.GroupPolicy,
		AllowFrom:   ic.AllowFrom,
	}

	// Default policies for DB instances only when truly empty
	if mmCfg.GroupPolicy == "" {
		mmCfg.GroupPolicy = "open"
	}
	if mmCfg.DMPolicy == "" {
		mmCfg.DMPolicy = "open"
	}

	// Create channel
	ch, err := New(mmCfg, msgBus, opts...)
	if err != nil {
		return nil, fmt.Errorf("create mattermost channel: %w", err)
	}

	// Override name from DB instance
	ch.SetName(name)
	return ch, nil
}

// --- File helpers (kept simple to avoid platform-specific imports) ---

// openFile opens a file for reading — used for media upload.
func openFile(path string) (io.ReadCloser, error) {
	return os.Open(path)
}

// uploadFromReader uploads file content to Mattermost via multipart form.
func (c *Channel) uploadFromReader(ctx context.Context, channelID string, reader io.Reader, contentType string) (string, error) {
	// Read all content first (simple approach)
	data, err := io.ReadAll(reader)
	if err != nil {
		return "", fmt.Errorf("read media data: %w", err)
	}

	if len(data) == 0 {
		return "", nil
	}

	// Upload via Mattermost files API
	// POST /api/v4/files?channel_id=xxx&filename=xxx
	url := fmt.Sprintf("%s/api/v4/files?channel_id=%s&filename=media", c.serverURL, channelID)

	body, err := c.uploadMultipart(ctx, url, data, contentType)
	if err != nil {
		return "", fmt.Errorf("upload multipart: %w", err)
	}

	// Parse response
	var resp struct {
		Infos []struct {
			ID string `json:"id"`
		} `json:"file_infos"`
	}
	if err := json.Unmarshal(body, &resp); err != nil {
		return "", fmt.Errorf("parse upload response: %w", err)
	}

	if len(resp.Infos) == 0 {
		return "", fmt.Errorf("no file_infos in upload response")
	}

	slog.Debug("mattermost file uploaded", "file_id", resp.Infos[0].ID, "size", len(data))
	return resp.Infos[0].ID, nil
}

// newMultipartRequest creates an HTTP request with multipart/form-data body.
func newMultipartRequest(ctx context.Context, method, url string, body []byte, boundary, token string) (*http.Request, error) {
	req, err := http.NewRequestWithContext(ctx, method, url, strings.NewReader(string(body)))
	if err != nil {
		return nil, fmt.Errorf("create multipart request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "multipart/form-data; boundary="+boundary)
	return req, nil
}

// uploadMultipart performs a multipart/form-data file upload.
func (c *Channel) uploadMultipart(ctx context.Context, url string, data []byte, contentType string) ([]byte, error) {
	// Build multipart manually
	boundary := "----GoClawBoundary1234567890"

	var body []byte
	body = append(body, []byte("--"+boundary+"\r\n")...)

	// Determine content type
	if contentType == "" {
		contentType = "application/octet-stream"
	}

	body = append(body, []byte(fmt.Sprintf(
		"Content-Disposition: form-data; name=\"files\"; filename=\"media\"\r\n"+
			"Content-Type: %s\r\n\r\n", contentType))...)
	body = append(body, data...)
	body = append(body, []byte("\r\n--"+boundary+"--\r\n")...)

	// Create request
	req, err := newMultipartRequest(ctx, "POST", url, body, boundary, c.botToken)
	if err != nil {
		return nil, err
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("upload request: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read upload response: %w", err)
	}

	if resp.StatusCode >= 400 {
		return nil, fmt.Errorf("upload HTTP %d: %s", resp.StatusCode, string(respBody))
	}

	return respBody, nil
}
