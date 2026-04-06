package teams

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"strings"
)

const maxWebhookBodySize = 1 << 20 // 1MB — Bot Framework activities are typically <10KB

// handleWebhook processes incoming Bot Framework webhook requests.
// Bot Framework expects a quick 200 OK — agent processing is async via bus.
func (c *Channel) handleWebhook(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	if !c.IsRunning() {
		w.WriteHeader(http.StatusServiceUnavailable)
		return
	}

	// Validate JWT from Authorization header
	authHeader := r.Header.Get("Authorization")
	token := extractBearerToken(authHeader)
	if token == "" {
		slog.Warn("teams: missing authorization header", "remote", r.RemoteAddr)
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	if err := c.validator.Validate(token); err != nil {
		slog.Warn("teams: JWT validation failed", "error", err, "remote", r.RemoteAddr)
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}

	// Parse Activity from body (size-limited to prevent DoS)
	r.Body = http.MaxBytesReader(w, r.Body, maxWebhookBodySize)
	var activity Activity
	if err := json.NewDecoder(r.Body).Decode(&activity); err != nil {
		slog.Warn("teams: invalid activity JSON", "error", err)
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}

	// Route by activity type
	switch activity.Type {
	case "message":
		c.handleMessage(activity)
	case "conversationUpdate":
		c.handleConversationUpdate(activity)
	default:
		slog.Debug("teams: ignored activity type", "type", activity.Type)
	}

	// Always return 200 OK immediately — agent processing is async
	w.WriteHeader(http.StatusOK)
}

// handleMessage processes an incoming message activity.
func (c *Channel) handleMessage(activity Activity) {
	// Validate required fields
	if activity.From.ID == "" {
		slog.Warn("teams: message with empty sender ID", "activity_id", activity.ID)
		return
	}
	if activity.Conversation.ID == "" {
		slog.Warn("teams: message with empty conversation ID", "activity_id", activity.ID)
		return
	}

	text := strings.TrimSpace(activity.Text)
	if text == "" {
		return
	}

	// Remove bot mention prefix from group messages (Teams adds "<at>BotName</at> " prefix)
	text = stripBotMention(text)
	if text == "" {
		return
	}

	// Determine peer kind from Activity's conversationType field.
	// Teams provides: "personal", "groupChat", or "channel".
	peerKind := "direct"
	switch activity.Conversation.ConversationType {
	case "groupChat", "channel":
		peerKind = "group"
	}

	// Enforce DM/Group policy before processing
	if !c.CheckPolicy(peerKind, c.cfg.DMPolicy, c.cfg.GroupPolicy, activity.From.ID) {
		slog.Debug("teams: message rejected by policy",
			"peer_kind", peerKind, "sender", activity.From.ID)
		return
	}

	// Store serviceURL only after policy check passes
	c.storeServiceURL(activity.Conversation.ID, activity.ServiceURL)

	// Start typing indicator while agent processes
	c.startTyping(activity.Conversation.ID)

	// Build metadata
	metadata := map[string]string{
		"activity_id":     activity.ID,
		"platform":        "teams",
		"service_url":     activity.ServiceURL,
		"sender_name":     activity.From.Name,
		"conversation_id": activity.Conversation.ID,
	}

	slog.Info("teams: received message",
		"from", activity.From.ID,
		"conversation", activity.Conversation.ID,
		"peer_kind", peerKind,
	)

	// Publish via BaseChannel.HandleMessage → bus
	c.BaseChannel.HandleMessage(
		activity.From.ID,
		activity.Conversation.ID,
		text,
		nil, // no media (text-only first)
		metadata,
		peerKind,
	)
}

// handleConversationUpdate logs member additions/removals.
func (c *Channel) handleConversationUpdate(activity Activity) {
	for _, m := range activity.MembersAdded {
		slog.Info("teams: member added", "id", m.ID, "name", m.Name, "conversation", activity.Conversation.ID)
	}
	for _, m := range activity.MembersRemoved {
		slog.Info("teams: member removed", "id", m.ID, "name", m.Name, "conversation", activity.Conversation.ID)
	}
	// Track serviceURL for future replies (validated against SSRF)
	if activity.ServiceURL != "" && activity.Conversation.ID != "" {
		c.storeServiceURL(activity.Conversation.ID, activity.ServiceURL)
	}
}

// stripBotMention removes Teams bot mention tags like "<at>BotName</at>" from message text.
func stripBotMention(text string) string {
	for {
		start := strings.Index(text, "<at>")
		if start == -1 {
			break
		}
		end := strings.Index(text[start:], "</at>")
		if end == -1 {
			break
		}
		end += start // convert to absolute index
		text = text[:start] + text[end+len("</at>"):]
	}
	return strings.TrimSpace(text)
}
