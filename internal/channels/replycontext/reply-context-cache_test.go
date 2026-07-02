package replycontext

import (
	"strings"
	"testing"
	"time"
)

func TestCacheBuild_MultiLevelReplyChain(t *testing.T) {
	cache := NewCache(Options{MaxDepth: 8, MaxTotalChars: 6000, MaxCharsPerMessage: 1000})
	scope := Scope{TenantID: "tenant-a", ChannelID: "zalo-main", ThreadID: "thread-1"}

	cache.Store(scope, Message{IDs: []string{"a"}, Sender: "Alice", Body: "original"})
	cache.Store(scope, Message{IDs: []string{"b"}, ParentIDs: []string{"a"}, Sender: "Bob", Body: "reply 1"})
	cache.Store(scope, Message{IDs: []string{"c"}, ParentIDs: []string{"b"}, Sender: "Carol", Body: "reply 2"})
	cache.Store(scope, Message{IDs: []string{"d"}, ParentIDs: []string{"c"}, Sender: "Dan", Body: "reply 3"})
	cache.Store(scope, Message{IDs: []string{"e"}, ParentIDs: []string{"d"}, Sender: "Eve", Body: "reply 4"})
	cache.Store(scope, Message{IDs: []string{"f"}, ParentIDs: []string{"e"}, Sender: "Frank", Body: "reply 5"})

	got := cache.Build(scope, Quote{IDs: []string{"f"}, Sender: "Frank", Body: "direct fallback"})
	for _, want := range []string{"original", "reply 1", "reply 2", "reply 3", "reply 4", "reply 5"} {
		if !strings.Contains(got, want) {
			t.Fatalf("missing %q in %q", want, got)
		}
	}
	assertOrder(t, got, "original", "reply 1", "reply 2", "reply 3", "reply 4", "reply 5")
	if strings.Contains(got, "direct fallback") {
		t.Fatalf("used fallback despite cache hit: %q", got)
	}
}

func TestCacheBuild_FallbackOnCacheMiss(t *testing.T) {
	cache := NewCache(Options{})
	scope := Scope{TenantID: "tenant-a", ChannelID: "zalo-main", ThreadID: "thread-1"}

	got := cache.Build(scope, Quote{IDs: []string{"missing"}, Sender: "Alice", Body: "quoted text"})
	want := "[Replying to Alice]\nquoted text\n[/Replying]"
	if got != want {
		t.Fatalf("Build() = %q, want %q", got, want)
	}
}

func TestCacheBuild_IsolatedByScope(t *testing.T) {
	cache := NewCache(Options{})
	scopeA := Scope{TenantID: "tenant-a", ChannelID: "zalo-main", ThreadID: "thread-1"}
	scopeB := Scope{TenantID: "tenant-b", ChannelID: "zalo-main", ThreadID: "thread-1"}

	cache.Store(scopeA, Message{IDs: []string{"same-id"}, Sender: "Alice", Body: "tenant a secret"})
	got := cache.Build(scopeB, Quote{IDs: []string{"same-id"}, Sender: "Bob", Body: "tenant b fallback"})
	if strings.Contains(got, "tenant a secret") {
		t.Fatalf("cross-scope context leaked: %q", got)
	}
	if !strings.Contains(got, "tenant b fallback") {
		t.Fatalf("missing fallback in %q", got)
	}
}

func TestCacheBuild_LimitsDepthAndStopsCycles(t *testing.T) {
	cache := NewCache(Options{MaxDepth: 2, MaxTotalChars: 6000, MaxCharsPerMessage: 1000})
	scope := Scope{TenantID: "tenant-a", ChannelID: "zalo-main", ThreadID: "thread-1"}

	cache.Store(scope, Message{IDs: []string{"a"}, ParentIDs: []string{"c"}, Sender: "Alice", Body: "a"})
	cache.Store(scope, Message{IDs: []string{"b"}, ParentIDs: []string{"a"}, Sender: "Bob", Body: "b"})
	cache.Store(scope, Message{IDs: []string{"c"}, ParentIDs: []string{"b"}, Sender: "Carol", Body: "c"})

	got := cache.Build(scope, Quote{IDs: []string{"c"}, Sender: "Carol", Body: "fallback"})
	if blocks := strings.Count(got, "[Replying to"); blocks != 2 {
		t.Fatalf("rendered %d blocks, want depth limit 2: %q", blocks, got)
	}
}

func TestCacheBuild_EnforcesCharacterCaps(t *testing.T) {
	cache := NewCache(Options{MaxDepth: 8, MaxTotalChars: 120, MaxCharsPerMessage: 32})
	scope := Scope{TenantID: "tenant-a", ChannelID: "zalo-main", ThreadID: "thread-1"}

	cache.Store(scope, Message{IDs: []string{"a"}, Sender: "Alice", Body: strings.Repeat("a", 200)})
	got := cache.Build(scope, Quote{IDs: []string{"a"}})
	if len(got) > 120 {
		t.Fatalf("rendered %d bytes, want <= 120: %q", len(got), got)
	}
	if !strings.Contains(got, "...") {
		t.Fatalf("missing truncation marker: %q", got)
	}
}

func TestCacheEvictsExpiredMessagesAndClear(t *testing.T) {
	cache := NewCache(Options{TTL: time.Minute})
	now := time.Date(2026, 7, 2, 20, 0, 0, 0, time.UTC)
	cache.now = func() time.Time { return now }
	scope := Scope{TenantID: "tenant-a", ChannelID: "zalo-main", ThreadID: "thread-1"}

	cache.Store(scope, Message{IDs: []string{"a"}, Sender: "Alice", Body: "old"})
	cache.now = func() time.Time { return now.Add(2 * time.Minute) }

	got := cache.Build(scope, Quote{IDs: []string{"a"}, Sender: "Alice", Body: "fallback"})
	if strings.Contains(got, "old") {
		t.Fatalf("expired context rendered: %q", got)
	}

	cache.Store(scope, Message{IDs: []string{"b"}, Sender: "Bob", Body: "fresh"})
	cache.Clear()
	got = cache.Build(scope, Quote{IDs: []string{"b"}, Sender: "Bob", Body: "fallback"})
	if strings.Contains(got, "fresh") {
		t.Fatalf("cleared context rendered: %q", got)
	}
}

func assertOrder(t *testing.T, haystack string, values ...string) {
	t.Helper()
	last := -1
	for _, value := range values {
		idx := strings.Index(haystack, value)
		if idx < 0 {
			t.Fatalf("missing %q in %q", value, haystack)
		}
		if idx <= last {
			t.Fatalf("%q appeared out of order in %q", value, haystack)
		}
		last = idx
	}
}
