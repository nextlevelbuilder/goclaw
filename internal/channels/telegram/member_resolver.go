package telegram

import (
	"context"
	"fmt"
	"strconv"

	"github.com/mymmrac/telego"
	tu "github.com/mymmrac/telego/telegoutil"

	"github.com/nextlevelbuilder/goclaw/internal/channels"
)

// ResolveMember implements channels.ChannelMemberResolver — fetches a
// group member profile via Bot API `getChatMember`. Used by the gateway
// to auto-enrich file_writer permission metadata when a caller grants
// access without providing displayName/username (e.g. Web UI path).
func (c *Channel) ResolveMember(ctx context.Context, chatID, userID string) (channels.MemberInfo, error) {
	if c.bot == nil {
		return channels.MemberInfo{}, fmt.Errorf("telegram bot not initialized")
	}
	chatIDInt, err := strconv.ParseInt(chatID, 10, 64)
	if err != nil {
		return channels.MemberInfo{}, fmt.Errorf("invalid chatID %q: %w", chatID, err)
	}
	userIDInt, err := strconv.ParseInt(userID, 10, 64)
	if err != nil {
		return channels.MemberInfo{}, fmt.Errorf("invalid userID %q: %w", userID, err)
	}
	member, err := c.bot.GetChatMember(ctx, &telego.GetChatMemberParams{
		ChatID: tu.ID(chatIDInt),
		UserID: userIDInt,
	})
	if err != nil {
		return channels.MemberInfo{}, err
	}
	user := member.MemberUser()
	return channels.MemberInfo{
		Username:    user.Username,
		DisplayName: user.FirstName,
	}, nil
}
