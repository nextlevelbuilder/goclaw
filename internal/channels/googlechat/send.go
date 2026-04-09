package googlechat

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"math"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/nextlevelbuilder/goclaw/internal/bus"
)

// sendTextMessage sends a plain text message to a Google Chat space.
func sendTextMessage(ctx context.Context, apiBase, token string, httpClient *http.Client, msg bus.OutboundMessage, threadName, replyOption string) error {
	_, err := sendTextMessageWithResponse(ctx, apiBase, token, httpClient, msg, threadName, replyOption)
	return err
}

// sendTextMessageWithResponse sends a text message and returns the API response (for thread chaining).
func sendTextMessageWithResponse(ctx context.Context, apiBase, token string, httpClient *http.Client, msg bus.OutboundMessage, threadName, replyOption string) (*chatMessageResponse, error) {
	text := strings.TrimSpace(msg.Content)
	if text == "" {
		return nil, nil
	}

	body := map[string]any{
		"text": markdownToGoogleChat(text),
	}
	if threadName != "" {
		body["thread"] = map[string]string{"name": threadName}
	}

	return postChatMessage(ctx, apiBase, token, httpClient, msg.ChatID, body, replyOption)
}

// sendCardMessage sends a Card V2 message.
func sendCardMessage(ctx context.Context, apiBase, token string, httpClient *http.Client, chatID string, card map[string]any, threadName, replyOption string) error {
	if threadName != "" {
		card["thread"] = map[string]string{"name": threadName}
	}
	_, err := postChatMessage(ctx, apiBase, token, httpClient, chatID, card, replyOption)
	return err
}

// chatMessageResponse is the response from Chat API message operations.
type chatMessageResponse struct {
	Name   string `json:"name"`   // spaces/{space}/messages/{message}
	Thread struct {
		Name string `json:"name"` // spaces/{space}/threads/{thread}
	} `json:"thread"`
}

// postChatMessage sends a message to the Chat API with retry logic.
func postChatMessage(ctx context.Context, apiBase, token string, httpClient *http.Client, spaceID string, body map[string]any, replyOption string) (*chatMessageResponse, error) {
	bodyBytes, err := json.Marshal(body)
	if err != nil {
		return nil, err
	}

	url := fmt.Sprintf("%s/%s/messages", apiBase, spaceID)
	if replyOption != "" {
		url += "?messageReplyOption=" + replyOption
	}

	var result chatMessageResponse
	err = retrySend(ctx, httpClient, func() (*http.Response, error) {
		req, err := http.NewRequestWithContext(ctx, "POST", url, strings.NewReader(string(bodyBytes)))
		if err != nil {
			return nil, err
		}
		req.Header.Set("Authorization", "Bearer "+token)
		req.Header.Set("Content-Type", "application/json")
		return httpClient.Do(req)
	}, &result)
	if err != nil {
		return nil, err
	}
	return &result, nil
}

// editMessage edits an existing message.
func editMessage(ctx context.Context, apiBase, token string, httpClient *http.Client, messageName string, text string) error {
	body := map[string]any{
		"text": text,
	}
	bodyBytes, _ := json.Marshal(body)

	url := fmt.Sprintf("%s/%s?updateMask=text", apiBase, messageName)

	return retrySend(ctx, httpClient, func() (*http.Response, error) {
		req, err := http.NewRequestWithContext(ctx, "PATCH", url, strings.NewReader(string(bodyBytes)))
		if err != nil {
			return nil, err
		}
		req.Header.Set("Authorization", "Bearer "+token)
		req.Header.Set("Content-Type", "application/json")
		return httpClient.Do(req)
	})
}

// deleteMessage deletes a message.
func deleteMessage(ctx context.Context, apiBase, token string, httpClient *http.Client, messageName string) error {
	url := fmt.Sprintf("%s/%s", apiBase, messageName)

	req, err := http.NewRequestWithContext(ctx, "DELETE", url, nil)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+token)

	resp, err := httpClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusNoContent {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("delete message %d: %s", resp.StatusCode, string(body))
	}
	return nil
}

