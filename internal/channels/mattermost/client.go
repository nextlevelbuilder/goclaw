package mattermost

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/url"
	"strings"
	"time"
)

const apiTimeout = 30 * time.Second

// apiClient is a minimal Mattermost REST API v4 client. It covers only the
// endpoints the channel needs, so the gateway does not pull in the full
// Mattermost server module and its dependency tree.
type apiClient struct {
	baseURL string // normalized server URL without trailing slash
	token   string // bot access token or personal access token
	http    *http.Client
}

// apiError is the Mattermost AppError payload returned on non-2xx responses.
type apiError struct {
	StatusCode int    `json:"status_code"`
	ID         string `json:"id"`
	Message    string `json:"message"`
}

func (e *apiError) Error() string {
	if e.Message != "" {
		return fmt.Sprintf("mattermost api: HTTP %d: %s (%s)", e.StatusCode, e.Message, e.ID)
	}
	return fmt.Sprintf("mattermost api: HTTP %d", e.StatusCode)
}

// isAuthError reports whether err is a permanent authentication failure
// (revoked token, deactivated bot) that reconnecting will not fix.
func isAuthError(err error) bool {
	var ae *apiError
	if errors.As(err, &ae) {
		return ae.StatusCode == http.StatusUnauthorized || ae.StatusCode == http.StatusForbidden
	}
	return false
}

type mmUser struct {
	ID        string `json:"id"`
	Username  string `json:"username"`
	FirstName string `json:"first_name"`
	LastName  string `json:"last_name"`
	Nickname  string `json:"nickname"`
	IsBot     bool   `json:"is_bot"`
}

// displayName picks the most human-friendly name Mattermost exposes.
func (u *mmUser) displayName() string {
	if u.Nickname != "" {
		return u.Nickname
	}
	if full := strings.TrimSpace(u.FirstName + " " + u.LastName); full != "" {
		return full
	}
	return u.Username
}

type mmChannel struct {
	ID          string `json:"id"`
	Type        string `json:"type"` // O public, P private, D direct, G group DM
	DisplayName string `json:"display_name"`
}

type mmFileInfo struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	Extension string `json:"extension"`
	Size      int64  `json:"size"`
	MimeType  string `json:"mime_type"`
}

type mmPostMetadata struct {
	Files []mmFileInfo `json:"files,omitempty"`
}

type mmPost struct {
	ID        string          `json:"id,omitempty"`
	CreateAt  int64           `json:"create_at,omitempty"`
	UserID    string          `json:"user_id,omitempty"`
	ChannelID string          `json:"channel_id"`
	RootID    string          `json:"root_id,omitempty"`
	Message   string          `json:"message"`
	Type      string          `json:"type,omitempty"`
	Props     map[string]any  `json:"props,omitempty"`
	FileIDs   []string        `json:"file_ids,omitempty"`
	Metadata  *mmPostMetadata `json:"metadata,omitempty"`
}

// fromBot reports whether the post was created by a bot or integration.
func (p *mmPost) fromBot() bool {
	switch v := p.Props["from_bot"].(type) {
	case bool:
		return v
	case string:
		return v == "true"
	}
	return false
}

// normalizeServerURL validates the configured server URL and strips the
// trailing slash. Only http and https are accepted.
func normalizeServerURL(raw string) (string, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "", fmt.Errorf("mattermost server_url is required")
	}
	u, err := url.Parse(raw)
	if err != nil {
		return "", fmt.Errorf("mattermost server_url is invalid: %w", err)
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return "", fmt.Errorf("mattermost server_url must use http or https")
	}
	if u.Host == "" {
		return "", fmt.Errorf("mattermost server_url must include a host")
	}
	if u.RawQuery != "" || u.Fragment != "" {
		return "", fmt.Errorf("mattermost server_url must not contain a query or fragment")
	}
	u.User = nil
	return strings.TrimRight(u.String(), "/"), nil
}

