package tools

import (
	"context"
	"encoding/json"
	"fmt"
)

// ForumTopicCreator can create forum topics in a Telegram supergroup.
// Implemented by telegram.Channel.
type ForumTopicCreator interface {
	CreateForumTopic(ctx context.Context, chatID int64, name string, iconColor int, iconEmojiID string) (threadID int, topicName string, err error)
}

// ForumTopicCreatorProvider returns a ForumTopicCreator (e.g. from the channel manager).
// Returns nil if no Telegram channel is available.
type ForumTopicCreatorProvider func() ForumTopicCreator

// CreateForumTopicTool lets agents create forum topics in Telegram supergroups.
type CreateForumTopicTool struct {
	provider ForumTopicCreatorProvider
}

// NewCreateForumTopicTool creates a new create_forum_topic tool.
func NewCreateForumTopicTool(provider ForumTopicCreatorProvider) *CreateForumTopicTool {
	return &CreateForumTopicTool{provider: provider}
}

func (t *CreateForumTopicTool) Name() string { return "create_forum_topic" }

func (t *CreateForumTopicTool) RequiredChannelTypes() []string { return []string{"telegram"} }

func (t *CreateForumTopicTool) Description() string {
	return "Create a new forum topic in a Telegram supergroup. " +
		"Returns the topic's message_thread_id which can be used for routing messages to the topic."
}

func (t *CreateForumTopicTool) Parameters() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"chat_id": map[string]any{
				"type":        "string",
				"description": "The Telegram chat ID of the supergroup (e.g. \"-1001234567890\").",
			},
			"name": map[string]any{
				"type":        "string",
				"description": "Topic name (1-128 characters).",
			},
			"icon_color": map[string]any{
				"type":        "integer",
				"description": "Optional icon color as RGB integer. Allowed values: 7322096, 16766590, 13338331, 9367192, 16749490, 16478047.",
			},
			"icon_custom_emoji_id": map[string]any{
				"type":        "string",
				"description": "Optional custom emoji ID for the topic icon (requires Telegram Premium).",
			},
		},
		"required": []string{"chat_id", "name"},
	}
}

func (t *CreateForumTopicTool) Execute(ctx context.Context, args map[string]any) *Result {
	creator := t.provider()
	if creator == nil {
		return &Result{ForLLM: "Error: no Telegram channel available", IsError: true}
	}

	chatIDStr, _ := args["chat_id"].(string)
	name, _ := args["name"].(string)

	if chatIDStr == "" || name == "" {
		return &Result{ForLLM: "Error: chat_id and name are required", IsError: true}
	}
	if len(name) > 128 {
		return &Result{ForLLM: "Error: topic name must be 1-128 characters", IsError: true}
	}

	var chatID int64
	if _, err := fmt.Sscanf(chatIDStr, "%d", &chatID); err != nil {
		return &Result{ForLLM: fmt.Sprintf("Error: invalid chat_id %q: %v", chatIDStr, err), IsError: true}
	}

	iconColor := 0
	if ic, ok := args["icon_color"].(float64); ok {
		iconColor = int(ic)
	}
	iconEmojiID, _ := args["icon_custom_emoji_id"].(string)

	threadID, topicName, err := creator.CreateForumTopic(ctx, chatID, name, iconColor, iconEmojiID)
	if err != nil {
		return &Result{ForLLM: fmt.Sprintf("Error creating forum topic: %v", err), IsError: true}
	}

	result := map[string]any{
		"message_thread_id": threadID,
		"name":              topicName,
		"chat_id":           chatIDStr,
	}

	jsonBytes, _ := json.Marshal(result)
	return &Result{ForLLM: string(jsonBytes)}
}
