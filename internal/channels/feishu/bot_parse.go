package feishu

import (
	"encoding/json"
	"fmt"
	"strings"
)

func (c *Channel) parseMessageEvent(event *MessageEvent) *messageContext {
	msg := &event.Event.Message
	sender := &event.Event.Sender

	chatID := msg.ChatID
	messageID := msg.MessageID
	chatType := msg.ChatType
	contentType := msg.MessageType
	rootID := msg.RootID
	parentID := msg.ParentID
	threadID := msg.ThreadID

	senderID := ""
	if sender != nil {
		senderID = sender.SenderID.OpenID
	}

	// Parse content
	content := parseMessageContent(msg.Content, contentType)

	// Parse mentions
	var mentions []mentionInfo
	mentionedBot := false
	for _, m := range msg.Mentions {
		mi := mentionInfo{
			Key:    m.Key,
			OpenID: m.ID.OpenID,
			Name:   m.Name,
		}
		mentions = append(mentions, mi)

		// Check if bot is mentioned.
		// If botOpenID is known, match exactly; otherwise treat any mention as bot mention
		// (fallback when probeBotInfo fails — better to process than silently drop).
		if c.botOpenID == "" || mi.OpenID == c.botOpenID {
			mentionedBot = true
		}
	}

	// Replace mention placeholders with readable names.
	// Bot mention is stripped entirely; other user mentions become @Name.
	content = resolveMentions(content, mentions, c.botOpenID)

	return &messageContext{
		ChatID:       chatID,
		MessageID:    messageID,
		SenderID:     senderID,
		ChatType:     chatType,
		Content:      content,
		ContentType:  contentType,
		MentionedBot: mentionedBot,
		RootID:       rootID,
		ParentID:     parentID,
		ThreadID:     threadID,
		Mentions:     mentions,
	}
}

// --- Content parsing ---

func parseMessageContent(rawContent, messageType string) string {
	if rawContent == "" {
		return ""
	}

	switch messageType {
	case "text":
		var textMsg struct {
			Text string `json:"text"`
		}
		if err := json.Unmarshal([]byte(rawContent), &textMsg); err == nil {
			return textMsg.Text
		}
		return rawContent

	case "post":
		return parsePostContent(rawContent)

	case "image":
		return "[image]"

	case "file":
		var fileMsg struct {
			FileName string `json:"file_name"`
		}
		if err := json.Unmarshal([]byte(rawContent), &fileMsg); err == nil {
			return fmt.Sprintf("[file: %s]", fileMsg.FileName)
		}
		return "[file]"

	default:
		return fmt.Sprintf("[%s message]", messageType)
	}
}

// resolvePostElements extracts the flattened element list from a Lark post message JSON.
// Handles two formats:
//  1. Language-wrapped: {"zh_cn": {"title":"...", "content": [[...]]}}
//  2. Flat (no language key): {"title":"...", "content": [[...]]}
func resolvePostElements(rawContent string) []any {
	var post map[string]any
	if err := json.Unmarshal([]byte(rawContent), &post); err != nil {
		return nil
	}

	// Format 2: flat — "content" at top level (no language wrapper).
	if contentArr, ok := post["content"].([]any); ok {
		return contentArr
	}

	// Format 1: language-wrapped — try known locales, then first map value.
	var langContent any
	for _, lang := range []string{"zh_cn", "en_us"} {
		if lc, ok := post[lang]; ok {
			langContent = lc
			break
		}
	}
	if langContent == nil {
		for _, v := range post {
			if _, ok := v.(map[string]any); ok {
				langContent = v
				break
			}
		}
	}
	if langContent == nil {
		return nil
	}

	langMap, ok := langContent.(map[string]any)
	if !ok {
		return nil
	}

	contentArr, ok := langMap["content"].([]any)
	if !ok {
		return nil
	}
	return contentArr
}

func parsePostContent(rawContent string) string {
	contentArr := resolvePostElements(rawContent)
	if contentArr == nil {
		return rawContent
	}

	var textParts []string
	for _, para := range contentArr {
		paraArr, ok := para.([]any)
		if !ok {
			continue
		}
		var lineParts []string
		for _, elem := range paraArr {
			elemMap, ok := elem.(map[string]any)
			if !ok {
				continue
			}
			tag, _ := elemMap["tag"].(string)
			switch tag {
			case "text":
				if t, ok := elemMap["text"].(string); ok {
					lineParts = append(lineParts, t)
				}
			case "md":
				if t, ok := elemMap["text"].(string); ok {
					lineParts = append(lineParts, t)
				}
			case "at":
				if name, ok := elemMap["user_name"].(string); ok {
					lineParts = append(lineParts, "@"+name)
				}
			case "a":
				if href, ok := elemMap["href"].(string); ok {
					text, _ := elemMap["text"].(string)
					if text != "" {
						lineParts = append(lineParts, fmt.Sprintf("[%s](%s)", text, href))
					} else {
						lineParts = append(lineParts, href)
					}
				}
			case "img":
				lineParts = append(lineParts, "[image]")
			}
		}
		if len(lineParts) > 0 {
			textParts = append(textParts, strings.Join(lineParts, ""))
		}
	}

	return strings.Join(textParts, "\n")
}

// extractPostImageKeys parses post content JSON and returns deduplicated image_key
// values from embedded img tags. Used to download images inline in post messages.
func extractPostImageKeys(rawContent string) []string {
	contentArr := resolvePostElements(rawContent)
	if contentArr == nil {
		return nil
	}

	seen := make(map[string]struct{})
	var keys []string
	for _, para := range contentArr {
		paraArr, ok := para.([]any)
		if !ok {
			continue
		}
		for _, elem := range paraArr {
			elemMap, ok := elem.(map[string]any)
			if !ok {
				continue
			}
			tag, _ := elemMap["tag"].(string)
			if tag == "img" {
				if key, ok := elemMap["image_key"].(string); ok && key != "" {
					if _, dup := seen[key]; !dup {
						seen[key] = struct{}{}
						keys = append(keys, key)
					}
				}
			}
		}
	}
	return keys
}

// resolveMentions replaces mention placeholders (@_user_1, @_user_2, etc.) in content.
// Bot mention is stripped entirely; other user mentions become @Name.
func resolveMentions(text string, mentions []mentionInfo, botOpenID string) string {
	for _, m := range mentions {
		if m.Key == "" {
			continue
		}
		if botOpenID != "" && m.OpenID == botOpenID {
			// Strip bot mention
			text = strings.ReplaceAll(text, m.Key, "")
		} else if m.Name != "" {
			// Replace with @Name
			text = strings.ReplaceAll(text, m.Key, "@"+m.Name)
		}
	}
	return strings.TrimSpace(text)
}
