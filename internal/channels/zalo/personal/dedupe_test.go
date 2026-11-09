package personal

import (
	"testing"
	"time"
)

func TestDedupeCacheSuppressesReplayAcrossIDVariants(t *testing.T) {
	now := time.Unix(0, 0)
	cache := newDedupeCache(10*time.Minute, 10, func() time.Time { return now })

	if cache.Seen("msg-1", "cli-1") {
		t.Fatal("first sight reported as duplicate")
	}
	if !cache.Seen("msg-1") {
		t.Fatal("msgId replay not suppressed")
	}
	if !cache.Seen("", "cli-1") {
		t.Fatal("cliMsgId replay not suppressed")
	}
	if cache.Seen("msg-2", "cli-2") {
		t.Fatal("different message reported as duplicate")
	}

	now = now.Add(11 * time.Minute)
	if cache.Seen("msg-1") {
		t.Fatal("expired entry still suppressed")
	}
}

func TestDedupeCacheEvictsOldest(t *testing.T) {
	cache := newDedupeCache(time.Hour, 2, nil)
	if cache.Seen("m1", "c1") {
		t.Fatal("first message reported as duplicate")
	}
	if cache.Seen("m2", "c2") {
		t.Fatal("second message reported as duplicate")
	}
	if cache.Seen("m1") {
		t.Fatal("oldest entry was not evicted")
	}
}

func TestDedupeCacheIgnoresEmptyKeys(t *testing.T) {
	cache := newDedupeCache(time.Minute, 10, nil)
	if cache.Seen("", "  ") {
		t.Fatal("empty keys must not be treated as a duplicate")
	}
	if cache.Seen("", "") {
		t.Fatal("empty keys must not be treated as a duplicate on second call")
	}
}
