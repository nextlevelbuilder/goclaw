package common

// Platform values for inbound message metadata.
const (
	PlatformZaloBot = "zalo_bot"
	PlatformZaloOA  = "zalo_oa"
)

// InboundMeta is the per-message metadata both bot and oa publish.
type InboundMeta struct {
	MessageID         string
	Platform          string
	SenderDisplayName string
}

// ToMap returns the shape BaseChannel.HandleMessage expects.
func (m InboundMeta) ToMap() map[string]string {
	out := map[string]string{
		"platform": m.Platform,
	}
	if m.MessageID != "" {
		out["message_id"] = m.MessageID
	}
	if m.SenderDisplayName != "" {
		out["sender_display_name"] = m.SenderDisplayName
	}
	return out
}
