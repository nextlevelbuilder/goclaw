// Package oauth implements OAuth 2.0 PKCE flows for LLM provider authentication.
package oauth

import (
	"context"
	"crypto/rand"
	"errors"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"html"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/url"
	"os/exec"
	"sync"
	"runtime"
	"time"
)

const (
	OpenAIAuthURL    = "https://auth.openai.com/oauth/authorize"
	OpenAITokenURL   = "https://auth.openai.com/oauth/token"
	OpenAIClientID   = "app_EMoamEEZ73f0CkXaXp7hrann"
	OpenAIScopes     = "openid profile email offline_access api.connectors.read api.connectors.invoke"
	OpenAIRedirectURI = "http://localhost:1455/auth/callback"

	callbackPort = "1455"

	tokenHTTPTimeout = 30 * time.Second
)

// httpClient is used for token exchange/refresh requests with a timeout.
var httpClient = &http.Client{Timeout: tokenHTTPTimeout}

// OpenAITokenResponse is the response from the OpenAI token endpoint.
type OpenAITokenResponse struct {
	AccessToken  string `json:"access_token"`
	RefreshToken string `json:"refresh_token"`
	ExpiresIn    int    `json:"expires_in"`
	TokenType    string `json:"token_type"`
	Scope        string `json:"scope"`
	IDToken      string `json:"id_token,omitempty"`
}

// generatePKCE generates a PKCE code verifier and S256 challenge.
func generatePKCE() (verifier, challenge string, err error) {
	buf := make([]byte, 64)
	if _, err := rand.Read(buf); err != nil {
		return "", "", fmt.Errorf("generate random bytes: %w", err)
	}
	verifier = base64.RawURLEncoding.EncodeToString(buf)
	h := sha256.Sum256([]byte(verifier))
	challenge = base64.RawURLEncoding.EncodeToString(h[:])
	return verifier, challenge, nil
}

// PendingLogin represents an in-progress OAuth flow.
// Call Wait() to block until the user completes authentication.
type PendingLogin struct {
	AuthURL  string
	codeCh   chan string
	errCh    chan error
	verifier string
	state    string
	srv      *http.Server
}

// Wait blocks until the OAuth callback is received or ctx is cancelled.
// Shuts down the callback server when done.
func (p *PendingLogin) Wait(ctx context.Context) (*OpenAITokenResponse, error) {
	defer p.srv.Shutdown(context.Background())

	select {
	case code := <-p.codeCh:
		slog.Debug("received authorization code")
		return exchangeOpenAICode(code, p.verifier)
	case err := <-p.errCh:
		return nil, err
	case <-ctx.Done():
		return nil, fmt.Errorf("authentication timed out: %w", ctx.Err())
	}
}

// Shutdown stops the callback server without waiting for a callback.
func (p *PendingLogin) Shutdown() {
	p.srv.Shutdown(context.Background())
}

// ExchangeRedirectURL extracts the code from a pasted redirect URL and exchanges it for tokens.
// Used for remote/VPS environments where the localhost callback can't be reached.
func (p *PendingLogin) ExchangeRedirectURL(redirectURL string) (*OpenAITokenResponse, error) {
	u, err := url.Parse(redirectURL)
	if err != nil {
		return nil, fmt.Errorf("invalid redirect URL: %w", err)
	}

	state := u.Query().Get("state")
	code := u.Query().Get("code")

	if code == "" {
		errMsg := u.Query().Get("error")
		if errMsg != "" {
			return nil, fmt.Errorf("OAuth error: %s", errMsg)
		}
		return nil, fmt.Errorf("no authorization code in redirect URL")
	}

	if state == "" || state != p.state {
		return nil, fmt.Errorf("invalid state parameter (possible CSRF)")
	}

	return exchangeOpenAICode(code, p.verifier)
}

