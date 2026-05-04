// Package oa implements the Zalo Official Account channel
// (OAuth v4 — oauth.zaloapp.com + openapi.zalo.me).
package oa

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/google/uuid"

	"github.com/nextlevelbuilder/goclaw/internal/store"
)

// ChannelCreds is the plaintext credentials JSON stored inside the
// channel_instances.credentials BLOB. The store layer encrypts the whole
// blob — do NOT field-level encrypt.
type ChannelCreds struct {
	AppID     string `json:"app_id"`
	SecretKey string `json:"secret_key"`
	OAID      string `json:"oa_id,omitempty"`

	// RedirectURI must match the URL registered on the Zalo dev console;
	// otherwise Zalo returns error_code=-14003 "Invalid redirect uri".
	RedirectURI string `json:"redirect_uri,omitempty"`

	// WebhookSecretKey is the signing secret from the Zalo dev console
	// (OA → Webhook). Distinct from SecretKey (OAuth v4). Used to verify
	// X-ZEvent-Signature headers when Transport=webhook.
	WebhookSecretKey string `json:"webhook_secret_key,omitempty"`

	AccessToken           string    `json:"access_token,omitempty"`
	RefreshToken          string    `json:"refresh_token,omitempty"`
	ExpiresAt             time.Time `json:"expires_at"`
	RefreshTokenExpiresAt time.Time `json:"refresh_token_expires_at,omitempty"`
	LastRefreshAt         time.Time `json:"last_refresh_at"`
}

// LoadCreds parses plaintext credentials JSON.
func LoadCreds(raw json.RawMessage) (*ChannelCreds, error) {
	var c ChannelCreds
	if err := json.Unmarshal(raw, &c); err != nil {
		return nil, err
	}
	return &c, nil
}

// Marshal returns plaintext JSON; store layer re-encrypts on Update.
func (c *ChannelCreds) Marshal() (json.RawMessage, error) {
	return json.Marshal(c)
}

// WithTokens copies new tokens onto the receiver and stamps LastRefreshAt.
// Preserves a previously set RefreshTokenExpiresAt if Zalo omits the field on
// this particular response — a one-time omission must not blank the deadline.
func (c *ChannelCreds) WithTokens(tok *Tokens) {
	c.AccessToken = tok.AccessToken
	c.RefreshToken = tok.RefreshToken
	c.ExpiresAt = tok.ExpiresAt
	if !tok.RefreshTokenExpiresAt.IsZero() {
		c.RefreshTokenExpiresAt = tok.RefreshTokenExpiresAt
	}
	c.LastRefreshAt = time.Now().UTC()
}

// Persist writes the plaintext creds blob; store layer re-encrypts on Update.
func Persist(ctx context.Context, s store.ChannelInstanceStore, id uuid.UUID, c *ChannelCreds) error {
	if s == nil {
		return fmt.Errorf("zalo_oa: nil ChannelInstanceStore in Persist")
	}
	if id == uuid.Nil {
		return fmt.Errorf("zalo_oa: nil instance ID in Persist")
	}
	blob, err := c.Marshal()
	if err != nil {
		return fmt.Errorf("zalo_oa: marshal creds: %w", err)
	}
	return s.Update(ctx, id, map[string]any{"credentials": []byte(blob)})
}
