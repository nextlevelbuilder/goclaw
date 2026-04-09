package oauth

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

const (
	DefaultGitHubCopilotProviderName = "github-copilot"
	DefaultGitHubCopilotAPIBase      = "https://api.individual.githubcopilot.com"
	GitHubCopilotUserAgent           = "GitHubCopilotChat/0.35.0"
	GitHubCopilotEditorVersion       = "vscode/1.107.0"
	GitHubCopilotPluginVersion       = "copilot-chat/0.35.0"
	GitHubCopilotIntegrationID       = "vscode-chat"
)

var gitHubCopilotClientID = decodeGitHubCopilotClientID("SXYxLmI1MDdhMDhjODdlY2ZlOTg=")

type GitHubCopilotDeviceCodeResponse struct {
	DeviceCode      string `json:"device_code"`
	UserCode        string `json:"user_code"`
	VerificationURI string `json:"verification_uri"`
	Interval        int    `json:"interval"`
	ExpiresIn       int    `json:"expires_in"`
}

type GitHubCopilotDeviceTokenSuccessResponse struct {
	AccessToken string `json:"access_token"`
	TokenType   string `json:"token_type,omitempty"`
	Scope       string `json:"scope,omitempty"`
}

type GitHubCopilotDeviceTokenErrorResponse struct {
	Error            string `json:"error"`
	ErrorDescription string `json:"error_description,omitempty"`
	Interval         int    `json:"interval,omitempty"`
}

type GitHubCopilotTokenResponse struct {
	Token     string `json:"token"`
	ExpiresAt int64  `json:"expires_at"`
	RefreshIn int    `json:"refresh_in,omitempty"`
	SKU       string `json:"sku,omitempty"`
}

type PendingGitHubCopilotLogin struct {
	Domain string
	Device GitHubCopilotDeviceCodeResponse
}

func decodeGitHubCopilotClientID(encoded string) string {
	decoded, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil {
		return ""
	}
	return string(decoded)
}

func gitHubCopilotHeaders() map[string]string {
	return map[string]string{
		"User-Agent":            GitHubCopilotUserAgent,
		"Editor-Version":        GitHubCopilotEditorVersion,
		"Editor-Plugin-Version": GitHubCopilotPluginVersion,
		"Copilot-Integration-Id": GitHubCopilotIntegrationID,
	}
}

func NormalizeGitHubCopilotDomain(input string) string {
	trimmed := strings.TrimSpace(input)
	if trimmed == "" {
		return ""
	}
	if !strings.Contains(trimmed, "://") {
		trimmed = "https://" + trimmed
	}
	u, err := url.Parse(trimmed)
	if err != nil {
		return ""
	}
	return strings.TrimSpace(u.Hostname())
}

func gitHubCopilotURLs(domain string) (deviceCodeURL, accessTokenURL, copilotTokenURL string) {
	if domain == "" {
		domain = "github.com"
	}
	return fmt.Sprintf("https://%s/login/device/code", domain),
		fmt.Sprintf("https://%s/login/oauth/access_token", domain),
		fmt.Sprintf("https://api.%s/copilot_internal/v2/token", domain)
}

func getGitHubCopilotBaseURLFromToken(token string) string {
	match := strings.Split(token, "proxy-ep=")
	if len(match) < 2 {
		return ""
	}
	proxyHost := match[1]
	if idx := strings.Index(proxyHost, ";"); idx >= 0 {
		proxyHost = proxyHost[:idx]
	}
	proxyHost = strings.TrimSpace(proxyHost)
	if proxyHost == "" {
		return ""
	}
	apiHost := strings.TrimPrefix(proxyHost, "proxy.")
	if apiHost == proxyHost {
		apiHost = proxyHost
	} else {
		apiHost = "api." + apiHost
	}
	return "https://" + apiHost
}

func GetGitHubCopilotBaseURL(token, enterpriseDomain string) string {
	if base := getGitHubCopilotBaseURLFromToken(token); base != "" {
		return base
	}
	if domain := NormalizeGitHubCopilotDomain(enterpriseDomain); domain != "" {
		return "https://copilot-api." + domain
	}
	return DefaultGitHubCopilotAPIBase
}

func fetchGitHubCopilotJSON(target string, init func(*http.Request)) (map[string]any, error) {
	req, err := http.NewRequest(http.MethodPost, target, nil)
	if err != nil {
		return nil, err
	}
	if init != nil {
		init(req)
	}
	resp, err := httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("%s %s: %s", resp.Status, target, string(body))
	}
	var data map[string]any
	if err := json.Unmarshal(body, &data); err != nil {
		return nil, fmt.Errorf("parse response: %w", err)
	}
	return data, nil
}

func StartLoginGitHubCopilot(enterpriseDomain string) (*PendingGitHubCopilotLogin, error) {
	domain := NormalizeGitHubCopilotDomain(enterpriseDomain)
	if domain == "" {
		domain = "github.com"
	}
	deviceCodeURL, _, _ := gitHubCopilotURLs(domain)
	form := url.Values{
		"client_id": {gitHubCopilotClientID},
		"scope":     {"read:user"},
	}
	req, err := http.NewRequest(http.MethodPost, deviceCodeURL, strings.NewReader(form.Encode()))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("User-Agent", GitHubCopilotUserAgent)
	resp, err := httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("device flow failed (%s): %s", resp.Status, string(body))
	}
	var device GitHubCopilotDeviceCodeResponse
	if err := json.Unmarshal(body, &device); err != nil {
		return nil, fmt.Errorf("parse device flow response: %w", err)
	}
	if device.DeviceCode == "" || device.UserCode == "" || device.VerificationURI == "" {
		return nil, fmt.Errorf("invalid device flow response")
	}
	if device.Interval <= 0 {
		device.Interval = 5
	}
	return &PendingGitHubCopilotLogin{Domain: domain, Device: device}, nil
}