// retrySend retries an HTTP request with exponential backoff on 429/5xx.
func retrySend(ctx context.Context, httpClient *http.Client, doReq func() (*http.Response, error), result ...any) error {
	delay := retrySendBaseDelay
	for attempt := 0; attempt < retrySendMaxAttempts; attempt++ {
		resp, err := doReq()
		if err != nil {
			if ctx.Err() != nil {
				return ctx.Err()
			}
			return err
		}

		if resp.StatusCode >= 200 && resp.StatusCode < 300 {
			if len(result) > 0 && result[0] != nil {
				body, _ := io.ReadAll(resp.Body)
				resp.Body.Close()
				json.Unmarshal(body, result[0])
			} else {
				resp.Body.Close()
			}
			return nil
		}

		body, _ := io.ReadAll(resp.Body)
		resp.Body.Close()

		if resp.StatusCode == 429 || resp.StatusCode >= 500 {
			if attempt < retrySendMaxAttempts-1 {
				slog.Debug("googlechat: retrying send", "status", resp.StatusCode, "attempt", attempt+1, "delay", delay)
				select {
				case <-ctx.Done():
					return ctx.Err()
				case <-time.After(delay):
				}
				delay = time.Duration(math.Min(float64(delay*2), float64(retrySendMaxDelay)))
				continue
			}
		}

		return fmt.Errorf("chat API %d: %s", resp.StatusCode, string(body))
	}
	return fmt.Errorf("chat API: max retries exceeded")
}

// buildCardMessage creates a Cards V2 message from content with tables/code.
// Handles: # (card title), ## (section header), ### (bold subtitle),
// tables (decoratedText for 2-col, <pre> for wider), bullets, code blocks, dividers.
func buildCardMessage(content string) map[string]any {
	if !detectStructuredContent(content) {
		return nil
	}

	var sections []map[string]any
	lines := strings.Split(content, "\n")
	var currentWidgets []map[string]any
	var currentHeader string // section-level header text
	var inTable bool
	var tableRows []string
	var inCodeBlock bool
	var codeLines []string

	flushWidgets := func() {
		if len(currentWidgets) == 0 {
			return
		}
		section := map[string]any{"widgets": currentWidgets}
		if currentHeader != "" {
			section["header"] = currentHeader
			currentHeader = ""
		}
		sections = append(sections, section)
		currentWidgets = nil
	}

	flushTable := func() {
		if len(tableRows) == 0 {
			return
		}
		tw := buildTableWidget(tableRows)
		tableRows = nil
		if tw == nil {
			return
		}
		// buildTableWidget returns {"__widgets": [...]} for 2-col decoratedText
		if multiWidgets, ok := tw["__widgets"]; ok {
			currentWidgets = append(currentWidgets, multiWidgets.([]map[string]any)...)
		} else {
			currentWidgets = append(currentWidgets, tw)
		}
	}

	flushCodeBlock := func() {
		if len(codeLines) == 0 {
			return
		}
		codeText := strings.Join(codeLines, "\n")
		currentWidgets = append(currentWidgets, map[string]any{
			"textParagraph": map[string]string{
				"text": "<pre>" + codeText + "</pre>",
			},
		})
		codeLines = nil
	}

	for _, line := range lines {
		trimmed := strings.TrimSpace(line)

		// Code block toggle.
		if strings.HasPrefix(trimmed, "```") {
			if inCodeBlock {
				flushCodeBlock()
				inCodeBlock = false
			} else {
				inCodeBlock = true
			}
			continue
		}
		if inCodeBlock {
			codeLines = append(codeLines, line)
			continue
		}

		// Table row detection.
		if strings.HasPrefix(trimmed, "|") && strings.HasSuffix(trimmed, "|") {
			inTable = true
			if isSeparatorRow(trimmed) {
				continue
			}
			tableRows = append(tableRows, trimmed)
			continue
		}
		if inTable {
			flushTable()
			inTable = false
		}

		// Horizontal rule → divider.
		if trimmed == "---" || trimmed == "***" || trimmed == "___" {
			currentWidgets = append(currentWidgets, map[string]any{"divider": map[string]any{}})
			continue
		}

		// # heading → card title (handled below), start new section
		if strings.HasPrefix(trimmed, "# ") && !strings.HasPrefix(trimmed, "## ") {
			flushWidgets()
			continue
		}

		// ## heading → section header (CardV2 section.header)
		if strings.HasPrefix(trimmed, "## ") && !strings.HasPrefix(trimmed, "### ") {
			flushWidgets()
			currentHeader = strings.TrimPrefix(trimmed, "## ")
			continue
		}

		// ### heading → bold subtitle widget
		if strings.HasPrefix(trimmed, "### ") {
			headerText := strings.TrimPrefix(trimmed, "### ")
			currentWidgets = append(currentWidgets, map[string]any{
				"decoratedText": map[string]any{
					"text": "<b>" + markdownToGoogleChatHTML(headerText) + "</b>",
				},
			})
			continue
		}

		// Empty line → skip
		if trimmed == "" {
			continue
		}

		// Regular text (including bullets) → textParagraph with HTML
		currentWidgets = append(currentWidgets, map[string]any{
			"textParagraph": map[string]string{
				"text": markdownToGoogleChatHTML(trimmed),
			},
		})
	}

	flushCodeBlock()
	flushTable()
	flushWidgets()

	if len(sections) == 0 {
		return nil
	}

	title := "Response"
	for _, line := range lines {
		t := strings.TrimSpace(line)
		if strings.HasPrefix(t, "# ") && !strings.HasPrefix(t, "## ") {
			title = strings.TrimPrefix(t, "# ")
			break
		}
	}

	return map[string]any{
		"cardsV2": []map[string]any{{
			"card": map[string]any{
				"header":   map[string]string{"title": title},
				"sections": sections,
			},
		}},
	}
}