func newAPIClient(serverURL, token string) *apiClient {
	return &apiClient{
		baseURL: serverURL,
		token:   token,
		http:    newHTTPClient(apiTimeout),
	}
}

// websocketURL derives the API v4 websocket endpoint from the server URL.
func (c *apiClient) websocketURL() string {
	switch {
	case strings.HasPrefix(c.baseURL, "https://"):
		return "wss://" + strings.TrimPrefix(c.baseURL, "https://") + "/api/v4/websocket"
	default:
		return "ws://" + strings.TrimPrefix(c.baseURL, "http://") + "/api/v4/websocket"
	}
}

func (c *apiClient) newRequest(ctx context.Context, method, path string, body io.Reader) (*http.Request, error) {
	req, err := http.NewRequestWithContext(ctx, method, c.baseURL+"/api/v4"+path, body)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+c.token)
	req.Header.Set("Accept", "application/json")
	return req, nil
}

// doJSON sends an optional JSON body and decodes an optional JSON response.
func (c *apiClient) doJSON(ctx context.Context, method, path string, in, out any) error {
	var body io.Reader
	if in != nil {
		data, err := json.Marshal(in)
		if err != nil {
			return fmt.Errorf("encode request: %w", err)
		}
		body = bytes.NewReader(data)
	}
	req, err := c.newRequest(ctx, method, path, body)
	if err != nil {
		return err
	}
	if in != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	return c.send(req, out)
}

func (c *apiClient) send(req *http.Request, out any) error {
	resp, err := c.http.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return decodeAPIError(resp)
	}
	if out == nil {
		_, _ = io.Copy(io.Discard, resp.Body)
		return nil
	}
	if err := json.NewDecoder(resp.Body).Decode(out); err != nil {
		return fmt.Errorf("decode response: %w", err)
	}
	return nil
}

func decodeAPIError(resp *http.Response) error {
	ae := &apiError{StatusCode: resp.StatusCode}
	data, _ := io.ReadAll(io.LimitReader(resp.Body, 64*1024))
	_ = json.Unmarshal(data, ae)
	ae.StatusCode = resp.StatusCode
	return ae
}

func (c *apiClient) getMe(ctx context.Context) (*mmUser, error) {
	var u mmUser
	if err := c.doJSON(ctx, http.MethodGet, "/users/me", nil, &u); err != nil {
		return nil, err
	}
	return &u, nil
}

func (c *apiClient) getUser(ctx context.Context, userID string) (*mmUser, error) {
	var u mmUser
	if err := c.doJSON(ctx, http.MethodGet, "/users/"+url.PathEscape(userID), nil, &u); err != nil {
		return nil, err
	}
	return &u, nil
}

func (c *apiClient) getPost(ctx context.Context, postID string) (*mmPost, error) {
	var p mmPost
	if err := c.doJSON(ctx, http.MethodGet, "/posts/"+url.PathEscape(postID), nil, &p); err != nil {
		return nil, err
	}
	return &p, nil
}

func (c *apiClient) getChannel(ctx context.Context, channelID string) (*mmChannel, error) {
	var ch mmChannel
	if err := c.doJSON(ctx, http.MethodGet, "/channels/"+url.PathEscape(channelID), nil, &ch); err != nil {
		return nil, err
	}
	return &ch, nil
}

func (c *apiClient) getFileInfo(ctx context.Context, fileID string) (*mmFileInfo, error) {
	var f mmFileInfo
	if err := c.doJSON(ctx, http.MethodGet, "/files/"+url.PathEscape(fileID)+"/info", nil, &f); err != nil {
		return nil, err
	}
	return &f, nil
}

func (c *apiClient) createPost(ctx context.Context, post *mmPost) (*mmPost, error) {
	var created mmPost
	if err := c.doJSON(ctx, http.MethodPost, "/posts", post, &created); err != nil {
		return nil, err
	}
	return &created, nil
}

