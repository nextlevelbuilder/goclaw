package facebook

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"regexp"
	"strings"
)

// WebhookHandler implements http.Handler for the Facebook webhook endpoint.
// Handles both the GET verification challenge and POST event delivery.
type WebhookHandler struct {
	appSecret    string
	verifyToken  string
	extraSecrets []string // additional app secrets for multi-Meta-App deployments
	onComment    func(entry WebhookEntry, change ChangeValue)
	onMessage    func(entry WebhookEntry, event MessagingEvent)
}

// NewWebhookHandler creates a new WebhookHandler.
func NewWebhookHandler(appSecret, verifyToken string) *WebhookHandler {
	return &WebhookHandler{
		appSecret:   appSecret,
		verifyToken: verifyToken,
	}
}

// hubChallengePattern validates that hub.challenge is safe to reflect.
var hubChallengePattern = regexp.MustCompile(`^[a-zA-Z0-9_\-]{1,256}$`)

// ServeHTTP handles Facebook webhook GET (verification) and POST (event delivery).
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

// handleVerification responds to Facebook's webhook verification challenge.
func (wh *WebhookHandler) handleVerification(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	if q.Get("hub.mode") != "subscribe" {
		http.Error(w, "invalid hub.mode", http.StatusForbidden)
		return
	}
	if q.Get("hub.verify_token") != wh.verifyToken {
		slog.Warn("security.facebook_webhook_verify_token_mismatch",
			"remote_addr", r.RemoteAddr)
		http.Error(w, "invalid verify token", http.StatusForbidden)
		return
	}
	challenge := q.Get("hub.challenge")
	// Validate challenge before reflecting to prevent injection if Content-Type changes.
	if !hubChallengePattern.MatchString(challenge) {
		http.Error(w, "invalid challenge", http.StatusBadRequest)
		return
	}
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte(challenge))
}

// handleEvent processes a Facebook webhook event delivery.
// Always returns 200 OK — Facebook retries on non-2xx for 24h.
func (wh *WebhookHandler) handleEvent(w http.ResponseWriter, r *http.Request) {
	const maxBodyBytes = 4 << 20 // 4 MB
	lr := io.LimitReader(r.Body, maxBodyBytes+1)
	body, err := io.ReadAll(lr)
	if err != nil {
		slog.Warn("facebook: webhook read body error", "err", err)
		w.WriteHeader(http.StatusOK) // 200 so Facebook stops retrying a bad delivery
		return
	}
	if len(body) > maxBodyBytes {
		slog.Warn("facebook: webhook body exceeded limit, event dropped", "bytes", len(body))
		w.WriteHeader(http.StatusOK)
		return
	}

	sig := r.Header.Get("X-Hub-Signature-256")
	verified := verifySignature(body, sig, wh.appSecret)
	if !verified {
		// Try extra secrets (multi-Meta-App deployments share one webhook endpoint).
		for _, s := range wh.extraSecrets {
			if verifySignature(body, sig, s) {
				verified = true
				break
			}
		}
	}
	if !verified {
		slog.Warn("security.facebook_webhook_signature_invalid", "remote_addr", r.RemoteAddr)
		w.WriteHeader(http.StatusOK) // return 200 so Facebook stops sending
		return
	}

	var payload WebhookPayload
	if err := json.Unmarshal(body, &payload); err != nil {
		slog.Warn("facebook: webhook parse error", "err", err)
		w.WriteHeader(http.StatusOK)
		return
	}

	if payload.Object != "page" {
		w.WriteHeader(http.StatusOK)
		return
	}

	for _, entry := range payload.Entry {
		// Feed events (comments, posts).
		for _, change := range entry.Changes {
			if change.Field == "feed" && change.Value.Item == "comment" {
				if wh.onComment != nil {
					wh.onComment(entry, change.Value)
				}
			}
		}
		// Messenger events.
		for _, event := range entry.Messaging {
			if wh.onMessage != nil {
				wh.onMessage(entry, event)
			}
		}
	}

	w.WriteHeader(http.StatusOK)
}

// verifySignature validates the X-Hub-Signature-256 header using HMAC-SHA256.
// Facebook sends "sha256=<hex_digest>"; we recompute and compare.
func verifySignature(body []byte, signature, appSecret string) bool {
	const prefix = "sha256="
	if !strings.HasPrefix(signature, prefix) {
		return false
	}
	expected, err := hex.DecodeString(signature[len(prefix):])
	if err != nil {
		return false
	}
	mac := hmac.New(sha256.New, []byte(appSecret))
	mac.Write(body)
	computed := mac.Sum(nil)
	return hmac.Equal(computed, expected)
}
