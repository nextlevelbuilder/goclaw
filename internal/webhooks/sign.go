// Package webhooks provides shared signing and verification helpers for webhook HMAC
// authentication. The same format is used for both inbound (verification in phase 03)
// and outbound (signing in phase 07 callback worker).
//
// Signature format: X-Webhook-Signature: t=<unix_seconds>,v1=<hex_hmac_sha256>
// Signed payload:   "<unix_seconds>.<request_body>"
// Key:              []byte(rawSecret) — the plaintext secret string (AES-decrypted
//                   from webhooks.encrypted_secret) as raw UTF-8 bytes.
package webhooks

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strconv"
)

// Sign computes X-Webhook-Signature header value for an outbound callback.
// key is []byte(rawSecret) — the AES-decrypted plaintext secret from encrypted_secret.
// ts is the Unix timestamp (seconds) to embed in the header.
// body is the request body bytes to sign.
//
// Returns the header value in format: "t=<ts>,v1=<hex>".
func Sign(key []byte, ts int64, body []byte) string {
	tsStr := strconv.FormatInt(ts, 10)
	signed := make([]byte, 0, len(tsStr)+1+len(body))
	signed = append(signed, tsStr+"."...)
	signed = append(signed, body...)

	mac := hmac.New(sha256.New, key)
	_, _ = mac.Write(signed)
	return fmt.Sprintf("t=%d,v1=%s", ts, hex.EncodeToString(mac.Sum(nil)))
}
