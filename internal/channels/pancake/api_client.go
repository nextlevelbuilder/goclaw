package pancake

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"mime/multipart"
	"net/http"
	"path/filepath"
	"strings"
	"time"
)

const (
	publicAPIBase = "https://pages.fm/api/public_api/v2" // page-level APIs
	userAPIBase   = "https://pages.fm/api/v1"            // user-level APIs (list pages, etc.)
	httpTimeout   = 30 * time.Second
)

// APIClient wraps the Pancake REST API for a single page instance.
type APIClient struct {
	pageV1BaseURL   string
	pageV2BaseURL   string
	userBaseURL     string
	pageAccessToken string
	apiKey          string
	pageID          string
	httpClient      *http.Client
}

// NewAPIClient creates a new Pancake APIClient for the given page.
func NewAPIClient(apiKey, pageAccessToken, pageID string) *APIClient {
	return &APIClient{
		pageV1BaseURL:   "https://pages.fm/api/public_api/v1",
		pageV2BaseURL:   publicAPIBase,
		userBaseURL:     userAPIBase,
		pageAccessToken: pageAccessToken,
		apiKey:          apiKey,
		pageID:          pageID,
		httpClient:      &http.Client{Timeout: httpTimeout},
	}
}

// VerifyToken validates the page_access_token via a lightweight API call.
func (c *APIClient) VerifyToken(ctx context.Context) error {
	url := fmt.Sprintf("%s/pages/%s/conversations?limit=1", c.pageV2BaseURL, c.pageID)
	if err := c.doRequest(ctx, http.MethodGet, url, nil); err != nil {
		return fmt.Errorf("pancake: token verification failed: %w", err)
	}
	slog.Info("pancake: page token verified", "page_id", c.pageID)
	return nil
}

// GetPage fetches page metadata including platform (facebook/zalo/instagram/tiktok/whatsapp/line).
func (c *APIClient) GetPage(ctx context.Context) (*PageInfo, error) {
	url := fmt.Sprintf("%s/pages", c.userBaseURL)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, fmt.Errorf("pancake: build get-pages request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+c.apiKey)
	req.Header.Set("Content-Type", "application/json")

	res, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("pancake: get pages request failed: %w", err)
	}
	defer res.Body.Close()

	body, err := io.ReadAll(res.Body)
	if err != nil {
		return nil, fmt.Errorf("pancake: read get-pages response: %w", err)
	}

	var result struct {
		Data []PageInfo `json:"data"`
	}
	if err := json.Unmarshal(body, &result); err != nil {
		return nil, fmt.Errorf("pancake: parse get-pages response: %w", err)
	}

	for i := range result.Data {
		if result.Data[i].ID == c.pageID {
			return &result.Data[i], nil
		}
	}

	// Page not found in list — return minimal info without platform
	slog.Warn("pancake: page not found in pages list, platform unknown", "page_id", c.pageID)
	return &PageInfo{ID: c.pageID}, nil
}

// SendMessage sends a text message to a conversation.
func (c *APIClient) SendMessage(ctx context.Context, conversationID, content string) error {
	body, _ := json.Marshal(SendMessageRequest{
		Action:  "reply_inbox",
		Message: content,
	})
	url := fmt.Sprintf("%s/pages/%s/conversations/%s/messages", c.pageV1BaseURL, c.pageID, conversationID)
	if err := c.doRequest(ctx, http.MethodPost, url, bytes.NewReader(body)); err != nil {
		return fmt.Errorf("pancake: send message: %w", err)
	}
	return nil
}

// SendAttachmentMessage sends one or more uploaded content IDs to a conversation.
func (c *APIClient) SendAttachmentMessage(ctx context.Context, conversationID string, contentIDs []string) error {
	body, _ := json.Marshal(SendMessageRequest{
		Action:     "reply_inbox",
		ContentIDs: contentIDs,
	})
	url := fmt.Sprintf("%s/pages/%s/conversations/%s/messages", c.pageV1BaseURL, c.pageID, conversationID)
	if err := c.doRequest(ctx, http.MethodPost, url, bytes.NewReader(body)); err != nil {
		return fmt.Errorf("pancake: send attachment message: %w", err)
	}
	return nil
}

