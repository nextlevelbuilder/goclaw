package mattermost

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"mime/multipart"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/gorilla/websocket"
)

// MMClient is a lightweight Mattermost APIv4 + WebSocket client.
// It replaces the heavy mattermost/server/public/model.Client4 SDK.
type MMClient struct {
	baseURL    string // e.g. "https://mm.example.com"
	token      string
	httpClient *http.Client
}

// NewMMClient creates a new lightweight Mattermost client.
func NewMMClient(serverURL, token string) *MMClient {
	return &MMClient{
		baseURL: strings.TrimRight(serverURL, "/"),
		token:   token,
		httpClient: &http.Client{
			Timeout: 30 * time.Second,
		},
	}
}

// --- HTTP helpers ---

func (c *MMClient) doJSON(ctx context.Context, method, path string, body any, result any) error {
	var bodyReader io.Reader
	if body != nil {
		data, err := json.Marshal(body)
		if err != nil {
			return fmt.Errorf("marshal request: %w", err)
		}
		bodyReader = bytes.NewReader(data)
	}

	req, err := http.NewRequestWithContext(ctx, method, c.baseURL+"/api/v4"+path, bodyReader)
	if err != nil {
		return fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+c.token)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("http request %s %s: %w", method, path, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		respBody, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return fmt.Errorf("mattermost API %s %s returned %d: %s", method, path, resp.StatusCode, string(respBody))
	}

	if result != nil {
		if err := json.NewDecoder(resp.Body).Decode(result); err != nil {
			return fmt.Errorf("decode response: %w", err)
		}
	}
	return nil
}

// --- API methods ---

// GetMe returns the authenticated user.
func (c *MMClient) GetMe(ctx context.Context) (*MMUser, error) {
	var user MMUser
	if err := c.doJSON(ctx, "GET", "/users/me", nil, &user); err != nil {
		return nil, err
	}
	return &user, nil
}

// GetUser returns a user by ID.
func (c *MMClient) GetUser(ctx context.Context, userID string) (*MMUser, error) {
	var user MMUser
	if err := c.doJSON(ctx, "GET", "/users/"+userID, nil, &user); err != nil {
		return nil, err
	}
	return &user, nil
}

// GetTeamByName returns a team by name.
func (c *MMClient) GetTeamByName(ctx context.Context, name string) (*MMTeam, error) {
	var team MMTeam
	if err := c.doJSON(ctx, "GET", "/teams/name/"+name, nil, &team); err != nil {
		return nil, err
	}
	return &team, nil
}

// GetTeamsForUser returns all teams a user belongs to.
func (c *MMClient) GetTeamsForUser(ctx context.Context, userID string) ([]*MMTeam, error) {
	var teams []*MMTeam
	if err := c.doJSON(ctx, "GET", "/users/"+userID+"/teams", nil, &teams); err != nil {
		return nil, err
	}
	return teams, nil
}

// GetChannel returns channel info.
func (c *MMClient) GetChannel(ctx context.Context, channelID string) (*MMChannel, error) {
	var ch MMChannel
	if err := c.doJSON(ctx, "GET", "/channels/"+channelID, nil, &ch); err != nil {
		return nil, err
	}
	return &ch, nil
}

// CreatePost creates a new post.
func (c *MMClient) CreatePost(ctx context.Context, post *MMPost) (*MMPost, error) {
	var result MMPost
	if err := c.doJSON(ctx, "POST", "/posts", post, &result); err != nil {
		return nil, err
	}
	return &result, nil
}

// PatchPost updates a post partially.
func (c *MMClient) PatchPost(ctx context.Context, postID string, patch *MMPostPatch) error {
	return c.doJSON(ctx, "PUT", "/posts/"+postID+"/patch", patch, nil)
}

// DeletePost deletes a post.
func (c *MMClient) DeletePost(ctx context.Context, postID string) error {
	return c.doJSON(ctx, "DELETE", "/posts/"+postID, nil, nil)
}

// GetFileInfo returns metadata about a file attachment.
func (c *MMClient) GetFileInfo(ctx context.Context, fileID string) (*MMFileInfo, error) {
	var info MMFileInfo
	if err := c.doJSON(ctx, "GET", "/files/"+fileID+"/info", nil, &info); err != nil {
		return nil, err
	}
	return &info, nil
}

// DownloadFileToPath streams a file download directly to disk (never holds full file in RAM).
func (c *MMClient) DownloadFileToPath(ctx context.Context, fileID, destPath string) error {
	req, err := http.NewRequestWithContext(ctx, "GET", c.baseURL+"/api/v4/files/"+fileID, nil)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+c.token)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("download file: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		return fmt.Errorf("download file returned %d", resp.StatusCode)
	}

	f, err := os.Create(destPath)
	if err != nil {
		return fmt.Errorf("create file: %w", err)
	}
	defer f.Close()

	if _, err := io.Copy(f, resp.Body); err != nil {
		os.Remove(destPath)
		return fmt.Errorf("write file: %w", err)
	}
	return nil
}

