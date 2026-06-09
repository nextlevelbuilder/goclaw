package mcpoauth

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"

	"github.com/nextlevelbuilder/goclaw/internal/security"
)

// DCRRequest is the Dynamic Client Registration request body (RFC 7591).
type DCRRequest struct {
	RedirectURIs            []string `json:"redirect_uris"`
	GrantTypes              []string `json:"grant_types"`
	ResponseTypes           []string `json:"response_types"`
	ClientName              string   `json:"client_name"`
	TokenEndpointAuthMethod string   `json:"token_endpoint_auth_method"`
}

// DCRResponse is the server response from Dynamic Client Registration.
type DCRResponse struct {
	ClientID                string `json:"client_id"`
	ClientSecret            string `json:"client_secret,omitempty"`
	ClientIDIssuedAt        int64  `json:"client_id_issued_at,omitempty"`
	ClientSecretExpiresAt   int64  `json:"client_secret_expires_at,omitempty"`
	TokenEndpointAuthMethod string `json:"token_endpoint_auth_method,omitempty"`
}

// RegisterClient performs Dynamic Client Registration (RFC 7591).
// callbackURL is the redirect_uri to register.
func RegisterClient(ctx context.Context, client *http.Client, registrationEndpoint, callbackURL string) (*DCRResponse, error) {
	body := DCRRequest{
		RedirectURIs:            []string{callbackURL},
		GrantTypes:              []string{"authorization_code", "refresh_token"},
		ResponseTypes:           []string{"code"},
		ClientName:              "GoClaw Gateway",
		TokenEndpointAuthMethod: "none", // public client — PKCE handles security
	}
	data, err := json.Marshal(body)
	if err != nil {
		return nil, err
	}
	_, ip, err := security.Validate(registrationEndpoint)
	if err != nil {
		return nil, fmt.Errorf("mcpoauth: SSRF validation failed for registration endpoint: %w", err)
	}
	ctx = security.WithPinnedIP(ctx, ip)

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, registrationEndpoint, bytes.NewReader(data))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")

	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("mcpoauth: DCR request failed: %w", err)
	}
	defer resp.Body.Close()
	respBody, _ := io.ReadAll(io.LimitReader(resp.Body, 32*1024))
	if resp.StatusCode != http.StatusCreated && resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("mcpoauth: DCR returned HTTP %d: %s", resp.StatusCode, string(respBody))
	}
	var result DCRResponse
	if err := json.Unmarshal(respBody, &result); err != nil {
		return nil, fmt.Errorf("mcpoauth: DCR response parse error: %w", err)
	}
	if result.ClientID == "" {
		return nil, fmt.Errorf("mcpoauth: DCR response missing client_id")
	}
	return &result, nil
}