// UploadMedia uploads a file via multipart/form-data and returns the attachment ID.
func (c *APIClient) UploadMedia(ctx context.Context, filename string, data io.Reader, contentType string) (string, error) {
	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)

	fw, err := mw.CreateFormFile("file", filepath.Base(filename))
	if err != nil {
		return "", fmt.Errorf("pancake: create form file: %w", err)
	}
	if _, err := io.Copy(fw, data); err != nil {
		return "", fmt.Errorf("pancake: copy file data: %w", err)
	}
	mw.Close()

	url := fmt.Sprintf("%s/pages/%s/upload_contents", c.pageV1BaseURL, c.pageID)
	req, err := c.newPageRequest(ctx, http.MethodPost, url, &buf)
	if err != nil {
		return "", fmt.Errorf("pancake: build upload request: %w", err)
	}
	req.Header.Set("Content-Type", mw.FormDataContentType())

	res, err := c.httpClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("pancake: upload request failed: %w", err)
	}
	defer res.Body.Close()

	respBody, err := io.ReadAll(res.Body)
	if err != nil {
		return "", fmt.Errorf("pancake: read upload response: %w", err)
	}

	var uploadResp UploadResponse
	if err := json.Unmarshal(respBody, &uploadResp); err != nil {
		return "", fmt.Errorf("pancake: parse upload response: %w", err)
	}

	if uploadResp.ID == "" {
		return "", fmt.Errorf("pancake: upload response missing attachment ID")
	}

	return uploadResp.ID, nil
}

// ReplyComment sends a reply to a comment on a post (action: "reply_comment").
// messageID is the ID of the comment being replied to — required by Pancake API.
func (c *APIClient) ReplyComment(ctx context.Context, conversationID, messageID, content string) error {
	body, _ := json.Marshal(SendMessageRequest{
		Action:    "reply_comment",
		Message:   content,
		MessageID: messageID,
	})
	url := fmt.Sprintf("%s/pages/%s/conversations/%s/messages", c.pageV1BaseURL, c.pageID, conversationID)
	if err := c.doRequest(ctx, http.MethodPost, url, bytes.NewReader(body)); err != nil {
		return fmt.Errorf("pancake: reply comment: %w", err)
	}
	return nil
}

// PrivateReply sends a one-time DM to a commenter (first-inbox, action: "private_reply").
func (c *APIClient) PrivateReply(ctx context.Context, conversationID, content string) error {
	body, _ := json.Marshal(SendMessageRequest{
		Action:  "private_reply",
		Message: content,
	})
	url := fmt.Sprintf("%s/pages/%s/conversations/%s/messages", c.pageV1BaseURL, c.pageID, conversationID)
	if err := c.doRequest(ctx, http.MethodPost, url, bytes.NewReader(body)); err != nil {
		return fmt.Errorf("pancake: private reply: %w", err)
	}
	return nil
}

