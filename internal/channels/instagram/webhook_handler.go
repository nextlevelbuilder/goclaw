package instagram

import (
	"crypto/hmac"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"regexp"
	"strings"
)

// WebhookHandler implements http.Handler for Instagram webhook.
type WebhookHandler struct {
	appSecret    string
	verifyToken  string
	extraSecrets []string // additional app secrets for multi-Meta-App deployments
	onMessage    func(entry WebhookEntry, event MessagingEvent)
}

// NewWebhookHandler creates a new WebhookHandler.
// appSecret is the Meta App Secret used to verify X-Hub-Signature-256 on POST deliveries.
func NewWebhookHandler(appSecret, verifyToken string) *WebhookHandler {
	return &WebhookHandler{
		appSecret:   appSecret,
		verifyToken: verifyToken,
	}
}

var hubChallengePattern = regexp.MustCompile(`^[a-zA-Z0-9_\-]{1,256}$`)

// ServeHTTP handles Instagram webhook GET (verification) and POST (event delivery).
func (wh *WebhookHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		wh.handleVerification(w, r)
	case http.MethodPost:
		wh.handleEvent(w, r)
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

// handleVerification responds to Instagram webhook verification challenge.
func (wh *WebhookHandler) handleVerification(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	if q.Get("hub.mode") != "subscribe" {
		http.Error(w, "invalid hub.mode", http.StatusForbidden)
		return
	}
	if subtle.ConstantTimeCompare([]byte(q.Get("hub.verify_token")), []byte(wh.verifyToken)) != 1 {
		slog.Warn("security.instagram_webhook_verify_token_mismatch",
			"remote_addr", r.RemoteAddr)
		http.Error(w, "invalid verify token", http.StatusForbidden)
		return
	}
	challenge := q.Get("hub.challenge")
	if !hubChallengePattern.MatchString(challenge) {
		http.Error(w, "invalid challenge", http.StatusBadRequest)
		return
	}
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte(challenge))
}

// handleEvent processes an Instagram webhook event delivery.
// Always returns 200 OK — Meta retries on non-2xx for up to 24h.
func (wh *WebhookHandler) handleEvent(w http.ResponseWriter, r *http.Request) {
	const maxBodyBytes = 4 << 20 // 4 MB
	lr := io.LimitReader(r.Body, maxBodyBytes+1)
	body, err := io.ReadAll(lr)
	if err != nil {
		slog.Warn("instagram: webhook read body error", "err", err)
		w.WriteHeader(http.StatusOK)
		return
	}
	if len(body) > maxBodyBytes {
		slog.Warn("instagram: webhook body exceeded limit, event dropped", "bytes", len(body))
		w.WriteHeader(http.StatusOK)
		return
	}

	// Verify X-Hub-Signature-256 against raw body BEFORE parsing. Instagram uses
	// the same signing scheme as Messenger/Facebook (HMAC-SHA256 of raw body with
	// the Meta App Secret).
	if wh.appSecret == "" {
		slog.Warn("security.instagram_webhook_app_secret_missing", "remote_addr", r.RemoteAddr)
		w.WriteHeader(http.StatusOK)
		return
	}
	sig := r.Header.Get("X-Hub-Signature-256")
	accepted := verifySignature(body, sig, wh.appSecret)
	if !accepted {
		for _, s := range wh.extraSecrets {
			if verifySignature(body, sig, s) {
				accepted = true
				break
			}
		}
	}
	if !accepted {
		slog.Warn("security.instagram_webhook_signature_invalid", "remote_addr", r.RemoteAddr)
		w.WriteHeader(http.StatusOK)
		return
	}

	var payload WebhookPayload
	if err := json.Unmarshal(body, &payload); err != nil {
		slog.Warn("instagram: webhook parse error", "err", err)
		w.WriteHeader(http.StatusOK)
		return
	}

	if payload.Object != "instagram" {
		w.WriteHeader(http.StatusOK)
		return
	}

	for _, entry := range payload.Entry {
		for _, event := range entry.Messaging {
			if wh.onMessage != nil {
				wh.onMessage(entry, event)
			}
		}
	}

	w.WriteHeader(http.StatusOK)
}

// verifySignature validates the X-Hub-Signature-256 header using HMAC-SHA256.
func verifySignature(body []byte, signature, secret string) bool {
	const prefix = "sha256="
	if !strings.HasPrefix(signature, prefix) {
		return false
	}
	expected, err := hex.DecodeString(signature[len(prefix):])
	if err != nil {
		return false
	}
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write(body)
	computed := mac.Sum(nil)
	return hmac.Equal(computed, expected)
}