// StartLoginOpenAI begins the OAuth PKCE flow: starts the callback server
// and returns a PendingLogin with the auth URL. Does NOT open a browser.
// The caller is responsible for directing the user to AuthURL.
func StartLoginOpenAI() (*PendingLogin, error) {
	verifier, challenge, err := generatePKCE()
	if err != nil {
		return nil, err
	}

	stateBuf := make([]byte, 16)
	if _, err := rand.Read(stateBuf); err != nil {
		return nil, fmt.Errorf("generate state: %w", err)
	}
	state := base64.RawURLEncoding.EncodeToString(stateBuf)

	params := url.Values{
		"client_id":                  {OpenAIClientID},
		"redirect_uri":              {OpenAIRedirectURI},
		"response_type":             {"code"},
		"scope":                     {OpenAIScopes},
		"code_challenge":            {challenge},
		"code_challenge_method":      {"S256"},
		"state":                     {state},
		"codex_cli_simplified_flow":  {"true"},
		"id_token_add_organizations": {"true"},
		"originator":                {"pi"},
	}
	authURL := OpenAIAuthURL + "?" + params.Encode()

	codeCh := make(chan string, 1)
	errCh := make(chan error, 1)
	var callbackOnce sync.Once

	listener, err := net.Listen("tcp", "127.0.0.1:"+callbackPort)
	if err != nil {
		return nil, fmt.Errorf("start callback server on port %s: %w", callbackPort, err)
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/auth/callback", func(w http.ResponseWriter, r *http.Request) {
		callbackOnce.Do(func() {
			if r.URL.Query().Get("state") != state {
				w.Header().Set("Content-Type", "text/html")
				fmt.Fprint(w, `<html><body><h2>Authentication Failed</h2><p>Invalid state parameter.</p></body></html>`)
				errCh <- fmt.Errorf("oauth callback: state mismatch (possible CSRF)")
				return
			}

			code := r.URL.Query().Get("code")
			if code == "" {
				errMsg := r.URL.Query().Get("error")
				if errMsg == "" {
					errMsg = "no authorization code received"
				}
				w.Header().Set("Content-Type", "text/html")
				fmt.Fprintf(w, `<html><body><h2>Authentication Failed</h2><p>%s</p></body></html>`, html.EscapeString(errMsg))
				errCh <- fmt.Errorf("oauth callback: %s", errMsg)
				return
			}
			w.Header().Set("Content-Type", "text/html")
			fmt.Fprint(w, `<html><body><h2>Authentication Successful!</h2><p>You can close this window.</p></body></html>`)
			codeCh <- code
		})
	})

	srv := &http.Server{Handler: mux}
	go func() {
		if err := srv.Serve(listener); err != nil && !errors.Is(err, http.ErrServerClosed) {
			errCh <- fmt.Errorf("callback server: %w", err)
		}
	}()

	return &PendingLogin{
		AuthURL:  authURL,
		codeCh:   codeCh,
		errCh:    errCh,
		verifier: verifier,
		state:    state,
		srv:      srv,
	}, nil
}

// LoginOpenAI runs the interactive OAuth PKCE flow for OpenAI.
// Opens the user's browser, waits for callback, and returns the token response.
func LoginOpenAI(ctx context.Context) (*OpenAITokenResponse, error) {
	pending, err := StartLoginOpenAI()
	if err != nil {
		return nil, err
	}

	// Open browser (CLI flow only)
	fmt.Println("Opening browser for OpenAI authentication...")
	fmt.Printf("If the browser doesn't open, visit:\n%s\n\n", pending.AuthURL)
	openBrowser(pending.AuthURL)

	fmt.Println("Waiting for authentication callback...")
	return pending.Wait(ctx)
}

// exchangeOpenAICode exchanges an authorization code for tokens.
func exchangeOpenAICode(code, verifier string) (*OpenAITokenResponse, error) {
	data := url.Values{
		"grant_type":    {"authorization_code"},
		"client_id":     {OpenAIClientID},
		"code":          {code},
		"redirect_uri":  {OpenAIRedirectURI},
		"code_verifier": {verifier},
	}

	resp, err := httpClient.PostForm(OpenAITokenURL, data)
	if err != nil {
		return nil, fmt.Errorf("token exchange request: %w", err)
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("token exchange failed (HTTP %d): %s", resp.StatusCode, string(body))
	}

	var tokenResp OpenAITokenResponse
	if err := json.Unmarshal(body, &tokenResp); err != nil {
		return nil, fmt.Errorf("parse token response: %w", err)
	}
	return &tokenResp, nil
}

// RefreshOpenAIToken refreshes an expired access token using the refresh token.
func RefreshOpenAIToken(refreshToken string) (*OpenAITokenResponse, error) {
	data := url.Values{
		"grant_type":    {"refresh_token"},
		"client_id":     {OpenAIClientID},
		"refresh_token": {refreshToken},
	}

	resp, err := httpClient.PostForm(OpenAITokenURL, data)
	if err != nil {
		return nil, fmt.Errorf("token refresh request: %w", err)
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("token refresh failed (HTTP %d): %s", resp.StatusCode, string(body))
	}

	var tokenResp OpenAITokenResponse
	if err := json.Unmarshal(body, &tokenResp); err != nil {
		return nil, fmt.Errorf("parse refresh response: %w", err)
	}
	return &tokenResp, nil
}

// openBrowser tries to open a URL in the user's default browser.
func openBrowser(url string) {
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "darwin":
		cmd = exec.Command("open", url)
	case "windows":
		cmd = exec.Command("rundll32", "url.dll,FileProtocolHandler", url)
	default:
		// Try common Linux openers
		for _, opener := range []string{"xdg-open", "sensible-browser", "x-www-browser"} {
			if path, err := exec.LookPath(opener); err == nil {
				cmd = exec.Command(path, url)
				break
			}
		}
	}
	if cmd != nil {
		_ = cmd.Start()
	}
}
