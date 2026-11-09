package personal

import (
	"testing"
	"time"

	"github.com/nextlevelbuilder/goclaw/internal/channels/zalo/personal/protocol"
)

func TestQuoteCacheStoresAllIDsAndExpires(t *testing.T) {
	now := time.Unix(1000, 0)
	cache := newQuoteCache(time.Minute, 10, func() time.Time { return now })
	msg := protocol.TMessage{MsgID: "m1", CliMsgID: "c1", GlobalMsgID: "g1"}
	cache.Store(msg)

	for _, id := range []string{"m1", "c1", "g1"} {
		if got, ok := cache.Get(id); !ok || got.MsgID != "m1" {
			t.Fatalf("Get(%q) = %#v, %v", id, got, ok)
		}
	}

	now = now.Add(2 * time.Minute)
	if _, ok := cache.Get("m1"); ok {
		t.Fatal("expired entry still returned")
	}
}

func TestQuoteCacheDelete(t *testing.T) {
	cache := newQuoteCache(time.Hour, 10, nil)
	cache.Store(protocol.TMessage{MsgID: "m1", CliMsgID: "c1"})
	cache.Delete("m1")
	if _, ok := cache.Get("m1"); ok {
		t.Fatal("deleted key still returned")
	}
	if _, ok := cache.Get("c1"); !ok {
		t.Fatal("deleting one alias removed the other")
	}
}

func TestQuoteCacheCapEvictsOldest(t *testing.T) {
	cache := newQuoteCache(time.Hour, 2, nil)
	cache.Store(protocol.TMessage{MsgID: "m1"})
	cache.Store(protocol.TMessage{MsgID: "m2"})
	cache.Store(protocol.TMessage{MsgID: "m3"})
	if _, ok := cache.Get("m1"); ok {
		t.Fatal("oldest entry was not evicted")
	}
	if _, ok := cache.Get("m3"); !ok {
		t.Fatal("newest entry was evicted")
	}
}

func TestResolveMentionsExactNamesAndAll(t *testing.T) {
	now := time.Unix(1000, 0)
	ch := &Channel{groupCache: newGroupMemberCache(time.Minute, func() time.Time { return now })}
	ch.groupCache.set("g1", map[string]string{
		"Alice":     "u1",
		"Alice Ngô": "u2",
		"Bob":       "u3",
	})

	text := "Hi @Alice Ngô and @Alice, @Bob and @all"
	got := ch.resolveMentions(t.Context(), "g1", text)
	if len(got) != 4 {
		t.Fatalf("mentions = %#v, want 4", got)
	}
	if got[0].UID != "u2" || got[0].Type != int(protocol.MentionEach) {
		t.Fatalf("longest name not preferred: %#v", got[0])
	}
	if got[1].UID != "u1" {
		t.Fatalf("second mention = %#v, want u1", got[1])
	}
	if got[2].UID != "u3" {
		t.Fatalf("third mention = %#v, want u3", got[2])
	}
	if got[3].UID != protocol.MentionAllUID || got[3].Type != int(protocol.MentionAll) {
		t.Fatalf("@all = %#v", got[3])
	}
	for _, m := range got {
		if m.Pos < 0 || m.Len <= 0 || m.Pos+m.Len > utf16Len(text) {
			t.Fatalf("mention out of bounds: %#v (text len %d)", m, utf16Len(text))
		}
	}
}

func TestResolveMentionsRejectsNamePrefix(t *testing.T) {
	ch := &Channel{groupCache: newGroupMemberCache(time.Minute, nil)}
	ch.groupCache.set("g1", map[string]string{"Ann": "u1"})
	if got := ch.resolveMentions(t.Context(), "g1", "@Anna"); len(got) != 0 {
		t.Fatalf("prefix matched as mention: %#v", got)
	}
}

func TestResolveMentionsUTF16Offsets(t *testing.T) {
	ch := &Channel{groupCache: newGroupMemberCache(time.Minute, nil)}
	ch.groupCache.set("g1", map[string]string{"Bob": "u1"})
	got := ch.resolveMentions(t.Context(), "g1", "😀 @Bob")
	if len(got) != 1 {
		t.Fatalf("mentions = %#v", got)
	}
	if got[0].Pos != 3 || got[0].Len != 4 {
		t.Fatalf("mention = %#v, want pos 3 len 4", got[0])
	}
}
