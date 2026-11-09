package protocol

import (
	"encoding/binary"
	"encoding/json"
	"testing"
)

func eventFrame(t *testing.T, cmd uint16, sub byte, payload string) []byte {
	t.Helper()
	envelope, err := json.Marshal(map[string]any{"encrypt": 0, "data": payload})
	if err != nil {
		t.Fatal(err)
	}
	frame := []byte{1, 0, 0, sub}
	binary.LittleEndian.PutUint16(frame[1:3], cmd)
	return append(frame, envelope...)
}

func newEventTestListener() *Listener {
	return &Listener{
		sess:         &Session{UID: "self"},
		messageCh:    make(chan Message, 1),
		undoCh:       make(chan UndoEvent, 1),
		reactionCh:   make(chan ReactionEvent, 1),
		groupEventCh: make(chan GroupEvent, 1),
		errorCh:      make(chan error, 1),
	}
}

func TestHandleFrameRoutesDMUndo(t *testing.T) {
	ln := newEventTestListener()
	payload := `{"data":{"msgs":[{"msgId":"m1","uidFrom":"u1","idTo":"self","msgType":"webchat","content":{"deleteMsg":1,"globalMsgId":123,"cliMsgId":456}}]}}`
	ln.handleFrame(t.Context(), eventFrame(t, 551, 0, payload))

	select {
	case ev := <-ln.Undos():
		if ev.MsgID != "123" || ev.CliMsgID != "456" || ev.IsGroup {
			t.Fatalf("undo = %#v", ev)
		}
	case <-t.Context().Done():
		t.Fatal("timed out")
	}
}

func TestHandleFrameRoutesGroupUndo(t *testing.T) {
	ln := newEventTestListener()
	payload := `{"data":{"groupMsgs":[{"msgId":"m1","uidFrom":"u1","idTo":"g1","msgType":"webchat","content":{"deleteMsg":1,"globalMsgId":123,"cliMsgId":456}}]}}`
	ln.handleFrame(t.Context(), eventFrame(t, 521, 0, payload))

	select {
	case ev := <-ln.Undos():
		if ev.ThreadID != "g1" || !ev.IsGroup {
			t.Fatalf("undo = %#v", ev)
		}
	case <-t.Context().Done():
		t.Fatal("timed out")
	}
}

func TestHandleFrameRoutesReaction(t *testing.T) {
	ln := newEventTestListener()
	payload := `{"data":{"reactGroups":[{"uidFrom":"u1","idTo":"g1","content":{"rMsg":[{"gMsgID":123,"cMsgID":456}],"rIcon":"❤"}}]}}`
	ln.handleFrame(t.Context(), eventFrame(t, 612, 0, payload))

	select {
	case ev := <-ln.Reactions():
		if ev.ThreadID != "g1" || ev.Icon != "❤" || !ev.IsGroup {
			t.Fatalf("reaction = %#v", ev)
		}
	case <-t.Context().Done():
		t.Fatal("timed out")
	}
}

func TestHandleFrameRoutesGroupEvent(t *testing.T) {
	ln := newEventTestListener()
	payload := `{"data":{"controls":[{"content":{"act_type":"group","act":"join","groupId":"g1","actorIds":["u1"],"data":{"groupId":"g1"}}}]}}`
	ln.handleFrame(t.Context(), eventFrame(t, 601, 0, payload))

	select {
	case ev := <-ln.GroupEvents():
		if ev.GroupID != "g1" || ev.Type != "group:join" {
			t.Fatalf("group event = %#v", ev)
		}
	case <-t.Context().Done():
		t.Fatal("timed out")
	}
}
