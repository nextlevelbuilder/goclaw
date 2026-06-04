package feishu

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"mime"
	"mime/multipart"
	"net/http"
	"sync"
	"time"
)

const (
	tokenExpiryBuffer = 3 * time.Minute
	tokenEndpoint     = "/open-apis/auth/v3/tenant_access_token/internal"
)

// LarkClient is a lightweight Feishu/Lark API client using net/http.
// Handles tenant_access_token auto-refresh and all REST API calls.
type LarkClient struct {
	baseURL    string
	appID      string
	appSecret  string
	httpClient *http.Client

	mu       sync.Mutex
	token    string
	tokenExp time.Time
}

// NewLarkClient creates a native Lark HTTP client.
func NewLarkClient(appID, appSecret, baseURL string) *LarkClient {
	return &LarkClient{
		baseURL:    baseURL,
		appID:      appID,
		appSecret:  appSecret,
		httpClient: &http.Client{Timeout: 30 * time.Second},
	}
}

// --- Token management ---

func (c *LarkClient) getToken(ctx context.Context) (string, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.token != "" && time.Now().Before(c.tokenExp) {
		return c.token, nil
	}

	body, _ := json.Marshal(map[string]string{
		"app_id":     c.appID,
		"app_secret": c.appSecret,
	})

	req, err := http.NewRequestWithContext(ctx, "POST", c.baseURL+tokenEndpoint, bytes.NewReader(body))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/json; charset=utf-8")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("lark token request: %w", err)
	}
	defer resp.Body.Close()

	var result struct {
		Code              int    `json:"code"`
		Msg               string `json:"msg"`
		TenantAccessToken string `json:"tenant_access_token"`
		Expire            int    `json:"expire"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return "", fmt.Errorf("lark token decode: %w", err)
	}
	if result.Code != 0 {
		return "", fmt.Errorf("lark token error: code=%d msg=%s", result.Code, result.Msg)
	}

	c.token = result.TenantAccessToken
	c.tokenExp = time.Now().Add(time.Duration(result.Expire)*time.Second - tokenExpiryBuffer)
	return c.token, nil
}

func (c *LarkClient) clearToken() {
	c.mu.Lock()
	c.token = ""
	c.tokenExp = time.Time{}
	c.mu.Unlock()
}

// isTokenError returns true if the error code indicates an expired/invalid token.
func isTokenError(code int) bool {
	return code == 99991663 || code == 99991664 || code == 99991671
}

// --- Generic API helpers ---

type apiResponse struct {
	Code int             `json:"code"`
	Msg  string          `json:"msg"`
	Data json.RawMessage `json:"data"`
	// Bot is only present on the legacy /open-apis/bot/v3/info endpoint,
	// which returns the bot object at the top level instead of inside "data".
	Bot json.RawMessage `json:"bot"`
}

// doJSON performs an authenticated JSON API call with auto token refresh.
func (c *LarkClient) doJSON(ctx context.Context, method, path string, body any) (*apiResponse, error) {
	resp, err := c.doJSONOnce(ctx, method, path, body)
	if err != nil {
		return nil, err
	}
	// Retry once on token error
	if isTokenError(resp.Code) {
		c.clearToken()
		return c.doJSONOnce(ctx, method, path, body)
	}
	return resp, nil
}

func (c *LarkClient) doJSONOnce(ctx context.Context, method, path string, body any) (*apiResponse, error) {
	token, err := c.getToken(ctx)
	if err != nil {
		return nil, err
	}

	var bodyReader io.Reader
	if body != nil {
		data, err := json.Marshal(body)
		if err != nil {
			return nil, fmt.Errorf("marshal body: %w", err)
		}
		bodyReader = bytes.NewReader(data)
	}

	req, err := http.NewRequestWithContext(ctx, method, c.baseURL+path, bodyReader)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	if body != nil {
		req.Header.Set("Content-Type", "application/json; charset=utf-8")
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("lark api %s %s: %w", method, path, err)
	}
	defer resp.Body.Close()

	var result apiResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("lark api decode: %w", err)
	}
	return &result, nil
}

// doDownload performs an authenticated GET that returns raw bytes.
func (c *LarkClient) doDownload(ctx context.Context, path string) ([]byte, string, error) {
	token, err := c.getToken(ctx)
	if err != nil {
		return nil, "", err
	}

	req, err := http.NewRequestWithContext(ctx, "GET", c.baseURL+path, nil)
	if err != nil {
		return nil, "", err
	}
	req.Header.Set("Authorization", "Bearer "+token)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, "", fmt.Errorf("lark download %s: %w", path, err)
	}
	defer resp.Body.Close()

	// Check for JSON error response
	ct := resp.Header.Get("Content-Type")
	if ct != "" {
		mt, _, _ := mime.ParseMediaType(ct)
		if mt == "application/json" {
			var errResp apiResponse
			if err := json.NewDecoder(resp.Body).Decode(&errResp); err == nil && errResp.Code != 0 {
				return nil, "", fmt.Errorf("lark download error: code=%d msg=%s", errResp.Code, errResp.Msg)
			}
		}
	}

	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, "", fmt.Errorf("lark read download: %w", err)
	}

	// Extract filename from Content-Disposition
	fileName := ""
	if cd := resp.Header.Get("Content-Disposition"); cd != "" {
		_, params, _ := mime.ParseMediaType(cd)
		fileName = params["filename"]
	}

	return data, fileName, nil
}

// doMultipart performs an authenticated multipart upload.
func (c *LarkClient) doMultipart(ctx context.Context, path string, fields map[string]string, fileField string, fileData io.Reader, fileName string) (*apiResponse, error) {
	token, err := c.getToken(ctx)
	if err != nil {
		return nil, err
	}

	var buf bytes.Buffer
	writer := multipart.NewWriter(&buf)

	for k, v := range fields {
		writer.WriteField(k, v)
	}

	if fileField != "" && fileData != nil {
		if fileName == "" {
			fileName = "upload"
		}
		part, err := writer.CreateFormFile(fileField, fileName)
		if err != nil {
			return nil, fmt.Errorf("create form file: %w", err)
		}
		if _, err := io.Copy(part, fileData); err != nil {
			return nil, fmt.Errorf("copy file data: %w", err)
		}
	}
	writer.Close()

	req, err := http.NewRequestWithContext(ctx, "POST", c.baseURL+path, &buf)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", writer.FormDataContentType())

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("lark upload %s: %w", path, err)
	}
	defer resp.Body.Close()

	var result apiResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("lark upload decode: %w", err)
	}
	return &result, nil
}
