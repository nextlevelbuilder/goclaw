package tools

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/nextlevelbuilder/goclaw/internal/channels"
)

// GroupLister abstracts listing the groups a channel account belongs to.
// Implemented by channels.Manager.ListGroups.
type GroupLister func(ctx context.Context, channel string) ([]channels.GroupInfo, error)

// GroupListerAware tools can receive a group lister function.
type GroupListerAware interface {
	SetGroupLister(GroupLister)
}

// ListGroupsTool lets the agent resolve a group's real chat ID from its
// display name before sending/forwarding a message to it. Without this,
// the only signal available is sessions_list (which exposes session keys,
// not human-readable names), so the agent has no reliable way to turn
// "the Ban Điều Hành group" into the ID the message tool actually requires.
type ListGroupsTool struct {
	lister GroupLister
}

func NewListGroupsTool() *ListGroupsTool { return &ListGroupsTool{} }

func (t *ListGroupsTool) SetGroupLister(l GroupLister) { t.lister = l }

func (t *ListGroupsTool) RequiredChannelTypes() []string { return []string{"zalo_personal"} }

func (t *ListGroupsTool) Name() string { return "zalo_list_groups" }
func (t *ListGroupsTool) Description() string {
	return "List all Zalo groups the connected account belongs to (group_id + display name + member count). Use this BEFORE sending/forwarding a message to a group by name — the message tool's `target` requires a real group_id, not a display name; passing the name directly will fail or silently go nowhere."
}

func (t *ListGroupsTool) Parameters() map[string]any {
	return map[string]any{
		"type":       "object",
		"properties": map[string]any{},
	}
}

func (t *ListGroupsTool) Execute(ctx context.Context, args map[string]any) *Result {
	if t.lister == nil {
		return ErrorResult("zalo_list_groups: no group lister available")
	}

	channel := ToolChannelFromCtx(ctx)
	if channel == "" {
		return ErrorResult("channel is required (no current channel in context)")
	}

	groups, err := t.lister(ctx, channel)
	if err != nil {
		return ErrorResult(fmt.Sprintf("failed to list groups: %v", err))
	}

	type groupOut struct {
		GroupID     string `json:"group_id"`
		Name        string `json:"name"`
		TotalMember int    `json:"total_member,omitempty"`
	}
	out := make([]groupOut, len(groups))
	for i, g := range groups {
		out[i] = groupOut{GroupID: g.GroupID, Name: g.Name, TotalMember: g.TotalMember}
	}

	data, _ := json.Marshal(map[string]any{
		"channel": channel,
		"count":   len(out),
		"groups":  out,
	})
	return NewResult(string(data))
}
