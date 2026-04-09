package googlechat

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"sync"
	"time"
)

// chatEvent is the parsed representation of a Google Chat event from Pub/Sub.
type chatEvent struct {
	Type        string           // MESSAGE, ADDED_TO_SPACE, REMOVED_FROM_SPACE, etc.
	SenderID    string           // users/{userId}
	SenderName  string           // display name
	SpaceID     string           // spaces/{spaceId}
	SpaceType   string           // DM, SPACE, ROOM
	PeerKind    string           // "direct" or "group"
	Text        string           // message text
	MessageName string           // spaces/{spaceId}/messages/{messageId}
	ThreadName  string           // spaces/{spaceId}/threads/{threadId}
	Attachments []chatAttachment // file attachments
}

// chatAttachment represents a file attachment in a Google Chat message.
type chatAttachment struct {
	Name         string // attachment resource name
	ContentType  string
	ResourceName string // for download via media API
}

// parseEvent parses a base64-encoded Pub/Sub message data into a chatEvent.
func parseEvent(encodedData string) (*chatEvent, error) {
	data, err := base64.StdEncoding.DecodeString(encodedData)
	if err != nil {
		return nil, fmt.Errorf("base64 decode: %w", err)
	}
	if len(data) == 0 {
		return nil, fmt.Errorf("empty event data")
	}

	var raw struct {
		Type    string `json:"type"`
		Message struct {
			Name   string `json:"name"`
			Text   string `json:"text"`
			Sender struct {
				Name        string `json:"name"`
				DisplayName string `json:"displayName"`
			} `json:"sender"`
			Thread struct {
				Name string `json:"name"`
			} `json:"thread"`
			Attachment []struct {
				Name            string `json:"name"`
				ContentType     string `json:"contentType"`
				AttachmentDataRef struct {
					ResourceName string `json:"resourceName"`
				} `json:"attachmentDataRef"`
			} `json:"attachment"`
		} `json:"message"`
		Space struct {
			Name string `json:"name"`
			Type string `json:"type"`
		} `json:"space"`
		User struct {
			Name string `json:"name"`
		} `json:"user"`
	}

	if err := json.Unmarshal(data, &raw); err != nil {
		return nil, fmt.Errorf("parse chat event: %w", err)
	}

	evt := &chatEvent{
		Type:      raw.Type,
		SpaceID:   raw.Space.Name,
		SpaceType: raw.Space.Type,
	}

	// Determine peer kind
	switch raw.Space.Type {
	case "DM":
		evt.PeerKind = "direct"
	default: // SPACE, ROOM
		evt.PeerKind = "group"
	}

	// Extract sender
	if raw.Type == "MESSAGE" {
		if raw.Message.Sender.Name == "" {
			return nil, fmt.Errorf("MESSAGE event missing sender")
		}
		evt.SenderID = raw.Message.Sender.Name
		evt.SenderName = raw.Message.Sender.DisplayName
		evt.Text = raw.Message.Text
		evt.MessageName = raw.Message.Name
		evt.ThreadName = raw.Message.Thread.Name

		// Parse attachments
		for _, att := range raw.Message.Attachment {
			evt.Attachments = append(evt.Attachments, chatAttachment{
				Name:         att.Name,
				ContentType:  att.ContentType,
				ResourceName: att.AttachmentDataRef.ResourceName,
			})
		}
	} else if raw.Type == "ADDED_TO_SPACE" || raw.Type == "REMOVED_FROM_SPACE" {
		evt.SenderID = raw.User.Name
	}

	return evt, nil
}

// dedupCache is a thread-safe cache for Pub/Sub message deduplication.
type dedupCache struct {
	mu      sync.Mutex
	entries map[string]time.Time
	ttl     time.Duration
}

func newDedupCache(ttl time.Duration) *dedupCache {
	return &dedupCache{
		entries: make(map[string]time.Time),
		ttl:     ttl,
	}
}

// seen returns true if the messageID was already processed.
func (d *dedupCache) seen(messageID string) bool {
	d.mu.Lock()
	defer d.mu.Unlock()

	if t, ok := d.entries[messageID]; ok {
		if time.Since(t) < d.ttl {
			return true
		}
		delete(d.entries, messageID)
	}
	return false
}

// add marks a messageID as processed.
func (d *dedupCache) add(messageID string) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.entries[messageID] = time.Now()

	// Periodic cleanup of expired entries (every 100 adds).
	if len(d.entries)%100 == 0 {
		now := time.Now()
		for k, t := range d.entries {
			if now.Sub(t) > d.ttl {
				delete(d.entries, k)
			}
		}
	}
}

// pubsubPullResponse is the response from Pub/Sub pull API.
type pubsubPullResponse struct {
	ReceivedMessages []struct {
		AckID   string `json:"ackId"`
		Message struct {
			Data      string `json:"data"`
			MessageID string `json:"messageId"`
		} `json:"message"`
	} `json:"receivedMessages"`
}

// pullMessages performs a single Pub/Sub pull request and returns received messages.
func pullMessages(ctx context.Context, auth *ServiceAccountAuth, httpClient *http.Client, projectID, subscriptionID string, maxMessages int) (*pubsubPullResponse, error) {
	token, err := auth.Token(ctx)
	if err != nil {
		return nil, fmt.Errorf("get token: %w", err)
	}

	url := fmt.Sprintf("%s/projects/%s/subscriptions/%s:pull", pubsubAPIBase, projectID, subscriptionID)
	body := fmt.Sprintf(`{"maxMessages":%d}`, maxMessages)

	req, err := http.NewRequestWithContext(ctx, "POST", url, strings.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")

	resp, err := httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("pubsub pull: %w", err)
	}
	defer resp.Body.Close()

	respBody, _ := io.ReadAll(resp.Body)

	if resp.StatusCode == http.StatusOK && len(respBody) <= 2 {
		// Empty response "{}" — no messages
		return &pubsubPullResponse{}, nil
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("pubsub pull %d: %s", resp.StatusCode, string(respBody))
	}

	var result pubsubPullResponse
	if err := json.Unmarshal(respBody, &result); err != nil {
		return nil, fmt.Errorf("parse pull response: %w", err)
	}
	return &result, nil
}

