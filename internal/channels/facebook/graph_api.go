package facebook

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"regexp"
	"strconv"
	"time"
)

const (
	graphAPIVersion = "v25.0"
	maxRetries      = 3
	// maxRetryAfterSec caps the Retry-After sleep to prevent goroutine stalls on abnormal values.
	maxRetryAfterSec = 60
)

// graphAPIBase is the Graph API root. Declared as a variable so tests can
// override it with an httptest.NewServer URL.
var graphAPIBase = "https://graph.facebook.com"

// fbIDPattern validates Facebook object IDs: numeric or "{num}_{num}" form (post IDs).
var fbIDPattern = regexp.MustCompile(`^\d+(_\d+)?$`)

// GraphClient wraps the Facebook Graph API for a single page instance.
type GraphClient struct {
	httpClient      *http.Client
	pageAccessToken string
	pageID          string
}

// NewGraphClient creates a new GraphClient for the given page.
func NewGraphClient(pageAccessToken, pageID string) *GraphClient {
	return &GraphClient{
		httpClient:      &http.Client{Timeout: 15 * time.Second},
		pageAccessToken: pageAccessToken,
		pageID:          pageID,
	}
}

// VerifyToken checks the page access token by calling GET /me.
func (g *GraphClient) VerifyToken(ctx context.Context) error {
	data, err := g.doRequest(ctx, http.MethodGet, "/me?fields=id,name", nil)
	if err != nil {
		return fmt.Errorf("facebook: token verification failed: %w", err)
	}
	var result struct {
		ID   string `json:"id"`
		Name string `json:"name"`
	}
	if err := json.Unmarshal(data, &result); err != nil {
		return fmt.Errorf("facebook: token verification parse error: %w", err)
	}
	slog.Info("facebook: page token verified", "page_id", result.ID, "name", result.Name)
	return nil
}

// SubscribeApp subscribes the app to the page's webhook events.
func (g *GraphClient) SubscribeApp(ctx context.Context) error {
	if err := validateFBID(g.pageID); err != nil {
		return fmt.Errorf("facebook: subscribe app: %w", err)
	}
	path := fmt.Sprintf("/%s/subscribed_apps?subscribed_fields=feed,messages", g.pageID)
	_, err := g.doRequest(ctx, http.MethodPost, path, nil)
	if err != nil {
		return fmt.Errorf("facebook: subscribe app failed: %w", err)
	}
	slog.Info("facebook: app subscribed to page webhooks", "page_id", g.pageID)
	return nil
}

// GetPost fetches a post by ID with message and story fields.
func (g *GraphClient) GetPost(ctx context.Context, postID string) (*GraphPost, error) {
	if err := validateFBID(postID); err != nil {
		return nil, fmt.Errorf("facebook: get post: %w", err)
	}
	path := fmt.Sprintf("/%s?fields=id,message,story,created_time", postID)
	data, err := g.doRequest(ctx, http.MethodGet, path, nil)
	if err != nil {
		return nil, err
	}
	var post GraphPost
	if err := json.Unmarshal(data, &post); err != nil {
		return nil, fmt.Errorf("facebook: parse post: %w", err)
	}
	return &post, nil
}

// GetComment fetches a single comment by ID.
func (g *GraphClient) GetComment(ctx context.Context, commentID string) (*GraphComment, error) {
	if err := validateFBID(commentID); err != nil {
		return nil, fmt.Errorf("facebook: get comment: %w", err)
	}
	path := fmt.Sprintf("/%s?fields=id,message,from,created_time", commentID)
	data, err := g.doRequest(ctx, http.MethodGet, path, nil)
	if err != nil {
		return nil, err
	}
	var c GraphComment
	if err := json.Unmarshal(data, &c); err != nil {
		return nil, fmt.Errorf("facebook: parse comment: %w", err)
	}
	return &c, nil
}

// GetCommentThread fetches up to limit comments under a parent comment.
func (g *GraphClient) GetCommentThread(ctx context.Context, parentCommentID string, limit int) ([]GraphComment, error) {
	if err := validateFBID(parentCommentID); err != nil {
		return nil, fmt.Errorf("facebook: get comment thread: %w", err)
	}
	if limit <= 0 {
		limit = 10
	}
	path := fmt.Sprintf("/%s/comments?fields=id,message,from,created_time&limit=%d", parentCommentID, limit)
	data, err := g.doRequest(ctx, http.MethodGet, path, nil)
	if err != nil {
		return nil, err
	}
	var resp GraphListResponse[GraphComment]
	if err := json.Unmarshal(data, &resp); err != nil {
		return nil, fmt.Errorf("facebook: parse comment thread: %w", err)
	}
	return resp.Data, nil
}

