package instagram

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strconv"
	"time"
)

const (
	graphAPIVersion = "v25.0"
	maxRetries      = 3
	maxRetryAfterSec = 60
)

// graphAPIBase is the Instagram Graph API root for Instagram Login for Business
// tokens (prefix "IGAA..."). Send API for Instagram DMs under this flow is:
//   POST https://graph.instagram.com/{version}/me/messages
// For the legacy Facebook Login for Business flow (Page-linked, "EAA..." tokens)
// the base is https://graph.facebook.com — but the modern default issued by
// Meta's Instagram Login flow is graph.instagram.com.
var graphAPIBase = "https://graph.instagram.com"

// GraphClient wraps the Instagram Graph API for messaging.
type GraphClient struct {
	httpClient      *http.Client
	userAccessToken string
	instagramID     string
}

// NewGraphClient creates a new GraphClient.
func NewGraphClient(userAccessToken, instagramID string) *GraphClient {
	return &GraphClient{
		httpClient:      &http.Client{Timeout: 15 * time.Second},
		userAccessToken: userAccessToken,
		instagramID:     instagramID,
	}
}

// VerifyToken checks the user access token via /me (works for IGAA tokens).
func (g *GraphClient) VerifyToken(ctx context.Context) error {
	_, err := g.doRequest(ctx, http.MethodGet, "/me?fields=id,username", nil)
	if err != nil {
		return fmt.Errorf("instagram: token verification failed: %w", err)
	}
	slog.Info("instagram: user token verified", "instagram_id", g.instagramID)
	return nil
}

// SendMessage sends a Direct Message to the given recipient.
func (g *GraphClient) SendMessage(ctx context.Context, recipientID, message string) (string, error) {
	body := map[string]any{
		"recipient": map[string]string{"id": recipientID},
		"message": map[string]string{"text": message},
	}
	data, err := g.doRequest(ctx, http.MethodPost, "/me/messages", body)
	if err != nil {
		return "", err
	}
	var result struct {
		MessageID string `json:"message_id"`
	}
	if err := json.Unmarshal(data, &result); err != nil {
		return "", fmt.Errorf("instagram: parse send message result: %w", err)
	}
	return result.MessageID, nil
}

// SendTypingOn sends a typing indicator to the recipient.
func (g *GraphClient) SendTypingOn(ctx context.Context, recipientID string) error {
	body := map[string]any{
		"recipient":     map[string]string{"id": recipientID},
		"sender_action": "typing_on",
	}
	_, err := g.doRequest(ctx, http.MethodPost, "/me/messages", body)
	return err
}

var graphBackoffBase = 1 * time.Second

// doRequest executes an Instagram Graph API call with retries.
func (g *GraphClient) doRequest(ctx context.Context, method, path string, body any) ([]byte, error) {
	apiURL := fmt.Sprintf("%s/%s%s", graphAPIBase, graphAPIVersion, path)

	for attempt := range maxRetries {
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
				return nil, fmt.Errorf("instagram: marshal request: %w", err)
			}
			reqBody = bytes.NewReader(b)
		}

		req, err := http.NewRequestWithContext(ctx, method, apiURL, reqBody)
		if err != nil {
			return nil, fmt.Errorf("instagram: build request: %w", err)
		}
		// Pass token via Authorization header.
		req.Header.Set("Authorization", "Bearer "+g.userAccessToken)
		if body != nil {
			req.Header.Set("Content-Type", "application/json")
		}

		resp, err := g.httpClient.Do(req)
		if err != nil {
			if attempt < maxRetries-1 {
				slog.Warn("instagram: api request error, retrying", "attempt", attempt+1, "err", err)
				continue
			}
			return nil, fmt.Errorf("instagram: api request: %w", err)
		}

		respBody, readErr := io.ReadAll(resp.Body)
		resp.Body.Close()
		if readErr != nil {
			return nil, fmt.Errorf("instagram: read response: %w", readErr)
		}

		// Retry on 5xx.
		if resp.StatusCode >= 500 && attempt < maxRetries-1 {
			slog.Warn("instagram: server error, retrying", "status", resp.StatusCode, "attempt", attempt+1)
			continue
		}

		// Parse error envelope.
		if resp.StatusCode >= 400 {
			var apiErr graphErrorBody
			if json.Unmarshal(respBody, &apiErr) == nil && apiErr.Error.Code != 0 {
				// 24h messaging window violation.
				// Instagram DM primarily reports this via subcode 2534014
				// ("human agent required" / "Message Not Sent"). Messenger
				// uses 551 / 2018109 — accept both to stay compatible in
				// case Meta normalizes codes across the shared webhook
				// surface. Misclassification here leaks into MarkDegraded
				// and causes unbounded pipeline retries.
				if apiErr.Error.Code == 551 ||
					apiErr.Error.Subcode == 2018109 ||
					apiErr.Error.Subcode == 2534014 {
					slog.Warn("instagram: 24h messaging window expired",
						"code", apiErr.Error.Code, "subcode", apiErr.Error.Subcode)
					return nil, &graphAPIError{code: apiErr.Error.Code, msg: apiErr.Error.Message}
				}
				// Rate limited: sleep and retry.
				if resp.StatusCode == 429 && attempt < maxRetries-1 {
					retryAfter := parseRetryAfter(resp)
					slog.Warn("instagram: rate limited", "retry_after", retryAfter)
					select {
					case <-ctx.Done():
						return nil, ctx.Err()
					case <-time.After(retryAfter):
					}
					continue
				}
				return nil, &graphAPIError{code: apiErr.Error.Code, msg: apiErr.Error.Message}
			}
			return nil, fmt.Errorf("instagram: http %d", resp.StatusCode)
		}

		return respBody, nil
	}

	return nil, fmt.Errorf("instagram: max retries exceeded")
}

// parseRetryAfter extracts the Retry-After header.
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

// graphErrorBody is the error envelope from Instagram Graph API.
type graphErrorBody struct {
	Error struct {
		Message string `json:"message"`
		Type    string `json:"type"`
		Code    int    `json:"code"`
		Subcode int    `json:"error_subcode"`
	} `json:"error"`
}

// graphAPIError is a structured error from Instagram Graph API.
type graphAPIError struct {
	code int
	msg  string
}

func (e *graphAPIError) Error() string {
	return fmt.Sprintf("instagram graph api error %d: %s", e.code, e.msg)
}

// IsAuthError returns true for token errors.
func IsAuthError(err error) bool {
	var ge *graphAPIError
	if !errors.As(err, &ge) {
		return false
	}
	return ge.code == 190 || ge.code == 102
}

// IsPermissionError returns true for permission errors.
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
