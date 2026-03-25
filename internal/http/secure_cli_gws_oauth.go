package http

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
)

const (
	gwsOAuthAuthURL    = "https://accounts.google.com/o/oauth2/v2/auth"
	gwsOAuthTokenURL   = "https://oauth2.googleapis.com/token"
	gwsOAuthRedirect   = "urn:ietf:wg:oauth:2.0:oob"
	gwsOAuthScope      = "https://mail.google.com/"
	gwsCredentialsDir  = "/tmp/goclaw-gws"
	gwsCredentialsFile = "/tmp/goclaw-gws/credentials.json"
)

// gwsOAuthURLRequest is the request body for the OAuth URL endpoint.
type gwsOAuthURLRequest struct {
	ClientID string `json:"client_id"`
}

// gwsOAuthExchangeRequest is the request body for the token exchange endpoint.
type gwsOAuthExchangeRequest struct {
	ClientID     string `json:"client_id"`
	ClientSecret string `json:"client_secret"`
	Code         string `json:"code"`
}

// handleGwsOAuthURL builds a Google OAuth authorization URL for gws.
// POST /v1/cli-credentials/gws/oauth-url
func (h *SecureCLIHandler) handleGwsOAuthURL(w http.ResponseWriter, r *http.Request) {
	var req gwsOAuthURLRequest
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<16)).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid JSON"})
		return
	}
	if strings.TrimSpace(req.ClientID) == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "client_id is required"})
		return
	}

	params := url.Values{}
	params.Set("client_id", req.ClientID)
	params.Set("redirect_uri", gwsOAuthRedirect)
	params.Set("response_type", "code")
	params.Set("scope", gwsOAuthScope)
	params.Set("access_type", "offline")
	params.Set("prompt", "consent")

	authURL := gwsOAuthAuthURL + "?" + params.Encode()
	writeJSON(w, http.StatusOK, map[string]string{"url": authURL})
}

// handleGwsOAuthExchange exchanges an authorization code for tokens and writes
// the authorized_user credentials file to disk.
// POST /v1/cli-credentials/gws/oauth-exchange
func (h *SecureCLIHandler) handleGwsOAuthExchange(w http.ResponseWriter, r *http.Request) {
	var req gwsOAuthExchangeRequest
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<16)).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid JSON"})
		return
	}
	if strings.TrimSpace(req.ClientID) == "" || strings.TrimSpace(req.ClientSecret) == "" || strings.TrimSpace(req.Code) == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "client_id, client_secret, and code are required"})
		return
	}

	refreshToken, err := exchangeGwsCode(req.ClientID, req.ClientSecret, req.Code)
	if err != nil {
		writeJSON(w, http.StatusBadGateway, map[string]string{"error": "token exchange failed: " + err.Error()})
		return
	}

	credPath, err := writeGwsCredentials(req.ClientID, req.ClientSecret, refreshToken)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to write credentials: " + err.Error()})
		return
	}

	writeJSON(w, http.StatusOK, map[string]string{
		"credentials_file": credPath,
		"status":           "authorized",
	})
}

// exchangeGwsCode posts to Google's token endpoint and returns the refresh_token.
func exchangeGwsCode(clientID, clientSecret, code string) (string, error) {
	form := url.Values{}
	form.Set("client_id", clientID)
	form.Set("client_secret", clientSecret)
	form.Set("code", code)
	form.Set("redirect_uri", gwsOAuthRedirect)
	form.Set("grant_type", "authorization_code")

	resp, err := http.PostForm(gwsOAuthTokenURL, form)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<16))
	if err != nil {
		return "", err
	}

	var result map[string]any
	if err := json.Unmarshal(body, &result); err != nil {
		return "", fmt.Errorf("unexpected response: %s", body)
	}
	if errMsg, ok := result["error"].(string); ok {
		desc, _ := result["error_description"].(string)
		if desc != "" {
			return "", fmt.Errorf("%s: %s", errMsg, desc)
		}
		return "", fmt.Errorf("%s", errMsg)
	}
	refreshToken, ok := result["refresh_token"].(string)
	if !ok || refreshToken == "" {
		return "", fmt.Errorf("no refresh_token in response (code may have been used already)")
	}
	return refreshToken, nil
}

// writeGwsCredentials writes the authorized_user JSON to the credentials file.
func writeGwsCredentials(clientID, clientSecret, refreshToken string) (string, error) {
	if err := os.MkdirAll(gwsCredentialsDir, 0o700); err != nil {
		return "", err
	}
	creds := map[string]string{
		"type":          "authorized_user",
		"client_id":     clientID,
		"client_secret": clientSecret,
		"refresh_token": refreshToken,
	}
	data, err := json.MarshalIndent(creds, "", "  ")
	if err != nil {
		return "", err
	}
	credPath := filepath.Join(gwsCredentialsDir, "credentials.json")
	if err := os.WriteFile(credPath, data, 0o644); err != nil {
		return "", err
	}
	return credPath, nil
}