// isSeparatorRow checks if a table row is a separator (e.g. |---|---|).
func isSeparatorRow(row string) bool {
	inner := strings.Trim(row, "|")
	for _, ch := range inner {
		if ch != '-' && ch != ':' && ch != ' ' && ch != '|' {
			return false
		}
	}
	return true
}

// Send implements the Channel interface for outbound messages.
func (c *Channel) Send(ctx context.Context, msg bus.OutboundMessage) error {
	content := strings.TrimSpace(msg.Content)
	if content == "" && len(msg.Media) == 0 {
		return nil
	}

	token, err := c.auth.Token(ctx)
	if err != nil {
		return err
	}

	// Determine thread context.
	peerKind := msg.Metadata["peer_kind"]
	threadName := ""
	replyOption := ""
	if peerKind == "group" {
		if tn, ok := msg.Metadata["thread_name"]; ok {
			threadName = tn
		} else {
			senderID := msg.Metadata["sender_id"]
			threadKey := msg.ChatID + ":" + senderID
			if v, ok := c.threadIDs.Load(threadKey); ok {
				threadName = v.(string)
			}
		}
		replyOption = "REPLY_MESSAGE_FALLBACK_TO_NEW_THREAD"
	}

	// Placeholder update (e.g. tool status, LLM retry): edit placeholder but keep it
	// alive for the final response. Same pattern as Telegram/Discord/Slack.
	if msg.Metadata["placeholder_update"] == "true" {
		if pName, ok := c.placeholders.Load(msg.ChatID); ok {
			_ = editMessage(ctx, c.apiBase, token, c.httpClient, pName.(string), markdownToGoogleChat(content))
		}
		return nil
	}

	// Check for placeholder edit (Thinking... → final response).
	if placeholderName, ok := c.placeholders.Load(msg.ChatID); ok {
		c.placeholders.Delete(msg.ChatID)
		pName := placeholderName.(string)

		if len([]byte(content)) <= googleChatMaxMessageBytes && !detectStructuredContent(content) &&
			(c.longFormThreshold == 0 || len(content) <= c.longFormThreshold) {
			if err := editMessage(ctx, c.apiBase, token, c.httpClient, pName, markdownToGoogleChat(content)); err != nil {
				slog.Warn("googlechat: placeholder edit failed, sending new", "error", err)
			} else {
				return nil
			}
		}
		deleteMessage(ctx, c.apiBase, token, c.httpClient, pName)
	}

	// Long-form content → file attachment.
	if c.longFormThreshold > 0 && len(content) > c.longFormThreshold {
		if err := c.sendLongForm(ctx, token, msg, content, threadName, replyOption); err != nil {
			slog.Warn("googlechat: long-form send failed, falling back to chunks", "error", err)
		} else {
			return nil
		}
	}

	// Card message for structured content.
	if card := buildCardMessage(content); card != nil {
		return sendCardMessage(ctx, c.apiBase, token, c.httpClient, msg.ChatID, card, threadName, replyOption)
	}

	// Chunked plain text.
	chunks := chunkByBytes(content, googleChatMaxMessageBytes)
	currentThread := threadName
	for i, chunk := range chunks {
		chunkMsg := msg
		chunkMsg.Content = chunk
		resp, err := sendTextMessageWithResponse(ctx, c.apiBase, token, c.httpClient, chunkMsg, currentThread, replyOption)
		if err != nil {
			return fmt.Errorf("send chunk %d/%d: %w", i+1, len(chunks), err)
		}
		if resp != nil && resp.Thread.Name != "" {
			currentThread = resp.Thread.Name
		}
	}

	return nil
}

