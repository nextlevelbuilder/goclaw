package channels

import "errors"

// ErrMediaUnsupported is returned when a channel does not support media attachments.
// Callers (e.g. webhook handler) should either degrade to text-only or return HTTP 501.
var ErrMediaUnsupported = errors.New("channel does not support media attachments")

// mediaCapableTypes lists channel platform types that consume msg.Media in their Send()
// implementation. Verified against adapters:
//   - telegram: internal/channels/telegram/send.go:251
//   - discord:  internal/channels/discord/discord.go:207
//   - whatsapp: internal/channels/whatsapp/outbound.go:68
//   - feishu:   internal/channels/feishu/feishu.go:250
//   - slack:    internal/channels/slack/send.go:80
//   - zalo_personal: internal/channels/zalo/personal/send.go:42
//   - pancake:  internal/channels/pancake/media_handler.go:18
//   - facebook: internal/channels/facebook/facebook.go:205
//
// NOT in this list:
//   - zalo_oa: internal/channels/zalo/zalo.go:115 — Send() does NOT consume msg.Media
var mediaCapableTypes = map[string]bool{
	TypeTelegram:     true,
	TypeDiscord:      true,
	TypeWhatsApp:     true,
	TypeFeishu:       true,
	TypeSlack:        true,
	TypeZaloPersonal: true,
	TypePancake:      true,
	TypeFacebook:     true,
}

// IsMediaCapable reports whether the given channel platform type supports media attachments.
// Use Manager.ChannelTypeForName to resolve the type from a channel instance name.
func IsMediaCapable(channelType string) bool {
	return mediaCapableTypes[channelType]
}