// ReplyToComment posts a reply to a comment. Returns the new comment ID.
func (g *GraphClient) ReplyToComment(ctx context.Context, commentID, message string) (string, error) {
	if err := validateFBID(commentID); err != nil {
		return "", fmt.Errorf("facebook: reply to comment: %w", err)
	}
	path := fmt.Sprintf("/%s/comments", commentID)
	body := map[string]string{"message": message}
	data, err := g.doRequest(ctx, http.MethodPost, path, body)
	if err != nil {
		return "", err
	}
	var result struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(data, &result); err != nil {
		return "", fmt.Errorf("facebook: parse reply result: %w", err)
	}
	return result.ID, nil
}

// SendMessage sends a Messenger message to the given recipient. Returns message ID.
func (g *GraphClient) SendMessage(ctx context.Context, recipientID, message string) (string, error) {
	body := map[string]any{
		"recipient": map[string]string{"id": recipientID},
		"message":   map[string]string{"text": message},
	}
	data, err := g.doRequest(ctx, http.MethodPost, "/me/messages", body)
	if err != nil {
		return "", err
	}
	var result struct {
		MessageID string `json:"message_id"`
	}
	if err := json.Unmarshal(data, &result); err != nil {
		return "", fmt.Errorf("facebook: parse send message result: %w", err)
	}
	return result.MessageID, nil
}

// SendTypingOn sends a typing indicator to the recipient (auto-off after 3s).
func (g *GraphClient) SendTypingOn(ctx context.Context, recipientID string) error {
	body := map[string]any{
		"recipient":     map[string]string{"id": recipientID},
		"sender_action": "typing_on",
	}
	_, err := g.doRequest(ctx, http.MethodPost, "/me/messages", body)
	return err
}

// graphBackoffBase is the base unit for exponential retry backoff in doRequest.
// Production default = 1s, giving 1s, 2s, 4s... per attempt.
// Tests override to 1ms via newFakeGraph so retry tests don't burn 6s of real
// wall-clock time. Production behavior is unchanged.
var graphBackoffBase = 1 * time.Second

// doRequest executes a Graph API call with retries on transient errors.
// The page access token is passed via Authorization header (never in the URL).
func (g *GraphClient) doRequest(ctx context.Context, method, path string, body any) ([]byte, error) {
	apiURL := fmt.Sprintf("%s/%s%s", graphAPIBase, graphAPIVersion, path)

	for attempt := 0; attempt < maxRetries; attempt++ {
		if attempt > 0 {
			backoff := time.Duration(1<<uint(attempt-1)) * graphBackoffBase
			select {
			case <-ctx.Done():
				return nil, ctx.Err()
			case <-time.After(backoff):
			}
		}

		var reqBody io.Reader
		if body != nil {
			b, err := json.Marshal(body)
			if err != nil {
				return nil, fmt.Errorf("facebook: marshal request: %w", err)
			}
			reqBody = bytes.NewReader(b)
		}

		req, err := http.NewRequestWithContext(ctx, method, apiURL, reqBody)
		if err != nil {
			return nil, fmt.Errorf("facebook: build request: %w", err)
		}
		// Pass token via header to avoid URL logging exposure.
		req.Header.Set("Authorization", "Bearer "+g.pageAccessToken)
		if body != nil {
			req.Header.Set("Content-Type", "application/json")
		}

		resp, err := g.httpClient.Do(req)
		if err != nil {
			if attempt < maxRetries-1 {
				slog.Warn("facebook: api request error, retrying", "attempt", attempt+1, "err", err)
				continue
			}
			return nil, fmt.Errorf("facebook: api request: %w", err)
		}

		respBody, readErr := io.ReadAll(resp.Body)
		resp.Body.Close()
		if readErr != nil {
			return nil, fmt.Errorf("facebook: read response: %w", readErr)
		}

		// Proactive rate limit monitoring.
		g.logRateLimit(resp)

		// Retry on 5xx.
		if resp.StatusCode >= 500 && attempt < maxRetries-1 {
			slog.Warn("facebook: server error, retrying", "status", resp.StatusCode, "attempt", attempt+1)
			continue
		}

		// Parse Graph API error envelope.
		if resp.StatusCode >= 400 {
			var apiErr graphErrorBody
			if json.Unmarshal(respBody, &apiErr) == nil && apiErr.Error.Code != 0 {
				// 24h messaging window violation — not retryable.
				if apiErr.Error.Code == 551 || apiErr.Error.Subcode == 2018109 {
					slog.Warn("facebook: 24h messaging window expired", "code", apiErr.Error.Code)
					return nil, &graphAPIError{code: apiErr.Error.Code, msg: apiErr.Error.Message}
				}
				// Rate limited: sleep and retry (capped).
				if resp.StatusCode == 429 && attempt < maxRetries-1 {
					retryAfter := parseRetryAfter(resp)
					slog.Warn("facebook: rate limited", "retry_after", retryAfter)
					select {
					case <-ctx.Done():
						return nil, ctx.Err()
					case <-time.After(retryAfter):
					}
					continue
				}
				return nil, &graphAPIError{code: apiErr.Error.Code, msg: apiErr.Error.Message}
			}
			return nil, fmt.Errorf("facebook: http %d", resp.StatusCode)
		}

		return respBody, nil
	}

	// All attempts exhausted (only reachable when every iteration took the continue path).
	return nil, fmt.Errorf("facebook: max retries exceeded")
}

