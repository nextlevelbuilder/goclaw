package channels

// routingMetaKeys enumerates the metadata keys that must survive the hop from
// inbound RunContext.Metadata into outbound OutboundMessage.Metadata so that
// replies, block replies, retries, and placeholder updates all land in the
// correct thread / topic / subgroup routing bucket on each channel.
var routingMetaKeys = []string{
	"message_thread_id",      // telegram forum topics
	"local_key",              // composite chat-id suffix
	"group_id",               // legacy group identifier
	"feishu_reply_target_id", // feishu/lark thread reply routing
	"fb_mode",                // facebook messenger vs comment routing
	"sender_id",              // facebook sender for first-inbox / pancake sender for first-inbox
	"page_id",                // facebook page routing
	"reply_to_comment_id",    // facebook/pancake comment reply target
	"pancake_mode",           // pancake inbox vs comment routing
}

var finalReplyMetaKeys = append([]string{
	"placeholder_key", // final outbound can update placeholder; block replies must not
}, routingMetaKeys...)

func copySelectedMeta(src map[string]string, keys []string) map[string]string {
	out := make(map[string]string)
	for _, k := range keys {
		if v := src[k]; v != "" {
			out[k] = v
		}
	}
	return out
}

// CopyFinalRoutingMeta copies the routing metadata required for final outbound
// delivery after an inbound message has been processed by the agent loop.
func CopyFinalRoutingMeta(src map[string]string) map[string]string {
	return copySelectedMeta(src, finalReplyMetaKeys)
}

// copyRoutingMeta copies only the subset safe for intermediate block replies,
// retries, and placeholder updates.
func copyRoutingMeta(src map[string]string) map[string]string {
	return copySelectedMeta(src, routingMetaKeys)
}