// ackMessages acknowledges received Pub/Sub messages.
func ackMessages(ctx context.Context, auth *ServiceAccountAuth, httpClient *http.Client, projectID, subscriptionID string, ackIDs []string) error {
	if len(ackIDs) == 0 {
		return nil
	}

	token, err := auth.Token(ctx)
	if err != nil {
		return fmt.Errorf("get token: %w", err)
	}

	url := fmt.Sprintf("%s/projects/%s/subscriptions/%s:acknowledge", pubsubAPIBase, projectID, subscriptionID)

	ackBody := struct {
		AckIDs []string `json:"ackIds"`
	}{AckIDs: ackIDs}
	bodyBytes, _ := json.Marshal(ackBody)

	req, err := http.NewRequestWithContext(ctx, "POST", url, strings.NewReader(string(bodyBytes)))
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")

	resp, err := httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("pubsub ack: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("pubsub ack %d: %s", resp.StatusCode, string(body))
	}
	return nil
}

// startPullLoop runs the Pub/Sub pull loop. Blocks until ctx is cancelled.
func (c *Channel) startPullLoop(ctx context.Context) {
	ticker := time.NewTicker(c.pullInterval)
	defer ticker.Stop()

	slog.Info("googlechat: pubsub pull loop started",
		"project", c.projectID, "subscription", c.subscriptionID,
		"interval", c.pullInterval)

	for {
		select {
		case <-ctx.Done():
			slog.Info("googlechat: pubsub pull loop stopped")
			return
		case <-ticker.C:
			c.doPull(ctx)
		}
	}
}

// doPull performs a single pull cycle.
func (c *Channel) doPull(ctx context.Context) {
	resp, err := pullMessages(ctx, c.auth, c.httpClient, c.projectID, c.subscriptionID, defaultPullMaxMessages)
	if err != nil {
		if ctx.Err() != nil {
			return // context cancelled, normal shutdown
		}
		slog.Warn("googlechat: pubsub pull failed", "error", err)
		return
	}

	if len(resp.ReceivedMessages) == 0 {
		return
	}

	var ackIDs []string
	for _, rm := range resp.ReceivedMessages {
		ackIDs = append(ackIDs, rm.AckID)

		// Dedup check
		if c.dedup.seen(rm.Message.MessageID) {
			slog.Debug("googlechat: duplicate pubsub message, skipping", "message_id", rm.Message.MessageID)
			continue
		}
		c.dedup.add(rm.Message.MessageID)

		evt, err := parseEvent(rm.Message.Data)
		if err != nil {
			slog.Warn("googlechat: malformed event, acking anyway", "error", err, "message_id", rm.Message.MessageID)
			continue
		}

		c.handleEvent(ctx, evt)
	}

	// Ack all messages (including malformed ones to prevent infinite re-delivery).
	if err := ackMessages(ctx, c.auth, c.httpClient, c.projectID, c.subscriptionID, ackIDs); err != nil {
		slog.Warn("googlechat: ack failed", "error", err)
	}
}

// handleEvent dispatches a parsed chat event.
func (c *Channel) handleEvent(ctx context.Context, evt *chatEvent) {
	// Filter bot self-messages.
	if c.botUser != "" && evt.SenderID == c.botUser {
		return
	}

	switch evt.Type {
	case "MESSAGE":
		c.handleMessage(ctx, evt)
	case "ADDED_TO_SPACE":
		slog.Info("googlechat: added to space", "space", evt.SpaceID, "by", evt.SenderID)
	case "REMOVED_FROM_SPACE":
		slog.Info("googlechat: removed from space", "space", evt.SpaceID)
	default:
		slog.Debug("googlechat: ignoring event", "type", evt.Type, "space", evt.SpaceID)
	}
}

// handleMessage processes an inbound MESSAGE event.
func (c *Channel) handleMessage(ctx context.Context, evt *chatEvent) {
	// Skip whitespace-only messages.
	text := strings.TrimSpace(evt.Text)
	if text == "" && len(evt.Attachments) == 0 {
		return
	}

	// Check DM/Group policy.
	if !c.BaseChannel.CheckPolicy(evt.PeerKind, c.dmPolicy, c.groupPolicy, evt.SenderID) {
		slog.Debug("googlechat: message rejected by policy",
			"sender", evt.SenderID, "peer_kind", evt.PeerKind)
		return
	}

	// Store thread name for outbound routing (groups).
	if evt.ThreadName != "" && evt.PeerKind == "group" {
		threadKey := evt.SpaceID + ":" + evt.SenderID
		c.threadIDs.Store(threadKey, evt.ThreadName)
	}

	// Download attachments.
	var mediaPaths []string
	for _, att := range evt.Attachments {
		path, err := c.downloadAttachment(ctx, att)
		if err != nil {
			slog.Warn("googlechat: attachment download failed", "error", err)
			continue
		}
		mediaPaths = append(mediaPaths, path)
	}

	metadata := map[string]string{
		"sender_name":  evt.SenderName,
		"message_name": evt.MessageName,
	}
	if evt.ThreadName != "" {
		metadata["thread_name"] = evt.ThreadName
	}

	c.BaseChannel.HandleMessage(evt.SenderID, evt.SpaceID, text, mediaPaths, metadata, evt.PeerKind)
}
