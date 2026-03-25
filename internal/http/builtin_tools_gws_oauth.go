package http

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
)

const (
	gwsOAuthRedirectURI = "urn:ietf:wg:oauth:2.0:oob"
	gwsOAuthAuthURL     = "https://accounts.google.com/o/oauth2/v2/auth"
	gwsOAuthTokenURL    = "https://oauth2.googleapis.com/token"
	gwsOAuthScope       = "https://mail.google.com/"
)

// handleGWSAuthURL generates a Google OAuth2 authorization URL for GWS.
// Request body: { "client_id": "xxx", "client_secret": "yyy" }
// Returns:      { "url": "https://accounts.google.com/..." }
func (h *BuiltinToolsHandler) handleGWSAuthURL(w http.ResponseWriter, r *http.Request) {
	var body struct {
		ClientID     string `json:"client_id"`
		ClientSecret string `json:"client_secret"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20)).Decode(&body); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid request body"})
		return
	}
	if body.ClientID == "" || body.ClientSecret == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "client_id and client_secret are required"})
		return
	}

	params := url.Values{}
	params.Set("client_id", body.ClientID)
	params.Set("redirect_uri", gwsOAuthRedirectURI)
	params.Set("response_type", "code")
	params.Set("scope", gwsOAuthScope)
	params.Set("access_type", "offline")
	params.Set("prompt", "consent")

	authURL := gwsOAuthAuthURL + "?" + params.Encode()
	writeJSON(w, http.StatusOK, map[string]string{"url": authURL})
}

// handleGWSExchange exchanges a Google authorization code for a refresh token
// and saves it to the GWS builtin tool settings.
// Request body: { "client_id": "xxx", "client_secret": "yyy", "code": "4/0Axx..." }
// Returns:      { "status": "authorized", "refresh_token_preview": "1//0xxx..." }
func (h *BuiltinToolsHandler) handleGWSExchange(w http.ResponseWriter, r *http.Request) {
	var body struct {
		ClientID     string `json:"client_id"`
		ClientSecret string `json:"client_secret"`
		Code         string `json:"code"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20)).Decode(&body); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid request body"})
		return
	}
	if body.ClientID == "" || body.ClientSecret == "" || body.Code == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "client_id, client_secret, and code are required"})
		return
	}

	refreshToken, err := exchangeGWSCode(body.ClientID, body.ClientSecret, body.Code)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": fmt.Sprintf("token exchange failed: %s", err.Error())})
		return
	}

	// Save refresh_token into builtin_tools settings for "gws"
	newSettings := map[string]any{
		"client_id":     body.ClientID,
		"client_secret": body.ClientSecret,
		"refresh_token": refreshToken,
	}
	if err := h.store.Update(r.Context(), "gws", map[string]any{"settings": newSettings}); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to save settings"})
		return
	}

	// Return only a preview of the refresh token for safety
	preview := refreshToken
	if len(preview) > 10 {
		preview = preview[:10] + "..."
	}
	writeJSON(w, http.StatusOK, map[string]string{
		"status":               "authorized",
		"refresh_token_preview": preview,
	})
}

// exchangeGWSCode posts to Google OAuth2 token endpoint and returns the refresh_token.
func exchangeGWSCode(clientID, clientSecret, code string) (string, error) {
	params := url.Values{}
	params.Set("code", code)
	params.Set("client_id", clientID)
	params.Set("client_secret", clientSecret)
	params.Set("redirect_uri", gwsOAuthRedirectURI)
	params.Set("grant_type", "authorization_code")

	resp, err := http.Post(gwsOAuthTokenURL, "application/x-www-form-urlencoded", strings.NewReader(params.Encode()))
	if err != nil {
		return "", fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	raw, err := io.ReadAll(io.LimitReader(resp.Body, 1<<16))
	if err != nil {
		return "", fmt.Errorf("read response: %w", err)
	}

	var tokenResp struct {
		RefreshToken string `json:"refresh_token"`
		Error        string `json:"error"`
		ErrorDesc    string `json:"error_description"`
	}
	if err := json.Unmarshal(raw, &tokenResp); err != nil {
		return "", fmt.Errorf("parse response: %w", err)
	}
	if tokenResp.Error != "" {
		desc := tokenResp.ErrorDesc
		if desc == "" {
			desc = tokenResp.Error
		}
		return "", fmt.Errorf("%s", desc)
	}
	if tokenResp.RefreshToken == "" {
		return "", fmt.Errorf("no refresh_token in response")
	}
	return tokenResp.RefreshToken, nil
}
