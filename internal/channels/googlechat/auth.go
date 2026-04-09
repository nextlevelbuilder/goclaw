package googlechat

import (
	"context"
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
	"sync"
	"time"
)

type ServiceAccountAuth struct {
	email         string
	privateKey    *rsa.PrivateKey
	scopes        []string
	token         string
	expiresAt     time.Time
	mu            sync.Mutex
	tokenEndpoint string
	httpClient    *http.Client
}

type serviceAccountFile struct {
	Type        string `json:"type"`
	ClientEmail string `json:"client_email"`
	PrivateKey  string `json:"private_key"`
	TokenURI    string `json:"token_uri"`
}

func NewServiceAccountAuth(saFilePath string, scopes []string) (*ServiceAccountAuth, error) {
	data, err := os.ReadFile(saFilePath)
	if err != nil {
		return nil, fmt.Errorf("read service account file: %w", err)
	}

	var sa serviceAccountFile
	if err := json.Unmarshal(data, &sa); err != nil {
		return nil, fmt.Errorf("parse service account file: %w", err)
	}
	if sa.ClientEmail == "" {
		return nil, fmt.Errorf("service account file missing client_email")
	}
	if sa.PrivateKey == "" {
		return nil, fmt.Errorf("service account file missing private_key")
	}

	block, _ := pem.Decode([]byte(sa.PrivateKey))
	if block == nil {
		return nil, fmt.Errorf("failed to decode PEM block from private_key")
	}

	key, err := x509.ParsePKCS8PrivateKey(block.Bytes)
	if err != nil {
		rsaKey, err2 := x509.ParsePKCS1PrivateKey(block.Bytes)
		if err2 != nil {
			return nil, fmt.Errorf("parse private key: %w (pkcs1: %w)", err, err2)
		}
		key = rsaKey
	}

	rsaKey, ok := key.(*rsa.PrivateKey)
	if !ok {
		return nil, fmt.Errorf("private key is not RSA")
	}

	ep := sa.TokenURI
	if ep == "" {
		ep = tokenEndpoint
	}

	return &ServiceAccountAuth{
		email:         sa.ClientEmail,
		privateKey:    rsaKey,
		scopes:        scopes,
		tokenEndpoint: ep,
		httpClient:    &http.Client{Timeout: 10 * time.Second},
	}, nil
}

func (a *ServiceAccountAuth) Token(ctx context.Context) (string, error) {
	a.mu.Lock()
	defer a.mu.Unlock()

	if a.token != "" && time.Now().Add(60*time.Second).Before(a.expiresAt) {
		return a.token, nil
	}

	now := time.Now()
	claims := map[string]any{
		"iss":   a.email,
		"scope": strings.Join(a.scopes, " "),
		"aud":   tokenEndpoint,
		"iat":   now.Unix(),
		"exp":   now.Add(time.Hour).Unix(),
	}

	signedJWT, err := signJWT(a.privateKey, claims)
	if err != nil {
		return "", fmt.Errorf("sign JWT: %w", err)
	}

	form := url.Values{
		"grant_type": {"urn:ietf:params:oauth:grant-type:jwt-bearer"},
		"assertion":  {signedJWT},
	}

	req, err := http.NewRequestWithContext(ctx, "POST", a.tokenEndpoint, strings.NewReader(form.Encode()))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	resp, err := a.httpClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("token exchange request: %w", err)
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("token exchange failed (%d): %s", resp.StatusCode, string(body))
	}

	var tokenResp struct {
		AccessToken string `json:"access_token"`
		ExpiresIn   int    `json:"expires_in"`
		TokenType   string `json:"token_type"`
	}
	if err := json.Unmarshal(body, &tokenResp); err != nil {
		return "", fmt.Errorf("parse token response: %w", err)
	}

	a.token = tokenResp.AccessToken
	a.expiresAt = now.Add(time.Duration(tokenResp.ExpiresIn) * time.Second)

	return a.token, nil
}

func signJWT(key *rsa.PrivateKey, claims map[string]any) (string, error) {
	header := base64URLEncode([]byte(`{"alg":"RS256","typ":"JWT"}`))
	payload, err := json.Marshal(claims)
	if err != nil {
		return "", err
	}
	payloadEnc := base64URLEncode(payload)
	signingInput := header + "." + payloadEnc

	hash := sha256.Sum256([]byte(signingInput))
	sig, err := rsa.SignPKCS1v15(rand.Reader, key, crypto.SHA256, hash[:])
	if err != nil {
		return "", err
	}

	return signingInput + "." + base64URLEncode(sig), nil
}

func base64URLEncode(data []byte) string {
	return base64.RawURLEncoding.EncodeToString(data)
}
