package providers

import (
	"context"
	"fmt"

	"golang.org/x/oauth2"
	"golang.org/x/oauth2/google"
)

const vertexCloudPlatformScope = "https://www.googleapis.com/auth/cloud-platform"

type oauth2TokenSourceAdapter struct {
	ts oauth2.TokenSource
}

func (a *oauth2TokenSourceAdapter) Token() (string, error) {
	tok, err := a.ts.Token()
	if err != nil {
		return "", err
	}
	if tok == nil || tok.AccessToken == "" {
		return "", fmt.Errorf("vertex: empty access token")
	}
	return tok.AccessToken, nil
}

// NewVertexTokenSource returns an ADC-backed token source for Vertex authentication.
func NewVertexTokenSource() (TokenSource, error) {
	creds, err := google.FindDefaultCredentials(context.Background(), vertexCloudPlatformScope)
	if err != nil {
		return nil, fmt.Errorf("vertex: ADC unavailable: %w", err)
	}
	if creds == nil || creds.TokenSource == nil {
		return nil, fmt.Errorf("vertex: ADC returned no token source")
	}
	return &oauth2TokenSourceAdapter{ts: creds.TokenSource}, nil
}
