package pancake

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"sync"
)

const (
	webhookPath  = "/channels/pancake/webhook"
	maxBodyBytes = 1 << 20 // 1 MB — prevent abuse
)

// verifyHMAC verifies a Pancake HMAC-SHA256 signature.
// Expected header format: "sha256=<hex-digest>"
func verifyHMAC(body []byte, secret, signature string) bool {
	const prefix = "sha256="
	if len(signature) <= len(prefix) {
		return false
	}
	got, err := hex.DecodeString(signature[len(prefix):])
	if err != nil {
		return false
	}
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write(body)
	expected := mac.Sum(nil)
	return hmac.Equal(got, expected)
}

// --- Global webhook router for multi-page support ---

// webhookRouter routes incoming Pancake webhook events to the correct channel instance by page_id.
// A single HTTP handler is shared across all pancake channel instances.
type webhookRouter struct {
	mu           sync.RWMutex
	instances    map[string]*Channel // pageID → channel
	routeHandled bool                // true after first webhookRoute() call
}

var globalRouter = &webhookRouter{
	instances: make(map[string]*Channel),
}

func (r *webhookRouter) register(ch *Channel) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.instances[ch.pageID] = ch
	if ch.webhookPageID != "" && ch.webhookPageID != ch.pageID {
		r.instances[ch.webhookPageID] = ch
	}
}

func (r *webhookRouter) unregister(pageID string, webhookPageID string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	delete(r.instances, pageID)
	if webhookPageID != "" && webhookPageID != pageID {
		delete(r.instances, webhookPageID)
	}
}

// webhookRoute returns the path+handler on first call; ("", nil) for subsequent calls.
// The HTTP mux retains the route once registered — routeHandled prevents duplicate mounts.
func (r *webhookRouter) webhookRoute() (string, http.Handler) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if !r.routeHandled {
		r.routeHandled = true
		return webhookPath, r
	}
	return "", nil
}

// ServeHTTP is the shared handler for all Pancake page webhooks.
// Always returns HTTP 200 — Pancake suspends webhooks if >80% errors in a 30-min window.
func (r *webhookRouter) ServeHTTP(w http.ResponseWriter, req *http.Request) {
	slog.Info("pancake: webhook request received",
		"method", req.Method,
		"remote_addr", req.RemoteAddr,
		"content_length", req.ContentLength)

	if req.Method != http.MethodPost {
		w.WriteHeader(http.StatusOK)
		return
	}

	lr := io.LimitReader(req.Body, maxBodyBytes+1)
	body, err := io.ReadAll(lr)
	if err != nil {
		slog.Warn("pancake: router read body error", "err", err)
		w.WriteHeader(http.StatusOK)
		return
	}

	slog.Debug("pancake: webhook body received",
		"body_len", len(body),
		"body_preview", truncateBody(body, 1000))

	// Parse top-level envelope.
	var event WebhookEvent
	if err := json.Unmarshal(body, &event); err != nil {
		slog.Warn("pancake: router parse event error", "err", err, "body_preview", truncateBody(body, 300))
		w.WriteHeader(http.StatusOK)
		return
	}

	// Parse the nested "data" object containing conversation + message.
	var data WebhookData
	if err := json.Unmarshal(event.Data, &data); err != nil {
		slog.Warn("pancake: router parse data error", "err", err)
		w.WriteHeader(http.StatusOK)
		return
	}

	// Resolve page_id: top-level field takes priority, then data-level, then first conv ID segment.
	pageID := event.PageID
	if pageID == "" {
		pageID = data.PageID
	}
	if pageID == "" {
		// Last resort: extract from conversation ID (format: pageID_senderID for INBOX events).
		if idx := strings.Index(data.Conversation.ID, "_"); idx > 0 {
			pageID = data.Conversation.ID[:idx]
		}
	}

	// Resolve conversation type.
	convType := strings.ToUpper(data.Conversation.Type)

	if event.EventType != "" && !strings.EqualFold(event.EventType, "messaging") {
		slog.Debug("pancake: skipping non-messaging webhook event",
			"event_type", event.EventType,
			"page_id", pageID)
		w.WriteHeader(http.StatusOK)
		return
	}

	slog.Debug("pancake: webhook event parsed",
		"event_type", event.EventType,
		"resolved_page_id", pageID,
		"conv_id", data.Conversation.ID,
		"conv_type", convType,
		"sender_id", data.Conversation.From.ID,
		"msg_id", data.Message.ID)

	if pageID == "" {
		slog.Warn("pancake: could not determine page_id from webhook payload",
			"event_page_id", event.PageID,
			"data_page_id", data.PageID,
			"conv_id", data.Conversation.ID)
		w.WriteHeader(http.StatusOK)
		return
	}
	r.mu.RLock()
	target := r.instances[pageID]
	r.mu.RUnlock()

	if target == nil {
		r.mu.RLock()
		var registered []string
		for pid := range r.instances {
			registered = append(registered, pid)
		}
		r.mu.RUnlock()
		slog.Warn("pancake: no channel instance for page_id",
			"page_id", pageID,
			"event_page_id", event.PageID,
			"data_page_id", data.PageID,
			"conv_id", data.Conversation.ID,
			"registered_pages", registered)
		w.WriteHeader(http.StatusOK)
		return
	}

	// HMAC signature verification — skip if webhook_secret not configured.
	if target.webhookSecret != "" {
		sig := req.Header.Get("X-Pancake-Signature")
		if !verifyHMAC(body, target.webhookSecret, sig) {
			slog.Warn("security.pancake_webhook_signature_mismatch",
				"page_id", pageID,
				"remote_addr", req.RemoteAddr)
			w.WriteHeader(http.StatusOK)
			return
		}
	}

	// Build normalized MessagingData from actual Pancake payload.
	msgContent := data.Message.Message
	if msgContent == "" {
		msgContent = data.Message.OriginalMessage
	}
	if msgContent == "" {
		msgContent = data.Message.Content
	}
	if msgContent == "" {
		msgContent = data.Conversation.Snippet
	}

	senderID := data.Conversation.From.ID
	senderName := data.Conversation.From.Name
	if data.Message.From != nil {
		if data.Message.From.ID != "" {
			senderID = data.Message.From.ID
		}
		if data.Message.From.Name != "" {
			senderName = data.Message.From.Name
		}
	}

	normalized := MessagingData{
		PageID:         pageID,
		ConversationID: data.Conversation.ID,
		PostID:         data.Conversation.PostID, // present for COMMENT events
		Type:           convType,
		Platform:       target.platform,
		AssigneeIDs:    append([]string(nil), data.Conversation.AssigneeIDs...),
		Message: MessagingMessage{
			ID:          data.Message.ID,
			Content:     msgContent,
			SenderID:    senderID,
			SenderName:  senderName,
			Attachments: data.Message.Attachments,
		},
	}

	// Route by conversation type.
	switch convType {
	case "INBOX":
		target.handleMessagingEvent(normalized)
	case "COMMENT":
		target.handleCommentEvent(normalized)
	default:
		slog.Debug("pancake: skipping unknown conversation type",
			"page_id", pageID, "conv_type", convType)
	}
	w.WriteHeader(http.StatusOK)
}

// truncateBody returns a string preview of body, truncated to maxLen bytes.
func truncateBody(body []byte, maxLen int) string {
	if len(body) <= maxLen {
		return string(body)
	}
	return string(body[:maxLen]) + "..."
}
