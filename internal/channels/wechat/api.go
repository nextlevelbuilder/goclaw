package wechat

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"strings"
	"time"
)

const (
	channelVersion = "goclaw-wechat/1.0.0"

	defaultLongPollTimeoutMs = 35000
	defaultAPITimeoutMs      = 15000
	defaultConfigTimeoutMs   = 10000
)

// APIClient communicates with the Weixin iLink Bot API.
type APIClient struct {
	BaseURL    string
	Token      string
	HTTPClient *http.Client
}

// NewAPIClient creates a new API client.
func NewAPIClient(baseURL, token string) *APIClient {
	return &APIClient{
		BaseURL: baseURL,
		Token:   token,
		HTTPClient: &http.Client{
			Timeout: 60 * time.Second,
		},
	}
}

func buildBaseInfo() *BaseInfo {
	return &BaseInfo{ChannelVersion: channelVersion}
}

func ensureTrailingSlash(u string) string {
	if strings.HasSuffix(u, "/") {
		return u
	}
	return u + "/"
}

// randomWechatUin generates X-WECHAT-UIN header: random uint32 -> decimal -> base64.
func randomWechatUin() string {
	b := make([]byte, 4)
	_, _ = rand.Read(b)
	uint32Val := binary.BigEndian.Uint32(b)
	decimal := fmt.Sprintf("%d", uint32Val)
	return base64.StdEncoding.EncodeToString([]byte(decimal))
}

func (c *APIClient) buildHeaders(bodyLen int) http.Header {
	h := http.Header{}
	h.Set("Content-Type", "application/json")
	h.Set("AuthorizationType", "ilink_bot_token")
	h.Set("Content-Length", fmt.Sprintf("%d", bodyLen))
	h.Set("X-WECHAT-UIN", randomWechatUin())
	if c.Token != "" {
		h.Set("Authorization", "Bearer "+strings.TrimSpace(c.Token))
	}
	return h
}

// apiPost sends a POST request and returns the response body.
func (c *APIClient) apiPost(ctx context.Context, endpoint string, body []byte, timeoutMs int) ([]byte, error) {
	base := ensureTrailingSlash(c.BaseURL)
	u, err := url.JoinPath(base, endpoint)
	if err != nil {
		return nil, fmt.Errorf("build URL: %w", err)
	}

	timeout := time.Duration(timeoutMs) * time.Millisecond
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, u, bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}
	for k, vs := range c.buildHeaders(len(body)) {
		for _, v := range vs {
			req.Header.Set(k, v)
		}
	}

	resp, err := c.HTTPClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("API %s %d: %s", endpoint, resp.StatusCode, string(respBody))
	}
	return respBody, nil
}

// apiGet sends a GET request and returns the response body.
func (c *APIClient) apiGet(ctx context.Context, reqPath string, query url.Values, timeoutMs int) ([]byte, error) {
	base, err := url.Parse(ensureTrailingSlash(c.BaseURL))
	if err != nil {
		return nil, fmt.Errorf("parse base URL: %w", err)
	}
	u := base.JoinPath(reqPath)
	u.RawQuery = query.Encode()

	timeout := time.Duration(timeoutMs) * time.Millisecond
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}
	for k, vs := range c.buildHeaders(0) {
		for _, v := range vs {
			req.Header.Set(k, v)
		}
	}

	resp, err := c.HTTPClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("API %s %d: %s", reqPath, resp.StatusCode, string(respBody))
	}
	return respBody, nil
}

// GetUpdates performs a long-poll getUpdates request.
// On context deadline (normal for long-poll), returns an empty response with ret=0.
func (c *APIClient) GetUpdates(ctx context.Context, getUpdatesBuf string, timeoutMs int) (*GetUpdatesResp, error) {
	if timeoutMs <= 0 {
		timeoutMs = defaultLongPollTimeoutMs
	}

	reqBody := GetUpdatesReq{
		GetUpdatesBuf: getUpdatesBuf,
		BaseInfo:      buildBaseInfo(),
	}
	body, err := json.Marshal(reqBody)
	if err != nil {
		return nil, fmt.Errorf("marshal getUpdates: %w", err)
	}

	respBody, err := c.apiPost(ctx, "ilink/bot/getupdates", body, timeoutMs)
	if err != nil {
		if ctx.Err() != nil {
			slog.Debug("wechat getUpdates: client-side timeout, returning empty response")
			return &GetUpdatesResp{Ret: 0, GetUpdatesBuf: getUpdatesBuf}, nil
		}
		return nil, err
	}

	var resp GetUpdatesResp
	if err := json.Unmarshal(respBody, &resp); err != nil {
		return nil, fmt.Errorf("unmarshal getUpdates: %w", err)
	}
	return &resp, nil
}

// SendMessage sends a single message downstream.
func (c *APIClient) SendMessage(ctx context.Context, req *SendMessageReq) error {
	if req.BaseInfo == nil {
		req.BaseInfo = buildBaseInfo()
	}
	body, err := json.Marshal(req)
	if err != nil {
		return fmt.Errorf("marshal sendMessage: %w", err)
	}
	_, err = c.apiPost(ctx, "ilink/bot/sendmessage", body, defaultAPITimeoutMs)
	return err
}

// GetUploadURL gets a pre-signed CDN upload URL.
func (c *APIClient) GetUploadURL(ctx context.Context, req *GetUploadURLReq) (*GetUploadURLResp, error) {
	if req.BaseInfo == nil {
		req.BaseInfo = buildBaseInfo()
	}
	body, err := json.Marshal(req)
	if err != nil {
		return nil, fmt.Errorf("marshal getUploadUrl: %w", err)
	}
	respBody, err := c.apiPost(ctx, "ilink/bot/getuploadurl", body, defaultAPITimeoutMs)
	if err != nil {
		return nil, err
	}
	var resp GetUploadURLResp
	if err := json.Unmarshal(respBody, &resp); err != nil {
		return nil, fmt.Errorf("unmarshal getUploadUrl: %w", err)
	}
	return &resp, nil
}

// GetConfig fetches bot config (includes typing_ticket).
func (c *APIClient) GetConfig(ctx context.Context, ilinkUserID, contextToken string) (*GetConfigResp, error) {
	reqBody := GetConfigReq{
		ILinkUserID:  ilinkUserID,
		ContextToken: contextToken,
		BaseInfo:     buildBaseInfo(),
	}
	body, err := json.Marshal(reqBody)
	if err != nil {
		return nil, fmt.Errorf("marshal getConfig: %w", err)
	}
	respBody, err := c.apiPost(ctx, "ilink/bot/getconfig", body, defaultConfigTimeoutMs)
	if err != nil {
		return nil, err
	}
	var resp GetConfigResp
	if err := json.Unmarshal(respBody, &resp); err != nil {
		return nil, fmt.Errorf("unmarshal getConfig: %w", err)
	}
	return &resp, nil
}

// SendTyping sends a typing indicator.
func (c *APIClient) SendTyping(ctx context.Context, req *SendTypingReq) error {
	if req.BaseInfo == nil {
		req.BaseInfo = buildBaseInfo()
	}
	body, err := json.Marshal(req)
	if err != nil {
		return fmt.Errorf("marshal sendTyping: %w", err)
	}
	_, err = c.apiPost(ctx, "ilink/bot/sendtyping", body, defaultConfigTimeoutMs)
	return err
}
