package cmd

import (
	"testing"
	"time"

	"github.com/nextlevelbuilder/goclaw/internal/bus"
)

// TestConsumerDedupSeedsAlbumSiblings — Rule #4 (Phase 1).
// When the debouncer flushes a merged message, all sibling message_ids
// (from metadata["merged_message_ids"]) MUST be seeded into the dedup cache
// so any platform retransmit of a sibling member is short-circuited.
func TestConsumerDedupSeedsAlbumSiblings(t *testing.T) {
	dedupe := bus.NewDedupeCache(20*time.Minute, 5000)

	merged := bus.InboundMessage{
		Channel:  "telegram",
		ChatID:   "chat-1",
		SenderID: "user-1",
		Metadata: map[string]string{
			"message_id":         "m3",
			"merged_message_ids": "m1,m2,m3",
		},
	}

	seedDedupFromMerged(dedupe, merged)

	// Sibling retransmits MUST be reported as duplicates after seeding.
	for _, mid := range []string{"m1", "m2", "m3"} {
		key := dedupKeyFor("telegram", "user-1", "chat-1", mid)
		if !dedupe.IsDuplicate(key) {
			t.Fatalf("sibling %q not seeded; key=%q", mid, key)
		}
	}

	// Unrelated message_id must NOT be marked duplicate.
	otherKey := dedupKeyFor("telegram", "user-1", "chat-1", "m99")
	if dedupe.IsDuplicate(otherKey) {
		t.Fatalf("unrelated message m99 wrongly marked duplicate")
	}
}

// TestConsumerDedupSeedsNoopWhenMergedEmpty: no merged_message_ids → no-op.
func TestConsumerDedupSeedsNoopWhenMergedEmpty(t *testing.T) {
	dedupe := bus.NewDedupeCache(20*time.Minute, 5000)
	msg := bus.InboundMessage{
		Channel:  "telegram",
		ChatID:   "chat-1",
		SenderID: "user-1",
		Metadata: map[string]string{"message_id": "m1"},
	}
	seedDedupFromMerged(dedupe, msg) // must not panic

	// m1 should NOT have been seeded by this helper (only the merged-list path seeds).
	key := dedupKeyFor("telegram", "user-1", "chat-1", "m1")
	if dedupe.IsDuplicate(key) {
		t.Fatalf("m1 wrongly seeded for non-merged message")
	}
}

// TestConsumerDedupSeedsHandlesNilMetadata: nil metadata → no-op, no panic.
func TestConsumerDedupSeedsHandlesNilMetadata(t *testing.T) {
	dedupe := bus.NewDedupeCache(20*time.Minute, 5000)
	msg := bus.InboundMessage{Channel: "telegram", ChatID: "c", SenderID: "u"}
	seedDedupFromMerged(dedupe, msg) // must not panic
}