// UploadFileStreaming uploads a file from disk without loading into RAM.
func (c *MMClient) UploadFileStreaming(ctx context.Context, filePath, channelID string) (*MMFileUploadResponse, error) {
	f, err := os.Open(filePath)
	if err != nil {
		return nil, fmt.Errorf("open file for upload: %w", err)
	}
	defer f.Close()

	// Uses io.Pipe to stream the file into the multipart body. This avoids loading
	// the entire file into memory — critical for the $5 VPS with ~35MB free RAM.
	pr, pw := io.Pipe()
	writer := multipart.NewWriter(pw)

	go func() {
		defer pw.Close()
		_ = writer.WriteField("channel_id", channelID)
		part, err := writer.CreateFormFile("files", filepath.Base(filePath))
		if err != nil {
			pw.CloseWithError(err)
			return
		}
		if _, err := io.Copy(part, f); err != nil {
			pw.CloseWithError(err)
			return
		}
		writer.Close()
	}()

	req, err := http.NewRequestWithContext(ctx, "POST", c.baseURL+"/api/v4/files", pr)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+c.token)
	req.Header.Set("Content-Type", writer.FormDataContentType())

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("upload file: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return nil, fmt.Errorf("upload file returned %d: %s", resp.StatusCode, string(body))
	}

	var result MMFileUploadResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("decode upload response: %w", err)
	}
	return &result, nil
}

// SaveReaction adds an emoji reaction to a post.
func (c *MMClient) SaveReaction(ctx context.Context, reaction *MMReaction) error {
	return c.doJSON(ctx, "POST", "/reactions", reaction, nil)
}

// DeleteReaction removes an emoji reaction from a post.
func (c *MMClient) DeleteReaction(ctx context.Context, userID, postID, emojiName string) error {
	path := fmt.Sprintf("/users/%s/posts/%s/reactions/%s", userID, postID, emojiName)
	return c.doJSON(ctx, "DELETE", path, nil, nil)
}

// PostTyping sends a typing indicator for a channel.
func (c *MMClient) PostTyping(ctx context.Context, userID, channelID string) error {
	body := map[string]string{"channel_id": channelID}
	return c.doJSON(ctx, "POST", "/users/"+userID+"/typing", body, nil)
}

// --- WebSocket ---

// MMWebSocketConn is a lightweight WebSocket connection for Mattermost events.
type MMWebSocketConn struct {
	conn      *websocket.Conn
	eventCh   chan *MMWebSocketEvent
	closeCh   chan struct{}
	closeOnce sync.Once
}

// ConnectWebSocket establishes a WebSocket connection for real-time events.
func (c *MMClient) ConnectWebSocket(ctx context.Context) (*MMWebSocketConn, error) {
	wsURL := strings.Replace(c.baseURL, "https://", "wss://", 1)
	wsURL = strings.Replace(wsURL, "http://", "ws://", 1)
	wsURL += "/api/v4/websocket"

	dialer := websocket.Dialer{
		HandshakeTimeout: 10 * time.Second,
	}
	conn, _, err := dialer.DialContext(ctx, wsURL, nil)
	if err != nil {
		return nil, fmt.Errorf("websocket dial: %w", err)
	}

	// Authenticate via challenge
	authMsg := map[string]any{
		"seq":    1,
		"action": "authentication_challenge",
		"data":   map[string]string{"token": c.token},
	}
	if err := conn.WriteJSON(authMsg); err != nil {
		conn.Close()
		return nil, fmt.Errorf("websocket auth: %w", err)
	}

	// ConnectWebSocket returns a buffered event channel (cap=100).
	// When the channel closes, readLoop has exited due to a network error.
	// Close() triggers conn.Close() which unblocks the blocking ReadJSON call
	// in readLoop — no mutex needed since gorilla/websocket's Close is safe
	// to call concurrently with ReadJSON.
	ws := &MMWebSocketConn{
		conn:    conn,
		eventCh: make(chan *MMWebSocketEvent, 100),
		closeCh: make(chan struct{}),
	}

	// Start reader goroutine
	go ws.readLoop()

	return ws, nil
}

// EventChannel returns the channel that receives WebSocket events.
func (ws *MMWebSocketConn) EventChannel() <-chan *MMWebSocketEvent {
	return ws.eventCh
}

// Close shuts down the WebSocket connection.
func (ws *MMWebSocketConn) Close() {
	ws.closeOnce.Do(func() {
		close(ws.closeCh)
		if ws.conn != nil {
			ws.conn.Close()
		}
	})
}

func (ws *MMWebSocketConn) readLoop() {
	defer close(ws.eventCh)
	for {
		select {
		case <-ws.closeCh:
			return
		default:
		}

		var evt MMWebSocketEvent
		err := ws.conn.ReadJSON(&evt)
		if err != nil {
			select {
			case <-ws.closeCh:
			default:
				slog.Debug("mattermost ws read error", "error", err)
			}
			return
		}

		// Skip non-event messages (e.g. auth response, hello)
		if evt.Event == "" {
			continue
		}

		select {
		case ws.eventCh <- &evt:
		case <-ws.closeCh:
			return
		}
	}
}
