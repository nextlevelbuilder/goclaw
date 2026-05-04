package oa

import (
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strconv"
	"time"

	"github.com/nextlevelbuilder/goclaw/internal/channels/zalo/common"
)

// Webhook signature scheme:
//   X-ZEvent-Signature = hex(SHA256(appID + rawBody + timestamp + secret))
//
// timestamp is read via json.Number → strconv.FormatInt so scientific-notation
// inputs round-trip to the canonical decimal Zalo signed against.

const (
	zaloOASignatureHeader   = "X-ZEvent-Signature"
	defaultReplayWindow     = 5 * time.Minute
	tsMillisecondsThreshold = int64(1e12) // ~year 2001 in ms; below = seconds
)

// SignatureMode controls verifier behavior; empty/unknown → disabled.
// Defaulting to disabled keeps onboarding frictionless — operators can
// opt into strict (or log_only during migration) once they've pasted
// the OA Secret Key into Credentials.
type SignatureMode = string

const (
	SignatureModeStrict   SignatureMode = "strict"
	SignatureModeLogOnly  SignatureMode = "log_only"
	SignatureModeDisabled SignatureMode = "disabled"
)

func normalizeMode(m string) string {
	switch m {
	case SignatureModeStrict, SignatureModeLogOnly, SignatureModeDisabled:
		return m
	default:
		return SignatureModeDisabled
	}
}

func computeOASignature(appID, body, timestamp, secret string) string {
	h := sha256.New()
	h.Write([]byte(appID))
	h.Write([]byte(body))
	h.Write([]byte(timestamp))
	h.Write([]byte(secret))
	return hex.EncodeToString(h.Sum(nil))
}

// oaSignatureVerifier validates X-ZEvent-Signature.
// Modes: strict / log_only / disabled.
type oaSignatureVerifier struct {
	appID        string
	secret       string
	mode         SignatureMode
	replayWindow time.Duration
}

func newOASignatureVerifier(appID, secret, mode string, replayWindow time.Duration) *oaSignatureVerifier {
	return &oaSignatureVerifier{
		appID:        appID,
		secret:       secret,
		mode:         normalizeMode(mode),
		replayWindow: replayWindow,
	}
}

func (v *oaSignatureVerifier) Verify(headers http.Header, body []byte) error {
	if v.mode == SignatureModeDisabled {
		slog.Warn("security.zalo_oa_webhook_unsigned_accept", "reason", "signature_mode=disabled")
		return nil
	}
	if v.secret == "" {
		return errors.New("zalo_oa.webhook: secret unset (open webhook is not allowed)")
	}

	tsInt, err := extractTimestamp(body)
	if err != nil {
		if v.mode == SignatureModeLogOnly {
			// log_only accepts: signature can't be recomputed without a parseable timestamp.
			slog.Warn("security.zalo_oa_webhook_bad_timestamp_log_only", "err", err)
			return nil
		}
		return err
	}
	tsStr := strconv.FormatInt(tsInt, 10) // canonical decimal

	if rejErr := v.checkReplayWindow(tsInt); rejErr != nil {
		return rejErr
	}

	sig := headers.Get(zaloOASignatureHeader)
	if sig == "" {
		if v.mode == SignatureModeLogOnly {
			slog.Warn("security.zalo_oa_webhook_missing_sig_log_only")
			return nil
		}
		return fmt.Errorf("zalo_oa.webhook: missing %s", zaloOASignatureHeader)
	}
	expected := computeOASignature(v.appID, string(body), tsStr, v.secret)

	// Reject length mismatch up front; ConstantTimeCompare's len path
	// isn't documented as constant-time.
	if len(sig) != len(expected) {
		if v.mode == SignatureModeLogOnly {
			slog.Warn("security.zalo_oa_webhook_sig_len_mismatch_log_only",
				"got_len", len(sig), "want_len", len(expected))
			return nil
		}
		return common.ErrSignatureMismatch
	}
	if subtle.ConstantTimeCompare([]byte(sig), []byte(expected)) != 1 {
		if v.mode == SignatureModeLogOnly {
			// Never log any part of `expected` — it's secret-keyed.
			slog.Warn("security.zalo_oa_webhook_sig_mismatch_log_only", "got", sig)
			return nil
		}
		return common.ErrSignatureMismatch
	}
	return nil
}

// extractTimestamp reads the top-level timestamp field via json.Number to
// preserve canonical-decimal round-trip on scientific-notation inputs.
func extractTimestamp(body []byte) (int64, error) {
	var env struct {
		Timestamp json.Number `json:"timestamp"`
	}
	if err := json.Unmarshal(body, &env); err != nil {
		return 0, fmt.Errorf("zalo_oa.webhook: decode timestamp: %w", err)
	}
	tsInt, err := env.Timestamp.Int64()
	if err != nil {
		return 0, fmt.Errorf("zalo_oa.webhook: timestamp not integer: %w", err)
	}
	return tsInt, nil
}

// checkReplayWindow rejects events whose timestamp is outside replayWindow.
// Detects ms vs s by magnitude (Zalo uses ms; older API used s).
func (v *oaSignatureVerifier) checkReplayWindow(tsInt int64) error {
	if v.replayWindow <= 0 {
		return nil
	}
	var eventTime time.Time
	if tsInt < tsMillisecondsThreshold {
		eventTime = time.Unix(tsInt, 0)
	} else {
		eventTime = time.UnixMilli(tsInt)
	}
	skew := time.Since(eventTime)
	if skew > v.replayWindow || skew < -v.replayWindow {
		if v.mode == SignatureModeLogOnly {
			// Don't log skew direction/magnitude — it's a clock-skew oracle
			// for a probing attacker.
			slog.Warn("security.zalo_oa_webhook_replay_log_only",
				"reason", "outside replay window")
			return nil
		}
		return fmt.Errorf("event timestamp outside replay window: skew=%v, window=±%v", skew, v.replayWindow)
	}
	return nil
}

// clampReplayWindowSeconds clamps to [60, 3600]; 0 → defaultReplayWindow.
func clampReplayWindowSeconds(s int) time.Duration {
	switch {
	case s <= 0:
		return defaultReplayWindow
	case s < 60:
		return 60 * time.Second
	case s > 3600:
		return 3600 * time.Second
	default:
		return time.Duration(s) * time.Second
	}
}
