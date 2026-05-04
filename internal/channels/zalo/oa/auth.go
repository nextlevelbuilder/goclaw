package oa

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"strconv"
	"strings"
	"time"
)

// ErrAuthExpired: refresh token rejected (single-use rotation burned or
// operator revoked OA permission). Operator must re-consent.
var ErrAuthExpired = errors.New("zalo_oa: refresh token expired, re-auth required")

// ErrNotAuthorized: channel has not yet completed the paste-code consent
// flow. Health stays Degraded (not Failed).
var ErrNotAuthorized = errors.New("zalo_oa: not yet authorized (paste consent code first)")

// classifyRefreshError escalates only the language-independent invalid_grant
// code (-118); substring-matching localized messages would force false
// re-consent on transient server errors.
func classifyRefreshError(err error) error {
	if err == nil {
		return nil
	}
	var apiErr *APIError
	if errors.As(err, &apiErr) && apiErr.Code == codeInvalidGrant {
		return fmt.Errorf("%w (zalo error %d: %s)", ErrAuthExpired, apiErr.Code, apiErr.Message)
	}
	return err
}

// Tokens is the parsed OAuth response.
type Tokens struct {
	AccessToken           string
	RefreshToken          string
	ExpiresAt             time.Time
	RefreshTokenExpiresAt time.Time // zero if Zalo omits refresh_token_expires_in
}

type tokenResponse struct {
	AccessToken           string      `json:"access_token"`
	RefreshToken          string      `json:"refresh_token"`
	ExpiresIn             flexSeconds `json:"expires_in"`
	RefreshTokenExpiresIn flexSeconds `json:"refresh_token_expires_in"`
}

// flexSeconds accepts either a JSON number or a quoted string for
// expires_in — Zalo's OA OAuth endpoint returns the latter in practice.
type flexSeconds int64

func (f *flexSeconds) UnmarshalJSON(b []byte) error {
	s := strings.Trim(string(b), `"`)
	if s == "" || s == "null" {
		return nil
	}
	n, err := strconv.ParseInt(s, 10, 64)
	if err != nil {
		return fmt.Errorf("expires_in: %w", err)
	}
	*f = flexSeconds(n)
	return nil
}

// ExchangeCode swaps an authorization code for an (access, refresh) pair.
// POST oauth.zaloapp.com/v4/oa/access_token, secret_key in HEADER.
func (c *Client) ExchangeCode(ctx context.Context, appID, secretKey, code string) (*Tokens, error) {
	form := url.Values{
		"app_id":     {appID},
		"code":       {code},
		"grant_type": {"authorization_code"},
	}
	return c.tokenCall(ctx, secretKey, form)
}

// RefreshToken trades a refresh token for a new (access, refresh) pair.
// Refresh tokens are SINGLE-USE — every successful refresh rotates both.
func (c *Client) RefreshToken(ctx context.Context, appID, secretKey, refresh string) (*Tokens, error) {
	form := url.Values{
		"app_id":        {appID},
		"refresh_token": {refresh},
		"grant_type":    {"refresh_token"},
	}
	return c.tokenCall(ctx, secretKey, form)
}

func (c *Client) tokenCall(ctx context.Context, secretKey string, form url.Values) (*Tokens, error) {
	headers := map[string]string{"secret_key": secretKey}
	raw, err := c.postForm(ctx, c.oauthBase+pathOAuthAccessToken, headers, form)
	if err != nil {
		return nil, err
	}
	var resp tokenResponse
	if err := json.Unmarshal(raw, &resp); err != nil {
		return nil, fmt.Errorf("decode token response: %w", err)
	}
	if resp.AccessToken == "" {
		return nil, fmt.Errorf("zalo oauth: empty access_token in response")
	}
	exp := time.Now().UTC().Add(time.Duration(resp.ExpiresIn) * time.Second)
	var refreshExp time.Time
	if resp.RefreshTokenExpiresIn > 0 {
		refreshExp = time.Now().UTC().Add(time.Duration(resp.RefreshTokenExpiresIn) * time.Second)
	}
	return &Tokens{
		AccessToken:           resp.AccessToken,
		RefreshToken:          resp.RefreshToken,
		ExpiresAt:             exp,
		RefreshTokenExpiresAt: refreshExp,
	}, nil
}

// ConsentURL builds the redirect URL the operator visits to authorize
// the OA. The state token is validated in the WS exchange_code handler.
func ConsentURL(appID, redirectURI, state string) string {
	q := url.Values{
		"app_id":       {appID},
		"redirect_uri": {redirectURI},
		"state":        {state},
	}
	return defaultOAuthBase + "/oa/permission?" + q.Encode()
}