// patchPost replaces the message of a post and, when props is non-nil, its
// props (e.g. message attachments).
func (c *apiClient) patchPost(ctx context.Context, postID, message string, props map[string]any) error {
	patch := map[string]any{"message": message}
	if props != nil {
		patch["props"] = props
	}
	return c.doJSON(ctx, http.MethodPut, "/posts/"+url.PathEscape(postID)+"/patch", patch, nil)
}

func (c *apiClient) deletePost(ctx context.Context, postID string) error {
	return c.doJSON(ctx, http.MethodDelete, "/posts/"+url.PathEscape(postID), nil, nil)
}

// userTyping shows "<bot> is typing…" in a channel, or in a thread when
// parentID is set. Clients hide it after a few seconds or on the next post.
func (c *apiClient) userTyping(ctx context.Context, userID, channelID, parentID string) error {
	body := map[string]string{"channel_id": channelID}
	if parentID != "" {
		body["parent_id"] = parentID
	}
	return c.doJSON(ctx, http.MethodPost, "/users/"+url.PathEscape(userID)+"/typing", body, nil)
}

func (c *apiClient) addReaction(ctx context.Context, userID, postID, emoji string) error {
	reaction := map[string]string{"user_id": userID, "post_id": postID, "emoji_name": emoji}
	return c.doJSON(ctx, http.MethodPost, "/reactions", reaction, nil)
}

func (c *apiClient) removeReaction(ctx context.Context, userID, postID, emoji string) error {
	path := "/users/" + url.PathEscape(userID) + "/posts/" + url.PathEscape(postID) + "/reactions/" + url.PathEscape(emoji)
	return c.doJSON(ctx, http.MethodDelete, path, nil, nil)
}

// downloadFile streams a file attachment into w, reading at most maxBytes.
// Returns an error when the file is larger than maxBytes instead of silently
// truncating it.
func (c *apiClient) downloadFile(ctx context.Context, fileID string, w io.Writer, maxBytes int64) error {
	req, err := c.newRequest(ctx, http.MethodGet, "/files/"+url.PathEscape(fileID), nil)
	if err != nil {
		return err
	}
	req.Header.Set("Accept", "*/*")

	resp, err := c.http.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return decodeAPIError(resp)
	}
	n, err := io.Copy(w, io.LimitReader(resp.Body, maxBytes+1))
	if err != nil {
		return err
	}
	if n > maxBytes {
		return fmt.Errorf("file exceeds %d bytes", maxBytes)
	}
	return nil
}

type fileUploadResponse struct {
	FileInfos []mmFileInfo `json:"file_infos"`
}

// uploadFile uploads one file to a channel and returns its file ID, which is
// then attached to a post via file_ids. The multipart body is streamed
// through a pipe, so the file is never buffered in memory.
func (c *apiClient) uploadFile(ctx context.Context, channelID, fileName string, data io.Reader) (string, error) {
	pr, pw := io.Pipe()
	mw := multipart.NewWriter(pw)

	// The writer goroutine is joined before returning: the caller closes data
	// right after uploadFile, and it must not be read past that point.
	written := make(chan struct{})
	go func() {
		defer close(written)
		pw.CloseWithError(writeUploadForm(mw, channelID, fileName, data))
	}()
	defer func() {
		_ = pr.Close() // unblocks the writer if the request ended early
		<-written
	}()

	req, err := c.newRequest(ctx, http.MethodPost, "/files", pr)
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", mw.FormDataContentType())

	var out fileUploadResponse
	if err := c.send(req, &out); err != nil {
		return "", err
	}
	if len(out.FileInfos) == 0 || out.FileInfos[0].ID == "" {
		return "", fmt.Errorf("upload returned no file info")
	}
	return out.FileInfos[0].ID, nil
}

// writeUploadForm writes the channel_id field and the file part.
func writeUploadForm(mw *multipart.Writer, channelID, fileName string, data io.Reader) error {
	if err := mw.WriteField("channel_id", channelID); err != nil {
		return err
	}
	part, err := mw.CreateFormFile("files", fileName)
	if err != nil {
		return err
	}
	if _, err := io.Copy(part, data); err != nil {
		return err
	}
	return mw.Close()
}
