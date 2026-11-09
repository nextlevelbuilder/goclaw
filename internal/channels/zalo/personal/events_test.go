package personal

import (
	"strings"
	"testing"

	"github.com/nextlevelbuilder/goclaw/internal/channels"
	"github.com/nextlevelbuilder/goclaw/internal/channels/zalo/personal/protocol"
	"github.com/nextlevelbuilder/goclaw/internal/config"
)

func TestHandleUndoRemovesHistoryReplyAndQuoteCache(t *testing.T) {
	ch := newTestChannel(t, config.ZaloPersonalConfig{DMPolicy: "open", GroupPolicy: "open"})
	ch.GroupHistory().Record("g1", channels.HistoryEntry{MessageID: "m1", Body: "deleted message"}, 10)
	ch.rememberReplyContext("g1", "Alice", protocol.TMessage{MsgID: "m1", CliMsgID: "c1"}, "deleted message")

	ch.handleUndo(protocol.UndoEvent{ThreadID: "g1", MsgID: "m1", CliMsgID: "c1", IsGroup: true})

	if got := ch.GroupHistory().GetEntries("g1"); len(got) != 0 {
		t.Fatalf("history = %#v, want empty", got)
	}
	if _, ok := ch.quoteCache.Get("m1"); ok {
		t.Fatal("quote cache still has recalled message")
	}
	if _, ok := ch.quoteCache.Get("c1"); ok {
		t.Fatal("quote cache still has recalled cli id")
	}
}

func TestHandleUndoReplyToRecalledMessageDoesNotCrash(t *testing.T) {
	ch := newTestChannel(t, config.ZaloPersonalConfig{DMPolicy: "open", GroupPolicy: "open"})
	ch.rememberReplyContext("g1", "Alice", protocol.TMessage{MsgID: "m1", CliMsgID: "c1"}, "deleted message")
	ch.handleUndo(protocol.UndoEvent{ThreadID: "g1", MsgID: "m1", CliMsgID: "c1", IsGroup: true})

	quote := &protocol.TQuote{FromD: "Alice", Msg: "deleted message", CliMsgID: "c1"}
	got := ch.buildReplyContext("g1", quote)
	if !strings.Contains(got, "deleted message") {
		t.Fatalf("fallback quote context = %q", got)
	}
}

func TestHandleReactionRecordsHistoryWithoutWakeup(t *testing.T) {
	ch := newTestChannel(t, config.ZaloPersonalConfig{DMPolicy: "open", GroupPolicy: "open"})
	ch.handleReaction(protocol.ReactionEvent{
		ThreadID: "g1",
		MsgID:    "m1",
		UIDFrom:  "u1",
		Icon:     "❤",
		IsGroup:  true,
	})
	entries := ch.GroupHistory().GetEntries("g1")
	if len(entries) != 1 {
		t.Fatalf("entries = %#v, want one reaction note", entries)
	}
	if !strings.Contains(entries[0].Body, "❤") || !strings.Contains(entries[0].Body, "m1") {
		t.Fatalf("note = %#v", entries[0])
	}
}
