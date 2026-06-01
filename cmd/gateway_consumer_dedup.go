package cmd

import (
	"fmt"
	"strings"

	"github.com/nextlevelbuilder/goclaw/internal/bus"
)

// dedupKeyFor builds the inbound-message dedup key.
// Format must match the inline key built in consumeInboundMessages
// (gateway_consumer.go) so siblings seeded via seedDedupFromMerged match
// the lookup performed on subsequent platform retransmits.
func dedupKeyFor(channel, senderID, chatID, messageID string) string {
	return fmt.Sprintf("%s|%s|%s|%s", channel, senderID, chatID, messageID)
}

// seedDedupFromMerged seeds every sibling message_id from the flushed merged
// message into the dedup cache. Implements Phase 1 Rule #4: a multi-attachment
// burst flushes as ONE merged InboundMessage carrying all source ids in
// metadata["merged_message_ids"]; any subsequent platform retransmit of a
// sibling member (e.g. Telegram webhook redelivery, WhatsApp retry) MUST be
// short-circuited at the dedup gate before re-entering the debouncer.
//
// No-op when:
//   - dedupe is nil (defensive — call sites should always pass a real cache)
//   - msg.Metadata is nil
//   - merged_message_ids is absent or empty
func seedDedupFromMerged(dedupe *bus.DedupeCache, msg bus.InboundMessage) {
	if dedupe == nil || msg.Metadata == nil {
		return
	}
	merged := msg.Metadata["merged_message_ids"]
	if merged == "" {
		return
	}
	for _, id := range strings.Split(merged, ",") {
		id = strings.TrimSpace(id)
		if id == "" {
			continue
		}
		// IsDuplicate has a side-effect of recording the key when absent,
		// which is exactly the seeding we want. Discard the boolean.
		_ = dedupe.IsDuplicate(dedupKeyFor(msg.Channel, msg.SenderID, msg.ChatID, id))
	}
}