func (p *PendingGitHubCopilotLogin) Wait(ctx context.Context) (string, *GitHubCopilotTokenResponse, error) {
	githubToken, err := pollForGitHubCopilotAccessToken(ctx, p.Domain, p.Device.DeviceCode, p.Device.Interval, p.Device.ExpiresIn)
	if err != nil {
		return "", nil, err
	}
	tokenResp, err := RefreshGitHubCopilotToken(githubToken, p.Domain)
	if err != nil {
		return "", nil, err
	}
	return githubToken, tokenResp, nil
}

func pollForGitHubCopilotAccessToken(ctx context.Context, domain, deviceCode string, intervalSeconds, expiresIn int) (string, error) {
	_, accessTokenURL, _ := gitHubCopilotURLs(domain)
	if intervalSeconds <= 0 {
		intervalSeconds = 5
	}
	deadline := time.Now().Add(time.Duration(expiresIn) * time.Second)
	interval := time.Duration(intervalSeconds) * time.Second
	for time.Now().Before(deadline) {
		select {
		case <-ctx.Done():
			return "", ctx.Err()
		case <-time.After(interval):
		}

		form := url.Values{
			"client_id":   {gitHubCopilotClientID},
			"device_code": {deviceCode},
			"grant_type":  {"urn:ietf:params:oauth:grant-type:device_code"},
		}
		req, err := http.NewRequest(http.MethodPost, accessTokenURL, strings.NewReader(form.Encode()))
		if err != nil {
			return "", err
		}
		req.Header.Set("Accept", "application/json")
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		req.Header.Set("User-Agent", GitHubCopilotUserAgent)
		resp, err := httpClient.Do(req)
		if err != nil {
			return "", err
		}
		body, readErr := io.ReadAll(resp.Body)
		resp.Body.Close()
		if readErr != nil {
			return "", readErr
		}
		if resp.StatusCode < 200 || resp.StatusCode >= 300 {
			return "", fmt.Errorf("device token poll failed (%s): %s", resp.Status, string(body))
		}
		var success GitHubCopilotDeviceTokenSuccessResponse
		if err := json.Unmarshal(body, &success); err == nil && success.AccessToken != "" {
			return success.AccessToken, nil
		}
		var tokenErr GitHubCopilotDeviceTokenErrorResponse
		if err := json.Unmarshal(body, &tokenErr); err != nil {
			return "", fmt.Errorf("parse device token response: %w", err)
		}
		switch tokenErr.Error {
		case "authorization_pending":
			continue
		case "slow_down":
			if tokenErr.Interval > 0 {
				interval = time.Duration(tokenErr.Interval) * time.Second
			} else {
				interval += 5 * time.Second
			}
			continue
		default:
			detail := strings.TrimSpace(tokenErr.ErrorDescription)
			if detail != "" {
				return "", fmt.Errorf("device flow failed: %s: %s", tokenErr.Error, detail)
			}
			return "", fmt.Errorf("device flow failed: %s", tokenErr.Error)
		}
	}
	return "", fmt.Errorf("device flow timed out")
}

func RefreshGitHubCopilotToken(githubAccessToken, enterpriseDomain string) (*GitHubCopilotTokenResponse, error) {
	domain := NormalizeGitHubCopilotDomain(enterpriseDomain)
	if domain == "" {
		domain = "github.com"
	}
	_, _, copilotTokenURL := gitHubCopilotURLs(domain)
	req, err := http.NewRequest(http.MethodGet, copilotTokenURL, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Authorization", "Bearer "+githubAccessToken)
	for key, value := range gitHubCopilotHeaders() {
		req.Header.Set(key, value)
	}
	resp, err := httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("copilot token mint failed (%s): %s", resp.Status, string(body))
	}
	var tokenResp GitHubCopilotTokenResponse
	if err := json.Unmarshal(body, &tokenResp); err != nil {
		return nil, fmt.Errorf("parse Copilot token response: %w", err)
	}
	if tokenResp.Token == "" || tokenResp.ExpiresAt == 0 {
		return nil, fmt.Errorf("invalid Copilot token response")
	}
	return &tokenResp, nil
}

func EnableGitHubCopilotModel(token, modelID, enterpriseDomain string) bool {
	if strings.TrimSpace(modelID) == "" || strings.TrimSpace(token) == "" {
		return false
	}
	baseURL := GetGitHubCopilotBaseURL(token, enterpriseDomain)
	target := strings.TrimRight(baseURL, "/") + "/models/" + url.PathEscape(modelID) + "/policy"
	body := strings.NewReader(`{"state":"enabled"}`)
	req, err := http.NewRequest(http.MethodPost, target, body)
	if err != nil {
		return false
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("openai-intent", "chat-policy")
	req.Header.Set("x-interaction-type", "chat-policy")
	for key, value := range gitHubCopilotHeaders() {
		req.Header.Set(key, value)
	}
	resp, err := httpClient.Do(req)
	if err != nil {
		return false
	}
	defer resp.Body.Close()
	return resp.StatusCode >= 200 && resp.StatusCode < 300
}