package oauth

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"strings"

	"github.com/nextlevelbuilder/goclaw/internal/providers"
)

var quotaHTTPClient = &http.Client{Timeout: openAIQuotaTimeout}

func requestOpenAIQuota(ctx context.Context, apiBase, accessToken, accountID string) (*openAIUsageResponse, error) {
	apiBase = strings.TrimRight(strings.TrimSpace(apiBase), "/")
	if apiBase == "" {
		apiBase = DefaultProviderAPIBase
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, apiBase+"/wham/usage", nil)
	if err != nil {
		return nil, fmt.Errorf("build quota request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+accessToken)
	req.Header.Set("ChatGPT-Account-Id", accountID)
	req.Header.Set("User-Agent", openAIQuotaUserAgent)

	resp, err := quotaHTTPClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
	if resp.StatusCode != http.StatusOK {
		return nil, &providers.HTTPError{
			Status:     resp.StatusCode,
			Body:       string(body),
			RetryAfter: providers.ParseRetryAfter(resp.Header.Get("Retry-After")),
		}
	}

	var payload openAIUsageResponse
	if err := json.Unmarshal(body, &payload); err != nil {
		return nil, fmt.Errorf("parse quota response: %w", err)
	}
	return &payload, nil
}

func buildOpenAIQuotaRequestFailure(result OpenAIQuotaResult, err error) OpenAIQuotaResult {
	var httpErr *providers.HTTPError
	if errors.As(err, &httpErr) {
		return buildOpenAIQuotaHTTPFailure(result, httpErr.Status, httpErr.Body)
	}

	if errors.Is(err, context.DeadlineExceeded) {
		return buildOpenAIQuotaFailure(result, "network_timeout", "Quota request timed out.", "Retry later. The upstream quota endpoint took too long to respond.", false, false, true)
	}

	var netErr net.Error
	if errors.As(err, &netErr) {
		return buildOpenAIQuotaFailure(result, "network_error", "Quota request failed.", "Retry later or inspect network connectivity to the upstream quota endpoint.", false, false, true)
	}

	return buildOpenAIQuotaFailure(result, "quota_request_failed", "Quota request failed.", "Retry later or inspect the upstream quota endpoint response.", false, false, false)
}

func buildOpenAIQuotaHTTPFailure(result OpenAIQuotaResult, status int, body string) OpenAIQuotaResult {
	switch status {
	case http.StatusUnauthorized:
		return buildOpenAIQuotaFailure(result, "reauth_required", "Token expired or invalid.", "Sign in again to refresh workspace access for this account.", true, false, false)
	case http.StatusPaymentRequired:
		return buildOpenAIQuotaFailure(result, "payment_required", "Workspace billing or entitlement is blocking quota access.", "Confirm the ChatGPT workspace plan or billing status for this account.", false, false, false)
	case http.StatusForbidden:
		return buildOpenAIQuotaFailure(result, "quota_api_forbidden", "Quota endpoint access is forbidden for this account.", "This account cannot read quota data from the upstream endpoint.", false, true, false)
	case http.StatusNotFound:
		return buildOpenAIQuotaFailure(result, "quota_endpoint_not_found", "Quota endpoint is unavailable.", "This provider API base does not expose the Codex quota endpoint.", false, false, false)
	case http.StatusTooManyRequests:
		return buildOpenAIQuotaFailure(result, "rate_limited", "Quota endpoint asked to retry later.", "Retry after a short delay. The upstream quota endpoint is rate limited right now.", false, false, true)
	default:
		if status >= http.StatusInternalServerError {
			return buildOpenAIQuotaFailure(result, "provider_unavailable", "Quota service is temporarily unavailable.", "Retry later. This looks like a temporary upstream problem.", false, false, true)
		}
		_ = sanitizeOpenAIQuotaErrorBody(body)
		return buildOpenAIQuotaFailure(result, "unknown_upstream_error", "Quota request failed.", "Inspect the upstream response and retry if appropriate.", false, false, false)
	}
}

func buildOpenAIQuotaFailure(result OpenAIQuotaResult, code, message, hint string, needsReauth, isForbidden, retryable bool) OpenAIQuotaResult {
	result.Success = false
	result.ErrorCode = code
	result.Error = message
	result.ActionHint = hint
	result.NeedsReauth = needsReauth
	result.IsForbidden = isForbidden
	result.Retryable = retryable
	return result
}

func sanitizeOpenAIQuotaErrorBody(body string) string {
	body = strings.TrimSpace(body)
	if body == "" {
		return ""
	}
	body = strings.ReplaceAll(body, "\n", " ")
	body = strings.ReplaceAll(body, "\r", " ")
	if len(body) > 240 {
		return body[:226] + "...[truncated]"
	}
	return body
}
