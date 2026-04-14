// Package oauth implements OAuth 2.0 PKCE flows for LLM provider authentication.
package oauth

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"fmt"
	"net/http"
	"os/exec"
	"runtime"
	"time"
)

const (
	tokenHTTPTimeout = 30 * time.Second
)

// httpClient is used for token exchange/refresh requests with a timeout.
var httpClient = &http.Client{Timeout: tokenHTTPTimeout}

// ProviderConfig holds OAuth endpoints for a provider.
type ProviderConfig struct {
	Name         string
	AuthURL      string
	TokenURL     string
	ClientID     string
	Scopes       string
	CallbackPort int    // 0 = OS-assigned random port
	CallbackPath string // "/callback" or "/auth/callback"
}

// TokenResponse is the base token response from any OAuth provider.
type TokenResponse struct {
	AccessToken  string `json:"access_token"`
	RefreshToken string `json:"refresh_token"`
	ExpiresIn    int    `json:"expires_in"`
	TokenType    string `json:"token_type"`
	Scope        string `json:"scope"`
	IDToken      string `json:"id_token,omitempty"` // optional; present for OIDC providers (e.g. OpenAI)
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
