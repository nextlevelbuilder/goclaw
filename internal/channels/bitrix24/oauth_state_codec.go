package bitrix24

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"strings"
	"time"

	"github.com/nextlevelbuilder/goclaw/internal/crypto"
)

// oauthStateTTL bounds how long a signed authorize-URL state stays valid.
// Long enough for a user to open the DM and click through Bitrix's approve
// page; short enough that a leaked link (log line, browser history) has a
// tight blast radius. Chosen during design brainstorm (design.md §9 Q2).
const oauthStateTTL = 10 * time.Minute

// oauthStatePayload is the identity + routing context carried through the
// Bitrix OAuth redirect round-trip. Stateless by design: no DB row to
// insert/delete/janitor — the state itself is self-verifying (HMAC + embedded
// expiry), so decodeOAuthState can reject garbage before any DB or Bitrix
// network call.
type oauthStatePayload struct {
	UserID   string `json:"u"`
	TenantID string `json:"t"`
	// BotID routes the callback back to the right Channel instance via
	// Router.DispatcherByBotID — Bitrix's redirect carries no bot_id of its
	// own, so it must be embedded here at BuildUserAuthorizeURL time.
	BotID       int    `json:"b"`
	ChannelName string `json:"c"` // logging only — BotID is the actual routing key
	Domain      string `json:"d"`
	DialogID    string `json:"dlg"`
	ExpiresAt   int64  `json:"exp"` // unix seconds
}

// encodeOAuthState serializes payload to base64url(json) + "." + hex(HMAC-SHA256).
// key comes from crypto.DeriveKey(GOCLAW_ENCRYPTION_KEY) — same key already used
// for AES-256-GCM elsewhere in this codebase (no second secret introduced).
func encodeOAuthState(p oauthStatePayload, key []byte) (string, error) {
	body, err := json.Marshal(p)
	if err != nil {
		return "", fmt.Errorf("bitrix24 oauth state: marshal payload: %w", err)
	}
	encoded := base64.RawURLEncoding.EncodeToString(body)
	sig := signOAuthStatePart(encoded, key)
	return encoded + "." + hex.EncodeToString(sig), nil
}

// decodeOAuthState verifies the HMAC signature (constant-time) and expiry
// BEFORE returning the payload, so a tampered or expired state never reaches
// a caller that might act on it (e.g. attempt a Bitrix token exchange).
func decodeOAuthState(state string, key []byte) (*oauthStatePayload, error) {
	idx := strings.LastIndex(state, ".")
	if idx < 0 {
		return nil, errors.New("bitrix24 oauth state: malformed (missing signature)")
	}
	encoded, sigHex := state[:idx], state[idx+1:]

	gotSig, err := hex.DecodeString(sigHex)
	if err != nil {
		return nil, errors.New("bitrix24 oauth state: malformed signature encoding")
	}
	wantSig := signOAuthStatePart(encoded, key)
	if !hmac.Equal(gotSig, wantSig) {
		return nil, errors.New("bitrix24 oauth state: signature mismatch")
	}

	body, err := base64.RawURLEncoding.DecodeString(encoded)
	if err != nil {
		return nil, fmt.Errorf("bitrix24 oauth state: decode payload: %w", err)
	}
	var p oauthStatePayload
	if err := json.Unmarshal(body, &p); err != nil {
		return nil, fmt.Errorf("bitrix24 oauth state: unmarshal payload: %w", err)
	}
	if time.Now().Unix() > p.ExpiresAt {
		return nil, errors.New("bitrix24 oauth state: expired")
	}
	return &p, nil
}

// signOAuthStatePart computes HMAC-SHA256(encodedPayload, key).
func signOAuthStatePart(encodedPayload string, key []byte) []byte {
	mac := hmac.New(sha256.New, key)
	mac.Write([]byte(encodedPayload))
	return mac.Sum(nil)
}

// BuildUserAuthorizeURL builds the Bitrix `/oauth/authorize/` link the DM
// invite (handle.go, sendOAuthInvite) points at.
//
// No `scope` parameter — the portal always grants the app's registered scope
// in full (confirmed against official Bitrix OAuth docs, design.md §9 — no
// way to request a narrower scope per call).
//
// No `redirect_uri` parameter either — Local Apps ignore it. Confirmed
// against live behavior: Bitrix always redirects back to the app's
// registered "Application URL" (handlerPath, /bitrix24/handler,
// webhook.go handleAppPage) regardless of what's passed here. Passing it
// anyway would be dead weight (design.md §12 changelog documents the
// correction) — omitted so a future reader doesn't assume it's honored.
func (c *Channel) BuildUserAuthorizeURL(userID, dialogID string) (string, error) {
	portal := c.Portal()
	if portal == nil {
		return "", errors.New("bitrix24 oauth: portal not available")
	}
	keyBytes, err := crypto.DeriveKey(c.encKey)
	if err != nil {
		return "", fmt.Errorf("bitrix24 oauth: derive state key: %w", err)
	}

	state, err := encodeOAuthState(oauthStatePayload{
		UserID:      userID,
		TenantID:    c.TenantID().String(),
		BotID:       c.BotID(),
		ChannelName: c.Name(),
		Domain:      portal.Domain(),
		DialogID:    dialogID,
		ExpiresAt:   time.Now().Add(oauthStateTTL).Unix(),
	}, keyBytes)
	if err != nil {
		return "", err
	}

	q := url.Values{}
	q.Set("client_id", portal.creds.ClientID)
	q.Set("state", state)
	return "https://" + portal.Domain() + "/oauth/authorize/?" + q.Encode(), nil
}
