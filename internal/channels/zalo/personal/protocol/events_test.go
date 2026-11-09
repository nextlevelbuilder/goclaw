package protocol

import (
	"encoding/json"
	"testing"
)

func TestUndoEventFromMessage(t *testing.T) {
	content := Content{Raw: json.RawMessage(`{"deleteMsg":1,"globalMsgId":123,"cliMsgId":456}`)}
	ev := undoEventFromMessage(content, "thread-1", true)
	if ev == nil {
		t.Fatal("expected undo event")
	}
	if ev.ThreadID != "thread-1" || ev.MsgID != "123" || ev.CliMsgID != "456" || !ev.IsGroup {
		t.Fatalf("event = %#v", ev)
	}
}

func TestUndoEventFromMessageRejectsOrdinaryContent(t *testing.T) {
	content := Content{Raw: json.RawMessage(`{"href":"https://example.com"}`)}
	if ev := undoEventFromMessage(content, "thread-1", false); ev != nil {
		t.Fatalf("unexpected undo event: %#v", ev)
	}
}

func TestParseReaction(t *testing.T) {
	raw := json.RawMessage(`{
		"uidFrom":"u1",
		"idTo":"g1",
		"content":{"rMsg":[{"gMsgID":123,"cMsgID":456}],"rIcon":"❤","rType":1,"source":0}
	}`)
	ev := parseReaction(raw, true)
	if ev == nil {
		t.Fatal("expected reaction event")
	}
	if ev.ThreadID != "g1" || ev.MsgID != "123" || ev.CliMsgID != "456" || ev.Icon != "❤" || !ev.IsGroup {
		t.Fatalf("event = %#v", ev)
	}
}

func TestParseReactionStringContent(t *testing.T) {
	raw := json.RawMessage(`{
		"uidFrom":"u1",
		"idTo":"g1",
		"content":"{\"rMsg\":[{\"gMsgID\":123,\"cMsgID\":456}],\"rIcon\":\"😍\"}"
	}`)
	ev := parseReaction(raw, true)
	if ev == nil || ev.Icon != "😍" || ev.MsgID != "123" {
		t.Fatalf("event = %#v", ev)
	}
}

func TestParseReactionDMUsesIDToWhenSelf(t *testing.T) {
	raw := json.RawMessage(`{
		"uidFrom":"0",
		"idTo":"u2",
		"content":{"rMsg":[{"gMsgID":1,"cMsgID":2}],"rIcon":"👍"}
	}`)
	ev := parseReaction(raw, false)
	if ev == nil || ev.ThreadID != "u2" {
		t.Fatalf("event = %#v", ev)
	}
}

func TestParseGroupEventObjectAndString(t *testing.T) {
	ev := parseGroupEvent("group", "join", "", []string{"u1"}, json.RawMessage(`{"groupId":"g1"}`))
	if ev == nil || ev.GroupID != "g1" || ev.Type != "group:join" || len(ev.ActorIDs) != 1 {
		t.Fatalf("event = %#v", ev)
	}

	ev = parseGroupEvent("fr", "accept", "", nil, json.RawMessage(`"{\"grid\":\"g2\"}"`))
	if ev == nil || ev.GroupID != "g2" {
		t.Fatalf("string-data event = %#v", ev)
	}
}