// sendLongForm uploads content as a file and sends a summary message.
func (c *Channel) sendLongForm(ctx context.Context, token string, msg bus.OutboundMessage, content, threadName, replyOption string) error {
	summary := extractSummary(content)

	ext := ".md"
	if c.longFormFormat == "txt" {
		ext = ".txt"
	}
	tmpPath := filepath.Join(os.TempDir(), uuid.New().String()+ext)
	if err := os.WriteFile(tmpPath, []byte(content), 0644); err != nil {
		return err
	}
	defer os.Remove(tmpPath)

	mimeType := "text/markdown"
	if c.longFormFormat == "txt" {
		mimeType = "text/plain"
	}
	_, webLink, err := c.uploadToDrive(ctx, tmpPath, "response"+ext, mimeType)
	if err != nil {
		return err
	}

	summaryText := markdownToGoogleChat(summary) + "\n\n📎 " + webLink
	body := map[string]any{
		"text": summaryText,
	}
	if threadName != "" {
		body["thread"] = map[string]string{"name": threadName}
	}

	_, err = postChatMessage(ctx, c.apiBase, token, c.httpClient, msg.ChatID, body, replyOption)
	return err
}

// sendPlaceholder sends a "Thinking..." placeholder message and stores its name.
func (c *Channel) sendPlaceholder(ctx context.Context, chatID, threadName, replyOption string) {
	token, err := c.auth.Token(ctx)
	if err != nil {
		slog.Warn("googlechat: placeholder auth failed", "error", err)
		return
	}

	body := map[string]any{
		"text": "🤔 Thinking...",
	}
	if threadName != "" {
		body["thread"] = map[string]string{"name": threadName}
	}

	bodyBytes, _ := json.Marshal(body)
	url := fmt.Sprintf("%s/%s/messages", c.apiBase, chatID)
	if replyOption != "" {
		url += "?messageReplyOption=" + replyOption
	}

	req, err := http.NewRequestWithContext(ctx, "POST", url, strings.NewReader(string(bodyBytes)))
	if err != nil {
		return
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusOK {
		var result struct {
			Name string `json:"name"`
		}
		respBody, _ := io.ReadAll(resp.Body)
		if json.Unmarshal(respBody, &result) == nil && result.Name != "" {
			c.placeholders.Store(chatID, result.Name)
			slog.Debug("googlechat: placeholder sent", "chat_id", chatID, "name", result.Name)
		}
	}
}