// GetPosts fetches recent posts for the page (used by PostFetcher for comment context enrichment).
// Bypasses doRequest because it needs to parse a JSON response body (doRequest discards the body).
// Connection reuse is preserved: defer res.Body.Close() + io.ReadAll drain the body fully.
func (c *APIClient) GetPosts(ctx context.Context, limit int) ([]PancakePost, error) {
	if limit <= 0 {
		limit = 20
	}
	url := fmt.Sprintf("%s/pages/%s/posts?limit=%d", c.pageV2BaseURL, c.pageID, limit)
	req, err := c.newPageRequest(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, fmt.Errorf("pancake: build get-posts request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	res, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("pancake: get posts request failed: %w", err)
	}
	defer res.Body.Close()

	respBody, err := io.ReadAll(res.Body)
	if err != nil {
		return nil, fmt.Errorf("pancake: read get-posts response: %w", err)
	}
	if res.StatusCode >= 400 {
		return nil, fmt.Errorf("pancake: get posts HTTP %d", res.StatusCode)
	}

	var result struct {
		Data []PancakePost `json:"data"`
	}
	if err := json.Unmarshal(respBody, &result); err != nil {
		return nil, fmt.Errorf("pancake: parse get-posts response: %w", err)
	}
	return result.Data, nil
}

// doRequest executes an authenticated HTTP request using the page_access_token.
// Always drains and closes the response body to enable connection reuse.
func (c *APIClient) doRequest(ctx context.Context, method, url string, body io.Reader) error {
	req, err := c.newPageRequest(ctx, method, url, body)
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")

	res, err := c.httpClient.Do(req)
	if err != nil {
		return err
	}
	defer res.Body.Close()

	// Always read the full body to allow HTTP connection reuse.
	respBody, _ := io.ReadAll(res.Body)

	if res.StatusCode >= 400 {
		var apiErr apiError
		if jsonErr := json.Unmarshal(respBody, &apiErr); jsonErr == nil && apiErr.Message != "" {
			return &apiErr
		}
		return fmt.Errorf("pancake: HTTP %d", res.StatusCode)
	}

	// Some Pancake endpoints return HTTP 200 with a JSON body carrying success=false.
	// Treat these as application-level send failures instead of silent success.
	var appResp struct {
		Success *bool  `json:"success,omitempty"`
		Message string `json:"message,omitempty"`
	}
	if err := json.Unmarshal(respBody, &appResp); err == nil && appResp.Success != nil && !*appResp.Success {
		if appResp.Message != "" {
			return fmt.Errorf("pancake: %s", appResp.Message)
		}
		return fmt.Errorf("pancake: request reported success=false")
	}

	return nil
}

func (c *APIClient) newPageRequest(ctx context.Context, method, rawURL string, body io.Reader) (*http.Request, error) {
	req, err := http.NewRequestWithContext(ctx, method, rawURL, body)
	if err != nil {
		return nil, err
	}

	query := req.URL.Query()
	query.Set("page_access_token", c.pageAccessToken)
	req.URL.RawQuery = query.Encode()

	// Keep the header for compatibility; official docs require the query token.
	req.Header.Set("Authorization", "Bearer "+c.pageAccessToken)
	return req, nil
}

// ReactComment toggles the page's like on a comment via Pancake user API:
// POST {userAPI}/pages/{pageID}/conversations/{convID}/messages/{msgID}/likes
// with multipart body (action=like_toggle, user_likes=false). Server flips the
// current state, so call once per fresh comment (dedup prevents double-toggle).
func (c *APIClient) ReactComment(ctx context.Context, conversationID, messageID string) error {
	if conversationID == "" || messageID == "" {
		return fmt.Errorf("pancake: react-comment requires conversation_id and message_id")
	}
	if strings.ContainsAny(conversationID, "/?#\\") || strings.ContainsAny(messageID, "/?#\\") {
		return fmt.Errorf("pancake: invalid character in conversation_id or message_id")
	}

	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	_ = mw.WriteField("action", "like_toggle")
	_ = mw.WriteField("user_likes", "false")
	mw.Close()

	url := fmt.Sprintf("%s/pages/%s/conversations/%s/messages/%s/likes",
		c.userBaseURL, c.pageID, conversationID, messageID)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, &buf)
	if err != nil {
		return fmt.Errorf("pancake: build react-comment request: %w", err)
	}
	q := req.URL.Query()
	q.Set("access_token", c.apiKey)
	req.URL.RawQuery = q.Encode()
	req.Header.Set("Content-Type", mw.FormDataContentType())

	res, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("pancake: react-comment request failed: %w", err)
	}
	defer res.Body.Close()

	body, _ := io.ReadAll(res.Body)
	if res.StatusCode >= 400 {
		snippet := string(body)
		if len(snippet) > 300 {
			snippet = snippet[:300]
		}
		return fmt.Errorf("pancake: react-comment HTTP %d: %s", res.StatusCode, snippet)
	}
	return nil
}

// isAuthError checks if an error is an authentication/authorization failure.
// Uses errors.As to handle wrapped errors consistently with Facebook channel pattern.
func isAuthError(err error) bool {
	var ae *apiError
	if !errors.As(err, &ae) {
		return false
	}
	return ae.Code == 401 || ae.Code == 403 || ae.Code == 4001 || ae.Code == 4003
}

// isRateLimitError checks if an error is a rate limit response.
func isRateLimitError(err error) bool {
	var ae *apiError
	if !errors.As(err, &ae) {
		return false
	}
	return ae.Code == 429 || ae.Code == 4029
}
