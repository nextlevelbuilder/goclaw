package tools

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/nextlevelbuilder/goclaw/internal/channels"
)

// GroupMemberLister abstracts listing members of a group chat.
// Implemented by channels.Manager.ListGroupMembers.
type GroupMemberLister func(ctx context.Context, channel, chatID string) ([]channels.GroupMember, error)

// GroupMemberListerAware tools can receive a group member lister function.
type GroupMemberListerAware interface {
	SetGroupMemberLister(GroupMemberLister)
}

// ListGroupMembersTool allows the agent to list members in a group chat.
type ListGroupMembersTool struct {
	lister GroupMemberLister
}

func NewListGroupMembersTool() *ListGroupMembersTool {
	return &ListGroupMembersTool{}
}

func (t *ListGroupMembersTool) SetGroupMemberLister(l GroupMemberLister) { t.lister = l }

func (t *ListGroupMembersTool) RequiredChannelTypes() []string { return []string{"feishu"} }

func (t *ListGroupMembersTool) Name() string { return "list_group_members" }
func (t *ListGroupMembersTool) Description() string {
	return "List all members of the current group chat. Returns member IDs (open_id) and display names. Only works in group conversations on supported channels (Lark/Feishu). Use this to find out who is in the group, check attendance, or identify members. To @mention a member in a message, use @member_id (e.g. @ou_abc123) — it will be converted to a native mention with notification."
}

func (t *ListGroupMembersTool) Parameters() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"channel": map[string]any{
				"type":        "string",
				"description": "Channel name (default: current channel from context)",
			},
			"chat_id": map[string]any{
				"type":        "string",
				"description": "Group chat ID (default: current chat from context)",
			},
		},
		"required": []string{},
	}
}

func (t *ListGroupMembersTool) Execute(ctx context.Context, args map[string]any) *Result {
	if t.lister == nil {
		return ErrorResult("list_group_members: no group member lister available")
	}

	channel, _ := args["channel"].(string)
	if channel == "" {
		channel = ToolChannelFromCtx(ctx)
	}
	if channel == "" {
		return ErrorResult("channel is required (no current channel in context)")
	}

	chatID, _ := args["chat_id"].(string)
	if chatID == "" {
		chatID = ToolChatIDFromCtx(ctx)
	}
	if chatID == "" {
		return ErrorResult("chat_id is required (no current chat in context)")
	}

	members, err := t.lister(ctx, channel, chatID)
	if err != nil {
		return ErrorResult(fmt.Sprintf("failed to list group members: %v", err))
	}

	type memberOut struct {
		MemberID string `json:"member_id"`
		Name     string `json:"name"`
	}
	out := make([]memberOut, len(members))
	for i, m := range members {
		out[i] = memberOut{MemberID: m.MemberID, Name: m.Name}
	}

	data, _ := json.Marshal(map[string]any{
		"channel": channel,
		"chat_id": chatID,
		"count":   len(out),
		"members": out,
	})
	return NewResult(string(data))
}