// logRateLimit parses the X-Business-Use-Case-Usage header and warns when approaching limits.
func (g *GraphClient) logRateLimit(resp *http.Response) {
	usage := resp.Header.Get("X-Business-Use-Case-Usage")
	if usage == "" {
		return
	}
	var parsed map[string][]struct {
		CallCount int `json:"call_count"`
	}
	if err := json.Unmarshal([]byte(usage), &parsed); err != nil {
		return
	}
	for _, entries := range parsed {
		for _, e := range entries {
			if e.CallCount >= 95 {
				slog.Warn("facebook: rate limit critical", "call_count_pct", e.CallCount, "page_id", g.pageID)
			} else if e.CallCount >= 80 {
				slog.Warn("facebook: rate limit warning", "call_count_pct", e.CallCount, "page_id", g.pageID)
			}
		}
	}
}

// parseRetryAfter extracts the Retry-After header, capped at maxRetryAfterSec.
func parseRetryAfter(resp *http.Response) time.Duration {
	val := resp.Header.Get("Retry-After")
	if val == "" {
		return 5 * time.Second
	}
	secs, err := strconv.Atoi(val)
	if err != nil || secs <= 0 {
		return 5 * time.Second
	}
	if secs > maxRetryAfterSec {
		secs = maxRetryAfterSec
	}
	return time.Duration(secs) * time.Second
}

// validateFBID returns an error if id is not a valid Facebook object ID format.
// Facebook IDs are numeric strings, or "{num}_{num}" for post IDs.
func validateFBID(id string) error {
	if id == "" {
		return fmt.Errorf("empty facebook ID")
	}
	if !fbIDPattern.MatchString(id) {
		return fmt.Errorf("invalid facebook ID format: %q", id)
	}
	return nil
}

// graphAPIError is a structured error from the Facebook Graph API.
type graphAPIError struct {
	code int
	msg  string
}

func (e *graphAPIError) Error() string {
	return fmt.Sprintf("facebook graph api error %d: %s", e.code, e.msg)
}

// IsAuthError returns true when the error is an expired or invalid token.
func IsAuthError(err error) bool {
	var ge *graphAPIError
	if !errors.As(err, &ge) {
		return false
	}
	return ge.code == 190 || ge.code == 102
}

// IsPermissionError returns true for permission-denied errors.
func IsPermissionError(err error) bool {
	var ge *graphAPIError
	if !errors.As(err, &ge) {
		return false
	}
	return ge.code == 10 || ge.code == 200
}

// IsRateLimitError returns true for rate limit errors.
func IsRateLimitError(err error) bool {
	var ge *graphAPIError
	if !errors.As(err, &ge) {
		return false
	}
	return ge.code == 4 || ge.code == 17 || ge.code == 32 || ge.code == 613
}

