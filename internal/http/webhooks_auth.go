package http

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"crypto/subtle"
	"database/sql"
	"encoding/hex"
	"errors"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/netip"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/nextlevelbuilder/goclaw/internal/crypto"
	"github.com/nextlevelbuilder/goclaw/internal/i18n"
	"github.com/nextlevelbuilder/goclaw/internal/store"
)

const (
	// webhookBearerPrefix is the well-known prefix for raw webhook secrets.
	// Presence allows fast rejection of non-webhook bearer tokens.
	webhookBearerPrefix = "wh_"

	// webhookHMACSkewSeconds is the maximum |now - t| allowed for HMAC timestamps.
	webhookHMACSkewSeconds = 300

	// webhookMaxBodyMessage is the body cap for /v1/webhooks/message endpoints.
	WebhookMaxBodyMessage = 256 * 1024 // 256 KB

	// webhookMaxBodyLLM is the body cap for /v1/webhooks/llm endpoints.
	WebhookMaxBodyLLM = 1024 * 1024 // 1 MB
)

// WebhookAuthMiddleware is the composed middleware chain for all /v1/webhooks/*
// runtime endpoints. Order: body cap → bearer/HMAC auth → localhost gate →
// IP allowlist → rate limit → idempotency guard → inject context → next.
//
// Parameters:
//   - ws:      WebhookStore for secret + row lookup.
//   - calls:   WebhookCallStore for idempotency checks.
//   - limiter: shared process-lifetime rate limiter (never nil).
//   - encKey:  AES-256-GCM key for decrypting encrypted_secret at HMAC verify time.
//     If "" and encrypted_secret is present, HMAC auth returns errWebhookHMACInvalid.
//   - kind:    expected webhook kind ("llm" or "message") — enforced vs row.
//   - maxBody: body size cap in bytes (use WebhookMaxBodyMessage/LLM constants).
func WebhookAuthMiddleware(
	ws store.WebhookStore,
	calls store.WebhookCallStore,
	limiter *webhookLimiter,
	encKey string,
	kind string,
	maxBody int64,
) func(http.Handler) http.Handler {
	// Shared per-handler nonce cache — process lifetime, single-node scope.
	// See docs/webhooks.md §"HMAC Replay Protection" for multi-node caveat.
	nonces := newWebhookNonceCache()

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			ctx := r.Context()
			locale := store.LocaleFromContext(ctx)

			// 1. Read and cap body — HMAC needs raw bytes, so we buffer once and
			//    restore r.Body so downstream JSON decoders see correct content.
			body, err := readLimitedBody(r, maxBody)
			if err != nil {
				slog.Warn("security.webhook.body_too_large",
					"path", r.URL.Path,
					"remote_addr", r.RemoteAddr,
				)
				writeJSON(w, http.StatusRequestEntityTooLarge, map[string]string{
					"error": i18n.T(locale, i18n.MsgWebhookBodyTooLarge),
				})
				return
			}

			// 2. Resolve webhook row via bearer or HMAC using unscoped lookups.
			//    K1: auth resolution happens BEFORE tenant is in context; we inject
			//    tenant below (step 7) so all downstream queries remain tenant-scoped.
			webhook, sig, err := resolveWebhook(r, body, ws, nonces, encKey)
			if err != nil {
				slog.Warn("security.webhook.auth_failed",
					"reason", err.Error(),
					"path", r.URL.Path,
					"remote_addr", r.RemoteAddr,
				)
				status := http.StatusUnauthorized
				msg := i18n.T(locale, i18n.MsgWebhookAuthFailed)
				// Surface specific reasons for well-defined failure modes.
				switch {
				case errors.Is(err, errWebhookRevoked):
					msg = i18n.T(locale, i18n.MsgWebhookRevoked)
				case errors.Is(err, errWebhookHMACInvalid):
					msg = i18n.T(locale, i18n.MsgWebhookHMACInvalid)
				case errors.Is(err, errWebhookTimestampSkew):
					msg = i18n.T(locale, i18n.MsgWebhookHMACTimestampSkew)
				case errors.Is(err, errWebhookBearerRequiresHMAC):
					msg = i18n.T(locale, i18n.MsgWebhookBearerRequiredHMAC)
				case errors.Is(err, errWebhookReplay):
					// Replay: still 401, but distinct log tag already emitted in resolver.
				}
				writeJSON(w, status, map[string]string{"error": msg})
				return
			}
			_ = sig // resolved sig used internally by resolveWebhook for nonce check

			// 3. Localhost-only gate (checked after auth to avoid timing oracle on
			//    the existence of localhost-only webhooks).
			if webhook.LocalhostOnly {
				if !isLoopback(r.RemoteAddr) {
					slog.Warn("security.webhook.localhost_only_violation",
						"webhook_id_hint", webhook.SecretPrefix,
						"remote_addr", r.RemoteAddr,
					)
					writeJSON(w, http.StatusForbidden, map[string]string{
						"error": i18n.T(locale, i18n.MsgWebhookLocalhostOnlyViolation),
					})
					return
				}
			}

			// 4. K7 — IP allowlist enforcement.
			//    Empty allowlist = allow all (back-compat).
			//    Entries may be single IPs or CIDRs (RFC 4632).
			//    Proxy note: X-Forwarded-For is NOT trusted — no proxy-trust config
			//    exists in this codebase (YAGNI). Use RemoteAddr only.
			if len(webhook.IPAllowlist) > 0 {
				if !ipAllowed(r.RemoteAddr, webhook.IPAllowlist) {
					slog.Warn("security.webhook.ip_denied",
						"webhook_id_hint", webhook.SecretPrefix,
						"remote_addr", r.RemoteAddr,
					)
					writeJSON(w, http.StatusForbidden, map[string]string{
						"error": i18n.T(locale, i18n.MsgWebhookIPDenied),
					})
					return
				}
			}

			// 5. Kind match — reject if caller path targets wrong kind.
			if webhook.Kind != kind {
				slog.Warn("security.webhook.kind_mismatch",
					"webhook_id_hint", webhook.SecretPrefix,
					"expected_kind", webhook.Kind,
					"requested_kind", kind,
				)
				writeJSON(w, http.StatusForbidden, map[string]string{
					"error": i18n.T(locale, i18n.MsgWebhookKindMismatch),
				})
				return
			}

			// 6. Rate limits — per-webhook then per-tenant (both must pass).
			tenantID := webhook.TenantID.String()
			webhookID := webhook.ID.String()

			if !limiter.AllowWebhook(webhookID, webhook.RateLimitPerMin) {
				slog.Warn("security.webhook.rate_limited",
					"webhook_id_hint", webhook.SecretPrefix,
					"tier", "webhook",
				)
				w.Header().Set("Retry-After", "60")
				writeJSON(w, http.StatusTooManyRequests, map[string]string{
					"error": i18n.T(locale, i18n.MsgWebhookRateLimited),
				})
				return
			}
			if !limiter.AllowTenant(tenantID) {
				slog.Warn("security.webhook.rate_limited",
					"webhook_id_hint", webhook.SecretPrefix,
					"tier", "tenant",
				)
				w.Header().Set("Retry-After", "60")
				writeJSON(w, http.StatusTooManyRequests, map[string]string{
					"error": i18n.T(locale, i18n.MsgWebhookRateLimited),
				})
				return
			}

			// 7. Idempotency check.
			proceed, _ := checkIdempotency(w, r, body, webhook.ID, calls)
			if !proceed {
				return
			}

			// 8. Inject webhook + tenant into context; propagate to stores.
			//    K1: tenant injected HERE so all store calls below are tenant-scoped.
			ctx = WithWebhookData(ctx, webhook)
			ctx = store.WithTenantID(ctx, webhook.TenantID)
			if webhook.AgentID != nil {
				ctx = store.WithAgentID(ctx, *webhook.AgentID)
			}

			// Best-effort touch — don't block on failure. Use WithoutCancel so
			// the DB write is not cancelled when the HTTP response completes.
			go func() { _ = ws.TouchLastUsed(context.WithoutCancel(r.Context()), webhook.ID) }()

			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

// ---- sentinel errors (unexported; tested via errors.Is) ----

var (
	errWebhookRevoked            = errors.New("webhook_revoked")
	errWebhookHMACInvalid        = errors.New("hmac_invalid")
	errWebhookTimestampSkew      = errors.New("hmac_timestamp_skew")
	errWebhookBearerRequiresHMAC = errors.New("bearer_requires_hmac")
	errWebhookNotFound           = errors.New("webhook_not_found")
	errWebhookReplay             = errors.New("hmac_replay")
	errWebhookIPDenied           = errors.New("ip_denied")
)

// resolveWebhook determines auth mode from headers and delegates to the
// appropriate resolver. Returns a non-nil *WebhookData on success.
// The second return value is the resolved HMAC signature hex (empty for bearer).
//
// Auth mode detection:
//   - HMAC mode: X-GoClaw-Signature header present → resolveByHMAC.
//   - Bearer mode: Authorization: Bearer wh_* → resolveByBearer.
//   - Neither → 401 (errWebhookNotFound used as catch-all).
//
// K1: uses unscoped store lookups — tenant is NOT required in ctx here.
// Tenant is injected by the caller (WebhookAuthMiddleware step 8) after resolution.
func resolveWebhook(r *http.Request, body []byte, ws store.WebhookStore, nonces *webhookNonceCache, encKey string) (*store.WebhookData, string, error) {
	sigHeader := r.Header.Get("X-GoClaw-Signature")
	authHeader := r.Header.Get("Authorization")

	if sigHeader != "" {
		// HMAC mode: need X-Webhook-Id to look up the row.
		webhookIDStr := r.Header.Get("X-Webhook-Id")
		return resolveByHMAC(r, body, ws, nonces, webhookIDStr, sigHeader, encKey)
	}

	if after, ok := strings.CutPrefix(authHeader, "Bearer "); ok {
		raw := after
		if strings.HasPrefix(raw, webhookBearerPrefix) {
			wh, err := resolveByBearer(r, raw, ws)
			return wh, "", err
		}
	}

	return nil, "", errWebhookNotFound
}

// resolveByBearer performs SHA-256 of the raw secret, then looks up the webhook
// by hash using an unscoped query (K1 fix). Rejects revoked rows and rows that
// require HMAC.
func resolveByBearer(r *http.Request, rawSecret string, ws store.WebhookStore) (*store.WebhookData, error) {
	// Always compute hash — constant-time mitigation against timing oracle on
	// "does this prefix exist" (hash computation is fixed cost).
	h := sha256.Sum256([]byte(rawSecret))
	hashHex := hex.EncodeToString(h[:])

	// K1: unscoped lookup — no tenant required in ctx at this stage.
	webhook, err := ws.GetByHashUnscoped(r.Context(), hashHex)
	if errors.Is(err, sql.ErrNoRows) || webhook == nil {
		return nil, errWebhookNotFound
	}
	if err != nil {
		return nil, errWebhookNotFound
	}
	if webhook.Revoked {
		return nil, errWebhookRevoked
	}
	if webhook.RequireHMAC {
		return nil, errWebhookBearerRequiresHMAC
	}
	return webhook, nil
}

// resolveByHMAC parses the X-GoClaw-Signature header, validates clock skew,
// looks up the webhook row by UUID using an unscoped query (K1 fix), verifies
// the HMAC, and checks the replay-nonce cache (K8).
//
// Signature format: "t=<unix_seconds>,v1=<hex_hmac_sha256>"
// Signed payload:   "<unix_seconds>.<raw_body>"
// HMAC key:        raw webhook secret (decrypted from encrypted_secret at verify time).
func resolveByHMAC(r *http.Request, body []byte, ws store.WebhookStore, nonces *webhookNonceCache, webhookIDStr, sigHeader, encKey string) (*store.WebhookData, string, error) {
	// Parse t= and v1= from header.
	ts, sig, err := parseHMACHeader(sigHeader)
	if err != nil {
		return nil, "", errWebhookHMACInvalid
	}

	// Clock-skew check before any DB lookup (cheap).
	now := time.Now().Unix()
	if abs64(now-ts) > webhookHMACSkewSeconds {
		return nil, "", errWebhookTimestampSkew
	}

	// Look up webhook by UUID using unscoped query (K1 fix).
	webhookID, uuidErr := uuid.Parse(webhookIDStr)
	if uuidErr != nil {
		return nil, "", errWebhookNotFound
	}

	// K1: unscoped lookup — no tenant required in ctx at this stage.
	webhook, err := ws.GetByIDUnscoped(r.Context(), webhookID)
	if errors.Is(err, sql.ErrNoRows) || webhook == nil {
		return nil, "", errWebhookNotFound
	}
	if err != nil {
		return nil, "", errWebhookNotFound
	}
	if webhook.Revoked {
		return nil, "", errWebhookRevoked
	}

	// K6: derive HMAC key from the decrypted raw secret (not from secret_hash bytes).
	// encrypted_secret = "" means the webhook was created before K6 and requires rotation.
	if webhook.EncryptedSecret == "" {
		slog.Warn("security.webhook.hmac_requires_rotation",
			"webhook_id_hint", webhook.SecretPrefix,
			"reason", "encrypted_secret empty — rotate webhook secret to enable HMAC auth",
		)
		return nil, "", errWebhookHMACInvalid
	}
	rawSecret, decErr := crypto.Decrypt(webhook.EncryptedSecret, encKey)
	if decErr != nil {
		slog.Error("security.webhook.hmac_decrypt_failed",
			"webhook_id_hint", webhook.SecretPrefix,
			"error", decErr,
		)
		return nil, "", errWebhookHMACInvalid
	}
	secretKeyBytes := []byte(rawSecret)

	tsStr := strconv.FormatInt(ts, 10)
	signed := append([]byte(tsStr+"."), body...)
	mac := hmac.New(sha256.New, secretKeyBytes)
	_, _ = mac.Write(signed)
	expected := mac.Sum(nil)

	// Decode caller-provided hex signature.
	callerSig, decErr := hex.DecodeString(sig)
	if decErr != nil || len(callerSig) == 0 {
		return nil, "", errWebhookHMACInvalid
	}

	// Constant-time comparison — no early exit on mismatch.
	if subtle.ConstantTimeCompare(expected, callerSig) != 1 {
		return nil, "", errWebhookHMACInvalid
	}

	// K8 — Replay nonce check. Must be after HMAC verify to avoid
	// cache poisoning by unsigned requests with arbitrary signatures.
	if nonces != nil {
		key := nonceKey(webhook.TenantID.String(), sig)
		if nonces.Seen(key) {
			slog.Warn("security.webhook.hmac_replay",
				"webhook_id_hint", webhook.SecretPrefix,
				"tenant_id", webhook.TenantID,
			)
			return nil, "", errWebhookReplay
		}
	}

	return webhook, sig, nil
}

// ipAllowed reports whether the request's remote IP matches any entry in the
// allowlist. Entries may be single IPs or CIDR ranges (RFC 4632).
// Invalid entries are logged and skipped (fail-open per entry, not per list).
// An empty allowlist always returns true (back-compat: deny-by-list must be
// explicitly configured).
//
// Proxy note: only r.RemoteAddr is consulted — X-Forwarded-For is NOT trusted
// as no proxy-trust configuration exists. Document in docs/webhooks.md.
func ipAllowed(remoteAddr string, allowlist []string) bool {
	// Strip port from RemoteAddr.
	host, _, err := net.SplitHostPort(remoteAddr)
	if err != nil {
		// remoteAddr has no port (unusual but handle gracefully).
		host = remoteAddr
	}
	clientIP := net.ParseIP(host)
	if clientIP == nil {
		// Cannot parse — deny.
		return false
	}

	for _, entry := range allowlist {
		entry = strings.TrimSpace(entry)
		if strings.Contains(entry, "/") {
			// CIDR entry.
			_, network, parseErr := net.ParseCIDR(entry)
			if parseErr != nil {
				slog.Warn("security.webhook.ip_allowlist_invalid_cidr",
					"entry", entry,
					"err", parseErr,
				)
				continue // skip malformed entry
			}
			if network.Contains(clientIP) {
				return true
			}
		} else {
			// Single IP entry.
			entryIP := net.ParseIP(entry)
			if entryIP == nil {
				slog.Warn("security.webhook.ip_allowlist_invalid_entry",
					"entry", entry,
				)
				continue // skip malformed entry
			}
			if entryIP.Equal(clientIP) {
				return true
			}
		}
	}
	return false
}

// readLimitedBody reads at most maxBytes from r.Body using http.MaxBytesReader.
// On success it replaces r.Body with a fresh NopCloser over the buffer so
// downstream JSON decoders see the same bytes. r.ContentLength is also updated.
func readLimitedBody(r *http.Request, maxBytes int64) ([]byte, error) {
	r.Body = http.MaxBytesReader(nil, r.Body, maxBytes)
	buf, err := io.ReadAll(r.Body)
	if err != nil {
		// http.MaxBytesReader returns an error when the limit is exceeded.
		return nil, err
	}
	// Restore body so downstream handlers can decode it.
	r.Body = io.NopCloser(bytes.NewReader(buf))
	r.ContentLength = int64(len(buf))
	return buf, nil
}

// parseHMACHeader splits "t=<unix>,v1=<hex>" into (timestamp, hexSig, error).
func parseHMACHeader(header string) (int64, string, error) {
	var ts int64
	var sig string
	for part := range strings.SplitSeq(header, ",") {
		part = strings.TrimSpace(part)
		switch {
		case strings.HasPrefix(part, "t="):
			v, err := strconv.ParseInt(strings.TrimPrefix(part, "t="), 10, 64)
			if err != nil {
				return 0, "", errors.New("invalid t= field")
			}
			ts = v
		case strings.HasPrefix(part, "v1="):
			sig = strings.TrimPrefix(part, "v1=")
		}
	}
	if ts == 0 || sig == "" {
		return 0, "", errors.New("missing t= or v1= field")
	}
	return ts, sig, nil
}

// isLoopback reports whether the RemoteAddr is a loopback address.
// Uses netip.ParseAddrPort for correct IPv4/IPv6 handling (not string prefix).
func isLoopback(remoteAddr string) bool {
	ap, err := netip.ParseAddrPort(remoteAddr)
	if err != nil {
		// Fall back: try parsing as bare address (no port).
		a, err2 := netip.ParseAddr(remoteAddr)
		if err2 != nil {
			return false
		}
		return a.IsLoopback()
	}
	return ap.Addr().IsLoopback()
}

// abs64 returns the absolute value of x.
func abs64(x int64) int64 {
	if x < 0 {
		return -x
	}
	return x
}
